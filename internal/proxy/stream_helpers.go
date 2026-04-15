package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"x-tool/internal/config"
	"x-tool/internal/protocol"
)

type chatStreamCallbacks struct {
	OnText      func(string) error
	OnThinking  func(string) error
	OnToolCalls func([]map[string]any) error
	OnError     func(string) error
	OnDone      func() error
}

type softToolCallSettings struct {
	Protocol            string
	Trigger             string
	Tools               []protocol.Tool
	ToolChoice          any
	validationRulesOnce sync.Once
	validationRules     map[string]toolValidationRule
}

func protocolNameOrDefault(softTool *softToolCallSettings) string {
	if softTool == nil || strings.TrimSpace(softTool.Protocol) == "" {
		return config.SoftToolProtocolXML
	}
	return softTool.Protocol
}

func newSoftToolCallSettings(protocolName, trigger string, tools []protocol.Tool, toolChoice any) *softToolCallSettings {
	return &softToolCallSettings{
		Protocol:   protocolName,
		Trigger:    trigger,
		Tools:      tools,
		ToolChoice: toolChoice,
	}
}

func (s *softToolCallSettings) toolValidationRules() map[string]toolValidationRule {
	if s == nil {
		return nil
	}
	s.validationRulesOnce.Do(func() {
		s.validationRules = buildToolValidationRules(s.Tools)
	})
	return s.validationRules
}

func (a *App) effectiveSoftToolProtocol(upstream config.UpstreamService) string {
	return a.resolveSoftToolPromptConfig(upstream).Protocol
}

func (a *App) buildUpstreamHeaders(stream bool, clientKey string, upstream config.UpstreamService) map[string]string {
	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
	}
	if stream {
		headers["Accept"] = "text/event-stream"
	}
	if a.Config().Features.KeyPassthrough {
		headers["Authorization"] = "Bearer " + clientKey
	} else {
		headers["Authorization"] = "Bearer " + upstream.APIKey
	}
	return headers
}

func (a *App) prepareChatProxyRequest(req *protocol.ChatCompletionRequest, actualModel string, upstream config.UpstreamService) (map[string]any, *softToolCallSettings, error) {
	requestBody := protocol.CloneMap(req.Raw)
	requestBody["model"] = actualModel

	hasTools := len(req.Tools) > 0
	hasFunctionCall := a.Config().Features.EnableFunctionCalling && hasTools
	var softTool *softToolCallSettings
	resolvedSoftTool := a.resolveSoftToolPromptConfig(upstream)
	protocolName := resolvedSoftTool.Protocol
	if hasFunctionCall {
		softTool = newSoftToolCallSettings(protocolName, a.trigger, req.Tools, req.ToolChoice)
	}

	processedMessages := protocol.PreprocessMessages(req.Messages, a.store, a.trigger, protocolName, a.Config().Features.ConvertDeveloperToSystem)
	if !protocol.ValidateMessageStructure(processedMessages, a.Config().Features.ConvertDeveloperToSystem) {
		a.logger.Warn("message structure validation failed")
	}
	requestBody["messages"] = processedMessages

	if hasFunctionCall {
		prompt, err := GenerateFunctionPrompt(req.Tools, a.trigger, resolvedSoftTool.Template, softTool.Protocol)
		if err != nil {
			return nil, nil, err
		}
		if extra := SafeProcessToolChoice(req.ToolChoice, softTool.Protocol); extra != "" {
			prompt += extra
		}

		injection := a.resolvePromptInjection(upstream)
		if injection.Target == config.PromptInjectionTargetLastUser {
			requestBody["messages"] = a.injectPromptIntoLatestChatUserMessage(processedMessages, req.Messages, prompt)
		} else {
			requestBody["messages"] = append([]map[string]any{{
				"role":    injection.Role,
				"content": prompt,
			}}, processedMessages...)
		}
		delete(requestBody, "tools")
		delete(requestBody, "tool_choice")
	} else if hasTools {
		delete(requestBody, "tools")
		delete(requestBody, "tool_choice")
	}

	return requestBody, softTool, nil
}

func (a *App) streamChatCompletion(ctx context.Context, upstreamURL string, requestBody map[string]any, headers map[string]string, model string, softTool *softToolCallSettings, callbacks chatStreamCallbacks) error {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
	hasFunctionCall := softTool != nil
	a.logStreamDebug("chat.completions", "outbound", "request", "upstream_url", upstreamURL, "model", model, "has_function_call", hasFunctionCall, "body", string(payload))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	start := time.Now()
	resp, err := a.client.Do(req)
	if err != nil {
		if callbacks.OnError != nil {
			_ = callbacks.OnError("Failed to connect to upstream service")
		}
		if callbacks.OnDone != nil {
			_ = callbacks.OnDone()
		}
		return err
	}
	defer resp.Body.Close()
	a.logUpstreamResponseInfo("chat.completions", upstreamURL, resp.StatusCode, time.Since(start))
	a.logStreamDebug("chat.completions", "inbound", "headers", "upstream_url", upstreamURL, "status_code", resp.StatusCode, "headers", resp.Header)

	if resp.StatusCode != http.StatusOK {
		if callbacks.OnError != nil {
			_ = callbacks.OnError(mapStreamErrorMessage(resp.StatusCode))
		}
		if callbacks.OnDone != nil {
			_ = callbacks.OnDone()
		}
		return nil
	}

	detector := NewStreamingFunctionCallDetector(a.trigger, protocolNameOrDefault(softTool))
	reader := bufio.NewReader(resp.Body)
	lineNo := 0
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			if callbacks.OnError != nil {
				_ = callbacks.OnError("Failed to connect to upstream service")
			}
			break
		}
		if line == "" && errors.Is(readErr, io.EOF) {
			break
		}

		line = strings.TrimRight(line, "\r\n")
		lineNo++
		if !strings.HasPrefix(line, "data:") {
			a.logStreamDebug("chat.completions", "inbound", "raw_line", "line_no", lineNo, "line", line)
			if errors.Is(readErr, io.EOF) {
				break
			}
			continue
		}

		lineData := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		a.logStreamDebug("chat.completions", "inbound", "data_line", "line_no", lineNo, "line", lineData)
		if lineData == "" || lineData == "[DONE]" {
			if errors.Is(readErr, io.EOF) {
				break
			}
			continue
		}

		var chunk map[string]any
		if err := json.Unmarshal([]byte(lineData), &chunk); err != nil {
			a.logStreamDebug("chat.completions", "inbound", "invalid_json_chunk", "line_no", lineNo, "chunk", lineData, "error", err)
			if errors.Is(readErr, io.EOF) {
				break
			}
			continue
		}

		deltaContent := extractDeltaContent(chunk)
		deltaThinking := extractDeltaThinking(chunk)
		a.logStreamDebug("chat.completions", "inbound", "parsed_chunk", "line_no", lineNo, "chunk", chunk, "delta_content", deltaContent, "delta_thinking", deltaThinking)
		if deltaThinking != "" && callbacks.OnThinking != nil {
			_ = callbacks.OnThinking(deltaThinking)
		}
		if hasFunctionCall {
			if detector.IsToolParsing() {
				detector.AppendToBuffer(deltaContent)
				a.logStreamDebug("chat.completions", "inbound", "tool_parsing_buffer", "buffer", detector.Buffer())
				if detector.HasCompleteToolTurn() {
					validatedTools, err := a.parseSoftToolCalls(detector.Buffer(), softTool)
					if err != nil {
						if callbacks.OnError != nil {
							_ = callbacks.OnError(softToolParseErrorMessage(err))
						}
					} else if len(validatedTools) == 0 {
						if callbacks.OnError != nil {
							_ = callbacks.OnError(softToolParseErrorMessage(nil))
						}
					} else if callbacks.OnToolCalls != nil {
						_ = callbacks.OnToolCalls(a.toolCallsFromValidatedTools(validatedTools))
					}
					if callbacks.OnDone != nil {
						_ = callbacks.OnDone()
					}
					return nil
				}
				continue
			}

			if deltaContent != "" {
				detected, contentToYield := detector.ProcessChunk(deltaContent)
				a.logStreamDebug("chat.completions", "inbound", "delta_processed", "delta", deltaContent, "detected_tool_signal", detected, "content_to_yield", contentToYield)
				if contentToYield != "" && callbacks.OnText != nil {
					_ = callbacks.OnText(contentToYield)
				}
				if detected {
					continue
				}
			}
		} else if deltaContent != "" && callbacks.OnText != nil {
			_ = callbacks.OnText(deltaContent)
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
	}

	if hasFunctionCall && detector.IsToolParsing() {
		validatedTools, err := a.parseSoftToolCalls(detector.Buffer(), softTool)
		a.logStreamDebug("chat.completions", "inbound", "stream_finalize_tool_parsing", "parsed_tools", validatedTools, "buffer", detector.Buffer())
		if err != nil {
			if callbacks.OnError != nil {
				_ = callbacks.OnError(softToolParseErrorMessage(err))
			}
		} else if len(validatedTools) == 0 {
			if callbacks.OnError != nil {
				_ = callbacks.OnError(softToolParseErrorMessage(nil))
			}
		} else if callbacks.OnToolCalls != nil {
			_ = callbacks.OnToolCalls(a.toolCallsFromValidatedTools(validatedTools))
		}
	} else if hasFunctionCall && detector.Buffer() != "" && callbacks.OnText != nil {
		a.logStreamDebug("chat.completions", "inbound", "stream_flush_buffer", "buffer", detector.Buffer())
		_ = callbacks.OnText(detector.Buffer())
	}

	if callbacks.OnDone != nil {
		_ = callbacks.OnDone()
	}
	return nil
}

func (a *App) toolCallsFromParsedTools(parsedTools []protocol.ParsedToolCall, softTool *softToolCallSettings) ([]map[string]any, error) {
	validated, err := a.validateParsedToolCalls(parsedTools, softTool)
	if err != nil {
		return nil, err
	}
	return a.toolCallsFromValidatedTools(toValidatedToolCalls(validated)), nil
}

func writeNamedSSE(w http.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func finishReasonToStopReason(finishReason string) string {
	switch finishReason {
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return "end_turn"
	}
}

func nowUnix() int64 {
	return time.Now().Unix()
}

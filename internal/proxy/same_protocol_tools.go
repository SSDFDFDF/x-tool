package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"x-tool/internal/config"
	"x-tool/internal/protocol"
)

var errResponsesSoftToolsPreviousResponseID = errors.New("previous_response_id is not supported when soft tool conversion is enabled")

func (a *App) buildAnthropicUpstreamHeaders(r *http.Request, stream bool, clientKey string, upstream config.UpstreamService) map[string]string {
	headers := map[string]string{
		"Content-Type":      "application/json",
		"Accept":            "application/json",
		"anthropic-version": strings.TrimSpace(r.Header.Get("anthropic-version")),
	}
	if stream {
		headers["Accept"] = "text/event-stream"
	}
	if headers["anthropic-version"] == "" {
		headers["anthropic-version"] = "2023-06-01"
	}
	if beta := strings.TrimSpace(r.Header.Get("anthropic-beta")); beta != "" {
		headers["anthropic-beta"] = beta
	}
	if a.Config().Features.KeyPassthrough {
		headers["x-api-key"] = clientKey
	} else {
		headers["x-api-key"] = upstream.APIKey
	}
	return headers
}

func (a *App) prepareResponsesSoftToolRequest(req *protocol.ResponsesRequest, actualModel string, upstream config.UpstreamService) (map[string]any, *softToolCallSettings, error) {
	requestBody := protocol.CloneMap(req.Raw)
	requestBody["model"] = actualModel

	hasTools := len(req.Tools) > 0
	hasFunctionCall := a.Config().Features.EnableFunctionCalling && hasTools
	var softTool *softToolCallSettings
	resolvedSoftTool := a.resolveSoftToolPromptConfig(upstream)
	protocolName := resolvedSoftTool.Protocol
	if hasFunctionCall {
		if previousResponseID, _ := requestBody["previous_response_id"].(string); strings.TrimSpace(previousResponseID) != "" {
			return nil, nil, errResponsesSoftToolsPreviousResponseID
		}
		chatTools, err := protocol.ResponsesToolsToChatTools(req.Tools)
		if err != nil {
			return nil, nil, err
		}
		softTool = &softToolCallSettings{
			Protocol:   protocolName,
			Trigger:    a.trigger,
			Tools:      chatTools,
			ToolChoice: protocol.AdaptResponsesToolChoice(req.ToolChoice),
		}
		prompt, err := GenerateFunctionPrompt(chatTools, a.trigger, resolvedSoftTool.Template, protocolName)
		if err != nil {
			return nil, nil, err
		}
		if extra := SafeProcessToolChoice(softTool.ToolChoice, protocolName); extra != "" {
			prompt += extra
		}

		injection := a.resolvePromptInjection(upstream)
		if injection.Target == config.PromptInjectionTargetMessage {
			requestBody["input"] = a.prependResponsesPromptInput(req.Input, prompt, injection.Role)
		} else {
			instructions := strings.TrimSpace(req.Instructions)
			if instructions != "" {
				requestBody["instructions"] = prompt + "\n\n" + instructions
			} else {
				requestBody["instructions"] = prompt
			}
		}
		requestBody["input"] = a.preprocessResponsesInputForSoftTools(requestBody["input"], protocolName)
		delete(requestBody, "tools")
		delete(requestBody, "tool_choice")
	} else if hasTools {
		delete(requestBody, "tools")
		delete(requestBody, "tool_choice")
	}

	return requestBody, softTool, nil
}

func (a *App) prependResponsesPromptInput(input any, prompt, role string) any {
	promptItem := map[string]any{
		"type": "message",
		"role": role,
		"content": []map[string]any{{
			"type": "input_text",
			"text": prompt,
		}},
	}

	switch value := input.(type) {
	case nil:
		return []any{promptItem}
	case string:
		return []any{
			promptItem,
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{{
					"type": "input_text",
					"text": value,
				}},
			},
		}
	case []any:
		return append([]any{promptItem}, value...)
	default:
		return []any{promptItem, value}
	}
}

func (a *App) preprocessResponsesInputForSoftTools(input any, protocolName string) any {
	items, ok := input.([]any)
	if !ok {
		return input
	}

	processed := make([]any, 0, len(items))
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			processed = append(processed, rawItem)
			continue
		}
		if itemType, _ := item["type"].(string); itemType == "function_call_output" {
			callID, _ := item["call_id"].(string)
			content := protocol.FormatToolResultForAI(a.store, callID, stringifyProxyValue(item["output"]), protocolName)
			processed = append(processed, map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{{
					"type": "input_text",
					"text": content,
				}},
			})
			continue
		}
		processed = append(processed, protocol.CloneMap(item))
	}
	return processed
}

func (a *App) prepareAnthropicSoftToolRequest(req *protocol.AnthropicRequest, actualModel string, upstream config.UpstreamService) (map[string]any, *softToolCallSettings, error) {
	requestBody := protocol.CloneMap(req.Raw)
	requestBody["model"] = actualModel

	hasTools := len(req.Tools) > 0
	hasFunctionCall := a.Config().Features.EnableFunctionCalling && hasTools
	var softTool *softToolCallSettings
	resolvedSoftTool := a.resolveSoftToolPromptConfig(upstream)
	protocolName := resolvedSoftTool.Protocol
	if hasFunctionCall {
		chatTools := protocol.AnthropicToolsToChatTools(req.Tools)
		softTool = &softToolCallSettings{
			Protocol:   protocolName,
			Trigger:    a.trigger,
			Tools:      chatTools,
			ToolChoice: protocol.AdaptAnthropicToolChoice(req.ToolChoice),
		}
		prompt, err := GenerateFunctionPrompt(chatTools, a.trigger, resolvedSoftTool.Template, protocolName)
		if err != nil {
			return nil, nil, err
		}
		if extra := SafeProcessToolChoice(softTool.ToolChoice, protocolName); extra != "" {
			prompt += extra
		}

		injection := a.resolvePromptInjection(upstream)
		if injection.Target == config.PromptInjectionTargetSystem {
			systemText := anthropicSystemToPromptString(req.System)
			if systemText != "" {
				requestBody["system"] = prompt + "\n\n" + systemText
			} else {
				requestBody["system"] = prompt
			}
		}
		messages := a.preprocessAnthropicMessagesForSoftTools(req.Messages, protocolName)
		if injection.Target == config.PromptInjectionTargetMessage {
			messages = prependAnthropicPromptMessage(messages, prompt, injection.Role)
		}
		requestBody["messages"] = messages
		delete(requestBody, "tools")
		delete(requestBody, "tool_choice")
	} else if hasTools {
		delete(requestBody, "tools")
		delete(requestBody, "tool_choice")
	}

	return requestBody, softTool, nil
}

func anthropicSystemToPromptString(system any) string {
	switch value := system.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := make([]string, 0, len(value))
		for _, raw := range value {
			part, _ := raw.(map[string]any)
			if partType, _ := part["type"].(string); partType != "text" {
				continue
			}
			if text, _ := part["text"].(string); strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func prependAnthropicPromptMessage(messages []map[string]any, prompt, role string) []map[string]any {
	promptMessage := map[string]any{
		"role":    role,
		"content": prompt,
	}
	return append([]map[string]any{promptMessage}, messages...)
}

func (a *App) preprocessAnthropicMessagesForSoftTools(messages []map[string]any, protocolName string) []map[string]any {
	processed := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		cloned := protocol.CloneMap(message)
		role, _ := cloned["role"].(string)
		switch role {
		case "user":
			cloned["content"] = a.preprocessAnthropicUserContentForSoftTools(cloned["content"], protocolName)
		case "assistant":
			cloned["content"] = a.preprocessAnthropicAssistantContentForSoftTools(cloned["content"], protocolName)
		}
		processed = append(processed, cloned)
	}
	return processed
}

func (a *App) preprocessAnthropicUserContentForSoftTools(content any, protocolName string) any {
	parts, ok := content.([]any)
	if !ok {
		return content
	}
	processed := make([]any, 0, len(parts))
	for _, rawPart := range parts {
		part, ok := rawPart.(map[string]any)
		if !ok {
			processed = append(processed, rawPart)
			continue
		}
		if partType, _ := part["type"].(string); partType == "tool_result" {
			callID, _ := part["tool_use_id"].(string)
			processed = append(processed, map[string]any{
				"type": "text",
				"text": protocol.FormatToolResultForAI(a.store, callID, anthropicContentToPromptString(part["content"]), protocolName),
			})
			continue
		}
		processed = append(processed, protocol.CloneMap(part))
	}
	return processed
}

func (a *App) preprocessAnthropicAssistantContentForSoftTools(content any, protocolName string) any {
	parts, ok := content.([]any)
	if !ok {
		return content
	}
	processed := make([]any, 0, len(parts))
	toolCalls := make([]any, 0)
	for _, rawPart := range parts {
		part, ok := rawPart.(map[string]any)
		if !ok {
			processed = append(processed, rawPart)
			continue
		}
		if partType, _ := part["type"].(string); partType == "tool_use" {
			toolCalls = append(toolCalls, map[string]any{
				"id":   part["id"],
				"type": "function",
				"function": map[string]any{
					"name":      part["name"],
					"arguments": mustJSON(part["input"]),
				},
			})
			continue
		}
		processed = append(processed, protocol.CloneMap(part))
	}
	if len(toolCalls) > 0 {
		processed = append(processed, map[string]any{
			"type": "text",
			"text": protocol.FormatAssistantToolCallsForAI(toolCalls, a.trigger, protocolName),
		})
	}
	return processed
}

func anthropicContentToPromptString(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		var parts []string
		for _, raw := range value {
			part, _ := raw.(map[string]any)
			if text, _ := part["text"].(string); strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return stringifyProxyValue(content)
	}
}

func (a *App) transformResponsesResponse(payload map[string]any, softTool *softToolCallSettings) {
	if softTool == nil {
		return
	}
	outputRaw, ok := payload["output"].([]any)
	if !ok || len(outputRaw) == 0 {
		return
	}

	output := make([]map[string]any, 0, len(outputRaw))
	rewritten := false
	convertedToolCount := 0
	for _, rawItem := range outputRaw {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		if rewritten {
			output = append(output, protocol.CloneMap(item))
			continue
		}
		if itemType, _ := item["type"].(string); itemType != "message" {
			output = append(output, protocol.CloneMap(item))
			continue
		}

		content := extractResponsesMessageText(item)
		trigger := a.trigger
		if softTool.Trigger != "" {
			trigger = softTool.Trigger
		}
		signalPos := strings.Index(content, trigger)
		if signalPos < 0 {
			output = append(output, protocol.CloneMap(item))
			continue
		}

		parsedTools := a.parseSoftToolCalls(content[signalPos:], softTool)
		if len(parsedTools) == 0 {
			output = append(output, protocol.CloneMap(item))
			continue
		}

		toolCalls, err := a.toolCallsFromParsedTools(parsedTools, softTool)
		if err != nil {
			output = append(output, protocol.CloneMap(item))
			continue
		}

		if prefix := strings.TrimSpace(content[:signalPos]); prefix != "" {
			output = append(output, responsesMessageOutputItem(prefix))
		}
		for _, toolCall := range toolCalls {
			output = append(output, responsesFunctionCallOutputItem(toolCall))
		}
		convertedToolCount = len(toolCalls)
		rewritten = true
	}

	if rewritten {
		payload["output"] = anySliceFromMaps(output)
		payload["output_text"] = extractResponsesOutputText(output)
		a.logInfo("tool.transform", "protocol", "responses", "tool_count", convertedToolCount, "result", "ok")
	}
}

func extractResponsesMessageText(item map[string]any) string {
	content, ok := item["content"].([]any)
	if !ok {
		if typed, ok := item["content"].([]map[string]any); ok {
			for _, part := range typed {
				if text, ok := part["text"].(string); ok {
					return text
				}
			}
		}
		return ""
	}
	var builder strings.Builder
	for _, rawPart := range content {
		part, _ := rawPart.(map[string]any)
		partType, _ := part["type"].(string)
		if partType != "output_text" && partType != "text" {
			continue
		}
		if text, _ := part["text"].(string); text != "" {
			builder.WriteString(text)
		}
	}
	return builder.String()
}

func responsesMessageOutputItem(content string) map[string]any {
	return map[string]any{
		"id":     newID("msg_"),
		"type":   "message",
		"status": "completed",
		"role":   "assistant",
		"content": []map[string]any{{
			"type":        "output_text",
			"text":        content,
			"annotations": []any{},
			"logprobs":    []any{},
		}},
	}
}

func responsesFunctionCallOutputItem(toolCall map[string]any) map[string]any {
	functionInfo, _ := toolCall["function"].(map[string]any)
	return map[string]any{
		"id":        newID("fc_"),
		"type":      "function_call",
		"status":    "completed",
		"call_id":   toolCall["id"],
		"name":      functionInfo["name"],
		"arguments": functionInfo["arguments"],
	}
}

func extractResponsesOutputText(output []map[string]any) string {
	var parts []string
	for _, item := range output {
		if itemType, _ := item["type"].(string); itemType != "message" {
			continue
		}
		if text := extractResponsesMessageText(item); strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func anySliceFromMaps(items []map[string]any) []any {
	result := make([]any, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	return result
}

func (a *App) transformAnthropicResponse(payload map[string]any, softTool *softToolCallSettings) {
	if softTool == nil {
		return
	}
	contentRaw, ok := payload["content"].([]any)
	if !ok || len(contentRaw) == 0 {
		return
	}

	trigger := a.trigger
	if softTool.Trigger != "" {
		trigger = softTool.Trigger
	}

	rewritten := false
	content := make([]any, 0, len(contentRaw))
	convertedToolCount := 0
	for _, rawBlock := range contentRaw {
		block, ok := rawBlock.(map[string]any)
		if !ok {
			content = append(content, rawBlock)
			continue
		}
		if rewritten {
			content = append(content, protocol.CloneMap(block))
			continue
		}
		if blockType, _ := block["type"].(string); blockType != "text" {
			content = append(content, protocol.CloneMap(block))
			continue
		}
		text, _ := block["text"].(string)
		signalPos := strings.Index(text, trigger)
		if signalPos < 0 {
			content = append(content, protocol.CloneMap(block))
			continue
		}
		parsedTools := a.parseSoftToolCalls(text[signalPos:], softTool)
		if len(parsedTools) == 0 {
			content = append(content, protocol.CloneMap(block))
			continue
		}
		toolCalls, err := a.toolCallsFromParsedTools(parsedTools, softTool)
		if err != nil {
			content = append(content, protocol.CloneMap(block))
			continue
		}
		if prefix := strings.TrimSpace(text[:signalPos]); prefix != "" {
			content = append(content, map[string]any{
				"type": "text",
				"text": prefix,
			})
		}
		for _, toolCall := range toolCalls {
			functionInfo, _ := toolCall["function"].(map[string]any)
			var input map[string]any
			if arguments, _ := functionInfo["arguments"].(string); arguments != "" {
				_ = json.Unmarshal([]byte(arguments), &input)
			}
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    toolCall["id"],
				"name":  functionInfo["name"],
				"input": input,
			})
		}
		convertedToolCount = len(toolCalls)
		rewritten = true
	}

	if rewritten {
		payload["content"] = content
		payload["stop_reason"] = "tool_use"
		a.logInfo("tool.transform", "protocol", "anthropic.messages", "tool_count", convertedToolCount, "result", "ok")
	}
}

func (a *App) streamResponsesFromResponsesUpstream(w http.ResponseWriter, ctx context.Context, upstreamURL string, requestBody map[string]any, headers map[string]string, model string, _ *softToolCallSettings) {
	a.streamResponsesWithCallbacks(w, model, func(callbacks chatStreamCallbacks) error {
		return a.streamResponsesUpstream(ctx, upstreamURL, requestBody, headers, callbacks)
	})
}

func (a *App) streamResponsesUpstream(ctx context.Context, upstreamURL string, requestBody map[string]any, headers map[string]string, callbacks chatStreamCallbacks) error {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
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
	a.logUpstreamResponseInfo("responses", upstreamURL, resp.StatusCode, time.Since(start))
	if resp.StatusCode != http.StatusOK {
		if callbacks.OnError != nil {
			_ = callbacks.OnError(mapStreamErrorMessage(resp.StatusCode))
		}
		if callbacks.OnDone != nil {
			_ = callbacks.OnDone()
		}
		return nil
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "data:") {
				payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if payload != "" && payload != "[DONE]" {
					var event map[string]any
					if json.Unmarshal([]byte(payload), &event) == nil {
						switch eventType, _ := event["type"].(string); eventType {
						case "response.output_text.delta":
							if delta, _ := event["delta"].(string); delta != "" && callbacks.OnText != nil {
								_ = callbacks.OnText(delta)
							}
						case "response.reasoning_summary_text.delta":
							if delta, _ := event["delta"].(string); delta != "" && callbacks.OnThinking != nil {
								_ = callbacks.OnThinking(delta)
							}
						case "response.failed":
							if callbacks.OnError != nil {
								errorBody, _ := event["error"].(map[string]any)
								message, _ := errorBody["message"].(string)
								if message == "" {
									message = "Request processing failed"
								}
								_ = callbacks.OnError(message)
							}
						}
					}
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) && callbacks.OnError != nil {
				_ = callbacks.OnError("Failed to connect to upstream service")
			}
			break
		}
	}
	if callbacks.OnDone != nil {
		_ = callbacks.OnDone()
	}
	return nil
}

func stringifyProxyValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(data)
	}
}

func (a *App) streamAnthropicFromAnthropicUpstream(w http.ResponseWriter, ctx context.Context, upstreamURL string, requestBody map[string]any, headers map[string]string, model string, _ *softToolCallSettings) {
	a.streamAnthropicWithCallbacks(w, model, func(callbacks chatStreamCallbacks) error {
		return a.streamAnthropicUpstream(ctx, upstreamURL, requestBody, headers, callbacks)
	})
}

func (a *App) streamAnthropicUpstream(ctx context.Context, upstreamURL string, requestBody map[string]any, headers map[string]string, callbacks chatStreamCallbacks) error {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
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
	a.logUpstreamResponseInfo("anthropic.messages", upstreamURL, resp.StatusCode, time.Since(start))
	if resp.StatusCode != http.StatusOK {
		if callbacks.OnError != nil {
			_ = callbacks.OnError(mapStreamErrorMessage(resp.StatusCode))
		}
		if callbacks.OnDone != nil {
			_ = callbacks.OnDone()
		}
		return nil
	}

	reader := bufio.NewReader(resp.Body)
	eventName := ""
	var dataLines []string
	processEvent := func() {
		if len(dataLines) == 0 {
			return
		}
		payload := strings.Join(dataLines, "\n")
		dataLines = nil
		var event map[string]any
		if json.Unmarshal([]byte(payload), &event) != nil {
			return
		}
		switch eventName {
		case "content_block_delta":
			delta, _ := event["delta"].(map[string]any)
			switch deltaType, _ := delta["type"].(string); deltaType {
			case "text_delta":
				if text, _ := delta["text"].(string); text != "" && callbacks.OnText != nil {
					_ = callbacks.OnText(text)
				}
			case "thinking_delta":
				if thinking, _ := delta["thinking"].(string); thinking != "" && callbacks.OnThinking != nil {
					_ = callbacks.OnThinking(thinking)
				}
			}
		case "error":
			if callbacks.OnError != nil {
				errorBody, _ := event["error"].(map[string]any)
				message, _ := errorBody["message"].(string)
				if message == "" {
					message = "Request processing failed"
				}
				_ = callbacks.OnError(message)
			}
		}
	}

	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "":
				processEvent()
				eventName = ""
			case strings.HasPrefix(line, "event:"):
				eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) && callbacks.OnError != nil {
				_ = callbacks.OnError("Failed to connect to upstream service")
			}
			break
		}
	}
	processEvent()
	if callbacks.OnDone != nil {
		_ = callbacks.OnDone()
	}
	return nil
}

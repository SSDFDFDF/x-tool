package proxy

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"x-tool/internal/config"
	"x-tool/internal/protocol"
)

func (a *App) emitToolCalls(w http.ResponseWriter, parsedTools []protocol.ParsedToolCall, model string) bool {
	if len(parsedTools) == 0 {
		return false
	}
	toolCalls, err := a.toolCallsFromParsedTools(parsedTools, nil)
	if err != nil {
		return false
	}
	return a.emitPreparedToolCalls(w, toolCalls, model) == nil
}

func (a *App) parseSoftToolCalls(content string, softTool *softToolCallSettings) []protocol.ParsedToolCall {
	if softTool == nil {
		return nil
	}
	var parsed []protocol.ParsedToolCall
	switch protocolNameOrDefault(softTool) {
	case config.SoftToolProtocolMarkdownBlock:
		parsed = ParseFunctionCallsMarkdownBlockWithTools(content, softTool.Trigger, softTool.Tools)
	case config.SoftToolProtocolSentinelJSON:
		parsed = ParseFunctionCallsSentinelJSON(content, softTool.Trigger)
	default:
		parsed = ParseFunctionCallsXML(content, softTool.Trigger)
	}
	if len(parsed) == 0 {
		a.logInfo("tool.parse",
			"protocol", protocolNameOrDefault(softTool),
			"tool_count", 0,
			"result", "empty",
		)
		return nil
	}
	validated, err := a.validateParsedToolCalls(parsed, softTool)
	if err != nil {
		a.logInfo("tool.parse",
			"protocol", protocolNameOrDefault(softTool),
			"tool_count", len(parsed),
			"result", "invalid",
		)
		a.logger.Warn("soft tool validation failed", "error", err.Error())
		return nil
	}
	a.logInfo("tool.parse",
		"protocol", protocolNameOrDefault(softTool),
		"tool_count", len(validated),
		"result", "ok",
	)
	return validated
}

func (a *App) validateParsedToolCalls(parsedTools []protocol.ParsedToolCall, softTool *softToolCallSettings) ([]protocol.ParsedToolCall, error) {
	if len(parsedTools) == 0 || softTool == nil {
		return parsedTools, nil
	}

	if choice, ok := softTool.ToolChoice.(string); ok && choice == "none" {
		return nil, fmt.Errorf("tool_choice none forbids tool calls")
	}

	allowedTools := make(map[string]map[string]struct{}, len(softTool.Tools))
	for _, tool := range softTool.Tools {
		required := map[string]struct{}{}
		switch raw := tool.Function.Parameters["required"].(type) {
		case []any:
			for _, item := range raw {
				if name, ok := item.(string); ok && strings.TrimSpace(name) != "" {
					required[name] = struct{}{}
				}
			}
		case []string:
			for _, name := range raw {
				if strings.TrimSpace(name) != "" {
					required[name] = struct{}{}
				}
			}
		}
		allowedTools[tool.Function.Name] = required
	}

	for _, tool := range parsedTools {
		required, ok := allowedTools[tool.Name]
		if !ok {
			return nil, fmt.Errorf("tool %q was not advertised", tool.Name)
		}
		for name := range required {
			if _, ok := tool.Args[name]; !ok {
				return nil, fmt.Errorf("tool %q missing required parameter %q", tool.Name, name)
			}
		}
	}

	if function, ok := softTool.ToolChoice.(map[string]any); ok {
		if value, ok := function["function"].(map[string]any); ok {
			if requiredName, ok := value["name"].(string); ok && strings.TrimSpace(requiredName) != "" {
				if len(parsedTools) != 1 {
					return nil, fmt.Errorf("tool_choice requires exactly one call to %q", requiredName)
				}
				if parsedTools[0].Name != requiredName {
					return nil, fmt.Errorf("tool_choice requires %q, got %q", requiredName, parsedTools[0].Name)
				}
			}
		}
	}

	return parsedTools, nil
}

func (a *App) emitPreparedToolCalls(w http.ResponseWriter, toolCalls []map[string]any, model string) error {
	if err := a.writeLoggedSSE(w, "chat.completions", "", map[string]any{
		"id":      newID("chatcmpl-"),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index": 0,
			"delta": map[string]any{
				"role":       "assistant",
				"content":    nil,
				"tool_calls": toolCalls,
			},
			"finish_reason": nil,
		}},
	}); err != nil {
		return err
	}
	if err := a.writeLoggedSSE(w, "chat.completions", "", map[string]any{
		"id":      newID("chatcmpl-"),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": "tool_calls",
		}},
	}); err != nil {
		return err
	}
	return nil
}

func (a *App) transformNonStreamResponse(payload map[string]any, softTool *softToolCallSettings) {
	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		return
	}
	firstChoice, ok := choices[0].(map[string]any)
	if !ok {
		return
	}
	message, ok := firstChoice["message"].(map[string]any)
	if !ok {
		return
	}
	content, _ := message["content"].(string)
	if content == "" {
		return
	}

	trigger := a.trigger
	if softTool != nil && strings.TrimSpace(softTool.Trigger) != "" {
		trigger = softTool.Trigger
	}

	signalPos := strings.Index(content, trigger)
	if signalPos < 0 {
		return
	}

	parsedTools := a.parseSoftToolCalls(content[signalPos:], softTool)
	if len(parsedTools) == 0 {
		return
	}

	toolCalls := make([]map[string]any, 0, len(parsedTools))
	for _, tool := range parsedTools {
		id := newID("call_")
		a.store.Put(id, tool.Name, tool.Args, "Calling tool "+tool.Name)
		toolCalls = append(toolCalls, map[string]any{
			"id":   id,
			"type": "function",
			"function": map[string]any{
				"name":      tool.Name,
				"arguments": mustJSON(tool.Args),
			},
		})
	}

	contentBeforeSignal := strings.TrimSpace(content[:signalPos])
	firstChoice["message"] = map[string]any{
		"role":       "assistant",
		"content":    nilIfEmpty(contentBeforeSignal),
		"tool_calls": toolCalls,
	}
	firstChoice["finish_reason"] = "tool_calls"
	a.logInfo("tool.transform", "protocol", "chat.completions", "tool_count", len(toolCalls), "result", "ok")
}

package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type AnthropicRequest struct {
	Raw         map[string]any
	Model       string
	MaxTokens   any
	System      any
	Messages    []map[string]any
	Tools       []AnthropicTool `json:"tools"`
	ToolChoice  any             `json:"tool_choice"`
	Stream      bool            `json:"stream"`
	Temperature any             `json:"temperature"`
	TopP        any             `json:"top_p"`
	StopSeqs    any             `json:"stop_sequences"`
}

type AnthropicTool struct {
	Name        string         `json:"name"`
	Description *string        `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

func DecodeAnthropicRequest(data []byte) (*AnthropicRequest, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode request body: %w", err)
	}

	var typed struct {
		Model         string           `json:"model"`
		MaxTokens     any              `json:"max_tokens"`
		System        any              `json:"system"`
		Messages      []map[string]any `json:"messages"`
		Tools         []AnthropicTool  `json:"tools"`
		ToolChoice    any              `json:"tool_choice"`
		Stream        bool             `json:"stream"`
		Temperature   any              `json:"temperature"`
		TopP          any              `json:"top_p"`
		StopSequences any              `json:"stop_sequences"`
	}
	if err := json.Unmarshal(data, &typed); err != nil {
		return nil, fmt.Errorf("decode typed request body: %w", err)
	}
	if strings.TrimSpace(typed.Model) == "" {
		return nil, fmt.Errorf("model is required")
	}
	if len(typed.Messages) == 0 {
		return nil, fmt.Errorf("messages is required")
	}

	return &AnthropicRequest{
		Raw:         raw,
		Model:       typed.Model,
		MaxTokens:   typed.MaxTokens,
		System:      typed.System,
		Messages:    typed.Messages,
		Tools:       typed.Tools,
		ToolChoice:  typed.ToolChoice,
		Stream:      typed.Stream,
		Temperature: typed.Temperature,
		TopP:        typed.TopP,
		StopSeqs:    typed.StopSequences,
	}, nil
}

func AdaptAnthropicRequestToChat(req *AnthropicRequest) (*ChatCompletionRequest, error) {
	messages, err := anthropicMessagesToChat(req.System, req.Messages)
	if err != nil {
		return nil, err
	}

	raw := cloneMap(req.Raw)
	delete(raw, "system")
	delete(raw, "max_tokens")
	delete(raw, "stop_sequences")
	delete(raw, "top_k")

	raw["model"] = req.Model
	raw["messages"] = messages
	if req.MaxTokens != nil {
		raw["max_tokens"] = req.MaxTokens
	}
	if req.StopSeqs != nil {
		raw["stop"] = req.StopSeqs
	}

	tools := make([]Tool, 0, len(req.Tools))
	for _, tool := range req.Tools {
		tools = append(tools, Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}
	if len(tools) > 0 {
		raw["tools"] = toAnySlice(tools)
	}
	toolChoice := adaptAnthropicToolChoice(req.ToolChoice)
	if toolChoice != nil {
		raw["tool_choice"] = toolChoice
	}

	return &ChatCompletionRequest{
		Raw:        raw,
		Model:      req.Model,
		Messages:   messages,
		Tools:      tools,
		ToolChoice: toolChoice,
		Stream:     req.Stream,
	}, nil
}

func AnthropicToolsToChatTools(input []AnthropicTool) []Tool {
	tools := make([]Tool, 0, len(input))
	for _, tool := range input {
		tools = append(tools, Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}
	return tools
}

func AdaptAnthropicToolChoice(input any) any {
	return adaptAnthropicToolChoice(input)
}

func anthropicMessagesToChat(system any, input []map[string]any) ([]map[string]any, error) {
	messages := make([]map[string]any, 0, len(input)+1)
	if systemText := anthropicSystemToString(system); strings.TrimSpace(systemText) != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": systemText,
		})
	}

	for _, message := range input {
		role, _ := message["role"].(string)
		content := message["content"]
		switch role {
		case "user":
			textParts, toolResults := anthropicUserContentToChat(content)
			messages = append(messages, toolResults...)
			if strings.TrimSpace(textParts) != "" || len(toolResults) == 0 {
				messages = append(messages, map[string]any{
					"role":    "user",
					"content": nilIfEmpty(textParts),
				})
			}
		case "assistant":
			text, toolCalls := anthropicAssistantContentToChat(content)
			assistant := map[string]any{
				"role": "assistant",
			}
			if strings.TrimSpace(text) != "" {
				assistant["content"] = text
			}
			if len(toolCalls) > 0 {
				assistant["tool_calls"] = toolCalls
			}
			messages = append(messages, assistant)
		default:
			messages = append(messages, map[string]any{
				"role":    role,
				"content": content,
			})
		}
	}
	return messages, nil
}

func anthropicSystemToString(system any) string {
	switch value := system.(type) {
	case string:
		return value
	case []any:
		var parts []string
		for _, rawPart := range value {
			part, _ := rawPart.(map[string]any)
			if partType, _ := part["type"].(string); partType == "text" {
				if text, _ := part["text"].(string); strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func anthropicUserContentToChat(content any) (string, []map[string]any) {
	switch value := content.(type) {
	case string:
		return value, nil
	case []any:
		var textParts []string
		var toolResults []map[string]any
		for _, rawPart := range value {
			part, _ := rawPart.(map[string]any)
			partType, _ := part["type"].(string)
			switch partType {
			case "text":
				if text, _ := part["text"].(string); strings.TrimSpace(text) != "" {
					textParts = append(textParts, text)
				}
			case "tool_result":
				toolUseID, _ := part["tool_use_id"].(string)
				toolResults = append(toolResults, map[string]any{
					"role":         "tool",
					"tool_call_id": toolUseID,
					"content":      anthropicContentToString(part["content"]),
				})
			}
		}
		return strings.Join(textParts, "\n"), toolResults
	default:
		return stringifyValue(content), nil
	}
}

func anthropicAssistantContentToChat(content any) (string, []any) {
	switch value := content.(type) {
	case string:
		return value, nil
	case []any:
		var textParts []string
		var toolCalls []any
		for _, rawPart := range value {
			part, _ := rawPart.(map[string]any)
			partType, _ := part["type"].(string)
			switch partType {
			case "text":
				if text, _ := part["text"].(string); strings.TrimSpace(text) != "" {
					textParts = append(textParts, text)
				}
			case "tool_use":
				name, _ := part["name"].(string)
				id, _ := part["id"].(string)
				input, _ := part["input"].(map[string]any)
				toolCalls = append(toolCalls, map[string]any{
					"id":   id,
					"type": "function",
					"function": map[string]any{
						"name":      name,
						"arguments": mustJSON(input),
					},
				})
			}
		}
		return strings.Join(textParts, "\n"), toolCalls
	default:
		return stringifyValue(content), nil
	}
}

func adaptAnthropicToolChoice(input any) any {
	value, ok := input.(map[string]any)
	if !ok {
		return nil
	}
	choiceType, _ := value["type"].(string)
	switch choiceType {
	case "none":
		return "none"
	case "tool":
		name, _ := value["name"].(string)
		if strings.TrimSpace(name) == "" {
			return nil
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": name,
			},
		}
	case "any":
		return "required"
	default:
		return nil
	}
}

func ConvertChatCompletionToAnthropic(payload map[string]any, model string) map[string]any {
	choices, _ := payload["choices"].([]any)
	contentBlocks := make([]map[string]any, 0)
	stopReason := "end_turn"
	if len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if finish, _ := choice["finish_reason"].(string); finish != "" {
				stopReason = finishReasonToStopReason(finish)
			}
			if message, ok := choice["message"].(map[string]any); ok {
				if content, _ := message["content"].(string); strings.TrimSpace(content) != "" {
					contentBlocks = append(contentBlocks, map[string]any{
						"type": "text",
						"text": content,
					})
				}
				for _, toolCall := range toolCallSlice(message["tool_calls"]) {
					functionInfo, _ := toolCall["function"].(map[string]any)
					name, _ := functionInfo["name"].(string)
					arguments, _ := functionInfo["arguments"].(string)
					var input map[string]any
					_ = UnmarshalJSONWithRepair(arguments, &input)
					contentBlocks = append(contentBlocks, map[string]any{
						"type":  "tool_use",
						"id":    toolCall["id"],
						"name":  name,
						"input": input,
					})
				}
			}
		}
	}

	return map[string]any{
		"id":            newID("msg_"),
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       contentBlocks,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage":         anthropicUsageFromChat(payload),
	}
}

func anthropicUsageFromChat(payload map[string]any) map[string]any {
	usage, _ := payload["usage"].(map[string]any)
	inputTokens, _ := usage["prompt_tokens"]
	outputTokens, _ := usage["completion_tokens"]
	return map[string]any{
		"input_tokens":  zeroIfNil(inputTokens),
		"output_tokens": zeroIfNil(outputTokens),
	}
}

func anthropicContentToString(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		var parts []string
		for _, rawPart := range value {
			part, _ := rawPart.(map[string]any)
			if text, _ := part["text"].(string); strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return stringifyValue(content)
	}
}

func verifyAnthropicAPIKey(r *http.Request, allowedKeys map[string]struct{}, keyPassthrough bool) (string, error) {
	if key := strings.TrimSpace(r.Header.Get("x-api-key")); key != "" {
		if keyPassthrough {
			return key, nil
		}
		if _, ok := allowedKeys[key]; ok {
			return key, nil
		}
		return "", errors.New("unauthorized")
	}

	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(auth, "Bearer ") {
		key := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if keyPassthrough {
			return key, nil
		}
		if _, ok := allowedKeys[key]; ok {
			return key, nil
		}
		return "", errors.New("unauthorized")
	}

	return "", ErrMissingAuthorization
}

func VerifyAnthropicAPIKey(r *http.Request, allowedKeys map[string]struct{}, keyPassthrough bool) (string, error) {
	return verifyAnthropicAPIKey(r, allowedKeys, keyPassthrough)
}

func zeroIfNil(value any) any {
	if value == nil {
		return 0
	}
	return value
}

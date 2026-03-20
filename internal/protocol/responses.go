package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ResponsesRequest struct {
	Raw          map[string]any
	Model        string
	Input        any
	Instructions string
	Tools        []ResponseTool
	ToolChoice   any
	Stream       bool
}

type ResponseTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description *string        `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Function    *ToolFunction  `json:"function"`
}

func DecodeResponsesRequest(data []byte) (*ResponsesRequest, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode request body: %w", err)
	}

	var typed struct {
		Model        string         `json:"model"`
		Input        any            `json:"input"`
		Instructions string         `json:"instructions"`
		Tools        []ResponseTool `json:"tools"`
		ToolChoice   any            `json:"tool_choice"`
		Stream       bool           `json:"stream"`
	}
	if err := json.Unmarshal(data, &typed); err != nil {
		return nil, fmt.Errorf("decode typed request body: %w", err)
	}
	if strings.TrimSpace(typed.Model) == "" {
		return nil, fmt.Errorf("model is required")
	}

	return &ResponsesRequest{
		Raw:          raw,
		Model:        typed.Model,
		Input:        typed.Input,
		Instructions: typed.Instructions,
		Tools:        typed.Tools,
		ToolChoice:   typed.ToolChoice,
		Stream:       typed.Stream,
	}, nil
}

func AdaptResponsesRequestToChat(req *ResponsesRequest) (*ChatCompletionRequest, error) {
	if previousResponseID, _ := req.Raw["previous_response_id"].(string); strings.TrimSpace(previousResponseID) != "" {
		return nil, fmt.Errorf("previous_response_id is not supported by this middleware")
	}

	messages, err := responsesInputToMessages(req.Input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Instructions) != "" {
		messages = append([]map[string]any{{
			"role":    "system",
			"content": req.Instructions,
		}}, messages...)
	}

	raw := cloneMap(req.Raw)
	delete(raw, "input")
	delete(raw, "instructions")
	delete(raw, "max_output_tokens")
	delete(raw, "text")

	if maxOutputTokens, ok := req.Raw["max_output_tokens"]; ok {
		raw["max_tokens"] = maxOutputTokens
	}
	if textConfig, ok := req.Raw["text"].(map[string]any); ok {
		if format, ok := textConfig["format"]; ok {
			raw["response_format"] = format
		}
	}

	chatTools, err := responsesToolsToChatTools(req.Tools)
	if err != nil {
		return nil, err
	}

	raw["model"] = req.Model
	raw["messages"] = messages
	if len(chatTools) > 0 {
		raw["tools"] = toAnySlice(chatTools)
	}
	if req.ToolChoice != nil {
		raw["tool_choice"] = adaptResponsesToolChoice(req.ToolChoice)
	}

	return &ChatCompletionRequest{
		Raw:        raw,
		Model:      req.Model,
		Messages:   messages,
		Tools:      chatTools,
		ToolChoice: adaptResponsesToolChoice(req.ToolChoice),
		Stream:     req.Stream,
	}, nil
}

func ResponsesToolsToChatTools(input []ResponseTool) ([]Tool, error) {
	return responsesToolsToChatTools(input)
}

func AdaptResponsesToolChoice(toolChoice any) any {
	return adaptResponsesToolChoice(toolChoice)
}

func ConvertChatCompletionToResponses(payload map[string]any, model string) map[string]any {
	createdAt := time.Now().Unix()
	if raw, ok := payload["created"].(float64); ok {
		createdAt = int64(raw)
	}

	output := make([]map[string]any, 0)
	choices, _ := payload["choices"].([]any)
	if len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if message, ok := choice["message"].(map[string]any); ok {
				if content, _ := message["content"].(string); strings.TrimSpace(content) != "" {
					output = append(output, responseMessageOutputItem(content))
				}

				for _, toolCall := range toolCallSlice(message["tool_calls"]) {
					if item := responseFunctionCallOutputItem(toolCall); item != nil {
						output = append(output, item)
					}
				}
			}
		}
	}

	result := map[string]any{
		"id":          newID("resp_"),
		"object":      "response",
		"created_at":  createdAt,
		"status":      "completed",
		"model":       model,
		"output":      output,
		"output_text": extractResponseOutputText(output),
		"error":       nil,
	}
	if usage, ok := payload["usage"]; ok {
		result["usage"] = usage
	}
	return result
}

func responsesInputToMessages(input any) ([]map[string]any, error) {
	switch value := input.(type) {
	case nil:
		return nil, fmt.Errorf("input is required")
	case string:
		return []map[string]any{{
			"role":    "user",
			"content": value,
		}}, nil
	case []any:
		messages := make([]map[string]any, 0, len(value))
		for _, rawItem := range value {
			item, ok := rawItem.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("unsupported input item type")
			}

			if role, ok := item["role"].(string); ok && strings.TrimSpace(role) != "" {
				messages = append(messages, map[string]any{
					"role":    role,
					"content": convertResponsesContentToChat(item["content"]),
				})
				continue
			}

			switch itemType, _ := item["type"].(string); itemType {
			case "message":
				role, _ := item["role"].(string)
				if strings.TrimSpace(role) == "" {
					role = "user"
				}
				messages = append(messages, map[string]any{
					"role":    role,
					"content": convertResponsesContentToChat(item["content"]),
				})
			case "function_call_output":
				callID, _ := item["call_id"].(string)
				output := stringifyValue(item["output"])
				messages = append(messages, map[string]any{
					"role":         "tool",
					"tool_call_id": callID,
					"content":      output,
				})
			default:
				return nil, fmt.Errorf("unsupported responses input item type: %s", itemType)
			}
		}
		return messages, nil
	default:
		return nil, fmt.Errorf("unsupported input type")
	}
}

func convertResponsesContentToChat(content any) any {
	parts, ok := content.([]any)
	if !ok {
		return content
	}

	result := make([]map[string]any, 0, len(parts))
	for _, rawPart := range parts {
		part, ok := rawPart.(map[string]any)
		if !ok {
			continue
		}
		partType, _ := part["type"].(string)
		switch partType {
		case "input_text", "output_text", "text":
			text, _ := part["text"].(string)
			result = append(result, map[string]any{
				"type": "text",
				"text": text,
			})
		case "input_image":
			imageURL, _ := part["image_url"].(string)
			if imageURL == "" {
				imageURL, _ = part["url"].(string)
			}
			if imageURL != "" {
				result = append(result, map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url": imageURL,
					},
				})
			}
		}
	}
	if len(result) == 0 {
		return content
	}
	return result
}

func responsesToolsToChatTools(input []ResponseTool) ([]Tool, error) {
	if len(input) == 0 {
		return nil, nil
	}

	result := make([]Tool, 0, len(input))
	for _, tool := range input {
		if tool.Type != "function" {
			return nil, fmt.Errorf("unsupported responses tool type: %s", tool.Type)
		}

		function := tool.Function
		if function == nil {
			function = &ToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			}
		}

		result = append(result, Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        function.Name,
				Description: function.Description,
				Parameters:  function.Parameters,
			},
		})
	}
	return result, nil
}

func adaptResponsesToolChoice(toolChoice any) any {
	choiceMap, ok := toolChoice.(map[string]any)
	if !ok {
		return toolChoice
	}

	if choiceType, _ := choiceMap["type"].(string); choiceType == "function" {
		if name, _ := choiceMap["name"].(string); strings.TrimSpace(name) != "" {
			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": name,
				},
			}
		}
	}

	return toolChoice
}

func responseMessageOutputItem(content string) map[string]any {
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

func responseFunctionCallOutputItem(toolCall map[string]any) map[string]any {
	callID, _ := toolCall["id"].(string)
	functionInfo, _ := toolCall["function"].(map[string]any)
	name, _ := functionInfo["name"].(string)
	arguments, _ := functionInfo["arguments"].(string)
	if strings.TrimSpace(callID) == "" || strings.TrimSpace(name) == "" {
		return nil
	}

	return map[string]any{
		"id":        newID("fc_"),
		"type":      "function_call",
		"status":    "completed",
		"call_id":   callID,
		"name":      name,
		"arguments": arguments,
	}
}

func extractResponseOutputText(output []map[string]any) string {
	var parts []string
	for _, item := range output {
		if itemType, _ := item["type"].(string); itemType != "message" {
			continue
		}
		content, _ := item["content"].([]map[string]any)
		for _, part := range content {
			if partType, _ := part["type"].(string); partType == "output_text" {
				if text, _ := part["text"].(string); strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

func toAnySlice[T any](input []T) []any {
	result := make([]any, 0, len(input))
	for _, item := range input {
		result = append(result, item)
	}
	return result
}

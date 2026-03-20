package protocol

import "testing"

func TestAdaptResponsesRequestToChat(t *testing.T) {
	description := "search docs"
	req := &ResponsesRequest{
		Raw: map[string]any{
			"model":             "gpt-4.1",
			"input":             "hello",
			"max_output_tokens": float64(128),
			"text": map[string]any{
				"format": map[string]any{
					"type": "json_object",
				},
			},
		},
		Model: "gpt-4.1",
		Input: "hello",
		Tools: []ResponseTool{
			{
				Type:        "function",
				Name:        "search",
				Description: &description,
				Parameters: map[string]any{
					"type": "object",
				},
			},
		},
	}

	adapted, err := AdaptResponsesRequestToChat(req)
	if err != nil {
		t.Fatalf("expected adaptation to succeed: %v", err)
	}
	if adapted.Raw["max_tokens"] != float64(128) {
		t.Fatalf("expected max_output_tokens to map to max_tokens, got %#v", adapted.Raw["max_tokens"])
	}
	if _, ok := adapted.Raw["response_format"]; !ok {
		t.Fatalf("expected text.format to map to response_format")
	}
	if len(adapted.Messages) != 1 || adapted.Messages[0]["role"] != "user" {
		t.Fatalf("expected single user message, got %#v", adapted.Messages)
	}
	if len(adapted.Tools) != 1 || adapted.Tools[0].Function.Name != "search" {
		t.Fatalf("expected function tool adaptation, got %#v", adapted.Tools)
	}
}

func TestConvertChatCompletionToResponses(t *testing.T) {
	payload := map[string]any{
		"created": float64(123),
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"content": "done",
					"tool_calls": []any{
						map[string]any{
							"id": "call_1",
							"function": map[string]any{
								"name":      "search",
								"arguments": `{"q":"go"}`,
							},
						},
					},
				},
			},
		},
	}

	response := ConvertChatCompletionToResponses(payload, "gpt-4.1")
	output, _ := response["output"].([]map[string]any)
	if len(output) != 2 {
		t.Fatalf("expected 2 output items, got %#v", output)
	}
	if response["output_text"] != "done" {
		t.Fatalf("expected output_text helper, got %#v", response["output_text"])
	}
}

package protocol

import "testing"

func TestConvertChatCompletionToAnthropicRepairsMalformedToolArguments(t *testing.T) {
	payload := map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"tool_calls": []any{
						map[string]any{
							"id": "call_1",
							"function": map[string]any{
								"name":      "search",
								"arguments": `{query:'weather',}`,
							},
						},
					},
				},
			},
		},
	}

	response := ConvertChatCompletionToAnthropic(payload, "claude-test")
	content, _ := response["content"].([]map[string]any)
	if len(content) != 1 {
		t.Fatalf("expected one content block, got %#v", content)
	}
	if content[0]["type"] != "tool_use" {
		t.Fatalf("expected tool_use block, got %#v", content[0]["type"])
	}
	input, _ := content[0]["input"].(map[string]any)
	if input["query"] != "weather" {
		t.Fatalf("expected repaired tool input, got %#v", input)
	}
}

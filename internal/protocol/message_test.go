package protocol

import (
	"strings"
	"testing"
	"time"

	"x-tool/internal/config"
	"x-tool/internal/toolcall"
)

func TestFormatAssistantToolCallsForAIWrapsNonObjectJSONArguments(t *testing.T) {
	toolCalls := []any{
		map[string]any{
			"function": map[string]any{
				"name":      "search",
				"arguments": `["a","b"]`,
			},
		},
	}

	result := FormatAssistantToolCallsForAI(toolCalls, "<Function_Test_Start>", config.SoftToolProtocolXML)
	if !strings.Contains(result, `<parameter name="content">[&#34;a&#34;,&#34;b&#34;]</parameter>`) {
		t.Fatalf("expected array arguments to be wrapped as content, got %s", result)
	}
}

func TestPreprocessMessagesPreservesNonStringAssistantContent(t *testing.T) {
	store := toolcall.NewManager(8, time.Minute, time.Minute)
	messages := []map[string]any{
		{
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "text", "text": "hello"},
			},
			"tool_calls": []any{
				map[string]any{
					"function": map[string]any{
						"name":      "search",
						"arguments": `{"q":"go"}`,
					},
				},
			},
		},
	}

	processed := PreprocessMessages(messages, store, "<Function_Test_Start>", config.SoftToolProtocolXML, true)
	content, _ := processed[0]["content"].(string)
	if !strings.Contains(content, "map[text:hello type:text]") {
		t.Fatalf("expected original non-string content to be preserved via stringification, got %q", content)
	}
	if !strings.Contains(content, `<invoke name="search">`) {
		t.Fatalf("expected tool call content to be appended, got %q", content)
	}
}

func TestFormatAssistantToolCallsForAISentinelJSONWrapsSingleCall(t *testing.T) {
	toolCalls := []any{
		map[string]any{
			"function": map[string]any{
				"name":      "search",
				"arguments": `{"q":"go"}`,
			},
		},
	}

	result := FormatAssistantToolCallsForAI(toolCalls, "<Function_Test_Start>", config.SoftToolProtocolSentinelJSON)
	if !strings.Contains(result, "<TOOL_CALL>") || !strings.Contains(result, `"name":"search"`) {
		t.Fatalf("expected sentinel JSON tool call block, got %s", result)
	}
	if !strings.Contains(result, `"arguments":{"q":"go"}`) {
		t.Fatalf("expected arguments object in sentinel JSON block, got %s", result)
	}
}

func TestFormatAssistantToolCallsForAIWrapsXMLCallsInFunctionCallsBlock(t *testing.T) {
	toolCalls := []any{
		map[string]any{
			"function": map[string]any{
				"name":      "search",
				"arguments": `{"q":"go"}`,
			},
		},
		map[string]any{
			"function": map[string]any{
				"name":      "write_file",
				"arguments": `{"path":"/tmp/out.txt"}`,
			},
		},
	}

	result := FormatAssistantToolCallsForAI(toolCalls, "<Function_Test_Start>", config.SoftToolProtocolXML)
	if !strings.Contains(result, "<function_calls>") || !strings.Contains(result, "</function_calls>") {
		t.Fatalf("expected XML tool calls to be wrapped in function_calls, got %s", result)
	}
	if !strings.Contains(result, `<invoke name="search">`) || !strings.Contains(result, `<invoke name="write_file">`) {
		t.Fatalf("expected both invoke blocks in XML wrapper, got %s", result)
	}
}

func TestFormatAssistantToolCallsForAIMarkdownBlockFlattensStructuredArguments(t *testing.T) {
	toolCalls := []any{
		map[string]any{
			"function": map[string]any{
				"name":      "search",
				"arguments": `{"headers":{"authorization":"Bearer token"},"query":"weather","tags":["news","local"]}`,
			},
		},
		map[string]any{
			"function": map[string]any{
				"name":      "write_file",
				"arguments": `{"content":"line 1\nline 2","path":"/tmp/out.txt"}`,
			},
		},
	}

	result := FormatAssistantToolCallsForAI(toolCalls, "<Function_Test_Start>", config.SoftToolProtocolMarkdownBlock)
	if !strings.Contains(result, "<Function_Test_Start>\n```toolcalls") {
		t.Fatalf("expected markdown block wrapper, got %s", result)
	}
	if !strings.Contains(result, "call search") || !strings.Contains(result, "call write_file") {
		t.Fatalf("expected call lines in markdown block, got %s", result)
	}
	if !strings.Contains(result, "arg_headers.authorization: Bearer token") {
		t.Fatalf("expected nested map to flatten into dot path, got %s", result)
	}
	if !strings.Contains(result, "arg_tags[]: news") || !strings.Contains(result, "arg_tags[]: local") {
		t.Fatalf("expected arrays to become repeated [] args, got %s", result)
	}
	if !strings.Contains(result, "arg_content:\n  line 1\n  line 2") {
		t.Fatalf("expected multiline strings to use bounded block continuations, got %s", result)
	}
}

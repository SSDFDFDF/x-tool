package proxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"x-tool/internal/config"
	"x-tool/internal/protocol"
)

func taskUpdateTestSoftTool(protocolName string) *softToolCallSettings {
	return &softToolCallSettings{
		Protocol: protocolName,
		Trigger:  "<Function_Test_Start>",
		Tools: []protocol.Tool{{
			Type: "function",
			Function: protocol.ToolFunction{
				Name: "TaskUpdate",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"taskId":  map[string]any{"type": "string"},
						"attempt": map[string]any{"type": "integer"},
						"ratio":   map[string]any{"type": "number"},
						"done":    map[string]any{"type": "boolean"},
						"meta": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"ownerId": map[string]any{"type": "string"},
							},
						},
						"labels": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"steps": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "integer"},
						},
					},
					"required": []any{"taskId", "attempt", "ratio", "done"},
				},
			},
		}},
	}
}

func TestParseSoftToolCallsNormalizesArgumentsBySchemaAcrossProtocols(t *testing.T) {
	const expected = `{"attempt":2,"done":true,"labels":["1","ready"],"meta":{"ownerId":"2"},"ratio":1.5,"steps":[3,4],"taskId":"1"}`

	cases := []struct {
		name     string
		protocol string
		content  string
	}{
		{
			name:     "xml",
			protocol: config.SoftToolProtocolXML,
			content: "<Function_Test_Start>\n<function_calls>\n" +
				"  <invoke name=\"TaskUpdate\">\n" +
				"    <parameter name=\"taskId\">1</parameter>\n" +
				"    <parameter name=\"attempt\">\"2\"</parameter>\n" +
				"    <parameter name=\"ratio\">\"1.5\"</parameter>\n" +
				"    <parameter name=\"done\">\"true\"</parameter>\n" +
				"    <parameter name=\"meta\">{\"ownerId\":2}</parameter>\n" +
				"    <parameter name=\"labels\">[1,\"ready\"]</parameter>\n" +
				"    <parameter name=\"steps\">[\"3\",4]</parameter>\n" +
				"  </invoke>\n" +
				"</function_calls>",
		},
		{
			name:     "sentinel_json",
			protocol: config.SoftToolProtocolSentinelJSON,
			content: "<Function_Test_Start>\n<TOOL_CALL>\n" +
				"{\"name\":\"TaskUpdate\",\"arguments\":{\"taskId\":1,\"attempt\":\"2\",\"ratio\":\"1.5\",\"done\":\"true\",\"meta\":{\"ownerId\":2},\"labels\":[1,\"ready\"],\"steps\":[\"3\",4]}}\n" +
				"</TOOL_CALL>",
		},
		{
			name:     "markdown_block",
			protocol: config.SoftToolProtocolMarkdownBlock,
			content: "<Function_Test_Start>\n```mbtoolcalls\n" +
				"mbcall: TaskUpdate\n" +
				"mbarg[taskId]: 1\n" +
				"mbarg[attempt]: \"2\"\n" +
				"mbarg[ratio]: \"1.5\"\n" +
				"mbarg[done]: \"true\"\n" +
				"mbarg[meta@json]: {\"ownerId\":2}\n" +
				"mbarg[labels@json]: [1,\"ready\"]\n" +
				"mbarg[steps@json]: [\"3\",4]\n" +
				"```",
		},
	}

	app := &App{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := app.parseSoftToolCalls(tc.content, taskUpdateTestSoftTool(tc.protocol))
			if err != nil {
				t.Fatalf("expected parse to succeed, got %v", err)
			}
			if len(parsed) != 1 {
				t.Fatalf("expected 1 tool call, got %#v", parsed)
			}
			if parsed[0].Name != "TaskUpdate" {
				t.Fatalf("expected TaskUpdate call, got %#v", parsed[0])
			}
			if got := mustJSON(parsed[0].Args); got != expected {
				t.Fatalf("expected normalized args %s, got %s", expected, got)
			}
		})
	}
}

func TestResponsesRouteNormalizesXMLArgumentsBySchema(t *testing.T) {
	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_task_update",
			"object":  "chat.completion",
			"created": 123,
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role": "assistant",
					"content": "prefix\n<Function_Test_Start>\n<function_calls>\n" +
						"  <invoke name=\"TaskUpdate\">\n" +
						"    <parameter name=\"taskId\">1</parameter>\n" +
						"    <parameter name=\"attempt\">\"2\"</parameter>\n" +
						"    <parameter name=\"done\">\"true\"</parameter>\n" +
						"  </invoke>\n" +
						"</function_calls>",
				},
				"finish_reason": "stop",
			}},
		})
	}))

	cfg := testConfig()
	cfg.UpstreamServices[0].BaseURL = upstream.URL + "/v1"

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.trigger = "<Function_Test_Start>"

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o","input":"ping","tools":[{"type":"function","name":"TaskUpdate","parameters":{"type":"object","properties":{"taskId":{"type":"string"},"attempt":{"type":"integer"},"done":{"type":"boolean"}},"required":["taskId","attempt","done"]}}],"tool_choice":"required"}`))
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	output, _ := body["output"].([]any)
	if len(output) != 2 {
		t.Fatalf("expected message + function_call output items, got %#v", output)
	}
	second, _ := output[1].(map[string]any)
	if got := second["arguments"]; got != `{"attempt":2,"done":true,"taskId":"1"}` {
		t.Fatalf("expected normalized arguments, got %#v", got)
	}
}

package proxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicMessagesRouteAdaptsToChat(t *testing.T) {
	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("expected chat completions path, got %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_1",
			"object":  "chat.completion",
			"created": 123,
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "hello from anthropic adapter",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
			},
		})
	}))

	cfg := testConfig()
	cfg.UpstreamServices[0].BaseURL = upstream.URL + "/v1"

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"gpt-4o","max_tokens":128,"messages":[{"role":"user","content":"ping"}]}`))
	req.Header.Set("x-api-key", "client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["type"] != "message" {
		t.Fatalf("expected anthropic message type, got %#v", body["type"])
	}
}

func TestAnthropicMessagesWithNoUpstreamConfiguredReturns503(t *testing.T) {
	cfg := testConfig()
	cfg.UpstreamServices = nil

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"gpt-4o","max_tokens":128,"messages":[{"role":"user","content":"ping"}]}`))
	req.Header.Set("x-api-key", "client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	errorBody, _ := body["error"].(map[string]any)
	if errorBody["code"] != "upstream_not_configured" {
		t.Fatalf("expected upstream_not_configured code, got %#v", errorBody["code"])
	}
}

func TestAnthropicMessagesRouteReturnsToolUseContent(t *testing.T) {
	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_tool",
			"object":  "chat.completion",
			"created": 123,
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "<Function_Test_Start>\n<invoke name=\"search\"><parameter name=\"query\">weather</parameter></invoke>",
				},
				"finish_reason": "stop",
			}},
		})
	}))

	cfg := testConfig()
	cfg.UpstreamServices[0].BaseURL = upstream.URL + "/v1"

	logger, logStore := testLogger(t)
	app, err := NewApp(cfg, nil, nil, logger, logStore, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.trigger = "<Function_Test_Start>"

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"gpt-4o","max_tokens":128,"messages":[{"role":"user","content":"ping"}],"tools":[{"name":"search","input_schema":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}],"tool_choice":{"type":"any"}}`))
	req.Header.Set("x-api-key", "client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	content, _ := body["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected single tool_use block, got %#v", content)
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "tool_use" {
		t.Fatalf("expected tool_use block, got %#v", block["type"])
	}
}

func TestResponsesStreamRouteProducesSemanticEvents(t *testing.T) {
	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"po\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ng\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))

	cfg := testConfig()
	cfg.UpstreamServices[0].BaseURL = upstream.URL + "/v1"

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o","input":"ping","stream":true}`))
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"response.output_text.delta"`) {
		t.Fatalf("expected output_text delta event, got %s", body)
	}
	if !strings.Contains(body, `"type":"response.completed"`) {
		t.Fatalf("expected completed event, got %s", body)
	}
}

func TestResponsesStreamRouteLogsEachClientSSEEvent(t *testing.T) {
	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"po\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ng\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))

	cfg := testConfig()
	cfg.UpstreamServices[0].BaseURL = upstream.URL + "/v1"

	logger, logStore := testLogger(t)
	app, err := NewApp(cfg, nil, nil, logger, logStore, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o","input":"ping","stream":true}`))
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	rawLogs := logStore.Raw(200)
	if !strings.Contains(rawLogs, "protocol=responses") || !strings.Contains(rawLogs, "response.created") {
		t.Fatalf("expected response SSE events to be logged, got %s", rawLogs)
	}
	if !strings.Contains(rawLogs, "response.completed") {
		t.Fatalf("expected response.completed SSE event log, got %s", rawLogs)
	}
}

func TestAnthropicStreamRouteProducesEvents(t *testing.T) {
	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))

	cfg := testConfig()
	cfg.UpstreamServices[0].BaseURL = upstream.URL + "/v1"

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"gpt-4o","max_tokens":128,"messages":[{"role":"user","content":"ping"}],"stream":true}`))
	req.Header.Set("x-api-key", "client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: message_start") {
		t.Fatalf("expected message_start event, got %s", body)
	}
	if !strings.Contains(body, "event: content_block_delta") {
		t.Fatalf("expected content_block_delta event, got %s", body)
	}
	if !strings.Contains(body, "event: message_stop") {
		t.Fatalf("expected message_stop event, got %s", body)
	}
}

func TestAnthropicStreamWithThinkingProducesThinkingBlock(t *testing.T) {
	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Simulate upstream returning reasoning_content (OpenAI style)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"Let me think...\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\" The answer is 42.\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"The answer is 42.\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))

	cfg := testConfig()
	cfg.UpstreamServices[0].BaseURL = upstream.URL + "/v1"

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"gpt-4o","max_tokens":128,"messages":[{"role":"user","content":"ping"}],"stream":true}`))
	req.Header.Set("x-api-key", "client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	body := rec.Body.String()
	// Should contain thinking block start
	if !strings.Contains(body, `"type":"thinking"`) {
		t.Fatalf("expected thinking block type, got %s", body)
	}
	// Should contain thinking_delta
	if !strings.Contains(body, `"type":"thinking_delta"`) {
		t.Fatalf("expected thinking_delta type, got %s", body)
	}
	// Should contain the thinking content
	if !strings.Contains(body, "Let me think...") {
		t.Fatalf("expected thinking content, got %s", body)
	}
	// Should also contain text block
	if !strings.Contains(body, `"type":"text"`) {
		t.Fatalf("expected text block type, got %s", body)
	}
}

func TestChatCompletionsStreamWithThinkingProducesReasoningContent(t *testing.T) {
	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Simulate upstream returning reasoning_content
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking...\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"answer\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))

	cfg := testConfig()
	cfg.UpstreamServices[0].BaseURL = upstream.URL + "/v1"

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"ping"}],"stream":true}`))
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	body := rec.Body.String()
	// Should contain reasoning_content in response
	if !strings.Contains(body, `"reasoning_content"`) {
		t.Fatalf("expected reasoning_content field, got %s", body)
	}
	if !strings.Contains(body, "thinking...") {
		t.Fatalf("expected thinking content, got %s", body)
	}
	// Should also contain content
	if !strings.Contains(body, `"content":"answer"`) {
		t.Fatalf("expected content field, got %s", body)
	}
}

func TestResponsesStreamWithThinkingProducesReasoningBlock(t *testing.T) {
	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Simulate upstream returning reasoning_content
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"reasoning...\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"text\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))

	cfg := testConfig()
	cfg.UpstreamServices[0].BaseURL = upstream.URL + "/v1"

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o","input":"ping","stream":true}`))
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	body := rec.Body.String()
	// Should contain reasoning type
	if !strings.Contains(body, `"type":"reasoning"`) {
		t.Fatalf("expected reasoning type, got %s", body)
	}
	// Should contain reasoning delta
	if !strings.Contains(body, `"type":"response.reasoning_summary_text.delta"`) {
		t.Fatalf("expected reasoning delta event, got %s", body)
	}
	if !strings.Contains(body, "reasoning...") {
		t.Fatalf("expected reasoning content, got %s", body)
	}
}

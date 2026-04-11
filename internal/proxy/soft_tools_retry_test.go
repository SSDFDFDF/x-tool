package proxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEffectiveSoftToolRetryAttempts(t *testing.T) {
	cases := []struct {
		name        string
		featureVal  int
		upstreamVal int
		expected    int
	}{
		{"both_zero", 0, 0, 0},
		{"global_default_applies", 3, 0, 3},
		{"upstream_positive_overrides", 4, 1, 1},
		{"upstream_higher_overrides", 1, 4, 4},
		{"upstream_negative_falls_back_to_feature", 2, -1, 2},
		{"feature_negative", -1, 0, 0},
		{"both_negative", -5, -3, 0},
		{"feature_over_5", 10, 0, 5},
		{"upstream_over_5", 2, 8, 5},
		{"both_over_5", 7, 9, 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Features.SoftToolRetryAttempts = tc.featureVal
			cfg.UpstreamServices[0].SoftToolRetryAttempts = tc.upstreamVal

			app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
			if err != nil {
				t.Fatalf("failed to create app: %v", err)
			}

			got := app.effectiveSoftToolRetryAttempts(cfg.UpstreamServices[0])
			if got != tc.expected {
				t.Errorf("expected %d, got %d", tc.expected, got)
			}
		})
	}
}

func TestProxyJSONRetriesOnSoftToolTransformFailure(t *testing.T) {
	var callCount int32

	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		if count <= 2 {
			// First two attempts: return malformed tool call (trigger but invalid format)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl-retry",
				"object":  "chat.completion",
				"created": 123,
				"choices": []map[string]any{{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "<Function_Test_Start> this is not valid xml",
					},
					"finish_reason": "stop",
				}},
			})
		} else {
			// Third attempt: return valid tool call
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl-retry",
				"object":  "chat.completion",
				"created": 123,
				"choices": []map[string]any{{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "<Function_Test_Start>\n<function_calls>\n  <invoke name=\"search\"><parameter name=\"query\">test</parameter></invoke>\n</function_calls>",
					},
					"finish_reason": "stop",
				}},
			})
		}
	}))

	cfg := testConfig()
	cfg.UpstreamServices[0].BaseURL = upstream.URL + "/v1"
	cfg.UpstreamServices[0].SoftToolRetryAttempts = 5
	cfg.Features.SoftToolRetryAttempts = 3

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.trigger = "<Function_Test_Start>"

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"search","parameters":{"type":"object","properties":{"query":{"type":"string"}}}}}]}`))
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

	choices, _ := body["choices"].([]any)
	if len(choices) == 0 {
		t.Fatalf("expected choices, got %v", body)
	}
	first, _ := choices[0].(map[string]any)
	msg, _ := first["message"].(map[string]any)
	toolCalls, _ := msg["tool_calls"].([]any)
	if len(toolCalls) == 0 {
		t.Fatalf("expected tool_calls after retry, got %v", msg)
	}

	// Verify we made 3 attempts (2 failures + 1 success)
	if count := atomic.LoadInt32(&callCount); count != 3 {
		t.Errorf("expected 3 upstream calls (2 failures + 1 success), got %d", count)
	}
}

func TestProxyJSONNoRetryWithoutSoftTool(t *testing.T) {
	var callCount int32

	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-no-retry",
			"object":  "chat.completion",
			"created": 123,
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "Hello!",
				},
				"finish_reason": "stop",
			}},
		})
	}))

	cfg := testConfig()
	cfg.UpstreamServices[0].BaseURL = upstream.URL + "/v1"
	cfg.Features.EnableFunctionCalling = false // no soft tool = no retry

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if count := atomic.LoadInt32(&callCount); count != 1 {
		t.Errorf("expected exactly 1 upstream call (no soft tool = no retry), got %d", count)
	}
}

func TestProxyJSONRetryExhausted(t *testing.T) {
	var callCount int32

	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		// Always return malformed tool call
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-exhaust",
			"object":  "chat.completion",
			"created": 123,
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "<Function_Test_Start> invalid tool call format",
				},
				"finish_reason": "stop",
			}},
		})
	}))

	cfg := testConfig()
	cfg.UpstreamServices[0].BaseURL = upstream.URL + "/v1"
	cfg.UpstreamServices[0].SoftToolRetryAttempts = 2 // max 2 retries = 3 total attempts

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.trigger = "<Function_Test_Start>"

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"search","parameters":{"type":"object","properties":{"query":{"type":"string"}}}}}]}`))
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", rec.Code, rec.Body.String())
	}

	// After exhausting retries, it should still return the response (with original content)
	// total calls = 1 (initial) + 2 (retries) = 3
	if count := atomic.LoadInt32(&callCount); count != 3 {
		t.Errorf("expected 3 upstream calls (initial + 2 retries), got %d", count)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	choices, _ := body["choices"].([]any)
	first, _ := choices[0].(map[string]any)
	msg, _ := first["message"].(map[string]any)
	content, _ := msg["content"].(string)
	// After retries exhausted, it should still return the last response as-is
	if content == "" {
		t.Errorf("expected non-empty content after retry exhaustion, got %v", msg)
	}
}

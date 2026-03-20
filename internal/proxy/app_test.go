package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"x-tool/internal/admin"
	"x-tool/internal/config"
	"x-tool/internal/logging"
	"x-tool/internal/stats"
	"x-tool/internal/storage"
)

func TestModelsMissingAuthorizationReturnsValidationStyle422(t *testing.T) {
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	errorBody, _ := body["error"].(map[string]any)
	if errorBody["code"] != "invalid_request" {
		t.Fatalf("expected invalid_request code, got %#v", errorBody["code"])
	}
}

func TestModelsWithNoUpstreamConfiguredReturns503(t *testing.T) {
	cfg := testConfig()
	cfg.UpstreamServices = nil

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	errorBody, _ := body["error"].(map[string]any)
	if errorBody["code"] != "upstream_not_configured" {
		t.Fatalf("expected upstream_not_configured code, got %#v", errorBody["code"])
	}
}

func TestModelsAreFilteredByClientKeyBindings(t *testing.T) {
	cfg := testConfig()
	cfg.UpstreamServices = []config.UpstreamService{
		{
			Name:       "openai-a",
			BaseURL:    "https://example.com/a",
			APIKey:     "upstream-key-a",
			IsDefault:  true,
			Models:     []string{"gpt-4o", "chat-fast:gpt-4.1-mini"},
			ClientKeys: []string{"client-a"},
		},
		{
			Name:       "openai-b",
			BaseURL:    "https://example.com/b",
			APIKey:     "upstream-key-b",
			Models:     []string{"o3-mini"},
			ClientKeys: []string{"client-b"},
		},
	}

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer client-a")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode models response: %v", err)
	}

	got := make([]string, 0, len(body.Data))
	for _, item := range body.Data {
		got = append(got, item.ID)
	}
	if strings.Join(got, ",") != "chat-fast,gpt-4o" {
		t.Fatalf("expected models [chat-fast gpt-4o], got %#v", got)
	}
}

func TestChatCompletionsRouteUsesBoundUpstreamForClientKey(t *testing.T) {
	upstreamA := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_a",
			"object":  "chat.completion",
			"created": 123,
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "from-upstream-a",
				},
				"finish_reason": "stop",
			}},
		})
	}))
	upstreamB := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_b",
			"object":  "chat.completion",
			"created": 123,
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "from-upstream-b",
				},
				"finish_reason": "stop",
			}},
		})
	}))

	cfg := testConfig()
	cfg.UpstreamServices = []config.UpstreamService{
		{
			Name:       "openai-a",
			BaseURL:    upstreamA.URL + "/v1",
			APIKey:     "upstream-key-a",
			IsDefault:  true,
			Models:     []string{"gpt-4o"},
			ClientKeys: []string{"client-a"},
		},
		{
			Name:       "openai-b",
			BaseURL:    upstreamB.URL + "/v1",
			APIKey:     "upstream-key-b",
			Models:     []string{"gpt-4o"},
			ClientKeys: []string{"client-b"},
		},
	}

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"ping"}]}`))
	req.Header.Set("Authorization", "Bearer client-b")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "from-upstream-b") {
		t.Fatalf("expected request to hit upstream-b, got %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "from-upstream-a") {
		t.Fatalf("expected upstream-a not to be used, got %s", rec.Body.String())
	}
}

func TestAdminOverviewRequiresSession(t *testing.T) {
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	ctx := newAdminTestContextForApp(t, app, nil, "secret", "")
	req := httptest.NewRequest(http.MethodGet, "/admin/api/overview", nil)
	rec := httptest.NewRecorder()

	ctx.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if header := rec.Header().Get("WWW-Authenticate"); header != "" {
		t.Fatalf("expected no basic auth challenge, got %q", header)
	}
}

func TestAdminLoginAllowsOverviewAccess(t *testing.T) {
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	ctx := newAdminTestContextForApp(t, app, nil, "secret", "")
	sessionCookie := loginAdmin(t, ctx.handler, "secret")

	if !sessionCookie.HttpOnly {
		t.Fatalf("expected admin session cookie to be HttpOnly")
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/api/overview", nil)
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()

	ctx.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected ok status, got %#v", body["status"])
	}
	models, _ := body["models"].([]any)
	if len(models) == 0 {
		t.Fatalf("expected visible models in admin overview")
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("expected utf-8 json content type, got %q", contentType)
	}
	statsBody, _ := body["stats"].(map[string]any)
	if statsBody["total_requests"] == nil {
		t.Fatalf("expected stats payload in admin overview")
	}
}

func TestRuntimeStatsAreTrackedAndPersisted(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "stats-test.db")
	db, err := storage.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate sqlite database: %v", err)
	}

	store := stats.NewStore(db)
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.SetStatsStore(store)
	defer func() {
		_ = app.Close()
	}()

	modelsReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	modelsReq.Header.Set("Authorization", "Bearer client-key")
	modelsRec := httptest.NewRecorder()
	app.Routes(nil).ServeHTTP(modelsRec, modelsReq)
	if modelsRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", modelsRec.Code)
	}

	invalidReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	invalidRec := httptest.NewRecorder()
	app.Routes(nil).ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", invalidRec.Code)
	}

	app.flushStatsNow()

	snapshot := app.GetRuntimeStats()
	if snapshot.TotalRequests != 2 {
		t.Fatalf("expected 2 total requests, got %d", snapshot.TotalRequests)
	}
	if snapshot.Status2xx != 1 || snapshot.Status4xx != 1 {
		t.Fatalf("expected 2xx=1 and 4xx=1, got %#v", snapshot)
	}

	reloaded, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create reloaded app: %v", err)
	}
	reloaded.SetStatsStore(store)
	defer func() {
		_ = reloaded.Close()
	}()

	persisted := reloaded.GetRuntimeStats()
	if persisted.TotalRequests != 2 {
		t.Fatalf("expected persisted total requests to be 2, got %d", persisted.TotalRequests)
	}
	if persisted.Status2xx != 1 || persisted.Status4xx != 1 {
		t.Fatalf("expected persisted 2xx=1 and 4xx=1, got %#v", persisted)
	}
}

func TestAdminEnvPasswordCanLogin(t *testing.T) {
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	ctx := newAdminTestContextForApp(t, app, nil, "env-secret", "")
	sessionCookie := loginAdmin(t, ctx.handler, "env-secret")
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("expected admin login to issue a session cookie")
	}
}

func TestAdminDBHashOverridesEnvPassword(t *testing.T) {
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	ctx := newAdminTestContextForApp(t, app, nil, "env-secret", "db-secret")

	loginReqBody, err := json.Marshal(map[string]string{"password": "env-secret"})
	if err != nil {
		t.Fatalf("marshal login body: %v", err)
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/admin/api/auth/login", bytes.NewReader(loginReqBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	ctx.handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected env password to be rejected when DB hash exists, got %d", loginRec.Code)
	}

	sessionCookie := loginAdmin(t, ctx.handler, "db-secret")
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("expected DB password login to succeed")
	}
}

func TestAdminLogoutInvalidatesSession(t *testing.T) {
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	ctx := newAdminTestContextForApp(t, app, nil, "secret", "")
	sessionCookie := loginAdmin(t, ctx.handler, "secret")

	logoutReq := httptest.NewRequest(http.MethodPost, "/admin/api/auth/logout", nil)
	logoutReq.AddCookie(sessionCookie)
	logoutRec := httptest.NewRecorder()
	ctx.handler.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from logout, got %d", logoutRec.Code)
	}

	overviewReq := httptest.NewRequest(http.MethodGet, "/admin/api/overview", nil)
	overviewReq.AddCookie(sessionCookie)
	overviewRec := httptest.NewRecorder()
	ctx.handler.ServeHTTP(overviewRec, overviewReq)
	if overviewRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected old session to be invalid after logout, got %d", overviewRec.Code)
	}
}

func TestAdminPasswordChangeInvalidatesOldSessionAndEnablesNewPassword(t *testing.T) {
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	ctx := newAdminTestContextForApp(t, app, nil, "old-secret", "")
	sessionCookie := loginAdmin(t, ctx.handler, "old-secret")

	changeBody, err := json.Marshal(map[string]string{
		"current_password": "old-secret",
		"new_password":     "new-secret",
	})
	if err != nil {
		t.Fatalf("marshal password update body: %v", err)
	}
	changeReq := httptest.NewRequest(http.MethodPost, "/admin/api/auth/password", bytes.NewReader(changeBody))
	changeReq.Header.Set("Content-Type", "application/json")
	changeReq.AddCookie(sessionCookie)
	changeRec := httptest.NewRecorder()
	ctx.handler.ServeHTTP(changeRec, changeReq)
	if changeRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from password change, got %d body=%s", changeRec.Code, changeRec.Body.String())
	}

	hash, err := ctx.store.GetAdminPasswordHash()
	if err != nil {
		t.Fatalf("get stored admin password hash: %v", err)
	}
	if strings.TrimSpace(hash) == "" {
		t.Fatalf("expected admin password hash to be persisted in DB")
	}

	overviewReq := httptest.NewRequest(http.MethodGet, "/admin/api/overview", nil)
	overviewReq.AddCookie(sessionCookie)
	overviewRec := httptest.NewRecorder()
	ctx.handler.ServeHTTP(overviewRec, overviewReq)
	if overviewRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected old session to be invalid after password change, got %d", overviewRec.Code)
	}

	oldLoginBody, err := json.Marshal(map[string]string{"password": "old-secret"})
	if err != nil {
		t.Fatalf("marshal old login body: %v", err)
	}
	oldLoginReq := httptest.NewRequest(http.MethodPost, "/admin/api/auth/login", bytes.NewReader(oldLoginBody))
	oldLoginReq.Header.Set("Content-Type", "application/json")
	oldLoginRec := httptest.NewRecorder()
	ctx.handler.ServeHTTP(oldLoginRec, oldLoginReq)
	if oldLoginRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected old password login to fail after change, got %d", oldLoginRec.Code)
	}

	newSessionCookie := loginAdmin(t, ctx.handler, "new-secret")
	statusReq := httptest.NewRequest(http.MethodGet, "/admin/api/auth/status", nil)
	statusReq.AddCookie(newSessionCookie)
	statusRec := httptest.NewRecorder()
	ctx.handler.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from auth status after re-login, got %d", statusRec.Code)
	}
}

func TestChatCompletionsWithNoUpstreamConfiguredReturns503(t *testing.T) {
	cfg := testConfig()
	cfg.UpstreamServices = nil

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"ping"}]}`))
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	errorBody, _ := body["error"].(map[string]any)
	if errorBody["code"] != "upstream_not_configured" {
		t.Fatalf("expected upstream_not_configured code, got %#v", errorBody["code"])
	}
}

func TestResponsesWithNoUpstreamConfiguredReturns503(t *testing.T) {
	cfg := testConfig()
	cfg.UpstreamServices = nil

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o","input":"ping"}`))
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	errorBody, _ := body["error"].(map[string]any)
	if errorBody["code"] != "upstream_not_configured" {
		t.Fatalf("expected upstream_not_configured code, got %#v", errorBody["code"])
	}
}

func TestResponsesRouteAdaptsToChatCompletions(t *testing.T) {
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
					"content": "pong",
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

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o","input":"ping"}`))
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
	if body["object"] != "response" {
		t.Fatalf("expected response object, got %#v", body["object"])
	}
	if body["output_text"] != "pong" {
		t.Fatalf("expected output_text pong, got %#v", body["output_text"])
	}
}

func TestResponsesRouteUsesResponsesProtocolUpstream(t *testing.T) {
	var openAIHits atomic.Int32
	var responsesHits atomic.Int32

	openAIUpstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		openAIHits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object":      "response",
			"output_text": "from-openai-compat",
		})
	}))
	responsesUpstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responsesHits.Add(1)
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("expected responses path, got %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object":      "response",
			"output_text": "from-responses-upstream",
		})
	}))

	cfg := testConfig()
	cfg.UpstreamServices = []config.UpstreamService{
		{
			Name:             "openai-compat",
			BaseURL:          openAIUpstream.URL + "/v1",
			APIKey:           "openai-key",
			IsDefault:        true,
			Models:           []string{"gpt-4o"},
			ClientKeys:       []string{"client-key"},
			UpstreamProtocol: config.UpstreamProtocolOpenAICompat,
		},
		{
			Name:             "responses",
			BaseURL:          responsesUpstream.URL + "/v1",
			APIKey:           "responses-key",
			Models:           []string{"gpt-4o"},
			ClientKeys:       []string{"client-key"},
			UpstreamProtocol: config.UpstreamProtocolResponses,
		},
	}

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o","input":"ping"}`))
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", rec.Code, rec.Body.String())
	}
	if openAIHits.Load() != 0 {
		t.Fatalf("expected openai_compat upstream not to be used, got %d hits", openAIHits.Load())
	}
	if responsesHits.Load() != 1 {
		t.Fatalf("expected responses upstream to be used once, got %d hits", responsesHits.Load())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body["output_text"] != "from-responses-upstream" {
		t.Fatalf("expected responses upstream payload, got %#v", body["output_text"])
	}
}

func TestResponsesRouteReturnsFunctionCallOutput(t *testing.T) {
	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_tool",
			"object":  "chat.completion",
			"created": 123,
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "prefix\n<Function_Test_Start>\n<invoke name=\"search\"><parameter name=\"query\">weather</parameter></invoke>",
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

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o","input":"ping","tools":[{"type":"function","name":"search","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}],"tool_choice":"required"}`))
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
	if second["type"] != "function_call" {
		t.Fatalf("expected function_call item, got %#v", second["type"])
	}
}

func TestResponsesRouteReturnsMultipleXMLFunctionCallOutputs(t *testing.T) {
	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_tool_multi",
			"object":  "chat.completion",
			"created": 123,
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role": "assistant",
					"content": "prefix\n<Function_Test_Start>\n<function_calls>\n" +
						"  <invoke name=\"search\"><parameter name=\"query\">weather</parameter></invoke>\n" +
						"  <invoke name=\"write_file\"><parameter name=\"path\">/tmp/out.txt</parameter></invoke>\n" +
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

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o","input":"ping","tools":[{"type":"function","name":"search","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}},{"type":"function","name":"write_file","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}],"tool_choice":"required"}`))
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
	if len(output) != 3 {
		t.Fatalf("expected message + 2 function_call output items, got %#v", output)
	}
	second, _ := output[1].(map[string]any)
	third, _ := output[2].(map[string]any)
	if second["name"] != "search" || third["name"] != "write_file" {
		t.Fatalf("expected search then write_file outputs, got %#v", output)
	}
}

func TestResponsesRouteRejectsUnadvertisedXMLFunctionCallOutput(t *testing.T) {
	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_tool_invalid",
			"object":  "chat.completion",
			"created": 123,
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role": "assistant",
					"content": "prefix\n<Function_Test_Start>\n<function_calls>\n" +
						"  <invoke name=\"delete_all\"><parameter name=\"confirm\">yes</parameter></invoke>\n" +
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

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o","input":"ping","tools":[{"type":"function","name":"search","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}],"tool_choice":"required"}`))
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
	if len(output) != 1 {
		t.Fatalf("expected invalid tool output to stay as plain message only, got %#v", output)
	}
}

func TestResponsesRouteReturnsSentinelJSONFunctionCallOutput(t *testing.T) {
	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl_tool_json",
			"object":  "chat.completion",
			"created": 123,
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "prefix\n<Function_Test_Start>\n<TOOL_CALL>\n{\"name\":\"search\",\"arguments\":{\"query\":\"weather\"}}\n</TOOL_CALL>",
				},
				"finish_reason": "stop",
			}},
		})
	}))

	cfg := testConfig()
	cfg.Features.SoftToolProtocol = config.SoftToolProtocolSentinelJSON
	cfg.UpstreamServices[0].BaseURL = upstream.URL + "/v1"

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.trigger = "<Function_Test_Start>"

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o","input":"ping","tools":[{"type":"function","name":"search","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}],"tool_choice":"required"}`))
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
	if second["type"] != "function_call" {
		t.Fatalf("expected function_call item, got %#v", second["type"])
	}
	if second["name"] != "search" {
		t.Fatalf("expected function_call name search, got %#v", second["name"])
	}
}

func TestAdminLogsReturnsBufferedEntries(t *testing.T) {
	logger, logStore := testLogger(t)
	app, err := NewApp(testConfig(), nil, nil, logger, logStore, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	ctx := newAdminTestContextForApp(t, app, logStore, "secret", "")
	logger.Info("admin.log.test", "scope", "buffered")

	sessionCookie := loginAdmin(t, ctx.handler, "secret")
	req := httptest.NewRequest(http.MethodGet, "/admin/api/logs?limit=10", nil)
	req.AddCookie(sessionCookie)
	rec := httptest.NewRecorder()

	ctx.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Status  string             `json:"status"`
		Count   int                `json:"count"`
		Entries []logging.LogEntry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("expected ok status, got %#v", body.Status)
	}
	if body.Count == 0 || len(body.Entries) == 0 {
		t.Fatalf("expected buffered log entries")
	}
	if body.Entries[len(body.Entries)-1].Message != "admin.log.test" {
		t.Fatalf("expected latest entry to be admin.log.test, got %#v", body.Entries[len(body.Entries)-1].Message)
	}
}

func TestAdminLogsStreamEmitsEntries(t *testing.T) {
	logger, logStore := testLogger(t)
	app, err := NewApp(testConfig(), nil, nil, logger, logStore, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	ctx := newAdminTestContextForApp(t, app, logStore, "secret", "")
	server := newTestServer(t, ctx.handler)
	sessionCookie := loginAdmin(t, ctx.handler, "secret")

	req, err := http.NewRequest(http.MethodGet, server.URL+"/admin/api/logs/stream", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.AddCookie(sessionCookie)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to connect to log stream: %v", err)
	}
	defer resp.Body.Close()

	logger.Info("stream.log.test", "scope", "sse")

	reader := bufio.NewReader(resp.Body)
	deadline := time.Now().Add(2 * time.Second)
	var received strings.Builder

	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("failed to read stream: %v", err)
		}
		received.WriteString(line)
		if strings.Contains(received.String(), `"message":"stream.log.test"`) {
			return
		}
	}

	t.Fatalf("expected stream to contain log event, got %s", received.String())
}

func TestAdminLogMetaAndRaw(t *testing.T) {
	logger, logStore := testLogger(t)
	app, err := NewApp(testConfig(), nil, nil, logger, logStore, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	ctx := newAdminTestContextForApp(t, app, logStore, "secret", "")
	logger.Info("admin.raw.test", "scope", "raw")
	sessionCookie := loginAdmin(t, ctx.handler, "secret")

	metaReq := httptest.NewRequest(http.MethodGet, "/admin/api/logs/meta", nil)
	metaReq.AddCookie(sessionCookie)
	metaRec := httptest.NewRecorder()
	ctx.handler.ServeHTTP(metaRec, metaReq)

	if metaRec.Code != http.StatusOK {
		t.Fatalf("expected meta 200, got %d", metaRec.Code)
	}

	var metaBody struct {
		Status string              `json:"status"`
		Meta   logging.LogFileMeta `json:"meta"`
	}
	if err := json.Unmarshal(metaRec.Body.Bytes(), &metaBody); err != nil {
		t.Fatalf("failed to decode meta body: %v", err)
	}
	if metaBody.Status != "ok" || !metaBody.Meta.Exists || metaBody.Meta.Path == "" {
		t.Fatalf("unexpected meta response: %#v", metaBody)
	}

	rawReq := httptest.NewRequest(http.MethodGet, "/admin/api/logs/raw?limit=10", nil)
	rawReq.AddCookie(sessionCookie)
	rawRec := httptest.NewRecorder()
	ctx.handler.ServeHTTP(rawRec, rawReq)

	if rawRec.Code != http.StatusOK {
		t.Fatalf("expected raw 200, got %d", rawRec.Code)
	}
	if !strings.Contains(rawRec.Body.String(), "msg=admin.raw.test") {
		t.Fatalf("expected raw tail to contain log line, got %s", rawRec.Body.String())
	}
}

func TestWriteSSEHeadersIncludeUTF8Charset(t *testing.T) {
	rec := httptest.NewRecorder()

	writeSSEHeaders(rec)

	if contentType := rec.Header().Get("Content-Type"); contentType != "text/event-stream; charset=utf-8" {
		t.Fatalf("expected utf-8 sse content type, got %q", contentType)
	}
}

func TestResponsesRawStreamProxyLogsAndRelaysEachSSELine(t *testing.T) {
	upstream := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("expected raw responses path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.created\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))

	cfg := testConfig()
	cfg.UpstreamServices[0].BaseURL = upstream.URL + "/v1"
	cfg.UpstreamServices[0].UpstreamProtocol = config.UpstreamProtocolResponses

	logger, logStore := testLogger(t)
	app, err := NewApp(cfg, nil, nil, logger, logStore, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o","input":"ping","previous_response_id":"resp_prev","stream":true}`))
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()

	app.Routes(nil).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `data: {"type":"response.created"}`) {
		t.Fatalf("expected raw SSE body to include response.created, got %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected raw SSE body to include done sentinel, got %s", body)
	}

	rawLogs := logStore.Raw(200)
	if !strings.Contains(rawLogs, "🟦 upstream stream") {
		t.Fatalf("expected raw SSE lines to be logged, got %s", rawLogs)
	}
	if !strings.Contains(rawLogs, "protocol=raw") || !strings.Contains(rawLogs, "event=sse_line") {
		t.Fatalf("expected raw SSE logs with protocol and event, got %s", rawLogs)
	}
	if !strings.Contains(rawLogs, "response.created") || !strings.Contains(rawLogs, "response.completed") {
		t.Fatalf("expected raw SSE payloads to appear in logs, got %s", rawLogs)
	}
}

func TestReloadConfigUpdatesLogLevel(t *testing.T) {
	levelVar := &slog.LevelVar{}
	levelVar.Set(slog.LevelInfo)

	logPath := filepath.Join(t.TempDir(), "x-tool-test.log")
	writer, err := logging.NewBufferedFileWriter(logPath, io.Discard)
	if err != nil {
		t.Fatalf("failed to create buffered file writer: %v", err)
	}
	t.Cleanup(func() {
		_ = writer.Close()
	})
	logStore := logging.NewLogStore(logPath, writer)
	options := &slog.HandlerOptions{Level: levelVar}
	handler := slog.NewTextHandler(writer, options)
	logger := slog.New(handler)

	dbPath := filepath.Join(t.TempDir(), "config-test.db")
	db, err := storage.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate sqlite database: %v", err)
	}
	configStore := config.NewConfigStore(db)

	cfg := testConfig()
	cfg.Features.LogLevel = "DEBUG"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if err := configStore.SaveAppConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app, err := NewApp(testConfig(), configStore, nil, logger, logStore, levelVar)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	if app.logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatalf("expected debug to be disabled before reload")
	}

	if err := app.ReloadConfig(); err != nil {
		t.Fatalf("reload config: %v", err)
	}

	if !app.logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatalf("expected debug to be enabled after reload")
	}
}

func testLogger(t *testing.T) (*slog.Logger, *logging.LogStore) {
	t.Helper()

	logPath := filepath.Join(t.TempDir(), "x-tool-test.log")
	writer, err := logging.NewBufferedFileWriter(logPath, io.Discard)
	if err != nil {
		t.Fatalf("failed to create buffered file writer: %v", err)
	}
	t.Cleanup(func() {
		_ = writer.Close()
	})

	store := logging.NewLogStore(logPath, writer)
	levelVar := &slog.LevelVar{}
	levelVar.Set(slog.LevelDebug)
	options := &slog.HandlerOptions{Level: levelVar}
	handler := slog.NewTextHandler(writer, options)
	return slog.New(handler), store
}

type adminTestContext struct {
	store   *config.ConfigStore
	handler http.Handler
	admin   *admin.Admin
}

func newAdminTestContextForApp(t *testing.T, app *App, logStore *logging.LogStore, envPassword string, dbPassword string) *adminTestContext {
	t.Helper()
	if app == nil {
		t.Fatalf("app is nil")
	}

	dbPath := filepath.Join(t.TempDir(), "admin-test.db")
	db, err := storage.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate sqlite database: %v", err)
	}

	store := config.NewConfigStore(db)
	if strings.TrimSpace(dbPassword) != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(dbPassword), bcrypt.DefaultCost)
		if err != nil {
			t.Fatalf("generate bcrypt hash: %v", err)
		}
		if err := store.SetAdminPasswordHash(string(hash)); err != nil {
			t.Fatalf("set admin password hash: %v", err)
		}
	}

	adminHandler := &admin.Admin{
		GetConfig:  app.Config,
		GetRouting: app.Routing,
		GetStats: func() map[string]any {
			snapshot := app.GetRuntimeStats()
			return map[string]any{
				"total_requests":    snapshot.TotalRequests,
				"inflight_requests": snapshot.InflightRequests,
				"stream_requests":   snapshot.StreamRequests,
				"status_2xx":        snapshot.Status2xx,
				"status_4xx":        snapshot.Status4xx,
				"status_5xx":        snapshot.Status5xx,
				"updated_at":        snapshot.UpdatedAt,
			}
		},
		ConfigStore:      store,
		LogStore:         logStore,
		StartedAt:        time.Now().UTC(),
		ReloadConfig:     app.ReloadConfig,
		EnvAdminPassword: envPassword,
	}

	return &adminTestContext{
		store:   store,
		handler: app.Routes(adminHandler),
		admin:   adminHandler,
	}
}

func loginAdmin(t *testing.T, handler http.Handler, password string) *http.Cookie {
	t.Helper()

	body, err := json.Marshal(map[string]string{"password": password})
	if err != nil {
		t.Fatalf("marshal login request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected admin login success, got %d body=%s", rec.Code, rec.Body.String())
	}

	resp := rec.Result()
	defer resp.Body.Close()
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "x_tool_admin_session" {
			return cookie
		}
	}
	t.Fatalf("expected admin session cookie in login response")
	return nil
}

func newTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("skipping test: cannot listen on TCP port: %v", err)
	}

	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func testConfig() *config.AppConfig {
	return &config.AppConfig{
		Server: config.ServerConfig{
			Port:    8000,
			Host:    "127.0.0.1",
			Timeout: 30,
		},
		UpstreamServices: []config.UpstreamService{
			{
				Name:       "openai",
				BaseURL:    "https://example.com/v1",
				APIKey:     "upstream-key",
				IsDefault:  true,
				Models:     []string{"gpt-4o"},
				ClientKeys: []string{"client-key"},
			},
		},
		Features: config.FeaturesConfig{
			EnableFunctionCalling:    true,
			LogLevel:                 "INFO",
			ConvertDeveloperToSystem: true,
			PromptTemplate:           "{tool_catalog}\n{trigger_signal}",
		},
	}
}

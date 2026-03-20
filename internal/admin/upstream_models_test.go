package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"x-tool/internal/config"
)

func TestHandleAdminUpstreamModelsReturnsSortedUniqueIDs(t *testing.T) {
	adminHandler := &Admin{
		GetConfig: func() *config.AppConfig {
			return &config.AppConfig{
				Server: config.ServerConfig{Timeout: 10},
			}
		},
		HTTPClient: newRoundTripClient(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodGet {
				t.Fatalf("expected GET, got %s", r.Method)
			}
			if got := r.URL.String(); got != "https://upstream.example.com/v1/models" {
				t.Fatalf("expected upstream URL, got %s", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer upstream-key" {
				t.Fatalf("expected bearer auth header, got %q", got)
			}
			rec := httptest.NewRecorder()
			_ = json.NewEncoder(rec).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "gpt-4o"},
					{"id": "gpt-4.1"},
					{"id": "gpt-4o"},
					{"name": "o4-mini"},
				},
			})
			return rec.Result(), nil
		}),
	}

	body := bytes.NewReader([]byte(`{"base_url":"https://upstream.example.com/v1","api_key":"upstream-key"}`))
	req := httptest.NewRequest(http.MethodPost, "/admin/api/upstream-models", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	adminHandler.handleAdminUpstreamModels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Status string   `json:"status"`
		Models []string `json:"models"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Status != "ok" {
		t.Fatalf("expected status ok, got %q", payload.Status)
	}
	want := []string{"gpt-4.1", "gpt-4o", "o4-mini"}
	if len(payload.Models) != len(want) {
		t.Fatalf("expected %d models, got %d (%v)", len(want), len(payload.Models), payload.Models)
	}
	for idx := range want {
		if payload.Models[idx] != want[idx] {
			t.Fatalf("expected models %v, got %v", want, payload.Models)
		}
	}
}

func TestHandleAdminUpstreamModelsRejectsMissingBaseURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/admin/api/upstream-models", bytes.NewReader([]byte(`{"api_key":"test"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	adminHandler := &Admin{}
	adminHandler.handleAdminUpstreamModels(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleAdminUpstreamModelsMapsUpstreamStatusErrors(t *testing.T) {
	body := bytes.NewReader([]byte(`{"base_url":"https://upstream.example.com","api_key":"bad-key"}`))
	req := httptest.NewRequest(http.MethodPost, "/admin/api/upstream-models", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	adminHandler := &Admin{
		HTTPClient: newRoundTripClient(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			rec.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(rec).Encode(map[string]any{
				"error": map[string]any{
					"message": "unauthorized",
				},
			})
			return rec.Result(), nil
		}),
	}
	adminHandler.handleAdminUpstreamModels(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body=%s", rec.Code, rec.Body.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func newRoundTripClient(fn roundTripFunc) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			resp, err := fn(r)
			if err != nil || resp == nil {
				return resp, err
			}
			if resp.Body == nil {
				resp.Body = io.NopCloser(bytes.NewReader(nil))
			}
			return resp, nil
		}),
	}
}

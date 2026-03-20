package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

var (
	errUpstreamModelsBaseURLRequired = errors.New("base_url is required")
	errUpstreamModelsMissingIDs      = errors.New("upstream response does not contain any model ids")
)

type upstreamModelsStatusError struct {
	StatusCode int
	Message    string
}

func (e *upstreamModelsStatusError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("upstream /models returned status %d", e.StatusCode)
	}
	return fmt.Sprintf("upstream /models returned status %d: %s", e.StatusCode, e.Message)
}

func (a *Admin) handleAdminUpstreamModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	var body struct {
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid upstream model sync request", "invalid_request", err.Error())
		return
	}

	modelIDs, err := a.fetchUpstreamModelIDs(body.BaseURL, body.APIKey)
	if err != nil {
		statusCode := http.StatusBadGateway
		var statusErr *upstreamModelsStatusError
		switch {
		case errors.Is(err, errUpstreamModelsBaseURLRequired):
			statusCode = http.StatusBadRequest
		case errors.As(err, &statusErr):
			statusCode = http.StatusBadGateway
		}
		writeError(w, statusCode, "Failed to sync upstream model list", "upstream_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"models": modelIDs,
	})
}

func (a *Admin) fetchUpstreamModelIDs(baseURL, apiKey string) ([]string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errUpstreamModelsBaseURLRequired
	}

	req, err := http.NewRequest(http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("build upstream /models request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}

	resp, err := a.upstreamModelsHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("request upstream /models: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read upstream /models response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &upstreamModelsStatusError{
			StatusCode: resp.StatusCode,
			Message:    upstreamModelsErrorMessage(body),
		}
	}

	modelIDs, err := decodeUpstreamModelIDs(body)
	if err != nil {
		return nil, err
	}
	return modelIDs, nil
}

func (a *Admin) upstreamModelsHTTPClient() *http.Client {
	if a != nil && a.HTTPClient != nil {
		return a.HTTPClient
	}
	timeout := 30 * time.Second
	if cfg := a.getConfig(); cfg != nil {
		if cfgTimeout := cfg.Server.TimeoutDuration(); cfgTimeout > 0 {
			timeout = cfgTimeout
		}
	}
	return &http.Client{Timeout: timeout}
}

func decodeUpstreamModelIDs(body []byte) ([]string, error) {
	var envelope struct {
		Data   []json.RawMessage `json:"data"`
		Models []json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		var rawItems []json.RawMessage
		if arrayErr := json.Unmarshal(body, &rawItems); arrayErr != nil {
			return nil, fmt.Errorf("decode upstream /models response: %w", err)
		}
		return collectUpstreamModelIDs(rawItems)
	}

	items := envelope.Data
	if len(items) == 0 {
		items = envelope.Models
	}
	return collectUpstreamModelIDs(items)
}

func collectUpstreamModelIDs(items []json.RawMessage) ([]string, error) {
	if len(items) == 0 {
		return nil, errUpstreamModelsMissingIDs
	}

	seen := map[string]struct{}{}
	modelIDs := make([]string, 0, len(items))
	for _, item := range items {
		modelID := strings.TrimSpace(extractUpstreamModelID(item))
		if modelID == "" {
			continue
		}
		if _, ok := seen[modelID]; ok {
			continue
		}
		seen[modelID] = struct{}{}
		modelIDs = append(modelIDs, modelID)
	}

	if len(modelIDs) == 0 {
		return nil, errUpstreamModelsMissingIDs
	}
	sort.Strings(modelIDs)
	return modelIDs, nil
}

func extractUpstreamModelID(raw json.RawMessage) string {
	var modelID string
	if err := json.Unmarshal(raw, &modelID); err == nil {
		return modelID
	}

	var model struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &model); err != nil {
		return ""
	}
	if strings.TrimSpace(model.ID) != "" {
		return model.ID
	}
	return model.Name
}

func upstreamModelsErrorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		if message := strings.TrimSpace(payload.Error.Message); message != "" {
			return message
		}
		if message := strings.TrimSpace(payload.Message); message != "" {
			return message
		}
	}

	message := strings.TrimSpace(string(body))
	if len(message) > 240 {
		message = message[:240]
	}
	return message
}

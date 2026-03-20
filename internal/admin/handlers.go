package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"x-tool/internal/config"
	"x-tool/internal/logging"
)

type Admin struct {
	GetConfig        func() *config.AppConfig
	GetRouting       func() *config.RoutingTable
	GetStats         func() map[string]any
	HTTPClient       *http.Client
	ConfigStore      *config.ConfigStore
	LogStore         *logging.LogStore
	StartedAt        time.Time
	ReloadConfig     func() error
	EnvAdminPassword string
}

func (a *Admin) RegisterRoutes(mux *http.ServeMux) {
	if mux == nil || a == nil {
		return
	}
	mux.Handle("/admin", http.HandlerFunc(a.handleAdminRedirect))
	mux.Handle("/admin/api/auth/login", http.HandlerFunc(a.handleAdminLogin))
	mux.Handle("/admin/api/auth/status", http.HandlerFunc(a.handleAdminStatus))
	mux.Handle("/admin/api/auth/logout", a.RequireSession(http.HandlerFunc(a.handleAdminLogout)))
	mux.Handle("/admin/api/auth/password", a.RequireSession(http.HandlerFunc(a.handleAdminPassword)))
	mux.Handle("/admin/api/overview", a.RequireSession(http.HandlerFunc(a.handleAdminOverview)))
	mux.Handle("/admin/api/config", a.RequireSession(http.HandlerFunc(a.handleAdminConfig)))
	mux.Handle("/admin/api/upstream-models", a.RequireSession(http.HandlerFunc(a.handleAdminUpstreamModels)))
	mux.Handle("/admin/api/logs", a.RequireSession(http.HandlerFunc(a.handleAdminLogs)))
	mux.Handle("/admin/api/logs/meta", a.RequireSession(http.HandlerFunc(a.handleAdminLogMeta)))
	mux.Handle("/admin/api/logs/raw", a.RequireSession(http.HandlerFunc(a.handleAdminLogRaw)))
	mux.Handle("/admin/api/logs/stream", a.RequireSession(http.HandlerFunc(a.handleAdminLogStream)))
}

func (a *Admin) handleAdminRedirect(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/admin/", http.StatusTemporaryRedirect)
}

func (a *Admin) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid login request", "invalid_request", err.Error())
		return
	}

	source, valid, err := a.verifyPassword(body.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to verify admin password", "server_error", "internal_error")
		return
	}
	if source == adminPasswordSourceNone {
		writeAdminUnavailable(w)
		return
	}
	if !valid {
		writeAdminUnauthorized(w)
		return
	}
	if err := a.issueSession(w, r); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create admin session", "server_error", "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"available":     true,
		"authenticated": true,
	})
}

func (a *Admin) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	source, _, err := a.passwordSource()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to inspect admin auth status", "server_error", "internal_error")
		return
	}

	available := source != adminPasswordSourceNone
	authenticated := false
	if available {
		authenticated, err = a.authenticated(r)
		if err != nil {
			if err == errAdminUnavailable {
				available = false
			} else {
				writeError(w, http.StatusInternalServerError, "Failed to inspect admin auth status", "server_error", "internal_error")
				return
			}
		}
	}
	if !authenticated {
		if _, ok := a.sessionToken(r); ok {
			a.clearSessionCookie(w, r)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"available":     available,
		"authenticated": authenticated,
	})
}

func (a *Admin) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	if token, ok := a.sessionToken(r); ok && a.ConfigStore != nil {
		if err := a.ConfigStore.DeleteAdminSession(hashAdminSessionToken(token)); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to clear admin session", "server_error", "internal_error")
			return
		}
	}
	a.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "logged_out",
	})
}

func (a *Admin) handleAdminPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if a.ConfigStore == nil {
		writeError(w, http.StatusInternalServerError, "Configuration store unavailable", "server_error", "internal_error")
		return
	}

	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid password update request", "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(body.NewPassword) == "" {
		writeError(w, http.StatusBadRequest, "New password cannot be empty", "invalid_request", "new_password_required")
		return
	}

	source, valid, err := a.verifyPassword(body.CurrentPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to verify current password", "server_error", "internal_error")
		return
	}
	if source == adminPasswordSourceNone {
		writeAdminUnavailable(w)
		return
	}
	if !valid {
		writeError(w, http.StatusUnauthorized, "Current password is incorrect", "authentication_error", "unauthorized")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to hash new password", "server_error", "internal_error")
		return
	}
	if err := a.ConfigStore.SetAdminPasswordHash(string(hash)); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to store new admin password", "server_error", "internal_error")
		return
	}
	if err := a.ConfigStore.DeleteAllAdminSessions(); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to invalidate admin sessions", "server_error", "internal_error")
		return
	}

	a.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "password_updated",
	})
}

func (a *Admin) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	cfg := a.getConfig()
	routing := a.getRouting()
	if cfg == nil || routing == nil {
		writeError(w, http.StatusInternalServerError, "Configuration unavailable", "server_error", "internal_error")
		return
	}

	visibleModels := visibleModels(routing)
	aliases := aliasSummaries(routing)
	upstreams := make([]map[string]any, 0, len(cfg.UpstreamServices))
	for _, upstream := range cfg.UpstreamServices {
		upstreams = append(upstreams, map[string]any{
			"name":                    upstream.Name,
			"base_url":                upstream.BaseURL,
			"description":             upstream.Description,
			"is_default":              upstream.IsDefault,
			"models_count":            len(upstream.Models),
			"client_keys_count":       len(upstream.ClientKeys),
			"upstream_protocol":       upstream.UpstreamProtocol,
			"prompt_injection_target": upstream.PromptInjectionTarget,
		})
	}

	logPath := ""
	if a.LogStore != nil {
		logPath = a.LogStore.Path()
	}
	startedAt := a.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}

	statsPayload := map[string]any{}
	if a.GetStats != nil {
		statsPayload = a.GetStats()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"runtime": map[string]any{
			"language":   "go",
			"started_at": startedAt.Format(time.RFC3339),
			"trigger":    "random",
			"log_path":   logPath,
		},
		"server": map[string]any{
			"host":            cfg.Server.Host,
			"port":            cfg.Server.Port,
			"timeout_seconds": cfg.Server.Timeout,
		},
		"auth": map[string]any{
			"client_keys_count": len(cfg.ClientKeys()),
			"key_passthrough":   cfg.Features.KeyPassthrough,
		},
		"features": map[string]any{
			"enable_function_calling":     cfg.Features.EnableFunctionCalling,
			"convert_developer_to_system": cfg.Features.ConvertDeveloperToSystem,
			"key_passthrough":             cfg.Features.KeyPassthrough,
			"model_passthrough":           cfg.Features.ModelPassthrough,
			"custom_prompt_template":      strings.TrimSpace(cfg.Features.PromptTemplate) != "",
			"log_level":                   cfg.Features.LogLevel,
			"prompt_injection_target":     cfg.Features.PromptInjectionTarget,
		},
		"summary": map[string]any{
			"upstreams_count":      len(cfg.UpstreamServices),
			"visible_models_count": len(visibleModels),
			"aliases_count":        len(aliases),
		},
		"stats":     statsPayload,
		"upstreams": upstreams,
		"models":    visibleModels,
		"aliases":   aliases,
	})
}

func (a *Admin) handleAdminConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		cfg := a.getConfig()
		if cfg == nil {
			writeError(w, http.StatusInternalServerError, "Configuration unavailable", "server_error", "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, cfg)
		return
	}

	if r.Method == http.MethodPost {
		var newConfig config.AppConfig
		if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid configuration representation", "invalid_request", err.Error())
			return
		}

		if err := newConfig.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, "Configuration validation failed", "invalid_request", err.Error())
			return
		}

		if a.ConfigStore == nil {
			writeError(w, http.StatusInternalServerError, "Configuration store unavailable", "server_error", "internal_error")
			return
		}

		if err := a.ConfigStore.SaveAppConfig(&newConfig); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to persist configuration", "server_error", err.Error())
			return
		}

		if a.ReloadConfig != nil {
			if err := a.ReloadConfig(); err != nil {
				writeError(w, http.StatusInternalServerError, "Failed to reload configuration", "server_error", err.Error())
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "Config applied"})
		return
	}

	writeMethodNotAllowed(w)
}

func (a *Admin) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	limit := 200
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	entries := []logging.LogEntry{}
	if a.LogStore != nil {
		entries = a.LogStore.List(limit)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"count":   len(entries),
		"entries": entries,
	})
}

func (a *Admin) handleAdminLogMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	meta := logging.LogFileMeta{}
	if a.LogStore != nil {
		meta = a.LogStore.Meta()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"meta":   meta,
	})
}

func (a *Admin) handleAdminLogRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	limit := 200
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if a.LogStore == nil {
		return
	}
	_, _ = io.WriteString(w, a.LogStore.Raw(limit))
}

func (a *Admin) handleAdminLogStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	if a.LogStore == nil {
		writeError(w, http.StatusInternalServerError, "Log store unavailable", "server_error", "internal_error")
		return
	}

	writeSSEHeaders(w)
	w.Header().Set("X-Accel-Buffering", "no")
	_, _ = io.WriteString(w, ": connected\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	entries, cancel := a.LogStore.Subscribe(128)
	defer cancel()

	for {
		select {
		case <-r.Context().Done():
			return
		case entry, ok := <-entries:
			if !ok {
				return
			}
			if err := writeSSEEvent(w, "log", entry); err != nil {
				return
			}
		}
	}
}

func (a *Admin) getConfig() *config.AppConfig {
	if a == nil || a.GetConfig == nil {
		return nil
	}
	return a.GetConfig()
}

func (a *Admin) getRouting() *config.RoutingTable {
	if a == nil || a.GetRouting == nil {
		return nil
	}
	return a.GetRouting()
}

func visibleModels(routing *config.RoutingTable) []string {
	if routing == nil {
		return nil
	}
	visible := map[string]struct{}{}
	for modelName := range routing.RequestedModelToRoutes {
		visible[modelName] = struct{}{}
	}

	models := make([]string, 0, len(visible))
	for modelName := range visible {
		models = append(models, modelName)
	}
	sort.Strings(models)
	return models
}

func aliasSummaries(routing *config.RoutingTable) []map[string]any {
	if routing == nil {
		return nil
	}
	aliases := make([]string, 0, len(routing.AliasToModels))
	for alias := range routing.AliasToModels {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)

	result := make([]map[string]any, 0, len(aliases))
	for _, alias := range aliases {
		targets := append([]string(nil), routing.AliasToModels[alias]...)
		sort.Strings(targets)
		result = append(result, map[string]any{
			"alias":   alias,
			"targets": targets,
		})
	}
	return result
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "Method not allowed", "invalid_request_error", "method_not_allowed")
}

func writeError(w http.ResponseWriter, status int, message, errorType, code string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    errorType,
			"code":    code,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
}

func writeSSEEvent(w http.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if strings.TrimSpace(event) != "" {
		_, err = io.WriteString(w, "event: "+event+"\ndata: "+string(data)+"\n\n")
	} else {
		_, err = io.WriteString(w, "data: "+string(data)+"\n\n")
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return err
}

package proxy

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func (a *App) debugEnabled() bool {
	if a == nil || a.logger == nil {
		return false
	}
	return a.logger.Enabled(context.Background(), slog.LevelDebug)
}

func (a *App) logInfo(stage string, attrs ...any) {
	if a == nil || a.logger == nil {
		return
	}
	a.logger.Info("proxy.info", append([]any{
		"stage", stage,
	}, attrs...)...)
}

func (a *App) logUpstreamResponseInfo(protocol, upstreamURL string, statusCode int, duration time.Duration) {
	a.logInfo("upstream.response",
		"protocol", protocol,
		"status", statusCode,
		"duration_ms", duration.Milliseconds(),
	)
}

// logRequestDebug logs incoming request with raw body
func (a *App) logRequestDebug(protocol string, r *http.Request, bodySize int, attrs ...any) {
	if !a.debugEnabled() {
		return
	}
	a.logger.Debug("📥 request", append([]any{
		"protocol", protocol,
		"method", r.Method,
		"path", r.URL.Path,
		"body_bytes", bodySize,
	}, attrs...)...)
}

// logUpstreamRequestDebug logs upstream request with transform hints
func (a *App) logUpstreamRequestDebug(protocol, upstreamURL string, requestBody map[string]any, hasFunctionCall bool) {
	if !a.debugEnabled() {
		return
	}
	a.logger.Debug("📤 upstream.request",
		"protocol", protocol,
		"url", upstreamURL,
		"model", requestBody["model"],
		"transform", hasFunctionCall,
		"body", jsonCompact(requestBody),
	)
}

// logJSONResponseDebug logs JSON response
func (a *App) logJSONResponseDebug(protocol string, statusCode int, payload map[string]any) {
	if !a.debugEnabled() {
		return
	}
	a.logger.Debug("📦 response",
		"protocol", protocol,
		"status", statusCode,
		"body", jsonCompact(payload),
	)
}

// logStreamDebug logs stream events with direction marker
func (a *App) logStreamDebug(protocol, direction, event string, attrs ...any) {
	if !a.debugEnabled() {
		return
	}
	emoji := "🌊"
	switch direction {
	case "inbound":
		emoji = "🟦 upstream"
	case "outbound":
		emoji = "🟩 client"
	}
	a.logger.Debug(emoji+" stream", append([]any{
		"protocol", protocol,
		"event", event,
	}, attrs...)...)
}

// logClientSSEEvent logs SSE event sent to client
func (a *App) logClientSSEEvent(protocol, event string, payload any) {
	if !a.debugEnabled() {
		return
	}
	eventName := strings.TrimSpace(event)
	if eventName == "" {
		eventName = "message"
	}
	a.logStreamDebug(protocol, "outbound", eventName, "payload", jsonCompact(payload))
}

// Helper: compact JSON for logging
func jsonCompact(v any) string {
	if v == nil {
		return ""
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

// summarizeChatRequest returns attrs for chat request logging (kept for compatibility)
func summarizeChatRequest(req any) []any {
	if r, ok := req.(interface{ GetRaw() map[string]any }); ok {
		return []any{"raw", jsonCompact(r.GetRaw())}
	}
	return nil
}

// summarizeResponsesRequest returns attrs for responses request logging (kept for compatibility)
func summarizeResponsesRequest(req any) []any {
	if r, ok := req.(interface{ GetRaw() map[string]any }); ok {
		return []any{"raw", jsonCompact(r.GetRaw())}
	}
	return nil
}

// summarizeAnthropicRequest returns attrs for anthropic request logging (kept for compatibility)
func summarizeAnthropicRequest(req any) []any {
	if r, ok := req.(interface{ GetRaw() map[string]any }); ok {
		return []any{"raw", jsonCompact(r.GetRaw())}
	}
	return nil
}

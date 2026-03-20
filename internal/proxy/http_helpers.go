package proxy

import (
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (a *App) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				a.logger.Error("panic recovered", "panic", rec)
				writeError(w, http.StatusInternalServerError, "Internal server error", "server_error", "internal_error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func extractDeltaContent(chunk map[string]any) string {
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	firstChoice, _ := choices[0].(map[string]any)
	delta, _ := firstChoice["delta"].(map[string]any)
	content, _ := delta["content"].(string)
	return content
}

func extractDeltaThinking(chunk map[string]any) string {
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	firstChoice, _ := choices[0].(map[string]any)
	delta, _ := firstChoice["delta"].(map[string]any)
	if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
		return rc
	}
	if r, ok := delta["reasoning"].(string); ok && r != "" {
		return r
	}
	return ""
}

func buildContentChunk(model, content string) map[string]any {
	return map[string]any{
		"id":      newID("chatcmpl-passthrough-"),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index": 0,
			"delta": map[string]any{
				"content": content,
			},
		}},
	}
}

func buildThinkingChunk(model, thinking string) map[string]any {
	return map[string]any{
		"id":      newID("chatcmpl-passthrough-"),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index": 0,
			"delta": map[string]any{
				"reasoning_content": thinking,
			},
		}},
	}
}

func writeErrorChunk(w http.ResponseWriter, content string) error {
	return writeSSE(w, map[string]any{
		"id": "error-chunk",
		"choices": []map[string]any{{
			"delta": map[string]any{
				"content": content,
			},
		}},
	})
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func nilIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func mapStreamErrorMessage(statusCode int) string {
	switch {
	case statusCode == http.StatusUnauthorized:
		return "Authentication failed"
	case statusCode == http.StatusForbidden:
		return "Access forbidden"
	case statusCode == http.StatusTooManyRequests:
		return "Rate limit exceeded"
	case statusCode >= http.StatusInternalServerError:
		return "Upstream service temporarily unavailable"
	default:
		return "Request processing failed"
	}
}

func writeMappedUpstreamError(w http.ResponseWriter, statusCode int) {
	switch {
	case statusCode == http.StatusBadRequest:
		writeError(w, statusCode, "Invalid request parameters", "invalid_request_error", "bad_request")
	case statusCode == http.StatusUnauthorized:
		writeError(w, statusCode, "Authentication failed", "authentication_error", "unauthorized")
	case statusCode == http.StatusForbidden:
		writeError(w, statusCode, "Access forbidden", "permission_error", "forbidden")
	case statusCode == http.StatusTooManyRequests:
		writeError(w, statusCode, "Rate limit exceeded", "rate_limit_error", "rate_limit_exceeded")
	case statusCode >= http.StatusInternalServerError:
		writeError(w, statusCode, "Upstream service temporarily unavailable", "service_error", "upstream_error")
	default:
		writeError(w, statusCode, "Request processing failed", "api_error", "unknown_error")
	}
}

func writeUpstreamTransportError(w http.ResponseWriter) {
	writeError(w, http.StatusBadGateway, "Failed to connect to upstream service", "connection_error", "bad_gateway")
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

func writeSSE(w http.ResponseWriter, payload any) error {
	return writeSSEEvent(w, "", payload)
}

func writeSSEEvent(w http.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeSSEEventData(w, event, data)
}

func writeSSEEventData(w http.ResponseWriter, event string, data []byte) error {
	if strings.TrimSpace(event) != "" {
		_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return err
	}

	_, err := fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return err
}

func writeDone(w http.ResponseWriter) error {
	_, err := io.WriteString(w, "data: [DONE]\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return err
}

func (a *App) writeLoggedSSE(w http.ResponseWriter, protocol, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	a.logClientSSEEvent(protocol, event, payload)
	return writeSSEEventData(w, event, data)
}

func (a *App) writeLoggedDone(w http.ResponseWriter, protocol string) error {
	a.logStreamDebug(protocol, "outbound", "done", "payload", "[DONE]")
	a.logInfo("client.receive.end", "protocol", protocol, "stream", true, "result", "[DONE]")
	return writeDone(w)
}

func newID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := cryptorand.Read(buf); err != nil {
		return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s%x", prefix, buf)
}

// ErrRequestBodyTooLarge is returned when request body exceeds the limit
var ErrRequestBodyTooLarge = errors.New("request body too large")

// readRequestBody reads request body with size limit
func (a *App) readRequestBody(r *http.Request) ([]byte, error) {
	maxBytes := a.MaxRequestBodyBytes()
	if maxBytes <= 0 {
		maxBytes = 50 * 1024 * 1024 // default 50MB
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, ErrRequestBodyTooLarge
	}
	return body, nil
}

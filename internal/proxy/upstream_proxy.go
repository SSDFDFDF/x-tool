package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

func (a *App) proxyJSON(w http.ResponseWriter, ctx context.Context, upstreamURL string, requestBody map[string]any, headers map[string]string, softTool *softToolCallSettings) {
	resp, body, err := a.doJSONRequest(ctx, upstreamURL, requestBody, headers)
	if err != nil {
		a.logger.Error("non-stream upstream request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal server error", "server_error", "internal_error")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		writeMappedUpstreamError(w, resp.StatusCode)
		return
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		a.logger.Error("failed to decode upstream JSON response", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal server error", "server_error", "internal_error")
		return
	}

	if softTool != nil {
		a.transformNonStreamResponse(payload, softTool)
	}
	a.logJSONResponseDebug("chat.completions", http.StatusOK, payload)
	a.logInfo("client.receive.end", "protocol", "chat.completions", "status", http.StatusOK, "stream", false, "result", "ok")
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) proxyStream(w http.ResponseWriter, ctx context.Context, upstreamURL string, requestBody map[string]any, headers map[string]string, model string, softTool *softToolCallSettings) {
	writeSSEHeaders(w)
	_ = a.streamChatCompletion(ctx, upstreamURL, requestBody, headers, model, softTool, chatStreamCallbacks{
		OnText: func(delta string) error {
			return a.writeLoggedSSE(w, "chat.completions", "", buildContentChunk(model, delta))
		},
		OnThinking: func(delta string) error {
			return a.writeLoggedSSE(w, "chat.completions", "", buildThinkingChunk(model, delta))
		},
		OnToolCalls: func(toolCalls []map[string]any) error {
			return a.emitPreparedToolCalls(w, toolCalls, model)
		},
		OnError: func(message string) error {
			return a.writeLoggedSSE(w, "chat.completions", "", map[string]any{
				"error": map[string]any{
					"message": message,
					"type":    "upstream_error",
				},
			})
		},
		OnDone: func() error {
			return a.writeLoggedDone(w, "chat.completions")
		},
	})
}

func (a *App) doJSONRequest(ctx context.Context, upstreamURL string, requestBody map[string]any, headers map[string]string) (*http.Response, []byte, error) {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, nil, err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, a.Config().Server.TimeoutDuration())
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodPost, upstreamURL, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	start := time.Now()
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	a.logUpstreamResponseInfo("json", upstreamURL, resp.StatusCode, time.Since(start))
	a.logStreamDebug("http.json", "inbound", "response", "upstream_url", upstreamURL, "status_code", resp.StatusCode, "body", string(body))
	return resp, body, nil
}

func (a *App) proxyRaw(w http.ResponseWriter, ctx context.Context, upstreamURL string, payload []byte, headers map[string]string, stream bool) {
	a.logStreamDebug("raw", "outbound", "request", "upstream_url", upstreamURL, "stream", stream, "body", string(payload))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Internal server error", "server_error", "internal_error")
		return
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	start := time.Now()
	resp, err := a.client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Failed to connect to upstream service", "connection_error", "bad_gateway")
		return
	}
	defer resp.Body.Close()
	a.logUpstreamResponseInfo("raw", upstreamURL, resp.StatusCode, time.Since(start))
	a.logStreamDebug("raw", "inbound", "headers", "upstream_url", upstreamURL, "status_code", resp.StatusCode, "headers", resp.Header)

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if stream || strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		a.proxyRawStream(w, resp.Body, upstreamURL, resp.StatusCode)
		return
	}

	body, _ := io.ReadAll(resp.Body)
	a.logStreamDebug("raw", "inbound", "body", "upstream_url", upstreamURL, "status_code", resp.StatusCode, "body", string(body))
	_, _ = w.Write(body)
	a.logInfo("client.receive.end", "protocol", "raw", "status", resp.StatusCode, "stream", false, "result", "ok")
}

func (a *App) proxyRawStream(w http.ResponseWriter, body io.Reader, upstreamURL string, statusCode int) {
	reader := bufio.NewReader(body)
	lineNo := 0

	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			lineNo++
			trimmed := strings.TrimRight(line, "\r\n")
			a.logStreamDebug("raw", "inbound", "sse_line", "upstream_url", upstreamURL, "status_code", statusCode, "line_no", lineNo, "line", trimmed)
			_, _ = io.WriteString(w, line)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			a.logStreamDebug("raw", "outbound", "sse_line", "upstream_url", upstreamURL, "status_code", statusCode, "line_no", lineNo, "line", trimmed)
		}

		if err != nil {
			if !errors.Is(err, io.EOF) {
				a.logStreamDebug("raw", "inbound", "sse_read_error", "upstream_url", upstreamURL, "status_code", statusCode, "line_no", lineNo, "error", err)
				return
			}
			a.logInfo("client.receive.end", "protocol", "raw", "stream", true, "result", "eof")
			return
		}
	}
}

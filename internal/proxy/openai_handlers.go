package proxy

import (
	"encoding/json"
	"errors"
	"net/http"

	"x-tool/internal/config"
	"x-tool/internal/protocol"
)

func (a *App) handleRoot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "OpenAI Function Call Middleware is running",
		"config": map[string]any{
			"upstream_services_count": len(a.Config().UpstreamServices),
			"client_keys_count":       len(a.Config().ClientKeys()),
			"models_count":            len(a.Routing().RequestedModelToRoutes),
			"features": map[string]any{
				"function_calling":            a.Config().Features.EnableFunctionCalling,
				"log_level":                   a.Config().Features.LogLevel,
				"convert_developer_to_system": a.Config().Features.ConvertDeveloperToSystem,
				"random_trigger":              true,
			},
		},
	})
}

func (a *App) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	clientKey, err := a.verifyAPIKey(r)
	if err != nil {
		if errors.Is(err, protocol.ErrMissingAuthorization) {
			writeError(w, http.StatusUnprocessableEntity, "Invalid request format", "invalid_request_error", "invalid_request")
			return
		}
		writeError(w, http.StatusUnauthorized, "Unauthorized", "authentication_error", "unauthorized")
		return
	}

	if len(a.Config().UpstreamServices) == 0 {
		writeError(w, http.StatusServiceUnavailable, "No upstream configured. Please configure at least one upstream in admin settings.", "service_unavailable", "upstream_not_configured")
		return
	}

	modelIDs := a.visibleModelsForProtocol(clientKey, config.UpstreamProtocolOpenAICompat)
	models := make([]map[string]any, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		models = append(models, map[string]any{
			"id":         modelID,
			"object":     "model",
			"created":    1677610602,
			"owned_by":   "openai",
			"permission": []any{},
			"root":       modelID,
			"parent":     nil,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   models,
	})
}

func (a *App) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	clientKey, err := a.verifyAPIKey(r)
	if err != nil {
		if errors.Is(err, protocol.ErrMissingAuthorization) {
			writeError(w, http.StatusUnprocessableEntity, "Invalid request format", "invalid_request_error", "invalid_request")
			return
		}
		writeError(w, http.StatusUnauthorized, "Unauthorized", "authentication_error", "unauthorized")
		return
	}

	bodyBytes, err := a.readRequestBody(r)
	if err != nil {
		if errors.Is(err, ErrRequestBodyTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "Request body too large", "invalid_request_error", "request_too_large")
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "Invalid request format", "invalid_request_error", "invalid_request")
		return
	}

	req, err := protocol.DecodeChatCompletionRequestBytes(bodyBytes)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Invalid request format", "invalid_request_error", "invalid_request")
		return
	}
	a.logRequestDebug("chat.completions", r, len(bodyBytes), summarizeChatRequest(req)...)
	a.logInfo("client.request",
		"protocol", "chat.completions",
		"model", req.Model,
		"stream", req.Stream,
		"tool_count", len(req.Tools),
	)

	upstream, actualModel, err := a.findUpstreamForProtocol(clientKey, req.Model, config.UpstreamProtocolOpenAICompat)
	if err != nil {
		if errors.Is(err, errNoUpstreamConfigured) {
			writeError(w, http.StatusServiceUnavailable, "No upstream configured. Please configure at least one upstream in admin settings.", "service_unavailable", "upstream_not_configured")
			return
		}
		if errors.Is(err, errModelNotAccessible) {
			writeError(w, http.StatusBadRequest, "The requested model is not available for this client key.", "invalid_request_error", "model_not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error(), "server_error", "internal_error")
		return
	}

	requestBody, softTool, err := a.prepareChatProxyRequest(req, actualModel, upstream)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Invalid prompt template", "server_error", "internal_error")
		return
	}
	headers := a.buildUpstreamHeaders(req.Stream, clientKey, upstream)

	upstreamURL := upstream.BaseURL + "/chat/completions"
	a.logUpstreamRequestDebug("chat.completions", upstreamURL, requestBody, softTool != nil)
	if req.Stream {
		a.recordStreamRequest()
		a.proxyStream(w, r.Context(), upstreamURL, requestBody, headers, actualModel, softTool)
		return
	}
	a.proxyJSON(w, r.Context(), upstreamURL, requestBody, headers, softTool)
}

func (a *App) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	clientKey, err := a.verifyAPIKey(r)
	if err != nil {
		if errors.Is(err, protocol.ErrMissingAuthorization) {
			writeError(w, http.StatusUnprocessableEntity, "Invalid request format", "invalid_request_error", "invalid_request")
			return
		}
		writeError(w, http.StatusUnauthorized, "Unauthorized", "authentication_error", "unauthorized")
		return
	}

	bodyBytes, err := a.readRequestBody(r)
	if err != nil {
		if errors.Is(err, ErrRequestBodyTooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "Request body too large", "invalid_request_error", "request_too_large")
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "Invalid request format", "invalid_request_error", "invalid_request")
		return
	}

	req, err := protocol.DecodeResponsesRequest(bodyBytes)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Invalid request format", "invalid_request_error", "invalid_request")
		return
	}
	a.logRequestDebug("responses", r, len(bodyBytes), summarizeResponsesRequest(req)...)
	a.logInfo("client.request",
		"protocol", "responses",
		"model", req.Model,
		"stream", req.Stream,
		"tool_count", len(req.Tools),
	)

	upstream, actualModel, err := a.findUpstreamForProtocol(clientKey, req.Model, config.UpstreamProtocolResponses)
	if err != nil {
		if errors.Is(err, errNoUpstreamConfigured) {
			writeError(w, http.StatusServiceUnavailable, "No upstream configured. Please configure at least one upstream in admin settings.", "service_unavailable", "upstream_not_configured")
			return
		}
		if errors.Is(err, errModelNotAccessible) {
			writeError(w, http.StatusBadRequest, "The requested model is not available for this client key.", "invalid_request_error", "model_not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error(), "server_error", "internal_error")
		return
	}

	normalizedProtocol, _ := config.NormalizeUpstreamProtocol(upstream.UpstreamProtocol)
	if normalizedProtocol == config.UpstreamProtocolResponses {
		a.handleResponsesViaNative(w, r, req, upstream, actualModel, clientKey)
	} else {
		a.handleResponsesViaChat(w, r, req, upstream, actualModel, clientKey)
	}
}

func (a *App) handleResponsesViaNative(w http.ResponseWriter, r *http.Request, req *protocol.ResponsesRequest, upstream config.UpstreamService, actualModel, clientKey string) {
	upstreamURL := upstream.BaseURL + "/responses"
	headers := a.buildUpstreamHeaders(req.Stream, clientKey, upstream)
	requestBody, softTool, err := a.prepareResponsesSoftToolRequest(req, actualModel, upstream)
	if err != nil {
		if errors.Is(err, errResponsesSoftToolsPreviousResponseID) {
			writeError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_request")
			return
		}
		writeError(w, http.StatusInternalServerError, "Invalid prompt template", "server_error", "internal_error")
		return
	}
	if softTool == nil {
		payload, err := json.Marshal(requestBody)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Internal server error", "server_error", "internal_error")
			return
		}
		if req.Stream {
			a.recordStreamRequest()
		}
		a.proxyRaw(w, r.Context(), upstreamURL, payload, headers, req.Stream)
		return
	}
	a.logUpstreamRequestDebug("responses.soft_tools", upstreamURL, requestBody, true)
	if req.Stream {
		a.recordStreamRequest()
		a.streamResponsesFromResponsesUpstream(w, r.Context(), upstreamURL, requestBody, headers, actualModel, softTool)
		return
	}
	resp, body, err := a.doJSONRequest(r.Context(), upstreamURL, requestBody, headers)
	if err != nil {
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
		writeError(w, http.StatusInternalServerError, "Internal server error", "server_error", "internal_error")
		return
	}
	a.transformResponsesResponse(payload, softTool)
	a.logJSONResponseDebug("responses", http.StatusOK, payload)
	a.logInfo("client.receive.end", "protocol", "responses", "status", http.StatusOK, "stream", false, "result", "ok")
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) handleResponsesViaChat(w http.ResponseWriter, r *http.Request, req *protocol.ResponsesRequest, upstream config.UpstreamService, actualModel, clientKey string) {
	headers := a.buildUpstreamHeaders(req.Stream, clientKey, upstream)

	adapted, adaptErr := protocol.AdaptResponsesRequestToChat(req)
	if adaptErr != nil {
		writeError(w, http.StatusUnprocessableEntity, adaptErr.Error(), "invalid_request_error", "invalid_request")
		return
	}
	requestBody, softTool, err := a.prepareChatProxyRequest(adapted, actualModel, upstream)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Invalid prompt template", "server_error", "internal_error")
		return
	}
	a.logUpstreamRequestDebug("responses.via_chat", upstream.BaseURL+"/chat/completions", requestBody, softTool != nil)

	if req.Stream {
		a.recordStreamRequest()
		a.streamResponsesFromChat(w, r, upstream.BaseURL+"/chat/completions", requestBody, headers, actualModel, softTool)
		return
	}

	resp, body, err := a.doJSONRequest(r.Context(), upstream.BaseURL+"/chat/completions", requestBody, headers)
	if err != nil {
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
		writeError(w, http.StatusInternalServerError, "Internal server error", "server_error", "internal_error")
		return
	}
	if softTool != nil {
		a.transformNonStreamResponse(payload, softTool)
	}
	responsePayload := protocol.ConvertChatCompletionToResponses(payload, actualModel)
	a.logJSONResponseDebug("responses", http.StatusOK, responsePayload)
	a.logInfo("client.receive.end", "protocol", "responses", "status", http.StatusOK, "stream", false, "result", "ok")
	writeJSON(w, http.StatusOK, responsePayload)
}

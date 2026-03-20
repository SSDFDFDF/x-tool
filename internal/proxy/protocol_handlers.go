package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"x-tool/internal/config"
	"x-tool/internal/protocol"
)

func (a *App) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	clientKey, err := a.verifyAnthropicAPIKey(r)
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

	req, err := protocol.DecodeAnthropicRequest(bodyBytes)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Invalid request format", "invalid_request_error", "invalid_request")
		return
	}
	a.logRequestDebug("anthropic.messages", r, len(bodyBytes), summarizeAnthropicRequest(req)...)
	a.logInfo("client.request",
		"protocol", "anthropic.messages",
		"model", req.Model,
		"stream", req.Stream,
		"tool_count", len(req.Tools),
	)

	upstream, actualModel, err := a.findUpstreamForProtocol(clientKey, req.Model, config.UpstreamProtocolAnthropic)
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
	if normalizedProtocol == config.UpstreamProtocolAnthropic {
		a.handleAnthropicViaNative(w, r, req, upstream, actualModel, clientKey)
	} else {
		a.handleAnthropicViaChat(w, r, req, upstream, actualModel, clientKey)
	}
}

func (a *App) handleAnthropicViaNative(w http.ResponseWriter, r *http.Request, req *protocol.AnthropicRequest, upstream config.UpstreamService, actualModel, clientKey string) {
	requestBody, softTool, err := a.prepareAnthropicSoftToolRequest(req, actualModel, upstream)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Invalid prompt template", "server_error", "internal_error")
		return
	}
	headers := a.buildAnthropicUpstreamHeaders(r, req.Stream, clientKey, upstream)
	upstreamURL := upstream.BaseURL + "/messages"
	a.logUpstreamRequestDebug("anthropic.messages", upstreamURL, requestBody, softTool != nil)

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

	if req.Stream {
		a.recordStreamRequest()
		a.streamAnthropicFromAnthropicUpstream(w, r.Context(), upstreamURL, requestBody, headers, actualModel, softTool)
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
	a.transformAnthropicResponse(payload, softTool)
	a.logJSONResponseDebug("anthropic.messages", http.StatusOK, payload)
	a.logInfo("client.receive.end", "protocol", "anthropic.messages", "status", http.StatusOK, "stream", false, "result", "ok")
	writeJSON(w, http.StatusOK, payload)
}

func (a *App) handleAnthropicViaChat(w http.ResponseWriter, r *http.Request, req *protocol.AnthropicRequest, upstream config.UpstreamService, actualModel, clientKey string) {
	adapted, err := protocol.AdaptAnthropicRequestToChat(req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), "invalid_request_error", "invalid_request")
		return
	}
	requestBody, softTool, err := a.prepareChatProxyRequest(adapted, actualModel, upstream)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Invalid prompt template", "server_error", "internal_error")
		return
	}
	headers := a.buildUpstreamHeaders(req.Stream, clientKey, upstream)
	a.logUpstreamRequestDebug("anthropic.via_chat", upstream.BaseURL+"/chat/completions", requestBody, softTool != nil)

	if req.Stream {
		a.recordStreamRequest()
		a.streamAnthropicFromChat(w, r, upstream.BaseURL+"/chat/completions", requestBody, headers, actualModel, softTool)
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
	responsePayload := protocol.ConvertChatCompletionToAnthropic(payload, actualModel)
	a.logJSONResponseDebug("anthropic.messages", http.StatusOK, responsePayload)
	a.logInfo("client.receive.end", "protocol", "anthropic.messages", "status", http.StatusOK, "stream", false, "result", "ok")
	writeJSON(w, http.StatusOK, responsePayload)
}

func (a *App) streamResponsesFromChat(w http.ResponseWriter, r *http.Request, upstreamURL string, requestBody map[string]any, headers map[string]string, model string, softTool *softToolCallSettings) {
	a.streamResponsesWithCallbacks(w, model, func(callbacks chatStreamCallbacks) error {
		return a.streamChatCompletion(r.Context(), upstreamURL, requestBody, headers, model, softTool, callbacks)
	})
}

func (a *App) streamResponsesWithCallbacks(w http.ResponseWriter, model string, run func(chatStreamCallbacks) error) {
	writeSSEHeaders(w)
	responseID := newID("resp_")
	messageID := newID("msg_")
	reasoningID := newID("rs_")
	thinkingStarted := false
	thinkingBuffer := strings.Builder{}
	thinkingOutputIdx := -1
	textStarted := false
	textBuffer := strings.Builder{}
	textOutputIdx := -1
	nextOutputIdx := 0

	_ = a.writeLoggedSSE(w, "responses", "", map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id":         responseID,
			"object":     "response",
			"created_at": nowUnix(),
			"status":     "in_progress",
			"model":      model,
			"output":     []any{},
		},
	})

	_ = run(chatStreamCallbacks{
		OnThinking: func(delta string) error {
			if !thinkingStarted {
				thinkingStarted = true
				thinkingOutputIdx = nextOutputIdx
				nextOutputIdx++
				if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
					"type":         "response.output_item.added",
					"output_index": thinkingOutputIdx,
					"item": map[string]any{
						"id":      reasoningID,
						"type":    "reasoning",
						"summary": []any{},
					},
				}); err != nil {
					return err
				}
				if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
					"type":          "response.reasoning_summary_part.added",
					"output_index":  thinkingOutputIdx,
					"summary_index": 0,
					"part": map[string]any{
						"type": "summary_text",
						"text": "",
					},
				}); err != nil {
					return err
				}
			}
			thinkingBuffer.WriteString(delta)
			return a.writeLoggedSSE(w, "responses", "", map[string]any{
				"type":          "response.reasoning_summary_text.delta",
				"output_index":  thinkingOutputIdx,
				"summary_index": 0,
				"delta":         delta,
			})
		},
		OnText: func(delta string) error {
			if !textStarted {
				textStarted = true
				textOutputIdx = nextOutputIdx
				nextOutputIdx++
				if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
					"type":         "response.output_item.added",
					"output_index": textOutputIdx,
					"item": map[string]any{
						"id":     messageID,
						"type":   "message",
						"status": "in_progress",
						"role":   "assistant",
						"content": []map[string]any{{
							"type": "output_text",
							"text": "",
						}},
					},
				}); err != nil {
					return err
				}
				if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
					"type":          "response.content_part.added",
					"output_index":  textOutputIdx,
					"content_index": 0,
					"part": map[string]any{
						"type": "output_text",
						"text": "",
					},
				}); err != nil {
					return err
				}
			}
			textBuffer.WriteString(delta)
			return a.writeLoggedSSE(w, "responses", "", map[string]any{
				"type":          "response.output_text.delta",
				"output_index":  textOutputIdx,
				"content_index": 0,
				"delta":         delta,
			})
		},
		OnToolCalls: func(toolCalls []map[string]any) error {
			startIndex := nextOutputIdx
			for i, toolCall := range toolCalls {
				functionInfo, _ := toolCall["function"].(map[string]any)
				arguments, _ := functionInfo["arguments"].(string)
				name, _ := functionInfo["name"].(string)
				if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
					"type":         "response.output_item.added",
					"output_index": startIndex + i,
					"item": map[string]any{
						"id":        newID("fc_"),
						"type":      "function_call",
						"status":    "completed",
						"call_id":   toolCall["id"],
						"name":      name,
						"arguments": "",
					},
				}); err != nil {
					return err
				}
				if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
					"type":         "response.function_call_arguments.delta",
					"output_index": startIndex + i,
					"delta":        arguments,
				}); err != nil {
					return err
				}
				if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
					"type":         "response.function_call_arguments.done",
					"output_index": startIndex + i,
					"arguments":    arguments,
				}); err != nil {
					return err
				}
				if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
					"type":         "response.output_item.done",
					"output_index": startIndex + i,
					"item": map[string]any{
						"type": "function_call",
					},
				}); err != nil {
					return err
				}
			}
			return nil
		},
		OnError: func(message string) error {
			return a.writeLoggedSSE(w, "responses", "", map[string]any{
				"type": "response.failed",
				"error": map[string]any{
					"message": message,
					"type":    "server_error",
				},
			})
		},
		OnDone: func() error {
			if thinkingStarted {
				if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
					"type":          "response.reasoning_summary_text.done",
					"output_index":  thinkingOutputIdx,
					"summary_index": 0,
					"text":          thinkingBuffer.String(),
				}); err != nil {
					return err
				}
				if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
					"type":          "response.reasoning_summary_part.done",
					"output_index":  thinkingOutputIdx,
					"summary_index": 0,
					"part": map[string]any{
						"type": "summary_text",
						"text": thinkingBuffer.String(),
					},
				}); err != nil {
					return err
				}
				if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
					"type":         "response.output_item.done",
					"output_index": thinkingOutputIdx,
					"item": map[string]any{
						"id":   reasoningID,
						"type": "reasoning",
						"summary": []map[string]any{{
							"type": "summary_text",
							"text": thinkingBuffer.String(),
						}},
					},
				}); err != nil {
					return err
				}
			}
			if textStarted {
				if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
					"type":          "response.output_text.done",
					"output_index":  textOutputIdx,
					"content_index": 0,
					"text":          textBuffer.String(),
				}); err != nil {
					return err
				}
				if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
					"type":          "response.content_part.done",
					"output_index":  textOutputIdx,
					"content_index": 0,
					"part": map[string]any{
						"type": "output_text",
						"text": textBuffer.String(),
					},
				}); err != nil {
					return err
				}
				if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
					"type":         "response.output_item.done",
					"output_index": textOutputIdx,
					"item": map[string]any{
						"id":   messageID,
						"type": "message",
					},
				}); err != nil {
					return err
				}
			}
			if err := a.writeLoggedSSE(w, "responses", "", map[string]any{
				"type": "response.completed",
				"response": map[string]any{
					"id":          responseID,
					"object":      "response",
					"created_at":  nowUnix(),
					"status":      "completed",
					"model":       model,
					"output_text": textBuffer.String(),
				},
			}); err != nil {
				return err
			}
			return a.writeLoggedDone(w, "responses")
		},
	})
}

func (a *App) streamAnthropicFromChat(w http.ResponseWriter, r *http.Request, upstreamURL string, requestBody map[string]any, headers map[string]string, model string, softTool *softToolCallSettings) {
	a.streamAnthropicWithCallbacks(w, model, func(callbacks chatStreamCallbacks) error {
		return a.streamChatCompletion(r.Context(), upstreamURL, requestBody, headers, model, softTool, callbacks)
	})
}

func (a *App) streamAnthropicWithCallbacks(w http.ResponseWriter, model string, run func(chatStreamCallbacks) error) {
	writeSSEHeaders(w)
	messageID := newID("msg_")
	thinkingIndex := -1
	textIndex := -1
	nextIndex := 0
	stopReason := "end_turn"

	_ = a.writeLoggedSSE(w, "anthropic.messages", "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            messageID,
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  0,
				"output_tokens": 0,
			},
		},
	})

	_ = run(chatStreamCallbacks{
		OnThinking: func(delta string) error {
			if thinkingIndex == -1 {
				thinkingIndex = nextIndex
				nextIndex++
				if err := a.writeLoggedSSE(w, "anthropic.messages", "content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": thinkingIndex,
					"content_block": map[string]any{
						"type":     "thinking",
						"thinking": "",
					},
				}); err != nil {
					return err
				}
			}
			return a.writeLoggedSSE(w, "anthropic.messages", "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": thinkingIndex,
				"delta": map[string]any{
					"type":     "thinking_delta",
					"thinking": delta,
				},
			})
		},
		OnText: func(delta string) error {
			if thinkingIndex >= 0 {
				if err := a.writeLoggedSSE(w, "anthropic.messages", "content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": thinkingIndex,
				}); err != nil {
					return err
				}
				thinkingIndex = -2
			}
			if textIndex == -1 {
				textIndex = nextIndex
				nextIndex++
				if err := a.writeLoggedSSE(w, "anthropic.messages", "content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": textIndex,
					"content_block": map[string]any{
						"type": "text",
						"text": "",
					},
				}); err != nil {
					return err
				}
			}
			return a.writeLoggedSSE(w, "anthropic.messages", "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": textIndex,
				"delta": map[string]any{
					"type": "text_delta",
					"text": delta,
				},
			})
		},
		OnToolCalls: func(toolCalls []map[string]any) error {
			if thinkingIndex >= 0 {
				if err := a.writeLoggedSSE(w, "anthropic.messages", "content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": thinkingIndex,
				}); err != nil {
					return err
				}
				thinkingIndex = -2
			}
			if textIndex != -1 {
				if err := a.writeLoggedSSE(w, "anthropic.messages", "content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": textIndex,
				}); err != nil {
					return err
				}
				textIndex = -1
			}
			stopReason = "tool_use"
			for _, toolCall := range toolCalls {
				idx := nextIndex
				nextIndex++
				functionInfo, _ := toolCall["function"].(map[string]any)
				name, _ := functionInfo["name"].(string)
				arguments, _ := functionInfo["arguments"].(string)
				if err := a.writeLoggedSSE(w, "anthropic.messages", "content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": idx,
					"content_block": map[string]any{
						"type":  "tool_use",
						"id":    toolCall["id"],
						"name":  name,
						"input": map[string]any{},
					},
				}); err != nil {
					return err
				}
				if err := a.writeLoggedSSE(w, "anthropic.messages", "content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": idx,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": arguments,
					},
				}); err != nil {
					return err
				}
				if err := a.writeLoggedSSE(w, "anthropic.messages", "content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": idx,
				}); err != nil {
					return err
				}
			}
			return nil
		},
		OnError: func(message string) error {
			return a.writeLoggedSSE(w, "anthropic.messages", "error", map[string]any{
				"type": "error",
				"error": map[string]any{
					"type":    "api_error",
					"message": message,
				},
			})
		},
		OnDone: func() error {
			if thinkingIndex >= 0 {
				if err := a.writeLoggedSSE(w, "anthropic.messages", "content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": thinkingIndex,
				}); err != nil {
					return err
				}
			}
			if textIndex != -1 {
				if err := a.writeLoggedSSE(w, "anthropic.messages", "content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": textIndex,
				}); err != nil {
					return err
				}
			}
			if err := a.writeLoggedSSE(w, "anthropic.messages", "message_delta", map[string]any{
				"type": "message_delta",
				"delta": map[string]any{
					"stop_reason":   stopReason,
					"stop_sequence": nil,
				},
				"usage": map[string]any{
					"output_tokens": 0,
				},
			}); err != nil {
				return err
			}
			if err := a.writeLoggedSSE(w, "anthropic.messages", "message_stop", map[string]any{
				"type": "message_stop",
			}); err != nil {
				return err
			}
			a.logInfo("client.receive.end", "protocol", "anthropic.messages", "stream", true, "result", "message_stop")
			return nil
		},
	})
}

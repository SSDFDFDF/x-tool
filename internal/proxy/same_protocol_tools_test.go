package proxy

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"x-tool/internal/config"
	"x-tool/internal/protocol"
)

func TestFindUpstreamForProtocolPrefersMatchingProtocol(t *testing.T) {
	cfg := testConfig()
	cfg.UpstreamServices = []config.UpstreamService{
		{
			Name:             "openai",
			BaseURL:          "https://openai.example.com/v1",
			APIKey:           "openai-key",
			IsDefault:        true,
			Models:           []string{"gpt-4o"},
			ClientKeys:       []string{"client-key"},
			UpstreamProtocol: config.UpstreamProtocolOpenAICompat,
		},
		{
			Name:             "anthropic",
			BaseURL:          "https://anthropic.example.com/v1",
			APIKey:           "anthropic-key",
			Models:           []string{"gpt-4o"},
			ClientKeys:       []string{"client-key"},
			UpstreamProtocol: config.UpstreamProtocolAnthropic,
		},
	}

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	// Anthropic protocol should find both anthropic and openai_compat services
	upstream, _, err := app.findUpstreamForProtocol("client-key", "gpt-4o", config.UpstreamProtocolAnthropic)
	if err != nil {
		t.Fatalf("expected upstream, got error: %v", err)
	}
	// Both match; default service (openai) wins the tie
	if upstream.UpstreamProtocol != config.UpstreamProtocolOpenAICompat && upstream.UpstreamProtocol != config.UpstreamProtocolAnthropic {
		t.Fatalf("expected openai_compat or anthropic upstream, got %q", upstream.UpstreamProtocol)
	}
}

func TestFindUpstreamFallsBackToOpenAICompat(t *testing.T) {
	cfg := testConfig()
	cfg.UpstreamServices = []config.UpstreamService{
		{
			Name:             "openai",
			BaseURL:          "https://openai.example.com/v1",
			APIKey:           "openai-key",
			IsDefault:        true,
			Models:           []string{"gpt-4o"},
			ClientKeys:       []string{"client-key"},
			UpstreamProtocol: config.UpstreamProtocolOpenAICompat,
		},
	}

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	// Anthropic protocol should find openai_compat
	upstream, _, err := app.findUpstreamForProtocol("client-key", "gpt-4o", config.UpstreamProtocolAnthropic)
	if err != nil {
		t.Fatalf("expected openai_compat fallback for anthropic, got error: %v", err)
	}
	if upstream.Name != "openai" {
		t.Fatalf("expected openai upstream, got %q", upstream.Name)
	}

	// Responses protocol should find openai_compat
	upstream, _, err = app.findUpstreamForProtocol("client-key", "gpt-4o", config.UpstreamProtocolResponses)
	if err != nil {
		t.Fatalf("expected openai_compat fallback for responses, got error: %v", err)
	}
	if upstream.Name != "openai" {
		t.Fatalf("expected openai upstream, got %q", upstream.Name)
	}
}

func TestFindUpstreamRejectsProtocolMismatch(t *testing.T) {
	cfg := testConfig()
	cfg.UpstreamServices = []config.UpstreamService{
		{
			Name:             "anthropic-only",
			BaseURL:          "https://anthropic.example.com/v1",
			APIKey:           "anthropic-key",
			IsDefault:        true,
			Models:           []string{"claude-3"},
			ClientKeys:       []string{"client-key"},
			UpstreamProtocol: config.UpstreamProtocolAnthropic,
		},
	}

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	// Responses protocol should NOT find anthropic upstream
	_, _, err = app.findUpstreamForProtocol("client-key", "claude-3", config.UpstreamProtocolResponses)
	if err == nil {
		t.Fatalf("expected error for protocol mismatch, got nil")
	}

	// OpenAI protocol should NOT find anthropic upstream
	_, _, err = app.findUpstreamForProtocol("client-key", "claude-3", config.UpstreamProtocolOpenAICompat)
	if err == nil {
		t.Fatalf("expected error for protocol mismatch, got nil")
	}
}

func TestServicesForClientKeyAndProtocolFiltering(t *testing.T) {
	cfg := testConfig()
	cfg.UpstreamServices = []config.UpstreamService{
		{
			Name:             "openai",
			BaseURL:          "https://openai.example.com/v1",
			APIKey:           "openai-key",
			IsDefault:        true,
			Models:           []string{"gpt-4o"},
			ClientKeys:       []string{"client-key"},
			UpstreamProtocol: config.UpstreamProtocolOpenAICompat,
		},
		{
			Name:             "anthropic",
			BaseURL:          "https://anthropic.example.com/v1",
			APIKey:           "anthropic-key",
			Models:           []string{"gpt-4o"},
			ClientKeys:       []string{"client-key"},
			UpstreamProtocol: config.UpstreamProtocolAnthropic,
		},
	}

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	// Anthropic protocol: anthropic + openai_compat
	services := app.servicesForClientKeyAndProtocol("client-key", config.UpstreamProtocolAnthropic)
	if len(services) != 2 {
		t.Fatalf("expected 2 services (openai_compat + anthropic), got %d", len(services))
	}

	// OpenAI protocol: only openai_compat (not anthropic)
	services = app.servicesForClientKeyAndProtocol("client-key", config.UpstreamProtocolOpenAICompat)
	if len(services) != 1 {
		t.Fatalf("expected 1 service for openai_compat, got %d", len(services))
	}
	if services[0].Name != "openai" {
		t.Fatalf("expected openai only, got %q", services[0].Name)
	}
}

func TestPrepareResponsesSoftToolRequestInjectsPromptAndRewritesFunctionCallOutput(t *testing.T) {
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.trigger = "<Function_Test_Start>"

	req := &protocol.ResponsesRequest{
		Raw: map[string]any{
			"model": "gpt-4o",
			"input": []any{
				map[string]any{
					"type":    "function_call_output",
					"call_id": "call_123",
					"output":  "tool result",
				},
			},
			"tools": []any{
				map[string]any{
					"type": "function",
					"name": "search",
				},
			},
			"tool_choice": "required",
		},
		Model: "gpt-4o",
		Input: []any{
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_123",
				"output":  "tool result",
			},
		},
		Tools: []protocol.ResponseTool{{
			Type: "function",
			Name: "search",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
				"required": []any{"query"},
			},
		}},
		ToolChoice: "required",
	}

	requestBody, softTool, err := app.prepareResponsesSoftToolRequest(req, "gpt-4o-mini", cfgUpstream(config.UpstreamProtocolResponses))
	if err != nil {
		t.Fatalf("prepare request: %v", err)
	}
	if softTool == nil {
		t.Fatalf("expected soft tool settings")
	}
	if requestBody["model"] != "gpt-4o-mini" {
		t.Fatalf("expected actual model override, got %#v", requestBody["model"])
	}
	if _, ok := requestBody["tools"]; ok {
		t.Fatalf("expected tools to be removed from upstream payload")
	}
	instructions, _ := requestBody["instructions"].(string)
	if !strings.Contains(instructions, "Tool choice: required") {
		t.Fatalf("expected injected instructions, got %q", instructions)
	}
	input, _ := requestBody["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("expected rewritten input, got %#v", input)
	}
	message, _ := input[0].(map[string]any)
	if message["role"] != "user" {
		t.Fatalf("expected function call output to become user message, got %#v", message)
	}
}

func TestPrepareResponsesSoftToolRequestInjectsPromptIntoMessageTarget(t *testing.T) {
	cfg := testConfig()
	cfg.Features.PromptInjectionTarget = config.PromptInjectionTargetMessage
	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.trigger = "<Function_Test_Start>"

	req := &protocol.ResponsesRequest{
		Raw: map[string]any{
			"model": "gpt-4o",
			"input": "ping",
		},
		Model: "gpt-4o",
		Input: "ping",
		Tools: []protocol.ResponseTool{{
			Type:       "function",
			Name:       "search",
			Parameters: map[string]any{"type": "object"},
		}},
	}

	requestBody, _, err := app.prepareResponsesSoftToolRequest(req, "gpt-4o-mini", cfgUpstream(config.UpstreamProtocolResponses))
	if err != nil {
		t.Fatalf("prepare request: %v", err)
	}
	if _, ok := requestBody["instructions"]; ok {
		t.Fatalf("did not expect instructions injection for message target")
	}
	input, _ := requestBody["input"].([]any)
	if len(input) < 2 {
		t.Fatalf("expected injected prompt message and original input, got %#v", input)
	}
	first, _ := input[0].(map[string]any)
	if first["role"] != "developer" {
		t.Fatalf("expected injected prompt role developer, got %#v", first["role"])
	}
}

func TestPrepareResponsesSoftToolRequestRejectsPreviousResponseID(t *testing.T) {
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	req := &protocol.ResponsesRequest{
		Raw: map[string]any{
			"model":                "gpt-4o",
			"previous_response_id": "resp_123",
		},
		Model: "gpt-4o",
		Tools: []protocol.ResponseTool{{
			Type:       "function",
			Name:       "search",
			Parameters: map[string]any{"type": "object"},
		}},
	}

	_, _, err = app.prepareResponsesSoftToolRequest(req, "gpt-4o", cfgUpstream(config.UpstreamProtocolResponses))
	if err == nil || err != errResponsesSoftToolsPreviousResponseID {
		t.Fatalf("expected previous_response_id soft tool error, got %v", err)
	}
}

func TestTransformResponsesResponseConvertsSoftToolTextToFunctionCall(t *testing.T) {
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.trigger = "<Function_Test_Start>"

	softTool := &softToolCallSettings{
		Protocol: config.SoftToolProtocolXML,
		Trigger:  app.trigger,
		Tools: []protocol.Tool{{
			Type: "function",
			Function: protocol.ToolFunction{
				Name:       "search",
				Parameters: map[string]any{"type": "object"},
			},
		}},
		ToolChoice: "required",
	}
	payload := map[string]any{
		"output": []any{
			map[string]any{
				"type": "message",
				"content": []any{
					map[string]any{
						"type": "output_text",
						"text": "prefix\n<Function_Test_Start>\n<invoke name=\"search\"><parameter name=\"query\">weather</parameter></invoke>",
					},
				},
			},
		},
	}

	app.transformResponsesResponse(payload, softTool)

	output, _ := payload["output"].([]any)
	if len(output) != 2 {
		t.Fatalf("expected message + function_call outputs, got %#v", output)
	}
	second, _ := output[1].(map[string]any)
	if second["type"] != "function_call" {
		t.Fatalf("expected function_call output item, got %#v", second)
	}
}

func TestTransformResponsesResponseConvertsMarkdownBlockSoftToolTextToFunctionCall(t *testing.T) {
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.trigger = "<Function_Test_Start>"

	softTool := &softToolCallSettings{
		Protocol: config.SoftToolProtocolMarkdownBlock,
		Trigger:  app.trigger,
		Tools: []protocol.Tool{{
			Type: "function",
			Function: protocol.ToolFunction{
				Name: "search",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string"},
					},
					"required": []any{"query"},
				},
			},
		}},
		ToolChoice: "required",
	}
	payload := map[string]any{
		"output": []any{
			map[string]any{
				"type": "message",
				"content": []any{
					map[string]any{
						"type": "output_text",
						"text": "prefix\n<Function_Test_Start>\n```toolcalls\ncall search\narg_query: weather\n```",
					},
				},
			},
		},
	}

	app.transformResponsesResponse(payload, softTool)

	output, _ := payload["output"].([]any)
	if len(output) != 2 {
		t.Fatalf("expected message + function_call outputs, got %#v", output)
	}
	second, _ := output[1].(map[string]any)
	if second["type"] != "function_call" || second["name"] != "search" {
		t.Fatalf("expected markdown block to convert into search function_call, got %#v", second)
	}
}

func TestPrepareAnthropicSoftToolRequestInjectsSystemAndRewritesToolMessages(t *testing.T) {
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.trigger = "<Function_Test_Start>"

	req := &protocol.AnthropicRequest{
		Raw: map[string]any{
			"model": "claude-test",
			"messages": []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{
							"type":        "tool_result",
							"tool_use_id": "call_123",
							"content":     "done",
						},
					},
				},
			},
			"tools": []any{
				map[string]any{"name": "search"},
			},
		},
		Model:  "claude-test",
		System: "Follow the rules",
		Messages: []map[string]any{{
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "tool_use", "id": "call_123", "name": "search", "input": map[string]any{"query": "weather"}},
			},
		}, {
			"role": "user",
			"content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "call_123", "content": "done"},
			},
		}},
		Tools: []protocol.AnthropicTool{{
			Name:        "search",
			InputSchema: map[string]any{"type": "object"},
		}},
		ToolChoice: map[string]any{"type": "any"},
	}

	requestBody, softTool, err := app.prepareAnthropicSoftToolRequest(req, "claude-sonnet", cfgUpstream(config.UpstreamProtocolAnthropic))
	if err != nil {
		t.Fatalf("prepare request: %v", err)
	}
	if softTool == nil {
		t.Fatalf("expected soft tool settings")
	}
	systemText, _ := requestBody["system"].(string)
	if !strings.Contains(systemText, "Follow the rules") || !strings.Contains(systemText, app.trigger) {
		t.Fatalf("expected prompt-injected system text, got %q", systemText)
	}
	messages, _ := requestBody["messages"].([]map[string]any)
	if len(messages) != 2 {
		t.Fatalf("expected rewritten messages, got %#v", requestBody["messages"])
	}
	userContent, _ := messages[1]["content"].([]any)
	firstPart, _ := userContent[0].(map[string]any)
	if firstPart["type"] != "text" {
		t.Fatalf("expected tool_result to become text part, got %#v", firstPart)
	}
}

func TestResolveSoftToolPromptConfigPrefersUpstreamBoundProfile(t *testing.T) {
	cfg := testConfig()
	cfg.Features.SoftToolProtocol = config.SoftToolProtocolXML
	cfg.Features.PromptTemplate = "LEGACY_TEMPLATE\n{tool_catalog}\n{protocol_rules}"
	cfg.Features.DefaultSoftToolPromptProfileID = "global-profile"
	cfg.SoftToolPromptProfiles = []config.SoftToolPromptProfile{
		{
			ID:       "global-profile",
			Name:     "Global Profile",
			Protocol: config.SoftToolProtocolSentinelJSON,
			Template: "GLOBAL_PROFILE\n{tool_catalog}\n{single_call_example}",
			Enabled:  true,
		},
		{
			ID:       "upstream-profile",
			Name:     "Upstream Profile",
			Protocol: config.SoftToolProtocolMarkdownBlock,
			Template: "UPSTREAM_PROFILE\n{tool_catalog}\n{single_call_example}",
			Enabled:  true,
		},
	}

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	resolved := app.resolveSoftToolPromptConfig(config.UpstreamService{
		SoftToolPromptProfileID: "upstream-profile",
		SoftToolProtocol:        config.SoftToolProtocolXML,
	})

	if resolved.ProfileID != "upstream-profile" {
		t.Fatalf("expected upstream profile id, got %#v", resolved)
	}
	if resolved.Protocol != config.SoftToolProtocolMarkdownBlock {
		t.Fatalf("expected upstream profile protocol to win, got %#v", resolved)
	}
	if !strings.Contains(resolved.Template, "UPSTREAM_PROFILE") {
		t.Fatalf("expected upstream profile template to win, got %#v", resolved)
	}
}

func TestResolveSoftToolPromptConfigFallsBackToLegacyTemplate(t *testing.T) {
	cfg := testConfig()
	cfg.Features.SoftToolProtocol = config.SoftToolProtocolSentinelJSON
	cfg.Features.PromptTemplate = "LEGACY_TEMPLATE\n{tool_catalog}\n{protocol_rules}"
	cfg.Features.DefaultSoftToolPromptProfileID = "global-profile"
	cfg.SoftToolPromptProfiles = []config.SoftToolPromptProfile{{
		ID:       "global-profile",
		Name:     "Global Profile",
		Protocol: "",
		Template: "",
		Enabled:  true,
	}}

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	resolved := app.resolveSoftToolPromptConfig(cfgUpstream(config.UpstreamProtocolResponses))
	if resolved.ProfileID != "global-profile" {
		t.Fatalf("expected global default profile to resolve, got %#v", resolved)
	}
	if resolved.Protocol != config.SoftToolProtocolSentinelJSON {
		t.Fatalf("expected feature protocol fallback, got %#v", resolved)
	}
	if !strings.Contains(resolved.Template, "LEGACY_TEMPLATE") {
		t.Fatalf("expected legacy template fallback, got %#v", resolved)
	}
}

func TestPrepareChatProxyRequestUsesBoundPromptProfileTemplate(t *testing.T) {
	cfg := testConfig()
	cfg.Features.PromptTemplate = "LEGACY_TEMPLATE\n{tool_catalog}\n{protocol_rules}"
	cfg.SoftToolPromptProfiles = []config.SoftToolPromptProfile{{
		ID:       "upstream-profile",
		Name:     "Upstream Profile",
		Protocol: config.SoftToolProtocolMarkdownBlock,
		Template: "PROFILE_TEMPLATE\n{tool_catalog}\n{single_call_example}",
		Enabled:  true,
	}}

	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.trigger = "<Function_Test_Start>"

	description := "Search"
	req := &protocol.ChatCompletionRequest{
		Raw: map[string]any{
			"model": "gpt-4o",
			"messages": []any{
				map[string]any{"role": "user", "content": "ping"},
			},
		},
		Model: "gpt-4o",
		Messages: []map[string]any{{
			"role":    "user",
			"content": "ping",
		}},
		Tools: []protocol.Tool{{
			Type: "function",
			Function: protocol.ToolFunction{
				Name:        "search",
				Description: &description,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string"},
					},
				},
			},
		}},
	}

	requestBody, softTool, err := app.prepareChatProxyRequest(req, "gpt-4o-mini", config.UpstreamService{
		Name:                    "upstream",
		BaseURL:                 "https://example.com/v1",
		APIKey:                  "upstream-key",
		IsDefault:               true,
		Models:                  []string{"gpt-4o"},
		ClientKeys:              []string{"client-key"},
		SoftToolPromptProfileID: "upstream-profile",
	})
	if err != nil {
		t.Fatalf("prepare request: %v", err)
	}
	if softTool == nil {
		t.Fatalf("expected soft tool settings")
	}
	if softTool.Protocol != config.SoftToolProtocolMarkdownBlock {
		t.Fatalf("expected profile protocol to be applied, got %#v", softTool)
	}
	messages, _ := requestBody["messages"].([]map[string]any)
	if len(messages) == 0 {
		t.Fatalf("expected injected prompt message, got %#v", requestBody["messages"])
	}
	content, _ := messages[0]["content"].(string)
	if !strings.Contains(content, "PROFILE_TEMPLATE") {
		t.Fatalf("expected profile template to be injected, got %q", content)
	}
	if strings.Contains(content, "LEGACY_TEMPLATE") {
		t.Fatalf("expected legacy template to be bypassed when profile template is set, got %q", content)
	}
}

func TestResolvePromptInjectionAutoDefaultsByProtocol(t *testing.T) {
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	if got := app.resolvePromptInjection(cfgUpstream(config.UpstreamProtocolOpenAICompat)); got.Target != config.PromptInjectionTargetMessage || got.Role != "system" {
		t.Fatalf("expected openai auto target to resolve to message/system, got %#v", got)
	}
	if got := app.resolvePromptInjection(cfgUpstream(config.UpstreamProtocolResponses)); got.Target != config.PromptInjectionTargetInstructions {
		t.Fatalf("expected responses auto target to resolve to instructions, got %#v", got)
	}
	if got := app.resolvePromptInjection(cfgUpstream(config.UpstreamProtocolAnthropic)); got.Target != config.PromptInjectionTargetSystem {
		t.Fatalf("expected anthropic auto target to resolve to system, got %#v", got)
	}
}

func TestResolvePromptInjectionResponsesExplicitUserRoleUsesMessage(t *testing.T) {
	cfg := testConfig()
	cfg.Features.PromptInjectionRole = "user"
	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	got := app.resolvePromptInjection(cfgUpstream(config.UpstreamProtocolResponses))
	if got.Target != config.PromptInjectionTargetMessage || got.Role != "user" {
		t.Fatalf("expected responses explicit user role to resolve to message/user, got %#v", got)
	}
}

func TestResolvePromptInjectionAnthropicExplicitAssistantRoleUsesMessage(t *testing.T) {
	cfg := testConfig()
	cfg.Features.PromptInjectionRole = "assistant"
	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	got := app.resolvePromptInjection(cfgUpstream(config.UpstreamProtocolAnthropic))
	if got.Target != config.PromptInjectionTargetMessage || got.Role != "assistant" {
		t.Fatalf("expected anthropic explicit assistant role to resolve to message/assistant, got %#v", got)
	}
}

func TestResolvePromptInjectionAnthropicMessageTargetRejectsSystemRole(t *testing.T) {
	cfg := testConfig()
	cfg.Features.PromptInjectionTarget = config.PromptInjectionTargetMessage
	cfg.Features.PromptInjectionRole = "system"
	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	got := app.resolvePromptInjection(cfgUpstream(config.UpstreamProtocolAnthropic))
	if got.Target != config.PromptInjectionTargetSystem {
		t.Fatalf("expected anthropic message target with system role to fall back to system target, got %#v", got)
	}
}

func TestPrepareAnthropicSoftToolRequestInjectsPromptIntoMessageTarget(t *testing.T) {
	cfg := testConfig()
	cfg.Features.PromptInjectionTarget = config.PromptInjectionTargetMessage
	cfg.Features.PromptInjectionRole = "assistant"
	app, err := NewApp(cfg, nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.trigger = "<Function_Test_Start>"

	req := &protocol.AnthropicRequest{
		Raw: map[string]any{
			"model":  "claude-test",
			"system": "Follow the rules",
			"messages": []any{
				map[string]any{
					"role":    "user",
					"content": "ping",
				},
			},
			"tools": []any{
				map[string]any{"name": "search"},
			},
		},
		Model:  "claude-test",
		System: "Follow the rules",
		Messages: []map[string]any{{
			"role":    "user",
			"content": "ping",
		}},
		Tools: []protocol.AnthropicTool{{
			Name:        "search",
			InputSchema: map[string]any{"type": "object"},
		}},
	}

	requestBody, softTool, err := app.prepareAnthropicSoftToolRequest(req, "claude-sonnet", cfgUpstream(config.UpstreamProtocolAnthropic))
	if err != nil {
		t.Fatalf("prepare request: %v", err)
	}
	if softTool == nil {
		t.Fatalf("expected soft tool settings")
	}
	if got, _ := requestBody["system"].(string); got != "Follow the rules" {
		t.Fatalf("expected original system prompt to be preserved, got %q", got)
	}
	messages, _ := requestBody["messages"].([]map[string]any)
	if len(messages) != 2 {
		t.Fatalf("expected injected prompt message and original message, got %#v", requestBody["messages"])
	}
	if messages[0]["role"] != "assistant" {
		t.Fatalf("expected injected prompt role assistant, got %#v", messages[0]["role"])
	}
	content, _ := messages[0]["content"].(string)
	if !strings.Contains(content, app.trigger) {
		t.Fatalf("expected injected anthropic prompt content, got %#v", messages[0]["content"])
	}
}

func TestTransformAnthropicResponseConvertsSoftToolTextToToolUse(t *testing.T) {
	app, err := NewApp(testConfig(), nil, nil, slog.Default(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}
	app.trigger = "<Function_Test_Start>"

	softTool := &softToolCallSettings{
		Protocol: config.SoftToolProtocolXML,
		Trigger:  app.trigger,
		Tools: []protocol.Tool{{
			Type: "function",
			Function: protocol.ToolFunction{
				Name:       "search",
				Parameters: map[string]any{"type": "object"},
			},
		}},
		ToolChoice: "required",
	}
	payload := map[string]any{
		"content": []any{
			map[string]any{
				"type": "text",
				"text": "prefix\n<Function_Test_Start>\n<invoke name=\"search\"><parameter name=\"query\">weather</parameter></invoke>",
			},
		},
		"stop_reason": "end_turn",
	}

	app.transformAnthropicResponse(payload, softTool)

	content, _ := payload["content"].([]any)
	if len(content) != 2 {
		encoded, _ := json.Marshal(payload)
		t.Fatalf("expected text + tool_use blocks, got %s", encoded)
	}
	second, _ := content[1].(map[string]any)
	if second["type"] != "tool_use" {
		t.Fatalf("expected tool_use block, got %#v", second)
	}
	if payload["stop_reason"] != "tool_use" {
		t.Fatalf("expected tool_use stop reason, got %#v", payload["stop_reason"])
	}
}

func cfgUpstream(protocolName string) config.UpstreamService {
	return config.UpstreamService{
		Name:             "upstream",
		BaseURL:          "https://example.com/v1",
		APIKey:           "upstream-key",
		IsDefault:        true,
		Models:           []string{"model"},
		ClientKeys:       []string{"client-key"},
		UpstreamProtocol: protocolName,
	}
}

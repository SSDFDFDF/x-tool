package proxy

import (
	"html"
	"strings"
	"testing"

	"x-tool/internal/config"
	"x-tool/internal/protocol"
)

func TestGenerateFunctionPromptPreservesCompactMetadataForComplexTool(t *testing.T) {
	description := "Search the web"
	tools := []protocol.Tool{{
		Type: "function",
		Function: protocol.ToolFunction{
			Name:        "search_web",
			Description: &description,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"filters": map[string]any{
						"type":        "object",
						"description": "advanced filters",
						"properties": map[string]any{
							"days": map[string]any{
								"type":    "integer",
								"minimum": 1,
								"maximum": 7,
							},
							"tags": map[string]any{
								"type": "array",
								"items": map[string]any{
									"type": "string",
									"enum": []any{"rain", "aqi", "wind"},
								},
							},
						},
						"required": []any{"days"},
					},
					"locale": map[string]any{
						"type":        "string",
						"description": "optional locale",
						"default":     "en-US",
						"enum":        []any{"en-US", "zh-CN"},
					},
					"query": map[string]any{
						"type":        "string",
						"description": "search query",
						"examples":    []any{"weather"},
						"minLength":   1,
					},
				},
				"required": []any{"query"},
			},
		},
	}}

	prompt, err := GenerateFunctionPrompt(tools, "<Function_Test_Start>", "{tool_catalog}", config.SoftToolProtocolXML)
	if err != nil {
		t.Fatalf("GenerateFunctionPrompt returned error: %v", err)
	}

	unescaped := html.UnescapeString(prompt)
	if !strings.Contains(unescaped, `<tool id="1" name="search_web"><description>Search the web</description><parameters>`) {
		t.Fatalf("expected compact tool tag, got %q", unescaped)
	}
	if !strings.Contains(unescaped, `<parameter name="locale" type="string" required="false"><description>optional locale</description><schema>`) {
		t.Fatalf("expected compact locale parameter, got %q", unescaped)
	}
	if !strings.Contains(unescaped, `"default":"en-US"`) || !strings.Contains(unescaped, `"enum":["en-US","zh-CN"]`) {
		t.Fatalf("expected locale metadata in schema, got %q", unescaped)
	}
	if !strings.Contains(unescaped, `<parameter name="query" type="string" required="true"><description>search query</description><schema>`) {
		t.Fatalf("expected compact query parameter, got %q", unescaped)
	}
	if !strings.Contains(unescaped, `"examples":["weather"]`) || !strings.Contains(unescaped, `"minLength":1`) {
		t.Fatalf("expected query metadata in schema, got %q", unescaped)
	}
	if !strings.Contains(unescaped, `"properties":{"days":{"maximum":7,"minimum":1,"type":"integer"},"tags":{"items":{"enum":["rain","aqi","wind"],"type":"string"},"type":"array"}}`) {
		t.Fatalf("expected nested object and array schema metadata, got %q", unescaped)
	}
	if !strings.Contains(unescaped, `"required":["days"]`) {
		t.Fatalf("expected nested required metadata, got %q", unescaped)
	}
	if strings.Contains(unescaped, "<default>") || strings.Contains(unescaped, "<examples>") || strings.Contains(unescaped, "<constraints>") || strings.Contains(unescaped, "<required>") {
		t.Fatalf("expected metadata to stay inside compact schema blocks, got %q", unescaped)
	}
}

func TestGenerateFunctionPromptSupportsMultipleComplexTools(t *testing.T) {
	routerDesc := "Route planning"
	fileDesc := "Write file"
	tools := []protocol.Tool{
		{
			Type: "function",
			Function: protocol.ToolFunction{
				Name:        "plan_route",
				Description: &routerDesc,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"origin": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"lat": map[string]any{"type": "number"},
								"lng": map[string]any{"type": "number"},
							},
							"required": []any{"lat", "lng"},
						},
						"transport": map[string]any{
							"type": "string",
							"enum": []any{"walking", "driving", "transit"},
						},
					},
					"required": []any{"origin", "transport"},
				},
			},
		},
		{
			Type: "function",
			Function: protocol.ToolFunction{
				Name:        "write_file",
				Description: &fileDesc,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"content": map[string]any{
							"type":      "string",
							"minLength": 1,
						},
						"path": map[string]any{
							"type":        "string",
							"description": "absolute path",
							"pattern":     "^/",
						},
					},
					"required": []any{"path", "content"},
				},
			},
		},
	}

	prompt, err := GenerateFunctionPrompt(tools, "<Function_Test_Start>", "{tool_catalog}", config.SoftToolProtocolXML)
	if err != nil {
		t.Fatalf("GenerateFunctionPrompt returned error: %v", err)
	}

	unescaped := html.UnescapeString(prompt)
	if !strings.Contains(unescaped, `<tool id="1" name="plan_route">`) || !strings.Contains(unescaped, `<tool id="2" name="write_file">`) {
		t.Fatalf("expected both tool definitions, got %q", unescaped)
	}
	if !strings.Contains(unescaped, `"enum":["walking","driving","transit"]`) {
		t.Fatalf("expected enum metadata for transport, got %q", unescaped)
	}
	if !strings.Contains(unescaped, `"properties":{"lat":{"type":"number"},"lng":{"type":"number"}}`) {
		t.Fatalf("expected nested origin object metadata, got %q", unescaped)
	}
	if !strings.Contains(unescaped, `"pattern":"^/"`) || !strings.Contains(unescaped, `"minLength":1`) {
		t.Fatalf("expected file parameter constraints, got %q", unescaped)
	}
}

func TestGenerateFunctionPromptHandlesToolWithoutParameters(t *testing.T) {
	description := "Show diagnostics"
	tools := []protocol.Tool{{
		Type: "function",
		Function: protocol.ToolFunction{
			Name:        "show_diagnostics",
			Description: &description,
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}}

	prompt, err := GenerateFunctionPrompt(tools, "<Function_Test_Start>", "{tool_catalog}", config.SoftToolProtocolXML)
	if err != nil {
		t.Fatalf("GenerateFunctionPrompt returned error: %v", err)
	}

	unescaped := html.UnescapeString(prompt)
	if !strings.Contains(unescaped, `<tool id="1" name="show_diagnostics"><description>Show diagnostics</description><parameters>None</parameters></tool>`) {
		t.Fatalf("expected tool without parameters to render compact None block, got %q", unescaped)
	}
}

func TestGenerateFunctionPromptConsolidatesOutputRulesIntoProtocolRules(t *testing.T) {
	description := "Search the web"
	tools := []protocol.Tool{{
		Type: "function",
		Function: protocol.ToolFunction{
			Name:        "search_web",
			Description: &description,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type": "string",
					},
				},
			},
		},
	}}

	prompt, err := GenerateFunctionPrompt(tools, "<Function_Test_Start>", "{tool_catalog}\n{protocol_rules}\n{output_rules}", config.SoftToolProtocolXML)
	if err != nil {
		t.Fatalf("GenerateFunctionPrompt returned error: %v", err)
	}

	unescaped := html.UnescapeString(prompt)
	if strings.Count(unescaped, "Output rules:") != 1 {
		t.Fatalf("expected output rules to appear once via protocol rules, got %q", unescaped)
	}
	if !strings.Contains(unescaped, "If a tool is needed: output the tool turn now in this same turn, optionally preceded by ONE brief sentence.") {
		t.Fatalf("expected prompt to forbid delaying tool use into a later turn, got %q", unescaped)
	}
	if !strings.Contains(unescaped, "After the tool turn starts: output NOTHING else. No explanations, no summaries, no follow-up.") {
		t.Fatalf("expected prompt to forbid text after tool call, got %q", unescaped)
	}
}

func TestGenerateFunctionPromptIncludesXMLMultiCallExample(t *testing.T) {
	description := "Search the web"
	tools := []protocol.Tool{{
		Type: "function",
		Function: protocol.ToolFunction{
			Name:        "search_web",
			Description: &description,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type": "string",
					},
				},
			},
		},
	}}

	prompt, err := GenerateFunctionPrompt(tools, "<Function_Test_Start>", "", config.SoftToolProtocolXML)
	if err != nil {
		t.Fatalf("GenerateFunctionPrompt returned error: %v", err)
	}

	if !strings.Contains(prompt, "Multiple call shape:") {
		t.Fatalf("expected XML prompt to include multi-call section, got %q", prompt)
	}
	if !strings.Contains(prompt, "exactly one <function_calls>...</function_calls> block containing one or more <invoke name=\"tool_name\">...</invoke> elements") {
		t.Fatalf("expected XML rules to allow multiple invokes, got %q", prompt)
	}
	if !strings.Contains(prompt, "<function_calls>\n  <invoke name=\"tool_name\">") || !strings.Contains(prompt, "<invoke name=\"tool_name_2\">") {
		t.Fatalf("expected XML multi-call example, got %q", prompt)
	}
}

func TestSafeProcessToolChoiceUsesConciseInstructions(t *testing.T) {
	required := SafeProcessToolChoice("required", config.SoftToolProtocolXML)
	if !strings.Contains(required, "Tool choice: required.") {
		t.Fatalf("expected concise required tool choice prompt, got %q", required)
	}
	if !strings.Contains(required, "Do not reply with a text turn only.") {
		t.Fatalf("expected required prompt to forbid text-only deferral, got %q", required)
	}
	if !strings.Contains(required, "Do not add any text after the tool call.") {
		t.Fatalf("expected required prompt to forbid text after tool call, got %q", required)
	}

	specific := SafeProcessToolChoice(map[string]any{
		"function": map[string]any{
			"name": "search_web",
		},
	}, config.SoftToolProtocolXML)
	if !strings.Contains(specific, "required tool `search_web`") {
		t.Fatalf("expected specific tool choice prompt, got %q", specific)
	}
	if !strings.Contains(specific, "Do not reply with a text turn only.") {
		t.Fatalf("expected specific prompt to forbid text-only deferral, got %q", specific)
	}
	if !strings.Contains(specific, "Do not add any text after the tool call.") {
		t.Fatalf("expected specific prompt to forbid text after tool call, got %q", specific)
	}
}

func TestGenerateFunctionPromptUsesSentinelJSONDefaultTemplateWhenCustomTemplateEmpty(t *testing.T) {
	description := "Search the web"
	tools := []protocol.Tool{{
		Type: "function",
		Function: protocol.ToolFunction{
			Name:        "search_web",
			Description: &description,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
			},
		},
	}}

	prompt, err := GenerateFunctionPrompt(tools, "<Function_Test_Start>", "", config.SoftToolProtocolSentinelJSON)
	if err != nil {
		t.Fatalf("GenerateFunctionPrompt returned error: %v", err)
	}
	if !strings.Contains(prompt, "<TOOL_CALL>") || !strings.Contains(prompt, "<TOOL_CALLS>") || !strings.Contains(prompt, `"arguments":{"arg_name":"value"}`) {
		t.Fatalf("expected sentinel JSON template, got %q", prompt)
	}
	if !strings.Contains(prompt, `Tools: [{"description":"Search the web","name":"search_web","parameters":{"properties":{"query":{"type":"string"}},"type":"object"}}]`) {
		t.Fatalf("expected protocol-native JSON tool catalog, got %q", prompt)
	}
	if strings.Contains(prompt, "<function_list>") {
		t.Fatalf("expected sentinel JSON prompt to avoid XML tool catalog, got %q", prompt)
	}
}

func TestGenerateFunctionPromptAllowsSentinelJSONCustomTemplate(t *testing.T) {
	description := "Search the web"
	tools := []protocol.Tool{{
		Type: "function",
		Function: protocol.ToolFunction{
			Name:        "search_web",
			Description: &description,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "search query",
					},
				},
				"required": []any{"query"},
			},
		},
	}}

	template := `Protocol: {protocol_name}
Catalog: {tool_catalog}
Rules:
{protocol_rules}
Single:
{single_call_example}
Output:
{output_rules}`

	prompt, err := GenerateFunctionPrompt(tools, "<Function_Test_Start>", template, config.SoftToolProtocolSentinelJSON)
	if err != nil {
		t.Fatalf("GenerateFunctionPrompt returned error: %v", err)
	}
	if !strings.Contains(prompt, "Protocol: sentinel + JSON") {
		t.Fatalf("expected custom sentinel protocol label, got %q", prompt)
	}
	if !strings.Contains(prompt, `Catalog: [{"description":"Search the web","name":"search_web","parameters":{"properties":{"query":{"description":"search query","type":"string"}},"required":["query"],"type":"object"}}]`) {
		t.Fatalf("expected custom template to receive JSON tool catalog, got %q", prompt)
	}
	if !strings.Contains(prompt, "Use <TOOL_CALL> for one tool call and <TOOL_CALLS> for multiple tool calls.") {
		t.Fatalf("expected protocol rules placeholder to render sentinel JSON instructions, got %q", prompt)
	}
	if !strings.Contains(prompt, "If a tool is needed: output the tool turn now in this same turn, optionally preceded by ONE brief sentence.") {
		t.Fatalf("expected sentinel JSON rules to forbid delaying tool use into a later turn, got %q", prompt)
	}
	if !strings.Contains(prompt, "<Function_Test_Start>\n<TOOL_CALL>") {
		t.Fatalf("expected custom template to render trigger-based sentinel JSON example, got %q", prompt)
	}
	if strings.Contains(prompt, "<function_list>") {
		t.Fatalf("expected sentinel JSON custom template to avoid XML tool catalog, got %q", prompt)
	}
	if strings.Count(prompt, "Output rules:") != 1 {
		t.Fatalf("expected output rules to come only from protocol rules, got %q", prompt)
	}
}

func TestGenerateFunctionPromptUsesMarkdownBlockDefaultTemplate(t *testing.T) {
	description := "Search the web"
	tools := []protocol.Tool{{
		Type: "function",
		Function: protocol.ToolFunction{
			Name:        "search_web",
			Description: &description,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"headers": map[string]any{
						"type":        "object",
						"description": "request headers",
					},
					"query": map[string]any{
						"type":        "string",
						"description": "search query",
					},
					"tags": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
				},
				"required": []any{"query"},
			},
		},
	}}

	prompt, err := GenerateFunctionPrompt(tools, "<Function_Test_Start>", "", config.SoftToolProtocolMarkdownBlock)
	if err != nil {
		t.Fatalf("GenerateFunctionPrompt returned error: %v", err)
	}
	if !strings.Contains(prompt, "Reply in one of two modes only: a complete text turn, or a single Markdown fenced tool turn") {
		t.Fatalf("expected markdown block prompt template, got %q", prompt)
	}
	if !strings.Contains(prompt, "```mbtoolcalls") || !strings.Contains(prompt, "mbcall: tool_name") || !strings.Contains(prompt, "mbarg[query]: value") {
		t.Fatalf("expected markdown block example in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Inside the fenced block, start each tool call with `mbcall: tool_name`.") || !strings.Contains(prompt, "Add arguments with line-start bracket headers, for example `mbarg[query]: value`.") || !strings.Contains(prompt, "For multiline or exact text, write `mbarg[name]:` and continue the value until the next line-start `mbarg[...]:` line") {
		t.Fatalf("expected markdown encoding hints in protocol rules, got %q", prompt)
	}
	if strings.Contains(prompt, "Encoding hints:") {
		t.Fatalf("expected markdown tool catalog to avoid duplicated encoding hints, got %q", prompt)
	}
	if !strings.Contains(prompt, "required args: query(string) - search query") {
		t.Fatalf("expected compact required args list, got %q", prompt)
	}
	if strings.Contains(prompt, "<function_list>") || strings.Contains(prompt, "<TOOL_CALL>") {
		t.Fatalf("expected markdown block prompt to avoid XML/JSON native catalogs, got %q", prompt)
	}
}

func TestGetFunctionCallPromptTemplateDefaultXMLIsComplete(t *testing.T) {
	template, err := GetFunctionCallPromptTemplate("", config.SoftToolProtocolXML)
	if err != nil {
		t.Fatalf("GetFunctionCallPromptTemplate returned error: %v", err)
	}
	if !containsAny(template, []string{"{tool_catalog}"}) {
		t.Fatalf("expected XML default template to include tool catalog placeholder, got %q", template)
	}
	if !containsAny(template, []string{"{trigger_signal}", "{protocol_rules}", "{single_call_example}", "{multi_call_example}"}) {
		t.Fatalf("expected XML default template to include protocol placeholder, got %q", template)
	}
}

func TestGetFunctionCallPromptTemplateDefaultSentinelJSONIsComplete(t *testing.T) {
	template, err := GetFunctionCallPromptTemplate("", config.SoftToolProtocolSentinelJSON)
	if err != nil {
		t.Fatalf("GetFunctionCallPromptTemplate returned error: %v", err)
	}
	if !containsAny(template, []string{"{tool_catalog}"}) {
		t.Fatalf("expected sentinel JSON default template to include tool catalog placeholder, got %q", template)
	}
	if !containsAny(template, []string{"{trigger_signal}", "{protocol_rules}", "{single_call_example}", "{multi_call_example}"}) {
		t.Fatalf("expected sentinel JSON default template to include protocol placeholder, got %q", template)
	}
}

func TestGetFunctionCallPromptTemplateDefaultMarkdownBlockIsComplete(t *testing.T) {
	template, err := GetFunctionCallPromptTemplate("", config.SoftToolProtocolMarkdownBlock)
	if err != nil {
		t.Fatalf("GetFunctionCallPromptTemplate returned error: %v", err)
	}
	if !containsAny(template, []string{"{tool_catalog}"}) {
		t.Fatalf("expected markdown block default template to include tool catalog placeholder, got %q", template)
	}
	if !containsAny(template, []string{"{trigger_signal}", "{protocol_rules}", "{single_call_example}", "{multi_call_example}"}) {
		t.Fatalf("expected markdown block default template to include protocol placeholder, got %q", template)
	}
}

func containsAny(value string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}

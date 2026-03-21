package proxy

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"

	"x-tool/internal/config"
	"x-tool/internal/protocol"
)

const toolOutputRules = `

Tool output rules:
- A text turn must be complete on its own. If a tool is needed, use the structured tool turn in the same turn.
- If a tool is needed, either output only the structured tool call, or output one brief sentence immediately before the tool call.
- After the tool call starts, do not output any extra text.
- Do not add explanations, summaries, or follow-up text after the tool call.
`

const defaultXMLPromptTemplate = `Reply in one of two modes only: a complete text turn, or a single XML tool turn, optionally preceded by one brief sentence.
Tools: {tool_catalog}
{protocol_rules}
Single call shape:
{single_call_example}
Multiple call shape:
{multi_call_example}
Examples:
Text turn: Hello -> Hello! How can I help?
Pure tool turn:
{single_call_example}
Brief text + tool turn:
Let me check.
{single_call_example}`

const defaultSentinelJSONPromptTemplate = `Reply in one of two modes only: a complete text turn, or a single sentinel + JSON tool turn, optionally preceded by one brief sentence.
Tools: {tool_catalog}
{protocol_rules}
Single call shape:
{single_call_example}
Multiple call shape:
{multi_call_example}
Examples:
Text turn: Hello -> Hello! How can I help?
Pure tool turn:
{single_call_example}
Brief text + tool turn:
Let me check.
{single_call_example}`

const defaultMarkdownBlockPromptTemplate = `Reply in one of two modes only: a complete text turn, or a single Markdown fenced tool turn, optionally preceded by one brief sentence.
Tools:
{tool_catalog}
{protocol_rules}
Single call shape:
{single_call_example}
Multiple call shape:
{multi_call_example}
Examples:
Text turn: Hello -> Hello! How can I help?
Pure tool turn:
{single_call_example}
Brief text + tool turn:
Let me check.
{single_call_example}`

type promptTemplateData struct {
	ProtocolName      string
	TriggerSignal     string
	ToolCatalog       string
	ProtocolRules     string
	SingleCallExample string
	MultiCallExample  string
	OutputRules       string
}

func GenerateRandomTriggerSignal() string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "<Function_FailSafe_Start>"
	}
	for i := range buf {
		buf[i] = chars[int(buf[i])%len(chars)]
	}
	return "<Function_" + string(buf) + "_Start>"
}

func GetFunctionCallPromptTemplate(customTemplate, protocolName string) (string, error) {
	template := strings.TrimSpace(customTemplate)
	if template == "" {
		switch protocolName {
		case config.SoftToolProtocolMarkdownBlock:
			template = defaultMarkdownBlockPromptTemplate
		case config.SoftToolProtocolSentinelJSON:
			template = defaultSentinelJSONPromptTemplate
		default:
			template = defaultXMLPromptTemplate
		}
	}
	if strings.TrimSpace(template) == "" {
		return "", fmt.Errorf("no valid prompt template found")
	}
	return template, nil
}

func GenerateFunctionPrompt(tools []protocol.Tool, triggerSignal, customTemplate, protocolName string) (string, error) {
	template, err := GetFunctionCallPromptTemplate(customTemplate, protocolName)
	if err != nil {
		return "", err
	}

	data := buildPromptTemplateData(tools, triggerSignal, protocolName)
	prompt := applyPromptTemplate(template, data)
	if !strings.Contains(template, "{output_rules}") {
		prompt += toolOutputRules
	}
	return prompt, nil
}

func SafeProcessToolChoice(toolChoice any, protocolName string) string {
	if toolChoice == nil {
		return ""
	}

	formatLabel := "XML"
	switch protocolName {
	case config.SoftToolProtocolSentinelJSON:
		formatLabel = "sentinel + JSON"
	case config.SoftToolProtocolMarkdownBlock:
		formatLabel = "Markdown fenced block"
	}

	switch value := toolChoice.(type) {
	case string:
		if value == "none" {
			return "\n\nTool choice: none. Reply with a complete text turn only."
		}
		if value == "required" {
			return "\n\nTool choice: required. Output one valid " + formatLabel + " tool turn in this same turn, optionally preceded by one brief sentence. Do not reply with a text turn only. Do not add any text after the tool call."
		}
	case map[string]any:
		if function, ok := value["function"].(map[string]any); ok {
			if name, ok := function["name"].(string); ok && strings.TrimSpace(name) != "" {
				return "\n\nTool choice: required tool `" + name + "`. Output one valid " + formatLabel + " tool turn using this tool in this same turn, optionally preceded by one brief sentence. Do not reply with a text turn only. Do not add any text after the tool call."
			}
		}
	}

	return ""
}

func isRequired(requiredSet map[string]struct{}, name string) bool {
	_, ok := requiredSet[name]
	return ok
}

func requiredParameterSet(schema map[string]any) map[string]struct{} {
	requiredSet := map[string]struct{}{}
	switch requiredRaw := schema["required"].(type) {
	case []any:
		for _, item := range requiredRaw {
			if s, ok := item.(string); ok {
				requiredSet[s] = struct{}{}
			}
		}
	case []string:
		for _, item := range requiredRaw {
			if strings.TrimSpace(item) != "" {
				requiredSet[item] = struct{}{}
			}
		}
	}
	return requiredSet
}

func sortedKeys(input map[string]any) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compactSchemaJSON(info map[string]any) string {
	if len(info) == 0 {
		return ""
	}

	data, err := json.Marshal(info)
	if err != nil {
		return ""
	}
	return string(data)
}

func buildPromptTemplateData(tools []protocol.Tool, triggerSignal, protocolName string) promptTemplateData {
	toolCatalog := renderToolCatalog(tools, protocolName)
	return promptTemplateData{
		ProtocolName:      protocolFormatLabel(protocolName),
		TriggerSignal:     triggerSignal,
		ToolCatalog:       toolCatalog,
		ProtocolRules:     buildProtocolRules(protocolName, triggerSignal),
		SingleCallExample: buildSingleCallExample(protocolName, triggerSignal),
		MultiCallExample:  buildMultiCallExample(protocolName, triggerSignal),
		OutputRules:       strings.TrimSpace(toolOutputRules),
	}
}

func applyPromptTemplate(template string, data promptTemplateData) string {
	replacer := strings.NewReplacer(
		"{protocol_name}", data.ProtocolName,
		"{trigger_signal}", data.TriggerSignal,
		"{tool_catalog}", data.ToolCatalog,
		"{protocol_rules}", data.ProtocolRules,
		"{single_call_example}", data.SingleCallExample,
		"{multi_call_example}", data.MultiCallExample,
		"{output_rules}", data.OutputRules,
	)
	return replacer.Replace(template)
}

func renderToolCatalog(tools []protocol.Tool, protocolName string) string {
	switch protocolName {
	case config.SoftToolProtocolMarkdownBlock:
		return renderMarkdownBlockToolCatalog(tools)
	case config.SoftToolProtocolSentinelJSON:
		return renderSentinelJSONToolCatalog(tools)
	default:
		return renderXMLToolCatalog(tools)
	}
}

func renderXMLToolCatalog(tools []protocol.Tool) string {
	toolBlocks := make([]string, 0, len(tools))
	for i, tool := range tools {
		schema := tool.Function.Parameters
		props, _ := schema["properties"].(map[string]any)
		description := ""
		if tool.Function.Description != nil {
			description = *tool.Function.Description
		}

		requiredSet := requiredParameterSet(schema)

		paramNames := sortedKeys(props)
		var params []string
		for _, name := range paramNames {
			raw := props[name]
			info, _ := raw.(map[string]any)
			paramType, _ := info["type"].(string)
			if paramType == "" {
				paramType = "any"
			}
			paramDescription, _ := info["description"].(string)
			paramDescription = strings.TrimSpace(paramDescription)
			if paramDescription == "" {
				paramDescription = "None"
			}
			paramBlock := []string{
				fmt.Sprintf(
					`<parameter name="%s" type="%s" required="%t">`,
					html.EscapeString(name),
					html.EscapeString(paramType),
					isRequired(requiredSet, name),
				),
				"<description>" + html.EscapeString(paramDescription) + "</description>",
			}
			if schemaJSON := compactSchemaJSON(info); schemaJSON != "" {
				paramBlock = append(paramBlock, "<schema>"+html.EscapeString(schemaJSON)+"</schema>")
			}
			paramBlock = append(paramBlock, "</parameter>")
			params = append(params, strings.Join(paramBlock, ""))
		}

		description = strings.TrimSpace(description)
		if description == "" {
			description = "None"
		}

		if len(params) == 0 {
			toolBlocks = append(toolBlocks, fmt.Sprintf(
				`<tool id="%d" name="%s"><description>%s</description><parameters>None</parameters></tool>`,
				i+1,
				html.EscapeString(tool.Function.Name),
				html.EscapeString(description),
			))
			continue
		}

		toolBlocks = append(toolBlocks, fmt.Sprintf(
			`<tool id="%d" name="%s"><description>%s</description><parameters>%s</parameters></tool>`,
			i+1,
			html.EscapeString(tool.Function.Name),
			html.EscapeString(description),
			strings.Join(params, ""),
		))
	}

	return "<function_list>" + strings.Join(toolBlocks, "") + "</function_list>"
}

func renderSentinelJSONToolCatalog(tools []protocol.Tool) string {
	catalog := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		entry := map[string]any{
			"name":       tool.Function.Name,
			"parameters": normalizedToolParameters(tool.Function.Parameters),
		}
		if tool.Function.Description != nil {
			description := strings.TrimSpace(*tool.Function.Description)
			if description != "" {
				entry["description"] = description
			}
		}
		catalog = append(catalog, entry)
	}

	payload, err := json.Marshal(catalog)
	if err != nil {
		return "[]"
	}
	return string(payload)
}

func renderMarkdownBlockToolCatalog(tools []protocol.Tool) string {
	if len(tools) == 0 {
		return "Available tools:\n- None"
	}

	toolBlocks := make([]string, 0, len(tools))
	for _, tool := range tools {
		schema := normalizedToolParameters(tool.Function.Parameters)
		props, _ := schema["properties"].(map[string]any)
		requiredSet := requiredParameterSet(schema)

		lines := []string{"- " + tool.Function.Name}
		if tool.Function.Description != nil {
			description := strings.TrimSpace(*tool.Function.Description)
			if description != "" {
				lines = append(lines, "  description: "+description)
			}
		}

		requiredArgs := make([]string, 0)
		optionalArgs := make([]string, 0)
		for _, name := range sortedKeys(props) {
			info, _ := props[name].(map[string]any)
			paramType, _ := info["type"].(string)
			if paramType == "" {
				paramType = "any"
			}
			paramText := name + "(" + paramType + ")"
			description, _ := info["description"].(string)
			description = strings.TrimSpace(description)
			if description != "" {
				paramText += " - " + description
			}
			if isRequired(requiredSet, name) {
				requiredArgs = append(requiredArgs, paramText)
			} else {
				optionalArgs = append(optionalArgs, paramText)
			}
		}

		if len(requiredArgs) > 0 {
			lines = append(lines, "  required args: "+strings.Join(requiredArgs, ", "))
		} else {
			lines = append(lines, "  required args: none")
		}
		if len(optionalArgs) > 0 {
			lines = append(lines, "  optional args: "+strings.Join(optionalArgs, ", "))
		}

		toolBlocks = append(toolBlocks, strings.Join(lines, "\n"))
	}

	toolBlocks = append(toolBlocks,
		"Encoding hints:\n- Start the fenced block with ```mbtoolcalls and each tool call with `mbcall: tool_name`.\n- Write arguments as line-start headers in bracket form, for example `mbarg[query]: weather`.\n- For nested fields use dot paths inside brackets, for example `mbarg[headers.authorization]: Bearer token`.\n- Use [] to append array items, for example `mbarg[tags[]]: news`.\n- Use @json only when a value must stay as raw JSON, for example `mbarg[payload@json]: {\"mode\":\"strict\"}`.\n- For multiline or exact text, write `mbarg[prompt]:` and continue the value on following lines until the next line-start `mbarg[...]:` line, the next `mbcall:` line, or the closing fence.",
	)

	return "Available tools:\n" + strings.Join(toolBlocks, "\n\n")
}

func normalizedToolParameters(parameters map[string]any) map[string]any {
	if len(parameters) == 0 {
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}
	return parameters
}

func protocolFormatLabel(protocolName string) string {
	switch protocolName {
	case config.SoftToolProtocolSentinelJSON:
		return "sentinel + JSON"
	case config.SoftToolProtocolMarkdownBlock:
		return "Markdown fenced block"
	}
	return "XML"
}

func buildProtocolRules(protocolName, triggerSignal string) string {
	switch protocolName {
	case config.SoftToolProtocolMarkdownBlock:
		return strings.Join([]string{
			"If no tool is needed, reply with a complete text turn.",
			"A text turn must be complete on its own. If a tool is needed to answer, continue, or complete the current turn, use a Markdown fenced tool turn in this same turn. Do not output a text turn that says you will call a tool next.",
			"If a tool is needed, output " + triggerSignal + " alone on its own line, then output exactly one ```mbtoolcalls fenced block.",
			"Inside the fenced block, start each tool call with `mbcall: tool_name`.",
			"Add arguments with line-start bracket headers, for example `mbarg[query]: value`.",
			"For multiline or exact text, write `mbarg[name]:` and continue the value on following lines until the next line-start `mbarg[...]:` line, the next `mbcall:` line, or the closing fence.",
			"For nested fields use dot paths. For arrays use key[]. Use key@json only when the value must be parsed as JSON.",
			"The tool name must come from the tool list. Include required parameters. Do not output any text after the closing fence.",
		}, "\n")
	case config.SoftToolProtocolSentinelJSON:
		return strings.Join([]string{
			"If no tool is needed, reply with a complete text turn.",
			"A text turn must be complete on its own. If a tool is needed to answer, continue, or complete the current turn, use a sentinel + JSON tool turn in this same turn. Do not output a text turn that says you will call a tool next.",
			"If a tool is needed, output " + triggerSignal + " alone on its own line, then output exactly one JSON tool block.",
			"Use <TOOL_CALL> for one tool call and <TOOL_CALLS> for multiple tool calls.",
			"The tool name must come from the tool list. The arguments field must be a JSON object. Do not output any text after the closing sentinel tag.",
		}, "\n")
	default:
		return strings.Join([]string{
			"If no tool is needed, reply with a complete text turn.",
			"A text turn must be complete on its own. If a tool is needed to answer, continue, or complete the current turn, use an XML tool turn in this same turn. Do not output a text turn that says you will call a tool next.",
			"If a tool is needed, output " + triggerSignal + " alone on its own line, then output exactly one <function_calls>...</function_calls> block containing one or more <invoke name=\"tool_name\">...</invoke> elements.",
			"The tool name must come from the tool list. Include required parameters. Use raw text inside <parameter>. Do not output any text after </function_calls>.",
		}, "\n")
	}
}

func buildSingleCallExample(protocolName, triggerSignal string) string {
	switch protocolName {
	case config.SoftToolProtocolMarkdownBlock:
		return strings.Join([]string{
			triggerSignal,
			"```mbtoolcalls",
			"mbcall: tool_name",
			"mbarg[query]: value",
			"```",
		}, "\n")
	case config.SoftToolProtocolSentinelJSON:
		return strings.Join([]string{
			triggerSignal,
			"<TOOL_CALL>",
			`{"name":"tool_name","arguments":{"arg_name":"value"}}`,
			"</TOOL_CALL>",
		}, "\n")
	default:
		return strings.Join([]string{
			triggerSignal,
			"<function_calls>",
			`  <invoke name="tool_name">`,
			`    <parameter name="arg_name">value</parameter>`,
			"  </invoke>",
			"</function_calls>",
		}, "\n")
	}
}

func buildMultiCallExample(protocolName, triggerSignal string) string {
	if protocolName == config.SoftToolProtocolMarkdownBlock {
		return strings.Join([]string{
			triggerSignal,
			"```mbtoolcalls",
			"mbcall: tool_name",
			"mbarg[query]: value",
			"",
			"mbcall: tool_name_2",
			"mbarg[prompt]:",
			"value line 1",
			"value line 2",
			"```",
		}, "\n")
	}
	if protocolName == config.SoftToolProtocolSentinelJSON {
		return strings.Join([]string{
			triggerSignal,
			"<TOOL_CALLS>",
			`[{"name":"tool_name","arguments":{"arg_name":"value"}}]`,
			"</TOOL_CALLS>",
		}, "\n")
	}
	return strings.Join([]string{
		triggerSignal,
		"<function_calls>",
		`  <invoke name="tool_name">`,
		`    <parameter name="arg_name">value</parameter>`,
		"  </invoke>",
		`  <invoke name="tool_name_2">`,
		`    <parameter name="arg_name_2">value_2</parameter>`,
		"  </invoke>",
		"</function_calls>",
	}, "\n")
}

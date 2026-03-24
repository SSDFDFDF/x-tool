package protocol

import (
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"

	"x-tool/internal/config"
	"x-tool/internal/toolcall"
)

func FormatToolResultForAI(store *toolcall.Manager, toolCallID, resultContent, protocolName string) string {
	name := "Unknown"
	id := ""
	if entry, ok := store.Get(toolCallID); ok {
		name = entry.Name
		id = entry.ID
	}

	return fmt.Sprintf(`
<tool_result name="%s" id="%s">
%s
</tool_result>
`, html.EscapeString(name), html.EscapeString(id), resultContent)
}

func FormatAssistantToolCallsForAI(toolCalls []any, triggerSignal, protocolName string) string {
	calls := make([]map[string]any, 0, len(toolCalls))

	for _, raw := range toolCalls {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		functionInfo, _ := call["function"].(map[string]any)
		name, _ := functionInfo["name"].(string)
		argumentsRaw := functionInfo["arguments"]
		args := map[string]any{}

		switch value := argumentsRaw.(type) {
		case string:
			var parsed any
			if !UnmarshalJSONWithRepair(value, &parsed) {
				args["content"] = value
			} else if dict, ok := parsed.(map[string]any); ok {
				args = dict
			} else {
				args["content"] = parsed
			}
		case map[string]any:
			args = value
		default:
			if value != nil {
				args["content"] = value
			}
		}

		calls = append(calls, map[string]any{
			"name":      name,
			"arguments": args,
		})
	}

	switch protocolName {
	case config.SoftToolProtocolMarkdownBlock:
		return formatAssistantMarkdownBlockCalls(calls, triggerSignal)
	case config.SoftToolProtocolSentinelJSON:
		return formatAssistantSentinelJSONCalls(calls, triggerSignal)
	default:
		return formatAssistantXMLToolCalls(calls, triggerSignal)
	}
}

func formatAssistantXMLToolCalls(calls []map[string]any, triggerSignal string) string {
	var invokes []string
	for _, call := range calls {
		name, _ := call["name"].(string)
		args, _ := call["arguments"].(map[string]any)
		var params []string
		for key, value := range args {
			params = append(params, fmt.Sprintf(`  <parameter name="%s">%s</parameter>`, html.EscapeString(key), html.EscapeString(stringifyValue(value))))
		}

		invoke := fmt.Sprintf(`<invoke name="%s">`, html.EscapeString(name))
		if len(params) > 0 {
			invoke += "\n" + strings.Join(params, "\n")
		}
		invoke += "\n</invoke>"
		invokes = append(invokes, invoke)
	}

	if len(invokes) == 0 {
		return triggerSignal
	}
	return triggerSignal + "\n<function_calls>\n" + strings.Join(invokes, "\n") + "\n</function_calls>"
}

func formatAssistantSentinelJSONCalls(calls []map[string]any, triggerSignal string) string {
	if len(calls) == 0 {
		return triggerSignal
	}

	var (
		payload []byte
		err     error
	)
	if len(calls) == 1 {
		payload, err = json.Marshal(calls[0])
		if err != nil {
			return triggerSignal
		}
		return triggerSignal + "\n<TOOL_CALL>\n" + string(payload) + "\n</TOOL_CALL>"
	}

	payload, err = json.Marshal(calls)
	if err != nil {
		return triggerSignal
	}
	return triggerSignal + "\n<TOOL_CALLS>\n" + string(payload) + "\n</TOOL_CALLS>"
}

func formatAssistantMarkdownBlockCalls(calls []map[string]any, triggerSignal string) string {
	if len(calls) == 0 {
		return triggerSignal
	}

	lines := []string{triggerSignal, "```mbtoolcalls"}
	for index, call := range calls {
		name, _ := call["name"].(string)
		args, _ := call["arguments"].(map[string]any)

		lines = append(lines, "mbcall: "+name)
		for _, key := range sortedMapKeys(args) {
			lines = append(lines, renderMarkdownArgumentLines(key, args[key])...)
		}
		if index < len(calls)-1 {
			lines = append(lines, "")
		}
	}
	lines = append(lines, "```")
	return strings.Join(lines, "\n")
}

func renderMarkdownArgumentLines(key string, value any) []string {
	switch typed := value.(type) {
	case map[string]any:
		lines := make([]string, 0)
		for _, nestedKey := range sortedMapKeys(typed) {
			lines = append(lines, renderMarkdownArgumentLines(key+"."+nestedKey, typed[nestedKey])...)
		}
		if len(lines) == 0 {
			return []string{renderMarkdownArgumentHeader(key+"@json") + " {}"}
		}
		return lines
	case []any:
		if markdownArrayIsScalarOnly(typed) {
			lines := make([]string, 0, len(typed))
			for _, item := range typed {
				lines = append(lines, renderMarkdownScalarArgumentLines(key+"[]", item)...)
			}
			return lines
		}
		return renderMarkdownJSONArgumentLines(key+"@json", typed)
	case []string:
		lines := make([]string, 0, len(typed))
		for _, item := range typed {
			lines = append(lines, renderMarkdownScalarArgumentLines(key+"[]", item)...)
		}
		return lines
	default:
		return renderMarkdownScalarArgumentLines(key, typed)
	}
}

func renderMarkdownScalarArgumentLines(key string, value any) []string {
	if text, ok := value.(string); ok {
		if markdownNeedsMultilineArgument(text) {
			return renderMarkdownMultilineArgumentLines(key, text)
		}
		return []string{renderMarkdownArgumentHeader(key) + " " + text}
	}
	return []string{renderMarkdownArgumentHeader(key) + " " + stringifyValue(value)}
}

func renderMarkdownJSONArgumentLines(key string, value any) []string {
	data, err := json.Marshal(value)
	if err != nil {
		return []string{renderMarkdownArgumentHeader(key) + " " + stringifyValue(value)}
	}
	return []string{renderMarkdownArgumentHeader(key) + " " + string(data)}
}

func markdownArrayIsScalarOnly(values []any) bool {
	for _, value := range values {
		switch value.(type) {
		case nil, string, bool, float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			continue
		default:
			return false
		}
	}
	return true
}

func markdownNeedsMultilineArgument(value string) bool {
	return strings.Contains(value, "\n") || value != strings.TrimSpace(value)
}

func renderMarkdownMultilineArgumentLines(key, value string) []string {
	lines := []string{renderMarkdownArgumentHeader(key)}
	lines = append(lines, strings.Split(value, "\n")...)
	return lines
}

func renderMarkdownArgumentHeader(key string) string {
	return "mbarg[" + key + "]:"
}

func sortedMapKeys(input map[string]any) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func PreprocessMessages(messages []map[string]any, store *toolcall.Manager, triggerSignal, protocolName string, convertDeveloperToSystem bool) []map[string]any {
	processed := make([]map[string]any, 0, len(messages))

	for _, message := range messages {
		role, _ := message["role"].(string)
		switch role {
		case "tool":
			toolCallID, _ := message["tool_call_id"].(string)
			content := stringifyMessageContent(message["content"])
			if toolCallID != "" && content != "" {
				processed = append(processed, map[string]any{
					"role":    "user",
					"content": FormatToolResultForAI(store, toolCallID, content, protocolName),
				})
			}
		case "assistant":
			rawToolCalls, ok := message["tool_calls"].([]any)
			if ok && len(rawToolCalls) > 0 {
				cloned := cloneMap(message)
				content := stringifyMessageContent(cloned["content"])
				formatted := FormatAssistantToolCallsForAI(rawToolCalls, triggerSignal, protocolName)
				cloned["content"] = strings.TrimSpace(strings.TrimSpace(content) + "\n" + formatted)
				delete(cloned, "tool_calls")
				processed = append(processed, cloned)
				continue
			}
			processed = append(processed, cloneMap(message))
		case "developer":
			cloned := cloneMap(message)
			if convertDeveloperToSystem {
				cloned["role"] = "system"
			}
			processed = append(processed, cloned)
		default:
			processed = append(processed, cloneMap(message))
		}
	}

	return processed
}

func ValidateMessageStructure(messages []map[string]any, convertDeveloperToSystem bool) bool {
	validRoles := map[string]struct{}{
		"system": {}, "user": {}, "assistant": {}, "tool": {},
	}
	if !convertDeveloperToSystem {
		validRoles["developer"] = struct{}{}
	}

	for _, msg := range messages {
		role, ok := msg["role"].(string)
		if !ok || role == "" {
			return false
		}
		if _, ok := validRoles[role]; !ok {
			return false
		}
		if role == "tool" {
			if _, ok := msg["tool_call_id"].(string); !ok {
				return false
			}
		}
	}
	return true
}

func stringifyValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(data)
	}
}

func cloneMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func CloneMap(input map[string]any) map[string]any {
	return cloneMap(input)
}

func stringifyMessageContent(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

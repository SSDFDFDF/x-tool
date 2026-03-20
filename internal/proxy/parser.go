package proxy

import (
	"encoding/json"
	"encoding/xml"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"x-tool/internal/config"
	"x-tool/internal/protocol"
)

var (
	functionCallsBlockPattern = regexp.MustCompile(`(?is)<function_calls\b[^>]*>.*?</function_calls>`)
	invokeBlockPattern        = regexp.MustCompile(`(?is)<invoke\b[^>]*>.*?</invoke>`)
	invokeNamePattern         = regexp.MustCompile(`(?is)<invoke\b[^>]*\bname\s*=\s*"([^"]*)"|<invoke\b[^>]*\bname\s*=\s*'([^']*)'`)
	nameTagPattern            = regexp.MustCompile(`(?is)<name>(.*?)</name>`)
	parameterTagPattern       = regexp.MustCompile(`(?is)<parameter\s+[^>]*name\s*=\s*"([^"]*)"[^>]*>(.*?)</parameter>|<parameter\s+[^>]*name\s*=\s*'([^']*)'[^>]*>(.*?)</parameter>`)
)

func RemoveThinkBlocks(text string) string {
	for strings.Contains(text, "<think>") && strings.Contains(text, "</think>") {
		start := strings.Index(text, "<think>")
		if start < 0 {
			break
		}

		pos := start + len("<think>")
		depth := 1
		for pos < len(text) && depth > 0 {
			switch {
			case strings.HasPrefix(text[pos:], "<think>"):
				depth++
				pos += len("<think>")
			case strings.HasPrefix(text[pos:], "</think>"):
				depth--
				pos += len("</think>")
			default:
				pos++
			}
		}

		if depth == 0 {
			text = text[:start] + text[pos:]
			continue
		}
		break
	}
	return text
}

func ParseFunctionCallsXML(xmlString, triggerSignal string) []protocol.ParsedToolCall {
	if strings.TrimSpace(xmlString) == "" {
		return nil
	}

	content := xmlString
	if triggerSignal != "" {
		if idx := strings.Index(content, triggerSignal); idx >= 0 {
			content = content[idx:]
		}
	}
	cleaned := RemoveThinkBlocks(content)
	if block := functionCallsBlockPattern.FindString(cleaned); block != "" {
		if parsed := parseXMLInvokeBlocks(invokeBlockPattern.FindAllString(block, -1)); len(parsed) > 0 {
			return parsed
		}
	}
	if parsed := parseXMLInvokeBlocks(invokeBlockPattern.FindAllString(cleaned, -1)); len(parsed) > 0 {
		return parsed
	}
	return nil
}

func parseXMLInvokeBlocks(invokes []string) []protocol.ParsedToolCall {
	if len(invokes) == 0 {
		return nil
	}

	parsed := make([]protocol.ParsedToolCall, 0, len(invokes))
	for _, invoke := range invokes {
		name := parseInvokeName(invoke)
		if strings.TrimSpace(name) == "" {
			return nil
		}

		if call, ok := parseInvokeXML(invoke); ok {
			parsed = append(parsed, call)
			continue
		}

		args := map[string]any{}
		matches := parameterTagPattern.FindAllStringSubmatch(invoke, -1)
		for _, match := range matches {
			paramName := firstNonEmpty(match[1], match[3])
			paramValue := strings.TrimSpace(html.UnescapeString(firstNonEmpty(match[2], match[4])))
			if paramName == "" {
				continue
			}
			args[paramName] = coerceJSON(paramValue)
		}

		parsed = append(parsed, protocol.ParsedToolCall{
			Name: html.UnescapeString(strings.TrimSpace(name)),
			Args: args,
		})
	}

	return parsed
}

func ParseFunctionCallsSentinelJSON(content, triggerSignal string) []protocol.ParsedToolCall {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	if triggerSignal != "" {
		if idx := strings.Index(content, triggerSignal); idx >= 0 {
			content = content[idx+len(triggerSignal):]
		}
	}

	cleaned := strings.TrimSpace(RemoveThinkBlocks(content))
	if payload, ok := extractTaggedPayload(cleaned, "<TOOL_CALL>", "</TOOL_CALL>"); ok {
		return parseSentinelJSONPayload(payload, false)
	}
	if payload, ok := extractTaggedPayload(cleaned, "<TOOL_CALLS>", "</TOOL_CALLS>"); ok {
		return parseSentinelJSONPayload(payload, true)
	}
	return nil
}

func ParseFunctionCallsMarkdownBlock(content, triggerSignal string) []protocol.ParsedToolCall {
	return ParseFunctionCallsMarkdownBlockWithTools(content, triggerSignal, nil)
}

func ParseFunctionCallsMarkdownBlockWithTools(content, triggerSignal string, tools []protocol.Tool) []protocol.ParsedToolCall {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	cleaned := RemoveThinkBlocks(content)
	if payload, ok := extractMarkdownToolPayload(cleaned, triggerSignal); ok {
		if parsed := parseMarkdownToolPayloadWithBoundedArgs(payload, tools); len(parsed) > 0 {
			return parsed
		}
	}
	if parsed := ParseFunctionCallsSentinelJSON(cleaned, triggerSignal); len(parsed) > 0 {
		return parsed
	}
	if parsed := ParseFunctionCallsXML(cleaned, triggerSignal); len(parsed) > 0 {
		return parsed
	}
	return nil
}

func parseMarkdownToolPayloadWithBoundedArgs(payload string, tools []protocol.Tool) []protocol.ParsedToolCall {
	lines := strings.Split(strings.ReplaceAll(payload, "\r\n", "\n"), "\n")
	if len(lines) == 0 {
		return nil
	}

	allowedArgs := markdownAllowedArgsByTool(tools)
	parsed := make([]protocol.ParsedToolCall, 0)
	var (
		currentName string
		currentArgs map[string]any
	)

	flushCurrentCall := func() bool {
		if currentName == "" {
			return true
		}
		parsed = append(parsed, protocol.ParsedToolCall{
			Name: currentName,
			Args: currentArgs,
		})
		currentName = ""
		currentArgs = nil
		return true
	}

	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}

		if name, ok := parseMarkdownCallName(line); ok {
			if !flushCurrentCall() {
				return nil
			}
			currentName = name
			currentArgs = map[string]any{}
			i++
			continue
		}

		if currentName == "" {
			return nil
		}

		key, inlineValue, ok := parseMarkdownBoundedArgumentHeader(line, allowedArgs[currentName])
		if !ok {
			return nil
		}

		valueLines := make([]string, 0, 4)
		if inlineValue != "" {
			valueLines = append(valueLines, inlineValue)
		}

		j := i + 1
		for j < len(lines) {
			nextLine := lines[j]
			if name, ok := parseMarkdownCallName(nextLine); ok && name != "" {
				break
			}
			if _, _, ok := parseMarkdownBoundedArgumentHeader(nextLine, allowedArgs[currentName]); ok {
				break
			}
			valueLines = append(valueLines, nextLine)
			j++
		}

		value := buildMarkdownBoundedArgumentValue(valueLines, inlineValue != "")
		if !assignMarkdownArgument(currentArgs, normalizeMarkdownBoundedArgumentKey(key), coerceMarkdownBoundedArgumentValue(key, value)) {
			return nil
		}
		i = j
	}

	if !flushCurrentCall() {
		return nil
	}
	return parsed
}

type StreamingFunctionCallDetector struct {
	triggerSignal string
	protocolName  string
	contentBuffer string
	state         string
	thinkDepth    int
}

func NewStreamingFunctionCallDetector(triggerSignal, protocolName string) *StreamingFunctionCallDetector {
	return &StreamingFunctionCallDetector{
		triggerSignal: triggerSignal,
		protocolName:  protocolName,
		state:         "detecting",
	}
}

func (d *StreamingFunctionCallDetector) ProcessChunk(deltaContent string) (bool, string) {
	if deltaContent == "" {
		return false, ""
	}

	d.contentBuffer += deltaContent
	if d.state == "tool_parsing" {
		return false, ""
	}

	var yield strings.Builder
	i := 0
	for i < len(d.contentBuffer) {
		if strings.HasPrefix(d.contentBuffer[i:], "<think>") {
			d.thinkDepth++
			yield.WriteString("<think>")
			i += len("<think>")
			continue
		}
		if strings.HasPrefix(d.contentBuffer[i:], "</think>") {
			if d.thinkDepth > 0 {
				d.thinkDepth--
			}
			yield.WriteString("</think>")
			i += len("</think>")
			continue
		}

		if d.thinkDepth == 0 {
			if offset, ok := toolTriggerOffset(d.contentBuffer[i:], d.triggerSignal); ok {
				d.state = "tool_parsing"
				d.contentBuffer = d.contentBuffer[i+offset:]
				return true, yield.String()
			}
		}

		if shouldHoldForPossibleControlPrefix(d.contentBuffer[i:], d.triggerSignal) {
			break
		}

		r, size := utf8.DecodeRuneInString(d.contentBuffer[i:])
		if r == utf8.RuneError && size == 1 {
			break
		}
		yield.WriteRune(r)
		i += size
	}

	d.contentBuffer = d.contentBuffer[i:]
	return false, yield.String()
}

func (d *StreamingFunctionCallDetector) Finalize() []protocol.ParsedToolCall {
	if d.state != "tool_parsing" {
		return nil
	}
	switch d.protocolName {
	case config.SoftToolProtocolMarkdownBlock:
		return ParseFunctionCallsMarkdownBlock(d.contentBuffer, d.triggerSignal)
	case config.SoftToolProtocolSentinelJSON:
		return ParseFunctionCallsSentinelJSON(d.contentBuffer, d.triggerSignal)
	default:
		return ParseFunctionCallsXML(d.contentBuffer, d.triggerSignal)
	}
}

func (d *StreamingFunctionCallDetector) IsToolParsing() bool {
	return d.state == "tool_parsing"
}

func (d *StreamingFunctionCallDetector) Buffer() string {
	return d.contentBuffer
}

func (d *StreamingFunctionCallDetector) AppendToBuffer(content string) {
	d.contentBuffer += content
}

func (d *StreamingFunctionCallDetector) HasCompleteToolTurn() bool {
	switch d.protocolName {
	case config.SoftToolProtocolMarkdownBlock:
		_, ok := extractMarkdownToolPayload(d.contentBuffer, d.triggerSignal)
		return ok
	case config.SoftToolProtocolSentinelJSON:
		return strings.Contains(d.contentBuffer, "</TOOL_CALL>") || strings.Contains(d.contentBuffer, "</TOOL_CALLS>")
	default:
		return strings.Contains(d.contentBuffer, "</function_calls>")
	}
}

func parseInvokeName(invoke string) string {
	if match := invokeNamePattern.FindStringSubmatch(invoke); len(match) > 0 {
		return firstNonEmpty(match[1], match[2])
	}
	if match := nameTagPattern.FindStringSubmatch(invoke); len(match) > 1 {
		return match[1]
	}
	return ""
}

func coerceJSON(value string) any {
	var result any
	if err := json.Unmarshal([]byte(value), &result); err == nil {
		return result
	}
	return value
}

func extractTaggedPayload(content, openTag, closeTag string) (string, bool) {
	start := strings.Index(content, openTag)
	if start < 0 {
		return "", false
	}
	start += len(openTag)
	end := strings.Index(content[start:], closeTag)
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(content[start : start+end]), true
}

func parseSentinelJSONPayload(payload string, expectArray bool) []protocol.ParsedToolCall {
	type rawToolCall struct {
		Name      string `json:"name"`
		Arguments any    `json:"arguments"`
	}

	normalizeArgs := func(arguments any) map[string]any {
		switch value := arguments.(type) {
		case nil:
			return map[string]any{}
		case map[string]any:
			return value
		default:
			return map[string]any{"content": value}
		}
	}

	if expectArray {
		var rawCalls []rawToolCall
		if err := json.Unmarshal([]byte(payload), &rawCalls); err != nil {
			return nil
		}
		parsed := make([]protocol.ParsedToolCall, 0, len(rawCalls))
		for _, call := range rawCalls {
			if strings.TrimSpace(call.Name) == "" {
				return nil
			}
			parsed = append(parsed, protocol.ParsedToolCall{
				Name: strings.TrimSpace(call.Name),
				Args: normalizeArgs(call.Arguments),
			})
		}
		return parsed
	}

	var rawCall rawToolCall
	if err := json.Unmarshal([]byte(payload), &rawCall); err != nil {
		return nil
	}
	if strings.TrimSpace(rawCall.Name) == "" {
		return nil
	}
	return []protocol.ParsedToolCall{{
		Name: strings.TrimSpace(rawCall.Name),
		Args: normalizeArgs(rawCall.Arguments),
	}}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func toolTriggerOffset(content, triggerSignal string) (int, bool) {
	switch {
	case strings.HasPrefix(content, triggerSignal):
		return 0, true
	case strings.HasPrefix(content, "```"+triggerSignal):
		return 3, true
	case strings.HasPrefix(content, "~~~"+triggerSignal):
		return 3, true
	default:
		return 0, false
	}
}

func shouldHoldForPossibleControlPrefix(content, triggerSignal string) bool {
	if content == "" {
		return false
	}

	candidates := []string{
		"<think>",
		"</think>",
		triggerSignal,
		"```" + triggerSignal,
		"~~~" + triggerSignal,
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if len(content) < len(candidate) && strings.HasPrefix(candidate, content) {
			return true
		}
	}
	return false
}

func extractMarkdownToolPayload(content, triggerSignal string) (string, bool) {
	cleaned := strings.ReplaceAll(content, "\r\n", "\n")

	for _, fence := range []string{"```", "~~~"} {
		prefix := fence + triggerSignal
		if triggerSignal != "" {
			if idx := strings.Index(cleaned, prefix); idx >= 0 {
				remainder := cleaned[idx+len(prefix):]
				if strings.HasPrefix(remainder, "\n") {
					remainder = remainder[1:]
				}
				return extractMarkdownFencePayload(remainder, fence)
			}
		}
	}

	if triggerSignal != "" {
		if idx := strings.Index(cleaned, triggerSignal); idx >= 0 {
			cleaned = cleaned[idx+len(triggerSignal):]
		}
	}
	cleaned = strings.TrimLeft(cleaned, " \t\n")
	if cleaned == "" {
		return "", false
	}

	if fence, info, remainder, ok := parseMarkdownFenceHeader(cleaned); ok {
		if isMarkdownToolFenceInfo(info) {
			return extractMarkdownFencePayload(remainder, fence)
		}
	}

	return extractMarkdownAliasPayload(cleaned)
}

func parseMarkdownFenceHeader(content string) (string, string, string, bool) {
	line, remainder, ok := splitFirstLine(content)
	if !ok {
		return "", "", "", false
	}

	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "```"):
		return "```", strings.TrimSpace(trimmed[3:]), remainder, true
	case strings.HasPrefix(trimmed, "~~~"):
		return "~~~", strings.TrimSpace(trimmed[3:]), remainder, true
	default:
		return "", "", "", false
	}
}

func splitFirstLine(content string) (string, string, bool) {
	if content == "" {
		return "", "", false
	}
	if idx := strings.IndexByte(content, '\n'); idx >= 0 {
		return content[:idx], content[idx+1:], true
	}
	return content, "", true
}

func isMarkdownToolFenceInfo(info string) bool {
	value := strings.ToLower(strings.TrimSpace(info))
	return value == "toolcalls" || value == "toolcall" || value == "tool"
}

func extractMarkdownFencePayload(content, fence string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	body := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == fence {
			return strings.TrimRight(strings.Join(body, "\n"), "\n"), true
		}
		body = append(body, line)
	}
	return "", false
}

func extractMarkdownAliasPayload(content string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	body := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "```" || trimmed == "~~~" {
			return strings.TrimRight(strings.Join(body, "\n"), "\n"), true
		}
		body = append(body, line)
	}
	return "", false
}

func parseMarkdownToolPayload(payload string) []protocol.ParsedToolCall {
	lines := strings.Split(strings.ReplaceAll(payload, "\r\n", "\n"), "\n")
	parsed := make([]protocol.ParsedToolCall, 0)
	var (
		currentName    string
		currentArgs    map[string]any
		pendingArgKey  string
		pendingArgJSON bool
		pendingArgYAML bool
		pendingArgText []string
	)

	flushPendingArg := func() bool {
		if pendingArgKey == "" {
			return true
		}
		text := strings.Join(normalizeMarkdownMultilineValue(pendingArgText, pendingArgYAML), "\n")
		value := any(text)
		if pendingArgJSON {
			value = coerceJSON(text)
		}
		if !assignMarkdownArgument(currentArgs, pendingArgKey, value) {
			return false
		}
		pendingArgKey = ""
		pendingArgJSON = false
		pendingArgYAML = false
		pendingArgText = nil
		return true
	}

	flushCurrentCall := func() bool {
		if currentName == "" {
			return true
		}
		if !flushPendingArg() {
			return false
		}
		parsed = append(parsed, protocol.ParsedToolCall{
			Name: currentName,
			Args: currentArgs,
		})
		currentName = ""
		currentArgs = nil
		return true
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if pendingArgKey != "" {
			if trimmed == "" {
				pendingArgText = append(pendingArgText, "")
				continue
			}
			if pendingArgYAML {
				if hasLeadingIndent(line) {
					pendingArgText = append(pendingArgText, line)
					continue
				}
				if !flushPendingArg() {
					return nil
				}
			}
			if continuation, ok := markdownArgContinuation(line); ok {
				pendingArgText = append(pendingArgText, continuation)
				continue
			}
			if !flushPendingArg() {
				return nil
			}
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if name, ok := parseMarkdownCallName(trimmed); ok {
			if !flushCurrentCall() {
				return nil
			}
			currentName = name
			currentArgs = map[string]any{}
			continue
		}

		if currentName == "" {
			return nil
		}

		key, value, multiline, jsonMode, yamlBlock, ok := parseMarkdownArgument(trimmed)
		if !ok {
			return nil
		}
		if multiline {
			pendingArgKey = key
			pendingArgJSON = jsonMode
			pendingArgYAML = yamlBlock
			pendingArgText = nil
			continue
		}
		if jsonMode {
			if !assignMarkdownArgument(currentArgs, key, coerceJSON(value)) {
				return nil
			}
			continue
		}
		if !assignMarkdownArgument(currentArgs, key, coerceJSON(value)) {
			return nil
		}
	}

	if !flushCurrentCall() {
		return nil
	}
	return parsed
}

func parseMarkdownCallName(line string) (string, bool) {
	switch {
	case strings.HasPrefix(strings.ToLower(line), "call "):
		name := strings.TrimSpace(line[len("call "):])
		return name, name != ""
	case strings.HasPrefix(strings.ToLower(line), "call:"):
		name := strings.TrimSpace(line[len("call:"):])
		return name, name != ""
	default:
		return "", false
	}
}

func parseMarkdownArgument(line string) (string, string, bool, bool, bool, bool) {
	var remainder string
	switch {
	case strings.HasPrefix(strings.ToLower(line), "arg "):
		remainder = strings.TrimSpace(line[len("arg "):])
	case strings.HasPrefix(strings.ToLower(line), "arg:"):
		remainder = strings.TrimSpace(line[len("arg:"):])
	default:
		return "", "", false, false, false, false
	}

	colon := strings.Index(remainder, ":")
	if colon < 0 {
		return "", "", false, false, false, false
	}

	key := strings.TrimSpace(remainder[:colon])
	value := strings.TrimSpace(remainder[colon+1:])
	if key == "" {
		return "", "", false, false, false, false
	}

	jsonMode := strings.HasSuffix(key, "@json")
	if jsonMode {
		key = strings.TrimSuffix(key, "@json")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false, false, false, false
	}

	if value == "" {
		return key, "", true, jsonMode, false, true
	}
	if value == "|" || value == "|-" || value == "|+" {
		return key, "", true, jsonMode, true, true
	}
	return key, value, false, jsonMode, false, true
}

func markdownArgContinuation(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "|") {
		return "", false
	}
	value := strings.TrimPrefix(trimmed, "|")
	if strings.HasPrefix(value, " ") {
		value = value[1:]
	}
	return value, true
}

func parseMarkdownBoundedArgumentHeader(line string, allowedTopLevel map[string]struct{}) (string, string, bool) {
	if line == "" || hasLeadingIndent(line) || !strings.HasPrefix(strings.ToLower(line), "arg_") {
		return "", "", false
	}

	remainder := line[len("arg_"):]
	colon := strings.Index(remainder, ":")
	if colon < 0 {
		return "", "", false
	}

	key := strings.TrimSpace(remainder[:colon])
	if key == "" || !isRecognizedMarkdownArgumentKey(key, allowedTopLevel) {
		return "", "", false
	}
	return key, strings.TrimSpace(remainder[colon+1:]), true
}

func markdownAllowedArgsByTool(tools []protocol.Tool) map[string]map[string]struct{} {
	allowed := make(map[string]map[string]struct{}, len(tools))
	for _, tool := range tools {
		params := map[string]struct{}{}
		properties, _ := normalizedToolParameters(tool.Function.Parameters)["properties"].(map[string]any)
		for name := range properties {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				params[trimmed] = struct{}{}
			}
		}
		allowed[tool.Function.Name] = params
	}
	return allowed
}

func isRecognizedMarkdownArgumentKey(key string, allowedTopLevel map[string]struct{}) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	if len(allowedTopLevel) == 0 {
		return true
	}

	base := strings.TrimSpace(key)
	if strings.HasSuffix(base, "@json") {
		base = strings.TrimSuffix(base, "@json")
	}
	if strings.HasSuffix(base, "[]") {
		base = strings.TrimSuffix(base, "[]")
	}
	if idx := strings.Index(base, "."); idx >= 0 {
		base = base[:idx]
	}
	_, ok := allowedTopLevel[base]
	return ok
}

func buildMarkdownBoundedArgumentValue(lines []string, hasInlineValue bool) string {
	if len(lines) == 0 {
		return ""
	}

	if hasInlineValue {
		if len(lines) == 1 {
			return lines[0]
		}
		rest := normalizeMarkdownMultilineValue(trimTrailingBlankLines(lines[1:]), true)
		return strings.TrimRight(strings.Join(append([]string{lines[0]}, rest...), "\n"), "\n")
	}

	normalized := normalizeMarkdownMultilineValue(trimTrailingBlankLines(lines), true)
	return strings.TrimRight(strings.Join(normalized, "\n"), "\n")
}

func trimTrailingBlankLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}

func coerceMarkdownBoundedArgumentValue(key, value string) any {
	if strings.HasSuffix(strings.TrimSpace(key), "@json") {
		return coerceJSON(value)
	}
	if strings.Contains(value, "\n") {
		return value
	}
	return coerceJSON(value)
}

func normalizeMarkdownBoundedArgumentKey(key string) string {
	normalized := strings.TrimSpace(key)
	if strings.HasSuffix(normalized, "@json") {
		normalized = strings.TrimSuffix(normalized, "@json")
	}
	return normalized
}

func normalizeMarkdownMultilineValue(lines []string, yamlBlock bool) []string {
	if !yamlBlock || len(lines) == 0 {
		return lines
	}

	minIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := leadingIndentWidth(line)
		if minIndent < 0 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent <= 0 {
		return lines
	}

	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			normalized = append(normalized, "")
			continue
		}
		normalized = append(normalized, trimLeadingIndent(line, minIndent))
	}
	return normalized
}

func hasLeadingIndent(line string) bool {
	if line == "" {
		return false
	}
	return line[0] == ' ' || line[0] == '\t'
}

func leadingIndentWidth(line string) int {
	width := 0
	for width < len(line) {
		if line[width] != ' ' && line[width] != '\t' {
			break
		}
		width++
	}
	return width
}

func trimLeadingIndent(line string, width int) string {
	if width <= 0 {
		return line
	}
	index := 0
	for index < len(line) && width > 0 {
		if line[index] != ' ' && line[index] != '\t' {
			break
		}
		index++
		width--
	}
	return line[index:]
}

func assignMarkdownArgument(target map[string]any, key string, value any) bool {
	if target == nil {
		return false
	}

	parts := strings.Split(key, ".")
	current := target
	for _, part := range parts[:len(parts)-1] {
		part = strings.TrimSpace(part)
		if part == "" {
			return false
		}
		existing, ok := current[part]
		if !ok {
			child := map[string]any{}
			current[part] = child
			current = child
			continue
		}
		child, ok := existing.(map[string]any)
		if !ok {
			return false
		}
		current = child
	}

	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "" {
		return false
	}
	appendMode := strings.HasSuffix(last, "[]")
	if appendMode {
		last = strings.TrimSuffix(last, "[]")
	}
	if last == "" {
		return false
	}

	existing, exists := current[last]
	switch {
	case appendMode && !exists:
		current[last] = []any{value}
	case appendMode:
		list, ok := existing.([]any)
		if !ok {
			return false
		}
		current[last] = append(list, value)
	case exists:
		switch typed := existing.(type) {
		case []any:
			current[last] = append(typed, value)
		default:
			current[last] = []any{typed, value}
		}
	default:
		current[last] = value
	}
	return true
}

type invokeXML struct {
	XMLName    xml.Name       `xml:"invoke"`
	Name       string         `xml:"name,attr"`
	NameNode   string         `xml:"name"`
	Parameters []parameterXML `xml:"parameter"`
}

type parameterXML struct {
	Name     string `xml:"name,attr"`
	InnerXML string `xml:",innerxml"`
}

func parseInvokeXML(invoke string) (protocol.ParsedToolCall, bool) {
	var root invokeXML
	if err := xml.Unmarshal([]byte(invoke), &root); err != nil {
		return protocol.ParsedToolCall{}, false
	}

	name := strings.TrimSpace(root.Name)
	if name == "" {
		name = strings.TrimSpace(root.NameNode)
	}
	if name == "" {
		return protocol.ParsedToolCall{}, false
	}

	args := map[string]any{}
	for _, param := range root.Parameters {
		paramName := strings.TrimSpace(param.Name)
		paramValue := strings.TrimSpace(extractXMLText(param.InnerXML))
		if paramName == "" {
			paramName = "param_" + strconv.Itoa(len(args)+1)
		}
		args[paramName] = coerceJSON(paramValue)
	}

	return protocol.ParsedToolCall{
		Name: html.UnescapeString(name),
		Args: args,
	}, true
}

func extractXMLText(inner string) string {
	if strings.TrimSpace(inner) == "" {
		return ""
	}

	wrapped := "<root>" + inner + "</root>"
	decoder := xml.NewDecoder(strings.NewReader(wrapped))
	var builder strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch value := token.(type) {
		case xml.CharData:
			builder.Write([]byte(value))
		}
	}
	return html.UnescapeString(strings.TrimSpace(builder.String()))
}

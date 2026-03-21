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
		if parsed := parseMarkdownToolPayloadMBArgs(payload, tools); len(parsed) > 0 {
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

func parseMarkdownToolPayloadMBArgs(payload string, tools []protocol.Tool) []protocol.ParsedToolCall {
	payload = strings.ReplaceAll(strings.ReplaceAll(payload, "\r\n", "\n"), "\r", "\n")
	tokens := tokenizeMarkdownMBControlTokens(payload)
	if len(tokens) == 0 {
		return nil
	}
	if strings.TrimSpace(payload[:tokens[0].start]) != "" {
		return nil
	}

	allowedArgs := markdownAllowedArgsByTool(tools)
	parsed := make([]protocol.ParsedToolCall, 0)
	var (
		currentName string
		currentArgs map[string]any
	)

	for index, token := range tokens {
		nextStart := len(payload)
		if index+1 < len(tokens) {
			nextStart = tokens[index+1].start
		}
		body := payload[token.bodyStart:nextStart]

		switch token.kind {
		case markdownMBControlKindCall:
			if currentName != "" {
				parsed = append(parsed, protocol.ParsedToolCall{
					Name: currentName,
					Args: currentArgs,
				})
			}
			currentName = strings.TrimSpace(body)
			if currentName == "" {
				return nil
			}
			currentArgs = map[string]any{}
		case markdownMBControlKindArg:
			if currentName == "" {
				return nil
			}
			if !isRecognizedMarkdownArgumentKey(token.arg.key, allowedArgs[currentName]) {
				return nil
			}
			lines := markdownMBValueLines(body)
			text := strings.Join(lines, "\n")
			value := any(text)
			if token.arg.jsonMode {
				value = coerceJSON(text)
			} else if len(lines) <= 1 {
				value = coerceJSON(text)
			}
			if !assignMarkdownArgument(currentArgs, token.arg.key, value) {
				return nil
			}
		default:
			return nil
		}
	}

	if currentName == "" {
		return nil
	}
	parsed = append(parsed, protocol.ParsedToolCall{
		Name: currentName,
		Args: currentArgs,
	})
	return parsed
}

func trimTrailingEmptyLines(lines []string) []string {
	end := len(lines)
	for end > 0 && lines[end-1] == "" {
		end--
	}
	return lines[:end]
}

type markdownMBControlKind uint8

const (
	markdownMBControlKindCall markdownMBControlKind = iota + 1
	markdownMBControlKindArg
)

type markdownMBArgHeader struct {
	key         string
	inlineValue string
	jsonMode    bool
}

type markdownMBControlToken struct {
	kind      markdownMBControlKind
	start     int
	bodyStart int
	arg       markdownMBArgHeader
}

func tokenizeMarkdownMBControlTokens(payload string) []markdownMBControlToken {
	if payload == "" {
		return nil
	}

	lowered := strings.ToLower(payload)
	tokens := make([]markdownMBControlToken, 0)
	for offset := 0; offset < len(payload); {
		callIndex := strings.Index(lowered[offset:], "mbcall:")
		argIndex := strings.Index(lowered[offset:], "mbarg[")
		if callIndex < 0 && argIndex < 0 {
			break
		}

		nextStart := len(payload)
		nextKind := markdownMBControlKind(0)
		if callIndex >= 0 {
			nextStart = offset + callIndex
			nextKind = markdownMBControlKindCall
		}
		if argIndex >= 0 && (nextKind == 0 || offset+argIndex < nextStart) {
			nextStart = offset + argIndex
			nextKind = markdownMBControlKindArg
		}

		switch nextKind {
		case markdownMBControlKindCall:
			tokens = append(tokens, markdownMBControlToken{
				kind:      markdownMBControlKindCall,
				start:     nextStart,
				bodyStart: nextStart + len("mbcall:"),
			})
			offset = nextStart + len("mbcall:")
		case markdownMBControlKindArg:
			token, ok := parseMarkdownMBArgToken(payload, lowered, nextStart)
			if !ok {
				offset = nextStart + 1
				continue
			}
			tokens = append(tokens, token)
			offset = token.bodyStart
		default:
			return tokens
		}
	}
	return tokens
}

func parseMarkdownMBArgToken(payload, lowered string, start int) (markdownMBControlToken, bool) {
	if start < 0 || start >= len(payload) || !strings.HasPrefix(lowered[start:], "mbarg[") {
		return markdownMBControlToken{}, false
	}

	remainder := payload[start+len("mbarg["):]
	keyEnd := strings.Index(remainder, "]:")
	if keyEnd < 0 {
		return markdownMBControlToken{}, false
	}

	key := strings.TrimSpace(remainder[:keyEnd])
	if key == "" {
		return markdownMBControlToken{}, false
	}
	jsonMode := strings.HasSuffix(key, "@json")
	if jsonMode {
		key = strings.TrimSpace(strings.TrimSuffix(key, "@json"))
	}
	if key == "" {
		return markdownMBControlToken{}, false
	}

	bodyStart := start + len("mbarg[") + keyEnd + 2
	if bodyStart < len(payload) && payload[bodyStart] == ' ' {
		bodyStart++
	}

	return markdownMBControlToken{
		kind:      markdownMBControlKindArg,
		start:     start,
		bodyStart: bodyStart,
		arg: markdownMBArgHeader{
			key:      key,
			jsonMode: jsonMode,
		},
	}, true
}

func markdownMBValueLines(segment string) []string {
	if strings.HasPrefix(segment, "\n") {
		segment = segment[1:]
	}
	if segment == "" {
		return nil
	}
	return trimTrailingEmptyLines(strings.Split(segment, "\n"))
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
	if payload, ok := extractMarkdownNamedFencePayload(cleaned); ok {
		return payload, true
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

func extractMarkdownNamedFencePayload(content string) (string, bool) {
	lowered := strings.ToLower(content)
	for _, fence := range []string{"```", "~~~"} {
		opener := fence + "mbtoolcalls"
		if idx := strings.Index(lowered, opener); idx >= 0 {
			remainder := content[idx+len(opener):]
			if strings.HasPrefix(remainder, "\n") {
				remainder = remainder[1:]
			}
			return extractMarkdownFencePayload(remainder, fence)
		}
	}
	return "", false
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
	return value == "mbtoolcalls"
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

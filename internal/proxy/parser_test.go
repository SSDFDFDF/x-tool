package proxy

import (
	"testing"

	"x-tool/internal/config"
	"x-tool/internal/protocol"
)

func TestParseFunctionCallsXMLUsesXMLParserBehavior(t *testing.T) {
	trigger := "<Function_Test_Start>"
	input := trigger + `
<invoke name="Write">
  <parameter name="content">foo <b>bar</b> baz</parameter>
  <parameter>123</parameter>
</invoke>`

	parsed := ParseFunctionCallsXML(input, trigger)
	if len(parsed) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(parsed))
	}
	if parsed[0].Name != "Write" {
		t.Fatalf("expected tool name Write, got %q", parsed[0].Name)
	}
	if parsed[0].Args["content"] != "foo bar baz" {
		t.Fatalf("expected XML text extraction, got %#v", parsed[0].Args["content"])
	}
	if parsed[0].Args["param_2"] != float64(123) {
		t.Fatalf("expected unnamed parameter fallback, got %#v", parsed[0].Args["param_2"])
	}
}

func TestStreamingFunctionCallDetectorPreservesUTF8AcrossChunks(t *testing.T) {
	trigger := "<Function_Test_Start>"
	detector := NewStreamingFunctionCallDetector(trigger, config.SoftToolProtocolXML)

	chunks := []string{
		"根",
		"据系统配置，我当前",
		"可用的工具列表如下：",
	}

	var got string
	for _, chunk := range chunks {
		detected, content := detector.ProcessChunk(chunk)
		if detected {
			t.Fatalf("did not expect tool detection for chunk %q", chunk)
		}
		got += content
	}

	got += detector.Buffer()

	want := "根据系统配置，我当前可用的工具列表如下："
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestParseFunctionCallsSentinelJSONParsesSingleCall(t *testing.T) {
	trigger := "<Function_Test_Start>"
	input := trigger + `
<TOOL_CALL>
{"name":"search","arguments":{"query":"weather"}}
</TOOL_CALL>`

	parsed := ParseFunctionCallsSentinelJSON(input, trigger)
	if len(parsed) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(parsed))
	}
	if parsed[0].Name != "search" {
		t.Fatalf("expected tool name search, got %q", parsed[0].Name)
	}
	if parsed[0].Args["query"] != "weather" {
		t.Fatalf("expected query argument, got %#v", parsed[0].Args["query"])
	}
}

func TestParseFunctionCallsSentinelJSONRepairsMalformedSingleCall(t *testing.T) {
	trigger := "<Function_Test_Start>"
	input := trigger + `
<TOOL_CALL>
{name:'search',arguments:{query:'weather',},}
</TOOL_CALL>`

	parsed := ParseFunctionCallsSentinelJSON(input, trigger)
	if len(parsed) != 1 {
		t.Fatalf("expected 1 repaired tool call, got %#v", parsed)
	}
	if parsed[0].Name != "search" {
		t.Fatalf("expected tool name search, got %q", parsed[0].Name)
	}
	if parsed[0].Args["query"] != "weather" {
		t.Fatalf("expected repaired query argument, got %#v", parsed[0].Args["query"])
	}
}

func TestParseFunctionCallsSentinelJSONRepairsMalformedMultipleCalls(t *testing.T) {
	trigger := "<Function_Test_Start>"
	input := trigger + `
<TOOL_CALLS>
[{name:'search',arguments:{query:'weather'}},{name:'write_file',arguments:{path:'/tmp/out.txt',content:'hello',},},]
</TOOL_CALLS>`

	parsed := ParseFunctionCallsSentinelJSON(input, trigger)
	if len(parsed) != 2 {
		t.Fatalf("expected 2 repaired tool calls, got %#v", parsed)
	}
	if parsed[0].Name != "search" || parsed[1].Name != "write_file" {
		t.Fatalf("expected search then write_file, got %#v", parsed)
	}
	if parsed[0].Args["query"] != "weather" {
		t.Fatalf("expected repaired first tool query argument, got %#v", parsed[0].Args["query"])
	}
	if parsed[1].Args["path"] != "/tmp/out.txt" || parsed[1].Args["content"] != "hello" {
		t.Fatalf("expected repaired second tool arguments, got %#v", parsed[1].Args)
	}
}

func TestParseFunctionCallsXMLParsesMultipleInvokesFromFunctionCallsBlock(t *testing.T) {
	trigger := "<Function_Test_Start>"
	input := trigger + `
<function_calls>
  <invoke name="search">
    <parameter name="query">weather</parameter>
  </invoke>
  <invoke name="write_file">
    <parameter name="path">/tmp/out.txt</parameter>
  </invoke>
</function_calls>`

	parsed := ParseFunctionCallsXML(input, trigger)
	if len(parsed) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(parsed))
	}
	if parsed[0].Name != "search" || parsed[1].Name != "write_file" {
		t.Fatalf("expected search then write_file, got %#v", parsed)
	}
	if parsed[0].Args["query"] != "weather" {
		t.Fatalf("expected first tool query argument, got %#v", parsed[0].Args["query"])
	}
	if parsed[1].Args["path"] != "/tmp/out.txt" {
		t.Fatalf("expected second tool path argument, got %#v", parsed[1].Args["path"])
	}
}

func TestParseFunctionCallsXMLParsesLegacyBareMultipleInvokes(t *testing.T) {
	trigger := "<Function_Test_Start>"
	input := trigger + `
<invoke name="search">
  <parameter name="query">weather</parameter>
</invoke>
<invoke name="write_file">
  <parameter name="path">/tmp/out.txt</parameter>
</invoke>`

	parsed := ParseFunctionCallsXML(input, trigger)
	if len(parsed) != 2 {
		t.Fatalf("expected 2 legacy tool calls, got %d", len(parsed))
	}
	if parsed[0].Name != "search" || parsed[1].Name != "write_file" {
		t.Fatalf("expected search then write_file, got %#v", parsed)
	}
}

func TestParseFunctionCallsMarkdownBlockParsesCanonicalFence(t *testing.T) {
	trigger := "<Function_Test_Start>"
	input := trigger + "\n```mbtoolcalls\n" +
		"mbcall: search\n" +
		"mbarg[query]: weather\n" +
		"mbarg[filters.city]: shanghai\n" +
		"mbarg[tags[]]: news\n" +
		"mbarg[tags[]]: local\n\n" +
		"mbcall: write_file\n" +
		"mbarg[path]: /tmp/out.txt\n" +
		"mbarg[content]:\n" +
		"line 1\n" +
		"line 2\n" +
		"```\n"

	parsed := ParseFunctionCallsMarkdownBlock(input, trigger)
	if len(parsed) != 2 {
		t.Fatalf("expected 2 markdown tool calls, got %#v", parsed)
	}
	if parsed[0].Name != "search" || parsed[1].Name != "write_file" {
		t.Fatalf("expected search then write_file, got %#v", parsed)
	}
	filters, _ := parsed[0].Args["filters"].(map[string]any)
	if parsed[0].Args["query"] != "weather" || filters["city"] != "shanghai" {
		t.Fatalf("expected nested query args, got %#v", parsed[0].Args)
	}
	tags, _ := parsed[0].Args["tags"].([]any)
	if len(tags) != 2 || tags[0] != "news" || tags[1] != "local" {
		t.Fatalf("expected repeated [] args to become array, got %#v", parsed[0].Args["tags"])
	}
	if parsed[1].Args["content"] != "line 1\nline 2" {
		t.Fatalf("expected multiline arg payload, got %#v", parsed[1].Args["content"])
	}
}

func TestParseFunctionCallsMarkdownBlockParsesFenceInfoAlias(t *testing.T) {
	trigger := "<Function_Test_Start>"
	input := "```" + trigger + "\n" +
		"mbcall: search\n" +
		"mbarg[options@json]: {\"query\":\"weather\",\"limit\":3}\n" +
		"```"

	parsed := ParseFunctionCallsMarkdownBlock(input, trigger)
	if len(parsed) != 1 {
		t.Fatalf("expected 1 markdown alias tool call, got %#v", parsed)
	}
	options, _ := parsed[0].Args["options"].(map[string]any)
	if parsed[0].Name != "search" || options["query"] != "weather" || options["limit"] != float64(3) {
		t.Fatalf("expected JSON arg parsing from alias fence, got %#v", parsed)
	}
}

func TestParseFunctionCallsMarkdownBlockParsesMultilineArgument(t *testing.T) {
	trigger := "<Function_Test_Start>"
	input := trigger + "\n```mbtoolcalls\n" +
		"mbcall: Agent\n" +
		"mbarg[description]: 检查代码冗余问题\n" +
		"mbarg[prompt]:\n" +
		"请全面分析这个代码库中的冗余问题，包括：\n" +
		"\n" +
		"1. 重复代码\n" +
		"2. 重复逻辑\n" +
		"mbarg[subagent_type]: Explore\n" +
		"```\n"

	parsed := ParseFunctionCallsMarkdownBlock(input, trigger)
	if len(parsed) != 1 {
		t.Fatalf("expected 1 markdown tool call, got %#v", parsed)
	}
	if parsed[0].Name != "Agent" {
		t.Fatalf("expected Agent tool call, got %#v", parsed)
	}
	if parsed[0].Args["description"] != "检查代码冗余问题" {
		t.Fatalf("expected description arg, got %#v", parsed[0].Args["description"])
	}
	if parsed[0].Args["prompt"] != "请全面分析这个代码库中的冗余问题，包括：\n\n1. 重复代码\n2. 重复逻辑" {
		t.Fatalf("expected multiline prompt arg, got %#v", parsed[0].Args["prompt"])
	}
	if parsed[0].Args["subagent_type"] != "Explore" {
		t.Fatalf("expected trailing arg after multiline block, got %#v", parsed[0].Args["subagent_type"])
	}
}

func TestParseFunctionCallsMarkdownBlockParsesInlineFirstLineWithContinuation(t *testing.T) {
	trigger := "<Function_Test_Start>"
	input := trigger + "\n```mbtoolcalls\n" +
		"mbcall: Agent\n" +
		"mbarg[description]: 探索前端代码冗余\n" +
		"mbarg[prompt]: 探索这个项目的前端代码结构，识别以下类型的冗余：\n" +
		"\n" +
		"1. 重复的组件或函数\n" +
		"2. 未使用的导入和变量\n" +
		"3. 相似逻辑的重复实现\n" +
		"4. 可以合并的重复样式或配置\n" +
		"5. 死代码（未使用的文件或导出）\n" +
		"\n" +
		"请先确定前端代码的位置（可能在 src、web、frontend、client 等目录），然后进行彻底的分析。提供具体的文件路径和代码示例来说明发现的问题。\n" +
		"mbarg[subagent_type]: Explore\n" +
		"```\n"

	parsed := ParseFunctionCallsMarkdownBlock(input, trigger)
	if len(parsed) != 1 {
		t.Fatalf("expected 1 markdown tool call, got %#v", parsed)
	}
	if parsed[0].Args["prompt"] != "探索这个项目的前端代码结构，识别以下类型的冗余：\n\n1. 重复的组件或函数\n2. 未使用的导入和变量\n3. 相似逻辑的重复实现\n4. 可以合并的重复样式或配置\n5. 死代码（未使用的文件或导出）\n\n请先确定前端代码的位置（可能在 src、web、frontend、client 等目录），然后进行彻底的分析。提供具体的文件路径和代码示例来说明发现的问题。" {
		t.Fatalf("expected inline-first-line prompt arg, got %#v", parsed[0].Args["prompt"])
	}
	if parsed[0].Args["subagent_type"] != "Explore" {
		t.Fatalf("expected subagent_type arg, got %#v", parsed[0].Args["subagent_type"])
	}
}

func TestParseFunctionCallsMarkdownBlockParsesGluedArgumentMarker(t *testing.T) {
	trigger := "<Function_Test_Start>"
	input := trigger + "\n```mbtoolcalls\n" +
		"mbcall: Edit\n" +
		"mbarg[file_path]: /tmp/example.txt\n" +
		"mbarg[new_string]:\n" +
		"line 1\n" +
		"line 2mbarg[old_string]:\n" +
		"original line\n" +
		"mbarg[replace_all]: false\n" +
		"```\n"

	tools := []protocol.Tool{
		{
			Type: "function",
			Function: protocol.ToolFunction{
				Name: "Edit",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_path":   map[string]any{"type": "string"},
						"new_string":  map[string]any{"type": "string"},
						"old_string":  map[string]any{"type": "string"},
						"replace_all": map[string]any{"type": "boolean"},
					},
					"required": []string{"file_path", "new_string", "old_string"},
				},
			},
		},
	}

	parsed := ParseFunctionCallsMarkdownBlockWithTools(input, trigger, tools)
	if len(parsed) != 1 {
		t.Fatalf("expected 1 markdown tool call, got %#v", parsed)
	}
	if parsed[0].Args["new_string"] != "line 1\nline 2" {
		t.Fatalf("expected glued old_string marker to split from new_string, got %#v", parsed[0].Args["new_string"])
	}
	if parsed[0].Args["old_string"] != "original line" {
		t.Fatalf("expected old_string arg, got %#v", parsed[0].Args["old_string"])
	}
	if parsed[0].Args["replace_all"] != false {
		t.Fatalf("expected replace_all false, got %#v", parsed[0].Args["replace_all"])
	}
}

func TestParseFunctionCallsMarkdownBlockParsesGluedCallMarker(t *testing.T) {
	trigger := "<Function_Test_Start>"
	input := trigger + "\n```mbtoolcalls\n" +
		"mbcall: search\n" +
		"mbarg[query]: weathermbcall: write_file\n" +
		"mbarg[path]: /tmp/out.txt\n" +
		"```\n"

	parsed := ParseFunctionCallsMarkdownBlock(input, trigger)
	if len(parsed) != 2 {
		t.Fatalf("expected 2 markdown tool calls, got %#v", parsed)
	}
	if parsed[0].Args["query"] != "weather" {
		t.Fatalf("expected first call query, got %#v", parsed[0].Args["query"])
	}
	if parsed[1].Name != "write_file" || parsed[1].Args["path"] != "/tmp/out.txt" {
		t.Fatalf("expected glued mbcall marker to start second call, got %#v", parsed[1])
	}
}

func TestParseFunctionCallsMarkdownBlockParsesMarkersWithoutNewlineBoundaries(t *testing.T) {
	trigger := "<Function_Test_Start>"
	input := trigger + "\n```mbtoolcalls\n" +
		"mbcall: searchmbarg[query]: weathermbcall: write_filembarg[path]: /tmp/out.txt\n" +
		"```\n"

	parsed := ParseFunctionCallsMarkdownBlock(input, trigger)
	if len(parsed) != 2 {
		t.Fatalf("expected 2 markdown tool calls, got %#v", parsed)
	}
	if parsed[0].Name != "search" || parsed[0].Args["query"] != "weather" {
		t.Fatalf("expected first call parsed without newline boundaries, got %#v", parsed[0])
	}
	if parsed[1].Name != "write_file" || parsed[1].Args["path"] != "/tmp/out.txt" {
		t.Fatalf("expected second call parsed without newline boundaries, got %#v", parsed[1])
	}
}

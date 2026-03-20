package proxy

import (
	"testing"

	"x-tool/internal/config"
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
	input := trigger + "\n```toolcalls\n" +
		"call search\n" +
		"arg_query: weather\n" +
		"arg_filters.city: shanghai\n" +
		"arg_tags[]: news\n" +
		"arg_tags[]: local\n\n" +
		"call write_file\n" +
		"arg_path: /tmp/out.txt\n" +
		"arg_content:\n" +
		"  line 1\n" +
		"  line 2\n" +
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
		"call search\n" +
		"arg_options@json:\n" +
		"  {\"query\":\"weather\",\"limit\":3}\n" +
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

func TestParseFunctionCallsMarkdownBlockParsesBoundedMultilineArg(t *testing.T) {
	trigger := "<Function_Test_Start>"
	input := trigger + "\n```toolcalls\n" +
		"call Agent\n" +
		"arg_description: 检查代码冗余问题\n" +
		"arg_prompt:\n" +
		"  请全面分析这个代码库中的冗余问题，包括：\n" +
		"  \n" +
		"  1. 重复代码\n" +
		"  2. 重复逻辑\n" +
		"arg_subagent_type: Explore\n" +
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
		t.Fatalf("expected bounded multiline prompt arg, got %#v", parsed[0].Args["prompt"])
	}
	if parsed[0].Args["subagent_type"] != "Explore" {
		t.Fatalf("expected trailing arg after multiline block, got %#v", parsed[0].Args["subagent_type"])
	}
}

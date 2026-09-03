package chat_completions

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIRequestToClaude_PreservesCursorNativeEnvelopeAndHistory(t *testing.T) {
	inputJSON := `{
		"model":"claude-sonnet-4-6",
		"system":[{"type":"text","text":"Use the repository instructions."}],
		"metadata":{"user_id":"user_hash_account_11111111-1111-4111-8111-111111111111_session_22222222-2222-4222-8222-222222222222"},
		"thinking":{"type":"adaptive"},
		"output_config":{"effort":"high"},
		"betas":["context-1m-2025-08-07"],
		"messages":[
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"Inspect the tree first.","signature":"sig_1"},
				{"type":"tool_use","id":"toolu_1","name":"Read","input":{"path":"README.md"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"toolu_1","content":"contents"}
			]}
		],
		"tools":[{"name":"Read","description":"Read a file","input_schema":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}],
		"tool_choice":{"type":"tool","name":"Read"}
	}`

	out := ConvertOpenAIRequestToClaude("claude-sonnet-4-6", []byte(inputJSON), false)
	assertJSONPathEquals(t, out, "system.0.text", "Use the repository instructions.")
	assertJSONPathEquals(t, out, "metadata.user_id", "user_hash_account_11111111-1111-4111-8111-111111111111_session_22222222-2222-4222-8222-222222222222")
	assertJSONPathEquals(t, out, "thinking.type", "adaptive")
	assertJSONPathEquals(t, out, "output_config.effort", "high")
	assertJSONPathEquals(t, out, "betas.0", "context-1m-2025-08-07")
	assertJSONPathEquals(t, out, "messages.0.content.0.type", "thinking")
	assertJSONPathEquals(t, out, "messages.0.content.0.signature", "sig_1")
	assertJSONPathEquals(t, out, "messages.0.content.1.type", "tool_use")
	assertJSONPathEquals(t, out, "messages.0.content.1.id", "toolu_1")
	assertJSONPathEquals(t, out, "messages.1.content.0.type", "tool_result")
	assertJSONPathEquals(t, out, "messages.1.content.0.tool_use_id", "toolu_1")
	assertJSONPathEquals(t, out, "tools.0.name", "Read")
	assertJSONPathEquals(t, out, "tools.0.input_schema.required.0", "path")
	assertJSONPathEquals(t, out, "tool_choice.type", "tool")
	assertJSONPathEquals(t, out, "tool_choice.name", "Read")
}

func TestConvertOpenAIRequestToClaude_PreservesAnthropicTypedTool(t *testing.T) {
	inputJSON := `{
		"model":"claude-sonnet-4-6",
		"messages":[{"role":"user","content":"Search the web."}],
		"tools":[{"type":"web_search_20250305","name":"web_search","max_uses":4}]
	}`

	out := ConvertOpenAIRequestToClaude("claude-sonnet-4-6", []byte(inputJSON), false)
	assertJSONPathEquals(t, out, "tools.0.type", "web_search_20250305")
	assertJSONPathEquals(t, out, "tools.0.name", "web_search")
	if got := gjson.GetBytes(out, "tools.0.max_uses").Int(); got != 4 {
		t.Fatalf("tools.0.max_uses = %d, want 4; output: %s", got, string(out))
	}
}

func assertJSONPathEquals(t *testing.T, raw []byte, path, want string) {
	t.Helper()
	if got := gjson.GetBytes(raw, path).String(); got != want {
		t.Fatalf("%s = %q, want %q; output: %s", path, got, want, string(raw))
	}
}

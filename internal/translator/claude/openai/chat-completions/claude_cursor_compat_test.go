package chat_completions

import (
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
)

func TestConvertOpenAIRequestToClaude_PreservesCursorNativeEnvelopeAndHistory(t *testing.T) {
	inputJSON := `{
		"model":"claude-sonnet-4-6",
		"system":[{"type":"text","text":"Use the repository instructions."}],
		"metadata":{"user_id":"user_hash_account_11111111-1111-4111-8111-111111111111_session_22222222-2222-4222-8222-222222222222"},
		"thinking":{"type":"adaptive"},
		"output_config":{"effort":"high"},
		"betas":["context-1m-2025-08-07"],
		"response_format":{"type":"json_object"},
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

	out, err := ConvertOpenAIRequestToClaude("claude-sonnet-4-6", []byte(inputJSON), false)

	if err != nil {
		t.Fatal(err)
	}
	assertJSONPathEquals(t, out, "system.0.text", "Use the repository instructions.")
	if instruction := ojson.GetBytes(out, "system.1.text").String(); !strings.Contains(instruction, "JSON object") {
		t.Fatalf("structured-output instruction was not appended after the native system block: %s", string(out))
	}
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

	out, err := ConvertOpenAIRequestToClaude("claude-sonnet-4-6", []byte(inputJSON), false)

	if err != nil {
		t.Fatal(err)
	}
	assertJSONPathEquals(t, out, "tools.0.type", "web_search_20250305")
	assertJSONPathEquals(t, out, "tools.0.name", "web_search")
	if got := ojson.GetBytes(out, "tools.0.max_uses").Int(); got != 4 {
		t.Fatalf("tools.0.max_uses = %d, want 4; output: %s", got, string(out))
	}
}

func assertJSONPathEquals(t *testing.T, raw []byte, path, want string) {
	t.Helper()
	if got := ojson.GetBytes(raw, path).String(); got != want {
		t.Fatalf("%s = %q, want %q; output: %s", path, got, want, string(raw))
	}
}

func TestConvertOpenAIRequestToClaude_CursorNativeChoicesRetainUpstreamNormalization(t *testing.T) {
	for _, tc := range []struct {
		choice, wantType string
		disableParallel  bool
	}{
		{`"auto"`, "auto", false},
		{`"required"`, "any", false},
		{`{"type":"auto","disable_parallel_tool_use":true}`, "auto", true},
		{`{"type":"tool","name":"Read","disable_parallel_tool_use":true}`, "tool", true},
	} {
		t.Run(tc.choice, func(t *testing.T) {
			input := []byte(`{"messages":[{"role":"user","content":"synthetic"}],"tools":[{"name":"Read","input_schema":{"type":"object"}}],"tool_choice":` + tc.choice + `}`)
			out, err := ConvertOpenAIRequestToClaude("claude-sonnet-4-6", input, false)
			if err != nil {
				t.Fatal(err)
			}
			if !ojson.GetBytes(out, "tool_choice").IsObject() {
				t.Fatalf("tool choice was not normalized: %s", out)
			}
			assertJSONPathEquals(t, out, "tool_choice.type", tc.wantType)
			if ojson.GetBytes(out, "tool_choice.disable_parallel_tool_use").Bool() != tc.disableParallel {
				t.Fatalf("native choice option lost: %s", out)
			}
		})
	}
}

func TestConvertOpenAIRequestToClaude_CursorNativeBlocksRequireCorrectRole(t *testing.T) {
	input := []byte(`{"messages":[{"role":"user","content":[{"type":"thinking","thinking":"must not be assistant reasoning","signature":"synthetic"},{"type":"tool_use","id":"wrong-role","name":"Read","input":{}},{"type":"text","text":"synthetic"}]},{"role":"assistant","content":[{"type":"tool_result","tool_use_id":"wrong-role","content":"invalid"},{"type":"text","text":"synthetic"}]}]}`)
	out, err := ConvertOpenAIRequestToClaude("claude-sonnet-4-6", input, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "wrong-role") || strings.Contains(string(out), "must not be assistant reasoning") {
		t.Fatalf("native block accepted in invalid role: %s", out)
	}
}

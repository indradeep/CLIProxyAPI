package chat_completions

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIRequestToClaude_SanitizesToolCallIDsForClaude(t *testing.T) {
	inputJSON := `{
		"model": "gpt-4.1",
		"messages": [
			{
				"role": "assistant",
				"tool_calls": [
					{
						"id": "call.with space:1",
						"type": "function",
						"function": {
							"name": "Read",
							"arguments": "{\"path\":\"README.md\"}"
						}
					}
				]
			},
			{
				"role": "tool",
				"tool_call_id": "call.with space:1",
				"content": "ok"
			}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-sonnet-4-5", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)
	toolUseID := resultJSON.Get("messages.0.content.0.id").String()
	toolResultID := resultJSON.Get("messages.1.content.0.tool_use_id").String()

	if toolUseID != "call_with_space_1" {
		t.Fatalf("tool_use id = %q, want %q", toolUseID, "call_with_space_1")
	}
	if toolResultID != toolUseID {
		t.Fatalf("tool_result tool_use_id = %q, want same sanitized id %q", toolResultID, toolUseID)
	}
}

func TestConvertOpenAIRequestToClaude_ToolResultTextAndBase64Image(t *testing.T) {
	inputJSON := `{
		"model": "gpt-4.1",
		"messages": [
			{
				"role": "assistant",
				"content": "",
				"tool_calls": [
					{
						"id": "call_1",
						"type": "function",
						"function": {
							"name": "do_work",
							"arguments": "{\"a\":1}"
						}
					}
				]
			},
			{
				"role": "tool",
				"tool_call_id": "call_1",
				"content": [
					{"type": "text", "text": "tool ok"},
					{
						"type": "image_url",
						"image_url": {
							"url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUg=="
						}
					}
				]
			}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-sonnet-4-5", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)
	messages := resultJSON.Get("messages").Array()

	if len(messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d. Messages: %s", len(messages), resultJSON.Get("messages").Raw)
	}

	toolResult := messages[1].Get("content.0")
	if got := toolResult.Get("type").String(); got != "tool_result" {
		t.Fatalf("Expected content[0].type %q, got %q", "tool_result", got)
	}
	if got := toolResult.Get("tool_use_id").String(); got != "call_1" {
		t.Fatalf("Expected tool_use_id %q, got %q", "call_1", got)
	}

	toolContent := toolResult.Get("content")
	if !toolContent.IsArray() {
		t.Fatalf("Expected tool_result content array, got %s", toolContent.Raw)
	}
	if got := toolContent.Get("0.type").String(); got != "text" {
		t.Fatalf("Expected first tool_result part type %q, got %q", "text", got)
	}
	if got := toolContent.Get("0.text").String(); got != "tool ok" {
		t.Fatalf("Expected first tool_result part text %q, got %q", "tool ok", got)
	}
	if got := toolContent.Get("1.type").String(); got != "image" {
		t.Fatalf("Expected second tool_result part type %q, got %q", "image", got)
	}
	if got := toolContent.Get("1.source.type").String(); got != "base64" {
		t.Fatalf("Expected image source type %q, got %q", "base64", got)
	}
	if got := toolContent.Get("1.source.media_type").String(); got != "image/png" {
		t.Fatalf("Expected image media type %q, got %q", "image/png", got)
	}
	if got := toolContent.Get("1.source.data").String(); got != "iVBORw0KGgoAAAANSUhEUg==" {
		t.Fatalf("Unexpected base64 image data: %q", got)
	}
}

func TestConvertOpenAIRequestToClaude_ToolResultURLImageOnly(t *testing.T) {
	inputJSON := `{
		"model": "gpt-4.1",
		"messages": [
			{
				"role": "assistant",
				"content": "",
				"tool_calls": [
					{
						"id": "call_1",
						"type": "function",
						"function": {
							"name": "do_work",
							"arguments": "{\"a\":1}"
						}
					}
				]
			},
			{
				"role": "tool",
				"tool_call_id": "call_1",
				"content": [
					{
						"type": "image_url",
						"image_url": {
							"url": "https://example.com/tool.png"
						}
					}
				]
			}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-sonnet-4-5", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)
	messages := resultJSON.Get("messages").Array()

	if len(messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d. Messages: %s", len(messages), resultJSON.Get("messages").Raw)
	}

	toolContent := messages[1].Get("content.0.content")
	if !toolContent.IsArray() {
		t.Fatalf("Expected tool_result content array, got %s", toolContent.Raw)
	}
	if got := toolContent.Get("0.type").String(); got != "image" {
		t.Fatalf("Expected tool_result part type %q, got %q", "image", got)
	}
	if got := toolContent.Get("0.source.type").String(); got != "url" {
		t.Fatalf("Expected image source type %q, got %q", "url", got)
	}
	if got := toolContent.Get("0.source.url").String(); got != "https://example.com/tool.png" {
		t.Fatalf("Unexpected image URL: %q", got)
	}
}

func TestConvertOpenAIRequestToClaude_SystemRoleBecomesTopLevelSystem(t *testing.T) {
	inputJSON := `{
		"model": "gpt-4.1",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."},
			{"role": "user", "content": "Hello"}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-sonnet-4-5", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)

	system := resultJSON.Get("system")
	if !system.IsArray() {
		t.Fatalf("Expected top-level system array, got %s", system.Raw)
	}
	if len(system.Array()) != 1 {
		t.Fatalf("Expected 1 system block, got %d. System: %s", len(system.Array()), system.Raw)
	}
	if got := system.Get("0.type").String(); got != "text" {
		t.Fatalf("Expected system block type %q, got %q", "text", got)
	}
	if got := system.Get("0.text").String(); got != "You are a helpful assistant." {
		t.Fatalf("Expected system text %q, got %q", "You are a helpful assistant.", got)
	}

	messages := resultJSON.Get("messages").Array()
	if len(messages) != 1 {
		t.Fatalf("Expected 1 non-system message, got %d. Messages: %s", len(messages), resultJSON.Get("messages").Raw)
	}
	if got := messages[0].Get("role").String(); got != "user" {
		t.Fatalf("Expected remaining message role %q, got %q", "user", got)
	}
	if got := messages[0].Get("content.0.text").String(); got != "Hello" {
		t.Fatalf("Expected user text %q, got %q", "Hello", got)
	}
}

func TestConvertOpenAIRequestToClaude_MultipleSystemMessagesMergedIntoTopLevelSystem(t *testing.T) {
	inputJSON := `{
		"model": "gpt-4.1",
		"messages": [
			{"role": "system", "content": "Rule 1"},
			{"role": "system", "content": [{"type": "text", "text": "Rule 2"}]},
			{"role": "user", "content": "Hello"}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-sonnet-4-5", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)

	system := resultJSON.Get("system").Array()
	if len(system) != 2 {
		t.Fatalf("Expected 2 system blocks, got %d. System: %s", len(system), resultJSON.Get("system").Raw)
	}
	if got := system[0].Get("text").String(); got != "Rule 1" {
		t.Fatalf("Expected first system text %q, got %q", "Rule 1", got)
	}
	if got := system[1].Get("text").String(); got != "Rule 2" {
		t.Fatalf("Expected second system text %q, got %q", "Rule 2", got)
	}

	messages := resultJSON.Get("messages").Array()
	if len(messages) != 1 {
		t.Fatalf("Expected 1 non-system message, got %d. Messages: %s", len(messages), resultJSON.Get("messages").Raw)
	}
	if got := messages[0].Get("role").String(); got != "user" {
		t.Fatalf("Expected remaining message role %q, got %q", "user", got)
	}
	if got := messages[0].Get("content.0.text").String(); got != "Hello" {
		t.Fatalf("Expected user text %q, got %q", "Hello", got)
	}
}

func TestConvertOpenAIRequestToClaude_SystemOnlyInputKeepsFallbackUserMessage(t *testing.T) {
	inputJSON := `{
		"model": "gpt-4.1",
		"messages": [
			{"role": "system", "content": "You are a helpful assistant."}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-sonnet-4-5", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)

	system := resultJSON.Get("system").Array()
	if len(system) != 1 {
		t.Fatalf("Expected 1 system block, got %d. System: %s", len(system), resultJSON.Get("system").Raw)
	}
	if got := system[0].Get("text").String(); got != "You are a helpful assistant." {
		t.Fatalf("Expected system text %q, got %q", "You are a helpful assistant.", got)
	}

	messages := resultJSON.Get("messages").Array()
	if len(messages) != 1 {
		t.Fatalf("Expected 1 fallback message, got %d. Messages: %s", len(messages), resultJSON.Get("messages").Raw)
	}
	if got := messages[0].Get("role").String(); got != "user" {
		t.Fatalf("Expected fallback message role %q, got %q", "user", got)
	}
	if got := messages[0].Get("content.0.type").String(); got != "text" {
		t.Fatalf("Expected fallback content type %q, got %q", "text", got)
	}
	if got := messages[0].Get("content.0.text").String(); got != "" {
		t.Fatalf("Expected fallback text %q, got %q", "", got)
	}
}

func TestConvertOpenAIRequestToClaude_CursorClaudeNativeToolsAndAutoChoiceSurvive(t *testing.T) {
	inputJSON := `{
		"model": "clip-claude-opus-4-8-xhigh",
		"tool_choice": {"type": "auto"},
		"tools": [
			{
				"name": "Shell",
				"description": "Run a shell command",
				"input_schema": {
					"type": "object",
					"properties": {
						"command": {"type": "string"}
					},
					"required": ["command"]
				},
				"cache_control": {"type": "ephemeral"}
			}
		],
		"messages": [
			{"role": "user", "content": "List files"}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-opus-4-8", []byte(inputJSON), true)
	resultJSON := gjson.ParseBytes(result)

	if got := resultJSON.Get("tools.0.name").String(); got != "Shell" {
		t.Fatalf("tools.0.name = %q, want Shell; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("tools.0.description").String(); got != "Run a shell command" {
		t.Fatalf("tools.0.description = %q, want Run a shell command; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("tools.0.input_schema.properties.command.type").String(); got != "string" {
		t.Fatalf("tools.0.input_schema.properties.command.type = %q, want string; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("tools.0.cache_control.type").String(); got != "ephemeral" {
		t.Fatalf("tools.0.cache_control.type = %q, want ephemeral; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("tool_choice.type").String(); got != "auto" {
		t.Fatalf("tool_choice.type = %q, want auto; payload=%s", got, string(result))
	}
}

func TestConvertOpenAIRequestToClaude_ClaudeNativeForcedToolChoiceSurvives(t *testing.T) {
	inputJSON := `{
		"model": "clip-claude-opus-4-8-xhigh",
		"tool_choice": {"type": "tool", "name": "Shell"},
		"tools": [
			{
				"name": "Shell",
				"description": "Run a shell command",
				"input_schema": {"type": "object", "properties": {"command": {"type": "string"}}}
			}
		],
		"messages": [
			{"role": "user", "content": "List files"}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-opus-4-8", []byte(inputJSON), true)
	resultJSON := gjson.ParseBytes(result)

	if got := resultJSON.Get("tool_choice.type").String(); got != "tool" {
		t.Fatalf("tool_choice.type = %q, want tool; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("tool_choice.name").String(); got != "Shell" {
		t.Fatalf("tool_choice.name = %q, want Shell; payload=%s", got, string(result))
	}
}

func TestConvertOpenAIRequestToClaude_OpenAIChatFunctionToolStillMaps(t *testing.T) {
	inputJSON := `{
		"model": "gpt-4.1",
		"tools": [
			{
				"type": "function",
				"function": {
					"name": "Shell",
					"description": "Run a shell command",
					"parameters": {
						"type": "object",
						"properties": {
							"command": {"type": "string"}
						},
						"required": ["command"]
					}
				}
			}
		],
		"messages": [
			{"role": "user", "content": "List files"}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-opus-4-8", []byte(inputJSON), true)
	resultJSON := gjson.ParseBytes(result)

	if got := resultJSON.Get("tools.0.name").String(); got != "Shell" {
		t.Fatalf("tools.0.name = %q, want Shell; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("tools.0.input_schema.properties.command.type").String(); got != "string" {
		t.Fatalf("tools.0.input_schema.properties.command.type = %q, want string; payload=%s", got, string(result))
	}
}

func TestConvertOpenAIRequestToClaude_OpenAIResponsesFunctionToolMapsOnClaudeRoute(t *testing.T) {
	inputJSON := `{
		"model": "clip-claude-opus-4-8-xhigh",
		"tools": [
			{
				"type": "function",
				"name": "Shell",
				"description": "Run a shell command",
				"parameters": {
					"type": "object",
					"properties": {
						"command": {"type": "string"}
					},
					"required": ["command"]
				}
			}
		],
		"messages": [
			{"role": "user", "content": "List files"}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-opus-4-8", []byte(inputJSON), true)
	resultJSON := gjson.ParseBytes(result)

	if got := resultJSON.Get("tools.0.name").String(); got != "Shell" {
		t.Fatalf("tools.0.name = %q, want Shell; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("tools.0.description").String(); got != "Run a shell command" {
		t.Fatalf("tools.0.description = %q, want Run a shell command; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("tools.0.input_schema.required.0").String(); got != "command" {
		t.Fatalf("tools.0.input_schema.required.0 = %q, want command; payload=%s", got, string(result))
	}
}

func TestConvertOpenAIRequestToClaude_AnthropicTypedToolSurvives(t *testing.T) {
	inputJSON := `{
		"model": "clip-claude-opus-4-8-xhigh",
		"tools": [
			{
				"type": "web_search_20250305",
				"name": "web_search",
				"cache_control": {"type": "ephemeral"}
			}
		],
		"messages": [
			{"role": "user", "content": "Search"}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-opus-4-8", []byte(inputJSON), true)
	resultJSON := gjson.ParseBytes(result)

	if got := resultJSON.Get("tools.0.type").String(); got != "web_search_20250305" {
		t.Fatalf("tools.0.type = %q, want web_search_20250305; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("tools.0.name").String(); got != "web_search" {
		t.Fatalf("tools.0.name = %q, want web_search; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("tools.0.cache_control.type").String(); got != "ephemeral" {
		t.Fatalf("tools.0.cache_control.type = %q, want ephemeral; payload=%s", got, string(result))
	}
}

func TestConvertOpenAIRequestToClaude_MalformedUnknownToolsAreDropped(t *testing.T) {
	inputJSON := `{
		"model": "clip-claude-opus-4-8-xhigh",
		"tools": [
			{"foo": "bar"},
			{"name": "NoSchema", "description": "missing schema"}
		],
		"messages": [
			{"role": "user", "content": "List files"}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-opus-4-8", []byte(inputJSON), true)
	resultJSON := gjson.ParseBytes(result)

	if tools := resultJSON.Get("tools"); tools.Exists() {
		t.Fatalf("tools should be absent for malformed unknown tools; tools=%s payload=%s", tools.Raw, string(result))
	}
}

func TestConvertOpenAIRequestToClaude_PreservesCursorClaudeNativeBlocksAndEnvelope(t *testing.T) {
	userID := "user_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef_account_11111111-1111-4111-8111-111111111111_session_22222222-2222-4222-8222-222222222222"
	inputJSON := `{
		"model": "clip-claude-opus-4-8-xhigh",
		"system": [
			{"type": "text", "text": "Cursor system", "cache_control": {"type": "ephemeral"}}
		],
		"metadata": {
			"user_id": "` + userID + `"
		},
		"thinking": {"type": "adaptive"},
		"output_config": {"effort": "high"},
		"betas": ["prompt-caching-scope-2026-01-05"],
		"tool_choice": {"type": "auto"},
		"tools": [
			{
				"name": "Read",
				"description": "Read a file",
				"input_schema": {
					"type": "object",
					"properties": {"file_path": {"type": "string"}},
					"required": ["file_path"]
				}
			}
		],
		"messages": [
			{
				"role": "assistant",
				"content": [
					{"type": "text", "text": "I will inspect it.", "cache_control": {"type": "ephemeral"}},
					{"type": "tool_use", "id": "toolu_cursor_1", "name": "Read", "input": {"file_path": "main.go"}}
				]
			},
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_cursor_1",
						"content": [{"type": "text", "text": "package main"}],
						"is_error": false,
						"cache_control": {"type": "ephemeral"}
					}
				]
			}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-opus-4-8", []byte(inputJSON), true)
	resultJSON := gjson.ParseBytes(result)

	if got := resultJSON.Get("metadata.user_id").String(); got != userID {
		t.Fatalf("metadata.user_id = %q, want %q; payload=%s", got, userID, string(result))
	}
	if got := resultJSON.Get("thinking.type").String(); got != "adaptive" {
		t.Fatalf("thinking.type = %q, want adaptive; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("output_config.effort").String(); got != "high" {
		t.Fatalf("output_config.effort = %q, want high; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("betas.0").String(); got != "prompt-caching-scope-2026-01-05" {
		t.Fatalf("betas.0 = %q, want prompt-caching-scope-2026-01-05; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("system.0.text").String(); got != "Cursor system" {
		t.Fatalf("system.0.text = %q, want Cursor system; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("system.0.cache_control.type").String(); got != "ephemeral" {
		t.Fatalf("system.0.cache_control.type = %q, want ephemeral; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("tools.0.name").String(); got != "Read" {
		t.Fatalf("tools.0.name = %q, want Read; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("tools.0.input_schema.properties.file_path.type").String(); got != "string" {
		t.Fatalf("tools.0.input_schema.properties.file_path.type = %q, want string; payload=%s", got, string(result))
	}
	if got := resultJSON.Get("tool_choice.type").String(); got != "auto" {
		t.Fatalf("tool_choice.type = %q, want auto; payload=%s", got, string(result))
	}

	assistantContent := resultJSON.Get("messages.0.content")
	if got := len(assistantContent.Array()); got != 2 {
		t.Fatalf("assistant content length = %d, want 2; content=%s", got, assistantContent.Raw)
	}
	if got := assistantContent.Get("0.cache_control.type").String(); got != "ephemeral" {
		t.Fatalf("assistant text cache_control = %q, want ephemeral; content=%s", got, assistantContent.Raw)
	}
	if got := assistantContent.Get("1.type").String(); got != "tool_use" {
		t.Fatalf("assistant content[1].type = %q, want tool_use; content=%s", got, assistantContent.Raw)
	}
	if got := assistantContent.Get("1.id").String(); got != "toolu_cursor_1" {
		t.Fatalf("tool_use id = %q, want toolu_cursor_1; content=%s", got, assistantContent.Raw)
	}
	if got := assistantContent.Get("1.name").String(); got != "Read" {
		t.Fatalf("tool_use name = %q, want Read; content=%s", got, assistantContent.Raw)
	}
	if got := assistantContent.Get("1.input.file_path").String(); got != "main.go" {
		t.Fatalf("tool_use input.file_path = %q, want main.go; content=%s", got, assistantContent.Raw)
	}

	userContent := resultJSON.Get("messages.1.content")
	if got := len(userContent.Array()); got != 1 {
		t.Fatalf("user content length = %d, want 1; content=%s", got, userContent.Raw)
	}
	if got := userContent.Get("0.type").String(); got != "tool_result" {
		t.Fatalf("user content[0].type = %q, want tool_result; content=%s", got, userContent.Raw)
	}
	if got := userContent.Get("0.tool_use_id").String(); got != "toolu_cursor_1" {
		t.Fatalf("tool_result tool_use_id = %q, want toolu_cursor_1; content=%s", got, userContent.Raw)
	}
	if got := userContent.Get("0.content.0.text").String(); got != "package main" {
		t.Fatalf("tool_result text = %q, want package main; content=%s", got, userContent.Raw)
	}
	if got := userContent.Get("0.cache_control.type").String(); got != "ephemeral" {
		t.Fatalf("tool_result cache_control = %q, want ephemeral; content=%s", got, userContent.Raw)
	}
}

func TestConvertOpenAIRequestToClaude_UnsupportedNativeContentBlocksStillIgnored(t *testing.T) {
	inputJSON := `{
		"model": "gpt-4.1",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "unsupported_native_block", "value": "drop me"},
					{"type": "text", "text": "keep me"}
				]
			}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-sonnet-4-5", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)
	content := resultJSON.Get("messages.0.content")

	if got := len(content.Array()); got != 1 {
		t.Fatalf("content length = %d, want 1; content=%s", got, content.Raw)
	}
	if got := content.Get("0.type").String(); got != "text" {
		t.Fatalf("content[0].type = %q, want text; content=%s", got, content.Raw)
	}
	if got := content.Get("0.text").String(); got != "keep me" {
		t.Fatalf("content[0].text = %q, want keep me; content=%s", got, content.Raw)
	}
}

func TestConvertOpenAIRequestToClaude_PreservesThinkingBlocksOnAssistantTurn(t *testing.T) {
	inputJSON := `{
		"model": "clip-claude-opus-4-8-xhigh",
		"messages": [
			{
				"role": "assistant",
				"content": [
					{"type": "thinking", "thinking": "Let me inspect the file.", "signature": "sig-abc"},
					{"type": "redacted_thinking", "data": "redacted-xyz"},
					{"type": "text", "text": "I will read it."},
					{"type": "tool_use", "id": "toolu_1", "name": "Read", "input": {"file_path": "main.go"}}
				]
			},
			{
				"role": "user",
				"content": [
					{"type": "tool_result", "tool_use_id": "toolu_1", "content": [{"type": "text", "text": "package main"}]}
				]
			}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-opus-4-8", []byte(inputJSON), true)
	content := gjson.ParseBytes(result).Get("messages.0.content")

	if got := len(content.Array()); got != 4 {
		t.Fatalf("assistant content length = %d, want 4; content=%s", got, content.Raw)
	}
	if got := content.Get("0.type").String(); got != "thinking" {
		t.Fatalf("content[0].type = %q, want thinking; content=%s", got, content.Raw)
	}
	if got := content.Get("0.thinking").String(); got != "Let me inspect the file." {
		t.Fatalf("content[0].thinking = %q; content=%s", got, content.Raw)
	}
	if got := content.Get("0.signature").String(); got != "sig-abc" {
		t.Fatalf("content[0].signature = %q, want sig-abc; content=%s", got, content.Raw)
	}
	if got := content.Get("1.type").String(); got != "redacted_thinking" {
		t.Fatalf("content[1].type = %q, want redacted_thinking; content=%s", got, content.Raw)
	}
	if got := content.Get("1.data").String(); got != "redacted-xyz" {
		t.Fatalf("content[1].data = %q, want redacted-xyz; content=%s", got, content.Raw)
	}
	if got := content.Get("3.type").String(); got != "tool_use" {
		t.Fatalf("content[3].type = %q, want tool_use; content=%s", got, content.Raw)
	}
}

func TestConvertOpenAIRequestToClaude_ThinkingBlockDroppedOnUserTurn(t *testing.T) {
	inputJSON := `{
		"model": "claude-opus-4-8",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "thinking", "thinking": "spoofed", "signature": "x"},
					{"type": "text", "text": "hello"}
				]
			}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-opus-4-8", []byte(inputJSON), false)
	content := gjson.ParseBytes(result).Get("messages.0.content")

	if got := len(content.Array()); got != 1 {
		t.Fatalf("user content length = %d, want 1; content=%s", got, content.Raw)
	}
	if got := content.Get("0.type").String(); got != "text" {
		t.Fatalf("content[0].type = %q, want text; content=%s", got, content.Raw)
	}
}

func TestConvertOpenAIRequestToClaude_PreservesServerToolHistoryOnAssistantTurn(t *testing.T) {
	inputJSON := `{
		"model": "clip-claude-opus-4-8-xhigh",
		"messages": [
			{
				"role": "assistant",
				"content": [
					{"type": "server_tool_use", "id": "srvtool_1", "name": "web_search", "input": {"query": "golang"}},
					{"type": "web_search_tool_result", "tool_use_id": "srvtool_1", "content": [{"type": "web_search_result", "title": "Go", "url": "https://go.dev"}]},
					{"type": "text", "text": "Found it."}
				]
			}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-opus-4-8", []byte(inputJSON), true)
	content := gjson.ParseBytes(result).Get("messages.0.content")

	if got := len(content.Array()); got != 3 {
		t.Fatalf("assistant content length = %d, want 3; content=%s", got, content.Raw)
	}
	if got := content.Get("0.type").String(); got != "server_tool_use" {
		t.Fatalf("content[0].type = %q, want server_tool_use; content=%s", got, content.Raw)
	}
	if got := content.Get("0.name").String(); got != "web_search" {
		t.Fatalf("content[0].name = %q, want web_search; content=%s", got, content.Raw)
	}
	if got := content.Get("1.type").String(); got != "web_search_tool_result" {
		t.Fatalf("content[1].type = %q, want web_search_tool_result; content=%s", got, content.Raw)
	}
	if got := content.Get("1.content.0.url").String(); got != "https://go.dev" {
		t.Fatalf("content[1].content[0].url = %q; content=%s", got, content.Raw)
	}
}

func TestConvertOpenAIRequestToClaude_ParallelToolResultsGroupedIntoOneUserTurn(t *testing.T) {
	inputJSON := `{
		"model": "gpt-4.1",
		"messages": [
			{
				"role": "assistant",
				"content": null,
				"tool_calls": [
					{"id": "call_a", "type": "function", "function": {"name": "Read", "arguments": "{\"file_path\":\"a.go\"}"}},
					{"id": "call_b", "type": "function", "function": {"name": "Read", "arguments": "{\"file_path\":\"b.go\"}"}}
				]
			},
			{"role": "tool", "tool_call_id": "call_a", "content": "contents of a"},
			{"role": "tool", "tool_call_id": "call_b", "content": "contents of b"}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-opus-4-8", []byte(inputJSON), false)
	resultJSON := gjson.ParseBytes(result)
	messages := resultJSON.Get("messages")

	if got := len(messages.Array()); got != 2 {
		t.Fatalf("messages length = %d, want 2 (assistant + grouped user); messages=%s", got, messages.Raw)
	}
	if got := messages.Get("1.role").String(); got != "user" {
		t.Fatalf("messages[1].role = %q, want user; messages=%s", got, messages.Raw)
	}
	userContent := messages.Get("1.content")
	if got := len(userContent.Array()); got != 2 {
		t.Fatalf("grouped user content length = %d, want 2; content=%s", got, userContent.Raw)
	}
	if got := userContent.Get("0.tool_use_id").String(); got != "call_a" {
		t.Fatalf("content[0].tool_use_id = %q, want call_a; content=%s", got, userContent.Raw)
	}
	if got := userContent.Get("1.tool_use_id").String(); got != "call_b" {
		t.Fatalf("content[1].tool_use_id = %q, want call_b; content=%s", got, userContent.Raw)
	}
	if got := userContent.Get("1.content").String(); got != "contents of b" {
		t.Fatalf("content[1].content = %q, want contents of b; content=%s", got, userContent.Raw)
	}
}

func TestConvertOpenAIRequestToClaude_ToolResultFollowedByUserTextStartsNewTurn(t *testing.T) {
	inputJSON := `{
		"model": "gpt-4.1",
		"messages": [
			{
				"role": "assistant",
				"content": null,
				"tool_calls": [
					{"id": "call_a", "type": "function", "function": {"name": "Read", "arguments": "{}"}}
				]
			},
			{"role": "tool", "tool_call_id": "call_a", "content": "result a"},
			{"role": "user", "content": "now do something else"},
			{"role": "tool", "tool_call_id": "call_b", "content": "result b"}
		]
	}`

	result := ConvertOpenAIRequestToClaude("claude-opus-4-8", []byte(inputJSON), false)
	messages := gjson.ParseBytes(result).Get("messages")

	// assistant, user(tool_result a), user(text), user(tool_result b) -> 4 turns;
	// tool_result b must NOT merge into the text user turn.
	if got := len(messages.Array()); got != 4 {
		t.Fatalf("messages length = %d, want 4; messages=%s", got, messages.Raw)
	}
	if got := messages.Get("1.content.0.type").String(); got != "tool_result" {
		t.Fatalf("messages[1].content[0].type = %q, want tool_result; messages=%s", got, messages.Raw)
	}
	if got := messages.Get("2.content.0.type").String(); got != "text" {
		t.Fatalf("messages[2].content[0].type = %q, want text; messages=%s", got, messages.Raw)
	}
	if got := messages.Get("3.content.0.type").String(); got != "tool_result" {
		t.Fatalf("messages[3].content[0].type = %q, want tool_result; messages=%s", got, messages.Raw)
	}
	if got := messages.Get("3.content.0.tool_use_id").String(); got != "call_b" {
		t.Fatalf("messages[3].content[0].tool_use_id = %q, want call_b; messages=%s", got, messages.Raw)
	}
}

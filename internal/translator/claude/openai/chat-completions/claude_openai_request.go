// Package openai provides request translation functionality for OpenAI to Claude Code API compatibility.
// It handles parsing and transforming OpenAI Chat Completions API requests into Claude Code API format,
// extracting model information, system instructions, message contents, and tool declarations.
// The package performs JSON data transformation to ensure compatibility
// between OpenAI API format and Claude Code API's expected format.
package chat_completions

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/thinking"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var (
	user    = ""
	account = ""
	session = ""
)

// ConvertOpenAIRequestToClaude parses and transforms an OpenAI Chat Completions API request into Claude Code API format.
// It extracts the model name, system instruction, message contents, and tool declarations
// from the raw JSON request and returns them in the format expected by the Claude Code API.
// The function performs comprehensive transformation including:
// 1. Model name mapping and parameter extraction (max_tokens, temperature, top_p, etc.)
// 2. Message content conversion from OpenAI to Claude Code format
// 3. Tool call and tool result handling with proper ID mapping
// 4. Image data conversion from OpenAI data URLs to Claude Code base64 format
// 5. Stop sequence and streaming configuration handling
//
// Parameters:
//   - modelName: The name of the model to use for the request
//   - rawJSON: The raw JSON request data from the OpenAI API
//   - stream: A boolean indicating if the request is for a streaming response
//
// Returns:
//   - []byte: The transformed request data in Claude Code API format
func ConvertOpenAIRequestToClaude(modelName string, inputRawJSON []byte, stream bool) []byte {
	rawJSON := inputRawJSON

	if account == "" {
		u, _ := uuid.NewRandom()
		account = u.String()
	}
	if session == "" {
		u, _ := uuid.NewRandom()
		session = u.String()
	}
	if user == "" {
		sum := sha256.Sum256([]byte(account + session))
		user = hex.EncodeToString(sum[:])
	}
	userID := fmt.Sprintf("user_%s_account_%s_session_%s", user, account, session)

	// Base Claude Code API template with default max_tokens value
	out := []byte(fmt.Sprintf(`{"model":"","max_tokens":32000,"messages":[],"metadata":{"user_id":"%s"}}`, userID))

	root := gjson.ParseBytes(rawJSON)
	out = preserveClaudeNativeEnvelope(out, root)

	// Convert OpenAI reasoning_effort to Claude thinking config.
	if v := root.Get("reasoning_effort"); v.Exists() {
		effort := strings.ToLower(strings.TrimSpace(v.String()))
		if effort != "" {
			mi := registry.LookupModelInfo(modelName, "claude")
			supportsAdaptive := mi != nil && mi.Thinking != nil && len(mi.Thinking.Levels) > 0
			supportsMax := supportsAdaptive && thinking.HasLevel(mi.Thinking.Levels, string(thinking.LevelMax))

			// Claude 4.6 supports adaptive thinking with output_config.effort.
			// MapToClaudeEffort normalizes levels (e.g. minimal→low, xhigh→high) to avoid
			// validation errors since validate treats same-provider unsupported levels as errors.
			if supportsAdaptive {
				switch effort {
				case "none":
					out, _ = sjson.SetBytes(out, "thinking.type", "disabled")
					out, _ = sjson.DeleteBytes(out, "thinking.budget_tokens")
					out, _ = sjson.DeleteBytes(out, "output_config.effort")
				case "auto":
					out, _ = sjson.SetBytes(out, "thinking.type", "adaptive")
					out, _ = sjson.DeleteBytes(out, "thinking.budget_tokens")
					out, _ = sjson.DeleteBytes(out, "output_config.effort")
				default:
					if mapped, ok := thinking.MapToClaudeEffort(effort, supportsMax); ok {
						effort = mapped
					}
					out, _ = sjson.SetBytes(out, "thinking.type", "adaptive")
					out, _ = sjson.DeleteBytes(out, "thinking.budget_tokens")
					out, _ = sjson.SetBytes(out, "output_config.effort", effort)
				}
			} else {
				// Legacy/manual thinking (budget_tokens).
				budget, ok := thinking.ConvertLevelToBudget(effort)
				if ok {
					switch budget {
					case 0:
						out, _ = sjson.SetBytes(out, "thinking.type", "disabled")
					case -1:
						out, _ = sjson.SetBytes(out, "thinking.type", "enabled")
					default:
						if budget > 0 {
							out, _ = sjson.SetBytes(out, "thinking.type", "enabled")
							out, _ = sjson.SetBytes(out, "thinking.budget_tokens", budget)
						}
					}
				}
			}
		}
	}

	// Helper for generating tool call IDs in the form: toolu_<alphanum>
	// This ensures unique identifiers for tool calls in the Claude Code format
	genToolCallID := func() string {
		const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		var b strings.Builder
		// 24 chars random suffix for uniqueness
		for i := 0; i < 24; i++ {
			n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
			b.WriteByte(letters[n.Int64()])
		}
		return "toolu_" + b.String()
	}

	// Model mapping to specify which Claude Code model to use
	out, _ = sjson.SetBytes(out, "model", modelName)

	// Max tokens configuration with fallback to default value
	if maxTokens := root.Get("max_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "max_tokens", maxTokens.Int())
	}

	// Temperature setting for controlling response randomness
	if temp := root.Get("temperature"); temp.Exists() {
		out, _ = sjson.SetBytes(out, "temperature", temp.Float())
	} else if topP := root.Get("top_p"); topP.Exists() {
		// Top P setting for nucleus sampling (filtered out if temperature is set)
		out, _ = sjson.SetBytes(out, "top_p", topP.Float())
	}

	// Stop sequences configuration for custom termination conditions
	if stop := root.Get("stop"); stop.Exists() {
		if stop.IsArray() {
			var stopSequences []string
			stop.ForEach(func(_, value gjson.Result) bool {
				stopSequences = append(stopSequences, value.String())
				return true
			})
			if len(stopSequences) > 0 {
				out, _ = sjson.SetBytes(out, "stop_sequences", stopSequences)
			}
		} else {
			out, _ = sjson.SetBytes(out, "stop_sequences", []string{stop.String()})
		}
	}

	// Stream configuration to enable or disable streaming responses
	out, _ = sjson.SetBytes(out, "stream", stream)

	// Process messages and transform them to Claude Code format
	if messages := root.Get("messages"); messages.Exists() && messages.IsArray() {
		messageIndex := 0
		messages.ForEach(func(_, message gjson.Result) bool {
			role := message.Get("role").String()
			contentResult := message.Get("content")

			switch role {
			case "system":
				if contentResult.Exists() && contentResult.Type == gjson.String && contentResult.String() != "" {
					textPart := []byte(`{"type":"text","text":""}`)
					textPart, _ = sjson.SetBytes(textPart, "text", contentResult.String())
					out, _ = sjson.SetRawBytes(out, "system.-1", textPart)
				} else if contentResult.Exists() && contentResult.IsArray() {
					contentResult.ForEach(func(_, part gjson.Result) bool {
						if part.Get("type").String() == "text" {
							if textPart := convertClaudeTextPart(part); textPart != "" {
								out, _ = sjson.SetRawBytes(out, "system.-1", []byte(textPart))
							}
						}
						return true
					})
				}
			case "user", "assistant":
				msg := []byte(`{"role":"","content":[]}`)
				msg, _ = sjson.SetBytes(msg, "role", role)

				// Handle content based on its type (string or array)
				if contentResult.Exists() && contentResult.Type == gjson.String && contentResult.String() != "" {
					part := []byte(`{"type":"text","text":""}`)
					part, _ = sjson.SetBytes(part, "text", contentResult.String())
					msg, _ = sjson.SetRawBytes(msg, "content.-1", part)
				} else if contentResult.Exists() && contentResult.IsArray() {
					contentResult.ForEach(func(_, part gjson.Result) bool {
						claudePart := convertOpenAIContentPartToClaudePart(part, role)
						if claudePart != "" {
							msg, _ = sjson.SetRawBytes(msg, "content.-1", []byte(claudePart))
						}
						return true
					})
				}

				// Handle tool calls (for assistant messages)
				if toolCalls := message.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() && role == "assistant" {
					toolCalls.ForEach(func(_, toolCall gjson.Result) bool {
						if toolCall.Get("type").String() == "function" {
							toolCallID := toolCall.Get("id").String()
							if toolCallID == "" {
								toolCallID = genToolCallID()
							}

							function := toolCall.Get("function")
							toolUse := []byte(`{"type":"tool_use","id":"","name":"","input":{}}`)
							toolUse, _ = sjson.SetBytes(toolUse, "id", toolCallID)
							toolUse, _ = sjson.SetBytes(toolUse, "name", function.Get("name").String())

							// Parse arguments for the tool call
							if args := function.Get("arguments"); args.Exists() {
								argsStr := args.String()
								if argsStr != "" && gjson.Valid(argsStr) {
									argsJSON := gjson.Parse(argsStr)
									if argsJSON.IsObject() {
										toolUse, _ = sjson.SetRawBytes(toolUse, "input", []byte(argsJSON.Raw))
									} else {
										toolUse, _ = sjson.SetRawBytes(toolUse, "input", []byte("{}"))
									}
								} else {
									toolUse, _ = sjson.SetRawBytes(toolUse, "input", []byte("{}"))
								}
							} else {
								toolUse, _ = sjson.SetRawBytes(toolUse, "input", []byte("{}"))
							}

							msg, _ = sjson.SetRawBytes(msg, "content.-1", toolUse)
						}
						return true
					})
				}

				out, _ = sjson.SetRawBytes(out, "messages.-1", msg)
				messageIndex++

			case "tool":
				// Handle tool result messages conversion
				toolCallID := message.Get("tool_call_id").String()
				toolContentResult := message.Get("content")

				msg := []byte(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"","content":""}]}`)
				msg, _ = sjson.SetBytes(msg, "content.0.tool_use_id", toolCallID)
				toolResultContent, toolResultContentRaw := convertOpenAIToolResultContent(toolContentResult)
				if toolResultContentRaw {
					msg, _ = sjson.SetRawBytes(msg, "content.0.content", []byte(toolResultContent))
				} else {
					msg, _ = sjson.SetBytes(msg, "content.0.content", toolResultContent)
				}
				out, _ = sjson.SetRawBytes(out, "messages.-1", msg)
				messageIndex++
			}
			return true
		})

		// Preserve a minimal conversational turn for system-only inputs.
		// Claude payloads with top-level system instructions but no messages are risky for downstream validation.
		if messageIndex == 0 {
			system := gjson.GetBytes(out, "system")
			if system.Exists() && system.IsArray() && len(system.Array()) > 0 {
				fallbackMsg := []byte(`{"role":"user","content":[{"type":"text","text":""}]}`)
				out, _ = sjson.SetRawBytes(out, "messages.-1", fallbackMsg)
			}
		}
	}

	// Tools mapping: OpenAI tools -> Claude Code tools
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() && len(tools.Array()) > 0 {
		hasAnthropicTools := false
		tools.ForEach(func(_, tool gjson.Result) bool {
			if anthropicTool := convertToolDefinitionToClaudeTool(tool); anthropicTool != "" {
				out, _ = sjson.SetRawBytes(out, "tools.-1", []byte(anthropicTool))
				hasAnthropicTools = true
			}
			return true
		})

		if !hasAnthropicTools {
			out, _ = sjson.DeleteBytes(out, "tools")
		}
	}

	// Tool choice mapping from OpenAI format to Claude Code format
	if toolChoice := root.Get("tool_choice"); toolChoice.Exists() {
		if claudeToolChoice := convertToolChoiceToClaude(toolChoice); claudeToolChoice != "" {
			out, _ = sjson.SetRawBytes(out, "tool_choice", []byte(claudeToolChoice))
		}
	}

	return out
}

func convertToolDefinitionToClaudeTool(tool gjson.Result) string {
	toolType := strings.TrimSpace(tool.Get("type").String())
	if toolType == "function" {
		return convertFunctionToolDefinitionToClaudeTool(tool)
	}
	if inputSchema := tool.Get("input_schema"); inputSchema.Exists() {
		return convertClaudeNativeCustomTool(tool, inputSchema)
	}
	if isClaudeTypedTool(toolType, tool) {
		return tool.Raw
	}
	return ""
}

func convertFunctionToolDefinitionToClaudeTool(tool gjson.Result) string {
	function := tool.Get("function")
	source := tool
	if function.Exists() && function.IsObject() {
		source = function
	}

	name := strings.TrimSpace(source.Get("name").String())
	if name == "" {
		return ""
	}

	schema := source.Get("parameters")
	if !schema.Exists() {
		schema = source.Get("parametersJsonSchema")
	}
	if !schema.Exists() || !schema.IsObject() {
		return ""
	}

	anthropicTool := []byte(`{"name":"","description":"","input_schema":{}}`)
	anthropicTool, _ = sjson.SetBytes(anthropicTool, "name", name)
	anthropicTool, _ = sjson.SetBytes(anthropicTool, "description", source.Get("description").String())
	anthropicTool, _ = sjson.SetRawBytes(anthropicTool, "input_schema", []byte(schema.Raw))
	anthropicTool = copyClaudeCacheControl(anthropicTool, tool)
	return string(anthropicTool)
}

func convertClaudeNativeCustomTool(tool gjson.Result, inputSchema gjson.Result) string {
	name := strings.TrimSpace(tool.Get("name").String())
	if name == "" || !inputSchema.IsObject() {
		return ""
	}

	anthropicTool := []byte(`{"name":"","description":"","input_schema":{}}`)
	anthropicTool, _ = sjson.SetBytes(anthropicTool, "name", name)
	anthropicTool, _ = sjson.SetBytes(anthropicTool, "description", tool.Get("description").String())
	anthropicTool, _ = sjson.SetRawBytes(anthropicTool, "input_schema", []byte(inputSchema.Raw))
	anthropicTool = copyClaudeCacheControl(anthropicTool, tool)
	return string(anthropicTool)
}

func isClaudeTypedTool(toolType string, tool gjson.Result) bool {
	if toolType == "" || strings.EqualFold(toolType, "function") {
		return false
	}
	if strings.TrimSpace(tool.Get("name").String()) == "" {
		return false
	}
	lastUnderscore := strings.LastIndex(toolType, "_")
	if lastUnderscore < 0 || len(toolType)-lastUnderscore-1 != 8 {
		return false
	}
	for _, r := range toolType[lastUnderscore+1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func convertToolChoiceToClaude(toolChoice gjson.Result) string {
	switch toolChoice.Type {
	case gjson.String:
		switch strings.ToLower(strings.TrimSpace(toolChoice.String())) {
		case "auto":
			return `{"type":"auto"}`
		case "required":
			return `{"type":"any"}`
		case "none":
			return ""
		default:
			return ""
		}
	case gjson.JSON:
		switch strings.TrimSpace(toolChoice.Get("type").String()) {
		case "auto":
			return `{"type":"auto"}`
		case "any":
			return `{"type":"any"}`
		case "tool":
			name := strings.TrimSpace(toolChoice.Get("name").String())
			if name == "" {
				return ""
			}
			claudeChoice := []byte(`{"type":"tool","name":""}`)
			claudeChoice, _ = sjson.SetBytes(claudeChoice, "name", name)
			return string(claudeChoice)
		case "function":
			name := strings.TrimSpace(toolChoice.Get("function.name").String())
			if name == "" {
				name = strings.TrimSpace(toolChoice.Get("name").String())
			}
			if name == "" {
				return ""
			}
			claudeChoice := []byte(`{"type":"tool","name":""}`)
			claudeChoice, _ = sjson.SetBytes(claudeChoice, "name", name)
			return string(claudeChoice)
		default:
			return ""
		}
	default:
		return ""
	}
}

func preserveClaudeNativeEnvelope(out []byte, root gjson.Result) []byte {
	if system := root.Get("system"); system.Exists() {
		switch {
		case system.IsArray():
			out, _ = sjson.SetRawBytes(out, "system", []byte(system.Raw))
		case system.Type == gjson.String && strings.TrimSpace(system.String()) != "":
			textPart := []byte(`{"type":"text","text":""}`)
			textPart, _ = sjson.SetBytes(textPart, "text", system.String())
			out, _ = sjson.SetRawBytes(out, "system", []byte("["+string(textPart)+"]"))
		}
	}

	if userID := root.Get("metadata.user_id"); userID.Exists() && userID.Type == gjson.String && strings.TrimSpace(userID.String()) != "" {
		out, _ = sjson.SetBytes(out, "metadata.user_id", userID.String())
	}
	if thinkingConfig := root.Get("thinking"); thinkingConfig.Exists() && thinkingConfig.IsObject() {
		out, _ = sjson.SetRawBytes(out, "thinking", []byte(thinkingConfig.Raw))
	}
	if outputConfig := root.Get("output_config"); outputConfig.Exists() && outputConfig.IsObject() {
		out, _ = sjson.SetRawBytes(out, "output_config", []byte(outputConfig.Raw))
	}
	if betas := root.Get("betas"); betas.Exists() && (betas.IsArray() || betas.Type == gjson.String) {
		out, _ = sjson.SetRawBytes(out, "betas", []byte(betas.Raw))
	}
	return out
}

func convertOpenAIContentPartToClaudePart(part gjson.Result, role string) string {
	switch part.Get("type").String() {
	case "text":
		return convertClaudeTextPart(part)

	case "image_url":
		return convertOpenAIImageURLToClaudePart(part.Get("image_url.url").String())

	case "file":
		fileData := part.Get("file.file_data").String()
		if strings.HasPrefix(fileData, "data:") {
			semicolonIdx := strings.Index(fileData, ";")
			commaIdx := strings.Index(fileData, ",")
			if semicolonIdx != -1 && commaIdx != -1 && commaIdx > semicolonIdx {
				mediaType := strings.TrimPrefix(fileData[:semicolonIdx], "data:")
				data := fileData[commaIdx+1:]
				docPart := []byte(`{"type":"document","source":{"type":"base64","media_type":"","data":""}}`)
				docPart, _ = sjson.SetBytes(docPart, "source.media_type", mediaType)
				docPart, _ = sjson.SetBytes(docPart, "source.data", data)
				return string(docPart)
			}
		}

	case "tool_use":
		if role == "assistant" {
			return convertClaudeNativeToolUsePart(part)
		}

	case "tool_result":
		if role == "user" {
			return convertClaudeNativeToolResultPart(part)
		}
	}

	return ""
}

func convertClaudeTextPart(part gjson.Result) string {
	textPart := []byte(`{"type":"text","text":""}`)
	textPart, _ = sjson.SetBytes(textPart, "text", part.Get("text").String())
	textPart = copyClaudeCacheControl(textPart, part)
	return string(textPart)
}

func convertClaudeNativeToolUsePart(part gjson.Result) string {
	id := strings.TrimSpace(part.Get("id").String())
	name := strings.TrimSpace(part.Get("name").String())
	if id == "" || name == "" {
		return ""
	}

	toolUse := []byte(`{"type":"tool_use","id":"","name":"","input":{}}`)
	toolUse, _ = sjson.SetBytes(toolUse, "id", id)
	toolUse, _ = sjson.SetBytes(toolUse, "name", name)
	if input := part.Get("input"); input.Exists() && input.IsObject() {
		toolUse, _ = sjson.SetRawBytes(toolUse, "input", []byte(input.Raw))
	}
	toolUse = copyClaudeCacheControl(toolUse, part)
	return string(toolUse)
}

func convertClaudeNativeToolResultPart(part gjson.Result) string {
	toolUseID := strings.TrimSpace(part.Get("tool_use_id").String())
	content := part.Get("content")
	if toolUseID == "" || !content.Exists() {
		return ""
	}

	toolResult := []byte(`{"type":"tool_result","tool_use_id":"","content":""}`)
	toolResult, _ = sjson.SetBytes(toolResult, "tool_use_id", toolUseID)
	if content.IsArray() || content.IsObject() {
		toolResult, _ = sjson.SetRawBytes(toolResult, "content", []byte(content.Raw))
	} else {
		toolResult, _ = sjson.SetBytes(toolResult, "content", content.String())
	}
	if isError := part.Get("is_error"); isError.Exists() && (isError.Type == gjson.True || isError.Type == gjson.False) {
		toolResult, _ = sjson.SetBytes(toolResult, "is_error", isError.Bool())
	}
	toolResult = copyClaudeCacheControl(toolResult, part)
	return string(toolResult)
}

func copyClaudeCacheControl(dst []byte, src gjson.Result) []byte {
	cacheControl := src.Get("cache_control")
	if !cacheControl.Exists() || !cacheControl.IsObject() {
		return dst
	}
	out, err := sjson.SetRawBytes(dst, "cache_control", []byte(cacheControl.Raw))
	if err != nil {
		return dst
	}
	return out
}

func convertOpenAIImageURLToClaudePart(imageURL string) string {
	if imageURL == "" {
		return ""
	}

	if strings.HasPrefix(imageURL, "data:") {
		parts := strings.SplitN(imageURL, ",", 2)
		if len(parts) != 2 {
			return ""
		}

		mediaTypePart := strings.SplitN(parts[0], ";", 2)[0]
		mediaType := strings.TrimPrefix(mediaTypePart, "data:")
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}

		imagePart := []byte(`{"type":"image","source":{"type":"base64","media_type":"","data":""}}`)
		imagePart, _ = sjson.SetBytes(imagePart, "source.media_type", mediaType)
		imagePart, _ = sjson.SetBytes(imagePart, "source.data", parts[1])
		return string(imagePart)
	}

	imagePart := []byte(`{"type":"image","source":{"type":"url","url":""}}`)
	imagePart, _ = sjson.SetBytes(imagePart, "source.url", imageURL)
	return string(imagePart)
}

func convertOpenAIToolResultContent(content gjson.Result) (string, bool) {
	if !content.Exists() {
		return "", false
	}

	if content.Type == gjson.String {
		return content.String(), false
	}

	if content.IsArray() {
		claudeContent := []byte("[]")
		partCount := 0

		content.ForEach(func(_, part gjson.Result) bool {
			if part.Type == gjson.String {
				textPart := []byte(`{"type":"text","text":""}`)
				textPart, _ = sjson.SetBytes(textPart, "text", part.String())
				claudeContent, _ = sjson.SetRawBytes(claudeContent, "-1", textPart)
				partCount++
				return true
			}

			claudePart := convertOpenAIContentPartToClaudePart(part, "")
			if claudePart != "" {
				claudeContent, _ = sjson.SetRawBytes(claudeContent, "-1", []byte(claudePart))
				partCount++
			}
			return true
		})

		if partCount > 0 || len(content.Array()) == 0 {
			return string(claudeContent), true
		}

		return content.Raw, false
	}

	if content.IsObject() {
		claudePart := convertOpenAIContentPartToClaudePart(content, "")
		if claudePart != "" {
			claudeContent := []byte("[]")
			claudeContent, _ = sjson.SetRawBytes(claudeContent, "-1", []byte(claudePart))
			return string(claudeContent), true
		}
		return content.Raw, false
	}

	return content.Raw, false
}

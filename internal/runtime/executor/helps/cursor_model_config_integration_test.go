package helps

import (
	"fmt"
	"os"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestCursorModelConfigPinsRequestedEffort(t *testing.T) {
	configPath := os.Getenv("CLIPROXY_CURSOR_CONFIG")
	if configPath == "" {
		t.Skip("CLIPROXY_CURSOR_CONFIG is not set")
	}
	data, errRead := os.ReadFile(configPath)
	if errRead != nil {
		t.Fatalf("read config: %v", errRead)
	}
	cfg, errParse := config.ParseConfigBytes(data)
	if errParse != nil {
		t.Fatalf("parse config: %v", errParse)
	}

	tests := []struct {
		alias          string
		model          string
		protocol       string
		root           string
		payload        string
		effortPath     string
		expectedEffort string
	}{
		{alias: "gpt-6-astra-medium", model: "gpt-6-astra", protocol: "codex", payload: `{"reasoning":{"effort":"low"}}`, effortPath: "reasoning.effort", expectedEffort: "medium"},
		{alias: "gpt-6-astra-high", model: "gpt-6-astra", protocol: "codex", payload: `{"reasoning":{"effort":"low"}}`, effortPath: "reasoning.effort", expectedEffort: "high"},
		{alias: "gpt-6-astra-xhigh", model: "gpt-6-astra", protocol: "codex", payload: `{"reasoning":{"effort":"low"}}`, effortPath: "reasoning.effort", expectedEffort: "xhigh"},
		{alias: "gpt-6-astra-max", model: "gpt-6-astra", protocol: "codex", payload: `{"reasoning":{"effort":"low"}}`, effortPath: "reasoning.effort", expectedEffort: "max"},
		{alias: "clip-gemini-3.8-flash-medium", model: "gemini-3.8-flash-high", protocol: "antigravity", root: "request", payload: `{"request":{"generationConfig":{"thinkingConfig":{"thinkingLevel":"low","thinkingBudget":1}}}}`, effortPath: "request.generationConfig.thinkingConfig.thinkingLevel", expectedEffort: "medium"},
		{alias: "clip-gemini-3.8-flash-high", model: "gemini-3.8-flash-high", protocol: "antigravity", root: "request", payload: `{"request":{"generationConfig":{"thinkingConfig":{"thinkingLevel":"low","thinkingBudget":1}}}}`, effortPath: "request.generationConfig.thinkingConfig.thinkingLevel", expectedEffort: "high"},
		{alias: "clip-gemini-3.1-pro-medium", model: "gemini-3.1-pro-low", protocol: "antigravity", root: "request", payload: `{"request":{"generationConfig":{"thinkingConfig":{"thinkingLevel":"low","thinkingBudget":1}}}}`, effortPath: "request.generationConfig.thinkingConfig.thinkingLevel", expectedEffort: "medium"},
		{alias: "clip-gemini-3.1-pro-high", model: "gemini-3.1-pro-low", protocol: "antigravity", root: "request", payload: `{"request":{"generationConfig":{"thinkingConfig":{"thinkingLevel":"low","thinkingBudget":1}}}}`, effortPath: "request.generationConfig.thinkingConfig.thinkingLevel", expectedEffort: "high"},
	}

	for _, family := range []string{"argon", "carbon"} {
		for _, effort := range []string{"medium", "high"} {
			tests = append(tests, struct {
				alias, model, protocol, root, payload, effortPath, expectedEffort string
			}{
				alias:    "clip-gemini-4-" + family + "-" + effort,
				model:    "gemini-4-" + family + "-high",
				protocol: "antigravity", root: "request",
				payload:    `{"request":{"generationConfig":{"thinkingConfig":{"thinkingLevel":"low","thinkingBudget":1}}}}`,
				effortPath: "request.generationConfig.thinkingConfig.thinkingLevel", expectedEffort: effort,
			})
		}
	}

	// Cursor must keep these names opaque instead of normalizing GPT registry slugs.
	for _, test := range tests[:4] {
		test.alias = "clip-" + test.alias
		tests = append(tests, test)
	}

	for _, family := range []struct{ prefix, model, protocol string }{
		{"clip-gpt-6-astra", "gpt-6-astra", "codex"},
		{"clip-sol-6-1", "gpt-6.1-sol", "codex"},
		{"clip-luna-6", "gpt-6-luna", "codex"},
		{"clip-claude-opus-5-5", "claude-opus-5-5", "claude"},
		{"clip-claude-sonnet-5-5", "claude-sonnet-5-5", "claude"},
		{"clip-claude-fable-5-1", "claude-fable-5-1", "claude"},
		{"clip-gemini-3.8-flash", "gemini-3.8-flash-high", "antigravity"},
		{"clip-gemini-3.1-pro", "gemini-3.1-pro-low", "antigravity"},
	} {
		for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
			if family.protocol == "antigravity" && (effort == "xhigh" || effort == "max") {
				continue
			}
			test := tests[0]
			test.alias, test.model, test.protocol, test.expectedEffort = family.prefix+"-"+effort, family.model, family.protocol, effort
			conflict := "low"
			if effort == "low" {
				conflict = "high"
			}
			switch family.protocol {
			case "codex":
				test.payload = fmt.Sprintf(`{"reasoning":{"effort":%q}}`, conflict)
			case "claude":
				test.effortPath = "output_config.effort"
				test.payload = fmt.Sprintf(`{"thinking":{"type":"disabled"},"output_config":{"effort":%q},"max_tokens":1}`, conflict)
			case "antigravity":
				test.root = "request"
				test.effortPath = "request.generationConfig.thinkingConfig.thinkingLevel"
				test.payload = fmt.Sprintf(`{"request":{"generationConfig":{"thinkingConfig":{"thinkingLevel":%q,"thinkingBudget":1}}}}`, conflict)
			}
			tests = append(tests, test)
		}
	}

	for _, test := range tests {
		t.Run(test.alias, func(t *testing.T) {
			missingPayload := `{}`
			if test.root == "request" {
				missingPayload = `{"request":{}}`
			}
			for scenario, payload := range map[string]string{
				"missing":       missingPayload,
				"contradictory": test.payload,
			} {
				t.Run(scenario, func(t *testing.T) {
					out := ApplyPayloadConfigWithRequest(cfg, test.model, test.protocol, "openai", test.root, []byte(payload), []byte(payload), test.alias, "/v1/chat/completions", nil)
					if got := ojson.GetBytes(out, test.effortPath).String(); got != test.expectedEffort {
						t.Fatalf("effort = %q, want %q; payload=%s", got, test.expectedEffort, out)
					}
					if test.protocol == "claude" {
						wantTokens := int64(16384)
						if test.expectedEffort == "xhigh" || test.expectedEffort == "max" {
							wantTokens = 65536
						}
						if got := ojson.GetBytes(out, "max_tokens").Int(); got != wantTokens {
							t.Fatalf("max_tokens = %d, want %d", got, wantTokens)
						}
						if got := ojson.GetBytes(out, "thinking.type").String(); got != "adaptive" {
							t.Fatalf("thinking.type = %q, want adaptive; payload=%s", got, out)
						}
					}
					if test.protocol == "antigravity" && ojson.GetBytes(out, "request.generationConfig.thinkingConfig.thinkingBudget").Exists() {
						t.Fatalf("thinkingBudget was not removed: %s", out)
					}
				})
			}
		})
	}
}

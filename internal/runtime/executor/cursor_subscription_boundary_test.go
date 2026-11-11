package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestCursorClaudeGeminiSubscriptionBoundary(t *testing.T) {
	path := os.Getenv("CLIPROXY_CURSOR_CONFIG")
	if path == "" {
		t.Skip("CLIPROXY_CURSOR_CONFIG is not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.ParseConfigBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range []struct{ prefix, model, provider string }{
		{"clip-claude-opus-5-5", "claude-opus-5-5", "claude"},
		{"clip-claude-sonnet-5-5", "claude-sonnet-5-5", "claude"},
		{"clip-claude-fable-5-1", "claude-fable-5-1", "claude"},
		{"clip-gemini-3.8-flash", "gemini-3.8-flash-high", "antigravity"},
		{"clip-gemini-3.1-pro", "gemini-3.1-pro-low", "antigravity"},
		{"clip-gemini-4-argon", "gemini-4-argon-high", "antigravity"},
		{"clip-gemini-4-carbon", "gemini-4-carbon-high", "antigravity"},
	} {
		for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
			if strings.HasPrefix(family.prefix, "clip-gemini-4-") && effort == "low" {
				continue
			}
			if family.provider == "antigravity" && (effort == "xhigh" || effort == "max") {
				continue
			}
			for _, streaming := range []bool{false, true} {
				for _, incoming := range []string{"", "conflicting"} {
					t.Run(fmt.Sprintf("%s-%s/stream=%t/%s", family.prefix, effort, streaming, incoming), func(t *testing.T) {
						var body []byte
						var requestPath string
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							if r.URL.Path == "/v1internal:loadCodeAssist" {
								_, _ = io.WriteString(w, `{"paidTier":{"id":"tier-1"}}`)
								return
							}
							body, _ = io.ReadAll(r.Body)
							requestPath = r.URL.Path
							// Stop at the actual HTTP boundary; no provider or credential is involved.
							w.WriteHeader(http.StatusBadRequest)
							_, _ = io.WriteString(w, `{"error":{"message":"synthetic boundary complete"}}`)
						}))
						defer server.Close()
						alias := family.prefix + "-" + effort
						conflict := "low"
						if effort == "low" {
							conflict = "high"
						}
						extra := ""
						if incoming != "" {
							extra = fmt.Sprintf(`,"reasoning_effort":%q,"thinking":{"type":"disabled"},"output_config":{"effort":%q}`, conflict, conflict)
						}
						payload := []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"Synthetic boundary"}],"max_tokens":1%s}`, alias, extra))
						req := cliproxyexecutor.Request{Model: family.model, Payload: payload}
						opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Stream: streaming, Metadata: map[string]any{cliproxyexecutor.RequestedModelMetadataKey: alias}}
						auth := &cliproxyauth.Auth{ID: t.Name(), Attributes: map[string]string{"base_url": server.URL, "api_key": "synthetic"}, Metadata: map[string]any{"access_token": "synthetic", "project_id": "synthetic", "expired": time.Now().Add(time.Hour).Format(time.RFC3339)}}
						var errExecute error
						if family.provider == "claude" {
							exec := NewClaudeExecutor(cfg)
							if streaming {
								_, errExecute = exec.ExecuteStream(context.Background(), auth, req, opts)
							} else {
								_, errExecute = exec.Execute(context.Background(), auth, req, opts)
							}
						} else {
							exec := NewAntigravityExecutor(cfg)
							if streaming {
								_, errExecute = exec.ExecuteStream(context.Background(), auth, req, opts)
							} else {
								_, errExecute = exec.Execute(context.Background(), auth, req, opts)
							}
						}
						if errExecute == nil || len(body) == 0 {
							t.Fatalf("expected synthetic boundary error, got %v; request bytes=%d", errExecute, len(body))
						}
						if got := ojson.GetBytes(body, "model").String(); got != family.model {
							t.Errorf("model=%q want %q", got, family.model)
						}
						effortPath := "output_config.effort"
						if family.provider == "antigravity" {
							effortPath = "request.generationConfig.thinkingConfig.thinkingLevel"
							if !strings.Contains(requestPath, "GenerateContent") && !strings.Contains(requestPath, "generateContent") {
								t.Errorf("unexpected Gemini protocol path %q", requestPath)
							}
							if ojson.GetBytes(body, "request.generationConfig.thinkingConfig.thinkingBudget").Exists() {
								t.Error("conflicting thinking budget remains")
							}
							if ojson.GetBytes(body, "enabledCreditTypes").Exists() {
								t.Error("credits enabled")
							}
						} else {
							if requestPath != "/v1/messages" {
								t.Errorf("unexpected Claude protocol path %q", requestPath)
							}
							if ojson.GetBytes(body, "thinking.type").String() != "adaptive" {
								t.Error("adaptive thinking lost")
							}
							ceiling := int64(16384)
							if effort == "xhigh" || effort == "max" {
								ceiling = 65536
							}
							if ojson.GetBytes(body, "max_tokens").Int() != ceiling {
								t.Error("output ceiling lost")
							}
						}
						if got := ojson.GetBytes(body, effortPath).String(); got != effort {
							t.Errorf("effort=%q want %q", got, effort)
						}
					})
				}
			}
		}
	}
}

package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

// This exercises the real executor/translator boundary with the deployment config.
// It never contacts a provider or uses credentials from that config.
func TestCursorGPT6ChatCompletionsUsesResponses(t *testing.T) {
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
	for _, family := range []struct{ prefix, model string }{{"clip-gpt-6-astra", "gpt-6-astra"}, {"clip-sol-6-1", "gpt-6.1-sol"}, {"clip-luna-6", "gpt-6-luna"}} {
		for _, incoming := range []string{"", "low", "high"} {
			for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
				for _, streaming := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/incoming=%s/stream=%t", family.model, effort, incoming, streaming), func(t *testing.T) {
						var body []byte
						var requestPath string
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							requestPath = r.URL.Path
							body, _ = io.ReadAll(r.Body)
							w.Header().Set("Content-Type", "text/event-stream")
							_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"object\":\"response\",\"status\":\"completed\",\"output\":[]}}\n\n")
						}))
						defer server.Close()
						alias := family.prefix + "-" + effort
						// Auth manager resolves the alias before invoking this executor; metadata
						// must retain the requested alias for the effort rule to match.
						req := cliproxyexecutor.Request{Model: family.model, Payload: []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"Synthetic probe"}],"reasoning_effort":%q,"tools":[{"type":"function","function":{"name":"probe","parameters":{"type":"object","properties":{}}}}]}`, alias, incoming))}
						if incoming == "" {
							doc := ojson.NewDoc(req.Payload)
							_ = doc.Delete("reasoning_effort")
							req.Payload = doc.Bytes()
						}
						opts := cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatOpenAI, Stream: streaming, Metadata: map[string]any{cliproxyexecutor.RequestedModelMetadataKey: alias}}
						auth := &cliproxyauth.Auth{Attributes: map[string]string{"base_url": server.URL, "api_key": "synthetic"}}
						exec := NewCodexExecutor(cfg)
						if streaming {
							result, err := exec.ExecuteStream(context.Background(), auth, req, opts)
							if err != nil {
								t.Fatal(err)
							}
							for chunk := range result.Chunks {
								if chunk.Err != nil {
									t.Fatal(chunk.Err)
								}
							}
						} else {
							if _, err := exec.Execute(context.Background(), auth, req, opts); err != nil {
								t.Fatal(err)
							}
						}
						if requestPath != "/responses" {
							t.Fatalf("path = %q", requestPath)
						}
						for field, want := range map[string]string{"model": family.model, "reasoning.effort": effort, "tools.0.name": "probe"} {
							if got := ojson.GetBytes(body, field).String(); got != want {
								t.Errorf("%s = %q, want %q", field, got, want)
							}
						}
						if !ojson.GetBytes(body, "input").IsArray() || ojson.GetBytes(body, "messages").Exists() {
							t.Error("Chat Completions was not translated to Responses")
						}
					})
				}
			}
		}

	}
}

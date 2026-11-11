package auth

import (
	"context"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"net/http"
	"testing"
)

func TestManagerExecuteStream_SubscriptionOnlyQuotaFailsWithoutCredits(t *testing.T) {
	const model = "gemini-3.8-flash-high"
	executor := &antigravityCreditsFallbackExecutor{}
	manager := NewManager(nil, nil, nil)
	manager.SetConfig(&internalconfig.Config{QuotaExceeded: internalconfig.QuotaExceeded{SwitchProject: true, SwitchPreviewModel: false, AntigravityCredits: false}})
	manager.RegisterExecutor(executor)
	registry.GetGlobalRegistry().RegisterClient("ag-subscription-only", "antigravity", []*registry.ModelInfo{{ID: model}, {ID: "gemini-3.1-pro-low"}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient("ag-subscription-only") })
	if _, err := manager.Register(context.Background(), &Auth{ID: "ag-subscription-only", Provider: "antigravity"}); err != nil {
		t.Fatal(err)
	}
	result, err := manager.ExecuteStream(context.Background(), []string{"antigravity"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if err == nil && result != nil {
		for chunk := range result.Chunks {
			if chunk.Err != nil {
				err = chunk.Err
			}
		}
	}
	if err == nil || statusCodeFromError(err) != http.StatusTooManyRequests {
		t.Fatalf("expected visible quota error, got %v", err)
	}
	if len(executor.streamCreditsRequested) != 1 || executor.streamCreditsRequested[0] {
		t.Fatalf("unexpected retry/credits attempts: %v", executor.streamCreditsRequested)
	}
}

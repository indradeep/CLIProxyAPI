package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
)

type delayedFirstChunkExecutor struct {
	release     <-chan struct{}
	terminalErr error
}

func (e *delayedFirstChunkExecutor) Identifier() string { return "codex" }

func (e *delayedFirstChunkExecutor) Execute(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{Code: "not_implemented", Message: "Execute not implemented"}
}

func (e *delayedFirstChunkExecutor) ExecuteStream(ctx context.Context, _ *coreauth.Auth, _ coreexecutor.Request, _ coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	chunks := make(chan coreexecutor.StreamChunk)
	go func() {
		defer close(chunks)
		select {
		case <-ctx.Done():
			return
		case <-e.release:
			if e.terminalErr != nil {
				chunks <- coreexecutor.StreamChunk{Err: e.terminalErr}
				return
			}
			chunks <- coreexecutor.StreamChunk{Payload: []byte(`{"id":"chunk-1"}`)}
		}
	}()
	return &coreexecutor.StreamResult{Chunks: chunks}, nil
}

func (e *delayedFirstChunkExecutor) Refresh(_ context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *delayedFirstChunkExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{Code: "not_implemented", Message: "CountTokens not implemented"}
}

func (e *delayedFirstChunkExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{Code: "not_implemented", Message: "HttpRequest not implemented", HTTPStatus: http.StatusNotImplemented}
}

func TestChatCompletionsStreamKeepAliveCoversBootstrapWait(t *testing.T) {
	gin.SetMode(gin.TestMode)

	release := make(chan struct{})
	executor := &delayedFirstChunkExecutor{release: release}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)

	auth := &coreauth.Auth{
		ID:       "bootstrap-wait-auth",
		Provider: "codex",
		Status:   coreauth.StatusActive,
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("manager.Register: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{
		Streaming: sdkconfig.StreamingConfig{KeepAliveSeconds: 1},
	}, manager)
	handler := NewOpenAIAPIHandler(base)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test-model","stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ChatCompletions(c)
	}()

	time.Sleep(1200 * time.Millisecond)
	close(release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler did not finish after the first payload was released")
	}

	body := recorder.Body.String()
	if !strings.Contains(body, ": keep-alive\n\n") {
		t.Fatalf("expected keep-alive during bootstrap wait, got %q", body)
	}
	if !strings.Contains(body, "data: {\"id\":\"chunk-1\"}\n\n") {
		t.Fatalf("expected streamed payload after bootstrap, got %q", body)
	}
	if !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatalf("expected terminal DONE marker, got %q", body)
	}
}

func TestChatCompletionsStreamErrorAfterBootstrapKeepAliveUsesSSEFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)

	release := make(chan struct{})
	executor := &delayedFirstChunkExecutor{
		release: release,
		terminalErr: &coreauth.Error{
			Code:       "upstream_closed",
			Message:    "upstream closed",
			HTTPStatus: http.StatusBadGateway,
		},
	}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)

	auth := &coreauth.Auth{
		ID:       "bootstrap-error-auth",
		Provider: "codex",
		Status:   coreauth.StatusActive,
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("manager.Register: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-error-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{
		Streaming: sdkconfig.StreamingConfig{KeepAliveSeconds: 1},
	}, manager)
	handler := NewOpenAIAPIHandler(base)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test-error-model","stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ChatCompletions(c)
	}()

	time.Sleep(1200 * time.Millisecond)
	close(release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler did not finish after the bootstrap error was released")
	}

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after keep-alive committed the stream", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.HasPrefix(body, ": keep-alive\n\n") {
		t.Fatalf("expected bootstrap keep-alive prefix, got %q", body)
	}
	if !strings.Contains(body, "data: {\"error\":") {
		t.Fatalf("expected terminal error as an SSE data frame, got %q", body)
	}
}

func TestChatCompletionsStreamImmediateBootstrapErrorKeepsHTTPStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	release := make(chan struct{})
	close(release)
	executor := &delayedFirstChunkExecutor{
		release: release,
		terminalErr: &coreauth.Error{
			Code:       "upstream_unavailable",
			Message:    "upstream unavailable",
			HTTPStatus: http.StatusBadGateway,
		},
	}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)

	auth := &coreauth.Auth{
		ID:       "bootstrap-immediate-error-auth",
		Provider: "codex",
		Status:   coreauth.StatusActive,
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("manager.Register: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: "test-immediate-error-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{
		Streaming: sdkconfig.StreamingConfig{KeepAliveSeconds: 1},
	}, manager)
	handler := NewOpenAIAPIHandler(base)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test-immediate-error-model","stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.ChatCompletions(c)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 before the stream is committed", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, ": keep-alive") || strings.Contains(body, "data: ") {
		t.Fatalf("expected plain HTTP error before stream commitment, got %q", body)
	}
	if !strings.Contains(body, `"message":"upstream_unavailable: upstream unavailable"`) {
		t.Fatalf("expected upstream error body, got %q", body)
	}
}

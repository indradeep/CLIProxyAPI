package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

type delayedChatCompletionsStreamExecutor struct {
	start       <-chan struct{}
	release     <-chan struct{}
	terminalErr error
	truncate    bool
}

func (e *delayedChatCompletionsStreamExecutor) Identifier() string { return "codex" }

func (e *delayedChatCompletionsStreamExecutor) Execute(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{Code: "not_implemented", Message: "Execute not implemented"}
}

func (e *delayedChatCompletionsStreamExecutor) ExecuteStream(ctx context.Context, _ *coreauth.Auth, _ coreexecutor.Request, _ coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	if e.start != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-e.start:
		}
	}

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
			payload := `{"id":"chunk-1","choices":[{"index":0,"delta":{"content":"synthetic"},"finish_reason":"stop"}]}`
			if e.truncate {
				payload = `{"id":"chunk-1","choices":[{"index":0,"delta":{"content":"synthetic"}}]}`
			}
			select {
			case chunks <- coreexecutor.StreamChunk{Payload: []byte(payload)}:
			case <-ctx.Done():
			}
		}
	}()
	return &coreexecutor.StreamResult{Chunks: chunks}, nil
}

func (e *delayedChatCompletionsStreamExecutor) Refresh(_ context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *delayedChatCompletionsStreamExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{Code: "not_implemented", Message: "CountTokens not implemented"}
}

func (e *delayedChatCompletionsStreamExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{Code: "not_implemented", Message: "HttpRequest not implemented", HTTPStatus: http.StatusNotImplemented}
}

func newChatCompletionsBootstrapHandler(t *testing.T, model string, executor *delayedChatCompletionsStreamExecutor) *OpenAIAPIHandler {
	t.Helper()
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)

	auth := &coreauth.Auth{ID: model + "-auth", Provider: "codex", Status: coreauth.StatusActive}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("manager.Register: %v", err)
	}
	registry.GetGlobalRegistry().RegisterClient(auth.ID, auth.Provider, []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	})

	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{
		Streaming: sdkconfig.StreamingConfig{KeepAliveSeconds: 1, SendKeepAliveBeforeFT: true},
	}, manager)
	return NewOpenAIAPIHandler(base)
}

func runChatCompletionsBootstrapRequest(handler *OpenAIAPIHandler, model string) (*httptest.ResponseRecorder, <-chan struct{}) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"`+model+`","stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ChatCompletions(c)
	}()
	return recorder, done
}

func waitForBootstrapRequest(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler did not finish after bootstrap release")
	}
}

func TestChatCompletionsStreamKeepAliveCoversExecutionSetup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	start := make(chan struct{})
	release := make(chan struct{})
	close(release)
	model := "test-bootstrap-setup-model"
	handler := newChatCompletionsBootstrapHandler(t, model, &delayedChatCompletionsStreamExecutor{start: start, release: release})
	recorder, done := runChatCompletionsBootstrapRequest(handler, model)

	time.Sleep(1200 * time.Millisecond)
	close(start)
	waitForBootstrapRequest(t, done)

	body := recorder.Body.String()
	if !strings.HasPrefix(body, ": keep-alive\n\n") {
		t.Fatalf("expected keep-alive during execution setup, got %q", body)
	}
	if !strings.Contains(body, "data: {\"id\":\"chunk-1\",") {
		t.Fatalf("expected streamed payload after execution setup, got %q", body)
	}
}

func TestChatCompletionsStreamKeepAliveCoversFirstChunkWait(t *testing.T) {
	gin.SetMode(gin.TestMode)
	release := make(chan struct{})
	model := "test-bootstrap-first-chunk-model"
	handler := newChatCompletionsBootstrapHandler(t, model, &delayedChatCompletionsStreamExecutor{release: release})
	recorder, done := runChatCompletionsBootstrapRequest(handler, model)

	time.Sleep(1200 * time.Millisecond)
	close(release)
	waitForBootstrapRequest(t, done)

	body := recorder.Body.String()
	if !strings.Contains(body, ": keep-alive\n\n") {
		t.Fatalf("expected keep-alive while waiting for first payload, got %q", body)
	}
	if !strings.Contains(body, "data: {\"id\":\"chunk-1\",") {
		t.Fatalf("expected streamed payload after bootstrap, got %q", body)
	}
	if !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatalf("expected terminal DONE marker, got %q", body)
	}
}

func TestChatCompletionsStreamErrorAfterBootstrapKeepAliveUsesSSEFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)
	release := make(chan struct{})
	model := "test-bootstrap-error-model"
	handler := newChatCompletionsBootstrapHandler(t, model, &delayedChatCompletionsStreamExecutor{
		release: release,
		terminalErr: &coreauth.Error{
			Code:       "upstream_closed",
			Message:    "upstream closed",
			HTTPStatus: http.StatusBadGateway,
		},
	})
	recorder, done := runChatCompletionsBootstrapRequest(handler, model)

	time.Sleep(1200 * time.Millisecond)
	close(release)
	waitForBootstrapRequest(t, done)

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
	model := "test-bootstrap-immediate-error-model"
	handler := newChatCompletionsBootstrapHandler(t, model, &delayedChatCompletionsStreamExecutor{
		release: release,
		terminalErr: &coreauth.Error{
			Code:       "upstream_unavailable",
			Message:    "upstream unavailable",
			HTTPStatus: http.StatusBadGateway,
		},
	})
	recorder, done := runChatCompletionsBootstrapRequest(handler, model)
	waitForBootstrapRequest(t, done)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 before the stream is committed", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, ": keep-alive") || strings.Contains(body, "data: ") {
		t.Fatalf("expected plain HTTP error before stream commitment, got %q", body)
	}
}

func TestChatCompletionsStreamBootstrapKeepAlivePreservesTruncationError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	release := make(chan struct{})
	handler := newChatCompletionsBootstrapHandler(t, "test-bootstrap-truncated-model", &delayedChatCompletionsStreamExecutor{release: release, truncate: true})
	recorder, done := runChatCompletionsBootstrapRequest(handler, "test-bootstrap-truncated-model")
	time.Sleep(1200 * time.Millisecond)
	close(release)
	waitForBootstrapRequest(t, done)
	body := recorder.Body.String()
	if !strings.HasPrefix(body, ": keep-alive\n\n") || !strings.Contains(body, "upstream stream closed before any chunk carried finish_reason") || strings.Contains(body, "data: [DONE]") {
		t.Fatalf("keepalive must preserve upstream truncation detection: %q", body)
	}
}

func TestChatCompletionsStreamBootstrapCancellationStopsSetup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	start, release := make(chan struct{}), make(chan struct{})
	model := "test-bootstrap-cancel-model"
	handler := newChatCompletionsBootstrapHandler(t, model, &delayedChatCompletionsStreamExecutor{start: start, release: release})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"`+model+`","stream":true}`)).WithContext(ctx)
	done := make(chan struct{})
	go func() { defer close(done); handler.ChatCompletions(c) }()
	cancel()
	waitForBootstrapRequest(t, done)
	if recorder.Body.Len() != 0 {
		t.Fatalf("cancelled setup emitted a response: %q", recorder.Body.String())
	}
}

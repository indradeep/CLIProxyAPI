package management

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestManagementCallbackURLDefaultsToLocalServer(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{Port: 8318}, nil)

	got, err := h.managementCallbackURL("codex/callback")
	if err != nil {
		t.Fatalf("managementCallbackURL() error = %v", err)
	}

	want := "http://127.0.0.1:8318/codex/callback"
	if got != want {
		t.Fatalf("managementCallbackURL() = %q, want %q", got, want)
	}
}

func TestManagementCallbackURLUsesPublicCallbackBaseURL(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{
		Port: 8318,
		RemoteManagement: config.RemoteManagement{
			PublicCallbackBaseURL: "https://proxy.example.test/",
		},
	}, nil)

	got, err := h.managementCallbackURL("/anthropic/callback")
	if err != nil {
		t.Fatalf("managementCallbackURL() error = %v", err)
	}

	want := "https://proxy.example.test/anthropic/callback"
	if got != want {
		t.Fatalf("managementCallbackURL() = %q, want %q", got, want)
	}
}

func TestManagementCallbackURLRejectsInvalidPublicCallbackBaseURL(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{
		Port: 8318,
		RemoteManagement: config.RemoteManagement{
			PublicCallbackBaseURL: "proxy.example.test",
		},
	}, nil)

	if _, err := h.managementCallbackURL("/codex/callback"); err == nil {
		t.Fatal("managementCallbackURL() error = nil, want invalid public callback base URL error")
	}
}

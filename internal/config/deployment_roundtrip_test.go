package config

import (
	"bytes"
	"gopkg.in/yaml.v3"
	"os"
	"reflect"
	"testing"
)

func TestDeploymentConfigV816RoundTrip(t *testing.T) {
	path := os.Getenv("CLIPROXY_CURSOR_CONFIG")
	if path == "" {
		t.Skip("CLIPROXY_CURSOR_CONFIG is not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := ParseConfigBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	normalized, _, err := NormalizeConfigLayout(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateV8Config(normalized); err != nil {
		t.Fatal(err)
	}
	after, err := ParseConfigBytes(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("configuration migration changed effective deployment settings")
	}
	marshalled, err := yaml.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ParseConfigBytes(marshalled)
	if err != nil {
		t.Fatal(err)
	}
	// YAML snapshots materialize absent collections as empty collections. Compare
	// canonical values so that nil slices do not masquerade as a behavior change.
	restoredSnapshot, err := yaml.Marshal(restored)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(marshalled, restoredSnapshot) {
		t.Fatal("configuration snapshot changed effective deployment settings")
	}
	if before.RemoteManagement.PublicCallbackBaseURL != "https://clip.indradeep.com" || restored.RemoteManagement.PublicCallbackBaseURL != before.RemoteManagement.PublicCallbackBaseURL {
		t.Fatal("public callback configuration was not preserved")
	}
}

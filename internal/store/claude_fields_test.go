package store

// claude_fields_test.go pins the settings/transcript fields the claude
// backend and the approval gate add: backend and mode ride the LLM
// settings, the claude session id rides the transcript, and old files
// without them still load.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSettingsBackendModeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	in := LLMConfig{Provider: "openai", Model: "gpt", Backend: "claude", Mode: "read-only"}
	if err := SaveSettings(path, in); err != nil {
		t.Fatal(err)
	}
	out, ok, err := LoadSettings(path)
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if out.Backend != "claude" || out.Mode != "read-only" {
		t.Fatalf("round trip: %+v", out)
	}
}

// A settings file from before the fields existed loads with them empty —
// normalization is the app layer's job, the file is not rewritten.
func TestSettingsLegacyWithoutBackendMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	legacy := []byte(`{"provider":"openai","model":"gpt","systemPrompt":"","thinking":""}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	out, ok, err := LoadSettings(path)
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if out.Backend != "" || out.Mode != "" {
		t.Fatalf("legacy fields should load empty: %+v", out)
	}
}

func TestTranscriptClaudeIDRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.json")
	in := Transcript{
		Mode:     ModeAgent,
		ClaudeID: "sess-1234",
		Rows:     []Message{{ID: "a1", Role: RoleAssistant, Kind: KindReasoned, Text: "hi", At: time.Now()}},
	}
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var out Transcript
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.ClaudeID != "sess-1234" {
		t.Fatalf("claudeID = %q", out.ClaudeID)
	}
}

// An old transcript (no claudeID) still unmarshals.
func TestTranscriptLegacyWithoutClaudeID(t *testing.T) {
	var out Transcript
	if err := json.Unmarshal([]byte(`{"mode":"agent","rows":[]}`), &out); err != nil {
		t.Fatal(err)
	}
	if out.ClaudeID != "" {
		t.Fatalf("claudeID = %q", out.ClaudeID)
	}
}

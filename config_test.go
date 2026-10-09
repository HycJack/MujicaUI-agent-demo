package main

// config_test.go covers the settings persistence: a save/load roundtrip
// keeps the editable fields and the max-tokens mirror, transient call
// state stays out of the file, a missing or corrupt file falls back to
// the defaults, and closing the Settings dialog saves.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSettingsPersistenceRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	a := newApp()
	a.configPath = path
	a.llm.Provider = "deepseek"
	a.llm.Model = "deepseek-chat"
	a.llm.APIKey = "sk-test"
	a.llm.BaseURL = "https://api.example.com/v1"
	a.llm.SystemPrompt = "You are terse."
	a.llm.Temperature = 0.3
	a.llm.MaxTokens = 1024
	a.llm.StreamOutput = false
	a.saveSettings()

	b := newApp()
	b.configPath = path
	b.loadSettings()
	if b.llm.Provider != "deepseek" || b.llm.Model != "deepseek-chat" ||
		b.llm.APIKey != "sk-test" || b.llm.BaseURL != "https://api.example.com/v1" ||
		b.llm.SystemPrompt != "You are terse." || b.llm.Temperature != 0.3 ||
		b.llm.MaxTokens != 1024 || b.llm.StreamOutput {
		t.Fatalf("loaded settings drifted: %+v", b.llm)
	}
	if b.maxTokField != "1024" {
		t.Fatalf("max tokens field %q, want \"1024\"", b.maxTokField)
	}
	if b.agentView.seededModel != "deepseek/deepseek-chat" {
		t.Fatalf("loaded model not marked seeded: %q", b.agentView.seededModel)
	}
}

func TestSettingsPersistenceTransient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	a := newApp()
	a.configPath = path
	a.llm.Busy = true
	a.llm.LastError = "boom"
	a.llm.ProviderOK = true
	a.llm.ConnectionErr = "HTTP 503"
	a.saveSettings()
	b := newApp()
	b.configPath = path
	b.loadSettings()
	if b.llm.Busy || b.llm.LastError != "" || b.llm.ProviderOK || b.llm.ConnectionErr != "" {
		t.Fatalf("transient fields persisted: %+v", b.llm)
	}
}

func TestSettingsPersistenceFallbacks(t *testing.T) {
	dir := t.TempDir()

	// A missing file keeps the defaults.
	a := newApp()
	a.configPath = filepath.Join(dir, "missing", "settings.json")
	a.loadSettings()
	if a.llm.Provider != "openai" || a.llm.MaxTokens != 2048 {
		t.Fatalf("defaults not kept without a file: %+v", a.llm)
	}

	// A corrupt file is ignored rather than fatal.
	corrupt := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(corrupt, []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := newApp()
	b.configPath = corrupt
	b.loadSettings()
	if b.llm.Provider != "openai" {
		t.Fatalf("corrupt file overwrote the defaults: %+v", b.llm)
	}

	// An empty configPath disables persistence instead of writing to CWD.
	c := newApp()
	c.configPath = ""
	c.saveSettings() // must be a no-op
}

func TestSettingsSaveOnClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	a := newApp()
	a.configPath = path
	a.openProviders()
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	a.llm.APIKey = "sk-from-ui"
	tst.Key(0, ui.KeyEscape) // close the dialog the way Escape does
	tst.Frame()
	if a.settingsOpen {
		t.Fatal("escape did not close the dialog")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("closing the dialog did not save: %v", err)
	}
	var saved LLMSettings
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.APIKey != "sk-from-ui" {
		t.Fatalf("saved API key %q, want \"sk-from-ui\"", saved.APIKey)
	}
}

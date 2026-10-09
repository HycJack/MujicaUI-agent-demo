package main

// config_test.go covers the settings persistence: a save/load roundtrip
// keeps the editable fields, transient call state stays out of the file,
// a missing or corrupt file falls back to the defaults, and the dialog
// saves live while open (so killing the app cannot lose the config) and
// once more on close.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// An old store under the OS config directory migrates into the home
// directory once: settings, workspace prefs, the session index and every
// per-session transcript — never overwriting newer files.
func TestConfigDirMigration(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(oldDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("settings.json", `{"provider":"deepseek","apiKey":"sk-old"}`)
	write("workspace.json", `{"current":"C:\\old"}`)
	write("sessions.json", `{"sessions":[]}`)
	write(filepath.Join("sessions", "s1.json"), `{"rows":[]}`)

	migrateConfigDirFrom(oldDir, newDir)

	for _, rel := range []string{"settings.json", "workspace.json", "sessions.json", filepath.Join("sessions", "s1.json")} {
		if _, err := os.Stat(filepath.Join(newDir, rel)); err != nil {
			t.Fatalf("%s was not migrated: %v", rel, err)
		}
	}
	buf, err := os.ReadFile(filepath.Join(newDir, "settings.json"))
	if err != nil || !strings.Contains(string(buf), "sk-old") {
		t.Fatalf("settings not migrated: %v %s", err, buf)
	}

	// Newer files in the home directory win; the old folder stays as backup.
	if err := os.WriteFile(filepath.Join(newDir, "settings.json"), []byte(`{"apiKey":"sk-new"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	migrateConfigDirFrom(oldDir, newDir)
	buf, _ = os.ReadFile(filepath.Join(newDir, "settings.json"))
	if !strings.Contains(string(buf), "sk-new") {
		t.Fatalf("migration overwrote newer settings: %s", buf)
	}
	if _, err := os.Stat(filepath.Join(oldDir, "settings.json")); err != nil {
		t.Fatal("the legacy folder was removed")
	}
}

func TestSettingsPersistenceRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	a := newApp()
	a.configPath = path
	a.llm.Provider = "deepseek"
	a.llm.Model = "deepseek-chat"
	a.llm.APIKey = "sk-test"
	a.llm.BaseURL = "https://api.example.com/v1"
	a.llm.SystemPrompt = "You are terse."
	a.llm.Thinking = "high"
	a.saveSettings()

	b := newApp()
	b.configPath = path
	b.loadSettings()
	if b.llm.Provider != "deepseek" || b.llm.Model != "deepseek-chat" ||
		b.llm.APIKey != "sk-test" || b.llm.BaseURL != "https://api.example.com/v1" ||
		b.llm.SystemPrompt != "You are terse." || b.llm.Thinking != "high" {
		t.Fatalf("loaded settings drifted: %+v", b.llm)
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
	if a.llm.Provider != "openai" {
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

// The dialog saves while it is open, the moment a value drifts — so killing
// the app (or closing the window with the dialog up) cannot lose the config.
func TestSettingsLiveSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	a := newApp()
	a.configPath = path
	a.openProviders()
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	a.llm.APIKey = "sk-live"
	tst.Frame() // the dialog is still open; the drift must already be on disk
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("live save did not write while open: %v", err)
	}
	var saved LLMSettings
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.APIKey != "sk-live" {
		t.Fatalf("live-saved API key %q, want \"sk-live\"", saved.APIKey)
	}
}

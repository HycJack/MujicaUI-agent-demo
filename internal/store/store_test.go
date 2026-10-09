package store

// store_test.go covers the data layer: the legacy-directory migration, the
// settings/workspace/session persistence roundtrips, the corrupt-file
// fallbacks, the legacy single-file index migration and the transcript
// path sanitization.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// An old store under a legacy directory migrates into the new one once:
// settings, workspace prefs, the session index and every per-session
// transcript — never overwriting newer files.
func TestDirMigration(t *testing.T) {
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

	migrateDirFrom(oldDir, newDir)

	for _, rel := range []string{"settings.json", "workspace.json", "sessions.json", filepath.Join("sessions", "s1.json")} {
		if _, err := os.Stat(filepath.Join(newDir, rel)); err != nil {
			t.Fatalf("%s was not migrated: %v", rel, err)
		}
	}
	buf, err := os.ReadFile(filepath.Join(newDir, "settings.json"))
	if err != nil || !strings.Contains(string(buf), "sk-old") {
		t.Fatalf("settings not migrated: %v %s", err, buf)
	}

	// Newer files in the new directory win; the old folder stays as backup.
	if err := os.WriteFile(filepath.Join(newDir, "settings.json"), []byte(`{"apiKey":"sk-new"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	migrateDirFrom(oldDir, newDir)
	buf, _ = os.ReadFile(filepath.Join(newDir, "settings.json"))
	if !strings.Contains(string(buf), "sk-new") {
		t.Fatalf("migration overwrote newer settings: %s", buf)
	}
	if _, err := os.Stat(filepath.Join(oldDir, "settings.json")); err != nil {
		t.Fatal("the legacy folder was removed")
	}
}

func TestSettingsRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	cfg := LLMConfig{
		Provider: "deepseek", Model: "deepseek-chat", APIKey: "sk-test",
		BaseURL: "https://api.example.com/v1", SystemPrompt: "You are terse.", Thinking: "high",
	}
	if err := SaveSettings(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadSettings(path)
	if err != nil || !ok || got != cfg {
		t.Fatalf("roundtrip drifted: ok=%v got=%+v err=%v", ok, got, err)
	}

	// A missing file reports found=false, not an error.
	if _, ok, err := LoadSettings(filepath.Join(t.TempDir(), "missing.json")); err != nil || ok {
		t.Fatalf("missing file: ok=%v err=%v", ok, err)
	}
	// A corrupt file is reported so the caller can fall back.
	corrupt := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(corrupt, []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadSettings(corrupt); err == nil {
		t.Fatal("a corrupt file should error")
	}
}

func TestWsPrefsRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.json")
	p := WsPrefs{Current: "C:\\root", Recents: []string{"C:\\root", "C:\\old"}}
	if err := SaveWsPrefs(path, p); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWsPrefs(path)
	if err != nil || got.Current != p.Current || len(got.Recents) != 2 {
		t.Fatalf("roundtrip drifted: %+v err=%v", got, err)
	}
}

func TestSessionsRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	idx := SessionIndex{Sessions: []SessionMeta{{
		ID: "s1", Title: "New chat 1", Updated: time.Now(), Workspace: dir, Threaded: true,
	}}}
	if err := SaveIndex(path, idx); err != nil {
		t.Fatal(err)
	}
	tr := Transcript{Mode: ModeAgent, Rows: []Message{
		{ID: "u1", Role: RoleUser, Kind: KindPlain, Text: "hello store", At: time.Now()},
		{ID: "a1", Role: RoleAssistant, Kind: KindReasoned, Text: "done", ThinkText: "hmm",
			At: time.Now(), Tools: []ToolCall{{CallID: "c1", Name: "bash", Command: "echo hi", Output: "hi"}}},
	}}
	if err := SaveTranscript(dir, "s1", tr); err != nil {
		t.Fatal(err)
	}

	// The index holds metadata only.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"rows"`) {
		t.Fatal("the session index should not embed transcripts")
	}
	got, err := LoadIndex(path)
	if err != nil || len(got.Sessions) != 1 || got.Sessions[0].ID != "s1" {
		t.Fatalf("index roundtrip: %+v err=%v", got, err)
	}
	tr2, err := LoadTranscript(dir, "s1")
	if err != nil || len(tr2.Rows) != 2 || tr2.Rows[0].Text != "hello store" {
		t.Fatalf("transcript roundtrip: %+v err=%v", tr2, err)
	}
	if len(tr2.Rows[1].Tools) != 1 || tr2.Rows[1].Tools[0].Output != "hi" {
		t.Fatalf("tool roundtrip: %+v", tr2.Rows[1].Tools)
	}
}

// A legacy single-file index (sessions with embedded rows) migrates once:
// each transcript lands in its own file, the index loses its rows, and the
// original is kept as .bak.
func TestLegacyIndexMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	at := time.Now()
	idx := SessionIndex{Sessions: []SessionMeta{{
		ID: "s1", Title: "legacy", Updated: at, Workspace: dir, Threaded: true, Mode: ModeAgent,
		LegacyRows: []Message{
			{ID: "u1", Role: RoleUser, Kind: KindPlain, Text: "legacy row", At: at},
			{ID: "a1", Role: RoleAssistant, Kind: KindReasoned, Text: "ok", At: at},
		},
	}}}
	buf, _ := json.MarshalIndent(idx, "", "  ")
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatal(err)
	}

	if !MigrateLegacyIndex(path, dir, idx) {
		t.Fatal("migration did not happen")
	}
	tr, err := LoadTranscript(dir, "s1")
	if err != nil || len(tr.Rows) != 2 || tr.Rows[0].Text != "legacy row" {
		t.Fatalf("legacy transcript not migrated: %+v err=%v", tr, err)
	}
	clean, err := LoadIndex(path)
	if err != nil || len(clean.Sessions) != 1 || len(clean.Sessions[0].LegacyRows) != 0 {
		t.Fatalf("index not cleaned: %+v err=%v", clean, err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatal("the legacy file was not kept as .bak")
	}
	// A second run is a no-op.
	if MigrateLegacyIndex(path, dir, clean) {
		t.Fatal("migration ran twice")
	}
}

// TranscriptPath sanitizes ids so a hostile index cannot escape the
// sessions directory.
func TestTranscriptPathSanitize(t *testing.T) {
	dir := t.TempDir()
	got := TranscriptPath(dir, "../../evil")
	if !strings.HasPrefix(got, filepath.Join(dir, "sessions")) {
		t.Fatalf("path escaped the sessions dir: %s", got)
	}
	base := strings.TrimSuffix(filepath.Base(got), ".json")
	if strings.ContainsAny(base, `/\.:`) {
		t.Fatalf("id not sanitized: %s", base)
	}
	if TranscriptPath(dir, "abc-DEF_123") != filepath.Join(dir, "sessions", "abc-DEF_123.json") {
		t.Fatal("safe id was changed")
	}
}

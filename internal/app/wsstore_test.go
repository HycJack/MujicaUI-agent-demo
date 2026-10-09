package app

// wsstore_test.go covers the UI layer's persistence glue: the persisted
// current workspace and recents, the per-workspace session transcripts
// (index + one file per session), the workspace switch, the sidebar's
// per-workspace filtering, and the Open-workspace dialog. The store
// package's own schemas are covered in internal/store.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crux-agent/internal/store"
	"github.com/egoist/mygo/ui"
)

func TestWsPrefsRoundtrip(t *testing.T) {
	dir := t.TempDir()
	a := newApp()
	a.wsPrefsPath = filepath.Join(dir, "workspace.json")
	a.ws = newWorkspace(dir)
	a.touchRecent(filepath.Join(dir, "proj"))
	a.saveWsPrefs()

	b := newApp()
	b.wsPrefsPath = a.wsPrefsPath
	b.loadWsPrefs()
	if b.ws.root != dir {
		t.Fatalf("restored root %q, want %q", b.ws.root, dir)
	}
	// The current workspace leads the recents, then the previously opened one.
	if len(b.recents) != 2 || b.recents[0] != dir || b.recents[1] != filepath.Join(dir, "proj") {
		t.Fatalf("recents %v", b.recents)
	}
}

func TestSessionsRoundtrip(t *testing.T) {
	dir := t.TempDir()
	a := newApp()
	a.sessionsPath = filepath.Join(dir, "sessions.json")
	a.ws = newWorkspace(dir)
	a.newThread() // belongs to dir, persisted
	a.thread.draft = "hello store"
	a.send() // persists the transcript rows (headless placeholder reply)

	// The index holds metadata only; the transcript lives in its own file.
	idx, err := store.LoadIndex(a.sessionsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Sessions) != 1 || !idx.Sessions[0].Threaded {
		t.Fatalf("index wrong: %+v", idx.Sessions)
	}
	trPath := filepath.Join(dir, "sessions", idx.Sessions[0].ID+".json")
	tr, err := os.ReadFile(trPath)
	if err != nil {
		t.Fatalf("transcript file missing: %v", err)
	}
	if !strings.Contains(string(tr), "hello store") {
		t.Fatalf("transcript lost the user row: %s", tr)
	}

	// A fresh app restores the index lazily; opening the session loads its
	// transcript from the per-session file.
	b := newApp()
	b.sessionsPath = a.sessionsPath
	b.ws = newWorkspace(dir)
	b.loadSessions()
	if len(b.sessions) != 1 {
		t.Fatalf("got %d sessions, want 1 (the new chat)", len(b.sessions))
	}
	if _, cached := b.threads[b.sessions[0].id]; cached {
		t.Fatal("transcripts should load lazily, not at startup")
	}
	b.restoreSession()
	tr2, ok := b.threads[b.sessions[0].id]
	if !ok || len(tr2.rows) < 2 {
		t.Fatalf("transcript not restored: ok=%v rows=%d", ok, len(tr2.rows))
	}
	if !strings.Contains(tr2.rows[0].text, "hello store") {
		t.Fatalf("user row lost: %q", tr2.rows[0].text)
	}
}

// A legacy single-file store (sessions with embedded rows) migrates once:
// each transcript lands in its own file and the original is kept as .bak.
func TestSessionsLegacyMigration(t *testing.T) {
	dir := t.TempDir()
	a := newApp()
	a.sessionsPath = filepath.Join(dir, "sessions.json")
	a.ws = newWorkspace(dir)
	a.newThread()
	a.thread.draft = "legacy row"
	a.send()

	// Fold the per-session layout back into the legacy shape on disk.
	idx, err := store.LoadIndex(a.sessionsPath)
	if err != nil {
		t.Fatal(err)
	}
	tr0, err := store.LoadTranscript(dir, idx.Sessions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range idx.Sessions {
		idx.Sessions[i].LegacyRows = tr0.Rows
	}
	buf, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.sessionsPath, buf, 0o600); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(dir, "sessions"))

	b := newApp()
	b.sessionsPath = a.sessionsPath
	b.ws = newWorkspace(dir)
	b.loadSessions()
	if len(b.sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(b.sessions))
	}
	b.restoreSession()
	if tr, ok := b.threads[b.sessions[0].id]; !ok || len(tr.rows) < 2 {
		t.Fatalf("legacy transcript not migrated: ok=%v", ok)
	}
	if _, err := os.Stat(a.sessionsPath + ".bak"); err != nil {
		t.Fatal("the legacy file was not kept as .bak")
	}
	if _, err := os.Stat(filepath.Join(dir, "sessions")); err != nil {
		t.Fatal("the per-session directory was not recreated")
	}
}

func TestOpenWorkspace(t *testing.T) {
	dir, other := t.TempDir(), t.TempDir()
	a := newApp()
	a.ws = newWorkspace(dir)
	a.newThread() // the only session of dir

	if msg := a.openWorkspace(filepath.Join(other, "nope")); !strings.Contains(msg, "Not a directory") {
		t.Fatalf("invalid path toast %q", msg)
	}
	if a.ws.root != dir {
		t.Fatal("an invalid path switched the workspace")
	}

	if msg := a.openWorkspace(other); !strings.Contains(msg, "Workspace:") {
		t.Fatalf("switch toast %q", msg)
	}
	if a.ws.root != other {
		t.Fatal("workspace not switched")
	}
	if a.sessionID != "" {
		t.Fatalf("a fresh workspace should start without sessions, got %q", a.sessionID)
	}
	if len(a.recents) < 2 || a.recents[0] != other || a.recents[1] != dir {
		t.Fatalf("recents %v", a.recents)
	}
	if _, ok := a.ws.nodes[dir]; ok {
		t.Fatal("the old tree state survived the switch")
	}

	// Switching back restores that workspace's sessions.
	if msg := a.openWorkspace(dir); msg == "" {
		t.Fatal("no toast on switching back")
	}
	if a.sessionID == "" {
		t.Fatal("session not restored after switching back")
	}
}

func TestSidebarFiltersSessions(t *testing.T) {
	dir, other := t.TempDir(), t.TempDir()
	a := newApp()
	a.ws = newWorkspace(dir)
	a.newThread()
	a.sessions = append(a.sessions,
		session{id: "x1", title: "Other workspace chat", updated: time.Now(), ws: other})
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	if !tst.HasText("New chat 1") {
		t.Fatal("the current workspace's session is missing from the list")
	}
	if _, ok := tst.Find("Other workspace chat"); ok {
		t.Fatal("another workspace's session leaked into the list")
	}
}

func TestWorkspaceDialog(t *testing.T) {
	dir, other := t.TempDir(), t.TempDir()
	a := newApp()
	a.ws = newWorkspace(dir)
	a.openWsDialog()
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	if !tst.HasText("Open workspace") {
		t.Fatal("the workspace dialog is missing")
	}
	if !tst.HasText("Recent workspaces") {
		t.Fatal("the recents section is missing")
	}
	a.wsPathField = other
	if err := tst.Click("Open"); err != nil {
		t.Fatalf("click Open: %v", err)
	}
	tst.Frame()
	if a.ws.root != other {
		t.Fatalf("Open did not switch the workspace: %q", a.ws.root)
	}
	if a.wsDialogOpen {
		t.Fatal("the dialog is still open")
	}
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("render nil after the switch")
	}
}

func TestHomeDirDefault(t *testing.T) {
	if store.HomeDir() == "" {
		t.Fatal("HomeDir resolved to an empty path")
	}
	a := newApp()
	if a.ws.root != store.HomeDir() {
		t.Fatalf("default workspace %q, want the home directory %q", a.ws.root, store.HomeDir())
	}
	if _, err := os.Stat(a.ws.root); err != nil {
		t.Fatalf("default workspace does not exist: %v", err)
	}
}

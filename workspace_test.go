package main

// workspace_test.go covers the workspace tree: directory listings sort
// folders first and skip noise, the tree lazy-loads headless, the preview
// reads and truncates, and the whole pane renders from a real temp
// directory.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacharyZhang-NY/MujicaUI/data"
	"github.com/egoist/mygo/ui"
)

func TestListDir(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"sub", ".git", "node_modules"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"b.txt", "a.txt", "Z.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := listDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.name)
	}
	want := []string{"sub", "a.txt", "b.txt", "Z.go"} // folders first, then case-insensitive names
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("listDir order %v, want %v", got, want)
	}
	if !entries[0].dir {
		t.Fatal("the folder entry is not marked as a directory")
	}
}

func TestWorkspaceLazyLoad(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "inner.md"), []byte("# in"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := newApp()
	a.ws = newWorkspace(root)
	if st := a.wsItemStatus(root); st != data.DataUnloaded {
		t.Fatalf("fresh root status %v, want DataUnloaded", st)
	}
	a.loadWsDir(root) // headless: synchronous
	if st := a.wsItemStatus(root); st != data.DataReady {
		t.Fatalf("root status %v after load, want DataReady", st)
	}
	kids := a.wsChildren(root)
	if len(kids) != 2 {
		t.Fatalf("root listed %d kids, want 2 (sub + hello.go)", len(kids))
	}
	if filepath.Base(kids[0]) != "sub" {
		t.Fatalf("folders should sort first, got %q", kids[0])
	}
	if a.wsChildren(kids[1]) != nil {
		t.Fatal("a file must be a leaf")
	}
	// The child directory registers unloaded and lists on demand.
	if st := a.wsItemStatus(kids[0]); st != data.DataUnloaded {
		t.Fatalf("child dir status %v, want DataUnloaded", st)
	}
	a.loadWsDir(kids[0])
	if got := a.wsChildren(kids[0]); len(got) != 1 || filepath.Base(got[0]) != "inner.md" {
		t.Fatalf("sub listing %v, want [inner.md]", got)
	}
	// A failed listing surfaces its error state.
	missing := filepath.Join(root, "gone")
	a.ws.nodes[missing] = wsNode{dir: true, status: data.DataUnloaded}
	a.loadWsDir(missing)
	if st := a.wsItemStatus(missing); st != data.DataFailed {
		t.Fatalf("missing dir status %v, want DataFailed", st)
	}
}

func TestWorkspacePreview(t *testing.T) {
	root := t.TempDir()
	notes := filepath.Join(root, "notes.md")
	if err := os.WriteFile(notes, []byte("# hello workspace"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApp()
	a.ws = newWorkspace(root)
	a.openWsPreview(notes)
	if a.repo.previewPath != notes || a.repo.previewText != "# hello workspace" {
		t.Fatalf("preview = %q %q", a.repo.previewPath, a.repo.previewText)
	}
	if a.repo.previewLang != "" {
		t.Fatalf("markdown maps to %q, want plain", a.repo.previewLang)
	}
	if a.repo.previewTruncated || a.repo.previewErr != "" {
		t.Fatalf("unexpected flags: trunc=%v err=%q", a.repo.previewTruncated, a.repo.previewErr)
	}

	// A Go file maps its language; a huge file truncates at the cap.
	goFile := filepath.Join(root, "main.go")
	if err := os.WriteFile(goFile, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openWsPreview(goFile)
	if a.repo.previewLang != "go" {
		t.Fatalf(".go maps to %q, want go", a.repo.previewLang)
	}
	big := filepath.Join(root, "big.txt")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", int(wsReadCap)+10)), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openWsPreview(big)
	if !a.repo.previewTruncated || len(a.repo.previewText) != int(wsReadCap) {
		t.Fatalf("truncation wrong: trunc=%v len=%d", a.repo.previewTruncated, len(a.repo.previewText))
	}
	// A missing file reports its error instead of panicking.
	a.openWsPreview(filepath.Join(root, "nope.txt"))
	if a.repo.previewErr == "" {
		t.Fatal("a missing file should set previewErr")
	}
}

func TestWorkspaceTreeRender(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.go"), []byte("package main\n\n// workspace marker line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApp()
	a.ws = newWorkspace(root)
	a.ws.tree.SetOpen(root, true) // the tree loads a directory when it opens
	a.repo.showRepo = true
	a.repo.paneTab = 0
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	tst.Frame() // the first frame opens the root and lists it; the second shows the rows
	if !tst.HasText("hello.go") {
		t.Fatalf("the tree does not list hello.go (texts: %v)", tst.Texts())
	}
	if err := tst.Click("hello.go"); err != nil {
		t.Fatalf("click the tree row: %v", err)
	}
	tst.Frame()
	if a.repo.previewPath == "" || !strings.Contains(a.repo.previewText, "workspace marker line") {
		t.Fatalf("clicking the row did not load the preview: %q %q", a.repo.previewPath, a.repo.previewText)
	}
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("render nil with the preview open")
	}
}

func TestWorkspaceCommands(t *testing.T) {
	a := newApp()
	if msg := a.runCommand("workspace"); msg != "" {
		t.Fatalf("workspace command toast %q", msg)
	}
	if !a.repo.showRepo || a.repo.paneTab != 0 {
		t.Fatalf("workspace command: showRepo=%v paneTab=%d", a.repo.showRepo, a.repo.paneTab)
	}
	a.ws = newWorkspace(t.TempDir())
	a.loadWsDir(a.ws.root)
	if msg := a.runCommand("reload-workspace"); msg == "" {
		t.Fatal("reload command should toast")
	}
	if st := a.wsItemStatus(a.ws.root); st != data.DataUnloaded {
		t.Fatalf("reload left the root %v, want DataUnloaded", st)
	}
	if a.repo.previewPath != "" {
		t.Fatal("reload kept the preview")
	}
}

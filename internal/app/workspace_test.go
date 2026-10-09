package app

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
	a.openFileDrawer(notes)
	if !a.fdraw.open || a.fdraw.title != "notes.md" || a.fdraw.text != "# hello workspace" {
		t.Fatalf("drawer = open=%v %q %q", a.fdraw.open, a.fdraw.title, a.fdraw.text)
	}
	if a.fdraw.lang != "" {
		t.Fatalf("markdown maps to %q, want plain", a.fdraw.lang)
	}
	if a.fdraw.truncated || a.fdraw.err != "" {
		t.Fatalf("unexpected flags: trunc=%v err=%q", a.fdraw.truncated, a.fdraw.err)
	}

	// A Go file maps its language; a huge file truncates at the cap.
	goFile := filepath.Join(root, "main.go")
	if err := os.WriteFile(goFile, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFileDrawer(goFile)
	if a.fdraw.lang != "go" {
		t.Fatalf(".go maps to %q, want go", a.fdraw.lang)
	}
	big := filepath.Join(root, "big.txt")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", int(wsReadCap)+10)), 0o644); err != nil {
		t.Fatal(err)
	}
	a.openFileDrawer(big)
	if !a.fdraw.truncated || len(a.fdraw.text) != int(wsReadCap) {
		t.Fatalf("truncation wrong: trunc=%v len=%d", a.fdraw.truncated, len(a.fdraw.text))
	}
	// A missing file reports its error instead of panicking.
	a.openFileDrawer(filepath.Join(root, "nope.txt"))
	if a.fdraw.err == "" {
		t.Fatal("a missing file should set drawer err")
	}
}

func TestWorkspaceTreeRender(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.go"), []byte("package main\n\n// workspace marker line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApp()
	a.ws = newWorkspace(root)
	a.ws.outline.Open.Add(root) // the tree loads a directory when it opens
	a.repo.showRepo = true
	a.repo.paneTab = 0
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	tst.Frame() // the first frame opens the root and lists it; the second shows the rows
	if !tst.HasText("hello.go") {
		t.Fatalf("the tree does not list hello.go (texts: %v)", tst.Texts())
	}
	if err := tst.Click("tree:" + filepath.Join(root, "hello.go")); err != nil {
		t.Fatalf("click the tree row: %v", err)
	}
	tst.Frame()
	if !a.fdraw.open || !strings.Contains(a.fdraw.text, "workspace marker line") {
		t.Fatalf("clicking the row did not open the drawer: open=%v text=%q", a.fdraw.open, a.fdraw.text)
	}
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("render nil with the drawer open")
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
	a.openFileDrawer(filepath.Join(a.ws.root, "x.txt"))
	if msg := a.runCommand("reload-workspace"); msg == "" {
		t.Fatal("reload command should toast")
	}
	if st := a.wsItemStatus(a.ws.root); st != data.DataUnloaded {
		t.Fatalf("reload left the root %v, want DataUnloaded", st)
	}
	if a.fdraw.open {
		t.Fatal("reload kept the drawer open")
	}
}

// formatSource formats Go with gofmt and pretty-prints JSON in-process;
// other languages (or unparsable sources) report not-formattable.
func TestFormatSource(t *testing.T) {
	raw := "package main\nfunc main(){\nx:=1\n_ = x\n}\n"
	out, ok := formatSource("go", raw)
	if !ok || !strings.Contains(out, "x := 1") {
		t.Fatalf("gofmt view wrong: ok=%v out=%q", ok, out)
	}
	if _, ok := formatSource("go", "not go at all"); ok {
		t.Fatal("unparsable go should not format")
	}
	out, ok = formatSource("json", `{"a":1,"b":[2,3]}`)
	if !ok || !strings.Contains(out, "\n  \"a\": 1") {
		t.Fatalf("json view wrong: %q", out)
	}
	if _, ok := formatSource("shell", "echo hi"); ok {
		t.Fatal("shell should not be formattable")
	}
	if !formattable("go") || !formattable("json") || formattable("python") {
		t.Fatal("formattable set wrong")
	}
}

// fmtView renders raw until toggled, then serves the cached formatted copy
// and recomputes when the content changes.
func TestFmtViewToggle(t *testing.T) {
	f := &fmtView{}
	raw := "package main\nfunc main(){\nx:=1\n_ = x\n}\n"
	if got := f.render("go", raw); len(got) == 0 || got[0] != "package main" {
		t.Fatalf("raw render wrong: %v", got)
	}
	f.tab = 1
	joined := strings.Join(f.render("go", raw), "\n")
	if !strings.Contains(joined, "x := 1") {
		t.Fatalf("formatted render wrong: %q", joined)
	}
	if f.key == "" {
		t.Fatal("the cache key was not set")
	}
	other := "package main\nfunc main(){\ny:=2\n_ = y\n}\n"
	if got := f.render("go", other); strings.Join(got, "\n") == joined {
		t.Fatal("changed content kept the stale formatted cache")
	}
}

// The code drawer offers the Raw/Fmt toggle for formattable files and
// renders both ways.
func TestPreviewFormatToggle(t *testing.T) {
	dir := t.TempDir()
	raw := "package main\n\nfunc main(){\nx:=1\n_ = x\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApp()
	a.ws = newWorkspace(dir)
	a.openFileDrawer(filepath.Join(dir, "main.go"))
	if !formattable(a.fdraw.lang) {
		t.Fatalf("drawer lang %q, want go", a.fdraw.lang)
	}
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("raw drawer render nil")
	}
	a.fdraw.fmt.tab = 1
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("formatted drawer render nil")
	}
	if joined := strings.Join(a.fdraw.fmt.render("go", a.fdraw.text), "\n"); !strings.Contains(joined, "x := 1") {
		t.Fatalf("formatted drawer wrong: %q", joined)
	}
}

// Right-clicking a file in the tree opens the context menu whose first
// entry attaches the file to the conversation.
func TestTreeFileContextMenu(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApp()
	a.ws = newWorkspace(root)
	a.ws.outline.Open.Add(root)
	a.repo.showRepo = true
	a.repo.paneTab = 0
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	tst.Frame()
	if err := tst.RightClick("tree:" + filepath.Join(root, "hello.go")); err != nil {
		t.Fatalf("right-click the file row: %v", err)
	}
	tst.Frame()
	if err := tst.ChooseMenuItem("Add to conversation"); err != nil {
		t.Fatalf("choose the attach item: %v", err)
	}
	tst.Frame() // the chosen item reports in the frame after the menu closes
	if len(a.thread.ctx) != 1 || a.thread.ctx[0].Label != "hello.go" {
		t.Fatalf("the context menu did not attach the file: %+v", a.thread.ctx)
	}
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("render nil with the attachment chip")
	}
}

// The sidebar is a session tree grouped by workspace: the current root's
// node is open, clicking another workspace's row expands it (it does not
// switch), and clicking a session under it opens that session — switching
// the workspace first when it belongs elsewhere.
func TestSidebarWorkspaceList(t *testing.T) {
	dir, other := t.TempDir(), t.TempDir()
	a := newApp()
	a.ws = newWorkspace(dir)
	a.recents = []string{dir, other}
	a.sessions = []session{
		{id: "s1", title: "Root session", ws: dir},
		{id: "s2", title: "Other session", ws: other},
	}
	a.restoreSession() // opens the current workspace's tree node, selects s1
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	if !tst.HasText("Workspaces") {
		t.Fatal("the workspace list section is missing")
	}
	if !tst.HasText("Root session") {
		t.Fatal("the current workspace's sessions are not visible")
	}
	if tst.HasText("Other session") {
		t.Fatal("another workspace's sessions should stay collapsed")
	}
	// Expanding the other workspace reveals its session but does not switch.
	if err := tst.Click("ws:" + other); err != nil {
		t.Fatalf("click the workspace row: %v", err)
	}
	tst.Frame()
	if !a.sessTree.Open.Has("ws:" + other) {
		t.Fatal("clicking the workspace row did not expand it")
	}
	if a.ws.root != dir {
		t.Fatalf("expanding a workspace switched the root: %q", a.ws.root)
	}
	if !tst.HasText("Other session") {
		t.Fatal("the expanded workspace's session is missing")
	}
	// Clicking that session opens it and switches the workspace.
	if err := tst.Click("sess:s2"); err != nil {
		t.Fatalf("click the session row: %v", err)
	}
	tst.Frame()
	if a.ws.root != other || a.sessionID != "s2" {
		t.Fatalf("clicking the session did not open it: root=%q id=%q", a.ws.root, a.sessionID)
	}
}

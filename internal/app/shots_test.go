package app

// shots_test.go renders the app's key surfaces to PNG for the README and
// for visual audits. It is a no-op unless MYGO_UI_SHOTS names a directory:
//
//	MYGO_UI_SHOTS=screenshots go test ./internal/app -run TestScreenshots

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// moduleRoot walks up from the working directory to the directory holding
// go.mod, so a relative MYGO_UI_SHOTS lands at the repo root no matter
// which package's directory the test runs in.
func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

func TestScreenshots(t *testing.T) {
	dir := os.Getenv("MYGO_UI_SHOTS")
	if dir == "" {
		t.Skip("set MYGO_UI_SHOTS to a directory to write the UI screenshots")
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(moduleRoot(), dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	shot := func(name string, a *app) {
		t.Helper()
		tt := ui.NewTester(a.view, 1280, 820)
		tt.Frame()
		img := tt.Image()
		if img == nil {
			t.Fatalf("%s: no frame rendered", name)
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}

	// A representative agent conversation, built by the shared fixture.
	shot("chat-conversation", newConversationApp())

	wk := newApp()
	if wd, err := os.Getwd(); err == nil {
		wk.ws = newWorkspace(wd) // browse the app's own directory for the shot
	}
	wk.runCommand("workspace")
	wk.ws.outline.Open.Add(wk.ws.root)
	wk.loadWsDir(wk.ws.root) // headless: lists synchronously
	shot("workspace-tree", wk)
	for _, e := range wk.wsChildren(wk.ws.root) {
		if filepath.Base(e) == "main.go" || strings.HasSuffix(e, ".go") {
			wk.openFileDrawer(e)
			break
		}
	}
	shot("file-drawer", wk)

	w := newApp()
	w.newThread()
	shot("chat-welcome", w)

	// The Repository shots browse a real throwaway repository with staged,
	// unstaged and untracked changes.
	demo := seedVCSDemo(t)
	d := newApp()
	d.ws = newWorkspace(demo)
	d.runCommand("diff")
	d.loadVCS() // headless: synchronous
	d.loadSelectedDiff()
	shot("repo-diff", d)

	s := newApp()
	s.ws = newWorkspace(demo)
	s.runCommand("source")
	s.loadVCS()
	s.loadSelectedDiff()
	shot("repo-source", s)

	p := newApp()
	p.openProviders()
	shot("settings-providers", p)

	g := newApp()
	g.openAgent()
	shot("settings-agent", g)

	k := newApp()
	k.paletteOpen = true
	shot("command-palette", k)
}

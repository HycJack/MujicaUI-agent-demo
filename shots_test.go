package main

// shots_test.go renders the app's key surfaces to PNG for the README and
// for visual audits. It is a no-op unless MYGO_UI_SHOTS names a directory:
//
//	MYGO_UI_SHOTS=screenshots go test . -run TestScreenshots

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestScreenshots(t *testing.T) {
	dir := os.Getenv("MYGO_UI_SHOTS")
	if dir == "" {
		t.Skip("set MYGO_UI_SHOTS to a directory to write the UI screenshots")
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
	wk.ws.tree.SetOpen(wk.ws.root, true)
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

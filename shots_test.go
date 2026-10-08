package main

// shots_test.go renders the app's key surfaces to PNG for the README and
// for visual audits. It is a no-op unless MYGO_UI_SHOTS names a directory:
//
//	MYGO_UI_SHOTS=screenshots go test . -run TestScreenshots

import (
	"image/png"
	"os"
	"path/filepath"
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

	shot("chat-conversation", newApp()) // the seeded "nightly export" thread

	w := newApp()
	w.newThread()
	shot("chat-welcome", w)

	d := newApp()
	d.runCommand("diff")
	shot("repo-diff", d)

	s := newApp()
	s.runCommand("source")
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

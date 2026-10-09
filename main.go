// Crux is a Codex-style coding agent built on mygo's native UI toolkit
// and the MujicaUI component library: a conversation beside a repo
// inspector, an agent thread that drops thinking, tool-call, file-change,
// terminal and diff cards into the flow, and a status bar that reads like
// a command console. No web page, no HTML, no JavaScript — the whole
// window is GPU-drawn.
//
// Layout of the code (three layers):
//   - internal/store — the data layer: on-disk schemas, atomic writes and
//     the legacy-store migration. No UI imports.
//   - internal/engine — the logic layer: the pi-ai agent loop, the toolset
//     and callback-based streaming. No UI imports.
//   - package main (this directory) — the UI layer: app state, rendering
//     and thin adapters that bridge the engine's callbacks into rows.
package main

import (
	"log"
	"time"

	"crux-agent/internal/store"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	store.Migrate() // pull an older store (%AppData% / ~/.mujicaui-agent-demo) into ~/.crux-agent once
	a := newApp()
	a.loadSettings() // restore the persisted LLM backend, if any
	if p, err := store.SessionsPath(); err == nil {
		a.sessionsPath = p
	}
	if p, err := store.WorkspacePrefsPath(); err == nil {
		a.wsPrefsPath = p
	}
	a.loadWsPrefs() // restore the last workspace (default: the home directory)
	a.loadSessions()
	a.restoreSession()
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "Crux — coding agent",
			Width:     1280,
			Height:    820,
			MinWidth:  1024,
			MinHeight: 640,
			StateKey:  "crux-app",
			Content:   ui.View(a.view),
		})
		// The LLM stream runs on a goroutine; a.redraw marshels its state
		// writes back onto the UI thread through Window.Update and repaints.
		a.redraw = win.Update
		// A one-second tick keeps the status bar's "live" dot breathing
		// while Crux is idle, the way an agent console heartbeats.
		go func() {
			for {
				time.Sleep(time.Second)
				win.Update(func() {})
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

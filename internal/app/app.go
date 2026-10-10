// Package app is Crux's UI layer: the app state, the rendering and the
// thin adapters that bridge the engine's callbacks into transcript rows
// and the store's schemas into persisted JSON. It sits on top of
// internal/store (the data layer) and internal/engine (the logic layer)
// and only touches their exported APIs.
package app

import (
	"time"

	"crux-agent/internal/store"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Run boots the app: migrate an older store, restore the persisted state,
// open the window and pump the event loop. It returns when the window
// closes.
func Run() error {
	registerFonts()
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
		// The LLM stream runs on a goroutine; a.redraw marshals its state
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
	return mygo.App.Run()
}

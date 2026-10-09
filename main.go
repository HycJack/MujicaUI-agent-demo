// Atlas is a Codex-style coding agent built on mygo's native UI toolkit
// and the MujicaUI component library: a conversation beside a repo
// inspector, an agent thread that drops thinking, tool-call, file-change,
// terminal and diff cards into the flow, and a status bar that reads like
// a command console. No web page, no HTML, no JavaScript — the whole
// window is GPU-drawn.
package main

import (
	"log"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	a := newApp()
	a.loadSettings() // restore the persisted LLM backend, if any
	if p, err := sessionsFile(); err == nil {
		a.sessionsPath = p
	}
	if p, err := wsPrefsFile(); err == nil {
		a.wsPrefsPath = p
	}
	a.loadWsPrefs() // restore the last workspace (default: the home directory)
	a.loadSessions()
	a.restoreSession()
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "Atlas — coding agent",
			Width:     1280,
			Height:    820,
			MinWidth:  1024,
			MinHeight: 640,
			StateKey:  "atlas-app",
			Content:   ui.View(a.view),
		})
		// The LLM stream runs on a goroutine; a.redraw marshels its state
		// writes back onto the UI thread through Window.Update and repaints.
		a.redraw = win.Update
		// A one-second tick keeps the status bar's "live" dot breathing
		// while Atlas is idle, the way an agent console heartbeats.
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

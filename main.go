// Crux is a Codex-style coding agent built on mygo's native UI toolkit
// and the MujicaUI component library: a conversation beside a repo
// inspector, an agent thread that drops thinking, tool-call, file-change,
// terminal and diff cards into the flow, and a status bar that reads like
// a command console. No web page, no HTML, no JavaScript — the whole
// window is GPU-drawn.
//
// Layout of the code (three layers plus a bootstrap):
//   - internal/store — the data layer: on-disk schemas, atomic writes and
//     the legacy-store migration. No UI imports.
//   - internal/engine — the logic layer: the pi-ai agent loop, the toolset
//     and callback-based streaming. No UI imports.
//   - internal/app — the UI layer: app state, rendering and thin adapters
//     that bridge the engine's callbacks into transcript rows.
//   - main.go (this file) — a bootstrap that calls app.Run.
package main

import (
	"log"

	"crux-agent/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

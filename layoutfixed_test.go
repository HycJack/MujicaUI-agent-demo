package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The chat pane must lay out at real sizes. Regression test for the
// collapse that ui.Row's default cross alignment (Center, not Stretch)
// caused in content(): without .AlignItems(ui.Stretch) the thread column
// kept its content height and the chat box inside it measured zero, so
// the whole conversation area — messages and composer — was invisible.
//
// The transcript is taller than the viewport and MessageList follows the
// end, so the LAST rows are the visible ones on the first frame; the top
// rows are virtualized away and show with real sizes once scrolled to.
func TestChatPaneLayout(t *testing.T) {
	a := newApp()
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()

	visible := []string{
		"Watching; I'll post the moment tonight's run passes ten minutes.",                                                 // a4, last row
		"Fixed and verified: the job now prunes yesterday's dump first, and last night's run finished **green** in 4m12s.", // a3
		"Nice. Watch it tonight and page me if it slips.",                                                                  // u2
		"OpenAI · GPT-4o", // composer backend label
		"Send",
	}
	for _, s := range visible {
		r, ok := tst.Find(s)
		if !ok || r.W <= 0 || r.H <= 0 {
			t.Fatalf("chat pane element %q not visible (ok=%v rect={%g %g %g %g})", s, ok, r.X, r.Y, r.W, r.H)
		}
	}

	// Scrolling up brings the first rows into view with real sizes.
	tst.Scroll(600, 300, 0, -5000)
	tst.Frame()
	tst.Frame()
	for _, s := range []string{
		"The nightly export failed again. Find out why and fix the job.",
		"The job died on a **quota error** at 02:14 — the archive bucket holds seven days of dumps. I raised the cap and re-ran it.",
	} {
		r, ok := tst.Find(s)
		if !ok || r.W <= 0 || r.H <= 0 {
			t.Fatalf("scrolled-up row %q not visible (ok=%v rect={%g %g %g %g})", s, ok, r.X, r.Y, r.W, r.H)
		}
	}
}

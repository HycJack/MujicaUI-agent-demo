package app

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
	a := newConversationApp()
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()

	visible := []string{
		"Watching; I'll post the moment tonight's run passes ten minutes.", // a2, last row
		"Nice. Watch it tonight and page me if it slips.",                  // u2
		"OpenAI · GPT-4o", // composer backend label
		"Send",
	}
	for _, s := range visible {
		r, ok := tst.Find(s)
		if !ok || r.W <= 0 || r.H <= 0 {
			t.Fatalf("chat pane element %q not visible (ok=%v rect={%g %g %g %g})", s, ok, r.X, r.Y, r.W, r.H)
		}
	}

	// Scrolling up brings the first rows into view with real sizes. The
	// user row is found exactly; the markdown content (paragraph runs,
	// list items, table cells) is asserted by substring — consecutive
	// prose paragraphs merge into one selectable element.
	tst.Scroll(600, 300, 0, -5000)
	tst.Frame()
	tst.Frame()
	r, ok := tst.Find("The nightly export failed again. Find out why and fix the job.")
	if !ok || r.W <= 0 || r.H <= 0 {
		t.Fatalf("scrolled-up row not visible (ok=%v rect={%g %g %g %g})", ok, r.X, r.Y, r.W, r.H)
	}
	for _, s := range []string{
		"the archive bucket holds seven days of dumps.",      // a1's paragraph
		"Raised the retention cap in jobs/export-nightly.sh", // ordered list item
		"quota exceeded",      // table cell
		"Watching; I'll post", // a2 stays rendered
	} {
		if !tst.HasText(s) {
			t.Fatalf("markdown content %q not rendered", s)
		}
	}
}

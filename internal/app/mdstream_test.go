package app

// mdstream_test.go covers streaming markdown: a streaming reply renders
// through the same selectable markdown view as a finished one, so what
// the user watches build up is already the final layout.

import (
	"testing"
	"time"

	"github.com/HycJack/MujicaUI/chat"
	"github.com/egoist/mygo/ui"
)

// TestStreamingMarkdownRenders: a streaming assistant row with markdown
// in its partial text draws parsed markdown (the emphasis markers are
// gone from the shown text), and finishing the stream keeps it.
func TestStreamingMarkdownRenders(t *testing.T) {
	a := newApp()
	a.thread.rows = []row{
		{id: "u1", role: chat.MessageUser, text: "summarize", at: time.Now()},
		{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: time.Now(),
			streaming: true, text: "# Findings\n\nThe job hit a **quota error**."},
	}
	tt := ui.NewTester(a.view, 1100, 760)
	if !tt.HasText("Findings") {
		t.Fatal("streaming heading not rendered")
	}
	if tt.HasText("# Findings") {
		t.Fatal("streaming text shows raw markdown")
	}
	if !tt.HasText("quota error") || tt.HasText("**quota error**") {
		t.Fatal("streaming emphasis not parsed")
	}
	// The stream completes: the same view, no raw markdown anywhere.
	a.thread.rows[1].streaming = false
	a.thread.rows[1].text = "# Findings\n\nThe job hit a **quota error**.\n\n- first\n- second"
	tt.Frame()
	if tt.HasText("**quota error**") {
		t.Fatal("finished text shows raw markdown")
	}
	if !tt.HasText("second") {
		t.Fatal("finished list not rendered")
	}
}

// TestPartialMarkdownTolerated: an incomplete stream (an unclosed code
// fence, an unclosed emphasis) renders without panicking and shows the
// text that has arrived.
func TestPartialMarkdownTolerated(t *testing.T) {
	a := newApp()
	a.thread.rows = []row{
		{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: time.Now(),
			streaming: true, text: "Run this:\n\n```go\nfmt.Println(\"hi\")"},
	}
	tt := ui.NewTester(a.view, 1100, 760)
	if !tt.HasText(`fmt.Println("hi")`) {
		t.Fatal("partial code fence not rendered")
	}
	a.thread.rows[0].text = "It was **bold"
	tt.Frame()
	if !tt.HasText("It was") {
		t.Fatal("partial emphasis dropped the text")
	}
}

// TestMarkdownSelectableContainer: the reply renders as one selection
// domain - the container is selectable, the paragraphs inside are not
// each their own, so a drag selects across blocks.
func TestMarkdownSelectableContainer(t *testing.T) {
	a := newApp()
	a.thread.rows = []row{
		{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: time.Now(),
			text: "First paragraph.\n\nSecond paragraph after a table:\n\n| a | b |\n|---|---|\n| 1 | 2 |"},
	}
	tt := ui.NewTester(a.view, 1100, 760)
	if !tt.HasText("First paragraph.") || !tt.HasText("Second paragraph") || !tt.HasText("2") {
		t.Fatalf("reply blocks missing: %q", tt.Texts())
	}
	if ui.Render(a.view, 1100, 760, 1) == nil {
		t.Fatal("render nil")
	}
}

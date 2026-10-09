package app

// mdview_test.go covers the selectable markdown renderer: the view
// rendering headless, and the message action row — copy on every message,
// regenerate on assistant replies, and the message time. The parser's
// block shapes are covered in internal/md.

import (
	"strings"
	"testing"
	"time"

	"github.com/ZacharyZhang-NY/MujicaUI/chat"
	"github.com/egoist/mygo/ui"
)

func TestMdViewRender(t *testing.T) {
	a := newConversationApp() // a1 carries lists, a table, bold and code spans
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("markdown view render nil")
	}
}

// Every message carries a copy button (user messages included) and its
// time; the copy button writes the row's text to the clipboard.
func TestMessageActionsCopyAndTime(t *testing.T) {
	a := newConversationApp()
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	// Scroll to the first message: virtualized-away rows measure zero, so
	// the click needs a button that is actually on screen.
	tst.Scroll(600, 300, 0, -5000)
	tst.Frame()
	tst.Frame()
	r, ok := tst.Find("Copy")
	if !ok || r.W <= 0 || r.H <= 0 {
		t.Fatalf("no visible copy button (ok=%v rect=%v)", ok, r)
	}
	if err := tst.Click("Copy"); err != nil {
		t.Fatal(err)
	}
	tst.Frame()
	if got := tst.Clipboard(); !strings.Contains(got, "nightly export") {
		t.Fatalf("copy wrote %q", got)
	}
	// The message time renders (the fixture stamps rows two hours back).
	hhmm := a.thread.rows[0].at.Format("15:04")
	if !tst.HasText(hhmm) {
		t.Fatalf("message time %q not shown", hhmm)
	}
}

// Regenerate drops the reply and everything after it, appends a fresh
// streaming reply row, and refuses while a reply is in flight.
func TestRegenerateRow(t *testing.T) {
	a := newConversationApp() // u1 a1 u2 a2
	if msg := a.regenerate(&a.thread.rows[1]); msg != "" {
		t.Fatalf("regenerate refused: %s", msg)
	}
	if len(a.thread.rows) != 2 {
		t.Fatalf("got %d rows after regenerate, want 2 (user + fresh reply)", len(a.thread.rows))
	}
	fresh := a.thread.rows[1]
	// Headless startStream settles the fresh row immediately with its
	// placeholder text; the shape is what matters.
	if fresh.role != chat.MessageAssistant || fresh.kind != rowReasoned || fresh.streaming {
		t.Fatalf("fresh reply row wrong: role=%v kind=%v streaming=%v", fresh.role, fresh.kind, fresh.streaming)
	}
	if a.thread.rows[0].text != "The nightly export failed again. Find out why and fix the job." {
		t.Fatalf("the user prompt did not survive: %q", a.thread.rows[0].text)
	}
	// Headless startStream settles the fresh row immediately.
	_ = fresh

	// A second regenerate on the same (now settled) row works and mints a
	// new row id, so cached element state never leaks between replies.
	first := a.thread.rows[1].id
	if msg := a.regenerate(&a.thread.rows[1]); msg != "" {
		t.Fatalf("second regenerate refused: %s", msg)
	}
	if a.thread.rows[1].id == first {
		t.Fatal("regenerated row reused its id")
	}

	// While a reply streams, regenerate is refused with a toast message.
	a.llm.Busy = true
	if msg := a.regenerate(&a.thread.rows[1]); msg == "" {
		t.Fatal("busy regenerate did not refuse")
	}
}

// A streaming reply with no content yet shows the typing indicator
// instead of an empty bubble.
func TestWaitingIndicatorRenders(t *testing.T) {
	a := newApp()
	a.thread.rows = []row{
		{id: "u1", role: chat.MessageUser, text: "hello", at: time.Now()},
		{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: time.Now(), streaming: true},
	}
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("waiting reply render nil")
	}
}

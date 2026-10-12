package app

// approval_test.go pins the approval flow end to end at the app layer:
// the composer's mode selector IS the approval switch; a pending ask
// renders a permission card on the timeline that settles the engine's
// channel; claude tool names map onto the card kinds; and the engine
// config carries everything the gate needs.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"crux-agent/internal/engine"

	"github.com/HycJack/MujicaUI/agent"
	"github.com/HycJack/MujicaUI/chat"
	"github.com/egoist/mygo/ui"
)

// The composer's segmented control switches the stored mode, and Busy
// freezes it.
func TestComposerModeSelector(t *testing.T) {
	a := newApp()
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	if a.llm.Mode != "agent" && a.llm.Mode != "" {
		t.Fatalf("default mode = %q", a.llm.Mode)
	}
	ro, ok := tst.Find("Read-only")
	if !ok || ro.W == 0 {
		t.Fatal("no Read-only segment")
	}
	tst.ClickAt(ro.X+ro.W/2, ro.Y+ro.H/2)
	tst.Frame()
	if a.llm.Mode != "read-only" {
		t.Fatalf("mode after click = %q", a.llm.Mode)
	}
	full, ok := tst.Find("Full access")
	if !ok || full.W == 0 {
		t.Fatal("no Full access segment")
	}
	tst.ClickAt(full.X+full.W/2, full.Y+full.H/2)
	tst.Frame()
	if a.llm.Mode != "full" {
		t.Fatalf("mode after click = %q", a.llm.Mode)
	}
	// Busy freezes the selector.
	a.llm.Busy = true
	tst.Frame()
	ro, _ = tst.Find("Read-only")
	tst.ClickAt(ro.X+ro.W/2, ro.Y+ro.H/2)
	tst.Frame()
	if a.llm.Mode != "full" {
		t.Fatalf("mode changed while busy: %q", a.llm.Mode)
	}
}

// A pending ask renders a permission card attached to the streaming row;
// Allow once settles the engine's channel and clears the card.
func TestApprovalCardAllow(t *testing.T) {
	a := newApp()
	a.thread.rows = append(a.thread.rows,
		row{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: time.Now(), streaming: true, procOpen: true})
	ch := make(chan engine.ApprovalDecision, 1)
	a.pending = &approvalReq{
		rowID: "a1",
		req:   engine.ApprovalRequest{Tool: "Bash", Summary: "$ rm -rf build", Reason: "the sandbox is unavailable on Windows"},
		ch:    ch,
	}
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	if !tst.HasText("Bash") || !tst.HasText("rm -rf build") {
		t.Fatal("the permission card is missing its tool and summary")
	}
	if !tst.HasText("Allow once") || !tst.HasText("Deny") {
		t.Fatal("the permission card is missing its buttons")
	}
	allow, ok := tst.Find("Allow once")
	if !ok || allow.W == 0 {
		t.Fatal("no Allow once button")
	}
	tst.ClickAt(allow.X+allow.W/2, allow.Y+allow.H/2)
	tst.Frame()
	if a.pending != nil {
		t.Fatal("the card did not clear after the decision")
	}
	select {
	case d := <-ch:
		if !d.Approved || d.Reason != "" {
			t.Fatalf("decision = %+v", d)
		}
	default:
		t.Fatal("the decision never reached the engine's channel")
	}
}

// Deny settles with the denial reason fed back verbatim.
func TestApprovalCardDeny(t *testing.T) {
	a := newApp()
	a.thread.rows = append(a.thread.rows,
		row{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: time.Now(), streaming: true, procOpen: true})
	ch := make(chan engine.ApprovalDecision, 1)
	a.pending = &approvalReq{
		rowID: "a1", req: engine.ApprovalRequest{Tool: "Edit", Summary: "main.go"},
		ch: ch,
	}
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	deny, ok := tst.Find("Deny")
	if !ok || deny.W == 0 {
		t.Fatal("no Deny button")
	}
	tst.ClickAt(deny.X+deny.W/2, deny.Y+deny.H/2)
	tst.Frame()
	select {
	case d := <-ch:
		if d.Approved || d.Reason == "" {
			t.Fatalf("decision = %+v", d)
		}
	default:
		t.Fatal("the denial never reached the channel")
	}
}

// Headless (a.redraw nil) nobody can click: the ask denies when its
// context fires.
func TestAwaitApprovalHeadless(t *testing.T) {
	a := newApp()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	d := a.awaitApproval(ctx, engine.ApprovalRequest{Tool: "Bash", Summary: "x"})
	if d.Approved || d.Reason == "" {
		t.Fatalf("headless decision = %+v", d)
	}
}

// awaitApproval with a live redraw publishes the card on the streaming
// row and passes a settled decision back through the channel.
func TestAwaitApprovalBridge(t *testing.T) {
	a := newApp()
	a.thread.rows = append(a.thread.rows,
		row{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: time.Now(), streaming: true})
	a.redraw = func(fn func()) { fn() } // settle inline
	a.streamRowID = "a1"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for a.pending == nil {
			time.Sleep(time.Millisecond)
		}
		if a.pending.rowID != "a1" {
			t.Errorf("card attached to %q, want a1", a.pending.rowID)
		}
		a.settleApproval(engine.ApprovalDecision{Approved: true})
	}()
	d := a.awaitApproval(ctx, engine.ApprovalRequest{Tool: "Bash", Summary: "go test"})
	if !d.Approved {
		t.Fatalf("bridged decision = %+v", d)
	}
	if a.pending != nil {
		t.Fatal("the card did not clear after the bridge answered")
	}
}

// Claude tool names land on the right card kinds: Bash is a command run,
// Edit/Write a file change, Read a plain tool call.
func TestClaudeToolNameMapping(t *testing.T) {
	a := newApp()
	a.thread.rows = append(a.thread.rows,
		row{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: time.Now()})
	a.appendTurnTool("a1", "t1", "Bash", `{"command":"go build ./..."}`)
	r := &a.thread.rows[0]
	if len(r.tools) != 1 || r.tools[0].run.Command != "go build ./..." || r.tools[0].run.Running != true {
		t.Fatalf("bash card state: %+v", r.tools[0])
	}
	if got := toolTitle(r.tools[0].name); got != "Ran command" {
		t.Fatalf("title = %q", got)
	}

	// An Edit's change data comes from its arguments (claude results are
	// plain text, not the JSON the builtin tools return).
	a.appendTurnTool("a1", "t2", "Edit", `{"file_path":"main.go","old_string":"a","new_string":"b"}`)
	a.finishToolRow("t2", "The file main.go has been updated.", false, time.Second)
	var change *agent.FileChange
	for _, t2 := range r.tools {
		if t2.callID == "t2" {
			change = &t2.change
		}
	}
	if change == nil || change.Path != "main.go" || change.Old != "a" || change.New != "b" {
		t.Fatalf("edit change = %+v", change)
	}

	// Write with only content: a new file, empty old.
	a.appendTurnTool("a1", "t3", "Write", `{"file_path":"x.go","content":"package x"}`)
	a.finishToolRow("t3", "done", false, time.Second)
	for _, t3 := range r.tools {
		if t3.callID == "t3" {
			if t3.change.Path != "x.go" || t3.change.New != "package x" || t3.change.Old != "" {
				t.Fatalf("write change = %+v", t3.change)
			}
		}
	}
}

// The engine config carries the gate's whole state.
func TestEngineConfigCarriesGate(t *testing.T) {
	a := newApp()
	a.llm.Backend = "claude"
	a.llm.Mode = "read-only"
	a.claudeBin = "C:\\claude.cmd"
	a.thread.claudeID = "sess-9"
	a.streamRowID = "a1" // an in-flight row exists
	cfg := a.engineConfig()
	if cfg.Backend != "claude" || cfg.Mode != engine.ModeReadOnly {
		t.Fatalf("backend/mode = %q/%v", cfg.Backend, cfg.Mode)
	}
	if cfg.ClaudeBin != "C:\\claude.cmd" || cfg.ClaudeSessionID != "sess-9" {
		t.Fatalf("claude wiring = %+v", cfg)
	}
	if cfg.OnApproval == nil {
		t.Fatal("no approval handler wired")
	}
}

// The claude session id rides the transcript file.
func TestClaudeIDPersists(t *testing.T) {
	dir := t.TempDir()
	a := newApp()
	a.sessionsPath = filepath.Join(dir, "sessions.json")
	a.ws = newWorkspace(dir)
	a.newThread()
	a.thread.claudeID = "persist-me"
	a.markDirty(a.sessionID)
	a.persistSessions()

	b := newApp()
	b.sessionsPath = a.sessionsPath
	b.ws = newWorkspace(dir)
	b.loadSessions()
	b.restoreSession()
	if b.thread.claudeID != "persist-me" {
		t.Fatalf("claudeID = %q", b.thread.claudeID)
	}
}

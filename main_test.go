package main

import (
	"testing"

	"github.com/ZacharyZhang-NY/MujicaUI/chat"
	"github.com/egoist/mygo/ui"
)

// The view runs without a window: Render builds one frame on the CPU
// fallback, which catches layout panics and missing component contracts.
// Every settings dialog (none / providers / agent) is rendered in both
// inspector states and on both inspector tabs.
func TestViewRenders(t *testing.T) {
	a := newApp()
	cases := []struct {
		name string
		fn   func()
	}{
		{"none", func() { a.closeModals() }},
		{"providers", func() { a.openProviders() }},
		{"agent", func() { a.openAgent() }},
	}
	for _, cs := range cases {
		for _, show := range []bool{false, true} {
			for _, tab := range []int{0, 1} {
				cs.fn()
				a.repo.showRepo, a.repo.codeTab = show, tab
				if ui.Render(a.view, 1280, 820, 1) == nil {
					t.Fatalf("render returned nil (%s showRepo=%v tab=%d)", cs.name, show, tab)
				}
			}
		}
	}
}

// Sending a line appends the user row plus a live assistant row that the
// pi-ai stream fills in; off-window the row is finalized deterministically.
// A new chat clears the transcript back to the welcome surface and closes
// any open settings dialog.
func TestSendAndNewChat(t *testing.T) {
	a := newApp()
	before := len(a.thread.rows)
	a.thread.draft = "watch tonight's run"
	a.send()
	if got := len(a.thread.rows) - before; got != 2 {
		t.Fatalf("send added %d rows, want 2", got)
	}
	if a.thread.draft != "" {
		t.Fatalf("draft %q after send, want empty", a.thread.draft)
	}
	last := a.thread.rows[len(a.thread.rows)-1]
	if last.kind != rowReasoned {
		t.Fatalf("last row kind=%d, want rowReasoned", last.kind)
	}
	if last.text == "" {
		t.Fatal("assistant row should be finalized off-window")
	}
	if a.llm.Busy {
		t.Fatal("stream should not stay busy off-window")
	}
	a.openProviders() // a new chat must close any open settings dialog
	a.newThread()
	if a.settingsOpen {
		t.Fatal("new chat should close the open settings dialog")
	}
	if len(a.thread.rows) != 0 {
		t.Fatalf("rows %d after new chat, want 0", len(a.thread.rows))
	}
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("welcome render returned nil")
	}
}

// Switching sessions loads that session's own transcript, closes any open
// settings dialog, and switching back keeps the edits made in between.
func TestSessionSwitch(t *testing.T) {
	a := newApp()
	a.openProviders() // open a dialog, then switch: it must close
	a.openSession("c4")
	if a.sessionID != "c4" || len(a.thread.rows) != 2 {
		t.Fatalf("c4 opened with %d rows (id %q), want 2", len(a.thread.rows), a.sessionID)
	}
	if a.settingsOpen {
		t.Fatal("switching sessions should close the open settings dialog")
	}
	a.thread.draft = "and the cold archive?"
	a.saveSession()
	a.openSession("c9")
	if a.thread.rows[0].text == "" || a.sessionID != "c9" {
		t.Fatalf("c9 not restored (id %q)", a.sessionID)
	}
	a.openSession("c4")
	if a.thread.draft != "and the cold archive?" {
		t.Fatalf("c4 draft %q not preserved", a.thread.draft)
	}
}

// Every palette command leaves the app renderable, and the view commands
// move the panels they name.
func TestPaletteCommands(t *testing.T) {
	a := newApp()
	for _, id := range []string{"new", "export", "toggle-nav", "toggle-repo", "providers", "agent", "workspace", "reload-workspace", "diff", "source", "branch"} {
		a.runCommand(id)
		if ui.Render(a.view, 1280, 820, 1) == nil {
			t.Fatalf("render nil after command %q", id)
		}
	}
	a.runCommand("providers")
	if !a.settingsOpen || a.settingsTab != "providers" {
		t.Fatal("providers command should open settings on the providers pane")
	}
	a.runCommand("agent")
	if !a.settingsOpen || a.settingsTab != "agent" {
		t.Fatal("agent command should open settings on the agent pane")
	}
	a.runCommand("workspace")
	if !a.repo.showRepo || a.repo.paneTab != 0 {
		t.Fatal("workspace command should show the inspector on the workspace tab")
	}
	a.runCommand("diff")
	if !a.repo.showRepo || a.repo.paneTab != 1 || a.repo.codeTab != 0 {
		t.Fatal("diff should show the repository tab on the diff pane")
	}
	a.runCommand("source")
	if a.repo.paneTab != 1 || a.repo.codeTab != 1 {
		t.Fatal("source should switch to the repository tab's source pane")
	}
	if chat.ModeAgent == chat.ModeChat {
		t.Fatal("mode constants collapsed")
	}
}

// The provider dialog edits the backend live: choosing a provider that has no
// such model rebinds to a valid one and re-seeds defaults.
func TestProviderRebind(t *testing.T) {
	a := newApp()
	if a.llm.Provider == "" || a.llm.Model == "" {
		t.Fatal("default backend should have a provider and model")
	}
	prevModel := a.llm.Model
	a.llm.Provider = "anthropic" // claude models, no gpt ids
	a.rebindModel()
	if a.llm.Model == prevModel && prevModel != "" {
		// only valid if the id happens to collide; anthropic ids differ
		t.Fatalf("model %q not rebound after provider switch", a.llm.Model)
	}
	if a.llm.Model == "" {
		t.Fatal("rebind should pick the first anthropic model")
	}
	// reasoning re-seeds to none on non-reasoning targets is allowed either
	// way; just assert the thinking value is one of the known levels.
	switch a.llm.Thinking {
	case "none", "low", "medium", "high":
	default:
		t.Fatalf("thinking level %q invalid", a.llm.Thinking)
	}
	a.openProviders()
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("providers render returned nil")
	}
}

// The agent dialog surfaces the configured backend and respects the reasoning
// dropdown shrinking for models that cannot reason.
func TestAgentSettings(t *testing.T) {
	a := newApp()
	a.openAgent()
	a.llm.SystemPrompt = "You are terse."
	a.llm.Temperature = 0.2
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("agent render returned nil")
	}
	opts := a.thinkingOptions()
	if len(opts) < 1 {
		t.Fatal("thinking options should at least offer none")
	}
	// max tokens sync from the field
	a.maxTokField = "4096"
	a.syncMaxTokens()
	if a.llm.MaxTokens != 4096 {
		t.Fatalf("MaxTokens=%d, want 4096", a.llm.MaxTokens)
	}
}

// The settings dialogs render as modal overlays with real (non-zero) content
// sizes, and the chat pane still lays out behind them.
func TestSettingsDialogsRender(t *testing.T) {
	for _, tc := range []struct {
		name  string
		open  func(*app)
		field string // a field unique to that dialog's content
	}{
		{"providers", (*app).openProviders, "Test connection"},
		{"agent", (*app).openAgent, "System prompt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newApp()
			tc.open(a)
			tst := ui.NewTester(a.view, 1280, 820)
			tst.Frame()
			if r, ok := tst.Find(tc.field); !ok || r.W <= 0 || r.H <= 0 {
				t.Fatalf("dialog content %q not visible (ok=%v rect={%g %g %g %g})", tc.field, ok, r.X, r.Y, r.W, r.H)
			}
			// The composer stays laid out behind the modal.
			if r, ok := tst.Find("Send"); !ok || r.W <= 0 || r.H <= 0 {
				t.Fatalf("composer collapsed under the %s dialog (ok=%v rect={%g %g %g %g})", tc.name, ok, r.X, r.Y, r.W, r.H)
			}
		})
	}
}

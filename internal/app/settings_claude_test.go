package app

// settings_claude_test.go pins the Agent-type selector on the Providers
// pane: switching to the Claude Code CLI swaps the form (no API key
// field, a CLI detection row instead), and the composer's backend label
// follows the choice.

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The Providers pane opens on the builtin form; clicking the Claude Code
// CLI segment swaps it.
func TestProvidersPaneAgentTypeSwitch(t *testing.T) {
	a := newApp()
	a.openProviders()
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	if !tst.HasText("API key") {
		t.Fatal("the builtin form should show the API key field")
	}
	seg, ok := tst.Find("Claude Code CLI")
	if !ok || seg.W == 0 {
		t.Fatal("the Claude Code CLI segment is not on the pane")
	}
	tst.ClickAt(seg.X+seg.W/2, seg.Y+seg.H/2)
	tst.Frame()
	if a.llm.Backend != "claude" {
		t.Fatalf("backend = %q, want claude", a.llm.Backend)
	}
	if tst.HasText("API key") {
		t.Fatal("the claude form must not show the API key field")
	}
	if !tst.HasText("Claude CLI") {
		t.Fatal("the claude form should have a CLI detection row")
	}
	// Switching back restores the builtin form.
	builtin, ok := tst.Find("Built-in LLM loop")
	if !ok || builtin.W == 0 {
		t.Fatal("the builtin segment is not on the pane")
	}
	tst.ClickAt(builtin.X+builtin.W/2, builtin.Y+builtin.H/2)
	tst.Frame()
	if a.llm.Backend != "builtin" {
		t.Fatalf("backend = %q, want builtin", a.llm.Backend)
	}
	if !tst.HasText("API key") {
		t.Fatal("switching back should restore the API key field")
	}
}

// The claude pane names the CLI path it found (or the absence), carries a
// model alias field, and says the CLI signs in itself.
func TestClaudePaneContent(t *testing.T) {
	a := newApp()
	a.llm.Backend = "claude"
	a.openProviders()
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	if !tst.HasText("Claude CLI") {
		t.Fatal("no Claude CLI row")
	}
	if !tst.HasText("Model") {
		t.Fatal("no model field")
	}
	if !tst.HasText("sign") && !tst.HasText("login") {
		t.Fatal("the pane should say the CLI signs in itself")
	}
}

// The composer's backend label follows the backend choice.
func TestBackendLabelClaude(t *testing.T) {
	a := newApp()
	a.llm.Backend = "claude"
	a.llm.Model = "sonnet"
	if got := a.backendLabel(); got != "claude · sonnet" {
		t.Fatalf("label = %q", got)
	}
	a.llm.Model = ""
	if got := a.backendLabel(); got != "claude · default" {
		t.Fatalf("empty model label = %q", got)
	}
}

// Headless detection (no redraw loop): the call still settles the
// provider view's CLI row without panicking — either a found path or a
// not-found reason.
func TestDetectClaudeHeadless(t *testing.T) {
	a := newApp()
	a.llm.Backend = "claude"
	a.detectClaude()
	if a.provView.claudeErr == "" && a.provView.claudePath == "" {
		t.Fatal("detection reported neither a path nor an error")
	}
}

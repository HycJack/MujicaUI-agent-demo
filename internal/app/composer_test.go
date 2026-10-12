package app

// composer_test.go covers the toolbar inside the composer card. The
// pickers' menus render and close; the choice logic itself is exercised
// through the same setters the menu callbacks use — the tester's
// coordinate clicks cannot reliably address a pop-over's rows (they
// hover-reorder under the pointer), so a click test there would be flaky
// for reasons that are the tester's, not the picker's.

import (
	"os"
	"path/filepath"
	"testing"

	"crux-agent/internal/engine"
	"github.com/HycJack/MujicaUI/chat"
	"github.com/egoist/mygo/ui"
)

// TestComposerModelPickerMenu opens the model menu: the provider's models
// are all offered. Escape closes it.
func TestComposerModelPickerMenu(t *testing.T) {
	a := newApp()
	tt := ui.NewTester(a.view, 1100, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	if err := tt.Click("Select a model"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"GPT-4o", "GPT-4o Mini", "o3", "o4-mini"} {
		if !tt.HasText(want) {
			t.Fatalf("model %q not offered: %q", want, tt.Texts())
		}
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("o4-mini") {
		t.Fatal("escape did not close the menu")
	}
}

// TestComposerSetModelAppliesDefaults switches the model through the
// picker's setter: the model sticks and the reasoning tier follows the
// model's capability.
func TestComposerSetModelAppliesDefaults(t *testing.T) {
	a := newApp()
	if a.llm.Model != "" {
		t.Fatalf("a default model ships: %q", a.llm.Model)
	}
	a.setModel("o3")
	if a.llm.Model != "o3" {
		t.Fatalf("model not switched: %q", a.llm.Model)
	}
	if a.llm.Thinking == "none" || a.llm.Thinking == "" {
		t.Fatalf("a reasoning model kept the tier %q", a.llm.Thinking)
	}
	if _, err := engine.ModelInfoOf(a.llm.Provider, a.llm.Model); err != nil {
		t.Fatalf("switched to an unknown model: %v", err)
	}
}

// TestComposerThinkingPickerNeedsReasoning: a non-reasoning model shows no
// thinking picker; a reasoning model shows one named for the current tier,
// and the choice lands through setThinking.
func TestComposerThinkingPickerNeedsReasoning(t *testing.T) {
	a := newApp()
	tt := ui.NewTester(a.view, 1100, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	if tt.HasText("Thinking") {
		t.Fatal("thinking picker shown for a non-reasoning model")
	}
	a.setModel("o3")
	tt.Frame()
	tier := thinkingLabel(a.llm.Thinking)
	if !tt.HasText(tier) {
		t.Fatalf("no thinking picker for a reasoning model: %q", tt.Texts())
	}
	a.setThinking("high")
	if a.llm.Thinking != "high" {
		t.Fatalf("thinking tier not set: %q", a.llm.Thinking)
	}
}

// TestAttachDialogAttachesWorkspaceFile opens the attach dialog over a
// temp workspace, filters out dot directories, and attaches a file to the
// composer's context chips.
func TestAttachDialogAttachesWorkspaceFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApp()
	a.ws = newWorkspace(dir)
	tt := ui.NewTester(a.view, 1100, 760)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	if err := tt.Click("Attach files"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Filter files") {
		t.Fatal("attach dialog did not open")
	}
	// The hidden directory is not offered.
	if tt.HasText(".git/config") {
		t.Fatal("dot directories leaked into the file list")
	}
	if err := tt.Click("notes.md"); err != nil {
		t.Fatal(err)
	}
	if a.attachOpen {
		t.Fatal("dialog did not close after choosing")
	}
	found := false
	for _, it := range a.thread.ctx {
		if it.Kind == chat.ContextFile && it.Detail == "notes.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("file not attached: %v", a.thread.ctx)
	}
}

// TestComposerEditorPresent: the frameless editor sits in the composer
// card at its minimum height.
func TestComposerEditorPresent(t *testing.T) {
	a := newApp()
	tt := ui.NewTester(a.view, 1100, 760)
	card, ok := tt.Find("Message")
	if !ok {
		t.Fatal("the composer editor is missing")
	}
	if card.H < 44 {
		t.Fatalf("composer editor too short: %v", card)
	}
}

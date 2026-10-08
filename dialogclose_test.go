package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The settings dialog is a real modal: Escape and a backdrop click both
// close it (component-provided), and the X button is present.
func TestDialogCloses(t *testing.T) {
	// Escape closes.
	a := newApp()
	a.openProviders()
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	tst.Key(0, ui.KeyEscape)
	tst.Frame()
	if a.settingsOpen {
		t.Fatal("Escape should close the settings dialog")
	}

	// Backdrop click closes.
	b := newApp()
	b.openAgent()
	tb := ui.NewTester(b.view, 1280, 820)
	tb.Frame()
	tb.ClickAt(40, 400) // far left, on the scrim, outside the centered panel
	tb.Frame()
	if b.settingsOpen {
		t.Fatal("backdrop click should close the settings dialog")
	}
}

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The settings dialog is a real modal: Escape and a backdrop click both
// close it (component-provided), the X button is present, and every close
// path saves the settings.
func TestDialogCloses(t *testing.T) {
	// Escape closes — and the close saves the settings.
	a := newApp()
	a.configPath = filepath.Join(t.TempDir(), "settings.json")
	a.openProviders()
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	tst.Key(0, ui.KeyEscape)
	tst.Frame()
	if a.settingsOpen {
		t.Fatal("Escape should close the settings dialog")
	}
	if _, err := os.Stat(a.configPath); err != nil {
		t.Fatalf("closing via Escape did not save the settings: %v", err)
	}

	// Backdrop click closes.
	b := newApp()
	b.configPath = filepath.Join(t.TempDir(), "settings.json")
	b.openAgent()
	tb := ui.NewTester(b.view, 1280, 820)
	tb.Frame()
	tb.ClickAt(40, 400) // far left, on the scrim, outside the centered panel
	tb.Frame()
	if b.settingsOpen {
		t.Fatal("backdrop click should close the settings dialog")
	}
	if _, err := os.Stat(b.configPath); err != nil {
		t.Fatalf("closing via the backdrop did not save the settings: %v", err)
	}
}

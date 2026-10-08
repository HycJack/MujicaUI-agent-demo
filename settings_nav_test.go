package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The merged Settings dialog has a left source-list that switches panes. The
// "Providers" nav row is unambiguous; the "Agent" row sits directly below it
// (the composer's mode selector also says "Agent", so it is clicked by
// position, derived from the Providers row). Selecting it switches the right
// pane to Agent without closing the dialog.
func TestSettingsNavSwitch(t *testing.T) {
	a := newApp()
	a.openProviders()
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	if a.settingsTab != "providers" {
		t.Fatalf("start tab=%q, want providers", a.settingsTab)
	}
	prov, ok := tst.Find("Providers")
	if !ok || prov.W == 0 || prov.H == 0 {
		t.Fatalf("Providers nav item not found with a real rect: {%g %g %g %g}", prov.X, prov.Y, prov.W, prov.H)
	}
	// Agent nav row = the row directly below Providers (Column gap is 4).
	tst.ClickAt(prov.X+prov.W/2, prov.Y+prov.H+4+prov.H/2)
	tst.Frame()
	if a.settingsTab != "agent" {
		t.Fatalf("after selecting the Agent nav row, tab=%q, want agent", a.settingsTab)
	}
	if !a.settingsOpen {
		t.Fatal("selecting a nav row closed the dialog")
	}
}

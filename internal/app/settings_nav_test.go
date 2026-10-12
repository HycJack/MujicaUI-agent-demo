package app

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

// The settings body's height is fixed from the window size, so switching
// panes must not resize the dialog: the labeled body row keeps its rect
// across tabs, and it adapts when the window does.
func TestSettingsDialogStableHeight(t *testing.T) {
	a := newApp()
	a.openProviders()
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	body, ok := tst.Find("settings-body")
	if !ok || body.H == 0 {
		t.Fatal("the settings body is missing")
	}
	if want := settingsBodyHeight(820); diff(body.H, want) > 0.01 {
		t.Fatalf("body height %g, want %g (the window height drives it)", body.H, want)
	}
	title, ok := tst.Find("Settings")
	if !ok {
		t.Fatal("the dialog title is missing")
	}

	a.settingsTab = "agent"
	tst.Frame()
	agentBody, ok := tst.Find("settings-body")
	if !ok || agentBody.H == 0 {
		t.Fatal("the settings body is missing on the agent pane")
	}
	if diff(agentBody.Y, body.Y) > 1 || diff(agentBody.H, body.H) > 1 {
		t.Fatalf("dialog resized when switching panes: body y %g->%g, h %g->%g",
			body.Y, agentBody.Y, body.H, agentBody.H)
	}
	if atitle, ok := tst.Find("Settings"); !ok || diff(atitle.Y, title.Y) > 1 {
		t.Fatalf("dialog title moved when switching panes: y %g->%g", title.Y, atitle.Y)
	}

	// The fixed height adapts to the window: a shorter window gives a
	// shorter (still stable) body.
	s := newApp()
	s.openProviders()
	st := ui.NewTester(s.view, 1280, 700)
	st.Frame()
	small, ok := st.Find("settings-body")
	if !ok || small.H == 0 {
		t.Fatal("the settings body is missing at 700px")
	}
	if small.H >= body.H {
		t.Fatalf("body height did not adapt to the window: %g at 700px vs %g at 820px", small.H, body.H)
	}
}

// diff reports the absolute difference of two floats.
func diff(a, b float32) float32 {
	if a > b {
		return a - b
	}
	return b - a
}

// settingsBodyHeight scales with the window and clamps so the dialog always
// fits; below the fit ceiling the ceiling wins.
func TestSettingsBodyHeight(t *testing.T) {
	cases := []struct {
		windowH, want float32
	}{
		{640, 420},   // min clamp == fit ceiling at the min window height
		{1000, 620},  // 62% of the window
		{2000, 1240}, // 62% still under the ceiling
		{300, 80},    // tiny window: the fit ceiling wins
	}
	for _, tc := range cases {
		if got := settingsBodyHeight(tc.windowH); got != tc.want {
			t.Errorf("settingsBodyHeight(%g) = %g, want %g", tc.windowH, got, tc.want)
		}
	}
}

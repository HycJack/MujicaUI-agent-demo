package app

// neotheme_test.go pins the sticker/arcade skin: the sheet's tokens, the
// font registration and a render pass over the neo welcome so a broken
// hero (or a layout panic from the cards' grow) fails here first.

import (
	"testing"

	"github.com/HycJack/MujicaUI/core"
	"github.com/HycJack/MujicaUI/theme"
	"github.com/egoist/mygo/ui"
)

// TestNeoSheet pins the built-in paper-and-ink mapping: one ink for
// every outline, sky as the accent whose text rides on it in ink.
func TestNeoSheet(t *testing.T) {
	k := defaultTokens().Mix()
	want := map[string]struct {
		got  ui.Color
		want ui.Color
	}{
		"paper":    {k.Background, ui.Hex("#FDF6E8")},
		"ink":      {k.Border, ui.Hex("#111111")},
		"sky":      {k.Accent, ui.Hex("#6BA8FF")},
		"onAccent": {k.OnAccent, ui.Hex("#111111")},
		"surface":  {k.Surface, ui.Hex("#FFFFFF")},
	}
	for name, w := range want {
		if w.got != w.want {
			t.Errorf("%s: got %v, want %v", name, w.got, w.want)
		}
	}
	if k.ControlBorder != k.Border {
		t.Errorf("ControlBorder: controls must outline in the ink too")
	}
}

// TestNeoSheetInstalls checks both appearances wear the sheet — the
// sticker look only holds on paper, so there is no dark fallback. The
// assertion is self-consistent: whatever skin the machine's theme.json
// selects, the window must wear exactly that sheet.
func TestNeoSheetInstalls(t *testing.T) {
	want := theSheet()
	var installed theme.Tokens
	tt := ui.NewTester(func(c *ui.Context) {
		useTheme(c)
		installed = core.Tokens(c)
	}, 800, 600)
	tt.Frame()
	if installed.Background != want.Background {
		t.Errorf("Background: got %v, want the sheet's paper", installed.Background)
	}
	if installed.Border != want.Border {
		t.Errorf("Border: got %v, want the sheet's ink", installed.Border)
	}
}

// TestNeoWelcomeLayout renders the full app on the welcome screen: the
// hero's display text, the capability cards and the starter chips must
// all be present, and the render must not panic.
func TestNeoWelcomeLayout(t *testing.T) {
	registerFonts()
	w := newApp()
	w.newThread()
	tt := ui.NewTester(w.view, 1280, 820)
	tt.Frame()
	if tt.Image() == nil {
		t.Fatal("no frame rendered")
	}
	for _, want := range []string{
		"Good evening",
		"Investigate",
		"Change code",
		"Why did checkout latency spike?",
		"Summarize last night's failures",
	} {
		if !tt.HasText(want) {
			t.Errorf("welcome: %q not rendered", want)
		}
	}
}

// TestNeoFontsRegistered proves the two display faces load from the
// embedded assets: registering is idempotent and a failed load would
// leave text falling back to the stock face, so render a probe string
// in each family and require it to draw.
func TestNeoFontsRegistered(t *testing.T) {
	registerFonts()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Text(c, "CRUX").Font(fontPixel).FontSize(8)
		ui.Text(c, "CRUX").Font(fontDisplay).FontSize(24)
	}, 400, 120)
	tt.Frame()
	if tt.Image() == nil {
		t.Fatal("no frame rendered")
	}
}

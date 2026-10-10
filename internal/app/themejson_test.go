package app

// themejson_test.go pins the reskin hatch: overrides apply, a missing
// or malformed theme.json falls back to the built-in sheet, and the
// mix fans the eight colors into every role.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestThemeJSONMissing checks the no-file case is the built-in sheet.
func TestThemeJSONMissing(t *testing.T) {
	got := LoadThemeTokens(filepath.Join(t.TempDir(), "theme.json"))
	if got != defaultTokens() {
		t.Errorf("missing file: got %+v, want defaults", got)
	}
	if got.Mix().Background != ui.Hex("#FDF6E8") {
		t.Errorf("missing file: Background not the built-in paper")
	}
}

// TestThemeJSONMalformed checks a bad skin never keeps the app from
// starting: garbage in, defaults out.
func TestThemeJSONMalformed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "theme.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadThemeTokens(p); got != defaultTokens() {
		t.Errorf("malformed file: got %+v, want defaults", got)
	}
}

// TestThemeJSONOverrides checks partial overrides: named colors swap,
// the rest keep the built-in values.
func TestThemeJSONOverrides(t *testing.T) {
	p := filepath.Join(t.TempDir(), "theme.json")
	const json = `{"paper": "#E8F4FD", "sky": "#FF8A5B", "ink": "#202020"}`
	if err := os.WriteFile(p, []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadThemeTokens(p)
	if got.Paper != "#E8F4FD" || got.Sky != "#FF8A5B" || got.Ink != "#202020" {
		t.Errorf("overrides not read: %+v", got)
	}
	if got.Green != defaultTokens().Green {
		t.Errorf("Green should keep the built-in value, got %q", got.Green)
	}
	k := got.Mix()
	if k.Background != ui.Hex("#E8F4FD") {
		t.Errorf("Mix: Background not the overridden paper")
	}
	if k.Accent != ui.Hex("#FF8A5B") {
		t.Errorf("Mix: Accent not the overridden sky")
	}
	if k.Border != ui.Hex("#202020") {
		t.Errorf("Mix: Border not the overridden ink")
	}
}

// TestThemeJSONMixFansOut checks the eight colors reach every role:
// amber is ornament and warning, green success, violet info, muted the
// secondary text, ink every outline and the text on accent.
func TestThemeJSONMixFansOut(t *testing.T) {
	k := defaultTokens().Mix()
	pairs := map[string]struct{ got, want ui.Color }{
		"Ornament":  {k.Ornament, ui.Hex("#FFC24B")},
		"Warning":   {k.Warning, ui.Hex("#FFC24B")},
		"Success":   {k.Success, ui.Hex("#6BE07A")},
		"Info":      {k.Info, ui.Hex("#8B6BFF")},
		"TextMuted": {k.TextMuted, ui.Hex("#635B4A")},
		"OnAccent":  {k.OnAccent, ui.Hex("#111111")},
		"Text":      {k.Text, ui.Hex("#111111")},
	}
	for name, p := range pairs {
		if p.got != p.want {
			t.Errorf("%s: got %v, want %v", name, p.got, p.want)
		}
	}
}

// TestThemeJSONPath checks the sheet lives in the data directory.
func TestThemeJSONPath(t *testing.T) {
	p := themeJSONPath()
	if p == "" {
		t.Skip("no data directory on this machine")
	}
	if filepath.Base(p) != "theme.json" {
		t.Errorf("themeJSONPath: got %q", p)
	}
}

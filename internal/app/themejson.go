package app

// themejson.go is the reskin hatch: ~/.crux-agent/theme.json overrides a
// handful of the sticker sheet's colors (and can switch the dot grain
// off). A missing or malformed file is the built-in sheet — a bad skin
// never keeps the app from starting.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"crux-agent/internal/store"
	"github.com/HycJack/MujicaUI/theme"
	"github.com/egoist/mygo/ui"
)

// ThemeTokens is the theme.json sheet: the overridable colors as hex
// strings plus the dot-grain switch. Every field is optional — empty
// keeps the built-in value. Paper is the window ground, ink every
// outline, sky the accent; the four state colors fan out as in the
// built-in sheet.
type ThemeTokens struct {
	Paper    string `json:"paper,omitempty"`
	Ink      string `json:"ink,omitempty"`
	Sky      string `json:"sky,omitempty"`
	Green    string `json:"green,omitempty"`
	Red      string `json:"red,omitempty"`
	Amber    string `json:"amber,omitempty"`
	Violet   string `json:"violet,omitempty"`
	Muted    string `json:"muted,omitempty"`
	DotGrain *bool  `json:"dotGrid,omitempty"`
}

// defaultTokens is the sheet the built-in palette mixes from; the dot
// grain is part of the style, so it ships on.
func defaultTokens() ThemeTokens {
	on := true
	return ThemeTokens{
		Paper:    "#FDF6E8",
		Ink:      "#111111",
		Sky:      "#6BA8FF",
		Green:    "#6BE07A",
		Red:      "#FF5B4A",
		Amber:    "#FFC24B",
		Violet:   "#8B6BFF",
		Muted:    "#635B4A",
		DotGrain: &on,
	}
}

// LoadThemeTokens reads a theme.json. A missing file is the default
// sheet, not an error; a malformed file falls back too.
func LoadThemeTokens(path string) ThemeTokens {
	t := defaultTokens()
	if path == "" {
		return t
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return t
	}
	var raw ThemeTokens
	if json.Unmarshal(data, &raw) != nil {
		return t
	}
	if raw.Paper != "" {
		t.Paper = raw.Paper
	}
	if raw.Ink != "" {
		t.Ink = raw.Ink
	}
	if raw.Sky != "" {
		t.Sky = raw.Sky
	}
	if raw.Green != "" {
		t.Green = raw.Green
	}
	if raw.Red != "" {
		t.Red = raw.Red
	}
	if raw.Amber != "" {
		t.Amber = raw.Amber
	}
	if raw.Violet != "" {
		t.Violet = raw.Violet
	}
	if raw.Muted != "" {
		t.Muted = raw.Muted
	}
	if raw.DotGrain != nil {
		t.DotGrain = raw.DotGrain
	}
	return t
}

// Mix fans the sheet into MujicaUI's tokens: ink is every border and
// control outline, sky the accent whose text rides on it in ink, amber
// doubles as the ornament, violet is info, and the state colors stay
// saturated chips on paper.
func (t ThemeTokens) Mix() theme.Tokens {
	paper := ui.Hex(t.Paper)
	ink := ui.Hex(t.Ink)
	sky := ui.Hex(t.Sky)
	green := ui.Hex(t.Green)
	red := ui.Hex(t.Red)
	amber := ui.Hex(t.Amber)
	violet := ui.Hex(t.Violet)
	muted := ui.Hex(t.Muted)
	return theme.Tokens{
		Background:    paper,
		Surface:       ui.Hex("#FFFFFF"),
		SurfaceHover:  ui.Hex("#FFF6E0"),
		Text:          ink,
		TextMuted:     muted,
		Border:        ink,
		ControlBorder: ink,

		Accent:        sky,
		AccentHover:   sky.Mix(ui.RGB(255, 255, 255), 0.22),
		AccentPressed: sky.Mix(ink, 0.18),
		OnAccent:      ink,
		AccentText:    ink,
		Selection:     sky.Alpha(0.38),
		Focus:         sky.Alpha(0.62),
		Ornament:      amber,

		Success:    green,
		Warning:    amber,
		Danger:     red,
		Info:       violet,
		DangerFill: red.Alpha(0.18),
		OnDanger:   ink,
	}
}

// themeJSONPath is the sheet's home in the data directory; empty when
// the OS cannot name one (then the built-in sheet applies).
func themeJSONPath() string {
	dir, err := store.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "theme.json")
}

// The sheet loads once per process — a reskin edits theme.json and
// restarts the app.
var (
	sheetOnce sync.Once
	sheetVal  theme.Tokens
	grainOnce sync.Once
	grainVal  bool
)

// theSheet reads the data directory's theme.json once and mixes it;
// every frame reads the cached result, never the file.
func theSheet() theme.Tokens {
	sheetOnce.Do(func() {
		sheetVal = LoadThemeTokens(themeJSONPath()).Mix()
	})
	return sheetVal
}

// theDotGrain reports the sheet's dotGrid switch, read once like the
// colors.
func theDotGrain() bool {
	grainOnce.Do(func() {
		if on := LoadThemeTokens(themeJSONPath()).DotGrain; on != nil {
			grainVal = *on
		}
	})
	return grainVal
}

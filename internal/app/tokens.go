package app

// tokens.go routes MujicaUI's tokens so every Crux pane draws through
// the library's theme rather than a hand-rolled palette. The sheet is
// the neobrutalist sticker/arcade look ported from mygo-agent: cream
// paper, one hard ink for every outline and shadow, sky as the accent.

import (
	"github.com/HycJack/MujicaUI/core"
	"github.com/HycJack/MujicaUI/theme"
	"github.com/egoist/mygo/ui"
)

// tokensT is the theme token set the panes read their colors from.
type tokensT = theme.Tokens

// tokens returns the active theme tokens for the window.
func tokens(c *ui.Context) tokensT {
	return core.Tokens(c)
}

// useTheme installs MujicaUI's theme on the window, once per frame the
// way every MujicaUI example shows. Both appearances wear the same
// sticker sheet: the ink outlines only hold on the paper ground, so
// there is no dark variant to fall back to.
func useTheme(c *ui.Context) {
	sheet := neoSheet()
	core.Use(c, core.Settings{Light: &sheet, Dark: &sheet})
}

// neoSheet mixes the sticker/arcade palette into MujicaUI's tokens.
// Ink is every border and control outline, sky is the accent whose
// text rides on it in ink, amber doubles as the ornament, violet is
// info, and the state colors stay saturated chips on paper.
func neoSheet() theme.Tokens {
	paper := ui.Hex("#FDF6E8")
	ink := ui.Hex("#111111")
	sky := ui.Hex("#6BA8FF")
	return theme.Tokens{
		Background:    paper,
		Surface:       ui.Hex("#FFFFFF"),
		SurfaceHover:  ui.Hex("#FFF6E0"),
		Text:          ink,
		TextMuted:     ui.Hex("#635B4A"),
		Border:        ink,
		ControlBorder: ink,

		Accent:        sky,
		AccentHover:   ui.Hex("#8FBFFF"),
		AccentPressed: ui.Hex("#5491E8"),
		OnAccent:      ink,
		AccentText:    ink,
		Selection:     ui.RGBA(107, 168, 255, 0.38),
		Focus:         ui.RGBA(107, 168, 255, 0.62),
		Ornament:      ui.Hex("#FFC24B"),

		Success:    ui.Hex("#6BE07A"),
		Warning:    ui.Hex("#FFC24B"),
		Danger:     ui.Hex("#FF5B4A"),
		Info:       ui.Hex("#8B6BFF"),
		DangerFill: ui.RGBA(255, 91, 74, 0.18),
		OnDanger:   ink,
	}
}

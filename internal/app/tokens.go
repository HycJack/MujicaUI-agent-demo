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
// there is no dark variant to fall back to. The sheet's colors come
// from ~/.crux-agent/theme.json when one is present (themejson.go).
func useTheme(c *ui.Context) {
	sheet := theSheet()
	core.Use(c, core.Settings{Light: &sheet, Dark: &sheet})
}

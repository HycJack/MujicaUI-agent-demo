package app

// tokens.go routes MujicaUI's tokens so every Crux pane draws through
// the library's theme rather than a hand-rolled palette.

import (
	"github.com/ZacharyZhang-NY/MujicaUI/core"
	"github.com/ZacharyZhang-NY/MujicaUI/theme"
	"github.com/egoist/mygo/ui"
)

// tokensT is the theme token set the panes read their colors from.
type tokensT = theme.Tokens

// tokens returns the active theme tokens for the window.
func tokens(c *ui.Context) tokensT {
	return core.Tokens(c)
}

// useTheme installs MujicaUI's theme on the window, once per frame the
// way every MujicaUI example shows.
func useTheme(c *ui.Context) {
	core.Use(c, core.Settings{})
}

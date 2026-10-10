package app

// neofonts.go carries the two display faces of the sticker/arcade look,
// embedded and registered once at startup: Press Start 2P is the pixel
// voice (section labels, state chips — always at whole-pixel sizes) and
// Bungee the fat display voice (the welcome hero). Both OFL; the
// licenses ride along in the same directory.

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

//go:embed assets/fonts/PressStart2P-Regular.ttf assets/fonts/Bungee-Regular.ttf
var fontFiles embed.FS

// The family names the views ask for with .Font(...).
const (
	fontPixel   = "Press Start 2P"
	fontDisplay = "Bungee"
)

// registerFonts loads the embedded faces into the toolkit. Safe to call
// twice (a re-register is rejected quietly — the first registration
// wins, which is the same face); a failed load keeps the stock faces.
func registerFonts() {
	for _, e := range []struct{ file, family string }{
		{"assets/fonts/PressStart2P-Regular.ttf", fontPixel},
		{"assets/fonts/Bungee-Regular.ttf", fontDisplay},
	} {
		data, err := fontFiles.ReadFile(e.file)
		if err != nil {
			continue
		}
		_ = ui.RegisterFont(data, e.family)
	}
}

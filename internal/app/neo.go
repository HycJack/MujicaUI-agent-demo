package app

// neo.go is the sticker/arcade kit: the handful of shapes the neo-
// brutalist look is built from, drawn over MujicaUI's tokens so the
// sheet stays the single source of color. Nothing here owns state —
// every piece renders from the token snapshot and reports back through
// the callback its caller passes.

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// Stroke weights, corner radii and shadow offsets, per the style
// sheet: cards 2px, chips 1.5px (1px grays out on hi-dpi), buttons and
// chips rounder than cards, shadows always blur 0 offset bottom-right.
const (
	neoStrokeCard = float32(2.0)
	neoStrokeChip = float32(1.5)
	neoRadiusCard = float32(14.0)
	neoRadiusChip = float32(8.0)
	neoShadowCard = float32(4.0)
	neoShadowBtn  = float32(2.0)
)

// neoCard is the workhorse surface: white face, 2px ink outline, the
// 4px hard shadow, the 14px corners. fn builds the content inside a
// padded column.
func neoCard(c *ui.Context, k tokensT, face ui.Color, fn func()) ui.Element {
	return ui.Column(c).FillWidth().Padding(14).Gap(10).Radius(neoRadiusCard).
		Background(face).Border(neoStrokeCard, k.Border).
		Shadow(neoShadowCard, neoShadowCard, 0, 0, k.Border).
		Children(fn)
}

// neoShadow paints the hard offset shadow of a small control; a pressed
// control drops the shadow and shifts onto its place — moving by exactly
// the shadow's extent is the whole "press" feel.
func neoShadow(e ui.Element, k tokensT, off float32) ui.Element {
	if e.Pressed() {
		return e.Margin(off, off, 0, 0)
	}
	return e.Shadow(off, off, 0, 0, k.Border)
}

// neoChip is the status sticker: a saturated face, the 1.5px ink
// outline, the label in pixel caps so state reads as text, never as
// color alone.
func neoChip(c *ui.Context, k tokensT, label string, face ui.Color) {
	neoChipBuild(c, k, face, func() {
		ui.Text(c, strings.ToUpper(label)).Font(fontPixel).FontSize(8).
			TextColor(k.Text)
	})
}

// neoChipBuild is the chip shell for callers that bring their own
// content (an icon, a count).
func neoChipBuild(c *ui.Context, k tokensT, face ui.Color, fn func()) {
	ui.Row(c).AlignItems(ui.Center).Gap(5).Padding(3, 8).Radius(neoRadiusChip).
		Background(face).Border(neoStrokeChip, k.Border).Children(fn)
}

// neoButton is the label button in its two voices: primary fills with
// the sky, plain stays card-white. Both carry the ink outline, the 2px
// hard shadow and the press-that-lands.
func neoButton(c *ui.Context, k tokensT, label string, primary bool, fn func()) {
	b := ui.ButtonBase(c).Label(label).Padding(6, 14).Radius(neoRadiusChip).Gap(6).
		Border(neoStrokeCard, k.Border).Cursor(ui.CursorPointer)
	if primary {
		b.Background(k.Accent)
	} else {
		b.Background(k.Surface)
	}
	if b.Hovered() {
		if primary {
			b.Background(k.AccentHover)
		} else {
			b.Background(k.SurfaceHover)
		}
	}
	neoShadow(b, k, neoShadowBtn)
	if b.Clicked() {
		fn()
	}
	b.Children(func() {
		ui.Text(c, label).FontSize(12.5).Bold().TextColor(k.Text)
	})
}

// neoPixelSVG parses a filled sprite on a crisp grid: no strokes, no
// anti-aliasing — the rect edges stay square at every size.
func neoPixelSVG(viewBox, shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="` + viewBox + `" shape-rendering="crispEdges" fill="currentColor" stroke="none">` + shapes + `</svg>`))
}

// The pixel sprites of the decoration layer — the arcade-flyer
// confetti that sits around the welcome hero.
var (
	neoPixelSparkle = neoPixelSVG("0 0 8 8",
		`<rect x="3" y="0" width="2" height="8"/>`+
			`<rect x="0" y="3" width="8" height="2"/>`+
			`<rect x="2" y="2" width="1" height="1"/>`+
			`<rect x="5" y="2" width="1" height="1"/>`+
			`<rect x="2" y="5" width="1" height="1"/>`+
			`<rect x="5" y="5" width="1" height="1"/>`)
	neoPixelCloud = neoPixelSVG("0 0 10 5",
		`<rect x="2" y="0" width="2" height="1"/>`+
			`<rect x="5" y="0" width="2" height="1"/>`+
			`<rect x="1" y="1" width="7" height="1"/>`+
			`<rect x="0" y="2" width="10" height="3"/>`)
	neoPixelHeart = neoPixelSVG("0 0 8 7",
		`<rect x="1" y="0" width="2" height="1"/>`+
			`<rect x="5" y="0" width="2" height="1"/>`+
			`<rect x="0" y="1" width="8" height="2"/>`+
			`<rect x="1" y="3" width="6" height="1"/>`+
			`<rect x="2" y="4" width="4" height="1"/>`+
			`<rect x="3" y="5" width="2" height="1"/>`+
			`<rect x="3.5" y="6" width="1" height="1"/>`)
)

// neoSidebarBG is the side panels' deeper cream: the paper mixed a
// step toward the amber, the way the style sheet separates the desk's
// margins from its cards.
func neoSidebarBG(k tokensT) ui.Color {
	return k.Background.Mix(k.Warning, 0.10)
}

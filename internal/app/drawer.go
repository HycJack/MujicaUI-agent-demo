package app

// drawer.go is Crux's large right-hand code viewer: an overlay.Drawer that
// slides over the inspector. The workspace tree opens file contents in it
// (replacing the old small inline preview box), and the repository pane
// maximizes its diff or source into it. The drawer's content area is a
// scroll, so the viewer inside gets an explicit height derived from the
// window instead of a grow.

import (
	"crux-agent/internal/fsutil"

	"github.com/ZacharyZhang-NY/MujicaUI/code"
	"github.com/ZacharyZhang-NY/MujicaUI/git"
	"github.com/ZacharyZhang-NY/MujicaUI/overlay"
	"github.com/egoist/mygo/ui"
)

// openFileDrawer loads a workspace file into the drawer and slides it in —
// async with a window, synchronous headless.
func (a *app) openFileDrawer(path string) {
	d := &a.fdraw
	d.title = relToRoot(a.ws.root, path)
	d.lang = fsutil.PreviewLang(path)
	d.diffMode = false
	d.fmt = fmtView{}
	d.text, d.err = "", ""
	d.truncated, d.loading = false, true
	d.open = true
	if a.redraw == nil {
		text, truncated, err := fsutil.ReadCapped(path, fsutil.ReadCap)
		a.applyFileDrawer(text, truncated, err)
		return
	}
	go func() {
		text, truncated, err := fsutil.ReadCapped(path, fsutil.ReadCap)
		a.redraw(func() { a.applyFileDrawer(text, truncated, err) })
	}()
}

// applyFileDrawer stores a finished file read.
func (a *app) applyFileDrawer(text string, truncated bool, err error) {
	d := &a.fdraw
	d.loading = false
	if err != nil {
		d.err = err.Error()
		return
	}
	d.text, d.truncated = text, truncated
}

// openSourceDrawer shows ready-made text (the repository pane's worktree
// copy) in file mode.
func (a *app) openSourceDrawer(text, lang, title string) {
	d := &a.fdraw
	d.title, d.lang = title, lang
	d.diffMode = false
	d.fmt = fmtView{}
	d.text, d.err, d.loading, d.truncated = text, "", false, false
	d.open = true
}

// openDiffDrawer maximizes the repository pane's current diff.
func (a *app) openDiffDrawer(from, to string, lang, title string) {
	d := &a.fdraw
	d.title, d.lang = title, lang
	d.diffMode, d.from, d.to = true, from, to
	d.fmt = fmtView{}
	d.text, d.err, d.loading, d.truncated = "", "", false, false
	d.open = true
}

// fileDrawerView builds the drawer every frame; the component renders only
// while open and Escape / the header ✕ close it.
func (a *app) fileDrawerView(c *ui.Context) {
	d := &a.fdraw
	title := d.title
	if title == "" {
		title = "File viewer" // Drawer panics on an empty title
	}
	overlay.Drawer(c, &d.open, overlay.DrawerOptions{
		Title: title,
		Side:  overlay.DrawerRight,
		Size:  720,
	}, func() { a.fileDrawerBody(c) })
}

// fileDrawerBody is the drawer's content: a toolbar (truncation note,
// Raw/Fmt toggle, attach) over a viewer that fills the drawer's height.
// The height is derived from the window because the drawer wraps its
// content in a scroll, where a grow would collapse.
func (a *app) fileDrawerBody(c *ui.Context) {
	k := tokens(c)
	d := &a.fdraw
	_, wh := c.Size()
	h := wh - 130 // drawer chrome: header, panel padding, slack
	ui.Column(c).Gap(8).Height(h).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if d.truncated {
				ui.Text(c, "truncated at 256 KiB").FontSize(10).TextColor(k.TextMuted)
			}
			ui.Text(c, "").Grow(1)
			if !d.diffMode && fsutil.Formattable(d.lang) {
				ui.Segmented(c, &d.fmt.tab, "Raw", "Fmt").Width(108)
			}
			if !d.diffMode {
				attach := ui.Button(c, "Attach to conversation")
				if attach.Clicked() {
					c.Toast(a.attachFile(d.title))
				}
			}
		})
		ui.Box(c).Grow(1).MinHeight(0).Clip().Children(func() {
			switch {
			case d.loading:
				ui.Text(c, "Loading…").FontSize(12).TextColor(kTextMuted(c))
			case d.err != "":
				ui.Text(c, "⚠ "+d.err).FontSize(12).TextColor(kDanger(c))
			case d.diffMode:
				git.DiffViewer(c, &d.ddiff, d.from, d.to, git.DiffViewerOptions{Language: d.lang}).Fill()
			default:
				code.CodeViewer(c, d.fmt.render(d.lang, d.text), &d.src,
					code.CodeViewerOptions{Language: d.lang, Label: d.title}).Element.Fill()
			}
		})
	})
}

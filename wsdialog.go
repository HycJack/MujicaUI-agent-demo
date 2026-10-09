package main

// wsdialog.go is the Open-workspace modal: a directory field plus the
// recent workspaces. The switching logic itself lives in persist.go.

import (
	"path/filepath"

	"crux-agent/internal/store"
	"github.com/ZacharyZhang-NY/MujicaUI/icons"
	"github.com/ZacharyZhang-NY/MujicaUI/input"
	"github.com/ZacharyZhang-NY/MujicaUI/overlay"
	"github.com/egoist/mygo/ui"
)

// openWsDialog opens the workspace picker with the current root prefilled.
func (a *app) openWsDialog() {
	a.wsPathField = a.ws.root
	a.wsDialogOpen = true
}

// workspaceDialog is the Open-workspace modal. It runs every frame like
// the Settings dialog.
func (a *app) workspaceDialog(c *ui.Context) {
	overlay.Dialog(c, &a.wsDialogOpen, overlay.DialogOptions{
		Title:       "Open workspace",
		Description: "Sessions and the file tree are kept per workspace. Pick a recent folder or type a path.",
		Width:       560,
		Actions: func() {
			if input.Button(c, "Open", input.ButtonOptions{}).Clicked() {
				if msg := a.openWorkspace(a.wsPathField); msg != "" {
					c.Toast(msg)
				}
			}
		},
	}, func() { a.wsDialogBody(c) })
}

// wsDialogBody is the picker's content: the path field and the recents.
func (a *app) wsDialogBody(c *ui.Context) {
	k := tokens(c)
	ui.Column(c).Gap(14).Children(func() {
		input.FormField(c, "Directory", input.FormFieldOptions{Description: "An absolute path to an existing folder."}, func() *ui.Element {
			return input.InputGroup(c, &a.wsPathField, input.InputGroupOptions{Placeholder: store.HomeDir(), Label: "Directory"}).Input
		})
		ui.Text(c, "Recent workspaces").FontSize(11).TextColor(k.TextMuted)
		if len(a.recents) == 0 {
			ui.Text(c, "No recent workspaces yet").FontSize(12).TextColor(kTextMuted(c))
			return
		}
		for _, r := range a.recents {
			root := r
			b := ui.ButtonBase(c).Label("recent:" + root).Tooltip(root).FillWidth().Radius(7).Padding(8).Cursor(ui.CursorPointer)
			if b.Hovered() {
				b.Background(k.SurfaceHover)
			}
			if b.Clicked() {
				if msg := a.openWorkspace(root); msg != "" {
					c.Toast(msg)
				}
			}
			b.Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icons.Must("folder")).FontSize(14).TextColor(k.Accent)
					ui.Column(c).Gap(1).Children(func() {
						ui.Text(c, filepath.Base(root)).FontSize(12).Bold().SingleLine()
						ui.Text(c, root).FontSize(10).TextColor(k.TextMuted).SingleLine()
					})
				})
			})
		}
	})
}

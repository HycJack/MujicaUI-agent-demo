package app

// attachdialog.go is the attach-file modal: a filter field over the flat
// list of the workspace's files (collected when the dialog opens), one
// click to hang a file on the composer's context chips. The walking and
// the attachment logic live here; the composer side is thread.go.

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/HycJack/MujicaUI/icons"
	"github.com/HycJack/MujicaUI/input"
	"github.com/HycJack/MujicaUI/overlay"
	"github.com/HycJack/MujicaUI/theme"
	"github.com/egoist/mygo/ui"
)

// attachLimit caps the collected file list; a repo larger than this needs
// the filter field anyway.
const attachLimit = 400

// openAttachDialog collects the workspace's files and opens the dialog.
func (a *app) openAttachDialog() {
	a.attachQuery = ""
	a.attachFiles = collectWorkspaceFiles(a.ws.root)
	a.attachOpen = true
}

// collectWorkspaceFiles lists the workspace's files as slash-separated
// paths relative to the root, skipping VCS, dependency and hidden
// directories, capped at attachLimit.
func collectWorkspaceFiles(root string) []string {
	if root == "" {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are simply not offered
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".git" || rel == "node_modules" || rel == "vendor" ||
				strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if len(out) < attachLimit {
			out = append(out, rel)
		} else {
			return filepath.SkipAll
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// attachDialog runs the attach-file modal every frame, like the other
// dialogs.
func (a *app) attachDialog(c *ui.Context) {
	overlay.Dialog(c, &a.attachOpen, overlay.DialogOptions{
		Title:       "Attach files",
		Description: "Files ride along with the next message; the model reads their contents.",
		Width:       520,
	}, func() { a.attachBody(c) })
}

// attachBody is the dialog's content: the filter field and the matching
// files.
func (a *app) attachBody(c *ui.Context) {
	k := tokens(c)
	ui.Column(c).Gap(10).Height(380).Children(func() {
		input.SearchInput(c, &a.attachQuery, input.SearchInputOptions{Placeholder: "Filter files"})
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Gap(2).FillWidth().Children(func() {
				q := strings.ToLower(strings.TrimSpace(a.attachQuery))
				shown := 0
				for _, rel := range a.attachFiles {
					if q != "" && !strings.Contains(strings.ToLower(rel), q) {
						continue
					}
					if shown >= 60 {
						ui.Text(c, "… refine the filter to see more").FontSize(11).TextColor(k.TextMuted).Padding(6)
						return
					}
					shown++
					row := ui.ButtonBase(c).Label(rel).FillWidth().Padding(6, 8).Radius(theme.ControlRadius).Gap(8).
						AlignItems(ui.Center).Cursor(ui.CursorPointer)
					if row.Hovered() {
						row.Background(k.SurfaceHover)
					}
					if row.Clicked() {
						if msg := a.attachFile(rel); msg != "" {
							c.Toast(msg)
						}
						a.attachOpen = false
						return
					}
					row.Children(func() {
						ui.Icon(c, icons.Must("file-text")).FontSize(13).TextColor(k.TextMuted)
						ui.Text(c, rel).FontSize(12).SingleLine()
					})
				}
				if shown == 0 {
					ui.Text(c, "No files match").FontSize(12).TextColor(k.TextMuted).Padding(6)
				}
			})
		})
	})
}

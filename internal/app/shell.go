package app

// shell.go is the Crux desk: a title bar on top, a conversation sidebar
// on the left, the working pane in the middle, a repo inspector that
// slides in on the right, and a status bar along the bottom. It is built
// from mygo's flex primitives plus MujicaUI's chrome, the same way the
// dashboard's shell folds together.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"crux-agent/internal/fsutil"
	"github.com/HycJack/MujicaUI/chat"
	"github.com/HycJack/MujicaUI/data"
	"github.com/HycJack/MujicaUI/icons"
	"github.com/HycJack/MujicaUI/layout"
	"github.com/egoist/mygo/ui"
)

// view is the whole window. Crux installs MujicaUI's theme, lays the
// desk out over the sticker sheet's dot grain, then binds the console
// shortcuts and the command palette.
func (a *app) view(c *ui.Context) {
	useTheme(c)
	k := tokens(c)
	root := ui.Column(c).Fill().Background(k.Background)
	if a.dotGrid {
		root.Draw(neoDotGrid(k, 22, 1.2))
	}
	root.Children(func() {
		a.titlebar(c, k)
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			if a.navOpen {
				a.sidebar(c, k)
				ui.Box(c).Width(1).Background(k.Border)
			}
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				a.content(c, k)
			})
		})
		a.statusbar(c, k)
	})
	a.settingsDialogs(c)
	a.workspaceDialog(c)
	a.attachDialog(c)
	a.fileDrawerView(c)
	a.shortcuts(c)
	a.palette(c)
}

// shortcuts binds the console keys: ⌘B folds the sessions, ⌘J the repo
// inspector, ⌘N a new chat; ⌘K opens the command palette.
func (a *app) shortcuts(c *ui.Context) {
	if c.Shortcut(ui.Cmd, ui.KeyB) {
		a.navOpen = !a.navOpen
	}
	if c.Shortcut(ui.Cmd, ui.KeyJ) {
		a.repo.showRepo = !a.repo.showRepo
	}
	if c.Shortcut(ui.Cmd, ui.KeyN) {
		a.newThread()
	}
	if c.Shortcut(ui.Cmd, ui.KeyK) {
		a.paletteOpen = true
	}
}

// titlebar is the window's top row: the sticker mark and name, a sidebar
// toggle, then the model and the inspector toggle.
func (a *app) titlebar(c *ui.Context, k tokensT) {
	layout.TitleBar(c, "Crux", layout.TitleBarOptions{
		Leading: func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				// The mark is the app's sticker: a sky block with the
				// ink outline and hard shadow, the name in the display
				// face.
				mark := ui.Box(c).Size(26, 26).Radius(neoRadiusChip).Center().
					Background(k.Accent).Border(neoStrokeCard, k.Border)
				mark.Shadow(neoShadowBtn, neoShadowBtn, 0, 0, k.Border)
				mark.Children(func() {
					ui.Text(c, "C").Font(fontDisplay).FontSize(12).TextColor(k.OnAccent)
				})
				ui.Text(c, "Crux").FontSize(14).Bold()
				a.iconToggle(c, k, "menu", "Toggle sessions", func() { a.navOpen = !a.navOpen })
			})
		},
		Trailing: func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.modelName()).FontSize(12).TextColor(k.TextMuted)
				a.iconToggle(c, k, "settings", "Provider settings", func() { a.openProviders() })
				a.iconToggle(c, k, "bot", "Agent settings", func() { a.openAgent() })
				a.iconToggle(c, k, "folder", "Workspace tree", func() {
					a.repo.showRepo, a.repo.paneTab = true, 0
				})
				a.iconToggle(c, k, "git-pull-request", "Toggle repo inspector", func() { a.repo.showRepo = !a.repo.showRepo })
				ui.Avatar(c, "Ada Lovelace", nil).Tooltip("Ada Lovelace")
			})
		},
	})
}

// sidebar is the session tree under a New chat button: sessions group by
// workspace directory — the way Codex-style consoles group projects — but
// the home directory is never shown as a group; its sessions sit flat.
// Clicking a workspace toggles it open; clicking a session opens it (and
// switches workspace first when it belongs elsewhere).
func (a *app) sidebar(c *ui.Context, k tokensT) {
	ui.Column(c).Width(260).Shrink(0).Background(neoSidebarBG(k)).Padding(10, 10, 10).Gap(8).Children(func() {
		ui.Row(c).FillWidth().Children(func() {
			if b := ui.PrimaryButton(c, "New chat"); b.Clicked() {
				a.newThread()
			}
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "WORKSPACES").Font(fontPixel).FontSize(8).TextColor(k.TextMuted).Grow(1)
			a.iconToggle(c, k, "plus", "Open workspace…", a.openWsDialog)
		})
		restore := data.DataSelectTheme(c, 0)
		e := ui.Outline(c, &a.sessTree, a.sessionTreeRoots(), a.sessionTreeChildren, func(item string) {
			a.sessionTreeRow(c, k, item)
		})
		restore()
		e.Grow(1).MinHeight(0)
	})
}

// sessionTreeRoots lists the tree's top level: the current workspace first,
// then the recents — the home directory never appears as a group; when it
// is the current root its sessions are listed flat instead.
func (a *app) sessionTreeRoots() []string {
	out := []string{}
	homeRoot := isHomeDir(a.ws.root)
	if a.ws.root != "" && !homeRoot {
		out = append(out, "ws:"+a.ws.root)
	}
	for _, r := range a.recents {
		if r != a.ws.root && !isHomeDir(r) {
			out = append(out, "ws:"+r)
		}
	}
	if homeRoot {
		for _, s := range a.sessions {
			if s.ws == a.ws.root {
				out = append(out, "sess:"+s.id)
			}
		}
	}
	return out
}

// isHomeDir reports whether path is the user's home directory.
func isHomeDir(path string) bool {
	if path == "" {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	return filepath.Clean(path) == filepath.Clean(home)
}

// sessionTreeChildren returns a workspace's sessions ("sess:<id>") or nil
// for a session leaf.
func (a *app) sessionTreeChildren(item string) []string {
	path, ok := strings.CutPrefix(item, "ws:")
	if !ok {
		return nil
	}
	out := []string{}
	for _, s := range a.sessions {
		if s.ws == path {
			out = append(out, "sess:"+s.id)
		}
	}
	return out
}

// sessionTreeRow builds one sidebar-tree row, sticker style: a workspace
// line (folder, name, count chip; click toggles open) or a session line —
// the state dot, the title and the right-aligned age — where the current
// session is the full sky block, the slot-machine selection rule.
func (a *app) sessionTreeRow(c *ui.Context, k tokensT, item string) {
	if path, ok := strings.CutPrefix(item, "ws:"); ok {
		count := 0
		for _, s := range a.sessions {
			if s.ws == path {
				count++
			}
		}
		row := ui.Row(c).Label(item).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).
			Tooltip(path).Cursor(ui.CursorPointer).Padding(3, 6).Radius(neoRadiusChip)
		if row.Clicked() {
			if a.sessTree.Open.Has(item) {
				a.sessTree.Open.Remove(item)
			} else {
				a.sessTree.Open.Add(item)
			}
		}
		row.Children(func() {
			ui.Icon(c, icons.Must("folder")).FontSize(14)
			ui.Text(c, fsutil.WorkspaceName(path)).FontSize(12).Bold().SingleLine().Grow(1).MinWidth(0)
			if count > 0 {
				neoChipBuild(c, k, k.Surface, func() {
					ui.Text(c, strconv.Itoa(count)).FontSize(10).Bold().TextColor(k.Text)
				})
			}
		})
		return
	}
	id, _ := strings.CutPrefix(item, "sess:")
	var sess session
	for _, s := range a.sessions {
		if s.id == id {
			sess = s
			break
		}
	}
	current := id == a.sessionID
	row := ui.Row(c).Label(item).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Padding(3, 6).Radius(neoRadiusChip)
	if current {
		row.Background(k.Accent)
	} else if row.Hovered() {
		row.Background(k.SurfaceHover)
	}
	if row.Clicked() {
		if sess.ws != "" && filepath.Clean(sess.ws) != filepath.Clean(a.ws.root) {
			if msg := a.openWorkspace(sess.ws); msg != "" {
				c.Toast(msg)
			}
		}
		a.openSession(id)
	}
	row.Children(func() {
		// The state dot: the open session's eye is filled, the rest are
		// hollow — state never rides on color alone.
		dot := ui.Box(c).Size(8, 8).Radius(4).Border(neoStrokeChip, k.Border)
		if current {
			dot.Background(k.Border)
		}
		ui.Text(c, sess.title).FontSize(12).SingleLine().Grow(1).MinWidth(0).
			TextColor(k.Text)
		if !sess.updated.IsZero() {
			ui.Text(c, sess.updated.Format("Jan 2")).FontSize(10).Bold().
				TextColor(k.TextMuted).SingleLine()
		}
	})
}

// content is the working pane: the thread, with the repo inspector sliding in
// beside it when open.
//
// Note the explicit AlignItems(Stretch): ui.Row's default cross alignment is
// Center, so without it the thread column keeps its content height and the
// Grow(1) chat box inside it collapses to zero.
func (a *app) content(c *ui.Context, k tokensT) {
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Padding(12).Children(func() {
			a.threadView(c)
		})
		if a.repo.showRepo {
			ui.Box(c).Width(1).Background(k.Border)
			a.repoPane(c, k)
		}
	})
}

// statusbar is the sticker strip along the bottom: workspace, branch and
// mode as paper chips, the context window as the segmented meter, and the
// live / changed state chips on the right.
func (a *app) statusbar(c *ui.Context, k tokensT) {
	ui.Column(c).Shrink(0).Children(func() {
		ui.Box(c).FillWidth().Height(2).Background(k.Border)
		ui.Row(c).FillWidth().Padding(5, 12).Gap(8).AlignItems(ui.Center).
			Background(neoSidebarBG(k)).Children(func() {
			neoChipBuild(c, k, k.Surface, func() {
				ui.Icon(c, icons.Must("folder")).FontSize(12).TextColor(k.Text)
				ui.Text(c, fsutil.WorkspaceName(a.ws.root)).FontSize(11).Bold().SingleLine()
			})
			neoChipBuild(c, k, k.Surface, func() {
				ui.Icon(c, icons.Must("git-branch")).FontSize(12).TextColor(k.Text)
				ui.Text(c, a.repo.branch).FontSize(11).Bold().SingleLine()
			})
			neoChipBuild(c, k, k.Surface, func() {
				ui.Icon(c, icons.Must("brain")).FontSize(12).TextColor(k.Text)
				ui.Text(c, strings.ToUpper(modeName(a.thread.mode))).Font(fontPixel).FontSize(8)
			})
			neoBlockMeter(c, k, 8, 10, k.Accent)
			ui.Text(c, "6.4K/8K").Font(fontPixel).FontSize(8).TextColor(k.TextMuted)
			ui.Spacer(c)
			neoStatusDot(c, k, "live", k.Success)
			neoChip(c, k, "3 changed", k.Warning)
		})
	})
}

// iconToggle is a small square sticker button: the ink outline, the 2px
// hard shadow and the press-that-lands; tint lights it when the
// associated state is on.
func (a *app) iconToggle(c *ui.Context, k tokensT, icon, label string, fn func()) {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Size(28, 28).Radius(neoRadiusChip).Center().Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(k.SurfaceHover)
	} else {
		b.Background(k.Surface)
	}
	b.Border(neoStrokeChip, k.Border)
	neoShadow(b, k, neoShadowBtn)
	if b.Clicked() {
		fn()
	}
	b.Children(func() {
		ui.Icon(c, icons.Must(icon)).FontSize(15).TextColor(k.TextMuted)
	})
}

// modelName returns the active model's display name.
func (a *app) modelName() string {
	return a.backendLabel()
}

// modeName labels a chat mode for the status bar.
func modeName(m chat.ChatMode) string {
	if m == chat.ModeAgent {
		return "agent"
	}
	return "chat"
}

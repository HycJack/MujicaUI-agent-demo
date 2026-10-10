package app

// shell.go is the Crux desk: a title bar on top, a conversation sidebar
// on the left, the working pane in the middle, a repo inspector that
// slides in on the right, and a status bar along the bottom. It is built
// from mygo's flex primitives plus MujicaUI's chrome, the same way the
// dashboard's shell folds together.

import (
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
// desk out, then binds the console shortcuts and the command palette.
func (a *app) view(c *ui.Context) {
	useTheme(c)
	k := tokens(c)
	ui.Column(c).Fill().Background(k.Background).Children(func() {
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

// titlebar is the window's top row: the mark and name, a sidebar toggle,
// then the model and the inspector toggle.
func (a *app) titlebar(c *ui.Context, k tokensT) {
	layout.TitleBar(c, "Crux", layout.TitleBarOptions{
		Leading: func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icons.Must("bot")).FontSize(16).TextColor(k.Accent)
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

// sidebar is the session tree: one level per workspace directory, its
// sessions nested beneath it — the way Codex-style consoles group projects.
// Clicking a workspace toggles it open; clicking a session opens it (and
// switches workspace first when it belongs elsewhere).
func (a *app) sidebar(c *ui.Context, k tokensT) {
	ui.Column(c).Width(260).Shrink(0).Background(k.Surface).Padding(10, 10, 10).Gap(8).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Workspaces").FontSize(11).TextColor(k.TextMuted).Grow(1)
			a.iconToggle(c, k, "plus", "Open workspace…", a.openWsDialog)
		})
		restore := data.DataSelectTheme(c, 0)
		e := ui.Outline(c, &a.sessTree, a.sessionTreeRoots(), a.sessionTreeChildren, func(item string) {
			a.sessionTreeRow(c, k, item)
		})
		restore()
		e.Grow(1).MinHeight(0)
		b := ui.ButtonBase(c).Label("workspace-open-row").FillWidth().Padding(8).Radius(7).Gap(8).Cursor(ui.CursorPointer)
		if b.Hovered() {
			b.Background(k.SurfaceHover)
		}
		if b.Clicked() {
			a.openWsDialog()
		}
		b.Children(func() {
			ui.Icon(c, icons.Must("folder")).FontSize(14).TextColor(k.TextMuted)
			ui.Text(c, "Open workspace…").FontSize(12).TextColor(k.TextMuted)
		})
		newBtn := ui.PrimaryButton(c, "New chat")
		if newBtn.Clicked() {
			a.newThread()
		}
	})
}

// sessionTreeRoots lists the tree's top level: the current workspace first,
// then the recents, each as a "ws:<path>" key.
func (a *app) sessionTreeRoots() []string {
	out := []string{}
	if a.ws.root != "" {
		out = append(out, "ws:"+a.ws.root)
	}
	for _, r := range a.recents {
		if r != a.ws.root {
			out = append(out, "ws:"+r)
		}
	}
	return out
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

// sessionTreeRow builds one sidebar-tree row: a workspace line (folder,
// name, session count; the current root highlighted; click toggles open)
// or a session line (icon, title; click opens it, switching workspace if
// it belongs to another root).
func (a *app) sessionTreeRow(c *ui.Context, k tokensT, item string) {
	if path, ok := strings.CutPrefix(item, "ws:"); ok {
		count := 0
		for _, s := range a.sessions {
			if s.ws == path {
				count++
			}
		}
		current := path == a.ws.root
		row := ui.Row(c).Label(item).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Tooltip(path).Cursor(ui.CursorPointer)
		if current {
			row.TextColor(k.AccentText)
		}
		row.Children(func() {
			ui.Icon(c, icons.Must("folder")).FontSize(14)
			ui.Text(c, fsutil.WorkspaceName(path)).FontSize(12).Bold().SingleLine().Grow(1).MinWidth(0)
			if count > 0 {
				ui.Text(c, strconv.Itoa(count)).FontSize(10).TextColor(k.TextMuted)
			}
		})
		if row.Clicked() {
			if a.sessTree.Open.Has(item) {
				a.sessTree.Open.Remove(item)
			} else {
				a.sessTree.Open.Add(item)
			}
		}
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
	row := ui.Row(c).Label(item).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	if id == a.sessionID {
		row.TextColor(k.AccentText)
	}
	row.Children(func() {
		ui.Icon(c, icons.Must("message-square")).FontSize(13).TextColor(k.TextMuted)
		ui.Text(c, sess.title).FontSize(12).SingleLine().Grow(1).MinWidth(0)
	})
	if row.Clicked() {
		if sess.ws != "" && filepath.Clean(sess.ws) != filepath.Clean(a.ws.root) {
			if msg := a.openWorkspace(sess.ws); msg != "" {
				c.Toast(msg)
			}
		}
		a.openSession(id)
	}
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

// statusbar is the strip along the bottom: workspace, branch, mode and
// context on the left; a live dot on the right.
func (a *app) statusbar(c *ui.Context, k tokensT) {
	layout.StatusBar(c, layout.StatusBarOptions{
		Left: []layout.StatusItem{
			{ID: "workspace", Text: fsutil.WorkspaceName(a.ws.root), Icon: icons.Must("folder")},
			{ID: "branch", Text: a.repo.branch, Icon: icons.Must("git-branch")},
			{ID: "mode", Text: modeName(a.thread.mode), Icon: icons.Must("brain")},
			{ID: "ctx", Text: "6.4k / 8k tokens", Icon: icons.Must("sliders-horizontal")},
		},
		Right: []layout.StatusItem{
			{ID: "live", Text: "live", Icon: icons.Must("circle"), Tone: layout.StatusItemSuccess},
			{ID: "files", Text: "3 changed", Icon: icons.Must("file-text"), Tone: layout.StatusItemWarning},
		},
	})
}

// iconToggle is a small icon button that reports its click. tint lights
// it when the associated state is on.
func (a *app) iconToggle(c *ui.Context, k tokensT, icon, label string, fn func()) {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Size(28, 28).Radius(7).Center().Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(k.SurfaceHover)
	}
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

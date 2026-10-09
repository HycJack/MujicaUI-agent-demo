package main

// shell.go is the Atlas desk: a title bar on top, a conversation sidebar
// on the left, the working pane in the middle, a repo inspector that
// slides in on the right, and a status bar along the bottom. It is built
// from mygo's flex primitives plus MujicaUI's chrome, the same way the
// dashboard's shell folds together.

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ZacharyZhang-NY/MujicaUI/chat"
	"github.com/ZacharyZhang-NY/MujicaUI/icons"
	"github.com/ZacharyZhang-NY/MujicaUI/layout"
	"github.com/egoist/mygo/ui"
)

// view is the whole window. Atlas installs MujicaUI's theme, lays the
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
	layout.TitleBar(c, "Atlas", layout.TitleBarOptions{
		Leading: func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icons.Must("bot")).FontSize(16).TextColor(k.Accent)
				ui.Text(c, "Atlas").FontSize(14).Bold()
				a.iconToggle(c, k, "menu", "Toggle sessions", func() { a.navOpen = !a.navOpen })
			})
		},
		Trailing: func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.modelName()).FontSize(12).TextColor(k.TextMuted)
				a.iconToggle(c, k, "settings", "Provider settings", func() { a.openProviders() })
				a.iconToggle(c, k, "bot", "Agent settings", func() { a.openAgent() })
				a.iconToggle(c, k, "git-pull-request", "Toggle repo inspector", func() { a.repo.showRepo = !a.repo.showRepo })
				ui.Avatar(c, "Ada Lovelace", nil).Tooltip("Ada Lovelace")
			})
		},
	})
}

// sidebar is the conversation index: a workspace switcher over a New-chat
// button and the list of the current workspace's sessions.
func (a *app) sidebar(c *ui.Context, k tokensT) {
	ui.Column(c).Width(260).Shrink(0).Background(k.Surface).Padding(10, 10, 10).Gap(8).Children(func() {
		// Sessions are organized per workspace, the way Codex-style agents
		// group a project's threads; the picker re-roots everything.
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icons.Must("folder")).FontSize(14).TextColor(k.Accent)
			ui.Text(c, workspaceName(a.ws.root)).FontSize(12).Bold().SingleLine().Grow(1).MinWidth(0).Tooltip(a.ws.root)
			a.iconToggle(c, k, "chevron-down", "Change workspace", a.openWsDialog)
		})
		newBtn := ui.PrimaryButton(c, "New chat")
		if newBtn.Clicked() {
			a.newThread()
		}
		ui.Text(c, "Sessions").FontSize(11).TextColor(k.TextMuted)
		items := make([]chat.ChatConversation, 0, len(a.sessions))
		for _, s := range a.sessions {
			if s.ws != a.ws.root {
				continue // each workspace keeps its own sessions
			}
			items = append(items, chat.ChatConversation{ID: s.id, Title: s.title, Updated: s.updated, Pinned: s.pinned})
		}
		conv := chat.ConversationList(c, &a.convList, items, chat.ConversationListOptions{Label: "Sessions"}, nil)
		conv.Element.Grow(1).MinHeight(0)
		if conv.Changed() {
			a.openSession(a.convList.Selected)
		}
		if conv.Submitted() {
			c.Toast("Opened " + a.convList.Selected)
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

// statusbar is the strip along the bottom: workspace, branch, mode and
// context on the left; a live dot on the right.
func (a *app) statusbar(c *ui.Context, k tokensT) {
	layout.StatusBar(c, layout.StatusBarOptions{
		Left: []layout.StatusItem{
			{ID: "workspace", Text: workspaceName(a.ws.root), Icon: icons.Must("folder")},
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

// newThread starts a fresh, empty chat under a new session id and selects
// it in the index.
func (a *app) newThread() {
	a.saveSession()
	a.nextID++
	a.sessionID = fmt.Sprintf("new-%d", a.nextID)
	a.convList.Selected = a.sessionID
	a.thread = thread{mode: chat.ModeAgent, model: a.thread.model}
	a.sessions = append([]session{{id: a.sessionID, title: "New chat " + strconv.Itoa(a.nextID), updated: time.Now(), ws: a.ws.root}}, a.sessions...)
	a.closeModals()
	a.persistSessions()
}

// openSession saves the working transcript and loads the chosen one,
// seeding it the first time it is opened. Selecting a session always brings
// the user back to the conversation, closing any settings dialog.
func (a *app) openSession(id string) {
	if id == "" || id == a.sessionID {
		return
	}
	a.closeModals()
	a.saveSession()
	a.sessionID = id
	if t, ok := a.threads[id]; ok {
		a.thread = t
		return
	}
	a.thread = seededFor(id)
	a.persistSessions()
}

// saveSession stashes the working transcript under its session id so
// switching away and back keeps what the user typed.
func (a *app) saveSession() {
	if a.sessionID != "" {
		a.threads[a.sessionID] = a.thread
	}
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

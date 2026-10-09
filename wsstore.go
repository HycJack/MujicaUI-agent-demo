package main

// wsstore.go is Atlas's workspace store: which directory the session works
// in (the user's home by default — never the binary's launch directory),
// the recent-workspaces list, and the per-workspace session transcripts.
// Everything persists under the user config directory so a restart
// restores the workspace, its session index and every conversation.

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ZacharyZhang-NY/MujicaUI/chat"
	"github.com/ZacharyZhang-NY/MujicaUI/icons"
	"github.com/ZacharyZhang-NY/MujicaUI/input"
	"github.com/ZacharyZhang-NY/MujicaUI/overlay"
	"github.com/egoist/mygo/ui"
)

// homeDir is the default workspace: the user's home directory, falling
// back to the process directory when the OS cannot name one.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// wsPrefsFile / sessionsFile name the store files next to settings.json.
func wsPrefsFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configDirName, "workspace.json"), nil
}

func sessionsFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configDirName, "sessions.json"), nil
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// wsPrefs is the persisted workspace choice.
type wsPrefs struct {
	Current string   `json:"current"`
	Recents []string `json:"recents,omitempty"`
}

// saveWsPrefs writes the current workspace and the recents list.
func (a *app) saveWsPrefs() {
	if a.wsPrefsPath == "" {
		return
	}
	buf, err := json.MarshalIndent(wsPrefs{Current: a.ws.root, Recents: a.recents}, "", "  ")
	if err != nil {
		log.Printf("atlas: workspace prefs not saved: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(a.wsPrefsPath), 0o700); err != nil {
		log.Printf("atlas: workspace prefs not saved: %v", err)
		return
	}
	if err := os.WriteFile(a.wsPrefsPath, buf, 0o600); err != nil {
		log.Printf("atlas: workspace prefs not saved: %v", err)
	}
}

// loadWsPrefs restores the persisted workspace, falling back to home when
// it is missing or no longer exists.
func (a *app) loadWsPrefs() {
	if a.wsPrefsPath == "" {
		return
	}
	buf, err := os.ReadFile(a.wsPrefsPath)
	if err != nil {
		return // first run: keep the home default
	}
	var p wsPrefs
	if err := json.Unmarshal(buf, &p); err != nil {
		log.Printf("atlas: workspace prefs ignored: %v", err)
		return
	}
	if p.Current != "" && dirExists(p.Current) {
		a.ws = newWorkspace(p.Current)
	}
	a.recents = p.Recents
	a.touchRecent(a.ws.root)
}

// touchRecent moves path to the front of the recents list (deduped, capped).
func (a *app) touchRecent(path string) {
	a.recents = slices.DeleteFunc(a.recents, func(r string) bool { return r == path })
	a.recents = append([]string{path}, a.recents...)
	if len(a.recents) > 6 {
		a.recents = a.recents[:6]
	}
}

// storedRow / storedSession are the on-disk shapes of the transcript store.
type storedRow struct {
	ID        string           `json:"id"`
	Role      chat.MessageRole `json:"role"`
	Kind      kind             `json:"kind"`
	Text      string           `json:"text"`
	LLMText   string           `json:"llmText,omitempty"`
	ThinkText string           `json:"thinkText,omitempty"`
	At        time.Time        `json:"at"`
}

type storedSession struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Updated   time.Time     `json:"updated"`
	Pinned    bool          `json:"pinned"`
	Workspace string        `json:"workspace"`
	Mode      chat.ChatMode `json:"mode,omitempty"`
	Threaded  bool          `json:"threaded"`
	Rows      []storedRow   `json:"rows,omitempty"`
}

type sessionStore struct {
	Sessions []storedSession `json:"sessions"`
}

// persistSessions writes every session — grouped by workspace, transcripts
// included — to sessions.json.
func (a *app) persistSessions() {
	if a.sessionsPath == "" {
		return
	}
	st := sessionStore{}
	for _, s := range a.sessions {
		ss := storedSession{ID: s.id, Title: s.title, Updated: s.updated, Pinned: s.pinned, Workspace: s.ws}
		if s.id == a.sessionID {
			// The live transcript may be ahead of the threads map (its rows
			// are only copied there on session switches).
			ss.Threaded, ss.Mode = true, a.thread.mode
			for _, r := range a.thread.rows {
				ss.Rows = append(ss.Rows, storedRow{
					ID: r.id, Role: r.role, Kind: r.kind, Text: r.text,
					LLMText: r.llmText, ThinkText: r.thinkText, At: r.at,
				})
			}
		} else if t, ok := a.threads[s.id]; ok {
			ss.Threaded, ss.Mode = true, t.mode
			for _, r := range t.rows {
				ss.Rows = append(ss.Rows, storedRow{
					ID: r.id, Role: r.role, Kind: r.kind, Text: r.text,
					LLMText: r.llmText, ThinkText: r.thinkText, At: r.at,
				})
			}
		}
		st.Sessions = append(st.Sessions, ss)
	}
	buf, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		log.Printf("atlas: sessions not saved: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(a.sessionsPath), 0o700); err != nil {
		log.Printf("atlas: sessions not saved: %v", err)
		return
	}
	if err := os.WriteFile(a.sessionsPath, buf, 0o600); err != nil {
		log.Printf("atlas: sessions not saved: %v", err)
	}
}

// loadSessions restores the persisted sessions and transcripts.
func (a *app) loadSessions() {
	if a.sessionsPath == "" {
		return
	}
	buf, err := os.ReadFile(a.sessionsPath)
	if err != nil {
		return // first run: keep the in-memory seed
	}
	var st sessionStore
	if err := json.Unmarshal(buf, &st); err != nil {
		log.Printf("atlas: sessions ignored: %v", err)
		return
	}
	a.sessions, a.threads = nil, map[string]thread{}
	for _, ss := range st.Sessions {
		a.sessions = append(a.sessions, session{
			id: ss.ID, title: ss.Title, updated: ss.Updated, pinned: ss.Pinned, ws: ss.Workspace,
		})
		if !ss.Threaded {
			continue // never opened: starts empty when selected
		}
		t := thread{mode: ss.Mode}
		for _, sr := range ss.Rows {
			t.rows = append(t.rows, row{
				id: sr.ID, role: sr.Role, kind: sr.Kind, text: sr.Text,
				llmText: sr.LLMText, thinkText: sr.ThinkText, at: sr.At,
			})
		}
		a.threads[ss.ID] = t
	}
}

// openWorkspace switches the session to dir: it validates the directory,
// saves the current transcript, re-roots the tree and the git pane, and
// restores (or starts) that workspace's session list. It returns the toast.
func (a *app) openWorkspace(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "Type a directory path"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "Cannot resolve " + path
	}
	if !dirExists(abs) {
		return "Not a directory: " + abs
	}
	if abs == a.ws.root {
		a.wsDialogOpen = false
		return ""
	}
	a.saveSession()
	a.persistSessions()
	a.touchRecent(a.ws.root) // the workspace being left becomes a recent
	a.ws = newWorkspace(abs)
	a.reloadWorkspace()
	a.repo.vcs = vcsState{} // the git pane re-collects for the new root
	a.touchRecent(abs)
	a.restoreSession()
	a.wsDialogOpen = false
	a.saveWsPrefs()
	a.persistSessions()
	return "Workspace: " + filepath.Base(abs)
}

// openWsDialog opens the workspace picker with the current root prefilled.
func (a *app) openWsDialog() {
	a.wsPathField = a.ws.root
	a.wsDialogOpen = true
}

// restoreSession selects the newest session of the current workspace, or
// an empty thread when the workspace has none yet.
func (a *app) restoreSession() {
	for _, s := range a.sessions {
		if s.ws != a.ws.root {
			continue
		}
		a.sessionID = s.id
		a.convList.Selected = s.id
		a.conv = chat.ChatConversation{ID: s.id, Title: s.title, Updated: s.updated, Pinned: s.pinned}
		if t, ok := a.threads[s.id]; ok {
			a.thread = t
		} else {
			a.thread = thread{mode: chat.ModeAgent} // never opened: start empty
		}
		return
	}
	a.sessionID = ""
	a.convList.Selected = ""
	a.thread = thread{mode: chat.ModeAgent}
}

// workspaceDialog is the Open-workspace modal: a directory field plus the
// recent workspaces. It runs every frame like the Settings dialog.
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
			return input.InputGroup(c, &a.wsPathField, input.InputGroupOptions{Placeholder: homeDir(), Label: "Directory"}).Input
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

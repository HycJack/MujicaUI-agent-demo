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

	"github.com/ZacharyZhang-NY/MujicaUI/agent"
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

// wsPrefsFile / sessionsFile name the store files inside the app's home
// directory folder (~/.mujicaui-agent-demo, next to settings.json).
func wsPrefsFile() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "workspace.json"), nil
}

func sessionsFile() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions.json"), nil
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
	if err := writeFileAtomic(a.wsPrefsPath, buf); err != nil {
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

// storedRow / storedTool / storedSession are the on-disk shapes of the
// transcript store. The index (sessions.json) holds only session metadata;
// each session's transcript lives in its own file under sessions/, so one
// long conversation never rewrites every other one.
type storedRow struct {
	ID        string           `json:"id"`
	Role      chat.MessageRole `json:"role"`
	Kind      kind             `json:"kind"`
	Text      string           `json:"text"`
	LLMText   string           `json:"llmText,omitempty"`
	ThinkText string           `json:"thinkText,omitempty"`
	At        time.Time        `json:"at"`
	ThinkOpen bool             `json:"thinkOpen,omitempty"`
	Tools     []*storedTool    `json:"tools,omitempty"`
}

// storedTool is one merged tool invocation of a reply row.
type storedTool struct {
	CallID   string             `json:"callId"`
	Name     string             `json:"name"`
	Args     string             `json:"args,omitempty"`
	Result   string             `json:"result,omitempty"`
	ErrMsg   string             `json:"errMsg,omitempty"`
	State    agent.AgentState   `json:"state"`
	Dur      time.Duration      `json:"dur,omitempty"`
	Run      agent.CommandRun   `json:"run,omitempty"`
	Decision agent.FileDecision `json:"decision,omitempty"`
	Change   agent.FileChange   `json:"change,omitempty"`
}

// transcriptStore is one session's transcript file.
type transcriptStore struct {
	Mode chat.ChatMode `json:"mode,omitempty"`
	Rows []storedRow   `json:"rows"`
}

type storedSession struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Updated   time.Time     `json:"updated"`
	Pinned    bool          `json:"pinned"`
	Workspace string        `json:"workspace"`
	Mode      chat.ChatMode `json:"mode,omitempty"`
	Threaded  bool          `json:"threaded"`
	// Rows only appears in the legacy single-file format; its presence
	// triggers the one-time migration into per-session files.
	LegacyRows []storedRow `json:"rows,omitempty"`
}

type sessionStore struct {
	Sessions []storedSession `json:"sessions"`
}

// sessionsDir is the per-session transcript directory next to the index.
func (a *app) sessionsDir() string {
	return filepath.Join(filepath.Dir(a.sessionsPath), "sessions")
}

// transcriptPath is one session's transcript file; ids are sanitized so a
// hostile index cannot escape the directory.
func (a *app) transcriptPath(id string) string {
	var sb strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			sb.WriteRune(r)
		default:
			sb.WriteRune('_')
		}
	}
	return filepath.Join(a.sessionsDir(), sb.String()+".json")
}

// writeFileAtomic replaces path with data: write a temp file, then rename.
// A stale target with a broken ACL (the "Access is denied" class) is removed
// once and the rename retried, so a bad file heals itself.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		if rm := os.Remove(path); rm == nil {
			if err2 := os.Rename(tmp, path); err2 == nil {
				return nil
			}
		}
		os.Remove(tmp)
		return err
	}
	return nil
}

// storeRow projects a transcript row to its on-disk shape.
func storeRow(r row) storedRow {
	sr := storedRow{
		ID: r.id, Role: r.role, Kind: r.kind, Text: r.text,
		LLMText: r.llmText, ThinkText: r.thinkText, At: r.at, ThinkOpen: r.thinkOpen,
	}
	for _, t := range r.tools {
		sr.Tools = append(sr.Tools, &storedTool{
			CallID: t.callID, Name: t.name, Args: t.args, Result: t.result,
			ErrMsg: t.errMsg, State: t.state, Dur: t.dur,
			Run: t.run, Decision: t.decision, Change: t.change,
		})
	}
	return sr
}

// loadRow restores a stored row (legacy rows keep their single tool field).
func loadRow(sr storedRow) row {
	r := row{
		id: sr.ID, role: sr.Role, kind: sr.Kind, text: sr.Text,
		llmText: sr.LLMText, thinkText: sr.ThinkText, at: sr.At, thinkOpen: sr.ThinkOpen,
	}
	for _, st := range sr.Tools {
		r.tools = append(r.tools, &toolRun{
			callID: st.CallID, name: st.Name, args: st.Args, result: st.Result,
			errMsg: st.ErrMsg, state: st.State, dur: st.Dur,
			run: st.Run, decision: st.Decision, change: st.Change,
		})
	}
	return r
}

// persistSessions writes the session index plus every transcript marked
// dirty since the last persist — one file per session.
func (a *app) persistSessions() {
	if a.sessionsPath == "" {
		return
	}
	st := sessionStore{}
	for _, s := range a.sessions {
		threaded := a.threaded[s.id]
		mode := chat.ChatMode(0)
		if s.id == a.sessionID {
			// The live transcript may be ahead of the threads map (its rows
			// are only copied there on session switches).
			threaded, mode = true, a.thread.mode
		} else if t, ok := a.threads[s.id]; ok {
			threaded, mode = true, t.mode
		}
		st.Sessions = append(st.Sessions, storedSession{
			ID: s.id, Title: s.title, Updated: s.updated, Pinned: s.pinned,
			Workspace: s.ws, Threaded: threaded, Mode: mode,
		})
	}
	buf, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		log.Printf("atlas: sessions not saved: %v", err)
		return
	}
	if err := writeFileAtomic(a.sessionsPath, buf); err != nil {
		log.Printf("atlas: sessions not saved: %v", err)
	}
	a.persistDirtyTranscripts()
}

// persistDirtyTranscripts writes each dirty session's transcript file and
// clears the dirty marks.
func (a *app) persistDirtyTranscripts() {
	for id := range a.dirty {
		t, ok := a.threads[id]
		if id == a.sessionID {
			t, ok = a.thread, true // the live transcript wins
		}
		if !ok {
			delete(a.dirty, id)
			continue
		}
		ts := transcriptStore{Mode: t.mode}
		for _, r := range t.rows {
			ts.Rows = append(ts.Rows, storeRow(r))
		}
		buf, err := json.MarshalIndent(ts, "", "  ")
		if err == nil {
			err = writeFileAtomic(a.transcriptPath(id), buf)
		}
		if err != nil {
			log.Printf("atlas: transcript %s not saved: %v", id, err)
			continue
		}
		a.threaded[id] = true
		delete(a.dirty, id)
	}
}

// markDirty flags a session's transcript for the next persist.
func (a *app) markDirty(id string) {
	if id != "" && a.dirty != nil {
		a.dirty[id] = true
	}
}

// loadSessions restores the session index; transcripts load lazily when a
// session is opened. A legacy single-file store (sessions with embedded
// rows) migrates once into the per-session layout.
func (a *app) loadSessions() {
	if a.sessionsPath == "" {
		return
	}
	buf, err := os.ReadFile(a.sessionsPath)
	if err != nil {
		return // first run: nothing stored yet
	}
	var st sessionStore
	if err := json.Unmarshal(buf, &st); err != nil {
		log.Printf("atlas: sessions ignored: %v", err)
		return
	}
	a.sessions, a.threads = nil, map[string]thread{}
	a.threaded, a.dirty = map[string]bool{}, map[string]bool{}
	legacy := false
	for _, ss := range st.Sessions {
		a.sessions = append(a.sessions, session{
			id: ss.ID, title: ss.Title, updated: ss.Updated, pinned: ss.Pinned, ws: ss.Workspace,
		})
		a.threaded[ss.ID] = ss.Threaded
		if len(ss.LegacyRows) > 0 {
			legacy = true
			t := thread{mode: ss.Mode}
			for _, sr := range ss.LegacyRows {
				t.rows = append(t.rows, loadRow(sr))
			}
			a.threads[ss.ID] = t
			a.dirty[ss.ID] = true // re-homed into its own file on next persist
		}
	}
	if legacy {
		a.persistSessions()
		if err := writeFileAtomic(a.sessionsPath+".bak", buf); err == nil {
			os.Remove(a.sessionsPath + ".tmp")
		}
		log.Printf("atlas: migrated the single-file session store into %s", a.sessionsDir())
	}
}

// loadTranscript reads one session's transcript file into the threads cache.
func (a *app) loadTranscript(id string) {
	if a.sessionsPath == "" || id == "" {
		return
	}
	if _, ok := a.threads[id]; ok {
		return
	}
	buf, err := os.ReadFile(a.transcriptPath(id))
	if err != nil {
		return // never opened, or lost: starts empty
	}
	var ts transcriptStore
	if err := json.Unmarshal(buf, &ts); err != nil {
		log.Printf("atlas: transcript %s ignored: %v", id, err)
		return
	}
	t := thread{mode: ts.Mode}
	for _, sr := range ts.Rows {
		t.rows = append(t.rows, loadRow(sr))
	}
	a.threads[id] = t
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
// an empty thread when the workspace has none yet. The current workspace's
// tree node starts open so its sessions are visible.
func (a *app) restoreSession() {
	if a.ws.root != "" {
		a.sessTree.Open.Add("ws:" + a.ws.root)
	}
	for _, s := range a.sessions {
		if s.ws != a.ws.root {
			continue
		}
		a.sessionID = s.id
		a.conv = chat.ChatConversation{ID: s.id, Title: s.title, Updated: s.updated, Pinned: s.pinned}
		a.loadTranscript(s.id) // transcripts load lazily from their own file
		if t, ok := a.threads[s.id]; ok {
			a.thread = t
		} else {
			a.thread = thread{mode: chat.ModeAgent} // never opened: start empty
		}
		return
	}
	a.sessionID = ""
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

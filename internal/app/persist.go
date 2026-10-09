package app

// persist.go is the UI layer's persistence glue: it moves transcript rows
// in and out of the store's data-layer schemas, keeps the session index and
// per-session transcripts in sync (dirty-marked, one file per session), and
// owns the session/workspace lifecycle. All rendering lives elsewhere.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"crux-agent/internal/store"
	"github.com/ZacharyZhang-NY/MujicaUI/chat"
)

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// saveWsPrefs writes the current workspace and the recents list.
func (a *app) saveWsPrefs() {
	if a.wsPrefsPath == "" {
		return
	}
	if err := store.SaveWsPrefs(a.wsPrefsPath, store.WsPrefs{Current: a.ws.root, Recents: a.recents}); err != nil {
		log.Printf("crux: workspace prefs not saved: %v", err)
	}
}

// loadWsPrefs restores the persisted workspace, falling back to home when
// it is missing or no longer exists.
func (a *app) loadWsPrefs() {
	if a.wsPrefsPath == "" {
		return
	}
	p, err := store.LoadWsPrefs(a.wsPrefsPath)
	if err != nil {
		return // first run: keep the home default
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

// --- row <-> store.Message conversion -------------------------------------

// storeRole / uiRole map between the UI's role enum and the store's string.
func storeRole(r chat.MessageRole) store.Role {
	if r == chat.MessageAssistant {
		return store.RoleAssistant
	}
	return store.RoleUser
}

func uiRole(s store.Role) chat.MessageRole {
	if s == store.RoleAssistant {
		return chat.MessageAssistant
	}
	return chat.MessageUser
}

// storeKind / uiKind map the row kinds.
func storeKind(k kind) store.Kind {
	switch k {
	case rowReasoned:
		return store.KindReasoned
	case rowTools:
		return store.KindTools
	case rowTyping:
		return store.KindTyping
	case rowCommand:
		return store.KindCommand
	case rowDiff:
		return store.KindDiff
	default:
		return store.KindPlain
	}
}

func uiKind(s store.Kind) kind {
	switch s {
	case store.KindReasoned:
		return rowReasoned
	case store.KindTools:
		return rowTools
	case store.KindTyping:
		return rowTyping
	case store.KindCommand:
		return rowCommand
	case store.KindDiff:
		return rowDiff
	default:
		return rowPlain
	}
}

// storeMode / uiMode map the conversation mode.
func storeMode(m chat.ChatMode) store.Mode {
	if m == chat.ModeChat {
		return store.ModeChat
	}
	return store.ModeAgent
}

func uiMode(s store.Mode) chat.ChatMode {
	if s == store.ModeChat {
		return chat.ModeChat
	}
	return chat.ModeAgent
}

// storeTool flattens a tool run for the store (the review decision is UI
// state and does not persist).
func storeTool(t *toolRun) store.ToolCall {
	return store.ToolCall{
		CallID: t.callID, Name: t.name, Args: t.args, Result: t.result,
		ErrMsg: t.errMsg, State: int(t.state), Dur: t.dur,
		Command: t.run.Command, Dir: t.run.Dir, Output: t.run.Output,
		Running: t.run.Running, ExitCode: t.run.ExitCode,
		ChangePath: t.change.Path, ChangeOld: t.change.Old, ChangeNew: t.change.New,
	}
}

func loadTool(t *store.ToolCall) *toolRun {
	return &toolRun{
		callID: t.CallID, name: t.Name, args: t.Args, result: t.Result,
		errMsg: t.ErrMsg, state: agentStateOf(t.State), dur: t.Dur,
		run: agentRunOf(t), change: agentChangeOf(t),
	}
}

// storeMsg projects a transcript row to its on-disk shape.
func storeMsg(r row) store.Message {
	m := store.Message{
		ID: r.id, Role: storeRole(r.role), Kind: storeKind(r.kind), Text: r.text,
		LLMText: r.llmText, ThinkText: r.thinkText, At: r.at, ThinkOpen: r.thinkOpen,
	}
	for _, t := range r.tools {
		m.Tools = append(m.Tools, storeTool(t))
	}
	if r.tool != nil {
		t := storeTool(r.tool)
		m.Tool = &t
	}
	return m
}

// loadMsg restores a stored message (legacy rows keep their single tool).
func loadMsg(m store.Message) row {
	r := row{
		id: m.ID, role: uiRole(m.Role), kind: uiKind(m.Kind), text: m.Text,
		llmText: m.LLMText, thinkText: m.ThinkText, at: m.At, thinkOpen: m.ThinkOpen,
	}
	for i := range m.Tools {
		r.tools = append(r.tools, loadTool(&m.Tools[i]))
	}
	if m.Tool != nil {
		r.tool = loadTool(m.Tool)
	}
	return r
}

// --- session lifecycle ------------------------------------------------------

// newThread starts a fresh, empty chat under a new session id and selects
// it in the index; the current workspace's tree node stays open so the new
// session shows up under it.
func (a *app) newThread() {
	a.saveSession()
	a.nextID++
	a.sessionID = fmt.Sprintf("new-%d", a.nextID)
	a.thread = thread{mode: chat.ModeAgent, model: a.thread.model}
	a.sessions = append([]session{{id: a.sessionID, title: "New chat " + strconv.Itoa(a.nextID), updated: time.Now(), ws: a.ws.root}}, a.sessions...)
	if a.ws.root != "" {
		a.sessTree.Open.Add("ws:" + a.ws.root)
	}
	a.closeModals()
	a.persistSessions()
}

// openSession saves the working transcript and loads the chosen one.
// Selecting a session always brings the user back to the conversation,
// closing any settings dialog.
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
	a.loadTranscript(id) // transcripts load lazily from their own file
	if t, ok := a.threads[id]; ok {
		a.thread = t
		return
	}
	a.thread = thread{mode: chat.ModeAgent} // never opened: start empty
	a.persistSessions()
}

// saveSession stashes the working transcript under its session id so
// switching away and back keeps what the user typed, and flags the
// transcript file for the next persist.
func (a *app) saveSession() {
	if a.sessionID != "" {
		a.threads[a.sessionID] = a.thread
		a.markDirty(a.sessionID)
	}
}

// --- settings persistence -----------------------------------------------------

// loadSettings restores the persisted LLM config into the app's settings;
// a missing or corrupt file keeps the defaults.
func (a *app) loadSettings() {
	if a.configPath == "" {
		return
	}
	cfg, ok, err := store.LoadSettings(a.configPath)
	if err != nil {
		log.Printf("crux: settings ignored: %v", err)
	}
	if ok {
		a.llm.LLMConfig = cfg
	}
	a.snapshotSettings()
}

// saveSettings persists the LLM config and refreshes the snapshot the
// live-save dirty check compares against.
func (a *app) saveSettings() {
	if a.configPath == "" {
		return
	}
	if err := store.SaveSettings(a.configPath, a.llm.LLMConfig); err != nil {
		log.Printf("crux: settings not saved: %v", err)
		return
	}
	a.snapshotSettings()
}

// snapshotSettings records the persisted settings JSON so live-save can
// tell a dirty dialog from a clean one.
func (a *app) snapshotSettings() {
	if buf, err := json.Marshal(a.llm); err == nil {
		a.savedSettings = string(buf)
	}
}

// --- persistence -------------------------------------------------------------

// persistSessions writes the session index plus every transcript marked
// dirty since the last persist — one file per session.
func (a *app) persistSessions() {
	if a.sessionsPath == "" {
		return
	}
	idx := store.SessionIndex{}
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
		idx.Sessions = append(idx.Sessions, store.SessionMeta{
			ID: s.id, Title: s.title, Updated: s.updated, Pinned: s.pinned,
			Workspace: s.ws, Threaded: threaded, Mode: storeMode(mode),
		})
	}
	if err := store.SaveIndex(a.sessionsPath, idx); err != nil {
		log.Printf("crux: sessions not saved: %v", err)
	}
	a.persistDirtyTranscripts()
}

// persistDirtyTranscripts writes each dirty session's transcript file and
// clears the dirty marks.
func (a *app) persistDirtyTranscripts() {
	dir := filepath.Dir(a.sessionsPath)
	for id := range a.dirty {
		t, ok := a.threads[id]
		if id == a.sessionID {
			t, ok = a.thread, true // the live transcript wins
		}
		if !ok {
			delete(a.dirty, id)
			continue
		}
		ts := store.Transcript{Mode: storeMode(t.mode)}
		for _, r := range t.rows {
			ts.Rows = append(ts.Rows, storeMsg(r))
		}
		if err := store.SaveTranscript(dir, id, ts); err != nil {
			log.Printf("crux: transcript %s not saved: %v", id, err)
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
	idx, err := store.LoadIndex(a.sessionsPath)
	if err != nil {
		return // first run: nothing stored yet
	}
	a.sessions, a.threads = nil, map[string]thread{}
	a.threaded, a.dirty = map[string]bool{}, map[string]bool{}
	if store.MigrateLegacyIndex(a.sessionsPath, filepath.Dir(a.sessionsPath), idx) {
		log.Printf("crux: migrated the single-file session store")
		if idx, err = store.LoadIndex(a.sessionsPath); err != nil {
			return
		}
	}
	for _, ss := range idx.Sessions {
		a.sessions = append(a.sessions, session{
			id: ss.ID, title: ss.Title, updated: ss.Updated, pinned: ss.Pinned, ws: ss.Workspace,
		})
		a.threaded[ss.ID] = ss.Threaded
		if len(ss.LegacyRows) > 0 {
			// The migration could not finish (read-only disk?): keep the
			// rows live and re-home them on the next persist.
			t := thread{mode: uiMode(ss.Mode)}
			for _, m := range ss.LegacyRows {
				t.rows = append(t.rows, loadMsg(m))
			}
			a.threads[ss.ID] = t
			a.dirty[ss.ID] = true
		}
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
	ts, err := store.LoadTranscript(filepath.Dir(a.sessionsPath), id)
	if err != nil {
		return // never opened, or lost: starts empty
	}
	t := thread{mode: uiMode(ts.Mode)}
	for _, m := range ts.Rows {
		t.rows = append(t.rows, loadMsg(m))
	}
	a.threads[id] = t
}

// --- workspace switching ------------------------------------------------------

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

// jsonIndent is only used by tests that shape store files by hand.
func jsonIndent(v any) []byte {
	buf, _ := json.MarshalIndent(v, "", "  ")
	return buf
}

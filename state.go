package main

// state.go holds the Atlas app's data: the thread transcript, the repo
// under the cursor, and the sessions list. Everything the UI shows is a
// function of this state, the way a Command.app holds its session.

import (
	"context"
	"time"

	piai "github.com/HycJack/pi-ai-go"
	"github.com/ZacharyZhang-NY/MujicaUI/agent"
	"github.com/ZacharyZhang-NY/MujicaUI/chat"
	"github.com/ZacharyZhang-NY/MujicaUI/code"
	"github.com/ZacharyZhang-NY/MujicaUI/data"
	"github.com/ZacharyZhang-NY/MujicaUI/git"
	"github.com/egoist/mygo/ui"
)

// kind selects how one transcript row renders.
type kind int

const (
	rowPlain    kind = iota
	rowReasoned      // thinking block + streaming text
	rowTools         // tool call + file-change cards
	rowTyping        // a "thinking…" row while a reply streams
	rowCommand       // a terminal run the agent executed
	rowDiff          // a reviewable multi-file diff
)

// row is one entry of the thread. One AI reply is ONE row: its thinking,
// tool calls and text all live here — thinking and tools fold into a
// collapsible ThinkingBlock, the text renders as markdown once done.
type row struct {
	id        string
	role      chat.MessageRole
	kind      kind
	text      string
	llmText   string // what the LLM sees when it differs from text (attached file contents)
	thinkText string // reasoning text collected while a reply streams
	at        time.Time
	tool      *toolRun   // legacy single-tool rows (transcripts stored before the merge)
	tools     []*toolRun // this reply's tool calls, in execution order
	thinkOpen bool       // the reply's ThinkingBlock collapse state
	streaming bool       // the reply is still streaming (text renders plain)
}

// toolRun carries one agent tool invocation for the transcript cards: the
// tool-call card shows args/result, the command card the terminal run, and
// the file-change card the before/after of a write.
type toolRun struct {
	callID string
	name   string // bash / read_file / write_file
	args   string // raw JSON arguments
	result string // raw JSON result (tool-call card)
	errMsg string // replaces result when the call failed
	state  agent.AgentState
	dur    time.Duration

	run      agent.CommandRun   // bash card
	decision agent.FileDecision // write_file card review state
	change   agent.FileChange   // write_file card data
}

// thread is the working conversation Atlas renders in the center pane.
type thread struct {
	rows   []row
	list   chat.MessageListState
	draft  string
	mode   chat.ChatMode
	model  string
	rating chat.MessageFeedback
	think  bool
	ctx    []chat.ContextItem
}

// session is one item of the sessions list; ws is the workspace
// (absolute directory) the session belongs to.
type session struct {
	id      string
	title   string
	updated time.Time
	pinned  bool
	ws      string
}

// fmtView is one code pane's formatted-view toggle (gofmt / pretty JSON):
// the Segmented binding plus a cache so the formatter runs once per
// content, not once per frame.
type fmtView struct {
	tab   int      // 0 = raw, 1 = formatted
	key   string   // lang + NUL + source text the cache was built from
	lines []string // formatted lines when key matches
}

// render returns the lines to display: raw when toggled off (or the source
// does not parse), the cached formatted copy otherwise.
func (f *fmtView) render(lang, text string) []string {
	if f.tab != 1 {
		return splitLines(text)
	}
	key := lang + "\x00" + text
	if f.key != key {
		if out, ok := formatSource(lang, text); ok {
			f.key, f.lines = key, splitLines(out)
		} else {
			f.key, f.lines = key, splitLines(text)
		}
	}
	return f.lines
}

// repo is the right-hand inspector's state: the real git backend (vcs),
// the workspace file preview the tree drives, and the component states.
type repo struct {
	changes  git.ChangesListState
	branches data.ListState[string]
	commits  data.ListState[string]
	diff     git.DiffViewerState
	dst      code.CodeViewerState
	msg      git.CommitMessage // commit input's title/body
	vcs      vcsState          // real git state, collected off the UI thread
	branch   string
	showRepo bool
	codeTab  int // inspector bottom pane: 0 working diff, 1 file source
	paneTab  int // right pane: 0 workspace tree, 1 repository

	// formatted-view toggle (repository source tab)
	srcFmt fmtView
}

// fileDrawer is the large right-hand code viewer: the workspace tree opens
// file contents in it, and the repository pane maximizes its diff or source
// into it. It replaces the old small inline preview box.
type fileDrawer struct {
	open      bool
	title     string // display title: workspace-relative path or file name
	lang      string
	text      string
	err       string
	loading   bool
	truncated bool
	fmt       fmtView // Raw/Fmt for file contents
	src       code.CodeViewerState

	diffMode bool   // render from/to as a DiffViewer instead of source
	from, to string // diff sides in diffMode
	ddiff    git.DiffViewerState
}

// LLMSettings carries the pi-ai-go backend configuration the provider and
// agent dialogs edit. Values are held in app state, passed to the LLM at
// call time, and persisted to the user's config directory (config.go);
// the json tags keep the transient call-state fields out of the file.
type LLMSettings struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	APIKey        string `json:"apiKey,omitempty"`
	BaseURL       string `json:"baseUrl,omitempty"`
	SystemPrompt  string `json:"systemPrompt"`
	Thinking      string `json:"thinking"`
	Busy          bool   `json:"-"`
	LastError     string `json:"-"`
	ProviderOK    bool   `json:"-"`
	ConnectionErr string `json:"-"`
}

// settingsPaneOpen toggles the right-hand config inspector.
func (a *app) defaultSettings() LLMSettings {
	s := LLMSettings{
		Provider:     "openai",
		Model:        "gpt-4o",
		SystemPrompt: "You are Atlas, a Codex-style coding agent built into a native desktop app. Answer in the user's language, prefer concise and concrete replies, and refer to the repo when relevant.",
		Thinking:     "none",
	}
	if m, err := piai.GetModel(piai.KnownProvider(s.Provider), s.Model); err == nil {
		s.Thinking = thinkDefault(m)
	}
	return s
}

// app is the whole Atlas window state, the state object main.go renders.
type app struct {
	thread    thread
	repo      repo
	ws        workspace
	fdraw     fileDrawer
	conv      chat.ChatConversation
	sessTree  ui.OutlineState[string] // sidebar tree: workspaces → their sessions
	sessionID string
	sessions  []session
	threads   map[string]thread // in-flight transcripts, keyed by session
	threaded  map[string]bool   // sessions that have a transcript on disk
	dirty     map[string]bool   // transcripts changed since their last persist

	// llm
	llm           LLMSettings
	configPath    string             // settings.json path; empty disables persistence
	sessionsPath  string             // sessions.json path; empty disables persistence
	wsPrefsPath   string             // workspace.json path; empty disables persistence
	settingsOpen  bool               // the merged Settings dialog is open
	settingsTab   string             // which pane: "providers" or "agent"
	redraw        func(func())       // runs a closure on the UI thread + repaints (win.Update); nil in tests
	cancelFn      context.CancelFunc // cancels the current streaming reply
	provView      providerView       // providers config pane state
	savedSettings string             // last persisted settings JSON (live-save dirty check)

	// workspace
	recents      []string // recently opened workspaces, newest first
	wsDialogOpen bool     // the Open-workspace dialog
	wsPathField  string   // the dialog's directory input
	wsCursor     int      // the file tree's selected row (-1 = none)

	// shell
	navOpen     bool
	paletteOpen bool
	nextID      int
}

func newApp() *app {
	// No seeded sessions: a fresh install starts with an empty session
	// list and the welcome screen, like a real agent console.
	a := &app{
		navOpen:     true,
		repo:        repo{branch: "main"},
		settingsTab: "providers",
		wsCursor:    -1,
	}
	a.llm = a.defaultSettings()
	if p, err := settingsFile(); err == nil {
		a.configPath = p
	}
	// The workspace defaults to the user's home directory — never the
	// directory the binary was launched from. main.go replaces it with the
	// persisted choice and wires the store paths (tests keep no paths, so
	// they never touch disk).
	a.ws = newWorkspace(homeDir())
	a.threads = map[string]thread{}
	a.threaded = map[string]bool{}
	a.dirty = map[string]bool{}
	a.provView = defaultProviderView()
	a.thread = thread{mode: chat.ModeAgent}
	return a
}

// closeModals closes the Settings and workspace dialogs.
func (a *app) closeModals() { a.settingsOpen, a.wsDialogOpen = false, false }

// openProviders opens the Settings dialog on the Providers pane.
func (a *app) openProviders() { a.settingsTab, a.settingsOpen = "providers", true }

// openAgent opens the Settings dialog on the Agent pane.
func (a *app) openAgent() { a.settingsTab, a.settingsOpen = "agent", true }

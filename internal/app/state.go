package app

// state.go holds the Crux app's data: the thread transcript, the repo
// under the cursor, and the sessions list. Everything the UI shows is a
// function of this state, the way a Command.app holds its session.

import (
	"context"
	"strings"
	"time"

	"crux-agent/internal/engine"
	"crux-agent/internal/fsutil"
	"crux-agent/internal/store"
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
// tool calls and text all live here — the process area lays the thinking
// and the tool calls out as a timeline (see thread.go processBlock), the
// text renders as markdown once done.
type row struct {
	id        string
	role      chat.MessageRole
	kind      kind
	text      string
	llmText   string // what the LLM sees when it differs from text (attached file contents)
	thinkText string // all reasoning segments joined (persisted shape)
	at        time.Time
	doneAt    time.Time  // when streaming ended; the completed process header's duration
	tool      *toolRun   // legacy single-tool rows (transcripts stored before the merge)
	tools     []*toolRun // this reply's tool calls, in execution order
	steps     []step     // the process timeline: reasoning segments and tool calls in order
	procOpen  bool       // process area (group header) state: open while streaming, folds on completion
	streaming bool       // the reply is still streaming (text renders plain)
}

// step is one entry of a reply's process timeline: a stretch of reasoning,
// an interim message the model wrote after seeing tool results, or one tool
// invocation, in the order it happened.
type step struct {
	think string   // the reasoning segment's full text (other steps leave it empty)
	text  string   // an interim message segment (other steps leave it empty)
	tool  *toolRun // nil for reasoning and text steps
	open  bool     // this step's fold state (not persisted; text steps ignore it)
}

// setThink lands one reasoning update on the timeline: the segment grows in
// place while the engine keeps extending it (same turn), and starts a new
// step once the engine reset it after tool calls — the interleaving DSH
// renders as reasoning parts between tool calls. thinkText keeps the joined
// segments for persistence.
func (r *row) setThink(text string) {
	if text == "" {
		return
	}
	if n := len(r.steps); n > 0 && r.steps[n-1].tool == nil && r.steps[n-1].text == "" &&
		strings.HasPrefix(text, r.steps[n-1].think) {
		r.steps[n-1].think = text
	} else {
		r.steps = append(r.steps, step{think: text})
	}
	var segs []string
	for _, s := range r.steps {
		if s.think != "" {
			segs = append(segs, s.think)
		}
	}
	r.thinkText = strings.Join(segs, "\n\n")
}

// setText lands one message update on the timeline, mirroring setThink: the
// segment grows in place within a turn, and the engine's per-turn reset
// starts a new segment — an interim reply after tool results. r.text keeps
// every segment joined with a blank line (the persistence and history shape).
func (r *row) setText(text string) {
	if text == "" {
		return
	}
	if n := len(r.steps); n > 0 && r.steps[n-1].tool == nil && r.steps[n-1].think == "" &&
		strings.HasPrefix(text, r.steps[n-1].text) {
		r.steps[n-1].text = text
	} else {
		r.steps = append(r.steps, step{text: text})
	}
	var segs []string
	for _, s := range r.steps {
		if s.text != "" {
			segs = append(segs, s.text)
		}
	}
	r.text = strings.Join(segs, "\n\n")
}

// answerText is the reply's final answer: the LAST message segment (earlier
// ones are interim replies rendered in the timeline), or the whole text for
// rows without segments (loaded transcripts, error rows, hand-built tests).
// It reports the segment's index in steps, -1 when it fell back to r.text.
func (r *row) answerText() (string, int) {
	for i := len(r.steps) - 1; i >= 0; i-- {
		if r.steps[i].text != "" {
			return r.steps[i].text, i
		}
	}
	return r.text, -1
}

// timeline returns the reply's process steps, materializing the legacy
// shape (one thinking blob, then the tools) once for rows stored before
// steps existed or built directly by tests.
func (r *row) timeline() []step {
	if r.steps == nil {
		if r.thinkText != "" {
			r.steps = append(r.steps, step{think: r.thinkText})
		}
		for _, t := range r.toolsOf() {
			r.steps = append(r.steps, step{tool: t})
		}
	}
	return r.steps
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

// agentStateOf / agentRunOf / agentChangeOf rebuild the UI card state from
// the store's flattened tool data (the review decision resets on load).
func agentStateOf(state int) agent.AgentState { return agent.AgentState(state) }

func agentRunOf(t *store.ToolCall) agent.CommandRun {
	return agent.CommandRun{
		Command: t.Command, Dir: t.Dir, Output: t.Output,
		Running: t.Running, ExitCode: t.ExitCode, Duration: t.Dur,
	}
}

func agentChangeOf(t *store.ToolCall) agent.FileChange {
	return agent.FileChange{Path: t.ChangePath, Old: t.ChangeOld, New: t.ChangeNew}
}

// thread is the working conversation Crux renders in the center pane.
type thread struct {
	rows  []row
	list  chat.MessageListState
	draft string
	mode  chat.ChatMode
	model string
	think bool
	ctx   []chat.ContextItem
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
		if out, ok := fsutil.FormatSource(lang, text); ok {
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
// LLMSettings is what the settings dialogs edit: the persisted fields come
// from the store's LLMConfig (data layer); the transient call-state fields
// live only in the UI layer and never reach the file.
type LLMSettings struct {
	store.LLMConfig
	Busy          bool   `json:"-"`
	LastError     string `json:"-"`
	ProviderOK    bool   `json:"-"`
	ConnectionErr string `json:"-"`
}

// defaultSystemPrompt is Crux's out-of-the-box prompt, ported from the
// DeepSeek Harness assembled system prompt: the rules that map to Crux's
// three tools, with the identity swapped. Lines naming tools Crux does not
// have were dropped rather than kept — they invite calls to nothing.
const defaultSystemPrompt = `You are an AI agent powered by Crux.

You are a coding agent.

Check the exit code on every bash result; investigate failures before moving on.

Use the read_file tool — not shell commands like cat — to inspect text files. Read an existing file before overwriting it with write_file, which always takes the complete final content: gather the current content with read_file first instead of guessing it.

Prefer showing the primary results within your final response alongside a brief explanation, and format code in fenced code blocks. Answer in the user's language — concise, concrete, and refer to the repo when relevant.`

// legacySystemPrompt is the pre-DSH default. loadSettings upgrades a stored
// copy of it to defaultSystemPrompt; a customized prompt passes through.
const legacySystemPrompt = "You are Crux, a Codex-style coding agent built into a native desktop app. Answer in the user's language, prefer concise and concrete replies, and refer to the repo when relevant."

// settingsPaneOpen toggles the right-hand config inspector.
func (a *app) defaultSettings() LLMSettings {
	s := LLMSettings{
		LLMConfig: store.LLMConfig{
			Provider:     "openai",
			Model:        "gpt-4o",
			SystemPrompt: defaultSystemPrompt,
			Thinking:     "none",
		},
	}
	s.Thinking = engine.ThinkDefault(s.Provider, s.Model)
	return s
}

// app is the whole Crux window state, the state object main.go renders.
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
	regenSeq  int               // nonce for regenerated reply row ids

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
	if p, err := store.SettingsPath(); err == nil {
		a.configPath = p
	}
	// The workspace defaults to the user's home directory — never the
	// directory the binary was launched from. main.go replaces it with the
	// persisted choice and wires the store paths (tests keep no paths, so
	// they never touch disk).
	a.ws = newWorkspace(store.HomeDir())
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

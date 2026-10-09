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

// row is one entry of the thread.
type row struct {
	id        string
	role      chat.MessageRole
	kind      kind
	text      string
	llmText   string // what the LLM sees when it differs from text (attached file contents)
	thinkText string // reasoning text collected while a reply streams
	at        time.Time
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
	plan   agent.FileDecision
	do     agent.FileDecision
	cmd    agent.CommandRun
	diff   []agent.FileDecision
	run    []agent.CommandRun
}

// seeded builds the demo conversation two days of history shows off the
// date separators; every row already satisfies MujicaUI's contracts.
func seeded() thread {
	now := time.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	ctx := []chat.ContextItem{
		{ID: "go.mod", Label: "go.mod", Kind: chat.ContextFile},
		{ID: "run", Label: "Run #4812", Detail: "CI", Kind: chat.ContextDoc},
	}
	return thread{
		mode:  chat.ModeAgent,
		model: "atlas",
		ctx:   ctx,
		rows: []row{
			{id: "u1", role: chat.MessageUser, text: "The nightly export failed again. Find out why and fix the job.", at: ago(2 * 24 * time.Hour)},
			{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: ago(2*24*time.Hour - 40*time.Second),
				text: "The job died on a **quota error** at 02:14 — the archive bucket holds seven days of dumps. I raised the cap and re-ran it."},
			{id: "a2", role: chat.MessageAssistant, kind: rowTools, at: ago(2*24*time.Hour - 90*time.Second)},
			{id: "a3", role: chat.MessageAssistant, at: ago(2*24*time.Hour - 2*time.Minute),
				text: "Fixed and verified: the job now prunes yesterday's dump first, and last night's run finished **green** in 4m12s."},
			{id: "u2", role: chat.MessageUser, text: "Nice. Watch it tonight and page me if it slips.", at: ago(26 * time.Hour)},
			{id: "a4", role: chat.MessageAssistant, text: "Watching; I'll post the moment tonight's run passes ten minutes.", at: ago(25*time.Hour + 30*time.Second)},
		},
	}
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

// seededFor returns the transcript a session opens with; unknown ids
// (a fresh New-chat session) get the full demo thread.
func seededFor(id string) thread {
	now := time.Now()
	switch id {
	case "c4":
		return thread{mode: chat.ModeAgent, model: "atlas", rows: []row{
			{id: "c4-u1", role: chat.MessageUser, text: "What's our archive retention policy?", at: now.Add(-26 * time.Hour)},
			{id: "c4-a1", role: chat.MessageAssistant, at: now.Add(-26*time.Hour + 30*time.Second),
				text: "The bucket keeps **seven days** of dumps; the prune job runs nightly at 02:00 and pages on-call if a run passes ten minutes."},
		}}
	case "c1":
		return thread{mode: chat.ModeChat, model: "swift", rows: []row{
			{id: "c1-u1", role: chat.MessageUser, text: "Build me a seating chart macro for the gala.", at: now.Add(-40 * 24 * time.Hour)},
			{id: "c1-a1", role: chat.MessageAssistant, at: now.Add(-40*24*time.Hour + time.Minute),
				text: "Done — a `seat()` macro that fills by table, then the balcony, skipping the reserved rows."},
		}}
	default:
		return seeded()
	}
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

	// workspace file preview (the tree's selected file)
	psrc             code.CodeViewerState
	previewPath      string
	previewLang      string
	previewText      string
	previewErr       string
	previewLoading   bool
	previewTruncated bool
}

// LLMSettings carries the pi-ai-go backend configuration the provider and
// agent dialogs edit. Values are held in app state, passed to the LLM at
// call time, and persisted to the user's config directory (config.go);
// the json tags keep the transient call-state fields out of the file.
type LLMSettings struct {
	Provider      string  `json:"provider"`
	Model         string  `json:"model"`
	APIKey        string  `json:"apiKey,omitempty"`
	BaseURL       string  `json:"baseUrl,omitempty"`
	SystemPrompt  string  `json:"systemPrompt"`
	Thinking      string  `json:"thinking"`
	Temperature   float64 `json:"temperature"`
	MaxTokens     int     `json:"maxTokens"`
	StreamOutput  bool    `json:"streamOutput"`
	Busy          bool    `json:"-"`
	LastError     string  `json:"-"`
	ProviderOK    bool    `json:"-"`
	ConnectionErr string  `json:"-"`
}

// settingsPaneOpen toggles the right-hand config inspector.
func (a *app) defaultSettings() LLMSettings {
	s := LLMSettings{
		Provider:     "openai",
		Model:        "gpt-4o",
		SystemPrompt: "You are Atlas, a Codex-style coding agent built into a native desktop app. Answer in the user's language, prefer concise and concrete replies, and refer to the repo when relevant.",
		Thinking:     "none",
		Temperature:  0.7,
		MaxTokens:    2048,
		StreamOutput: true,
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
	conv      chat.ChatConversation
	convList  chat.ConversationListState
	sessionID string
	sessions  []session
	threads   map[string]thread // in-flight transcripts, keyed by session

	// llm
	llm          LLMSettings
	configPath   string             // settings.json path; empty disables persistence
	sessionsPath string             // sessions.json path; empty disables persistence
	wsPrefsPath  string             // workspace.json path; empty disables persistence
	settingsOpen bool               // the merged Settings dialog is open
	settingsTab  string             // which pane: "providers" or "agent"
	redraw       func(func())       // runs a closure on the UI thread + repaints (win.Update); nil in tests
	cancelFn     context.CancelFunc // cancels the current streaming reply
	provView     providerView       // providers config pane state
	agentView    agentView          // agent config pane state
	maxTokField  string             // mirrors llm.MaxTokens for the Agent form

	// workspace
	recents      []string // recently opened workspaces, newest first
	wsDialogOpen bool     // the Open-workspace dialog
	wsPathField  string   // the dialog's directory input

	// shell
	navOpen     bool
	paletteOpen bool
	nextID      int
}

func newApp() *app {
	a := &app{
		sessionID:   "c9",
		navOpen:     true,
		repo:        repo{branch: "main"},
		conv:        chat.ChatConversation{ID: "c9", Title: "Nightly export postmortem", Updated: time.Now().Add(-2 * time.Hour)},
		settingsTab: "providers",
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
	a.sessions = seedSessions(a.ws.root)
	a.threads = map[string]thread{}
	a.provView = defaultProviderView()
	a.agentView = defaultAgentView()
	a.thread = seededFor("c9")
	return a
}

// closeModals closes the Settings and workspace dialogs.
func (a *app) closeModals() { a.settingsOpen, a.wsDialogOpen = false, false }

// openProviders opens the Settings dialog on the Providers pane.
func (a *app) openProviders() { a.settingsTab, a.settingsOpen = "providers", true }

// openAgent opens the Settings dialog on the Agent pane.
func (a *app) openAgent() { a.settingsTab, a.settingsOpen = "agent", true }

// seedSessions returns the first-run demo index (newest first), bound to
// the given workspace.
func seedSessions(ws string) []session {
	now := time.Now()
	return []session{
		{id: "c9", title: "Nightly export postmortem", updated: now.Add(-2 * time.Hour), ws: ws},
		{id: "c4", title: "Archive retention policy", updated: now.Add(-26 * time.Hour), ws: ws},
		{id: "c1", title: "Seating chart macro", updated: now.Add(-40 * 24 * time.Hour), pinned: true, ws: ws},
	}
}

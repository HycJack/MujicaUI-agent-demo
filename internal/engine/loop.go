// Package engine is Crux's logic layer: the agent loop integration and the
// toolset the model can call. It bridges the pi-ai-go library to plain
// callbacks so the UI layer never touches streaming internals — it only
// reacts to thinking/text deltas and tool events. No UI imports here.
package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	piai "github.com/HycJack/pi-ai-go"
	"github.com/HycJack/pi-ai-go/agent"
	"github.com/HycJack/pi-ai-go/core"
	_ "github.com/HycJack/pi-ai-go/providers" // registers the built-in providers at init
)

// ProviderOpenAICompat is the id of the user-supplied OpenAI-compatible
// provider (model id and endpoint are free-form).
const ProviderOpenAICompat = "openai-compatible"

// Config carries everything the loop needs from the settings: which backend
// to call, how to steer it, and where the tools operate. The Backend field
// selects the loop kind (spec §4.4): "" or "builtin" drive the pi-ai agent
// loop with the gated toolset; "claude" drives the Claude Code CLI.
type Config struct {
	Provider     string
	Model        string
	APIKey       string
	BaseURL      string
	SystemPrompt string
	Thinking     string // "" / none / low / medium / high / xhigh
	Workdir      string // the tools' root and the exec environment's dir

	Backend         string        // "" | "builtin" | "claude"
	Mode            Mode          // three-level approval selector (spec §2)
	OnApproval      ApprovalFunc  // nil → an ask settles as a denial
	ApprovalTimeout time.Duration // 0 → DefaultApprovalTimeout
	ClaudeBin       string        // resolved claude CLI path (tests: a fake)
	ClaudeSessionID string        // --resume
}

// backend is the resolved loop kind: only "claude" dispatches to the CLI,
// everything else (including typos) stays on the builtin loop.
func (c Config) backend() string {
	if c.Backend == "claude" {
		return "claude"
	}
	return "builtin"
}

// Role says who a history message came from.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one history entry the loop receives.
type Message struct {
	Role Role
	Text string
	At   time.Time
}

// Callbacks receive the loop's events. Thinking and text are each turn's
// OWN accumulated buffer (the loop resets both on every message start), so
// a caller that wants the full reply joins the segments itself — a turn's
// text that follows tool results is the model's interim reply, which the
// UI lays out between the tool calls.
type Callbacks struct {
	OnThinking  func(text string)
	OnText      func(text string)
	OnToolStart func(callID, name, args string)
	OnToolEnd   func(callID, result string, isErr bool, dur time.Duration)
	// OnSession reports a backend's session id worth persisting so the
	// next turn resumes it (the claude init event). The builtin loop
	// never fires it.
	OnSession func(sessionID string)
	// OnNote reports a one-line note for the reply's timeline (compact
	// notices, turn summaries). The builtin loop never fires it.
	OnNote func(text string)
}

// Run executes one turn on the selected backend and folds its events into
// cb. It blocks; the caller runs it on a goroutine. A model-resolution failure
// or a stream error returns as an error (a context cancellation returns
// the loop's own error, which the caller filters by ctx.Err()).
func Run(ctx context.Context, cfg Config, history []Message, cb Callbacks) error {
	if cfg.backend() == "claude" {
		prompt, ok := lastUserMessage(history)
		if !ok {
			return fmt.Errorf("claude: nothing to send — the prompt is empty")
		}
		return RunClaude(ctx, ClaudeConfig{
			Bin:             cfg.ClaudeBin,
			Model:           cfg.Model,
			SystemPrompt:    cfg.SystemPrompt,
			Mode:            cfg.Mode,
			SessionID:       cfg.ClaudeSessionID,
			Workdir:         cfg.Workdir,
			ApprovalTimeout: cfg.ApprovalTimeout,
			OnApproval:      cfg.OnApproval,
		}, prompt, cb)
	}
	model, err := ResolveModel(cfg)
	if err != nil {
		return err
	}
	msgs := make([]piai.Message, 0, len(history))
	for _, m := range history {
		switch m.Role {
		case RoleUser:
			msgs = append(msgs, piai.UserMessage{Content: m.Text, Timestamp: m.At})
		case RoleAssistant:
			msgs = append(msgs, piai.AssistantMessage{
				Content:   []piai.ContentBlock{piai.TextContent{Type: "text", Text: m.Text}},
				Timestamp: m.At,
			})
		}
	}
	loopCfg := agent.AgentLoopConfig{
		Model:        model,
		SystemPrompt: cfg.SystemPrompt,
		Tools: Tools(cfg.Workdir, Gate{
			Mode:            cfg.Mode,
			Sandbox:         NewSandbox(),
			OnApproval:      cfg.OnApproval,
			ApprovalTimeout: cfg.ApprovalTimeout,
		}),
		ToolExecution:       core.ToolExecSequential,
		ExecEnv:             core.NewDefaultExecutionEnvWithDir(cfg.Workdir),
		SimpleStreamOptions: streamOptions(cfg),
	}
	stream := agent.AgentLoop(ctx, msgs, loopCfg)

	var think, body strings.Builder
	toolStart := map[string]time.Time{}
	_, err = stream.ForEach(ctx, func(evt agent.AgentEvent) error {
		switch e := evt.(type) {
		case agent.EventMessageStart:
			// Later turns (after tool results) keep filling the same reply,
			// but each one's thinking and text start fresh: the caller
			// joins the segments, keeping interim replies distinct.
			think.Reset()
			body.Reset()
		case agent.EventMessageUpdate:
			switch ev := e.AssistantEvent.(type) {
			case piai.EventThinkingDelta:
				think.WriteString(ev.Delta)
				if cb.OnThinking != nil {
					cb.OnThinking(think.String())
				}
			case piai.EventTextDelta:
				body.WriteString(ev.Delta)
				if cb.OnText != nil {
					cb.OnText(body.String())
				}
			}
		case agent.EventToolExecStart:
			toolStart[e.ToolCallID] = time.Now()
			if cb.OnToolStart != nil {
				cb.OnToolStart(e.ToolCallID, e.ToolName, string(e.Args))
			}
		case agent.EventToolExecEnd:
			dur := time.Since(toolStart[e.ToolCallID])
			if cb.OnToolEnd != nil {
				cb.OnToolEnd(e.ToolCallID, string(e.Result), e.IsError, dur)
			}
		}
		return nil
	})
	return err
}

// ResolveModel maps the config to a pi-ai Model. A known provider comes
// from the built-in registry (with a base-URL override when present); the
// OpenAI-compatible provider is built directly, since its model id and
// endpoint are user-supplied and not in the registry.
func ResolveModel(cfg Config) (piai.Model, error) {
	if cfg.Provider == ProviderOpenAICompat {
		base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
		if base == "" {
			return piai.Model{}, fmt.Errorf("openai-compatible: a Base URL is required")
		}
		id := strings.TrimSpace(cfg.Model)
		if id == "" {
			return piai.Model{}, fmt.Errorf("openai-compatible: a model is required")
		}
		return piai.Model{
			ID:            id,
			Name:          id,
			Provider:      piai.ProviderOpenAI,
			API:           piai.APIOpenAICompletions,
			BaseURL:       base,
			ContextWindow: 8192,
		}, nil
	}
	m, err := piai.GetModel(piai.KnownProvider(cfg.Provider), cfg.Model)
	if err != nil {
		return piai.Model{}, err
	}
	if u := strings.TrimSpace(cfg.BaseURL); u != "" {
		m.BaseURL = u
	}
	return m, nil
}

// ThinkDefault returns the default thinking-level string for a model that
// supports reasoning, else "none".
func ThinkDefault(provider, model string) string {
	m, err := piai.GetModel(piai.KnownProvider(provider), model)
	if err != nil || !m.Reasoning {
		return "none"
	}
	return string(piai.ThinkingMedium)
}

// lastUserMessage finds the newest user message: the claude backend's
// turn prompt. The CLI keeps its own history (--resume); Crux only sends
// what the user just typed.
func lastUserMessage(history []Message) (string, bool) {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == RoleUser {
			return history[i].Text, true
		}
	}
	return "", false
}

// streamOptions builds the streaming options: the API key and the reasoning
// level. Sampling (temperature / max tokens) is left to the provider's
// defaults — the settings UI does not expose them.
func streamOptions(cfg Config) piai.SimpleStreamOptions {
	s := piai.SimpleStreamOptions{
		StreamOptions: piai.StreamOptions{
			APIKey: strings.TrimSpace(cfg.APIKey),
		},
	}
	switch strings.ToLower(cfg.Thinking) {
	case "low":
		s.Reasoning = piai.ThinkingLow
	case "medium", "med":
		s.Reasoning = piai.ThinkingMedium
	case "high":
		s.Reasoning = piai.ThinkingHigh
	case "xhigh":
		s.Reasoning = piai.ThinkingXHigh
	}
	return s
}

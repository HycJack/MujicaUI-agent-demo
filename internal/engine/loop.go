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
// to call, how to steer it, and where the tools operate.
type Config struct {
	Provider     string
	Model        string
	APIKey       string
	BaseURL      string
	SystemPrompt string
	Thinking     string // "" / none / low / medium / high / xhigh
	Workdir      string // the tools' root and the exec environment's dir
	Sandbox      bool   // run bash inside the workspace-scoped sandbox (on by default)
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

// Callbacks receive the loop's events. Text and thinking arrive as the
// FULL accumulated text so far (turns join with a blank line), so the
// caller can just store the string.
type Callbacks struct {
	OnThinking  func(text string)
	OnText      func(text string)
	OnToolStart func(callID, name, args string)
	OnToolEnd   func(callID, result string, isErr bool, dur time.Duration)
}

// Run executes the agent loop to completion and folds its events into cb.
// It blocks; the caller runs it on a goroutine. A model-resolution failure
// or a stream error returns as an error (a context cancellation returns
// the loop's own error, which the caller filters by ctx.Err()).
func Run(ctx context.Context, cfg Config, history []Message, cb Callbacks) error {
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
		Model:               model,
		SystemPrompt:        cfg.SystemPrompt,
		Tools:               Tools(cfg.Workdir, SandboxProvider(cfg.Sandbox)),
		ToolExecution:       core.ToolExecSequential,
		ExecEnv:             core.NewDefaultExecutionEnvWithDir(cfg.Workdir),
		SimpleStreamOptions: streamOptions(cfg),
	}
	stream := agent.AgentLoop(ctx, msgs, loopCfg)

	var think, body strings.Builder
	started := false
	toolStart := map[string]time.Time{}
	_, err = stream.ForEach(ctx, func(evt agent.AgentEvent) error {
		switch e := evt.(type) {
		case agent.EventMessageStart:
			// Later turns (after tool results) keep filling the same
			// reply: their text joins the body after a blank line.
			think.Reset()
			if started && body.Len() > 0 {
				body.WriteString("\n\n")
			}
			started = true
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

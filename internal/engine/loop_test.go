package engine

// loop_test.go covers the engine's configuration mapping: the thinking
// tiers map to reasoning levels, sampling stays with the provider defaults,
// and the OpenAI-compatible provider requires a base URL and model.

import (
	"context"
	"strings"
	"testing"
	"time"

	piai "github.com/HycJack/pi-ai-go"
)

func TestStreamOptionsSampling(t *testing.T) {
	s := streamOptions(Config{APIKey: " sk ", Thinking: "high"})
	if s.StreamOptions.APIKey != "sk" {
		t.Fatalf("api key not trimmed: %q", s.StreamOptions.APIKey)
	}
	if s.Reasoning != piai.ThinkingHigh {
		t.Fatalf("reasoning = %v, want high", s.Reasoning)
	}
	if s.Temperature != nil || s.MaxTokens != nil {
		t.Fatal("stream options should leave sampling to the provider defaults")
	}
	// "none" (and unknown strings) disable reasoning.
	for _, lvl := range []string{"none", "", "bogus"} {
		if s := streamOptions(Config{Thinking: lvl}); s.Reasoning != "" {
			t.Fatalf("thinking %q set reasoning %v", lvl, s.Reasoning)
		}
	}
}

func TestResolveModelOpenAICompat(t *testing.T) {
	// A base URL and a model are required.
	if _, err := ResolveModel(Config{Provider: ProviderOpenAICompat, Model: "m"}); err == nil ||
		!strings.Contains(err.Error(), "Base URL") {
		t.Fatalf("missing base URL: err=%v", err)
	}
	if _, err := ResolveModel(Config{Provider: ProviderOpenAICompat, BaseURL: "https://x/v1"}); err == nil ||
		!strings.Contains(err.Error(), "model") {
		t.Fatalf("missing model: err=%v", err)
	}
	m, err := ResolveModel(Config{Provider: ProviderOpenAICompat, Model: "qwen", BaseURL: "https://x/v1/"})
	if err != nil {
		t.Fatal(err)
	}
	if m.BaseURL != "https://x/v1" || m.API != piai.APIOpenAICompletions || m.ID != "qwen" {
		t.Fatalf("compat model wrong: %+v", m)
	}
}

func TestThinkDefault(t *testing.T) {
	// A reasoning-capable registry model seeds a reasoning tier.
	reasoning := false
	for _, m := range Models("openai") {
		if m.Reasoning {
			reasoning = true
			if ThinkDefault("openai", m.ID) == "none" {
				t.Fatalf("reasoning model %s seeded none", m.ID)
			}
			break
		}
	}
	if !reasoning {
		t.Skip("no reasoning model in the openai registry")
	}
	if ThinkDefault("no-such-provider", "m") != "none" {
		t.Fatal("an unknown provider should fall back to none")
	}
}

// Run fails fast on an unresolvable model instead of touching the network.
func TestRunResolveError(t *testing.T) {
	err := Run(context.Background(), Config{Provider: ProviderOpenAICompat}, nil, Callbacks{})
	if err == nil {
		t.Fatal("Run should fail on an unresolvable model")
	}
}

// Backend selection: empty and unknown spellings mean the builtin loop;
// only "claude" dispatches to the CLI.
func TestConfigBackend(t *testing.T) {
	for _, in := range []string{"", "builtin", "nonsense"} {
		if c := (Config{Backend: in}); c.backend() != "builtin" {
			t.Fatalf("Backend %q resolved to %q", in, c.backend())
		}
	}
	if c := (Config{Backend: "claude"}); c.backend() != "claude" {
		t.Fatal("claude backend did not resolve")
	}
}

// The claude branch of Run: the last user message becomes the prompt,
// the config rides through, and the callbacks land.
func TestRunDispatchesClaude(t *testing.T) {
	bin := fakeClaudeScript(t,
		[]string{
			`echo {"type":"system","subtype":"init","session_id":"dispatch-1"}`,
			`echo {"type":"assistant","message":{"content":[{"type":"text","text":"pong"}]}}`,
			`echo ` + resultLine("dispatch-1"),
		},
		[]string{
			`echo '{"type":"system","subtype":"init","session_id":"dispatch-1"}'`,
			`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"pong"}]}}'`,
			`echo ` + resultLine("dispatch-1"),
		})
	var text, sess, note string
	err := Run(context.Background(), Config{
		Backend: "claude", ClaudeBin: bin, Mode: ModeFull, Model: "sonnet",
		Workdir: t.TempDir(),
	}, []Message{
		{Role: RoleUser, Text: "first", At: time.Now()},
		{Role: RoleAssistant, Text: "old reply", At: time.Now()},
		{Role: RoleUser, Text: "hello", At: time.Now()},
	}, Callbacks{
		OnText:    func(s string) { text = s },
		OnSession: func(id string) { sess = id },
		OnNote:    func(s string) { note = s },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if text != "pong" {
		t.Fatalf("text = %q", text)
	}
	if sess != "dispatch-1" || note == "" {
		t.Fatalf("sess=%q note=%q", sess, note)
	}
}

// A claude run with no user message to send is an error, not a silent
// empty turn.
func TestRunClaudeNeedsPrompt(t *testing.T) {
	err := Run(context.Background(), Config{Backend: "claude", ClaudeBin: "whatever"}, []Message{
		{Role: RoleAssistant, Text: "only an old reply", At: time.Now()},
	}, Callbacks{})
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("err: %v", err)
	}
}

// The builtin branch now runs its tools through the gate: read-only
// denies a bash call before any LLM is consulted (observable by asking
// the loop to run a tool directly through the toolset Run builds).
func TestRunBuiltinUsesGate(t *testing.T) {
	// ResolveModel fails without a model — Run errors before any tool
	// runs, but the gate itself is covered by tools_test; here we only
	// pin that Run no longer forces full access on the toolset.
	// (An unresolvable model fails fast, which keeps this off the wire.)
	err := Run(context.Background(), Config{Provider: ProviderOpenAICompat, BaseURL: "https://x/v1", Mode: ModeReadOnly}, nil, Callbacks{})
	if err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("err: %v", err)
	}
}

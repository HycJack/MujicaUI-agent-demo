package engine

// loop_test.go covers the engine's configuration mapping: the thinking
// tiers map to reasoning levels, sampling stays with the provider defaults,
// and the OpenAI-compatible provider requires a base URL and model.

import (
	"context"
	"strings"
	"testing"

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

package engine

// registry.go is the engine's face to the pi-ai provider registry: the
// settings UI lists providers/models and probes a connection through these
// helpers instead of touching the library directly.

import (
	"context"
	"slices"

	piai "github.com/HycJack/pi-ai-go"
)

// ModelInfo is the UI-facing summary of a registry model.
type ModelInfo struct {
	ID            string
	Name          string
	Reasoning     bool
	ContextWindow int
}

// Providers lists the registered provider ids, sorted.
func Providers() []string {
	provs := piai.GetProviders()
	out := make([]string, 0, len(provs))
	for _, p := range provs {
		out = append(out, string(p))
	}
	slices.Sort(out)
	return out
}

// Models lists a provider's registry models.
func Models(provider string) []ModelInfo {
	ms := piai.GetModels(piai.KnownProvider(provider))
	out := make([]ModelInfo, 0, len(ms))
	for _, m := range ms {
		out = append(out, ModelInfo{
			ID: m.ID, Name: m.Name, Reasoning: m.Reasoning, ContextWindow: m.ContextWindow,
		})
	}
	return out
}

// ModelInfoOf resolves one model from the registry.
func ModelInfoOf(provider, model string) (ModelInfo, error) {
	m, err := piai.GetModel(piai.KnownProvider(provider), model)
	if err != nil {
		return ModelInfo{}, err
	}
	return ModelInfo{
		ID: m.ID, Name: m.Name, Reasoning: m.Reasoning, ContextWindow: m.ContextWindow,
	}, nil
}

// TestConnection resolves the model and issues a one-message completion to
// verify the key/base URL work, returning ok and an error message.
func TestConnection(ctx context.Context, cfg Config) (bool, string) {
	m, err := ResolveModel(cfg)
	if err != nil {
		return false, err.Error()
	}
	_, err = piai.Complete(ctx, m, []piai.Message{
		piai.UserMessage{Content: "Reply with exactly: ok"},
	}, streamOptions(cfg))
	if err != nil {
		return false, err.Error()
	}
	return true, ""
}

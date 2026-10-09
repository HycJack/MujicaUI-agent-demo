package app

import (
	"testing"

	"github.com/ZacharyZhang-NY/MujicaUI/input"
)

// The app projects its settings onto the engine's Config: provider, model,
// key, base URL and the workspace root as the tools' workdir. The resolve
// rules themselves are covered in internal/engine.
func TestEngineConfigProjection(t *testing.T) {
	a := newApp()
	a.llm.Provider = openaiCompat
	a.llm.BaseURL = "https://example.com/v1/"
	a.llm.Model = "my-model"
	a.llm.APIKey = "sk"
	cfg := a.engineConfig()
	if cfg.Provider != openaiCompat || cfg.BaseURL != "https://example.com/v1/" ||
		cfg.Model != "my-model" || cfg.APIKey != "sk" {
		t.Fatalf("engine config projection drifted: %+v", cfg)
	}
	if cfg.Workdir != a.ws.root {
		t.Fatal("engine config workdir should follow the workspace root")
	}
}

// The Model TreeSelect offers registry models for a known provider and the
// fetched list for a custom endpoint, always keeping the current selection.
func TestModelTreeNodes(t *testing.T) {
	a := newApp()
	a.llm.Provider = "openai"
	a.llm.Model = "gpt-4o"
	if nodes := a.modelTreeNodes(); len(nodes) == 0 || !hasNode(nodes, "gpt-4o") {
		t.Fatal("known provider should offer registry models including the current one")
	}

	b := newApp()
	b.llm.Provider = openaiCompat
	b.provView.fetched = []string{"alpha", "beta"}
	b.llm.Model = "gamma" // a custom id not in the fetched list
	nodes := b.modelTreeNodes()
	if !hasNode(nodes, "alpha") || !hasNode(nodes, "beta") || !hasNode(nodes, "gamma") {
		t.Fatalf("custom endpoint nodes missing fetched/custom model: %v", nodes)
	}
}

// Off-window the fetch is a no-op that clears stale state (no network), so
// tests stay deterministic and fast.
func TestFetchModelsHeadless(t *testing.T) {
	a := newApp() // redraw == nil
	a.llm.Provider = openaiCompat
	a.llm.BaseURL = ""
	a.fetchModels()
	if a.provView.fetchErr == "" {
		t.Fatal("expected an error when the Base URL is empty")
	}
	a.llm.BaseURL = "https://x/v1"
	a.provView.fetchErr = "stale"
	a.provView.fetching = true
	a.fetchModels()
	if a.provView.fetchErr != "" || a.provView.fetching {
		t.Fatal("headless fetch should clear the error and not stay fetching")
	}
}

func hasNode(nodes []input.TreeSelectNode, id string) bool {
	for _, n := range nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

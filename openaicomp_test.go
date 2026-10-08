package main

import (
	"testing"

	piai "github.com/HycJack/pi-ai-go"
	"github.com/ZacharyZhang-NY/MujicaUI/input"
)

// An OpenAI-compatible backend resolves to a directly-built model (not from
// the registry), carrying the user's base URL and model id, and errors when
// the required pieces are missing.
func TestOpenAICompatResolve(t *testing.T) {
	a := newApp()
	a.llm.Provider = openaiCompat
	a.llm.BaseURL = "https://example.com/v1/"
	a.llm.Model = "my-model"
	m, err := a.resolveModel()
	if err != nil {
		t.Fatalf("resolveModel: %v", err)
	}
	if m.Provider != piai.ProviderOpenAI {
		t.Fatalf("provider=%q, want OpenAI", m.Provider)
	}
	if m.API != piai.APIOpenAICompletions {
		t.Fatalf("api=%q, want openai-completions", m.API)
	}
	if m.BaseURL != "https://example.com/v1" {
		t.Fatalf("baseURL=%q, want trailing slash trimmed", m.BaseURL)
	}
	if m.ID != "my-model" {
		t.Fatalf("id=%q, want my-model", m.ID)
	}

	b := newApp()
	b.llm.Provider = openaiCompat
	b.llm.Model = "m"
	if _, err := b.resolveModel(); err == nil {
		t.Fatal("expected an error when the Base URL is empty")
	}

	c := newApp()
	c.llm.Provider = openaiCompat
	c.llm.BaseURL = "https://x/v1"
	c.llm.Model = ""
	if _, err := c.resolveModel(); err == nil {
		t.Fatal("expected an error when the model is empty")
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

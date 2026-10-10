package app

// settings.go adds the Settings dialog the frontend needs: one modal with a
// left source-list (Providers / Agent). Providers lets the user pick the
// pi-ai backend (a known provider or a custom OpenAI-compatible endpoint),
// model (fetched list or registry), API key, base URL, reasoning tier, and
// test the connection. Agent holds the system prompt and the fixed toolset
// description. Both panes edit a.llm directly — values apply live and are
// saved the moment they change (see settingsDialogs) — and the dialog opens
// from the titlebar or the ⌘K palette.

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"crux-agent/internal/engine"
	"github.com/HycJack/MujicaUI/icons"
	"github.com/HycJack/MujicaUI/input"
	"github.com/HycJack/MujicaUI/overlay"
	"github.com/egoist/mygo/ui"
)

// openaiCompat is the synthetic provider id for a custom OpenAI-compatible
// endpoint (user supplies Base URL + API key + model id); the id itself is
// owned by the engine.
const openaiCompat = engine.ProviderOpenAICompat

// providerView is the Providers pane's transient UI state.
type providerView struct {
	testing  bool
	fetching bool     // a /models fetch is in flight
	fetched  []string // model ids fetched from the endpoint
	fetchErr string   // last fetch error, if any
}

// defaultProviderView returns the Providers pane's initial UI state.
func defaultProviderView() providerView { return providerView{} }

// knownProviderOptions builds the provider <select> choices: the OpenAI-
// compatible custom endpoint first, then pi-ai's registered providers, ordered
// and deduped so the dropdown reads cleanly.
func (a *app) knownProviderOptions() []input.SelectOption[string] {
	out := []input.SelectOption[string]{{Value: openaiCompat, Label: providerLabel(openaiCompat)}}
	for _, p := range engine.Providers() {
		out = append(out, input.SelectOption[string]{Value: p, Label: providerLabel(p)})
	}
	return out
}

// modelOptions builds the model <select> choices for the selected provider.
func (a *app) modelOptions() []input.SelectOption[string] {
	ms := engine.Models(a.llm.Provider)
	out := make([]input.SelectOption[string], 0, len(ms))
	for _, m := range ms {
		out = append(out, input.SelectOption[string]{Value: m.ID, Label: modelLabel(m)})
	}
	return out
}

// providerLabel is a friendly name for a provider id.
func providerLabel(id string) string {
	switch id {
	case openaiCompat:
		return "OpenAI-compatible"
	case "openai":
		return "OpenAI"
	case "anthropic":
		return "Anthropic"
	case "google":
		return "Google Gemini"
	case "google-vertex":
		return "Google Vertex"
	case "deepseek":
		return "DeepSeek"
	case "amazon-bedrock":
		return "AWS Bedrock"
	case "mistral":
		return "Mistral"
	case "azure-openai":
		return "Azure OpenAI"
	case "openai-codex":
		return "OpenAI Codex"
	case "github-copilot":
		return "GitHub Copilot"
	case "xiaomi":
		return "Xiaomi"
	case "glm":
		return "GLM"
	case "kimi":
		return "Kimi"
	case "moonshotai":
		return "Moonshot"
	}
	return id
}

// modelLabel is a friendly name for a model (falls back to its id).
func modelLabel(m engine.ModelInfo) string {
	if m.Name != "" {
		return m.Name
	}
	return m.ID
}

// backendLabel is the provider·model display for the title bar and composer,
// resolving the configured backend against the engine's registry.
func (a *app) backendLabel() string {
	m, err := engine.ModelInfoOf(a.llm.Provider, a.llm.Model)
	name := ""
	if err == nil {
		name = modelLabel(m)
	}
	if name == "" {
		name = a.llm.Model
	}
	if name == "" {
		return "no model configured"
	}
	return providerLabel(a.llm.Provider) + " · " + name
}

// settingsDialogs builds the single merged Settings modal: a left source-list
// (Providers / Agent) beside the active pane, the way a settings screen reads.
// The dialog runs every frame. Saving is live: while the dialog is open the
// marshaled settings are compared against the last persisted snapshot each
// frame and written on drift, so closing the window — or killing the process
// — with the dialog open can no longer lose the configuration. The open→closed
// transition still forces a final save for every close path.
func (a *app) settingsDialogs(c *ui.Context) {
	wasOpen := a.settingsOpen
	overlay.Dialog(c, &a.settingsOpen, overlay.DialogOptions{
		Title:       "Settings",
		Description: "Configure the LLM backend and how Crux drives it. Values apply live and persist locally across restarts.",
		Width:       780,
		Actions: func() {
			if input.Button(c, "Done", input.ButtonOptions{}).Clicked() {
				a.settingsOpen = false
			}
		},
	}, func() { a.settingsBody(c) })
	if a.settingsOpen {
		a.saveSettingsIfChanged()
	}
	if wasOpen && !a.settingsOpen {
		a.saveSettings()
	}
}

// saveSettingsIfChanged persists the settings when their marshaled form has
// drifted from the last snapshot. Cheap: the struct is a handful of strings.
func (a *app) saveSettingsIfChanged() {
	buf, err := json.Marshal(a.llm)
	if err != nil || string(buf) == a.savedSettings {
		return
	}
	a.saveSettings()
}

// settingsBody is the dialog's content: a left source-list choosing the pane
// and the pane itself on the right. The body's height is fixed from the
// window size, so switching panes never resizes the dialog; a pane taller
// than the body scrolls inside it.
func (a *app) settingsBody(c *ui.Context) {
	k := tokens(c)
	_, wh := c.Size()
	h := settingsBodyHeight(wh)
	ui.Row(c).Height(h).AlignItems(ui.Stretch).Label("settings-body").Children(func() {
		ui.Column(c).Width(180).Shrink(0).Background(k.Surface).Padding(8).Gap(4).Children(func() {
			a.settingsNavItem(c, k, "providers", "Providers", icons.Must("settings"))
			a.settingsNavItem(c, k, "agent", "Agent", icons.Must("bot"))
		})
		ui.Box(c).Width(1).Shrink(0).Background(k.Border)
		ui.Scroll(c).Grow(1).MinWidth(0).Children(func() {
			ui.Column(c).Gap(14).PaddingX(22).Children(func() {
				switch a.settingsTab {
				case "agent":
					a.agentPane(c)
				default:
					a.providersPane(c)
				}
			})
		})
	})
}

// settingsBodyHeight maps the window height to the settings body's fixed
// height: about 62% of the window, clamped so the dialog (header + actions
// included) always fits inside it.
func settingsBodyHeight(windowH float32) float32 {
	h := windowH * 0.62
	if h < 420 {
		h = 420
	}
	if max := windowH - 220; h > max {
		h = max
	}
	return h
}

// settingsNavItem is one selectable row in the settings left rail.
func (a *app) settingsNavItem(c *ui.Context, k tokensT, id, label string, icon *ui.SVG) {
	selected := a.settingsTab == id
	b := ui.ButtonBase(c).Label(label).FillWidth().Padding(8).Radius(7).Gap(8).Cursor(ui.CursorPointer)
	switch {
	case selected:
		b.Background(k.Selection)
	case b.Hovered():
		b.Background(k.SurfaceHover)
	}
	if b.Clicked() {
		a.settingsTab = id
	}
	b.Children(func() {
		ic := ui.Icon(c, icon).FontSize(15)
		t := ui.Text(c, label).FontSize(13)
		if selected {
			ic.TextColor(k.AccentText)
			t.TextColor(k.Text)
		} else {
			ic.TextColor(k.TextMuted)
			t.TextColor(k.TextMuted)
		}
	})
}

// providersPane is the Providers settings pane content.
func (a *app) providersPane(c *ui.Context) {
	ui.Column(c).Gap(14).Children(func() {
		// Provider
		prevProvider := a.llm.Provider
		input.FormField(c, "Provider", input.FormFieldOptions{Description: "A known provider, or OpenAI-compatible for a custom endpoint."}, func() ui.Element {
			return input.Select(c, &a.llm.Provider, a.knownProviderOptions(), input.SelectOptions{}).Element
		})
		if prevProvider != a.llm.Provider {
			a.rebindModel()
		}

		// Base URL (optional for known providers, required for OpenAI-compatible)
		prevBase := a.llm.BaseURL
		desc := "Optional; overrides the provider's default endpoint."
		if a.llm.Provider == openaiCompat {
			desc = "Required for OpenAI-compatible, e.g. https://host/v1"
		}
		input.FormField(c, "Base URL", input.FormFieldOptions{Description: desc}, func() ui.Element {
			return input.InputGroup(c, &a.llm.BaseURL, input.InputGroupOptions{Placeholder: "https://…/v1", Label: "Base URL"}).Input
		})
		if prevBase != a.llm.BaseURL {
			a.provView.fetched = nil
			a.provView.fetchErr = ""
		}

		// API key
		input.FormField(c, "API key", input.FormFieldOptions{Description: "Leave blank to use the provider's env var."}, func() ui.Element {
			return input.InputGroup(c, &a.llm.APIKey, input.InputGroupOptions{Placeholder: "sk-…", Label: "API key"}).Input
		})

		// Model
		a.modelField(c)

		// Reasoning: a fixed ladder of thinking tiers; it belongs with the
		// backend it applies to, and models without reasoning ignore it.
		input.FormField(c, "Reasoning", input.FormFieldOptions{Description: "Thinking tier for models that support it; ignored otherwise."}, func() ui.Element {
			return input.Select(c, &a.llm.Thinking, reasoningTiers, input.SelectOptions{}).Element
		})

		// Test connection
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			testBtn := ui.Button(c, a.provView.testingText())
			if testBtn.Clicked() {
				a.testConnection()
			}
			if a.llm.ConnectionErr != "" {
				ui.Text(c, "⚠ "+a.llm.ConnectionErr).FontSize(11).TextColor(kDanger(c))
			} else if a.llm.ProviderOK {
				ui.Text(c, "✓ connected").FontSize(11).TextColor(kSuccess(c))
			}
		})
	})
}

// modelField renders the Model TreeSelect, plus a "fetch models" action and its
// status whenever a Base URL is set (the custom-endpoint path).
func (a *app) modelField(c *ui.Context) {
	input.FormField(c, "Model", input.FormFieldOptions{Description: "Choose a model; for a custom endpoint, fetch the list first."}, func() ui.Element {
		sel := []string{}
		if a.llm.Model != "" {
			sel = append(sel, a.llm.Model)
		}
		ts := input.TreeSelect(c, &sel, a.modelTreeNodes(), input.TreeSelectOptions{Label: "Model", Placeholder: "Select a model"})
		if ts.Changed() {
			a.llm.Model = ""
			if len(sel) > 0 {
				a.llm.Model = sel[0]
			}
		}
		return ts.Element
	})
	if a.llm.Provider == openaiCompat || strings.TrimSpace(a.llm.BaseURL) != "" {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			fetchBtn := ui.Button(c, a.provView.fetchText())
			if fetchBtn.Clicked() {
				a.fetchModels()
			}
			switch {
			case a.provView.fetchErr != "":
				ui.Text(c, "⚠ "+a.provView.fetchErr).FontSize(11).TextColor(kDanger(c))
			case len(a.provView.fetched) > 0:
				ui.Text(c, fmt.Sprintf("%d models loaded", len(a.provView.fetched))).FontSize(11).TextColor(kTextMuted(c))
			case strings.TrimSpace(a.llm.BaseURL) == "":
				ui.Text(c, "Enter a Base URL to fetch models").FontSize(11).TextColor(kTextMuted(c))
			}
		})
	}
}

// fetchText labels the fetch-models button.
func (pv *providerView) fetchText() string {
	if pv.fetching {
		return "Fetching…"
	}
	return "Fetch models"
}

// modelTreeNodes builds the TreeSelect nodes for the Model field: the fetched
// list for a custom endpoint, otherwise the provider's registry models. The
// current selection is always present so it is not lost.
func (a *app) modelTreeNodes() []input.TreeSelectNode {
	var nodes []input.TreeSelectNode
	if a.llm.Provider == openaiCompat {
		for _, id := range a.provView.fetched {
			nodes = append(nodes, input.TreeSelectNode{ID: id, Label: id})
		}
		if a.llm.Model != "" && !slices.Contains(a.provView.fetched, a.llm.Model) {
			nodes = append(nodes, input.TreeSelectNode{ID: a.llm.Model, Label: a.llm.Model + " (custom)"})
		}
		return nodes
	}
	for _, o := range a.modelOptions() {
		nodes = append(nodes, input.TreeSelectNode{ID: o.Value, Label: o.Label})
	}
	if a.llm.Model != "" && !slices.ContainsFunc(nodes, func(n input.TreeSelectNode) bool { return n.ID == a.llm.Model }) {
		nodes = append(nodes, input.TreeSelectNode{ID: a.llm.Model, Label: a.llm.Model})
	}
	return nodes
}

// fetchModels loads the model list from an OpenAI-compatible GET {base}/models,
// off the UI thread, and lands the result through a.redraw.
func (a *app) fetchModels() {
	base := strings.TrimRight(strings.TrimSpace(a.llm.BaseURL), "/")
	if base == "" {
		a.provView.fetchErr = "enter a Base URL first"
		return
	}
	if a.redraw == nil {
		// Headless / tests: no network; clear and mark ready so tests are deterministic.
		a.provView.fetching = false
		a.provView.fetchErr = ""
		return
	}
	if a.provView.fetching {
		return
	}
	key := strings.TrimSpace(a.llm.APIKey)
	a.provView.fetching = true
	a.provView.fetchErr = ""
	go func() {
		ids, err := engine.FetchOpenAIModels(base, key)
		a.redraw(func() {
			a.provView.fetching = false
			if err != nil {
				a.provView.fetchErr = err.Error()
				return
			}
			a.provView.fetched = ids
		})
	}()
}

func (a *app) rebindModel() {
	// Custom endpoint: the model comes from the fetched list (or is typed),
	// so only keep it if it is known; no model is picked automatically.
	if a.llm.Provider == openaiCompat {
		if a.llm.Model != "" && !slices.Contains(a.provView.fetched, a.llm.Model) {
			a.llm.Model = ""
		}
		return
	}
	// A known provider: keep the current model only if it is still on the
	// provider's list; a model that is not is dropped — none is auto-picked.
	ms := a.modelOptions()
	if a.llm.Model != "" && !slices.ContainsFunc(ms, func(o input.SelectOption[string]) bool { return o.Value == a.llm.Model }) {
		a.llm.Model = ""
	}
	a.applyModelDefaults()
}

// applyModelDefaults resets the reasoning level when the model cannot
// reason. Sampling is left to the provider defaults.
func (a *app) applyModelDefaults() {
	m, err := engine.ModelInfoOf(a.llm.Provider, a.llm.Model)
	if err != nil {
		return
	}
	if !m.Reasoning {
		a.llm.Thinking = "none"
	}
}

// testingText labels the test-connection button.
func (pv *providerView) testingText() string {
	if pv.testing {
		return "Testing…"
	}
	return "Test connection"
}

// testConnection asks the engine to resolve the model and issue a
// one-message completion, surfacing the outcome on the Providers pane.
func (a *app) testConnection() {
	if a.llm.ProviderOK || a.provView.testing {
		return
	}
	cfg := a.engineConfig()
	if a.redraw == nil {
		// No window: just check resolution so tests stay fast.
		ok, msg := engine.TestConnection(context.Background(), cfg)
		a.llm.ProviderOK = ok
		a.llm.ConnectionErr = msg
		return
	}
	go func() {
		a.redraw(func() { a.provView.testing = true })
		ok, msg := engine.TestConnection(context.Background(), cfg)
		a.redraw(func() {
			a.provView.testing = false
			a.llm.ProviderOK = ok
			a.llm.ConnectionErr = msg
		})
	}()
}

// agentPane is the Agent dialog's content: the system prompt and the
// agent-loop facts. Sampling (temperature / max tokens) is left to the
// provider defaults — an agent console has no business tuning them.
func (a *app) agentPane(c *ui.Context) {
	k := tokens(c)
	ui.Column(c).Gap(14).Children(func() {
		// System prompt
		input.FormField(c, "System prompt", input.FormFieldOptions{Description: "Shapes role and tone for every reply."}, func() ui.Element {
			return ui.TextArea(c, &a.llm.SystemPrompt).MinHeight(96).Label("System prompt")
		})

		// What the agent can do — fixed toolset, no toggles.
		ui.Text(c, "Tools").FontSize(11).TextColor(k.TextMuted)
		for _, t := range []struct{ name, desc string }{
			{"bash", "Run shell commands in the workspace (build, test, git…)."},
			{"read_file", "Read files from the workspace."},
			{"write_file", "Create or overwrite workspace files (shown as review cards)."},
		} {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icons.Must("terminal")).FontSize(13).TextColor(k.Accent)
				ui.Column(c).Gap(1).Children(func() {
					ui.Text(c, t.name).FontSize(12).Bold()
					ui.Text(c, t.desc).FontSize(11).TextColor(k.TextMuted)
				})
			})
		}

		// Sandbox: bash runs inside the workspace-scoped boundary —
		// workdir read-write, a scratch HOME, no network, credential
		// stores excluded. Off runs commands as plain child processes.
		on := a.llm.SandboxOn()
		if input.Switch(c, &on, "Sandbox shell commands", input.SwitchOptions{
			Description: "Run bash inside the workspace boundary: writes limited to the workspace and a scratch home, network denied. A missing backend reports an error instead of running unsandboxed.",
		}).Changed() {
			a.llm.Sandbox = &on
		}

		// Stop / status
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if a.llm.Busy {
				stop := ui.Button(c, "Stop streaming")
				if stop.Clicked() {
					a.cancel()
				}
				ui.Text(c, "A reply is streaming…").FontSize(11).TextColor(kTextMuted(c))
			} else if a.llm.LastError != "" {
				ui.Text(c, "⚠ "+a.llm.LastError).FontSize(11).TextColor(kDanger(c))
			} else {
				ui.Text(c, "Idle").FontSize(11).TextColor(kTextMuted(c))
			}
		})
	})
}

// reasoningTiers is the fixed ladder of thinking levels surfaced in the
// Providers pane, next to the backend it applies to; the values match
// pi-ai's ThinkingLevel strings.
var reasoningTiers = []input.SelectOption[string]{
	{Value: "none", Label: "Off"},
	{Value: "low", Label: "Low"},
	{Value: "medium", Label: "Medium"},
	{Value: "high", Label: "High"},
}

// kTextMuted/kDanger/kSuccess resolve theme accents on the active token set.
func kTextMuted(c *ui.Context) ui.Color { return tokens(c).TextMuted }
func kDanger(c *ui.Context) ui.Color    { return tokens(c).Danger }
func kSuccess(c *ui.Context) ui.Color   { return tokens(c).Success }

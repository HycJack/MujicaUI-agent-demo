package main

// settings.go adds the Settings dialog the frontend needs: one modal with a
// left source-list (Providers / Agent). Providers lets the user pick the
// pi-ai backend (a known provider or a custom OpenAI-compatible endpoint),
// model (fetched list or registry), API key and base URL, and test the
// connection. Agent holds the system prompt, reasoning level, sampling, max
// tokens and streaming. Both panes edit a.llm directly — values apply live,
// the way the rest of Atlas renders state — and the dialog opens from the
// titlebar or the ⌘K palette.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	piai "github.com/HycJack/pi-ai-go"
	"github.com/ZacharyZhang-NY/MujicaUI/icons"
	"github.com/ZacharyZhang-NY/MujicaUI/input"
	"github.com/ZacharyZhang-NY/MujicaUI/overlay"
	"github.com/egoist/mygo/ui"
)

// openaiCompat is the synthetic provider id for a custom OpenAI-compatible
// endpoint (user supplies Base URL + API key + model id).
const openaiCompat = "openai-compatible"

// providerView is the Providers pane's transient UI state.
type providerView struct {
	testing  bool
	fetching bool     // a /models fetch is in flight
	fetched  []string // model ids fetched from the endpoint
	fetchErr string   // last fetch error, if any
}

// defaultProviderView returns the Providers pane's initial UI state.
func defaultProviderView() providerView { return providerView{} }

// agentView is the Agent pane's transient UI state.
type agentView struct {
	seededModel string // provider·model the max-tokens field was last seeded for
}

// defaultAgentView returns the Agent pane's initial UI state.
func defaultAgentView() agentView { return agentView{} }

// knownProviderOptions builds the provider <select> choices: the OpenAI-
// compatible custom endpoint first, then pi-ai's registered providers, ordered
// and deduped so the dropdown reads cleanly.
func (a *app) knownProviderOptions() []input.SelectOption[string] {
	out := []input.SelectOption[string]{{Value: openaiCompat, Label: providerLabel(openaiCompat)}}
	provs := piai.GetProviders()
	var provs2 []string
	for _, p := range provs {
		provs2 = append(provs2, string(p))
	}
	slices.Sort(provs2)
	for _, p := range provs2 {
		out = append(out, input.SelectOption[string]{Value: p, Label: providerLabel(p)})
	}
	return out
}

// modelOptions builds the model <select> choices for the selected provider.
func (a *app) modelOptions() []input.SelectOption[string] {
	ms := piai.GetModels(piai.KnownProvider(a.llm.Provider))
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
func modelLabel(m piai.Model) string {
	if m.Name != "" {
		return m.Name
	}
	return m.ID
}

// backendLabel is the provider·model display for the title bar and composer,
// resolving the configured backend against the pi-ai registry.
func (a *app) backendLabel() string {
	m, err := piai.GetModel(piai.KnownProvider(a.llm.Provider), a.llm.Model)
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
func (a *app) settingsDialogs(c *ui.Context) {
	overlay.Dialog(c, &a.settingsOpen, overlay.DialogOptions{
		Title:       "Settings",
		Description: "Configure the LLM backend and how Atlas drives it. Values apply live and are kept in app memory only.",
		Width:       780,
		Actions: func() {
			if input.Button(c, "Done", input.ButtonOptions{}).Clicked() {
				a.settingsOpen = false
			}
		},
	}, func() { a.settingsBody(c) })
}

// settingsBody is the dialog's content: a left source-list choosing the pane
// and the pane itself on the right, the way a settings screen reads.
func (a *app) settingsBody(c *ui.Context) {
	k := tokens(c)
	ui.Row(c).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Width(180).Shrink(0).Background(k.Surface).Padding(8).Gap(4).Children(func() {
			a.settingsNavItem(c, k, "providers", "Providers", icons.Must("settings"))
			a.settingsNavItem(c, k, "agent", "Agent", icons.Must("bot"))
		})
		ui.Box(c).Width(1).Background(k.Border).Height(420)
		ui.Column(c).Grow(1).MinWidth(0).Gap(14).Children(func() {
			switch a.settingsTab {
			case "agent":
				a.agentPane(c)
			default:
				a.providersPane(c)
			}
		})
	})
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
		input.FormField(c, "Provider", input.FormFieldOptions{Description: "A known provider, or OpenAI-compatible for a custom endpoint."}, func() *ui.Element {
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
		input.FormField(c, "Base URL", input.FormFieldOptions{Description: desc}, func() *ui.Element {
			return input.InputGroup(c, &a.llm.BaseURL, input.InputGroupOptions{Placeholder: "https://…/v1", Label: "Base URL"}).Input
		})
		if prevBase != a.llm.BaseURL {
			a.provView.fetched = nil
			a.provView.fetchErr = ""
		}

		// API key
		input.FormField(c, "API key", input.FormFieldOptions{Description: "Leave blank to use the provider's env var."}, func() *ui.Element {
			return input.InputGroup(c, &a.llm.APIKey, input.InputGroupOptions{Placeholder: "sk-…", Label: "API key"}).Input
		})

		// Model
		a.modelField(c)

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
	input.FormField(c, "Model", input.FormFieldOptions{Description: "Choose a model; for a custom endpoint, fetch the list first."}, func() *ui.Element {
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
		ids, err := fetchOpenAIModels(base, key)
		a.redraw(func() {
			a.provView.fetching = false
			if err != nil {
				a.provView.fetchErr = err.Error()
				return
			}
			a.provView.fetched = ids
			if a.llm.Model == "" && len(ids) > 0 {
				a.llm.Model = ids[0]
			}
		})
	}()
}

// fetchOpenAIModels calls GET {base}/models and returns the model ids, sorted.
func fetchOpenAIModels(base, key string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.Data))
	for _, d := range out.Data {
		if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (a *app) rebindModel() {
	// Custom endpoint: the model comes from the fetched list (or is typed), so
	// only keep it if it is known, else fall back to the first fetched or clear.
	if a.llm.Provider == openaiCompat {
		if a.llm.Model != "" && !slices.Contains(a.provView.fetched, a.llm.Model) {
			if len(a.provView.fetched) > 0 {
				a.llm.Model = a.provView.fetched[0]
			} else {
				a.llm.Model = ""
			}
		}
		return
	}
	ms := a.modelOptions()
	if len(ms) == 0 {
		a.llm.Model = ""
		return
	}
	// Keep the same id if still present, else pick the first model.
	if !slices.ContainsFunc(ms, func(o input.SelectOption[string]) bool { return o.Value == a.llm.Model }) {
		a.llm.Model = ms[0].Value
	}
	a.applyModelDefaults()
}

// applyModelDefaults seeds MaxTokens from the resolved model and resets the
// reasoning level when the model cannot reason.
func (a *app) applyModelDefaults() {
	m, err := piai.GetModel(piai.KnownProvider(a.llm.Provider), a.llm.Model)
	if err != nil {
		return
	}
	if m.MaxTokens > 0 {
		a.llm.MaxTokens = m.MaxTokens
		a.maxTokField = fmt.Sprintf("%d", m.MaxTokens)
	}
	if !m.Reasoning {
		a.llm.Thinking = "none"
	}
}

// syncMaxTokens copies the max-tokens field into the settings each frame so
// edits take effect; the field stays the editing source of truth.
func (a *app) syncMaxTokens() {
	if n := parseMaxTokens(a.maxTokField); n >= 64 {
		a.llm.MaxTokens = n
	}
}

// testingText labels the test-connection button.
func (pv *providerView) testingText() string {
	if pv.testing {
		return "Testing…"
	}
	return "Test connection"
}

// testConnection resolves the model and issues a one-message completion to
// verify the key/base URL work, surfacing the outcome on the Providers pane.
func (a *app) testConnection() {
	if a.llm.ProviderOK || a.provView.testing {
		return
	}
	if a.redraw == nil {
		// No window: just check resolution so tests stay fast.
		_, err := a.resolveModel()
		a.llm.ProviderOK = err == nil
		if err != nil {
			a.llm.ConnectionErr = err.Error()
		}
		return
	}
	go func() {
		a.redraw(func() { a.provView.testing = true })
		ok, msg := a.runConnectionTest()
		a.redraw(func() {
			a.provView.testing = false
			a.llm.ProviderOK = ok
			a.llm.ConnectionErr = msg
		})
	}()
}

// runConnectionTest sends a one-message completion against the resolved model.
func (a *app) runConnectionTest() (bool, string) {
	m, err := a.resolveModel()
	if err != nil {
		return false, err.Error()
	}
	_, err = piai.Complete(context.Background(), m, []piai.Message{
		piai.UserMessage{Content: "Reply with exactly: ok"},
	}, a.streamOptions())
	if err != nil {
		return false, err.Error()
	}
	return true, ""
}

// agentPane is the Agent dialog's content.
func (a *app) agentPane(c *ui.Context) {
	ui.Column(c).Gap(14).Children(func() {
		// System prompt
		input.FormField(c, "System prompt", input.FormFieldOptions{Description: "Shapes role and tone for every reply."}, func() *ui.Element {
			return ui.TextArea(c, &a.llm.SystemPrompt).MinHeight(96).Label("System prompt")
		})

		// Thinking level
		input.FormField(c, "Reasoning", input.FormFieldOptions{Description: "Thinking budget for models that support it."}, func() *ui.Element {
			return input.Select(c, &a.llm.Thinking, a.thinkingOptions(), input.SelectOptions{}).Element
		})

		// Temperature
		input.FormField(c, "Temperature", input.FormFieldOptions{Description: "Sampling randomness; lower is more deterministic."}, func() *ui.Element {
			return input.Slider(c, &a.llm.Temperature, input.SliderOptions{Min: 0, Max: 2, Step: 0.1, ShowValue: true, Label: "Temperature"}).Element
		})

		// Max tokens (seeded from the model default when the model changes).
		a.seedMaxTokensField()
		input.FormField(c, "Max tokens", input.FormFieldOptions{Description: "Upper bound on an assistant reply."}, func() *ui.Element {
			return input.InputGroup(c, &a.maxTokField, input.InputGroupOptions{Label: "Max tokens"}).Input
		})
		a.syncMaxTokens()

		// Streaming toggle
		input.FormField(c, "Stream output", input.FormFieldOptions{Description: "Stream the reply live into the thread."}, func() *ui.Element {
			return input.Switch(c, &a.llm.StreamOutput, "Stream output", input.SwitchOptions{})
		})

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

// thinkingOptions is a copy of thinkOptions exposed for the Agent page.
func (a *app) thinkingOptions() []input.SelectOption[string] {
	out := make([]input.SelectOption[string], 0, 4)
	for _, t := range thinkChoices {
		if a.thinkingEnabled() || t == "none" {
			out = append(out, input.SelectOption[string]{Value: t, Label: t})
		}
	}
	return out
}

// thinkingEnabled mirrors thinkOptions so the dropdown shrinks for models that
// cannot reason.
func (a *app) thinkingEnabled() bool {
	m, err := piai.GetModel(piai.KnownProvider(a.llm.Provider), a.llm.Model)
	return err == nil && m.Reasoning
}

// thinkChoices is the ordered set of reasoning levels surfaced in the Agent UI.
var thinkChoices = []string{"none", "low", "medium", "high"}

// seedMaxTokensField initializes the max-tokens field from the resolved
// model's default the first time that model is seen, leaving user edits alone
// thereafter.
func (a *app) seedMaxTokensField() {
	key := a.llm.Provider + "/" + a.llm.Model
	if a.agentView.seededModel == key {
		return
	}
	a.agentView.seededModel = key
	if n := a.modelMaxTokens(); n >= 64 {
		a.maxTokField = fmt.Sprintf("%d", n)
	}
}

// modelMaxTokens returns the configured model's MaxTokens, or 0 if unknown.
func (a *app) modelMaxTokens() int {
	m, err := piai.GetModel(piai.KnownProvider(a.llm.Provider), a.llm.Model)
	if err != nil {
		return 0
	}
	return m.MaxTokens
}

func parseMaxTokens(s string) int {
	n := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil {
		return -1
	}
	return n
}

// kTextMuted/kDanger/kSuccess resolve theme accents on the active token set.
func kTextMuted(c *ui.Context) ui.Color { return tokens(c).TextMuted }
func kDanger(c *ui.Context) ui.Color    { return tokens(c).Danger }
func kSuccess(c *ui.Context) ui.Color   { return tokens(c).Success }

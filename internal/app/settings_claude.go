package app

// settings_claude.go is the Claude Code form on the Providers pane: the
// CLI detection row (a `claude --version` probe), the model alias field,
// and the auth note. It shares the pane with the builtin form through the
// Agent type selector (settings.go providersPane).

import (
	"os/exec"

	"github.com/ZacharyZhang-NY/MujicaUI/input"
	"github.com/egoist/mygo/ui"
)

// claudeProvidersPane is the Providers pane's claude form: no key, no
// endpoint — the CLI signs in itself and picks its own default model.
func (a *app) claudeProvidersPane(c *ui.Context) {
	ui.Column(c).Gap(14).Children(func() {
		// CLI detection
		input.FormField(c, "Claude CLI", input.FormFieldOptions{
			Description: "The claude binary Crux spawns; install with npm i -g @anthropic-ai/claude-code and sign in with claude login.",
		}, func() *ui.Element {
			return ui.Column(c).Gap(6).Children(func() {
				status := "not found on PATH"
				color := kDanger(c)
				if a.provView.claudePath != "" {
					status = a.provView.claudePath
					if a.provView.claudeVer != "" {
						status += " (" + a.provView.claudeVer + ")"
					}
					color = kSuccess(c)
				}
				if a.provView.claudeErr != "" {
					status = a.provView.claudeErr
					color = kDanger(c)
				}
				ui.Text(c, status).FontSize(11).TextColor(color)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					btn := ui.Button(c, a.provView.claudeProbeText())
					if btn.Clicked() {
						a.detectClaude()
					}
				})
			})
		})

		// Model alias: sonnet / opus / …; empty = the CLI's default.
		input.FormField(c, "Model", input.FormFieldOptions{
			Description: "A model alias the CLI accepts (sonnet, opus, …); leave empty for the CLI's default.",
		}, func() *ui.Element {
			return input.InputGroup(c, &a.llm.Model, input.InputGroupOptions{
				Placeholder: "sonnet — empty uses the CLI default", Label: "Model",
			}).Input
		})

		// Auth
		input.FormField(c, "Auth", input.FormFieldOptions{
			Description: "The CLI signs in itself (claude login); Crux stores no key for this backend.",
		}, func() *ui.Element {
			return ui.Text(c, "Uses the CLI's own sign-in").FontSize(11).TextColor(kTextMuted(c))
		})
	})
}

// claudeProbeText labels the detection button.
func (pv *providerView) claudeProbeText() string {
	if pv.claudeProbing {
		return "Detecting…"
	}
	return "Detect"
}

// detectClaude probes the claude CLI (a --version run) and settles the
// pane's detection row. Headless (a.redraw nil, tests) it settles
// synchronously; live it parks a goroutine behind a.redraw.
func (a *app) detectClaude() {
	if a.provView.claudeProbing {
		return
	}
	bin := a.claudeBin
	if bin == "" {
		a.provView.claudeErr = "no claude CLI on PATH — install it, then reopen settings"
		return
	}
	if a.redraw == nil {
		// Headless: run inline, settle in place.
		a.provView.claudeProbing = true
		out, err := exec.Command(bin, "--version").Output()
		a.provView.claudeProbing = false
		a.settleClaudeProbe(bin, string(out), err)
		return
	}
	a.provView.claudeProbing = true
	a.redraw(func() {})
	go func() {
		out, err := exec.Command(bin, "--version").Output()
		a.redraw(func() { a.settleClaudeProbe(bin, string(out), err) })
	}()
}

// settleClaudeProbe records one probe's outcome on the pane's row.
func (a *app) settleClaudeProbe(bin, out string, err error) {
	a.provView.claudeProbing = false
	a.provView.claudePath = bin
	if err != nil {
		a.provView.claudeErr = "claude --version failed: " + err.Error()
		return
	}
	a.provView.claudeErr = ""
	a.provView.claudeVer = firstLine(out)
}

package app

// thread.go renders the Crux conversation: the scrolling transcript in
// the middle, a composer below it, and the agent cards that a Codex-style
// assistant drops into the flow (thinking, tool calls, file changes,
// terminal runs and a multi-file diff). This mirrors the dashboard's
// agent thread but as the app's working surface.

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"crux-agent/internal/engine"

	"github.com/ZacharyZhang-NY/MujicaUI/agent"
	"github.com/ZacharyZhang-NY/MujicaUI/chat"
	"github.com/ZacharyZhang-NY/MujicaUI/core"
	"github.com/ZacharyZhang-NY/MujicaUI/data"
	"github.com/ZacharyZhang-NY/MujicaUI/icons"
	"github.com/egoist/mygo/ui"
)

// threadView is the center pane: the message list over the composer.
func (a *app) threadView(c *ui.Context) {
	k := tokens(c)
	ui.Box(c).Grow(1).MinHeight(0).Border(1, k.Border).Clip().Children(func() {
		if len(a.thread.rows) == 0 {
			a.welcome(c, k)
			return
		}
		chat.ChatContainer(c, &a.thread.list, len(a.thread.rows), chat.ChatContainerOptions{
			List: chat.MessageListOptions{
				ID:    func(i int) string { return a.thread.rows[i].id },
				Date:  func(i int) time.Time { return a.thread.rows[i].at },
				Label: func(i int) string { return a.thread.rows[i].text },
			},
			Input: func() { a.composer(c) },
		}, func(i int) { a.renderRow(c, &a.thread.rows[i]) })
	})
}

// renderRow paints one transcript row by kind. One AI reply is one bubble:
// the process area folds its thinking and tool calls in three layers (see
// processBlock), and the text renders as markdown once the reply completes.
// The standalone tool-row kinds only remain for transcripts stored before
// the merge.
func (a *app) renderRow(c *ui.Context, r *row) {
	switch r.kind {
	case rowTyping:
		chat.TypingIndicator(c, "Crux")
	case rowTools:
		t := r.tool
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Crux"}, func() {
			agent.ToolCallCard(c, agent.ToolCall{
				ID: t.callID, Name: t.name, Args: t.args, Result: t.result,
				Error: t.errMsg, State: t.state, Duration: t.dur,
			}, agent.ToolCallCardOptions{})
		})
	case rowCommand:
		t := r.tool
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Crux"}, func() {
			// Changed requests an abort: stop the in-flight agent loop.
			if agent.CommandExecutionCard(c, t.run, agent.CommandExecutionCardOptions{}).Changed() {
				a.cancel()
			}
		})
	case rowDiff:
		t := r.tool
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Crux"}, func() {
			// The write already happened; the card is the review record.
			agent.FileChangeCard(c, &t.decision, t.change, agent.FileChangeCardOptions{Preview: 4})
		})
	case rowReasoned:
		// The bubble is the WHOLE reply's container: the process timeline
		// (group header + steps laid out in order) and the answer text all
		// live in it, the answer being the LAST message segment (earlier
		// segments are interim replies inside the timeline).
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Crux"}, func() {
			// Waiting for the model's first token: the dots keep the bubble
			// from sitting empty.
			if r.streaming && r.text == "" && r.thinkText == "" && len(r.tools) == 0 {
				chat.TypingIndicator(c, "Crux")
			}
			if r.thinkText != "" || len(r.tools) > 0 || r.tool != nil {
				a.processBlock(c, r)
			}
			// A permission ask parks its card here while the engine waits.
			if a.pending != nil && a.pending.rowID == r.id {
				a.approvalCard(c)
			}
			ans, _ := r.answerText()
			switch {
			case r.streaming:
				chat.StreamingText(c, ans, chat.StreamingTextOptions{Streaming: ans != ""})
			case ans != "":
				mdView(c, ans) // selectable markdown once the reply is done
			}
			if r.note != "" {
				ui.Text(c, r.note).FontSize(11).TextColor(tokens(c).TextMuted).MaxLines(2)
			}
			a.actions(c, r)
		})
	default:
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Crux"}, func() {
			if r.text != "" {
				// User text stays literal: one selectable element, no
				// markdown interpretation of what the user typed.
				ui.RichText(c, ui.Span{Text: r.text}).FontSize(14).LineHeight(1.6).Selectable()
			}
			a.actions(c, r)
		})
	}
}

// processBlock is the reply's process area, laid out the way DSH renders an
// agent turn: a one-line group header that sums the work, and — once opened —
// the timeline of reasoning segments and tool invocations as rows in the
// message flow, in the order they happened. procOpen is true while the reply
// streams (the activity is visible) and flips closed by endStreamingRow when
// it lands; each step's expanded body caps its own height.
func (a *app) processBlock(c *ui.Context, r *row) {
	data.Collapsible(c, &r.procOpen, data.CollapsibleOptions{Trigger: func() {
		a.procHeader(c, r)
	}}, func() {
		a.processSteps(c, r)
	})
}

// processSteps renders the reply's timeline: reasoning segments, interim
// messages and tool invocations as rows in the message flow in the order
// they happened (DSH's group members). The LAST message segment is the
// answer and renders in the reply's bubble instead.
func (a *app) processSteps(c *ui.Context, r *row) {
	_, ans := r.answerText()
	ui.Column(c).Gap(4).Children(func() {
		steps := r.timeline() // materializes legacy rows once
		for i := range steps {
			switch {
			case i == ans:
				// the answer segment renders in the bubble below
			case steps[i].tool != nil:
				a.toolStepRow(c, r, i)
			case steps[i].text != "":
				// an interim message the model wrote after seeing tool
				// results: visible text (markdown) between the tool rows.
				mdView(c, steps[i].text)
			default:
				a.thinkStepRow(c, r, i)
			}
		}
	})
}

// procHeader is the group header's one line: a spinner and the live
// activity while the reply streams, the top activity's mark and the summary
// once it lands, with the elapsed time at the end (none for old transcripts
// that never pinned a doneAt).
func (a *app) procHeader(c *ui.Context, r *row) {
	k := tokens(c)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		dur, showTime := time.Duration(0), false
		if r.streaming {
			dur = max(c.Now().Sub(r.at), 0)
			c.After(time.Second - dur%time.Second) // keep the live timer ticking
			showTime = true
			core.Spinner(c, core.SpinnerOptions{Size: 14})
		} else {
			ui.Icon(c, icons.Must(procIcon(r))).Size(14, 14).TextColor(k.TextMuted)
			if r.doneAt.After(r.at) {
				dur, showTime = r.doneAt.Sub(r.at), true
			}
		}
		ui.Text(c, procSummary(r)).FontSize(12).TextColor(k.TextMuted).SingleLine().Grow(1)
		if showTime {
			ui.Text(c, agent.AgentDuration(dur)).FontSize(11).TextColor(k.TextMuted).FontFeatures("tnum")
		}
	})
}

// procIcon picks the header's activity mark, DSH's PROCESS_ICONS[top]: the
// running tool's kind while the reply streams, else the first tool kind,
// else the brain.
func procIcon(r *row) string {
	runs := r.toolsOf()
	if r.streaming {
		for i := len(runs) - 1; i >= 0; i-- {
			if runs[i].state == agent.AgentRunning {
				return toolIcon(runs[i].name)
			}
		}
		return "brain"
	}
	if len(runs) > 0 {
		return toolIcon(runs[0].name)
	}
	return "brain"
}

// procSummary is the header's activity line: what runs right now while the
// reply streams ("Running bash…", else "Thinking…"), and once it lands what
// the agent did — the top three tool kinds, DSH-style, without counts.
func procSummary(r *row) string {
	runs := r.tools
	if r.tool != nil {
		runs = append([]*toolRun{r.tool}, runs...) // legacy stored rows
	}
	if r.streaming {
		for i := len(runs) - 1; i >= 0; i-- {
			if runs[i].state == agent.AgentRunning {
				return "Running " + runs[i].name + "…"
			}
		}
		return "Thinking…"
	}
	var kinds []string
	seen := map[string]bool{}
	for _, t := range runs {
		if seen[t.name] {
			continue
		}
		seen[t.name] = true
		kinds = append(kinds, activityWord(t.name))
	}
	s := joinActivities(kinds)
	if s == "" {
		return "Thought"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// activityWord names what one tool kind did.
func activityWord(name string) string {
	switch name {
	case "bash":
		return "ran commands"
	case "write_file":
		return "edited files"
	case "read_file":
		return "read files"
	}
	return "called " + name
}

// joinActivities composes up to three activities into one line, the way
// DSH's process title reads ("Ran commands and edited files…").
func joinActivities(ws []string) string {
	switch len(ws) {
	case 0:
		return ""
	case 1:
		return ws[0]
	case 2:
		return ws[0] + " and " + ws[1]
	}
	s := ws[0] + ", " + ws[1] + " and " + ws[2]
	if len(ws) > 3 {
		s += "…"
	}
	return s
}

// thinkStepRow is one reasoning segment of the timeline: a one-line
// disclosure whose summary is the segment's first line (the live line while
// it streams, DSH's reasoning preview), expanding to the full text in a
// capped scroller.
func (a *app) thinkStepRow(c *ui.Context, r *row, i int) {
	st := &r.timeline()[i]
	if st.think == "" {
		return
	}
	k := tokens(c)
	data.Collapsible(c, &st.open, data.CollapsibleOptions{Trigger: func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icons.Must("brain")).Size(14, 14).TextColor(k.TextMuted)
			ui.Text(c, "Thinking").FontSize(12).FontWeight(500).TextColor(k.TextMuted).SingleLine()
			ui.Text(c, firstLine(st.think)).FontSize(12).TextColor(k.TextMuted).SingleLine().Grow(1)
		})
	}}, func() {
		ui.Scroll(c).Shrink(1).MaxHeight(280).Children(func() {
			ui.Text(c, st.think).Selectable()
		})
	})
}

// toolStepRow is one tool invocation of the timeline: a one-line disclosure
// led by its state mark (check / cross / spinner — success reads at a
// glance), the tool kind's mark and an args summary, expanding to the
// specialized card.
func (a *app) toolStepRow(c *ui.Context, r *row, i int) {
	t := r.timeline()[i].tool
	k := tokens(c)
	data.Collapsible(c, &r.steps[i].open, data.CollapsibleOptions{Trigger: func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			switch t.state {
			case agent.AgentRunning:
				core.Spinner(c, core.SpinnerOptions{Size: 12})
			case agent.AgentFailed:
				ui.Icon(c, icons.Must("circle-x")).Size(14, 14).TextColor(k.Danger)
			default:
				ui.Icon(c, icons.Must("circle-check")).Size(14, 14).TextColor(k.Success)
			}
			ui.Icon(c, icons.Must(toolIcon(t.name))).Size(14, 14).TextColor(k.TextMuted)
			ui.Text(c, toolTitle(t.name)).FontSize(12).FontWeight(500).SingleLine()
			ui.Text(c, toolSummary(t)).FontSize(12).TextColor(k.TextMuted).SingleLine().Grow(1)
			if t.dur > 0 {
				ui.Text(c, agent.AgentDuration(t.dur)).FontSize(11).TextColor(k.TextMuted).FontFeatures("tnum")
			}
			if t.state == agent.AgentFailed {
				ui.Text(c, "failed").FontSize(11).TextColor(k.Danger).SingleLine()
			}
		})
	}}, func() {
		a.toolCard(c, t)
	})
}

// toolCard renders one invocation's specialized card: bash as a terminal
// card (its stop button aborts the loop), write_file as a before/after
// review card, everything else as a tool-call card.
func (a *app) toolCard(c *ui.Context, t *toolRun) {
	switch t.name {
	case "bash":
		// Changed requests an abort: stop the in-flight agent loop.
		if agent.CommandExecutionCard(c, t.run, agent.CommandExecutionCardOptions{}).Changed() {
			a.cancel()
		}
	case "write_file":
		// The write already happened; the card is the review record.
		agent.FileChangeCard(c, &t.decision, t.change, agent.FileChangeCardOptions{Preview: 4})
	default:
		agent.ToolCallCard(c, agent.ToolCall{
			ID: t.callID, Name: t.name, Args: t.args, Result: t.result,
			Error: t.errMsg, State: t.state, Duration: t.dur,
		}, agent.ToolCallCardOptions{})
	}
}

// toolIcon maps a tool kind to its timeline mark (DSH's per-tool rows).
// Names verified against icons/svg: terminal, pencil, file-text, bot.
func toolIcon(name string) string {
	switch name {
	case "bash":
		return "terminal"
	case "write_file":
		return "pencil"
	case "read_file":
		return "file-text"
	}
	return "bot"
}

// toolTitle names the row in what-it-did words.
func toolTitle(name string) string {
	switch name {
	case "bash":
		return "Ran command"
	case "write_file":
		return "Wrote file"
	case "read_file":
		return "Read file"
	}
	return name
}

// toolSummary derives the collapsed row's one-line summary: the command for
// bash, the path argument for file tools, the raw args otherwise.
func toolSummary(t *toolRun) string {
	if t.name == "bash" && t.run.Command != "" {
		return firstLine(t.run.Command)
	}
	var p struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(t.args), &p) == nil && p.Path != "" {
		return p.Path
	}
	return firstLine(t.args)
}

// firstLine clips one line of text for collapsed summaries.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// actions is the quiet row under every message: copy for all messages,
// regenerate for assistant replies, and the message time. The library's
// MessageActions toolbar also ships like/dislike/speak/share/edit, which
// Crux does not use — these two buttons are the whole set.
func (a *app) actions(c *ui.Context, r *row) {
	k := tokens(c)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if msgIconBtn(c, k, "copy", "Copy") {
			c.WriteClipboard(r.text)
			c.Toast("Copied to the clipboard")
		}
		if r.role == chat.MessageAssistant && msgIconBtn(c, k, "refresh-cw", "Regenerate") {
			if msg := a.regenerate(r); msg != "" {
				c.Toast(msg)
			}
		}
		ui.Text(c, r.at.Format("15:04")).FontSize(11).TextColor(k.TextMuted).SingleLine()
	})
}

// msgIconBtn is a small quiet icon button; it reports its click.
func msgIconBtn(c *ui.Context, k tokensT, icon, label string) bool {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Size(22, 22).Radius(5).Center().Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(k.SurfaceHover)
	}
	clicked := b.Clicked()
	b.Children(func() {
		ui.Icon(c, icons.Must(icon)).FontSize(12).TextColor(k.TextMuted)
	})
	return clicked
}

// regenerate re-runs the user prompt that precedes an assistant reply:
// the reply and everything after it are dropped, a fresh streaming reply
// row is appended, and the agent loop starts over with the same history.
// It returns a toast message, empty when the regenerate started.
func (a *app) regenerate(r *row) string {
	if a.llm.Busy {
		return "Wait for the current reply to finish"
	}
	idx := slices.IndexFunc(a.thread.rows, func(x row) bool { return x.id == r.id })
	if idx <= 0 || a.thread.rows[idx-1].role != chat.MessageUser {
		return "Nothing to regenerate"
	}
	a.thread.rows = a.thread.rows[:idx]
	// A unique id so the fresh row gets fresh element state (the md parse
	// cache rides the row element).
	a.regenSeq++
	id := fmt.Sprintf("a%d-r%d", idx, a.regenSeq)
	a.thread.rows = append(a.thread.rows, row{
		id: id, role: chat.MessageAssistant, kind: rowReasoned,
		at: time.Now(), procOpen: true, streaming: true,
	})
	a.thread.list.ScrollToEnd()
	a.markDirty(a.sessionID)
	a.persistSessions()
	a.startStream(idx-1, id)
	return ""
}

// composer is the thread's input area: context chips, the prompt editor
// with a send button, then the model and a context meter. The chat/agent
// mode picker is gone — Crux is an agent console, the mode stays Agent.
func (a *app) composer(c *ui.Context) {
	ui.Column(c).Gap(8).Children(func() {
		if id, ok := chat.ContextChips(c, a.thread.ctx, chat.ContextChipsOptions{Max: 2}).Removed(); ok {
			a.thread.ctx = slices.DeleteFunc(a.thread.ctx, func(it chat.ContextItem) bool { return it.ID == id })
		}
		p := chat.PromptComposer(c, &a.thread.draft, chat.PromptComposerOptions{
			Placeholder: "Direct Crux — it can read files, run commands and edit.",
			Actions: func() {
				if chat.SendButton(c, chat.SendButtonOptions{
					Shortcut: "Enter", Disabled: strings.TrimSpace(a.thread.draft) == "",
				}).Sent() {
					a.send()
				}
			},
		})
		if p.Submitted() {
			a.send()
		}
		ui.Row(c).Gap(14).AlignItems(ui.Center).Wrap().Children(func() {
			// The permission switch: three modes, the composer owns the
			// choice (spec §2). Frozen while a reply streams.
			modeIdx := int(engine.ParseMode(a.llm.Mode))
			if seg := ui.Segmented(c, &modeIdx, "Read-only", "Agent", "Full access").Label("Permissions"); seg.Changed() && !a.llm.Busy {
				a.llm.Mode = engine.Mode(modeIdx).String()
			}
			// The active backend, set on the Agent settings page (⌘K → Agent).
			ui.Text(c, a.backendLabel()).FontSize(12).TextColor(tokens(c).TextMuted).SingleLine()
			chat.TokenCounter(c, 6400, 8000)
		})
	})
}

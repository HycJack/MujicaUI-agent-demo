package app

// thread.go renders the Crux conversation: the scrolling transcript in
// the middle, a composer below it, and the agent cards that a Codex-style
// assistant drops into the flow (thinking, tool calls, file changes,
// terminal runs and a multi-file diff). This mirrors the dashboard's
// agent thread but as the app's working surface.

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"crux-agent/internal/engine"
	"github.com/HycJack/MujicaUI/agent"
	"github.com/HycJack/MujicaUI/chat"
	"github.com/HycJack/MujicaUI/icons"
	"github.com/HycJack/MujicaUI/input"
	"github.com/HycJack/MujicaUI/overlay"
	"github.com/HycJack/MujicaUI/theme"
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
				Label: func(i int) string { return mdPlain(a.thread.rows[i].text) },
			},
			Input: func() { a.composer(c) },
		}, func(i int) { a.renderRow(c, &a.thread.rows[i]) })
	})
}

// renderRow paints one transcript row by kind. One AI reply is one bubble:
// its thinking and tool calls fold into a collapsible ThinkingBlock, and
// the text renders as markdown once the reply completes. The standalone
// tool-row kinds only remain for transcripts stored before the merge.
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
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Crux"}, func() {
			// Waiting for the model's first token: show the typing dots so
			// the reply never sits as an empty bubble.
			if r.streaming && r.text == "" && r.thinkText == "" && len(r.tools) == 0 {
				chat.TypingIndicator(c, "Crux")
			}
			if r.thinkText != "" || len(r.tools) > 0 {
				chat.ThinkingBlock(c, &r.thinkOpen, chat.ThinkingBlockOptions{
					Thinking: r.streaming && r.text == "",
					Started:  r.at,
				}, func() {
					ui.Column(c).Gap(8).Children(func() {
						if r.thinkText != "" {
							ui.Text(c, r.thinkText).Selectable()
						}
						a.toolCards(c, r)
					})
				})
			}
			switch {
			case r.streaming:
				// Streaming markdown: the same selectable renderer as the
				// finished reply; the parse re-runs as the text grows.
				mdView(c, r.text)
			case r.text != "":
				mdView(c, r.text) // selectable markdown once the reply is done
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

// toolCards renders a reply's tool invocations in order: bash as a terminal
// card (its stop button aborts the loop), write_file as a before/after
// review card, everything else as a tool-call card.
func (a *app) toolCards(c *ui.Context, r *row) {
	runs := r.tools
	if r.tool != nil {
		runs = append([]*toolRun{r.tool}, runs...) // legacy stored rows
	}
	for _, t := range runs {
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
		at: time.Now(), streaming: true,
	})
	a.thread.list.ScrollToEnd()
	a.markDirty(a.sessionID)
	a.persistSessions()
	a.startStream(idx-1, id)
	return ""
}

// composer is the thread's input area: one card holds the frameless editor
// and, inside it along the bottom edge, the toolbar — attach, model and
// thinking pickers on the left, the token counter and the send button on
// the right. Enter sends; the chat/agent mode picker is gone — Crux is an
// agent console, the mode stays Agent.
func (a *app) composer(c *ui.Context) {
	k := tokens(c)
	ui.Column(c).Gap(8).Children(func() {
		if id, ok := chat.ContextChips(c, a.thread.ctx, chat.ContextChipsOptions{Max: 2}).Removed(); ok {
			a.thread.ctx = slices.DeleteFunc(a.thread.ctx, func(it chat.ContextItem) bool { return it.ID == id })
		}
		ui.Column(c).MinWidth(0).Background(k.Background).Border(1, k.ControlBorder).
			Radius(theme.CardRadius).Padding(6).Gap(2).Children(func() {
			ta := input.TextArea(c, &a.thread.draft, input.TextAreaOptions{
				Placeholder: "Direct Crux — it can read files, run commands and edit.",
				Label:       "Message", MinHeight: 44, MaxHeight: 180,
				SubmitKey: ui.KeyEnter, Frameless: true,
			})
			if ta.Submitted() {
				a.send()
			}
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				a.composerAttach(c, k)
				a.composerModel(c, k)
				a.composerThinking(c, k)
				ui.Spacer(c)
				chat.TokenCounter(c, 6400, 8000)
				if chat.SendButton(c, chat.SendButtonOptions{
					Shortcut: "Enter", Disabled: strings.TrimSpace(a.thread.draft) == "",
				}).Sent() {
					a.send()
				}
			})
		})
	})
}

// composerAttach is the toolbar's paperclip: it opens the attach-file
// dialog over the workspace.
func (a *app) composerAttach(c *ui.Context, k tokensT) {
	b := ui.ButtonBase(c).Label("Attach files").Tooltip("Attach files").Size(28, 28).Radius(theme.ControlRadius).Center().Cursor(ui.CursorPointer)
	if b.Hovered() {
		b.Background(k.SurfaceHover)
	}
	if b.Clicked() {
		a.openAttachDialog()
	}
	b.Children(func() {
		ui.Icon(c, icons.Must("paperclip")).FontSize(15).TextColor(k.TextMuted)
	})
}

// composerModel is the toolbar's model picker: the active model's short
// name opens a menu of the provider's models. No model is chosen yet —
// the trigger says so and the menu is still there to pick from.
func (a *app) composerModel(c *ui.Context, k tokensT) {
	label := a.llm.Model
	if m, err := engine.ModelInfoOf(a.llm.Provider, a.llm.Model); err == nil && m.Name != "" {
		label = m.Name
	}
	if strings.TrimSpace(label) == "" {
		label = "Select a model"
	}
	opts := a.modelOptions()
	overlay.DropdownMenu(c, label, overlay.DropdownMenuOptions{Icon: icons.Must("bot"), Label: "Model"}, func(m *overlay.PopupMenu) {
		for _, o := range opts {
			on := o.Value == a.llm.Model
			if m.Check(o.Label, &on, overlay.PopupMenuItemOptions{}) && on {
				a.setModel(o.Value)
			}
		}
	})
}

// setModel switches the backend's model and re-applies its defaults:
// a model that cannot reason drops the tier, one that can lifts a
// switched-off tier back to the model's default. Also the test surface
// for the picker.
func (a *app) setModel(id string) {
	a.llm.Model = id
	m, err := engine.ModelInfoOf(a.llm.Provider, id)
	if err != nil {
		return
	}
	if !m.Reasoning {
		a.llm.Thinking = "none"
		return
	}
	if a.llm.Thinking == "none" || a.llm.Thinking == "" {
		a.llm.Thinking = engine.ThinkDefault(a.llm.Provider, id)
	}
}

// composerThinking is the toolbar's reasoning picker, shown only for
// models that reason.
func (a *app) composerThinking(c *ui.Context, k tokensT) {
	if m, err := engine.ModelInfoOf(a.llm.Provider, a.llm.Model); err != nil || !m.Reasoning {
		return
	}
	overlay.DropdownMenu(c, thinkingLabel(a.llm.Thinking), overlay.DropdownMenuOptions{Icon: icons.Must("brain"), Label: "Thinking"}, func(m *overlay.PopupMenu) {
		for _, t := range reasoningTiers {
			on := t.Value == a.llm.Thinking
			if m.Check(t.Label, &on, overlay.PopupMenuItemOptions{}) && on {
				a.setThinking(t.Value)
			}
		}
	})
}

// setThinking sets the reasoning tier. Also the test surface for the picker.
func (a *app) setThinking(v string) { a.llm.Thinking = v }

// thinkingLabel names a thinking tier for the picker.
func thinkingLabel(v string) string {
	for _, t := range reasoningTiers {
		if t.Value == v {
			return t.Label
		}
	}
	return v
}

package main

// thread.go renders the Atlas conversation: the scrolling transcript in
// the middle, a composer below it, and the agent cards that a Codex-style
// assistant drops into the flow (thinking, tool calls, file changes,
// terminal runs and a multi-file diff). This mirrors the dashboard's
// agent thread but as the app's working surface.

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ZacharyZhang-NY/MujicaUI/agent"
	"github.com/ZacharyZhang-NY/MujicaUI/chat"
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
// its thinking and tool calls fold into a collapsible ThinkingBlock, and
// the text renders as markdown once the reply completes. The standalone
// tool-row kinds only remain for transcripts stored before the merge.
func (a *app) renderRow(c *ui.Context, r *row) {
	switch r.kind {
	case rowTyping:
		chat.TypingIndicator(c, "Atlas")
	case rowTools:
		t := r.tool
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Atlas"}, func() {
			agent.ToolCallCard(c, agent.ToolCall{
				ID: t.callID, Name: t.name, Args: t.args, Result: t.result,
				Error: t.errMsg, State: t.state, Duration: t.dur,
			}, agent.ToolCallCardOptions{})
		})
	case rowCommand:
		t := r.tool
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Atlas"}, func() {
			// Changed requests an abort: stop the in-flight agent loop.
			if agent.CommandExecutionCard(c, t.run, agent.CommandExecutionCardOptions{}).Changed() {
				a.cancel()
			}
		})
	case rowDiff:
		t := r.tool
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Atlas"}, func() {
			// The write already happened; the card is the review record.
			agent.FileChangeCard(c, &t.decision, t.change, agent.FileChangeCardOptions{Preview: 4})
		})
	case rowReasoned:
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Atlas"}, func() {
			// Waiting for the model's first token: show the typing dots so
			// the reply never sits as an empty bubble.
			if r.streaming && r.text == "" && r.thinkText == "" && len(r.tools) == 0 {
				chat.TypingIndicator(c, "Atlas")
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
				chat.StreamingText(c, r.text, chat.StreamingTextOptions{Streaming: r.text != ""})
			case r.text != "":
				mdView(c, r.text) // selectable markdown once the reply is done
			}
			a.actions(c, r)
		})
	default:
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Atlas"}, func() {
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
// Atlas does not use — these two buttons are the whole set.
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

// composer is the thread's input area: context chips, the prompt editor
// with a send button, then the model and a context meter. The chat/agent
// mode picker is gone — Atlas is an agent console, the mode stays Agent.
func (a *app) composer(c *ui.Context) {
	ui.Column(c).Gap(8).Children(func() {
		if id, ok := chat.ContextChips(c, a.thread.ctx, chat.ContextChipsOptions{Max: 2}).Removed(); ok {
			a.thread.ctx = slices.DeleteFunc(a.thread.ctx, func(it chat.ContextItem) bool { return it.ID == id })
		}
		p := chat.PromptComposer(c, &a.thread.draft, chat.PromptComposerOptions{
			Placeholder: "Direct Atlas — it can read files, run commands and edit.",
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
			// The active backend, set on the Agent settings page (⌘K → Agent).
			ui.Text(c, a.backendLabel()).FontSize(12).TextColor(tokens(c).TextMuted).SingleLine()
			chat.TokenCounter(c, 6400, 8000)
		})
	})
}

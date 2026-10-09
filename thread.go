package main

// thread.go renders the Atlas conversation: the scrolling transcript in
// the middle, a composer below it, and the agent cards that a Codex-style
// assistant drops into the flow (thinking, tool calls, file changes,
// terminal runs and a multi-file diff). This mirrors the dashboard's
// agent thread but as the app's working surface.

import (
	"slices"
	"strings"
	"time"

	"github.com/ZacharyZhang-NY/MujicaUI/agent"
	"github.com/ZacharyZhang-NY/MujicaUI/chat"
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

// renderRow paints one transcript row by kind. The tool rows render the
// real agent invocation stored on the row; the assistant rows render the
// streamed thinking and text.
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
			if r.thinkText != "" {
				chat.ThinkingBlock(c, &a.thread.think, chat.ThinkingBlockOptions{}, func() {
					ui.Text(c, r.thinkText)
				})
			}
			chat.StreamingText(c, r.text, chat.StreamingTextOptions{Streaming: a.llm.Busy && r.text != ""})
			a.actions(c, r)
		})
	default:
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Atlas"}, func() {
			if r.text != "" {
				chat.MarkdownView(c, r.text)
			}
			a.actions(c, r)
		})
	}
}

// actions is the copy/regenerate/feedback row assistant messages carry.
func (a *app) actions(c *ui.Context, r *row) {
	if r.role != chat.MessageAssistant {
		return
	}
	res := chat.MessageActions(c, r.role, chat.MessageActionsOptions{Feedback: &a.thread.rating})
	if act, ok := res.Action(); ok {
		switch act {
		case chat.ActionCopy:
			c.WriteClipboard(r.text)
			c.Toast("Copied to the clipboard")
		case chat.ActionRegenerate:
			c.Toast("Regenerating…")
		}
	}
}

// composer is the thread's input area: context chips, the prompt editor
// with a mode picker and send button, then the model and a context meter.
func (a *app) composer(c *ui.Context) {
	ui.Column(c).Gap(8).Children(func() {
		if id, ok := chat.ContextChips(c, a.thread.ctx, chat.ContextChipsOptions{Max: 2}).Removed(); ok {
			a.thread.ctx = slices.DeleteFunc(a.thread.ctx, func(it chat.ContextItem) bool { return it.ID == id })
		}
		p := chat.PromptComposer(c, &a.thread.draft, chat.PromptComposerOptions{
			Placeholder: "Direct Atlas — it can read files, run commands and edit.",
			Tools:       func() { chat.ModeSelector(c, &a.thread.mode, chat.ModeSelectorOptions{}) },
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

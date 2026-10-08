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

// filesChanged seeds the last tool row's diff set so the review has
// something to leaf through.
var filesChanged = []agent.FileChange{
	{Path: "jobs/export-nightly.sh", Old: "build dump\nupload dump\n", New: "build dump\nprune yesterday\nupload dump\n"},
	{Path: "jobs/quota.go", Old: "cap = 7\n", New: "cap = 14\n"},
	{Path: "docs/runbook.md", Old: "The bucket keeps 7 days.\n"},
}

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

// renderRow paints one transcript row by kind.
func (a *app) renderRow(c *ui.Context, r *row) {
	switch r.kind {
	case rowTyping:
		chat.TypingIndicator(c, "Atlas")
	case rowTools:
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Atlas"}, func() {
			agent.ToolCallCard(c, agent.ToolCall{
				ID: "call-1", Name: "read_file",
				Args:   `{"path":"var/log/export-nightly.log","tail":40}`,
				Result: `{"lines":40,"error":"quota exceeded"}`,
				State:  agent.AgentDone, Duration: 420 * time.Millisecond,
			}, agent.ToolCallCardOptions{})
			agent.FileChangeCard(c, &a.thread.plan, agent.FileChange{
				Path: "jobs/export-nightly.sh",
				Old:  "build dump\nupload dump\n",
				New:  "build dump\nprune yesterday\nupload dump\n",
			}, agent.FileChangeCardOptions{Preview: 4})
		})
	case rowCommand:
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Atlas"}, func() {
			if agent.CommandExecutionCard(c, a.thread.cmd, agent.CommandExecutionCardOptions{}).Changed() {
				a.thread.cmd.Running, a.thread.cmd.ExitCode = false, 130
				a.thread.cmd.Output += "\x1b[31maborted by user\x1b[0m\n"
			}
		})
	case rowDiff:
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Atlas"}, func() {
			agent.MultiFileDiffReview(c, &a.thread.diff, filesChanged, agent.MultiFileDiffReviewOptions{})
		})
	case rowReasoned:
		chat.MessageBubble(c, r.role, chat.MessageBubbleOptions{Name: "Atlas"}, func() {
			if r.thinkText != "" {
				chat.ThinkingBlock(c, &a.thread.think, chat.ThinkingBlockOptions{Elapsed: 6 * time.Second}, func() {
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

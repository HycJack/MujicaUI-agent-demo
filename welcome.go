package main

// welcome.go is the new-chat surface: MujicaUI's WelcomeScreen with
// capability cards and starter chips that feed the composer below.

import (
	"github.com/ZacharyZhang-NY/MujicaUI/chat"
	"github.com/ZacharyZhang-NY/MujicaUI/icons"
	"github.com/egoist/mygo/ui"
)

// welcome fills the working pane before the first message: the greeting,
// capability cards and starter prompts, over the composer.
func (a *app) welcome(c *ui.Context, k tokensT) {
	ui.Column(c).Fill().Children(func() {
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(28).Children(func() {
			v := chat.WelcomeScreen(c, chat.WelcomeScreenOptions{
				Greeting: "Good evening",
				Subtitle: "Atlas is caught up on the repo. What should it look at next?",
				Cards: []chat.CapabilityCard{
					{Title: "Investigate", Description: "Logs, traces and metrics.", Icon: icons.Must("search"),
						Examples: []string{"Why did checkout latency spike?", "Which job failed last?"}},
					{Title: "Change code", Description: "Patches, diffs and tests.", Icon: icons.Must("pencil"),
						Examples: []string{"Add retries to the exporter", "Raise the archive cap"}},
				},
				Prompts: func() {
					chips := chat.SuggestionChips(c, &a.thread.draft,
						[]string{"Summarize last night's failures", "Draft the incident report"},
						chat.SuggestionChipsOptions{Label: "Starters", Send: true})
					if chips.Sent && chips.Chosen != "" {
						a.thread.draft = chips.Chosen
						a.send()
					}
				},
			})
			// A picked capability example lands in the draft, ready to send.
			if v.Example != "" {
				a.thread.draft = v.Example
			}
		})
		ui.Column(c).Padding(12).Children(func() { a.composer(c) })
	})
}

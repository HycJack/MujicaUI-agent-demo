package app

// welcome.go is the new-chat surface: the sticker/arcade hero — Bungee
// greeting, pixel-caps subtitle, pixel-sprite confetti — over two neo
// capability cards whose examples feed the composer below.

import (
	"github.com/HycJack/MujicaUI/chat"
	"github.com/HycJack/MujicaUI/icons"
	"github.com/egoist/mygo/ui"
)

// welcome fills the working pane before the first message: the hero and
// capability cards, over the composer.
func (a *app) welcome(c *ui.Context, k tokensT) {
	ui.Column(c).Fill().Children(func() {
		ui.Scroll(c).Grow(1).MinHeight(0).Padding(28).Children(func() {
			ui.Column(c).FillWidth().Gap(18).Children(func() {
				a.welcomeHero(c, k)
				a.welcomeCards(c, k)
				ui.Spacer(c).Height(6)
				chips := chat.SuggestionChips(c, &a.thread.draft,
					[]string{"Summarize last night's failures", "Draft the incident report"},
					chat.SuggestionChipsOptions{Label: "Starters", Send: true})
				if chips.Sent && chips.Chosen != "" {
					a.thread.draft = chips.Chosen
					a.send()
				}
			})
		})
		ui.Column(c).Padding(12).Children(func() { a.composer(c) })
	})
}

// welcomeHero is the greeting: the state badge and the dashed sticker
// tag up top, the fat display voice for the salutation, the pixel voice
// for the subtitle, and the sprite confetti around them.
func (a *app) welcomeHero(c *ui.Context, k tokensT) {
	ui.Column(c).FillWidth().Gap(10).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			neoStatusDot(c, k, "coding agent", k.Accent)
			neoTag(c, k, "v0.3 ✦ sticker edition", k.Ornament, -3)
		})
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, neoPixelSparkle).FontSize(14).TextColor(k.Ornament)
			ui.Icon(c, neoPixelCloud).FontSize(18).TextColor(k.Accent)
			ui.Icon(c, neoPixelHeart).FontSize(12).TextColor(k.Danger)
			ui.Icon(c, neoPixelSparkle).FontSize(10).TextColor(k.Success)
		})
		ui.Text(c, "Good evening").Font(fontDisplay).FontSize(36).TextColor(k.Text)
		ui.Text(c, "CRUX IS CAUGHT UP ON THE REPO — WHAT SHOULD IT LOOK AT NEXT?").
			Font(fontPixel).FontSize(8).TextColor(k.TextMuted)
		neoStatCard(c, k, StatVM{
			Title:  "context",
			Badge:  "healthy",
			Big:    "6.4K",
			Unit:   "of 8K tokens",
			Filled: 8, Total: 10,
			Note: "The window before compaction kicks in.",
		})
	})
}

// welcomeCard is one capability sticker: a neo card with the icon on an
// amber chip, the title, the blurb, then its example prompts as clickable
// rows that load the composer's draft.
func (a *app) welcomeCard(c *ui.Context, k tokensT, icon, title, blurb string, examples []string) {
	neoCard(c, k, k.Surface, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			neoChipBuild(c, k, k.Ornament, func() {
				ui.Icon(c, icons.Must(icon)).FontSize(13).TextColor(k.Text)
			})
			ui.Text(c, title).FontSize(14).Bold().TextColor(k.Text)
		})
		ui.Text(c, blurb).FontSize(11.5).TextColor(k.TextMuted)
		ui.Column(c).Gap(2).Children(func() {
			for _, ex := range examples {
				row := ui.Row(c).Padding(3, 6).Gap(6).AlignItems(ui.Center).
					Radius(neoRadiusChip).Cursor(ui.CursorPointer)
				if row.Hovered() {
					row.Background(k.SurfaceHover)
				}
				picked := ex
				if row.Clicked() {
					a.thread.draft = picked
				}
				row.Children(func() {
					ui.Text(c, "▸").FontSize(11).TextColor(k.Accent)
					ui.Text(c, picked).FontSize(11.5).TextColor(k.Text)
				})
			}
		})
	})
}

// welcomeCards lays the capability cards side by side; each grows to an
// equal share of the row's definite width.
func (a *app) welcomeCards(c *ui.Context, k tokensT) {
	ui.Row(c).FillWidth().Gap(14).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			a.welcomeCard(c, k, "search", "Investigate", "Logs, traces and metrics.", []string{
				"Why did checkout latency spike?",
				"Which job failed last?",
			})
		})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			a.welcomeCard(c, k, "pencil", "Change code", "Patches, diffs and tests.", []string{
				"Add retries to the exporter",
				"Raise the archive cap",
			})
		})
	})
}

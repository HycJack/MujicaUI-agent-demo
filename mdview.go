package main

// mdview.go renders parsed markdown as SELECTABLE native text: every
// prose run, list item, heading, quote and table cell is a selectable
// element, so drag-select and copy work on reply content (MujicaUI's
// MarkdownView builds unselectable elements and hides its parser behind
// an internal package, so Crux carries this adapted renderer — the
// block dispatch follows the mygo-agent reference renderer).

import (
	"strings"

	"crux-agent/internal/md"
	"github.com/ZacharyZhang-NY/MujicaUI/chat"
	"github.com/ZacharyZhang-NY/MujicaUI/theme"
	"github.com/egoist/mygo/ui"
)

// mdParsed is one row's cached parse: the source it was built from and
// its blocks. ui.Local keeps it across frames; a changed src re-parses.
type mdParsed struct {
	src    string
	blocks []md.Block
}

// mdView renders src as selectable markdown. Blocks parse once per
// element identity and rebuild as native elements each frame.
func mdView(c *ui.Context, src string) {
	k := tokens(c)
	root := ui.Column(c).Gap(6).MinWidth(0)
	st := ui.Local(root, "md", func() mdParsed { return mdParsed{src: src, blocks: md.Parse(src)} })
	if st.src != src {
		*st = mdParsed{src: src, blocks: md.Parse(src)}
	}
	root.Children(func() {
		for i := range st.blocks {
			ui.Column(c).Key(i).MinWidth(0).Children(func() {
				mdBlockEl(c, &st.blocks[i], k)
			})
		}
	})
}

// mdBlockEl renders one block.
func mdBlockEl(c *ui.Context, b *md.Block, k tokensT) {
	switch b.Kind {
	case md.KindHeading:
		size := []float32{17, 15.5, 14}[min(b.Level, 3)-1]
		ui.Text(c, b.Text).FontSize(size).Bold().Selectable()
	case md.KindPara:
		mdProse(c, strings.Split(b.Text, "\n"), k, nil)
	case md.KindList:
		ui.Column(c).Gap(4).Children(func() {
			for _, it := range b.Items {
				mark := "•"
				if it.Num != "" {
					mark = it.Num + "."
				}
				ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
					ui.Text(c, mark).FontSize(14).TextColor(k.TextMuted).MinWidth(14).Shrink(0)
					mdProse(c, []string{it.Text}, k, func(e *ui.Element) { e.Grow(1).MinWidth(0) })
				})
			}
		})
	case md.KindQuote:
		ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
			ui.Box(c).Width(2).Background(k.Border)
			ui.Column(c).Gap(4).Grow(1).MinWidth(0).Children(func() {
				for _, q := range strings.Split(b.Text, "\n") {
					mdProse(c, []string{q}, k, func(e *ui.Element) { e.TextColor(k.TextMuted) })
				}
			})
		})
	case md.KindTable:
		mdTableEl(c, b, k)
	case md.KindCode:
		opts := chat.CodeBlockOptions{Language: b.Lang}
		chat.CodeBlock(c, strings.TrimRight(b.Text, "\n"), opts)
	case md.KindRule:
		ui.Divider(c)
	}
}

// mdTableEl renders a table as a grid: a bold header over a rule, body
// rows with hairline separators, selectable cells.
func mdTableEl(c *ui.Context, b *md.Block, k tokensT) {
	n := len(b.Header)
	for _, r := range b.Rows {
		if len(r) > n {
			n = len(r)
		}
	}
	if n == 0 {
		return
	}
	grid := ui.Grid(c).Columns(n).GapX(14).GapY(0)
	grid.Children(func() {
		total := len(b.Rows)
		for ri := -1; ri < total; ri++ {
			last := ri == total-1
			for ci := 0; ci < n; ci++ {
				text := ""
				if ri < 0 {
					if ci < len(b.Header) {
						text = b.Header[ci]
					}
				} else if ci < len(b.Rows[ri]) {
					text = b.Rows[ri][ci]
				}
				cell := ui.Column(c).Padding(5, 2).MinWidth(0)
				if ri < 0 {
					cell.Padding(2, 2, 6, 2).BorderWidth(0, 0, 1, 0).BorderColor(k.Border)
				} else if !last {
					cell.BorderWidth(0, 0, 1, 0).BorderColor(k.Border)
				}
				weight := 400
				if ri < 0 {
					weight = 600
				}
				cell.Children(func() {
					mdProse(c, []string{text}, k, func(e *ui.Element) { e.FontWeight(weight) })
				})
			}
		}
	})
}

// mdProse renders one prose run (lines joined by newlines) as selectable
// text: constructor spans when the run carries no link (the form whose
// selectable editor receives presses), the element form when it does —
// a link is a clickable element and cannot be a span. style tweaks the
// element in either form.
func mdProse(c *ui.Context, lines []string, k tokensT, style func(*ui.Element)) {
	if mdHasLink(lines) {
		e := ui.RichText(c).Selectable().Children(func() {
			for i, l := range lines {
				if i > 0 {
					ui.Text(c, "\n")
				}
				mdInline(c, l, k)
			}
		})
		if style != nil {
			style(e)
		}
		return
	}
	var spans []ui.Span
	for i, l := range lines {
		if i > 0 {
			spans = append(spans, ui.Span{Text: "\n"})
		}
		spans = append(spans, mdSpans(l, k)...)
	}
	e := ui.RichText(c, spans...).FontSize(14).LineHeight(1.6).Selectable()
	if style != nil {
		style(e)
	}
}

// mdHasLink reports whether any line carries a markdown link.
func mdHasLink(lines []string) bool {
	for _, l := range lines {
		if strings.Contains(l, "](") && strings.Contains(l, "[") {
			return true
		}
	}
	return false
}

// mdSpans renders one line's inline markdown as constructor spans:
// `code`, **bold** and ~~struck~~.
func mdSpans(line string, k tokensT) []ui.Span {
	var out []ui.Span
	plain := &strings.Builder{}
	flush := func() {
		if plain.Len() > 0 {
			out = append(out, ui.Span{Text: plain.String()})
			plain.Reset()
		}
	}
	for i := 0; i < len(line); {
		switch {
		case line[i] == '`':
			if end := strings.IndexByte(line[i+1:], '`'); end >= 0 {
				flush()
				out = append(out, ui.Span{Text: line[i+1 : i+1+end],
					Font: theme.MonoFont, Size: 12.5, Background: k.SurfaceHover})
				i += end + 2
				continue
			}
			plain.WriteByte(line[i])
			i++
		case strings.HasPrefix(line[i:], "**"):
			if end := strings.Index(line[i+2:], "**"); end >= 0 {
				flush()
				out = append(out, ui.Span{Text: line[i+2 : i+2+end], Weight: 700})
				i += end + 4
				continue
			}
			plain.WriteByte(line[i])
			i++
		case strings.HasPrefix(line[i:], "~~"):
			if end := strings.Index(line[i+2:], "~~"); end >= 0 {
				flush()
				out = append(out, ui.Span{Text: line[i+2 : i+2+end],
					Strikethrough: true, Color: k.TextMuted})
				i += end + 4
				continue
			}
			plain.WriteByte(line[i])
			i++
		default:
			plain.WriteByte(line[i])
			i++
		}
	}
	flush()
	return out
}

// mdInline renders one line's inline markdown as child elements — the
// form a line with a link must take so the link stays clickable.
func mdInline(c *ui.Context, line string, k tokensT) {
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			ui.Text(c, plain.String())
			plain.Reset()
		}
	}
	for i := 0; i < len(line); {
		switch {
		case line[i] == '`':
			if end := strings.IndexByte(line[i+1:], '`'); end >= 0 {
				flush()
				ui.Text(c, line[i+1:i+1+end]).Font(theme.MonoFont).FontSize(12.5).
					TextBackground(k.SurfaceHover)
				i += end + 2
				continue
			}
			plain.WriteByte(line[i])
			i++
		case strings.HasPrefix(line[i:], "**"):
			if end := strings.Index(line[i+2:], "**"); end >= 0 {
				flush()
				ui.Text(c, line[i+2:i+2+end]).FontWeight(700)
				i += end + 4
				continue
			}
			plain.WriteByte(line[i])
			i++
		case strings.HasPrefix(line[i:], "~~"):
			if end := strings.Index(line[i+2:], "~~"); end >= 0 {
				flush()
				ui.Text(c, line[i+2:i+2+end]).Strikethrough().TextColor(k.TextMuted)
				i += end + 4
				continue
			}
			plain.WriteByte(line[i])
			i++
		case line[i] == '[':
			if end := strings.Index(line[i:], "]("); end > 0 {
				after := strings.Index(line[i+end:], ")")
				if after > 0 {
					label := line[i+1 : i+end]
					url := line[i+end+2 : i+end+after]
					flush()
					ui.Link(c, label, url)
					i += end + after + 1
					continue
				}
			}
			plain.WriteByte(line[i])
			i++
		default:
			plain.WriteByte(line[i])
			i++
		}
	}
	flush()
}

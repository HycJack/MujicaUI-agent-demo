package main

// mdparse.go parses the markdown subset Atlas replies use into blocks:
// headings, prose paragraphs, lists, quotes, tables, fenced/indented code
// and rules. Parsing is separate from rendering so a row's blocks can be
// cached on its element (ui.Local in mdview.go) and rebuilt as native
// elements every frame.

import "strings"

type mdKind uint8

const (
	mdPara    mdKind = iota // one selectable prose run (paragraphs joined by \n)
	mdHeading               // # / ## / ###
	mdList                  // bullet and ordered items
	mdQuote                 // > quote lines merged into one block
	mdTable                 // | a | b | grid with a separator line
	mdCode                  // fenced ``` block (or a 4-column indented run)
	mdRule                  // --- / *** / ___
)

// mdItem is one list entry; num is empty for a bullet.
type mdItem struct {
	depth int // indent level, two source columns per level
	num   string
	text  string
}

type mdBlock struct {
	kind   mdKind
	level  int      // heading level 1-3
	text   string   // para/quote/code source (lines joined by \n)
	lang   string   // fenced code language tag
	items  []mdItem // list
	header []string // table header cells
	rows   [][]string
}

// parseMarkdown splits src into blocks. Consecutive prose lines (and the
// blank lines between them) merge into ONE paragraph block, so a drag
// selection crosses paragraphs instead of stopping at every line.
func parseMarkdown(src string) []mdBlock {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var blocks []mdBlock
	var para []string
	flushPara := func() {
		if len(para) > 0 {
			blocks = append(blocks, mdBlock{kind: mdPara, text: strings.Join(para, "\n")})
			para = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		ind := indentOf(line)
		switch {
		case trimmed == "":
			// A blank line rides inside the prose run when prose resumes
			// after it; otherwise it closes the run.
			if len(para) > 0 {
				if i+1 < len(lines) && paragraphContinues(lines[i+1]) {
					para = append(para, "")
					continue
				}
				flushPara()
			}
		case strings.HasPrefix(trimmed, "```"):
			flushPara()
			lang := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			var code []string
			for i+1 < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i+1]), "```") {
				i++
				code = append(code, lines[i])
			}
			i++ // the closing fence, or the end of the source
			blocks = append(blocks, mdBlock{kind: mdCode, lang: lang, text: strings.Join(code, "\n")})
		case strings.HasPrefix(trimmed, "|") && i+1 < len(lines) &&
			isTableSeparator(strings.TrimSpace(lines[i+1])):
			flushPara()
			blocks = append(blocks, parseTable(lines, &i))
		case trimmed == "---" || trimmed == "***" || trimmed == "___":
			flushPara()
			blocks = append(blocks, mdBlock{kind: mdRule})
		case strings.HasPrefix(trimmed, "### "), strings.HasPrefix(trimmed, "## "), strings.HasPrefix(trimmed, "# "):
			flushPara()
			level, text := 1, strings.TrimPrefix(trimmed, "# ")
			if strings.HasPrefix(trimmed, "### ") {
				level, text = 3, strings.TrimPrefix(trimmed, "### ")
			} else if strings.HasPrefix(trimmed, "## ") {
				level, text = 2, strings.TrimPrefix(trimmed, "## ")
			}
			blocks = append(blocks, mdBlock{kind: mdHeading, level: level, text: text})
		case strings.HasPrefix(trimmed, "> "):
			flushPara()
			var quote []string
			for ; i < len(lines); i++ {
				t := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(t, ">") {
					i--
					break
				}
				quote = append(quote, strings.TrimPrefix(strings.TrimPrefix(t, ">"), " "))
			}
			blocks = append(blocks, mdBlock{kind: mdQuote, text: strings.Join(quote, "\n")})
		case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "),
			isListItem(trimmed):
			flushPara()
			var items []mdItem
			for ; i < len(lines); i++ {
				t := strings.TrimSpace(lines[i])
				num, rest := "", t
				switch {
				case strings.HasPrefix(t, "- "), strings.HasPrefix(t, "* "):
					rest = strings.TrimPrefix(strings.TrimPrefix(t, "- "), "* ")
				default:
					n, r := numberedItem(t)
					if n == "" {
						i--
						goto listDone
					}
					num, rest = n, r
				}
				items = append(items, mdItem{depth: indentOf(lines[i]) / 2, num: num, text: rest})
			}
		listDone:
			blocks = append(blocks, mdBlock{kind: mdList, items: items})
		case trimmed != "" && ind >= 4:
			// Four columns of indent outside a list item is an indented
			// code block; collect the whole run so it renders as one card.
			flushPara()
			var code []string
			for ; i < len(lines); i++ {
				if strings.TrimSpace(lines[i]) == "" || indentOf(lines[i]) < 4 {
					i--
					break
				}
				code = append(code, strings.TrimSpace(lines[i]))
			}
			blocks = append(blocks, mdBlock{kind: mdCode, text: strings.Join(code, "\n")})
		default:
			para = append(para, trimmed)
		}
	}
	flushPara()
	return blocks
}

// parseTable reads the |…| run starting at lines[*i] (its header) into a
// table block, advancing *i past the last row. The |---| separator line
// is skipped, not treated as a row or the end of the table.
func parseTable(lines []string, i *int) mdBlock {
	var b mdBlock
	b.kind = mdTable
	for ; *i < len(lines); *i++ {
		t := strings.TrimSpace(lines[*i])
		if isTableSeparator(t) {
			continue
		}
		if !strings.HasPrefix(t, "|") {
			*i--
			break
		}
		cells := splitTableRow(t)
		if b.header == nil {
			b.header = cells
			continue
		}
		b.rows = append(b.rows, cells)
	}
	return b
}

// paragraphContinues reports whether a line is still plain paragraph
// prose — a run of such lines renders as one selectable paragraph.
func paragraphContinues(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	switch {
	case strings.HasPrefix(trimmed, "#"), strings.HasPrefix(trimmed, "> "),
		strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "),
		trimmed == "---", trimmed == "***", trimmed == "___",
		strings.HasPrefix(trimmed, "```"), strings.HasPrefix(trimmed, "|"):
		return false
	}
	if indentOf(line) >= 4 {
		return false // an indented code block
	}
	if n, _ := numberedItem(trimmed); n != "" {
		return false
	}
	return true
}

// isListItem reports whether a line opens an ordered list item.
func isListItem(trimmed string) bool {
	n, _ := numberedItem(trimmed)
	return n != ""
}

// indentOf is the width of a line's leading indent, a tab counting as the
// four columns markdown measures it as.
func indentOf(line string) int {
	n := 0
	for _, r := range line {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

// numberedItem recognises "12. text" list lines and splits them. The
// separator has to open the item — "3.14 is pi" is prose, not the item
// "3." followed by "14 is pi".
func numberedItem(line string) (num, rest string) {
	i := strings.IndexAny(line, ".)")
	if i <= 0 || i > 4 {
		return "", ""
	}
	if i+1 >= len(line) || (line[i+1] != ' ' && line[i+1] != '\t') {
		return "", ""
	}
	for _, r := range line[:i] {
		if r < '0' || r > '9' {
			return "", ""
		}
	}
	return line[:i], strings.TrimLeft(line[i+1:], " \t")
}

// isTableSeparator reports whether a line is the |---|---| divider of a
// markdown table.
func isTableSeparator(line string) bool {
	if !strings.Contains(line, "-") {
		return false
	}
	for _, r := range line {
		switch r {
		case '|', '-', ':', ' ':
		default:
			return false
		}
	}
	return true
}

// splitTableRow splits one "| a | b |" line into its cells.
func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

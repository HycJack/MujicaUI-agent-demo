package md

// md_test.go covers the parser's block shapes: merged prose runs, lists,
// tables, fences, headings, quotes and rules.

import (
	"strings"
	"testing"
)

func TestParseBlocks(t *testing.T) {
	src := strings.Join([]string{
		"# Title",
		"",
		"First paragraph with **bold** and `code`.",
		"",
		"Second paragraph continues here.",
		"",
		"- bullet one",
		"- bullet two",
		"",
		"1. ordered",
		"2. items",
		"",
		"> quoted line",
		"",
		"| a | b |",
		"| --- | --- |",
		"| 1 | 2 |",
		"",
		"```go",
		"fmt.Println(\"hi\")",
		"```",
		"",
		"---",
	}, "\n")
	blocks := Parse(src)
	kinds := make([]Kind, len(blocks))
	for i, b := range blocks {
		kinds[i] = b.Kind
	}
	want := []Kind{KindHeading, KindPara, KindList, KindList, KindQuote, KindTable, KindCode, KindRule}
	if len(blocks) != len(want) {
		t.Fatalf("got %d blocks %v, want %d", len(blocks), kinds, len(want))
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("block %d = %v, want %v (all: %v)", i, kinds[i], want[i], kinds)
		}
	}
	if blocks[1].Text != "First paragraph with **bold** and `code`.\n\nSecond paragraph continues here." {
		t.Fatalf("prose run did not merge across the blank line: %q", blocks[1].Text)
	}
	if len(blocks[2].Items) != 2 || blocks[2].Items[0].Num != "" {
		t.Fatalf("bullet list wrong: %+v", blocks[2].Items)
	}
	if len(blocks[3].Items) != 2 || blocks[3].Items[1].Num != "2" {
		t.Fatalf("ordered list wrong: %+v", blocks[3].Items)
	}
	if len(blocks[5].Header) != 2 || len(blocks[5].Rows) != 1 {
		t.Fatalf("table wrong: %+v", blocks[5])
	}
	if blocks[6].Lang != "go" || !strings.Contains(blocks[6].Text, "Println") {
		t.Fatalf("code fence wrong: %+v", blocks[6])
	}
}

package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListDir(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"sub", ".git", "node_modules"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"b.txt", "a.txt", "Z.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := ListDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name)
	}
	want := []string{"sub", "a.txt", "b.txt", "Z.go"} // folders first, then case-insensitive names
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ListDir order %v, want %v", got, want)
	}
	if !entries[0].Dir {
		t.Fatal("the folder entry is not marked as a directory")
	}
}

// FormatSource formats Go with gofmt and pretty-prints JSON in-process;
// other languages (or unparsable sources) report not-formattable.
func TestFormatSource(t *testing.T) {
	raw := "package main\nfunc main(){\nx:=1\n_ = x\n}\n"
	out, ok := FormatSource("go", raw)
	if !ok || !strings.Contains(out, "x := 1") {
		t.Fatalf("gofmt view wrong: ok=%v out=%q", ok, out)
	}
	if _, ok := FormatSource("go", "not go at all"); ok {
		t.Fatal("unparsable go should not format")
	}
	out, ok = FormatSource("json", `{"a":1,"b":[2,3]}`)
	if !ok || !strings.Contains(out, "\n  \"a\": 1") {
		t.Fatalf("json view wrong: %q", out)
	}
	if _, ok := FormatSource("shell", "echo hi"); ok {
		t.Fatal("shell should not be formattable")
	}
	if !Formattable("go") || !Formattable("json") || Formattable("python") {
		t.Fatal("Formattable set wrong")
	}
}

func TestPreviewLang(t *testing.T) {
	cases := map[string]string{
		"a.go": "go", "b.sh": "shell", "c.jsx": "javascript",
		"d.ts": "typescript", "e.py": "python", "f.json": "json",
		"g.sql": "sql", "h.md": "",
	}
	for path, want := range cases {
		if got := PreviewLang(path); got != want {
			t.Fatalf("PreviewLang(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestReadCapped(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte(strings.Repeat("x", int(ReadCap)+10)), 0o644); err != nil {
		t.Fatal(err)
	}
	text, truncated, err := ReadCapped(p, ReadCap)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(text) != int(ReadCap) {
		t.Fatalf("truncation wrong: trunc=%v len=%d", truncated, len(text))
	}
}

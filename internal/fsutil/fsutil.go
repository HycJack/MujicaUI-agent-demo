// Package fsutil holds Crux's filesystem-facing pure helpers: listing a
// workspace directory, reading a bounded file, and mapping a file to its
// code-viewer language or in-process formatter. It depends on nothing but
// the standard library; the UI layer owns the async scheduling and state.
package fsutil

import (
	"bytes"
	"encoding/json"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// IgnoreSet lists directory entries the workspace tree skips: VCS internals,
// build and dependency directories — the noise mainstream agents hide too.
var IgnoreSet = map[string]bool{
	".git": true, ".gocache": true, ".gopath": true, ".mygo": true,
	".venv": true, "venv": true, "node_modules": true, "__pycache__": true,
	"dist": true, "build": true, "target": true,
	".DS_Store": true, "desktop.ini": true,
}

// ReadCap caps a previewed file's size; larger files load truncated.
const ReadCap = 256 << 10 // 256 KiB

// AttachCap caps one attached file's content folded into a message.
const AttachCap = 64 << 10 // 64 KiB

// Entry is one child of a listed directory.
type Entry struct {
	Path string
	Name string
	Dir  bool
}

// ListDir reads one directory's entries: folders first, then files, each
// case-insensitively named, noise directories skipped. It mirrors the way
// the tree shows the working directory.
func ListDir(dir string) ([]Entry, error) {
	reads, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(reads))
	for _, e := range reads {
		if IgnoreSet[e.Name()] {
			continue
		}
		out = append(out, Entry{Path: filepath.Join(dir, e.Name()), Name: e.Name(), Dir: e.IsDir()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// ReadCapped reads up to limit bytes of a file and reports whether it was
// truncated.
func ReadCapped(path string, limit int64) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	buf := make([]byte, limit)
	n, err := io.ReadFull(f, buf)
	if err == nil {
		// The buffer filled: one more byte means there was more to read.
		var probe [1]byte
		switch m, perr := f.Read(probe[:]); {
		case m > 0:
			return string(buf), true, nil
		case perr == nil || perr == io.EOF:
			return string(buf), false, nil
		default:
			return "", false, perr
		}
	}
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		return string(buf[:n]), false, nil
	}
	return "", false, err
}

// PreviewLang maps a file extension to a code-viewer highlighting language
// (go, javascript/typescript, python, json, shell, sql); unknown
// extensions render plain.
func PreviewLang(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".sh", ".bash", ".zsh":
		return "shell"
	case ".js", ".mjs", ".cjs", ".jsx":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".py":
		return "python"
	case ".json":
		return "json"
	case ".sql":
		return "sql"
	}
	return ""
}

// Formattable reports whether the language has an in-process formatter.
func Formattable(lang string) bool {
	return lang == "go" || lang == "json"
}

// FormatSource renders a formatted copy of text for the languages Crux can
// format in-process — gofmt for Go, two-space pretty-print for JSON. ok is
// false for other languages or when the source does not parse (the caller
// then shows the raw text).
func FormatSource(lang, text string) (string, bool) {
	switch lang {
	case "go":
		out, err := format.Source([]byte(text))
		if err != nil {
			return "", false
		}
		return string(out), true
	case "json":
		var buf bytes.Buffer
		if err := json.Indent(&buf, []byte(text), "", "  "); err != nil {
			return "", false
		}
		return buf.String(), true
	}
	return "", false
}

// WorkspaceName is the status bar's short workspace label.
func WorkspaceName(root string) string {
	if root == "" {
		return "no workspace"
	}
	return filepath.Base(root)
}

package main

// workspace.go is Crux's workspace: the real directory the session works
// in, browsed as a lazily-loaded file tree in the right-hand pane the way
// Codex-style consoles show the working tree. Listings run off the UI
// thread and land through a.redraw, the same way the model fetch does;
// headless (tests) they resolve synchronously.

import (
	"bytes"
	"encoding/json"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZacharyZhang-NY/MujicaUI/chat"
	"github.com/ZacharyZhang-NY/MujicaUI/data"
	"github.com/egoist/mygo/ui"
)

// wsIgnore lists directory entries the tree skips: VCS internals, build
// and dependency directories — the noise mainstream agents hide too.
var wsIgnore = map[string]bool{
	".git": true, ".gocache": true, ".gopath": true, ".mygo": true,
	".venv": true, "venv": true, "node_modules": true, "__pycache__": true,
	"dist": true, "build": true, "target": true,
	".DS_Store": true, "desktop.ini": true,
}

// wsReadCap caps a previewed file's size; larger files load truncated.
const wsReadCap = 256 << 10 // 256 KiB

// wsAttachCap caps one attached file's content folded into a message.
const wsAttachCap = 64 << 10 // 64 KiB

// wsEntry is one child of a listed directory.
type wsEntry struct {
	path string
	name string
	dir  bool
}

// wsNode is what the tree knows about one path.
type wsNode struct {
	dir    bool
	kids   []wsEntry
	status data.DataStatus
	err    string
}

// workspace is the lazily-listed file tree under the workspace root;
// outline is ui.Outline's open/selection state — the rows are custom-built
// (icon, listing status, right-click menu), so the tree is a plain Outline
// rather than data.Tree.
type workspace struct {
	root    string
	nodes   map[string]wsNode
	outline ui.OutlineState[string]
}

// newWorkspace roots the tree at dir; the root itself lists on first open.
func newWorkspace(dir string) workspace {
	w := workspace{root: dir, nodes: map[string]wsNode{}}
	if dir != "" {
		w.nodes[dir] = wsNode{dir: true, status: data.DataUnloaded}
	}
	return w
}

// reset drops every listing (and the tree's open rows) so the tree
// re-reads the disk on next open.
func (w *workspace) reset() {
	w.nodes = map[string]wsNode{}
	w.outline = ui.OutlineState[string]{}
	if w.root != "" {
		w.nodes[w.root] = wsNode{dir: true, status: data.DataUnloaded}
	}
}

// listDir reads one directory's entries: folders first, then files, each
// case-insensitively named, noise directories skipped.
func listDir(dir string) ([]wsEntry, error) {
	reads, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]wsEntry, 0, len(reads))
	for _, e := range reads {
		if wsIgnore[e.Name()] {
			continue
		}
		out = append(out, wsEntry{path: filepath.Join(dir, e.Name()), name: e.Name(), dir: e.IsDir()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].dir != out[j].dir {
			return out[i].dir
		}
		return strings.ToLower(out[i].name) < strings.ToLower(out[j].name)
	})
	return out, nil
}

// wsRoots is the tree's root list: the workspace directory, or nothing.
func wsRoots(w workspace) []string {
	if w.root == "" {
		return nil
	}
	return []string{w.root}
}

// wsLabel names a tree row; nodes are paths, rows show their base name.
func wsLabel(path string) string {
	return filepath.Base(path)
}

// wsChildren feeds data.Tree: nil for files and unknown paths (leaves), an
// empty slice for directories whose listing has not landed yet.
func (a *app) wsChildren(path string) []string {
	n, ok := a.ws.nodes[path]
	if !ok || !n.dir {
		return nil
	}
	if n.kids == nil {
		return []string{}
	}
	out := make([]string, len(n.kids))
	for i, e := range n.kids {
		out[i] = e.path
	}
	return out
}

// wsItemStatus reports a directory's listing state; files are ready leaves.
func (a *app) wsItemStatus(path string) data.DataStatus {
	n, ok := a.ws.nodes[path]
	if !ok || !n.dir {
		return data.DataReady
	}
	return n.status
}

// wsEnsureLoaded asks an open, never-listed directory for its listing —
// the Outline equivalent of data.Tree's Load hook. loadWsDir flips the
// status to Loading synchronously, so this asks at most once per listing.
func (a *app) wsEnsureLoaded(path string) {
	n, ok := a.ws.nodes[path]
	if ok && n.dir && n.status == data.DataUnloaded && a.ws.outline.Open.Has(path) {
		a.loadWsDir(path)
	}
}

// loadWsDir marks a directory loading and lists it — off the UI thread
// when a window is up, synchronously headless so tests stay deterministic.
func (a *app) loadWsDir(path string) {
	n, ok := a.ws.nodes[path]
	if !ok || !n.dir || n.status == data.DataLoading {
		return
	}
	n.status = data.DataLoading
	a.ws.nodes[path] = n
	if a.redraw == nil {
		kids, err := listDir(path)
		a.applyWsDir(path, kids, err)
		return
	}
	go func() {
		kids, err := listDir(path)
		a.redraw(func() { a.applyWsDir(path, kids, err) })
	}()
}

// applyWsDir stores a finished listing and registers child directories as
// unloaded so expanding them lists them in turn.
func (a *app) applyWsDir(path string, kids []wsEntry, err error) {
	n := a.ws.nodes[path]
	if err != nil {
		n.status, n.err, n.kids = data.DataFailed, err.Error(), nil
		a.ws.nodes[path] = n
		return
	}
	n.status, n.err, n.kids = data.DataReady, "", kids
	for _, e := range kids {
		if e.dir {
			if _, seen := a.ws.nodes[e.path]; !seen {
				a.ws.nodes[e.path] = wsNode{dir: true, status: data.DataUnloaded}
			}
		}
	}
	a.ws.nodes[path] = n
}

// selectWsNode reacts to a tree selection: files (which are not registered
// as nodes) open in the code drawer, an explicitly submitted directory
// toggles open.
func (a *app) selectWsNode(path string, submitted bool) {
	if n, ok := a.ws.nodes[path]; ok && n.dir {
		if submitted {
			if a.ws.outline.Open.Has(path) {
				a.ws.outline.Open.Remove(path)
			} else {
				a.ws.outline.Open.Add(path)
			}
		}
		return
	}
	a.openFileDrawer(path)
}

// attachFile adds a workspace file to the composer's context chips and
// returns the toast to show. Paths may be absolute (tree/preview) or
// repository-relative (changes list); ids are slash-separated relative
// paths so the same file is only attached once.
func (a *app) attachFile(path string) string {
	rel := relToRoot(a.ws.root, path)
	id := "file:" + rel
	for _, it := range a.thread.ctx {
		if it.ID == id {
			return "Already attached " + rel
		}
	}
	if len(a.thread.ctx) >= 8 {
		return "Attachment limit reached"
	}
	a.thread.ctx = append(a.thread.ctx, chat.ContextItem{
		ID:     id,
		Label:  filepath.Base(rel),
		Detail: rel,
		Kind:   chat.ContextFile,
	})
	return "Attached " + rel
}

// reloadWorkspace drops the listings and closes the code drawer so the
// pane re-reads the disk.
func (a *app) reloadWorkspace() {
	a.ws.reset()
	a.fdraw = fileDrawer{}
}

// readCapped reads up to limit bytes of a file and reports whether it was
// truncated.
func readCapped(path string, limit int64) (string, bool, error) {
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

// previewLang maps a file extension to a code-viewer highlighting language
// (go, javascript/typescript, python, json, shell, sql); unknown
// extensions render plain.
func previewLang(path string) string {
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

// formattable reports whether the language has an in-process formatter.
func formattable(lang string) bool {
	return lang == "go" || lang == "json"
}

// formatSource renders a formatted copy of text for the languages Crux can
// format in-process — gofmt for Go, two-space pretty-print for JSON. ok is
// false for other languages or when the source does not parse (the caller
// then shows the raw text).
func formatSource(lang, text string) (string, bool) {
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

// workspaceName is the status bar's short workspace label.
func workspaceName(root string) string {
	if root == "" {
		return "no workspace"
	}
	return filepath.Base(root)
}

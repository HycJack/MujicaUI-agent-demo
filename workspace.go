package main

// workspace.go is Atlas's workspace: the real directory the session works
// in, browsed as a lazily-loaded file tree in the right-hand pane the way
// Codex-style consoles show the working tree. Listings run off the UI
// thread and land through a.redraw, the same way the model fetch does;
// headless (tests) they resolve synchronously.

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZacharyZhang-NY/MujicaUI/data"
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

// workspace is the lazily-listed file tree under the workspace root; tree
// is data.Tree's open/selection state.
type workspace struct {
	root  string
	nodes map[string]wsNode
	tree  data.TreeState[string]
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
	w.tree = data.TreeState[string]{}
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
// as nodes) open in the preview, an explicitly submitted directory toggles
// open.
func (a *app) selectWsNode(path string, submitted bool) {
	if n, ok := a.ws.nodes[path]; ok && n.dir {
		if submitted {
			a.ws.tree.SetOpen(path, !a.ws.tree.IsOpen(path))
		}
		return
	}
	a.openWsPreview(path)
}

// openWsPreview loads a workspace file into the preview pane — async with
// a window, synchronous headless.
func (a *app) openWsPreview(path string) {
	if path == a.repo.previewPath && (a.repo.previewLoading || a.repo.previewText != "" || a.repo.previewErr != "") {
		return
	}
	a.repo.previewPath = path
	a.repo.previewLang = previewLang(path)
	a.repo.previewText, a.repo.previewErr = "", ""
	a.repo.previewTruncated, a.repo.previewLoading = false, true
	if a.redraw == nil {
		text, truncated, err := readCapped(path, wsReadCap)
		a.applyWsPreview(text, truncated, err)
		return
	}
	go func() {
		text, truncated, err := readCapped(path, wsReadCap)
		a.redraw(func() { a.applyWsPreview(text, truncated, err) })
	}()
}

// applyWsPreview stores a finished file read.
func (a *app) applyWsPreview(text string, truncated bool, err error) {
	a.repo.previewLoading = false
	if err != nil {
		a.repo.previewErr = err.Error()
		return
	}
	a.repo.previewText, a.repo.previewTruncated = text, truncated
}

// reloadWorkspace drops the listings and the preview so the pane re-reads
// the disk.
func (a *app) reloadWorkspace() {
	a.ws.reset()
	a.repo.previewPath, a.repo.previewText, a.repo.previewErr = "", "", ""
	a.repo.previewLoading, a.repo.previewTruncated = false, false
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
	case ".sh", ".bash":
		return "shell"
	case ".js", ".mjs", ".cjs":
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

// workspaceName is the status bar's short workspace label.
func workspaceName(root string) string {
	if root == "" {
		return "no workspace"
	}
	return filepath.Base(root)
}

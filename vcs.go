package main

// vcs.go is Crux's real version-control backend for the workspace: it
// shells out to git in the workspace root and feeds the Repository tab's
// branch selector, changes list, commit input and diff viewer. Collecting
// runs off the UI thread and lands through a.redraw, the same way the
// workspace listing and the model fetch do; headless (tests) it resolves
// synchronously.

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZacharyZhang-NY/MujicaUI/git"
)

// vcsFile is the raw entry behind one ChangedFile: what git said, plus the
// rename source needed to diff a moved file.
type vcsFile struct {
	path    string
	oldPath string // for renames: the pre-move path
	status  git.GitStatus
	staged  bool
}

// vcsState is the async version-control state the Repository tab renders.
type vcsState struct {
	loaded, loading bool
	err             string // collection error, e.g. "not a git repository"
	flash           string // one-shot toast for finished git actions
	committing      bool

	branch   string
	branches []git.Branch
	files    []git.ChangedFile
	entries  []vcsFile // parallel to files
	commits  []git.Commit

	diffKey     string // which selected change the diff below belongs to
	diffFrom    string // old version for the diff viewer
	diffTo      string // new version for the diff viewer
	diffWt      string // worktree content for the source tab
	diffErr     string
	diffLoading bool
}

// runGit runs one git command in dir and returns its combined output.
func runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if ctx.Err() != nil {
		return text, fmt.Errorf("git %s: timed out", strings.Join(args, " "))
	}
	if err != nil {
		return text, fmt.Errorf("git %s: %s", strings.Join(args, " "), text)
	}
	return text, nil
}

// statusFromCode maps a porcelain status letter to a GitStatus.
func statusFromCode(c byte) (git.GitStatus, bool) {
	switch c {
	case 'M', 'T':
		return git.GitModified, true
	case 'A', 'C':
		return git.GitAdded, true
	case 'D':
		return git.GitDeleted, true
	case 'R':
		return git.GitRenamed, true
	}
	return 0, false
}

// unquotePath strips the C-style quotes git puts around exotic paths.
func unquotePath(p string) string {
	if len(p) >= 2 && strings.HasPrefix(p, `"`) && strings.HasSuffix(p, `"`) {
		if u, err := strconv.Unquote(p); err == nil {
			return u
		}
	}
	return p
}

// parseStatus parses `git status --porcelain=v1 -b`: the branch header and
// one entry per staged or unstaged change (a file can appear twice).
func parseStatus(out string) (branch string, ahead, behind int, files []git.ChangedFile, entries []vcsFile) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "## "):
			head := strings.TrimPrefix(line, "## ")
			detached := strings.HasPrefix(head, "HEAD (no branch)")
			if i := strings.Index(head, " ["); i >= 0 {
				for _, part := range strings.Split(strings.Trim(head[i+1:], " []"), ",") {
					f := strings.Fields(part)
					if len(f) == 2 {
						if n, err := strconv.Atoi(f[1]); err == nil {
							switch f[0] {
							case "ahead":
								ahead = n
							case "behind":
								behind = n
							}
						}
					}
				}
				head = head[:i]
			}
			switch {
			case detached:
				head = ""
			default:
				if i := strings.Index(head, "..."); i >= 0 {
					head = head[:i]
				}
			}
			branch = strings.TrimSpace(head)
		case len(line) < 4 || line[2] != ' ':
			// not an entry line
		default:
			x, y := line[0], line[1]
			rest := unquotePath(line[3:])
			if x == 'R' || x == 'C' { // renamed: "old -> new"
				if parts := strings.SplitN(rest, " -> ", 2); len(parts) == 2 {
					if e, ok := stagedEntry(parts[1], parts[0], x); ok {
						files = append(files, e.changed())
						entries = append(entries, e)
					}
				}
				continue
			}
			if e, ok := stagedEntry(rest, "", x); ok {
				files = append(files, e.changed())
				entries = append(entries, e)
			}
			if s, ok := statusFromCode(y); ok && x != '?' {
				files = append(files, git.ChangedFile{Path: rest, Status: s})
				entries = append(entries, vcsFile{path: rest, status: s})
			} else if x == '?' && y == '?' {
				files = append(files, git.ChangedFile{Path: rest, Status: git.GitUntracked})
				entries = append(entries, vcsFile{path: rest, status: git.GitUntracked})
			}
		}
	}
	return branch, ahead, behind, files, entries
}

// changed projects the entry into the changes-list model.
func (e vcsFile) changed() git.ChangedFile {
	return git.ChangedFile{Path: e.path, Status: e.status, Staged: e.staged}
}

// stagedEntry builds the staged half of an entry; oldPath carries a rename's
// source path.
func stagedEntry(path, oldPath string, code byte) (vcsFile, bool) {
	s, ok := statusFromCode(code)
	if !ok {
		return vcsFile{}, false
	}
	return vcsFile{
		path:    path,
		oldPath: oldPath,
		status:  s,
		staged:  true,
	}, true
}

// parseBranches parses `git branch --all --format=...` output: five
// tab-separated fields per line.
func parseBranches(out string) []git.Branch {
	var outb []git.Branch
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) < 3 || f[0] == "" {
			continue
		}
		b := git.Branch{Name: f[1], Current: f[2] == "*"}
		b.Remote = strings.HasPrefix(f[0], "refs/remotes/")
		if len(f) > 3 {
			b.Upstream = f[3]
		}
		if len(f) > 4 {
			track := strings.Trim(f[4], "[]")
			for _, part := range strings.Split(track, ",") {
				p := strings.Fields(part)
				if len(p) == 2 {
					if n, err := strconv.Atoi(p[1]); err == nil {
						switch p[0] {
						case "ahead":
							b.Ahead = n
						case "behind":
							b.Behind = n
						}
					}
				}
			}
		}
		outb = append(outb, b)
	}
	return outb
}

// parseLog parses `git log --pretty=format:%H%x1f%P%x1f%an%x1f%at%x1f%s%x1f%D`
// with records separated by %x1e.
func parseLog(out string) []git.Commit {
	var commits []git.Commit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, "\x1f")
		if len(f) < 5 {
			continue
		}
		c := git.Commit{Hash: f[0], Author: f[2], Subject: f[4]}
		if f[1] != "" {
			c.Parents = strings.Fields(f[1])
		}
		if sec, err := strconv.ParseInt(f[3], 10, 64); err == nil {
			c.When = time.Unix(sec, 0)
		}
		if len(f) > 5 {
			for _, ref := range strings.Split(f[5], ", ") {
				ref = strings.TrimSpace(ref)
				ref = strings.TrimPrefix(ref, "HEAD -> ")
				ref = strings.TrimPrefix(ref, "tag: ")
				if ref != "" {
					c.Refs = append(c.Refs, ref)
				}
			}
		}
		commits = append(commits, c)
	}
	return commits
}

// vcsSnapshot is one collected view of the workspace repository.
type vcsSnapshot struct {
	branch   string
	branches []git.Branch
	files    []git.ChangedFile
	entries  []vcsFile
	commits  []git.Commit
	err      string
}

// collectVCS gathers the repository state with three git calls.
func collectVCS(root string) vcsSnapshot {
	var s vcsSnapshot
	st, err := runGit(root, "status", "--porcelain=v1", "-b")
	if err != nil {
		s.err = strings.TrimSpace(st)
		if s.err == "" {
			s.err = err.Error()
		}
		return s
	}
	s.branch, _, _, s.files, s.entries = parseStatus(st)
	if br, err := runGit(root, "branch", "--all",
		"--format=%(refname)%09%(refname:short)%09%(HEAD)%09%(upstream:short)%09%(upstream:track)"); err == nil {
		s.branches = parseBranches(br)
	}
	if lg, err := runGit(root, "log", "--max-count=30", "--date-order",
		"--pretty=format:%H%x1f%P%x1f%an%x1f%at%x1f%s%x1f%D%x1e"); err == nil {
		s.commits = parseLog(lg)
	}
	return s
}

// gitShow reads one blob out of the index or a revision.
func gitShow(root, spec string) (string, error) {
	out, err := runGit(root, "show", spec)
	if err != nil {
		return "", err
	}
	return out, nil
}

// readWorktree reads a file from the working tree, capped.
func readWorktree(root, rel string) (string, error) {
	content, _, err := readCapped(filepath.Join(root, filepath.FromSlash(rel)), wsReadCap)
	return content, err
}

// plainText replaces binary-looking content with a note so the viewers
// never render garbage.
func plainText(s string) string {
	if strings.ContainsRune(s, 0) {
		return "(binary file, not shown)"
	}
	if len(s) > wsReadCap {
		return s[:wsReadCap] + "\n… (truncated)"
	}
	return s
}

// fileVersions produces the diff viewer's old/new texts for one change,
// plus the worktree content for the source tab.
func fileVersions(root string, e vcsFile) (from, to, wt string, err error) {
	switch {
	case e.status == git.GitUntracked:
		to, err = readWorktree(root, e.path)
	case e.staged && e.status == git.GitAdded:
		to, err = gitShow(root, ":"+e.path)
	case e.staged && e.status == git.GitDeleted:
		from, err = gitShow(root, "HEAD:"+e.path)
	case e.staged && e.status == git.GitRenamed:
		if from, err = gitShow(root, "HEAD:"+e.oldPath); err == nil {
			to, err = gitShow(root, ":"+e.path)
		}
	case e.staged:
		if from, err = gitShow(root, "HEAD:"+e.path); err == nil {
			to, err = gitShow(root, ":"+e.path)
		}
	case e.status == git.GitDeleted:
		from, err = gitShow(root, ":"+e.path)
	default: // unstaged modify: index vs worktree
		if from, err = gitShow(root, ":"+e.path); err == nil {
			to, err = readWorktree(root, e.path)
		}
	}
	if err != nil {
		return "", "", "", err
	}
	wt, werr := readWorktree(root, e.path)
	if werr != nil {
		wt = ""
	}
	return plainText(from), plainText(to), plainText(wt), nil
}

// loadVCS refreshes the repository state — off the UI thread with a window,
// synchronously headless.
func (a *app) loadVCS() {
	if a.repo.vcs.loading {
		return
	}
	a.repo.vcs.loading = true
	if a.redraw == nil {
		a.applyVCS(collectVCS(a.ws.root))
		return
	}
	go func() {
		snap := collectVCS(a.ws.root)
		a.redraw(func() { a.applyVCS(snap) })
	}()
}

// applyVCS stores a collected snapshot and primes the diff for the
// currently selected change.
func (a *app) applyVCS(s vcsSnapshot) {
	v := &a.repo.vcs
	v.loading = false
	v.loaded = true
	v.err = s.err
	v.branch = s.branch
	v.branches = s.branches
	v.files = s.files
	v.entries = s.entries
	v.commits = s.commits
	if s.branch != "" {
		a.repo.branch = s.branch
	}
	v.diffKey = ""
	a.loadSelectedDiff()
}

// refreshVCS drops the cached repository state and collects it again.
func (a *app) refreshVCS() {
	a.repo.vcs.loaded = false
	a.loadVCS()
}

// diffKeyOf names the selected change so a stale async diff can be told
// apart from the current selection.
func diffKeyOf(files []git.ChangedFile, i int) string {
	if i < 0 || i >= len(files) {
		return ""
	}
	if files[i].Staged {
		return "s:" + files[i].Path
	}
	return "w:" + files[i].Path
}

// loadSelectedDiff makes sure the diff pane matches the selected change,
// loading its versions off the UI thread when they are not cached.
func (a *app) loadSelectedDiff() {
	v := &a.repo.vcs
	if len(v.files) == 0 || len(v.entries) == 0 {
		v.diffKey, v.diffFrom, v.diffTo, v.diffWt, v.diffErr = "", "", "", "", ""
		return
	}
	i := a.repo.changes.Selected()
	if i < 0 || i >= len(v.files) {
		i = 0
	}
	key := diffKeyOf(v.files, i)
	if key == v.diffKey || v.diffLoading {
		return
	}
	e := v.entries[i]
	v.diffKey = key
	v.diffLoading = true
	v.diffErr = ""
	if a.redraw == nil {
		from, to, wt, err := fileVersions(a.ws.root, e)
		a.applySelectedDiff(key, from, to, wt, err)
		return
	}
	go func() {
		from, to, wt, err := fileVersions(a.ws.root, e)
		a.redraw(func() { a.applySelectedDiff(key, from, to, wt, err) })
	}()
}

// applySelectedDiff stores a finished version read, unless the selection
// moved on while it was in flight.
func (a *app) applySelectedDiff(key, from, to, wt string, err error) {
	v := &a.repo.vcs
	if key != v.diffKey {
		return
	}
	v.diffLoading = false
	if err != nil {
		v.diffErr = err.Error()
		return
	}
	v.diffFrom, v.diffTo, v.diffWt, v.diffErr = from, to, wt, ""
}

// gitThen runs one mutating git command off the UI thread, flashes the
// outcome and refreshes the repository state.
func (a *app) gitThen(args []string, okMsg string) {
	if a.redraw == nil {
		_, err := runGit(a.ws.root, args...)
		a.afterGit(err, okMsg)
		return
	}
	go func() {
		_, err := runGit(a.ws.root, args...)
		a.redraw(func() { a.afterGit(err, okMsg) })
	}()
}

// afterGit flashes the result of a git action and reloads the state.
func (a *app) afterGit(err error, okMsg string) {
	if err != nil {
		msg := strings.TrimSpace(err.Error())
		if i := strings.Index(msg, "\n"); i > 0 {
			msg = msg[:i]
		}
		a.repo.vcs.flash = msg
	} else {
		a.repo.vcs.flash = okMsg
	}
	a.loadVCS()
}

// vcsAction performs a ChangesList action: stage or unstage one file,
// or the whole working tree.
func (a *app) vcsAction(action, path string) {
	switch action {
	case "stage":
		a.gitThen([]string{"add", "--", path}, "Staged "+path)
	case "unstage":
		a.gitThen([]string{"restore", "--staged", "--", path}, "Unstaged "+path)
	case "stage_all":
		a.gitThen([]string{"add", "-A"}, "Staged all changes")
	case "unstage_all":
		a.gitThen([]string{"restore", "--staged", "."}, "Unstaged all changes")
	}
}

// checkoutBranch switches to an existing branch.
func (a *app) checkoutBranch(name string) {
	if name == "" || name == a.repo.vcs.branch {
		return
	}
	a.gitThen([]string{"checkout", name}, "Switched to "+name)
}

// createBranch creates and checks out a new branch.
func (a *app) createBranch(name string) {
	if name == "" {
		return
	}
	a.gitThen([]string{"checkout", "-b", name}, "Created branch "+name)
}

// commitStaged commits (or amends) the staged changes with the commit
// input's message; the Busy flag locks the input while it runs.
func (a *app) commitStaged(amend bool) {
	v := &a.repo.vcs
	if v.committing {
		return
	}
	title := strings.TrimSpace(a.repo.msg.Title)
	if amend && title == "" {
		a.gitThen([]string{"commit", "--amend", "--no-edit"}, "Amended")
		return
	}
	if title == "" {
		v.flash = "Write a commit title first"
		return
	}
	if !amend {
		staged := false
		for _, f := range v.files {
			if f.Staged {
				staged = true
				break
			}
		}
		if !staged {
			v.flash = "Nothing staged"
			return
		}
	}
	args := []string{"commit", "-m", title}
	if body := strings.TrimSpace(a.repo.msg.Body); body != "" {
		args = append(args, "-m", body)
	}
	if amend {
		args = append(args, "--amend")
	}
	okMsg := "Committed"
	if amend {
		okMsg = "Amended"
	}
	v.committing = true
	if a.redraw == nil {
		_, err := runGit(a.ws.root, args...)
		v.committing = false
		if err == nil {
			a.repo.msg = git.CommitMessage{}
		}
		a.afterGit(err, okMsg)
		return
	}
	go func() {
		_, err := runGit(a.ws.root, args...)
		a.redraw(func() {
			v.committing = false
			if err == nil {
				a.repo.msg = git.CommitMessage{}
			}
			a.afterGit(err, okMsg)
		})
	}()
}

// relToRoot renders a path relative to the workspace root, slash-separated,
// for display and attachment ids.
func relToRoot(root, path string) string {
	rel := path
	if abs, err := filepath.Abs(path); err == nil {
		if r, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	return filepath.ToSlash(rel)
}

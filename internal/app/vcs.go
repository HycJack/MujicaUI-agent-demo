package app

// vcs.go is the app's version-control adapter: it schedules the real git
// collection (internal/vcs) off the UI thread and, through a.redraw, lands
// the snapshot into the Repository tab's component state, converting the
// vcs package's value types to the MujicaUI git types the renderers take.
// Headless (tests) it resolves synchronously.

import (
	"strings"

	"crux-agent/internal/vcs"
	"github.com/HycJack/MujicaUI/git"
)

// vcsState is the async version-control state the Repository tab renders.
type vcsState struct {
	loaded, loading bool
	err             string // collection error, e.g. "not a git repository"
	flash           string // one-shot toast for finished git actions
	committing      bool

	branch   string
	branches []git.Branch
	files    []git.ChangedFile
	entries  []vcs.File // parallel to files
	commits  []git.Commit

	diffKey     string // which selected change the diff below belongs to
	diffFrom    string // old version for the diff viewer
	diffTo      string // new version for the diff viewer
	diffWt      string // worktree content for the source tab
	diffErr     string
	diffLoading bool
}

// snapshotOf converts a collected vcs.Snapshot into the renderers' git
// component types plus the parallel raw entries.
func snapshotOf(s vcs.Snapshot) (branches []git.Branch, files []git.ChangedFile, commits []git.Commit, entries []vcs.File) {
	for _, b := range s.Branches {
		branches = append(branches, git.Branch{
			Name: b.Name, Remote: b.Remote, Current: b.Current,
			Upstream: b.Upstream, Ahead: b.Ahead, Behind: b.Behind,
		})
	}
	for _, f := range s.Files {
		files = append(files, git.ChangedFile{Path: f.Path, Status: gitStatusOf(f.Status), Staged: f.Staged})
	}
	for _, c := range s.Commits {
		commits = append(commits, git.Commit{
			Hash: c.Hash, Parents: c.Parents, Author: c.Author, When: c.When,
			Subject: c.Subject, Refs: c.Refs,
		})
	}
	return branches, files, commits, s.Entries
}

// gitStatusOf maps the vcs package's status to the MujicaUI git status.
func gitStatusOf(s vcs.GitStatus) git.GitStatus {
	switch s {
	case vcs.GitModified:
		return git.GitModified
	case vcs.GitAdded:
		return git.GitAdded
	case vcs.GitDeleted:
		return git.GitDeleted
	case vcs.GitRenamed:
		return git.GitRenamed
	default:
		return git.GitUntracked
	}
}

// loadVCS refreshes the repository state — off the UI thread with a window,
// synchronously headless.
func (a *app) loadVCS() {
	if a.repo.vcs.loading {
		return
	}
	a.repo.vcs.loading = true
	if a.redraw == nil {
		a.applyVCS(vcs.Collect(a.ws.root))
		return
	}
	go func() {
		snap := vcs.Collect(a.ws.root)
		a.redraw(func() { a.applyVCS(snap) })
	}()
}

// applyVCS stores a collected snapshot and primes the diff for the
// currently selected change.
func (a *app) applyVCS(s vcs.Snapshot) {
	v := &a.repo.vcs
	v.loading = false
	v.loaded = true
	v.err = s.Err
	v.branch = s.Branch
	v.branches, v.files, v.commits, v.entries = snapshotOf(s)
	if s.Branch != "" {
		a.repo.branch = s.Branch
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
		from, to, wt, err := vcs.FileVersions(a.ws.root, e)
		a.applySelectedDiff(key, from, to, wt, err)
		return
	}
	go func() {
		from, to, wt, err := vcs.FileVersions(a.ws.root, e)
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
		_, err := vcs.RunGit(a.ws.root, args...)
		a.afterGit(err, okMsg)
		return
	}
	go func() {
		_, err := vcs.RunGit(a.ws.root, args...)
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
		_, err := vcs.RunGit(a.ws.root, args...)
		v.committing = false
		if err == nil {
			a.repo.msg = git.CommitMessage{}
		}
		a.afterGit(err, okMsg)
		return
	}
	go func() {
		_, err := vcs.RunGit(a.ws.root, args...)
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
func relToRoot(root, path string) string { return vcs.RelToRoot(root, path) }

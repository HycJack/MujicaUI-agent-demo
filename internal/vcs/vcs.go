// Package vcs is Crux's version-control business layer: it shells out to
// git in a workspace root and turns the output into plain value types. The
// UI layer (internal/app) converts these types at its boundary and owns the
// async scheduling; this package knows nothing about MyGo, MujicaUI or any
// window state.
package vcs

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"crux-agent/internal/fsutil"
)

// GitStatus is a file's change state in version control.
type GitStatus int

// Statuses mirror the statuses the ChangesList component shows.
const (
	GitModified GitStatus = iota
	GitAdded
	GitDeleted
	GitRenamed
	GitUntracked
)

// ChangedFile is a changed file in the tree or index.
type ChangedFile struct {
	Path   string
	Status GitStatus
	Staged bool
}

// Branch is a local or remote branch.
type Branch struct {
	Name          string
	Remote        bool
	Current       bool
	Upstream      string
	Ahead, Behind int
}

// Commit is one commit of a history, newest first.
type Commit struct {
	Hash    string
	Parents []string
	Author  string
	When    time.Time
	Subject string
	Refs    []string
}

// File is the raw entry behind one ChangedFile: what git said, plus the
// rename source needed to diff a moved file.
type File struct {
	Path    string
	OldPath string // for renames: the pre-move path
	Status  GitStatus
	Staged  bool
}

// Snapshot is one collected view of the workspace repository.
type Snapshot struct {
	Branch   string
	Branches []Branch
	Files    []ChangedFile
	Entries  []File
	Commits  []Commit
	Err      string
}

// RunGit runs one git command in dir and returns its combined output.
func RunGit(dir string, args ...string) (string, error) {
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
func statusFromCode(c byte) (GitStatus, bool) {
	switch c {
	case 'M', 'T':
		return GitModified, true
	case 'A', 'C':
		return GitAdded, true
	case 'D':
		return GitDeleted, true
	case 'R':
		return GitRenamed, true
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

// ParseStatus parses `git status --porcelain=v1 -b`: the branch header and
// one entry per staged or unstaged change (a file can appear twice).
func ParseStatus(out string) (branch string, ahead, behind int, files []ChangedFile, entries []File) {
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
						files = append(files, changedOf(e))
						entries = append(entries, e)
					}
				}
				continue
			}
			if e, ok := stagedEntry(rest, "", x); ok {
				files = append(files, changedOf(e))
				entries = append(entries, e)
			}
			if s, ok := statusFromCode(y); ok && x != '?' {
				files = append(files, ChangedFile{Path: rest, Status: s})
				entries = append(entries, File{Path: rest, Status: s})
			} else if x == '?' && y == '?' {
				files = append(files, ChangedFile{Path: rest, Status: GitUntracked})
				entries = append(entries, File{Path: rest, Status: GitUntracked})
			}
		}
	}
	return branch, ahead, behind, files, entries
}

// changedOf projects the entry into the changes-list model.
func changedOf(e File) ChangedFile {
	return ChangedFile{Path: e.Path, Status: e.Status, Staged: e.Staged}
}

// stagedEntry builds the staged half of an entry; oldPath carries a rename's
// source path.
func stagedEntry(path, oldPath string, code byte) (File, bool) {
	s, ok := statusFromCode(code)
	if !ok {
		return File{}, false
	}
	return File{
		Path:    path,
		OldPath: oldPath,
		Status:  s,
		Staged:  true,
	}, true
}

// ParseBranches parses `git branch --all --format=...` output: five
// tab-separated fields per line.
func ParseBranches(out string) []Branch {
	var outb []Branch
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) < 3 || f[0] == "" {
			continue
		}
		b := Branch{Name: f[1], Current: f[2] == "*"}
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

// ParseLog parses `git log --pretty=format:%H%x1f%P%x1f%an%x1f%at%x1f%s%x1f%D`
// with records separated by %x1e.
func ParseLog(out string) []Commit {
	var commits []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, "\x1f")
		if len(f) < 5 {
			continue
		}
		c := Commit{Hash: f[0], Author: f[2], Subject: f[4]}
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

// Collect gathers the repository state with three git calls.
func Collect(root string) Snapshot {
	var s Snapshot
	st, err := RunGit(root, "status", "--porcelain=v1", "-b")
	if err != nil {
		s.Err = strings.TrimSpace(st)
		if s.Err == "" {
			s.Err = err.Error()
		}
		return s
	}
	s.Branch, _, _, s.Files, s.Entries = ParseStatus(st)
	if br, err := RunGit(root, "branch", "--all",
		"--format=%(refname)%09%(refname:short)%09%(HEAD)%09%(upstream:short)%09%(upstream:track)"); err == nil {
		s.Branches = ParseBranches(br)
	}
	if lg, err := RunGit(root, "log", "--max-count=30", "--date-order",
		"--pretty=format:%H%x1f%P%x1f%an%x1f%at%x1f%s%x1f%D%x1e"); err == nil {
		s.Commits = ParseLog(lg)
	}
	return s
}

// GitShow reads one blob out of the index or a revision.
func GitShow(root, spec string) (string, error) {
	out, err := RunGit(root, "show", spec)
	if err != nil {
		return "", err
	}
	return out, nil
}

// readWorktree reads a file from the working tree, capped.
func readWorktree(root, rel string) (string, error) {
	content, _, err := fsutil.ReadCapped(filepath.Join(root, filepath.FromSlash(rel)), fsutil.ReadCap)
	return content, err
}

// plainText replaces binary-looking content with a note so the viewers
// never render garbage.
func plainText(s string) string {
	if strings.ContainsRune(s, 0) {
		return "(binary file, not shown)"
	}
	if len(s) > fsutil.ReadCap {
		return s[:fsutil.ReadCap] + "\n… (truncated)"
	}
	return s
}

// FileVersions produces the diff viewer's old/new texts for one change,
// plus the worktree content for the source tab.
func FileVersions(root string, e File) (from, to, wt string, err error) {
	switch {
	case e.Status == GitUntracked:
		to, err = readWorktree(root, e.Path)
	case e.Staged && e.Status == GitAdded:
		to, err = GitShow(root, ":"+e.Path)
	case e.Staged && e.Status == GitDeleted:
		from, err = GitShow(root, "HEAD:"+e.Path)
	case e.Staged && e.Status == GitRenamed:
		if from, err = GitShow(root, "HEAD:"+e.OldPath); err == nil {
			to, err = GitShow(root, ":"+e.Path)
		}
	case e.Staged:
		if from, err = GitShow(root, "HEAD:"+e.Path); err == nil {
			to, err = GitShow(root, ":"+e.Path)
		}
	case e.Status == GitDeleted:
		from, err = GitShow(root, ":"+e.Path)
	default: // unstaged modify: index vs worktree
		if from, err = GitShow(root, ":"+e.Path); err == nil {
			to, err = readWorktree(root, e.Path)
		}
	}
	if err != nil {
		return "", "", "", err
	}
	wt, werr := readWorktree(root, e.Path)
	if werr != nil {
		wt = ""
	}
	return plainText(from), plainText(to), plainText(wt), nil
}

// RelToRoot renders a path relative to the workspace root, slash-separated,
// for display and attachment ids.
func RelToRoot(root, path string) string {
	rel := path
	if abs, err := filepath.Abs(path); err == nil {
		if r, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	return filepath.ToSlash(rel)
}

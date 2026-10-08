package main

// repo.go is the Atlas repo inspector: the branch list, the working
// tree's changes, the commit log, and the diff of whichever file is
// chosen — the version-control side of a Codex-style assistant.

import (
	"strings"
	"time"

	"github.com/ZacharyZhang-NY/MujicaUI/code"
	"github.com/ZacharyZhang-NY/MujicaUI/git"
	"github.com/ZacharyZhang-NY/MujicaUI/icons"
	"github.com/egoist/mygo/ui"
)

// demoFiles is the working tree the inspector browses; index aligns with
// the changes list so a chosen row shows its diff.
func demoFiles() []file {
	return []file{
		{path: "jobs/export-nightly.sh", status: git.GitModified, lang: "bash",
			from: "#!/usr/bin/env bash\nbuild dump\nupload dump\n",
			to:   "#!/usr/bin/env bash\nbuild dump\nprune yesterday\nupload dump\n"},
		{path: "jobs/quota.go", status: git.GitModified, lang: "go",
			from: "package jobs\n\n// cap keeps a week of dumps.\nvar cap = 7\n",
			to:   "package jobs\n\n// cap keeps a week of dumps.\nvar cap = 14\n"},
		{path: "docs/runbook.md", status: git.GitAdded, lang: "markdown",
			from: "",
			to:   "# Export runbook\n\nThe archive bucket keeps a week of dumps; raise the cap before it jams.\n"},
	}
}

// changedFiles projects demoFiles into the changes list model.
func changedFiles() []git.ChangedFile {
	files := demoFiles()
	out := make([]git.ChangedFile, 0, len(files))
	for i, f := range files {
		out = append(out, git.ChangedFile{Path: f.path, Status: f.status, Staged: i == 0})
	}
	return out
}

// branchList is the demo branch set.
func branchList() []git.Branch {
	return []git.Branch{
		{Name: "main", Current: true, Upstream: "origin/main", Ahead: 1},
		{Name: "fix/export-quota", Ahead: 2, Behind: 1},
		{Name: "origin/main", Remote: true},
		{Name: "origin/release", Remote: true},
	}
}

// commitList is the demo history, newest first.
func commitList() []git.Commit {
	now := time.Now()
	return []git.Commit{
		{Hash: "a1b2c3d", Author: "Ada Lovelace", When: now.Add(-2 * time.Hour), Subject: "Raise the archive bucket cap", Refs: []string{"main"}},
		{Hash: "9f8e7d6", Author: "Ada Lovelace", When: now.Add(-26 * time.Hour), Subject: "Prune yesterday's dump before upload"},
		{Hash: "4c5d6e7", Author: "Grace Hopper", When: now.Add(-3 * 24 * time.Hour), Subject: "Add the nightly export job"},
	}
}

// repoPane is the right-hand inspector, shown when repo.showRepo is set.
func (a *app) repoPane(c *ui.Context, k tokensT) {
	ui.Column(c).Width(380).Shrink(0).Background(k.Surface).Padding(12).Gap(10).Children(func() {
		// Header: title and a branch selector that actually switches branch.
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icons.Must("git-branch")).FontSize(14).TextColor(k.TextMuted)
			ui.Text(c, "Repository").FontSize(13).Bold().Grow(1)
		})
		sel := git.BranchSelector(c, &a.repo.branch, branchList(), git.BranchSelectorOptions{AllowCreate: true})
		if sel.Changed() {
			c.Toast("Switched to " + a.repo.branch)
		}
		if name, ok := sel.Created(); ok {
			c.Toast("Created branch " + name)
		}

		ui.Text(c, "Changes").FontSize(11).TextColor(k.TextMuted)
		cl := git.ChangesList(c, &a.repo.changes, changedFiles(), git.ChangesListOptions{})
		cl.Element.Height(96)

		ui.Text(c, "History").FontSize(11).TextColor(k.TextMuted)
		history := git.CommitList(c, &a.repo.commits, commitList(), git.CommitListOptions{})
		history.Element.Height(96)

		// Bottom pane: the chosen file's diff, or its source, toggled.
		files := demoFiles()
		i := a.repo.changes.Selected()
		if i < 0 || i >= len(files) {
			i = 0
		}
		f := files[i]
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			git.GitStatusBadge(c, f.status, git.GitStatusBadgeOptions{})
			ui.Text(c, f.path).FontSize(11).TextColor(k.TextMuted).SingleLine().Grow(1).MinWidth(0)
		})
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Segmented(c, &a.repo.codeTab, "Diff", "Source").Width(160)
		})
		ui.Box(c).Grow(1).MinHeight(140).Clip().Children(func() {
			if a.repo.codeTab == 1 {
				code.CodeViewer(c, splitLines(f.to), &a.repo.dst, code.CodeViewerOptions{Language: f.lang, Label: f.path})
			} else {
				git.DiffViewer(c, &a.repo.diff, f.from, f.to, git.DiffViewerOptions{Language: f.lang}).Fill()
			}
		})
	})
}

// splitLines breaks text into lines for the code viewer.
func splitLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

package main

// repo.go is the Atlas right-hand inspector: the Workspace tab browses the
// real directory tree, and the Repository tab manages the workspace's real
// git repository — branch, staged/unstaged changes, commit, history, diff.

import (
	"strings"

	"github.com/ZacharyZhang-NY/MujicaUI/code"
	"github.com/ZacharyZhang-NY/MujicaUI/data"
	"github.com/ZacharyZhang-NY/MujicaUI/git"
	"github.com/ZacharyZhang-NY/MujicaUI/icons"
	"github.com/egoist/mygo/ui"
)

// repoPane is the right-hand inspector, shown when repo.showRepo is set:
// a Workspace tab browsing the real directory tree, and a Repository tab
// with the (demo) version-control side.
func (a *app) repoPane(c *ui.Context, k tokensT) {
	ui.Column(c).Width(380).Shrink(0).Background(k.Surface).Padding(12).Gap(10).Children(func() {
		ui.Segmented(c, &a.repo.paneTab, "Workspace", "Repository").FillWidth()
		if a.repo.paneTab == 0 {
			a.workspacePane(c, k)
			return
		}
		a.repositoryPane(c, k)
	})
}

// workspacePane is the Workspace tab: where the session runs, the real
// directory tree below it, and the selected file's source at the bottom.
func (a *app) workspacePane(c *ui.Context, k tokensT) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icons.Must("folder")).FontSize(14).TextColor(k.TextMuted)
		ui.Text(c, a.ws.root).FontSize(11).TextColor(k.TextMuted).SingleLine().Grow(1).MinWidth(0).Tooltip(a.ws.root)
		a.iconToggle(c, k, "refresh-cw", "Reload workspace", a.reloadWorkspace)
	})
	tree := data.Tree(c, &a.ws.tree, data.TreeOptions[string]{
		Roots:      wsRoots(a.ws),
		Children:   a.wsChildren,
		Label:      wsLabel,
		ItemStatus: a.wsItemStatus,
		Load:       a.loadWsDir,
		Empty:      "No workspace directory",
	})
	tree.Element.Grow(1).MinHeight(0)
	if tree.Changed() {
		if path, ok := a.ws.tree.Selected(); ok {
			a.selectWsNode(path, false)
		}
	}
	if tree.Submitted() {
		if path, ok := a.ws.tree.Selected(); ok {
			a.selectWsNode(path, true)
		}
	}
	a.wsPreview(c, k)
}

// wsPreview is the Workspace tab's bottom pane: the selected file's source,
// or a hint while nothing is picked.
func (a *app) wsPreview(c *ui.Context, k tokensT) {
	if a.repo.previewPath == "" {
		ui.Text(c, "Select a file to preview").FontSize(11).TextColor(k.TextMuted)
		return
	}
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icons.Must("file-code")).FontSize(13).TextColor(k.TextMuted)
		ui.Text(c, a.repo.previewPath).FontSize(11).TextColor(k.TextMuted).SingleLine().Grow(1).MinWidth(0)
		if a.repo.previewTruncated {
			ui.Text(c, "truncated").FontSize(10).TextColor(k.TextMuted)
		}
		if formattable(a.repo.previewLang) {
			ui.Segmented(c, &a.repo.wsFmt.tab, "Raw", "Fmt").Width(108)
		}
		a.iconToggle(c, k, "plus", "Attach to conversation", func() { c.Toast(a.attachFile(a.repo.previewPath)) })
	})
	ui.Box(c).Height(340).Shrink(0).Clip().Children(func() {
		switch {
		case a.repo.previewErr != "":
			ui.Text(c, "⚠ "+a.repo.previewErr).FontSize(12).TextColor(kDanger(c))
		case a.repo.previewLoading:
			ui.Text(c, "Loading…").FontSize(12).TextColor(kTextMuted(c))
		default:
			code.CodeViewer(c, a.repo.wsFmt.render(a.repo.previewLang, a.repo.previewText), &a.repo.psrc,
				code.CodeViewerOptions{Language: a.repo.previewLang, Label: a.repo.previewPath})
		}
	})
}

// repositoryPane is the Repository tab: the workspace repository's real
// git state — branch, staged/unstaged changes, commit input, history, and
// the selected file's diff or source.
func (a *app) repositoryPane(c *ui.Context, k tokensT) {
	v := &a.repo.vcs
	if !v.loaded && !v.loading {
		a.loadVCS()
	}
	if v.flash != "" {
		c.Toast(v.flash)
		v.flash = ""
	}
	// Header: title and a refresh button.
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icons.Must("git-branch")).FontSize(14).TextColor(k.TextMuted)
		ui.Text(c, "Repository").FontSize(13).Bold().Grow(1)
		a.iconToggle(c, k, "refresh-cw", "Refresh repository", a.refreshVCS)
	})
	if v.err != "" {
		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, "⚠ "+v.err).FontSize(12).TextColor(kDanger(c))
			ui.Text(c, "Atlas manages the git repository at the workspace root.").FontSize(11).TextColor(kTextMuted(c))
		})
		return
	}

	// Branch selector that actually checks out.
	branches := v.branches
	if len(branches) == 0 && v.branch != "" {
		branches = []git.Branch{{Name: v.branch, Current: true}}
	}
	sel := git.BranchSelector(c, &a.repo.branch, branches, git.BranchSelectorOptions{AllowCreate: true})
	if sel.Changed() {
		a.checkoutBranch(a.repo.branch)
	}
	if name, ok := sel.Created(); ok {
		a.createBranch(name)
	}

	ui.Text(c, "Changes").FontSize(11).TextColor(k.TextMuted)
	cl := git.ChangesList(c, &a.repo.changes, v.files, git.ChangesListOptions{})
	cl.Element.Height(96)
	if action, path, ok := cl.Action(); ok {
		a.vcsAction(action, path)
	}
	if cl.Submitted() {
		if f, ok := a.selectedChange(); ok {
			c.Toast(a.attachFile(f.Path))
		}
	}

	ui.Text(c, "History").FontSize(11).TextColor(k.TextMuted)
	history := git.CommitList(c, &a.repo.commits, v.commits, git.CommitListOptions{})
	history.Element.Height(72)

	// Commit box: title + body, locked with Busy while git runs.
	ci := git.CommitInput(c, &a.repo.msg, git.CommitInputOptions{Busy: v.committing})
	if ci.Committed() {
		a.commitStaged(false)
	}
	if ci.Amended() {
		a.commitStaged(true)
	}

	// Bottom pane: the selected change's diff or source.
	a.loadSelectedDiff()
	f, ok := a.selectedChange()
	if !ok {
		ui.Text(c, "Working tree clean — nothing to review").FontSize(11).TextColor(kTextMuted(c))
		return
	}
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		git.GitStatusBadge(c, f.Status, git.GitStatusBadgeOptions{})
		ui.Text(c, f.Path).FontSize(11).TextColor(k.TextMuted).SingleLine().Grow(1).MinWidth(0)
		a.iconToggle(c, k, "plus", "Attach to conversation", func() { c.Toast(a.attachFile(f.Path)) })
	})
	lang := previewLang(f.Path)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Segmented(c, &a.repo.codeTab, "Diff", "Source").Width(160)
		if a.repo.codeTab == 1 && formattable(lang) {
			ui.Segmented(c, &a.repo.srcFmt.tab, "Raw", "Fmt").Width(108)
		}
	})
	ui.Box(c).Grow(1).MinHeight(220).Clip().Children(func() {
		switch {
		case v.diffLoading:
			ui.Text(c, "Loading versions…").FontSize(12).TextColor(kTextMuted(c))
		case v.diffErr != "":
			ui.Text(c, "⚠ "+v.diffErr).FontSize(12).TextColor(kDanger(c))
		case a.repo.codeTab == 1:
			code.CodeViewer(c, a.repo.srcFmt.render(lang, v.diffWt), &a.repo.dst, code.CodeViewerOptions{Language: lang, Label: f.Path})
		default:
			git.DiffViewer(c, &a.repo.diff, v.diffFrom, v.diffTo, git.DiffViewerOptions{Language: lang}).Fill()
		}
	})
}

// selectedChange returns the changes list's selected file, clamped to the
// first one when nothing (or an out-of-range row) is selected.
func (a *app) selectedChange() (git.ChangedFile, bool) {
	v := &a.repo.vcs
	if len(v.files) == 0 {
		return git.ChangedFile{}, false
	}
	i := a.repo.changes.Selected()
	if i < 0 || i >= len(v.files) {
		i = 0
	}
	return v.files[i], true
}

// splitLines breaks text into lines for the code viewer.
func splitLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

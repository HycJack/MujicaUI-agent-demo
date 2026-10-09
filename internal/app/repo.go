package app

// repo.go is the Crux right-hand inspector: the Workspace tab browses the
// real directory tree, and the Repository tab manages the workspace's real
// git repository — branch, staged/unstaged changes, commit, history, diff.

import (
	"path/filepath"
	"strings"

	"github.com/ZacharyZhang-NY/MujicaUI/code"
	"github.com/ZacharyZhang-NY/MujicaUI/core"
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

// workspacePane is the Workspace tab: the real directory tree, filling the
// pane; picking a file opens it in the large code drawer, right-clicking a
// file offers attach / viewer / copy-path.
func (a *app) workspacePane(c *ui.Context, k tokensT) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icons.Must("folder")).FontSize(14).TextColor(k.TextMuted)
		ui.Text(c, a.ws.root).FontSize(11).TextColor(k.TextMuted).SingleLine().Grow(1).MinWidth(0).Tooltip(a.ws.root)
		a.iconToggle(c, k, "refresh-cw", "Reload workspace", a.reloadWorkspace)
	})
	restore := data.DataSelectTheme(c, 0)
	a.ws.outline.List.Selected = &a.wsCursor // clicks register a choice
	e := ui.Outline(c, &a.ws.outline, wsRoots(a.ws), a.wsChildren, func(path string) {
		a.wsTreeRow(c, k, path)
	})
	restore()
	e.Grow(1).MinHeight(0)
	if e.Changed() {
		if i := a.ws.outline.List.Selected; i != nil && *i >= 0 && *i < a.ws.outline.Rows() {
			a.selectWsNode(a.ws.outline.Item(*i), false)
		}
	}
	if e.Submitted() {
		if i := a.ws.outline.List.Selected; i != nil && *i >= 0 && *i < a.ws.outline.Rows() {
			a.selectWsNode(a.ws.outline.Item(*i), true)
		}
	}
}

// wsTreeRow builds one directory-tree row: icon, name, listing status, and
// — on files — a right-click menu (attach to the conversation, open in the
// viewer, copy the path). Open unloaded directories ask for their listing.
func (a *app) wsTreeRow(c *ui.Context, k tokensT, path string) {
	n, isNode := a.ws.nodes[path]
	isDir := isNode && n.dir
	a.wsEnsureLoaded(path)
	row := ui.Row(c).Label("tree:" + path).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Tooltip(path)
	row.Children(func() {
		icon := "file"
		if isDir {
			icon = "folder"
		} else if strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".js") ||
			strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".json") ||
			strings.HasSuffix(path, ".py") || strings.HasSuffix(path, ".sql") ||
			strings.HasSuffix(path, ".sh") {
			icon = "file-code"
		}
		ui.Icon(c, icons.Must(icon)).FontSize(14).TextColor(k.TextMuted)
		ui.Text(c, filepath.Base(path)).SingleLine().Grow(1).MinWidth(0)
		switch {
		case isDir && n.status == data.DataLoading:
			core.Spinner(c, core.SpinnerOptions{Size: 13})
		case isDir && n.status == data.DataFailed:
			ui.Icon(c, icons.Must("circle-alert")).FontSize(13).TextColor(k.Danger)
		}
	})
	if !isDir {
		// Right-click a file: the attach entry point lives where the file is.
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item("Add to conversation").Chosen() {
				c.Toast(a.attachFile(path))
			}
			if m.Item("Open in viewer").Chosen() {
				a.openFileDrawer(path)
			}
			m.Separator()
			if m.Item("Copy path").Chosen() {
				c.WriteClipboard(path)
				c.Toast("Path copied")
			}
		})
	}
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
			ui.Text(c, "Crux manages the git repository at the workspace root.").FontSize(11).TextColor(kTextMuted(c))
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
	lang := previewLang(f.Path)
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		git.GitStatusBadge(c, f.Status, git.GitStatusBadgeOptions{})
		ui.Text(c, f.Path).FontSize(11).TextColor(k.TextMuted).SingleLine().Grow(1).MinWidth(0)
		a.iconToggle(c, k, "maximize", "Open in large viewer", func() {
			if a.repo.codeTab == 1 {
				a.openSourceDrawer(a.repo.vcs.diffWt, lang, f.Path)
			} else {
				a.openDiffDrawer(a.repo.vcs.diffFrom, a.repo.vcs.diffTo, lang, f.Path)
			}
		})
		a.iconToggle(c, k, "plus", "Attach to conversation", func() { c.Toast(a.attachFile(f.Path)) })
	})
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

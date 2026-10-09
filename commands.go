package main

// commands.go is Atlas's command palette (⌘K): the actions a coding
// console exposes — new chat, fold the panels, jump between the diff and
// the file source, export the thread — reachable without the mouse.

import (
	"github.com/ZacharyZhang-NY/MujicaUI/icons"
	"github.com/ZacharyZhang-NY/MujicaUI/navigation"
	"github.com/egoist/mygo/ui"
)

// palette builds the ⌘K command dialog and runs whichever action is
// chosen this frame.
func (a *app) palette(c *ui.Context) {
	cmds := []navigation.Command{
		{ID: "new", Label: "New chat", Group: "Session", Icon: icons.Must("plus")},
		{ID: "export", Label: "Export conversation", Group: "Session", Keywords: []string{"save", "markdown"}, Icon: icons.Must("download")},
		{ID: "toggle-nav", Label: "Toggle sessions", Group: "View", Icon: icons.Must("panel-left")},
		{ID: "toggle-repo", Label: "Toggle repo inspector", Group: "View", Icon: icons.Must("git-pull-request")},
		{ID: "providers", Label: "Providers settings", Group: "Settings", Keywords: []string{"backend", "api key"}, Icon: icons.Must("settings")},
		{ID: "agent", Label: "Agent settings", Group: "Settings", Keywords: []string{"prompt", "model"}, Icon: icons.Must("bot")},
		{ID: "workspace", Label: "Show workspace tree", Group: "Repo", Keywords: []string{"files", "directory"}, Icon: icons.Must("folder")},
		{ID: "reload-workspace", Label: "Reload workspace", Group: "Repo", Keywords: []string{"refresh", "files"}, Icon: icons.Must("refresh-cw")},
		{ID: "diff", Label: "Show working diff", Group: "Repo", Icon: icons.Must("git-commit-horizontal")},
		{ID: "source", Label: "Show file source", Group: "Repo", Icon: icons.Must("file-code")},
		{ID: "branch", Label: "Switch branch…", Group: "Repo", Icon: icons.Must("git-branch")},
	}
	r := navigation.CommandPalette(c, &a.paletteOpen, cmds, navigation.CommandPaletteOptions{})
	if id, ok := r.Chosen(); ok {
		if msg := a.runCommand(id); msg != "" {
			c.Toast(msg)
		}
	}
}

// runCommand performs a palette action and returns the toast it should
// raise, if any. It takes no context so it is testable on its own.
func (a *app) runCommand(id string) string {
	switch id {
	case "new":
		a.newThread()
	case "export":
		return "Conversation exported (demo)"
	case "toggle-nav":
		a.navOpen = !a.navOpen
	case "toggle-repo":
		a.repo.showRepo = !a.repo.showRepo
	case "providers":
		a.openProviders()
	case "agent":
		a.openAgent()
	case "workspace":
		a.repo.showRepo, a.repo.paneTab = true, 0
	case "reload-workspace":
		a.reloadWorkspace()
		return "Workspace reloaded"
	case "diff":
		a.repo.showRepo, a.repo.paneTab, a.repo.codeTab = true, 1, 0
	case "source":
		a.repo.showRepo, a.repo.paneTab, a.repo.codeTab = true, 1, 1
	case "branch":
		a.repo.showRepo, a.repo.paneTab = true, 1
		return "Pick a branch in the inspector"
	}
	return ""
}

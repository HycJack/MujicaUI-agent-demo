package app

// vcs_test.go covers the real git backend: porcelain parsing (pure), and
// the whole version-management loop against a throwaway repository that
// gitInit creates with real git commands.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZacharyZhang-NY/MujicaUI/chat"
	"github.com/ZacharyZhang-NY/MujicaUI/git"
	"github.com/egoist/mygo/ui"
)

// gitRun runs one git command in dir, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// gitInit makes a real git repository in a temp dir: branch main, one
// committed file. Skips the test when no git binary exists.
func gitInit(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git binary available")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init")
	gitRun(t, dir, "checkout", "-b", "main") // git <2.28 defaults to master
	gitRun(t, dir, "config", "user.email", "crux@example.com")
	gitRun(t, dir, "config", "user.name", "Crux")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "init")
	return dir
}

// seedVCSDemo makes a repository with something to review: a file both
// staged and modified further (MM), and an untracked doc.
func seedVCSDemo(t *testing.T) string {
	t.Helper()
	dir := gitInit(t)
	w := func(name, content string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("jobs/quota.go", "package jobs\n\n// cap keeps a week of dumps.\nvar cap = 7\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "Add the quota job")
	w("jobs/quota.go", "package jobs\n\n// cap keeps a week of dumps.\nvar cap = 14\n")
	gitRun(t, dir, "add", "jobs/quota.go")
	w("jobs/quota.go", "package jobs\n\n// cap keeps a week of dumps.\nvar cap = 21\n")
	w("docs/runbook.md", "# Export runbook\n\nRaise the cap before the bucket jams.\n")
	return dir
}

func TestVCSRealRepo(t *testing.T) {
	dir := gitInit(t)
	a := newApp()
	a.ws = newWorkspace(dir)
	a.repo.showRepo, a.repo.paneTab = true, 1

	a.loadVCS() // headless: synchronous
	v := &a.repo.vcs
	if v.err != "" {
		t.Fatalf("collect error: %s", v.err)
	}
	if v.branch != "main" || a.repo.branch != "main" {
		t.Fatalf("branch = %q / %q, want main", v.branch, a.repo.branch)
	}
	if len(v.files) != 0 || len(v.commits) != 1 {
		t.Fatalf("fresh repo: %d files, %d commits", len(v.files), len(v.commits))
	}

	// An untracked file shows up unstaged; its diff is empty vs content.
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshVCS()
	if len(v.files) != 1 || v.files[0].Status != git.GitUntracked || v.files[0].Staged {
		t.Fatalf("untracked wrong: %+v", v.files)
	}
	a.loadSelectedDiff()
	if v.diffFrom != "" || v.diffTo != "hello" {
		t.Fatalf("untracked diff %q -> %q", v.diffFrom, v.diffTo)
	}

	// Stage it through the ChangesList action; the diff becomes "" -> content.
	a.vcsAction("stage", "hello.txt")
	if len(v.files) != 1 || !v.files[0].Staged {
		t.Fatalf("staged wrong: %+v", v.files)
	}
	a.loadSelectedDiff()
	if v.diffFrom != "" || v.diffTo != "hello" {
		t.Fatalf("staged diff %q -> %q", v.diffFrom, v.diffTo)
	}

	// Commit through the commit input path; the tree goes clean.
	a.repo.msg = git.CommitMessage{Title: "add hello"}
	a.commitStaged(false)
	if v.committing {
		t.Fatal("commit still marked busy")
	}
	if a.repo.msg.Title != "" {
		t.Fatal("commit message not cleared")
	}
	if len(v.commits) != 2 || v.commits[0].Subject != "add hello" {
		t.Fatalf("history wrong: %+v", v.commits)
	}
	if len(v.files) != 0 {
		t.Fatalf("tree not clean after commit: %+v", v.files)
	}

	// Modify again: unstaged diff is index vs worktree.
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshVCS()
	a.loadSelectedDiff()
	if v.diffFrom != "hello" || v.diffTo != "hello v2" || v.diffWt != "hello v2" {
		t.Fatalf("modify diff %q -> %q (wt %q)", v.diffFrom, v.diffTo, v.diffWt)
	}

	// Branch create + checkout round-trip.
	a.createBranch("feature")
	if v.branch != "feature" {
		t.Fatalf("createBranch landed on %q", v.branch)
	}
	a.checkoutBranch("main")
	if v.branch != "main" {
		t.Fatalf("checkout landed on %q", v.branch)
	}

	// The pane renders from the real repository.
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("render nil with the repository pane")
	}
}

func TestRepositoryPaneAttach(t *testing.T) {
	dir := seedVCSDemo(t)
	a := newApp()
	a.ws = newWorkspace(dir)
	a.newThread()
	a.repo.showRepo, a.repo.paneTab = true, 1
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	tst.Frame()
	if !tst.HasText("jobs/quota.go") {
		t.Fatalf("the change is not listed (texts: %v)", tst.Texts())
	}
	if err := tst.Click("Attach to conversation"); err != nil {
		t.Fatalf("click the attach button: %v", err)
	}
	tst.Frame()
	if len(a.thread.ctx) != 1 || a.thread.ctx[0].ID != "file:jobs/quota.go" {
		t.Fatalf("attach button did not attach the selected change: %+v", a.thread.ctx)
	}
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("render nil after attaching")
	}
}

func TestAttachFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApp()
	a.ws = newWorkspace(dir)
	a.newThread()

	if msg := a.attachFile(filepath.Join(dir, "a.go")); !strings.Contains(msg, "Attached") {
		t.Fatalf("attach toast %q", msg)
	}
	if len(a.thread.ctx) != 1 || a.thread.ctx[0].ID != "file:a.go" || a.thread.ctx[0].Kind != chat.ContextFile {
		t.Fatalf("context chips wrong: %+v", a.thread.ctx)
	}
	if msg := a.attachFile(filepath.Join(dir, "a.go")); !strings.Contains(msg, "Already") {
		t.Fatalf("duplicate attach toast %q", msg)
	}

	// Sending folds the file contents into the LLM message and clears the chips.
	a.thread.draft = "explain this"
	a.send()
	if a.thread.ctx != nil {
		t.Fatalf("attachments should clear after send: %+v", a.thread.ctx)
	}
	u := a.thread.rows[0]
	if !strings.Contains(u.text, "Attached: `a.go`") {
		t.Fatalf("visible row missing the attachment note: %q", u.text)
	}
	if !strings.Contains(u.llmText, "--- a.go ---") || !strings.Contains(u.llmText, "package a") {
		t.Fatalf("llm message missing the file contents: %q", u.llmText)
	}
	// The LLM history carries the folded text for user rows (historyMessages
	// prefers llmText); just check a user message exists.
	if len(a.historyMessages()) == 0 {
		t.Fatal("history has no messages after send")
	}
}

package main

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

func TestParseStatus(t *testing.T) {
	out := "## main...origin/main [ahead 1, behind 2]\n" +
		"M  staged.go\n" +
		" M work.go\n" +
		"MM both.go\n" +
		"A  new.go\n" +
		"?? note.txt\n" +
		"R  old.go -> moved.go\n" +
		" D gone.go\n"
	branch, ahead, behind, files, entries := parseStatus(out)
	if branch != "main" || ahead != 1 || behind != 2 {
		t.Fatalf("header parsed as %q +%d/-%d", branch, ahead, behind)
	}
	if len(files) != len(entries) {
		t.Fatalf("files and entries diverge: %d vs %d", len(files), len(entries))
	}
	// both.go appears twice: staged modified + unstaged modified.
	var both int
	for i, f := range files {
		if f.Path != "both.go" {
			continue
		}
		both++
		if !f.Staged || entries[i].staged {
			if f.Staged != entries[i].staged {
				t.Fatalf("both.go entry %d staged mismatch: %+v vs %+v", i, f, entries[i])
			}
		}
	}
	if both != 2 {
		t.Fatalf("both.go should appear staged and unstaged, got %d entries", both)
	}
	if files[0].Path != "staged.go" || !files[0].Staged || files[0].Status != git.GitModified {
		t.Fatalf("staged.go wrong: %+v", files[0])
	}
	if files[1].Path != "work.go" || files[1].Staged {
		t.Fatalf("work.go should be unstaged: %+v", files[1])
	}
	// Index order: staged.go, work.go, both.go ×2, new.go, note.txt,
	// moved.go, gone.go.
	if files[4].Path != "new.go" || !files[4].Staged || files[4].Status != git.GitAdded {
		t.Fatalf("new.go wrong: %+v", files[4])
	}
	if files[5].Status != git.GitUntracked || files[5].Staged {
		t.Fatalf("note.txt should be untracked: %+v", files[5])
	}
	if files[6].Path != "moved.go" || files[6].Status != git.GitRenamed || entries[6].oldPath != "old.go" {
		t.Fatalf("rename wrong: %+v / %+v", files[6], entries[6])
	}
	if files[7].Path != "gone.go" || files[7].Status != git.GitDeleted || files[7].Staged {
		t.Fatalf("deletion wrong: %+v", files[7])
	}
}

func TestParseBranchesAndLog(t *testing.T) {
	br := parseBranches("refs/heads/main\tmain\t*\torigin/main\t[ahead 2, behind 1]\n" +
		"refs/heads/feature\tfeature\t \t\t\n" +
		"refs/remotes/origin/main\torigin/main\t \t\t\n")
	if len(br) != 3 {
		t.Fatalf("got %d branches", len(br))
	}
	if !br[0].Current || br[0].Upstream != "origin/main" || br[0].Ahead != 2 || br[0].Behind != 1 {
		t.Fatalf("main wrong: %+v", br[0])
	}
	if br[1].Remote || br[2].Remote != true {
		t.Fatalf("remote flags wrong: %+v %+v", br[1], br[2])
	}

	lg := parseLog("\x1eabc123\x1fdef456\x1fAda\x1f1700000000\x1fRaise the cap\x1fHEAD -> main, tag: v1\n" +
		"\x1e999999\x1f\x1fGrace\x1f1700000100\x1fInit\n")
	if len(lg) != 2 {
		t.Fatalf("got %d commits", len(lg))
	}
	if lg[0].Hash != "abc123" || lg[0].Author != "Ada" || lg[0].Subject != "Raise the cap" {
		t.Fatalf("commit 0 wrong: %+v", lg[0])
	}
	if len(lg[0].Parents) != 1 || lg[0].Parents[0] != "def456" {
		t.Fatalf("parents wrong: %+v", lg[0].Parents)
	}
	if len(lg[0].Refs) != 2 || lg[0].Refs[0] != "main" || lg[0].Refs[1] != "v1" {
		t.Fatalf("refs wrong: %+v", lg[0].Refs)
	}
	if lg[1].Subject != "Init" || lg[1].Parents != nil {
		t.Fatalf("root commit wrong: %+v", lg[1])
	}
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

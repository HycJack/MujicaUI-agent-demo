package vcs

import "testing"

func TestParseStatus(t *testing.T) {
	out := "## main...origin/main [ahead 1, behind 2]\n" +
		"M  staged.go\n" +
		" M work.go\n" +
		"MM both.go\n" +
		"A  new.go\n" +
		"?? note.txt\n" +
		"R  old.go -> moved.go\n" +
		" D gone.go\n"
	branch, ahead, behind, files, entries := ParseStatus(out)
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
		if f.Staged != entries[i].Staged {
			t.Fatalf("both.go entry %d staged mismatch: %+v vs %+v", i, f, entries[i])
		}
	}
	if both != 2 {
		t.Fatalf("both.go should appear staged and unstaged, got %d entries", both)
	}
	if files[0].Path != "staged.go" || !files[0].Staged || files[0].Status != GitModified {
		t.Fatalf("staged.go wrong: %+v", files[0])
	}
	if files[1].Path != "work.go" || files[1].Staged {
		t.Fatalf("work.go should be unstaged: %+v", files[1])
	}
	// Index order: staged.go, work.go, both.go ×2, new.go, note.txt,
	// moved.go, gone.go.
	if files[4].Path != "new.go" || !files[4].Staged || files[4].Status != GitAdded {
		t.Fatalf("new.go wrong: %+v", files[4])
	}
	if files[5].Status != GitUntracked || files[5].Staged {
		t.Fatalf("note.txt should be untracked: %+v", files[5])
	}
	if files[6].Path != "moved.go" || files[6].Status != GitRenamed || entries[6].OldPath != "old.go" {
		t.Fatalf("rename wrong: %+v / %+v", files[6], entries[6])
	}
	if files[7].Path != "gone.go" || files[7].Status != GitDeleted || files[7].Staged {
		t.Fatalf("deletion wrong: %+v", files[7])
	}
}

func TestParseBranchesAndLog(t *testing.T) {
	br := ParseBranches("refs/heads/main\tmain\t*\torigin/main\t[ahead 2, behind 1]\n" +
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

	lg := ParseLog("\x1eabc123\x1fdef456\x1fAda\x1f1700000000\x1fRaise the cap\x1fHEAD -> main, tag: v1\n" +
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

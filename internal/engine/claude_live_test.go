package engine

// claude_live_test.go is the opt-in probe against a REAL claude CLI: it
// only runs when CRUX_LIVE_CLAUDE=1 (and the CLI exists on PATH), so CI
// and machines without the CLI skip it. It is the last-mile check that
// the wire shapes, the CRLF line endings, the spawn flags and the
// resident-CLI reaping all hold against the real binary.

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestLiveClaudeSmoke asks the real CLI for one word and checks the
// round trip: session init, streamed text, and the result note.
func TestLiveClaudeSmoke(t *testing.T) {
	if os.Getenv("CRUX_LIVE_CLAUDE") != "1" {
		t.Skip("set CRUX_LIVE_CLAUDE=1 to run the live claude probe")
	}
	if testing.Short() {
		t.Skip("short mode")
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("no claude CLI on PATH")
	}

	var text, session string
	var note string
	// The turn budget is generous: a CLI with heavy SessionStart hooks
	// and a slow first token can take minutes.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	err = RunClaude(ctx, ClaudeConfig{
		Bin:   bin,
		Model: "sonnet",
		Mode:  ModeFull,
	}, "Reply with exactly: pong", Callbacks{
		OnText:    func(s string) { text = s },
		OnSession: func(id string) { session = id },
		OnNote:    func(s string) { note = s },
	})
	if err != nil {
		t.Fatalf("live run: %v", err)
	}
	if session == "" {
		t.Fatal("no session id reported")
	}
	if strings.TrimSpace(text) == "" {
		t.Fatalf("no text streamed (note=%q) — is the CLI's endpoint reachable?", note)
	}
	if !strings.Contains(text, "pong") {
		t.Fatalf("unexpected reply: %q (note=%q)", text, note)
	}
	t.Logf("text=%q note=%q session=%q", text, note, session)
}

// TestLiveClaudeResume pins the two-turn contract: the first turn's
// session id rides --resume on the second, and the CLI actually
// continues that conversation — the fact planted in turn one is still
// known in turn two. One session per THREAD, not one per message.
func TestLiveClaudeResume(t *testing.T) {
	if os.Getenv("CRUX_LIVE_CLAUDE") != "1" {
		t.Skip("set CRUX_LIVE_CLAUDE=1 to run the live claude probe")
	}
	if testing.Short() {
		t.Skip("short mode")
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("no claude CLI on PATH")
	}
	dir := t.TempDir()

	var first, second string
	var session1, session2 string
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	err = RunClaude(ctx, ClaudeConfig{Bin: bin, Mode: ModeFull, Workdir: dir},
		"My favorite fruit is durian. Reply with exactly: OK", Callbacks{
			OnText:    func(s string) { first = s },
			OnSession: func(id string) { session1 = id },
		})
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if session1 == "" {
		t.Fatal("turn 1 reported no session id")
	}
	err = RunClaude(ctx, ClaudeConfig{Bin: bin, Mode: ModeFull, Workdir: dir, SessionID: session1},
		"What is my favorite fruit? Answer with only the fruit name.", Callbacks{
			OnText:    func(s string) { second = s },
			OnSession: func(id string) { session2 = id },
		})
	if err != nil {
		t.Fatalf("turn 2 (resume %s): %v", session1, err)
	}
	if !strings.Contains(strings.ToLower(second), "durian") {
		t.Fatalf("the resumed session forgot turn 1: turn1=%q turn2=%q session1=%s session2=%s",
			first, second, session1, session2)
	}
	t.Logf("turn1=%q turn2=%q session1=%s session2=%s (same=%v)",
		first, second, session1, session2, session1 == session2)
}

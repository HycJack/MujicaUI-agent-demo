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

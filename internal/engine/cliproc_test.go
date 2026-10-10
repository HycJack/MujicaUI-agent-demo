package engine

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTrunc(t *testing.T) {
	if got := Trunc("hello", 10); got != "hello" {
		t.Fatalf("Trunc short: %q", got)
	}
	if got := Trunc("hello world", 5); got != "hello…" {
		t.Fatalf("Trunc long: %q", got)
	}
	// Rune boundary: cutting counts runes, not bytes.
	if got := Trunc("中文测试", 3); got != "中文测…" {
		t.Fatalf("Trunc runes: %q", got)
	}
}

// TrimOutput keeps head AND tail: a command's errors live at the end of
// its output, so a head-only cut throws away exactly the half a failing
// run was about to show.
func TestTrimOutputHeadTail(t *testing.T) {
	var head, tail strings.Builder
	for i := 0; i < 100; i++ {
		head.WriteString("head line\n")
		tail.WriteString("tail line\n")
	}
	// The error sits at the very END: the kept tail (max/3) must carry it.
	s := head.String() + tail.String() + "THE ERROR\n"
	got := TrimOutput(s, 600)
	if !strings.Contains(got, "THE ERROR") || !strings.Contains(got, "truncated") {
		t.Fatalf("TrimOutput lost the tail that carries the error: %.80q", got)
	}
}

func TestTrimOutputShort(t *testing.T) {
	if got := TrimOutput("keep me\n", 64); got != "keep me" {
		t.Fatalf("TrimOutput short: %q", got)
	}
}

// A Windows command ends its line with CRLF; trimming only the \n leaves
// a stray \r on screen.
func TestTrimOutputCRLF(t *testing.T) {
	if got := TrimOutput("done\r\n", 64); got != "done" {
		t.Fatalf("CRLF not trimmed: %q", got)
	}
}

func TestShortArgs(t *testing.T) {
	if got := ShortArgs([]byte(`{"command":"go test","path":"x"}`)); got != "go test x" {
		t.Fatalf("ShortArgs: %q", got)
	}
}

func TestRedactSecrets(t *testing.T) {
	cases := []string{
		`curl -H "Authorization: Bearer sk-abcdef1234567890" host`,
		`export API_KEY=supersecretvalue1`,
		`token: ghp_0123456789abcdefghij`,
		`-password hunter2000`,
	}
	for _, s := range cases {
		got := Redact(s)
		if got == s ||
			strings.Contains(got, "sk-abcdef") ||
			strings.Contains(got, "supersecret") ||
			strings.Contains(got, "ghp_01234") ||
			strings.Contains(got, "hunter2000") {
			t.Fatalf("Redact leaked: %q", got)
		}
	}
	if got := Redact("plain path /usr/local no secrets"); got != "plain path /usr/local no secrets" {
		t.Fatalf("Redact over-eager: %q", got)
	}
}

func TestCompactedNotice(t *testing.T) {
	if s := CompactedNotice("claude", 25900, 5253); !strings.Contains(s, "25.9k") || !strings.Contains(s, "5.3k") {
		t.Fatalf("CompactedNotice: %q", s)
	}
	if s := CompactedNotice("claude", 0, 0); !strings.Contains(s, "compacted") {
		t.Fatalf("CompactedNotice without counts still speaks: %q", s)
	}
}

func TestCompactTokens(t *testing.T) {
	cases := map[int]string{999: "999", 25900: "25.9k", 258000: "258k", 2100000: "2.1M"}
	for n, want := range cases {
		if got := CompactTokens(n); got != want {
			t.Errorf("CompactTokens(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestShortSession(t *testing.T) {
	if got := ShortSession("0123456789abcdef"); got != "01234567" {
		t.Fatalf("ShortSession: %q", got)
	}
}

// lingerCommand is a command that ignores everything for d: the stand-in
// for a CLI that announced its result but refuses to exit.
func lingerCommand(d time.Duration) *exec.Cmd {
	secs := int(d.Seconds()) + 1
	if runtime.GOOS == "windows" {
		// ping waits (n-1) seconds; > nul keeps it silent.
		return exec.Command("cmd", "/C",
			fmt.Sprintf("ping -n %d 127.0.0.1 > nul", secs))
	}
	return exec.Command("sh", "-c", "sleep "+d.String())
}

// TestReapKillsLingerer: a CLI that announced the result but ignores
// stdin-close must be gone within the grace (spec §4.1 — the CLI stays
// resident waiting for the next input; the reaper must not hang the turn).
func TestReapKillsLingerer(t *testing.T) {
	old := ReapGrace
	ReapGrace = 150 * time.Millisecond
	defer func() { ReapGrace = old }()
	cmd := lingerCommand(3 * time.Second)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, killed := Reap(cmd, nil)
	if !killed {
		t.Fatal("the lingering CLI was not killed")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Reap took %v — the grace did not bound it", d)
	}
}

// TestReapFastExit: a CLI that already exited returns its wait error
// without any kill.
func TestReapFastExit(t *testing.T) {
	cmd := exec.Command("true")
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", "exit 0")
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitErr, killed := Reap(cmd, nil)
	if killed || waitErr != nil {
		t.Fatalf("Reap fast exit: killed=%v err=%v", killed, waitErr)
	}
}

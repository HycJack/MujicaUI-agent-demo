package engine

// claude_proc_test.go pins RunClaude at the process level with a fake
// CLI — a .cmd batch on Windows, a .sh script on Unix — so the spawn
// contract, the prompt-on-stdin wire, the approval round trip, and the
// reaping all run for real on every platform, this Windows machine
// included.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeClaudeScript writes a platform fake CLI from the given bodies and
// returns its path. The Windows body is .cmd batch syntax, the Unix body
// sh syntax; each test provides both.
func fakeClaudeScript(t *testing.T, win, sh []string) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "fake-claude.cmd")
		body := "@echo off\r\n" + strings.Join(win, "\r\n") + "\r\n"
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	path := filepath.Join(dir, "fake-claude")
	body := "#!/bin/sh\n" + strings.Join(sh, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// readSmall reads a small file the fake CLI wrote in the workdir.
func readSmall(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fake CLI file %s: %v (did the script write it?)", path, err)
	}
	return strings.TrimSpace(string(data))
}

func resultLine(sess string) string {
	return fmt.Sprintf(`{"type":"result","subtype":"success","is_error":false,"duration_ms":10,"total_cost_usd":0.0,"result":"ok","session_id":%q}`, sess)
}

// TestClaudeSpawnFlagsAndPrompt pins the spawn contract: the flags the
// CLI is launched with, and the prompt arriving as one user JSON line on
// stdin.
func TestClaudeSpawnFlagsAndPrompt(t *testing.T) {
	bin := fakeClaudeScript(t,
		[]string{
			`echo %* > args.txt`,
			`set /p promptline=`,
			`echo %promptline% > prompt.txt`,
			`echo {"type":"system","subtype":"init","session_id":"fake-sess-1"}`,
			`echo ` + resultLine("fake-sess-1"),
		},
		[]string{
			`printf '%s\n' "$*" > args.txt`,
			`read -r promptline`,
			`printf '%s\n' "$promptline" > prompt.txt`,
			`echo '{"type":"system","subtype":"init","session_id":"fake-sess-1"}'`,
			`echo ` + resultLine("fake-sess-1"),
		})
	workdir := t.TempDir()
	var sess, note string
	err := RunClaude(context.Background(), ClaudeConfig{
		Bin: bin, Model: "sonnet", SystemPrompt: "be-crux", Mode: ModeAgent,
		SessionID: "sess-9", Workdir: workdir,
	}, "hello", Callbacks{
		OnSession: func(id string) { sess = id },
		OnNote:    func(s string) { note = s },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if sess != "fake-sess-1" {
		t.Fatalf("session = %q", sess)
	}
	if note == "" {
		t.Fatal("the result note never fired")
	}
	args := readSmall(t, filepath.Join(workdir, "args.txt"))
	for _, want := range []string{
		"-p", "--output-format", "stream-json", "--verbose",
		"--input-format", "stream-json", "--include-partial-messages",
		"--permission-mode", "default", "--model", "sonnet",
		"--resume", "sess-9", "--append-system-prompt", "be-crux",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("args missing %q: %q", want, args)
		}
	}
	var p struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(readSmall(t, filepath.Join(workdir, "prompt.txt"))), &p); err != nil {
		t.Fatalf("prompt line is not JSON: %v", err)
	}
	if p.Type != "user" || p.Message.Role != "user" || len(p.Message.Content) != 1 || p.Message.Content[0].Text != "hello" {
		t.Fatalf("prompt line: %+v", p)
	}
}

// Full access runs the CLI with bypassPermissions and never asks.
func TestClaudeFullAccessBypasses(t *testing.T) {
	bin := fakeClaudeScript(t,
		[]string{`echo %* > args.txt`, `echo ` + resultLine("s1")},
		[]string{`printf '%s\n' "$*" > args.txt`, `echo ` + resultLine("s1")})
	workdir := t.TempDir()
	err := RunClaude(context.Background(), ClaudeConfig{Bin: bin, Mode: ModeFull, Workdir: workdir}, "hi", Callbacks{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if args := readSmall(t, filepath.Join(workdir, "args.txt")); !strings.Contains(args, "bypassPermissions") {
		t.Fatalf("full access did not bypass: %q", args)
	}
}

// The approval round trip rides the wire: the CLI asks can_use_tool, the
// host answers on stdin with a control_response the CLI can read back.
func TestClaudeApprovalWireRoundTrip(t *testing.T) {
	bin := fakeClaudeScript(t,
		[]string{
			`set /p promptjunk=`,
			`echo {"type":"system","subtype":"init","session_id":"wire-1"}`,
			`echo {"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls"}}]}}`,
			`echo {"type":"control_request","request_id":"rq1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`,
			`set /p resp=`,
			`echo %resp% > resp.txt`,
			`echo ` + resultLine("wire-1"),
		},
		[]string{
			`read -r promptjunk`,
			`echo '{"type":"system","subtype":"init","session_id":"wire-1"}'`,
			`echo '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls"}}]}}'`,
			`echo '{"type":"control_request","request_id":"rq1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}'`,
			`read -r resp`,
			`printf '%s\n' "$resp" > resp.txt`,
			`echo ` + resultLine("wire-1"),
		})
	workdir := t.TempDir()
	var toolStart string
	err := RunClaude(context.Background(), ClaudeConfig{
		Bin: bin, Mode: ModeAgent, Workdir: workdir,
		OnApproval: func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
			return ApprovalDecision{Approved: true}
		},
	}, "hi", Callbacks{
		OnToolStart: func(id, name, args string) { toolStart = name + " " + args },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(toolStart, "Bash") {
		t.Fatalf("toolStart = %q", toolStart)
	}
	// The wire shape: {"type":"control_response","response":{subtype,
	// request_id, "response":{behavior, updatedInput}}} — the envelope
	// carries the decision payload nested under its own "response" key.
	var resp struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Inner     struct {
				Behavior     string         `json:"behavior"`
				UpdatedInput map[string]any `json:"updatedInput"`
			} `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(readSmall(t, filepath.Join(workdir, "resp.txt"))), &resp); err != nil {
		t.Fatalf("resp line is not JSON: %v", err)
	}
	if resp.Type != "control_response" || resp.Response.Inner.Behavior != "allow" {
		t.Fatalf("resp: %+v", resp)
	}
	if resp.Response.Inner.UpdatedInput["command"] != "ls" {
		t.Fatalf("updatedInput: %+v", resp.Response.Inner.UpdatedInput)
	}
}

// A denial rides back as behavior=deny with the reason.
func TestClaudeApprovalWireDenied(t *testing.T) {
	bin := fakeClaudeScript(t,
		[]string{
			`set /p promptjunk=`,
			`echo {"type":"control_request","request_id":"rq2","request":{"subtype":"can_use_tool","tool_name":"Edit","input":{"file_path":"a.go"}}}`,
			`set /p resp=`,
			`echo %resp% > resp.txt`,
			`echo ` + resultLine("wire-2"),
		},
		[]string{
			`read -r promptjunk`,
			`echo '{"type":"control_request","request_id":"rq2","request":{"subtype":"can_use_tool","tool_name":"Edit","input":{"file_path":"a.go"}}}'`,
			`read -r resp`,
			`printf '%s\n' "$resp" > resp.txt`,
			`echo ` + resultLine("wire-2"),
		})
	workdir := t.TempDir()
	err := RunClaude(context.Background(), ClaudeConfig{
		Bin: bin, Mode: ModeAgent, Workdir: workdir,
		OnApproval: func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
			return ApprovalDecision{Reason: "not today"}
		},
	}, "hi", Callbacks{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var resp struct {
		Response struct {
			Inner struct {
				Behavior string `json:"behavior"`
				Message  string `json:"message"`
			} `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(readSmall(t, filepath.Join(workdir, "resp.txt"))), &resp); err != nil {
		t.Fatalf("resp: %v", err)
	}
	if resp.Response.Inner.Behavior != "deny" || resp.Response.Inner.Message != "not today" {
		t.Fatalf("resp: %+v", resp)
	}
}

// A CLI that announces the result but refuses to exit is reaped within
// the grace — the turn settles anyway (spec §4.1: the CLI stays resident
// waiting for the next input).
func TestClaudeReapsResidentCLI(t *testing.T) {
	old := ReapGrace
	ReapGrace = 150 * time.Millisecond
	defer func() { ReapGrace = old }()
	bin := fakeClaudeScript(t,
		[]string{`echo ` + resultLine("reap-1"), `ping -n 10 127.0.0.1 > nul`},
		[]string{`echo ` + resultLine("reap-1"), `sleep 10`})
	start := time.Now()
	err := RunClaude(context.Background(), ClaudeConfig{Bin: bin, Mode: ModeFull, Workdir: t.TempDir()}, "hi", Callbacks{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("the resident CLI held the turn for %v", d)
	}
}

// A run cut off before the CLI said anything at all reports itself as a
// stopped-before-answer, not as a clean empty turn.
func TestClaudeStopBeforeAnswer(t *testing.T) {
	bin := fakeClaudeScript(t,
		[]string{`ping -n 10 127.0.0.1 > nul`},
		[]string{`sleep 10`})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := RunClaude(ctx, ClaudeConfig{Bin: bin, Mode: ModeFull, Workdir: t.TempDir()}, "hi", Callbacks{})
	if err == nil || !strings.Contains(err.Error(), "stopped before it answered") {
		t.Fatalf("err: %v", err)
	}
}

// The CLI's stderr tail is the failure text when the run went nowhere.
func TestClaudeStderrTailIsError(t *testing.T) {
	bin := fakeClaudeScript(t,
		[]string{`echo boom 1>&2`, `exit /b 1`},
		[]string{`echo boom >&2`, `exit 1`})
	err := RunClaude(context.Background(), ClaudeConfig{Bin: bin, Mode: ModeFull, Workdir: t.TempDir()}, "hi", Callbacks{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err: %v", err)
	}
}

// A turn that reached its result line is SETTLED: the CLI may exit
// non-zero right after (its own error turn — the API error already rode
// the assistant message and the result note) and the exit status must
// not replace the reply. Found by the live probe against a real CLI
// whose endpoint was unreachable.
func TestClaudeResultSettlesDespiteExitCode(t *testing.T) {
	bin := fakeClaudeScript(t,
		[]string{
			`echo {"type":"assistant","message":{"content":[{"type":"text","text":"API Error: Connection error"}]}}`,
			`echo {"type":"result","subtype":"error_during_execution","is_error":true,"duration_ms":180545,"total_cost_usd":0.0,"result":"API Error: Connection error","session_id":"settled-1"}`,
			`exit /b 1`,
		},
		[]string{
			`echo '{"type":"assistant","message":{"content":[{"type":"text","text":"API Error: Connection error"}]}}'`,
			`echo '{"type":"result","subtype":"error_during_execution","is_error":true,"duration_ms":180545,"total_cost_usd":0.0,"result":"API Error: Connection error","session_id":"settled-1"}'`,
			`exit 1`,
		})
	var text, note string
	err := RunClaude(context.Background(), ClaudeConfig{Bin: bin, Mode: ModeFull, Workdir: t.TempDir()}, "hi", Callbacks{
		OnText: func(s string) { text = s },
		OnNote: func(s string) { note = s },
	})
	if err != nil {
		t.Fatalf("a settled turn must not fail on the CLI's exit status: %v", err)
	}
	if !strings.Contains(text, "API Error") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(note, "Stopped") {
		t.Fatalf("note = %q", note)
	}
}

// A missing binary is a setup error, not a run failure.
func TestClaudeMissingBin(t *testing.T) {
	err := RunClaude(context.Background(), ClaudeConfig{Mode: ModeFull, Workdir: t.TempDir()}, "hi", Callbacks{})
	if err == nil || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("err: %v", err)
	}
}

// scannerMaxLine pins the buffer the runner reads lines with.
func TestClaudeScannerBuffer(t *testing.T) {
	var big strings.Builder
	for i := 0; i < 200; i++ {
		big.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"chunk"}]}}`)
		big.WriteString("\n")
	}
	lines := 0
	sc := bufio.NewScanner(strings.NewReader(big.String()))
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	for sc.Scan() {
		lines++
	}
	if lines != 200 {
		t.Fatalf("lines = %d", lines)
	}
}

package main

// agenttools_test.go covers the real agent toolset: bash runs a real shell
// command in the workspace, read_file/write_file round-trip contents (the
// write reporting old/new for the review card), and the three card rows a
// tool call produces render headless.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/HycJack/pi-ai-go/core"
	muiagent "github.com/ZacharyZhang-NY/MujicaUI/agent"
	"github.com/ZacharyZhang-NY/MujicaUI/chat"
	"github.com/egoist/mygo/ui"
)

func TestAgentToolsBash(t *testing.T) {
	dir := t.TempDir()
	a := newApp()
	a.ws = newWorkspace(dir)
	tools := a.agentTools()
	var bash core.AgentTool
	for _, tl := range tools {
		if tl.Name == "bash" {
			bash = tl
		}
	}
	if bash.Execute == nil {
		t.Fatal("the bash tool is missing")
	}
	// A portable command: echo works under both cmd and sh.
	res, err := bash.Execute(context.Background(), "c1", json.RawMessage(`{"command":"echo atlas-ok"}`), nil)
	if err != nil {
		t.Fatalf("bash execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("bash result is an error: %+v", res)
	}
	if !strings.Contains(toolResultText(mustJSON(t, res)), "atlas-ok") {
		t.Fatalf("bash output missing marker: %+v", res)
	}
	// A failing command reports an error result with a non-zero exit.
	res, err = bash.Execute(context.Background(), "c2", json.RawMessage(`{"command":"exit 3"}`), nil)
	if err != nil {
		t.Fatalf("bash execute (failing): %v", err)
	}
	if !res.IsError {
		t.Fatal("a failing command should be an error result")
	}
	var details struct {
		Exit int `json:"exit"`
	}
	if err := json.Unmarshal(res.Details, &details); err != nil || details.Exit == 0 {
		t.Fatalf("exit code not reported: details=%s err=%v", res.Details, err)
	}
}

func TestAgentToolsReadWrite(t *testing.T) {
	dir := t.TempDir()
	a := newApp()
	a.ws = newWorkspace(dir)
	tools := a.agentTools()
	var read, write core.AgentTool
	for _, tl := range tools {
		switch tl.Name {
		case "read_file":
			read = tl
		case "write_file":
			write = tl
		}
	}
	if read.Execute == nil || write.Execute == nil {
		t.Fatal("the file tools are missing")
	}
	// Reading a missing file is an error result, not a panic.
	res, err := read.Execute(context.Background(), "r1", json.RawMessage(`{"path":"nope.txt"}`), nil)
	if err != nil || !res.IsError {
		t.Fatalf("missing file: err=%v isError=%v", err, res.IsError)
	}
	// Write, then read back.
	if res, err = write.Execute(context.Background(), "w1", json.RawMessage(`{"path":"sub/notes.txt","content":"hello tools"}`), nil); err != nil || res.IsError {
		t.Fatalf("write: err=%v isError=%v", err, res.IsError)
	}
	var d struct {
		Path string `json:"path"`
		Old  string `json:"old"`
		New  string `json:"new"`
	}
	if err := json.Unmarshal(res.Details, &d); err != nil {
		t.Fatalf("write details: %v", err)
	}
	if d.Old != "" || d.New != "hello tools" || !strings.HasSuffix(filepath.ToSlash(d.Path), "sub/notes.txt") {
		t.Fatalf("write details wrong: %+v", d)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "sub", "notes.txt")); err != nil || string(data) != "hello tools" {
		t.Fatalf("file not written: %q %v", data, err)
	}
	// Overwrite reports the previous content as Old.
	if res, err = write.Execute(context.Background(), "w2", json.RawMessage(`{"path":"sub/notes.txt","content":"v2"}`), nil); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(res.Details, &d); err != nil || d.Old != "hello tools" || d.New != "v2" {
		t.Fatalf("overwrite details wrong: %+v err=%v", d, err)
	}
	// Read back returns the new content.
	res, err = read.Execute(context.Background(), "r2", json.RawMessage(`{"path":"sub/notes.txt"}`), nil)
	if err != nil || res.IsError || !strings.Contains(toolResultText(mustJSON(t, res)), "v2") {
		t.Fatalf("read back: err=%v isError=%v res=%s", err, res.IsError, toolResultText(mustJSON(t, res)))
	}
}

// mustJSON marshals v or fails the test.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	buf, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

// The three tool card rows render headless with real toolRun data.
func TestAgentCardsRender(t *testing.T) {
	a := newApp()
	a.thread.rows = []row{
		{id: "u1", role: chat.MessageUser, text: "run the tests", at: time.Now()},
		{id: "t1", role: chat.MessageAssistant, kind: rowCommand, at: time.Now(),
			tool: &toolRun{
				callID: "c1", name: "bash", state: muiagent.AgentDone, dur: 120 * time.Millisecond,
				args: `{"command":"go test ./..."}`,
				run:  muiagent.CommandRun{Command: "go test ./...", Dir: ".", ExitCode: 0, Duration: 120 * time.Millisecond, Output: "ok"},
			}},
		{id: "t2", role: chat.MessageAssistant, kind: rowTools, at: time.Now(),
			tool: &toolRun{
				callID: "c2", name: "read_file", state: muiagent.AgentDone, dur: 5 * time.Millisecond,
				args: `{"path":"main.go"}`, result: `{"content":[{"type":"text","text":"package main"}]}`,
			}},
		{id: "t3", role: chat.MessageAssistant, kind: rowDiff, at: time.Now(),
			tool: &toolRun{
				callID: "c3", name: "write_file", state: muiagent.AgentDone, dur: 3 * time.Millisecond,
				args:   `{"path":"x.txt"}`,
				change: muiagent.FileChange{Path: "x.txt", Old: "a\n", New: "b\n"},
			}},
		{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: time.Now(),
			thinkText: "the tests pass", text: "All green."},
	}
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("agent cards render nil")
	}
}

// The shell wrapper matches the platform so the bash tool actually runs.
func TestShellWrapper(t *testing.T) {
	if runtime.GOOS == "windows" {
		if shell, flag := shellCommand(); shell != "cmd" || flag != "/C" {
			t.Fatalf("windows shell = %s %s", shell, flag)
		}
		return
	}
	if shell, flag := shellCommand(); shell != "sh" || flag != "-c" {
		t.Fatalf("unix shell = %s %s", shell, flag)
	}
}

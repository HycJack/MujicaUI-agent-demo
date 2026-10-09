package engine

// tools_test.go covers the real agent toolset: bash runs a real shell
// command in the workspace, read_file/write_file round-trip contents (the
// write reporting old/new for the review card), and the shell wrapper
// matches the platform.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/HycJack/pi-ai-go/core"
)

// toolByName finds one tool's Execute in the toolset rooted at workdir.
func toolByName(t *testing.T, workdir, name string) func(ctx context.Context, id string, params json.RawMessage, push func(json.RawMessage)) (core.AgentToolResult, error) {
	t.Helper()
	for _, tl := range Tools(workdir) {
		if tl.Name == name {
			return tl.Execute
		}
	}
	t.Fatalf("the %s tool is missing", name)
	return nil
}

// resultText extracts the first text block of a marshaled AgentToolResult.
func resultText(t *testing.T, v any) string {
	t.Helper()
	buf, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(buf, &r) == nil && len(r.Content) > 0 {
		return r.Content[0].Text
	}
	return string(buf)
}

func TestToolsBash(t *testing.T) {
	dir := t.TempDir()
	bash := toolByName(t, dir, "bash")
	// A portable command: echo works under both cmd and sh.
	res, err := bash(context.Background(), "c1", json.RawMessage(`{"command":"echo crux-ok"}`), nil)
	if err != nil {
		t.Fatalf("bash execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("bash result is an error: %+v", res)
	}
	if !strings.Contains(resultText(t, res), "crux-ok") {
		t.Fatalf("bash output missing marker: %+v", res)
	}
	// A failing command reports an error result with a non-zero exit.
	res, err = bash(context.Background(), "c2", json.RawMessage(`{"command":"exit 3"}`), nil)
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

func TestToolsReadWrite(t *testing.T) {
	dir := t.TempDir()
	read := toolByName(t, dir, "read_file")
	write := toolByName(t, dir, "write_file")
	// Reading a missing file is an error result, not a panic.
	res, err := read(context.Background(), "r1", json.RawMessage(`{"path":"nope.txt"}`), nil)
	if err != nil || !res.IsError {
		t.Fatalf("missing file: err=%v isError=%v", err, res.IsError)
	}
	// Write, then read back.
	if res, err = write(context.Background(), "w1", json.RawMessage(`{"path":"sub/notes.txt","content":"hello tools"}`), nil); err != nil || res.IsError {
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
	if res, err = write(context.Background(), "w2", json.RawMessage(`{"path":"sub/notes.txt","content":"v2"}`), nil); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(res.Details, &d); err != nil || d.Old != "hello tools" || d.New != "v2" {
		t.Fatalf("overwrite details wrong: %+v err=%v", d, err)
	}
	// Read back returns the new content.
	res, err = read(context.Background(), "r2", json.RawMessage(`{"path":"sub/notes.txt"}`), nil)
	if err != nil || res.IsError || !strings.Contains(resultText(t, res), "v2") {
		t.Fatalf("read back: err=%v isError=%v res=%s", err, res.IsError, resultText(t, res))
	}
}

// The shell wrapper matches the platform so the bash tool actually runs.
func TestShellWrapper(t *testing.T) {
	if runtime.GOOS == "windows" {
		if shell, flag := ShellCommand(); shell != "cmd" || flag != "/C" {
			t.Fatalf("windows shell = %s %s", shell, flag)
		}
		return
	}
	if shell, flag := ShellCommand(); shell != "sh" || flag != "-c" {
		t.Fatalf("unix shell = %s %s", shell, flag)
	}
}

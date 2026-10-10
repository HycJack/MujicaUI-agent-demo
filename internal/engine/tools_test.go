package engine

// tools_test.go covers the real agent toolset: bash runs a real shell
// command in the workspace, read_file/write_file round-trip contents (the
// write reporting old/new for the review card), and the shell wrapper
// matches the platform.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/HycJack/pi-ai-go/core"
)

// toolByName finds one tool's Execute in the full-access toolset rooted
// at workdir (the gate's zero-surprise form).
func toolByName(t *testing.T, workdir, name string) func(ctx context.Context, id string, params json.RawMessage, push func(json.RawMessage)) (core.AgentToolResult, error) {
	t.Helper()
	for _, tl := range Tools(workdir, Gate{Mode: ModeFull}) {
		if tl.Name == name {
			return tl.Execute
		}
	}
	t.Fatalf("the %s tool is missing", name)
	return nil
}

// gateTools builds a gated toolset with a recording approval callback
// (nil decisions deny and record the ask).
func gateTools(t *testing.T, workdir string, mode Mode, decide ApprovalFunc) []core.AgentTool {
	t.Helper()
	return Tools(workdir, Gate{
		Mode:            mode,
		Sandbox:         NewSandbox(),
		OnApproval:      decide,
		ApprovalTimeout: 60 * time.Millisecond,
	})
}

// callTool executes one tool by name and returns its first text block
// and error flag.
func callTool(t *testing.T, tools []core.AgentTool, name, args string) (string, bool) {
	t.Helper()
	for _, tl := range tools {
		if tl.Name != name {
			continue
		}
		res, err := tl.Execute(context.Background(), "call1", json.RawMessage(args), nil)
		if err != nil {
			t.Fatalf("%s execute: %v", name, err)
		}
		return resultText(t, res), res.IsError
	}
	t.Fatalf("the %s tool is missing", name)
	return "", false
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

// --- the gate (spec §2.1) ---

// Read-only denies bash and write_file without asking anyone; read_file
// stays allowed — that is the point of the mode.
func TestGateReadOnlyDeniesWithoutAsking(t *testing.T) {
	dir := t.TempDir()
	tools := gateTools(t, dir, ModeReadOnly, func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		t.Fatalf("read-only asked: %+v", req)
		return ApprovalDecision{}
	})
	if text, isErr := callTool(t, tools, "bash", `{"command":"echo hi"}`); !isErr || !strings.Contains(text, "read-only") {
		t.Fatalf("bash under read-only: err=%v text=%q", isErr, text)
	}
	if _, isErr := callTool(t, tools, "write_file", `{"path":"a.txt","content":"x"}`); !isErr {
		t.Fatal("write_file under read-only was allowed")
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("read me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if text, isErr := callTool(t, tools, "read_file", `{"path":"a.txt"}`); isErr || !strings.Contains(text, "read me") {
		t.Fatalf("read_file under read-only: err=%v text=%q", isErr, text)
	}
}

// On Windows the agent-mode ask approves a command and it then runs;
// denying blocks it with the reason fed back to the model.
func TestGateAskApprovesAndRuns(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the ask path needs a platform without an OS sandbox backend")
	}
	dir := t.TempDir()
	var asked []ApprovalRequest
	tools := gateTools(t, dir, ModeAgent, func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		asked = append(asked, req)
		return ApprovalDecision{Approved: true}
	})
	if text, isErr := callTool(t, tools, "bash", `{"command":"echo gated"}`); isErr || !strings.Contains(text, "gated") {
		t.Fatalf("approved bash did not run: err=%v text=%q", isErr, text)
	}
	if len(asked) != 1 || asked[0].Tool != "bash" || !strings.Contains(asked[0].Summary, "echo gated") {
		t.Fatalf("asked = %+v", asked)
	}
	// The ASK is redacted — the card shows a masked command; the tool's
	// own output is the command's real result, unredacted.
	if _, isErr := callTool(t, tools, "bash", `{"command":"echo secret sk-abcdef1234567890"}`); isErr {
		t.Fatal("the second ask was denied")
	}
	if strings.Contains(asked[1].Summary, "sk-abcdef1234567890") {
		t.Fatalf("the ask must be redacted: %q", asked[1].Summary)
	}
}

func TestGateAskDeniedBlocks(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the ask path needs a platform without an OS sandbox backend")
	}
	dir := t.TempDir()
	tools := gateTools(t, dir, ModeAgent, func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		return ApprovalDecision{Reason: "the user denied this call"}
	})
	if text, isErr := callTool(t, tools, "bash", `{"command":"echo nope"}`); !isErr || !strings.Contains(text, "the user denied this call") {
		t.Fatalf("denied bash ran anyway: err=%v text=%q", isErr, text)
	}
}

// A nil approval handler denies the ask — headless runs fail closed.
func TestGateNilApprovalDenies(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the ask path needs a platform without an OS sandbox backend")
	}
	tools := gateTools(t, t.TempDir(), ModeAgent, nil)
	if _, isErr := callTool(t, tools, "bash", `{"command":"echo x"}`); !isErr {
		t.Fatal("nil OnApproval must deny an ask")
	}
}

// A decision that arrives after the deadline is not a decision.
func TestGateApprovalTimeoutDenies(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the ask path needs a platform without an OS sandbox backend")
	}
	tools := gateTools(t, t.TempDir(), ModeAgent, func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		<-ctx.Done() // never answers in time
		return ApprovalDecision{Approved: true}
	})
	if text, isErr := callTool(t, tools, "bash", `{"command":"echo x"}`); !isErr || !strings.Contains(text, "approval timed out") {
		t.Fatalf("timeout must deny: err=%v text=%q", isErr, text)
	}
}

// Agent-mode writes stay confined to the workspace: an absolute path
// outside it is refused, a relative one lands inside.
func TestGateWriteConfinedToWorkdir(t *testing.T) {
	dir := t.TempDir()
	tools := gateTools(t, dir, ModeAgent, nil)
	outside := filepath.Join(t.TempDir(), "escape.txt")
	if _, isErr := callTool(t, tools, "write_file", fmt.Sprintf(`{"path":%q,"content":"x"}`, outside)); !isErr {
		t.Fatal("agent mode allowed a write outside the workspace")
	}
	if text, isErr := callTool(t, tools, "write_file", `{"path":"inside.txt","content":"x"}`); isErr {
		t.Fatalf("agent mode refused a workspace write: %s", text)
	}
	if _, err := os.Stat(filepath.Join(dir, "inside.txt")); err != nil {
		t.Fatalf("inside.txt not written: %v", err)
	}
}

// fakeSandbox reports available and execs without confinement, so the
// sandboxed-bash wiring (Dir, HOME/TMPDIR into the scratch, output
// capture, details) is testable on every platform.
type fakeSandbox struct{}

func (fakeSandbox) Available() bool { return true }
func (fakeSandbox) Command(ctx context.Context, b Boundary, name string, arg ...string) (*exec.Cmd, func(), error) {
	return exec.CommandContext(ctx, name, arg...), nil, nil
}

// Agent-mode bash with an available sandbox backend routes through the
// sandbox path: the child sees the scratch as HOME and the workdir as
// its directory, and the result carries sandboxed=true.
func TestGateSandboxedBashWiring(t *testing.T) {
	dir := t.TempDir()
	tools := Tools(dir, Gate{Mode: ModeAgent, Sandbox: fakeSandbox{}})
	echo := `echo HOME=$HOME`
	if runtime.GOOS == "windows" {
		echo = `echo HOME=%HOME%`
	}
	res := bashResult(t, tools, fmt.Sprintf(`{"command":%q}`, echo))
	if res.isErr {
		t.Fatalf("sandboxed bash failed: %s", res.text)
	}
	if !strings.Contains(res.text, "crux-sbx-") {
		t.Fatalf("child HOME is not the scratch: %q", res.text)
	}
	if !bytesContains(res.details, []byte(`"sandboxed":true`)) {
		t.Fatalf("details missing sandboxed flag: %s", res.details)
	}
}

type toolOutcome struct {
	text    string
	details []byte
	isErr   bool
}

func bashResult(t *testing.T, tools []core.AgentTool, args string) toolOutcome {
	t.Helper()
	for _, tl := range tools {
		if tl.Name != "bash" {
			continue
		}
		res, err := tl.Execute(context.Background(), "sbx1", json.RawMessage(args), nil)
		if err != nil {
			t.Fatalf("bash execute: %v", err)
		}
		return toolOutcome{text: resultText(t, res), details: res.Details, isErr: res.IsError}
	}
	t.Fatal("the bash tool is missing")
	return toolOutcome{}
}

func bytesContains(b, sub []byte) bool {
	return len(sub) > 0 && len(b) >= len(sub) && strings.Contains(string(b), string(sub))
}

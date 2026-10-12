package engine

// tools_test.go covers the real agent toolset: bash runs a real shell
// command in the workspace, read_file/write_file round-trip contents (the
// write reporting old/new for the review card), the shell wrapper matches
// the platform, and the gate (spec §2.1) denies, confines and asks per
// the mode table.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/HycJack/pi-ai-go/core"

	"crux-agent/internal/sandbox"
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
// (nil decisions deny and record the ask). The provider is the
// no-backend stand-in, so agent-mode bash lands on the ask path on
// every platform.
func gateTools(t *testing.T, workdir string, mode Mode, decide ApprovalFunc) []core.AgentTool {
	t.Helper()
	return Tools(workdir, Gate{
		Mode:            mode,
		Sandbox:         failingSandbox{},
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

// Agent-mode bash with no usable sandbox backend (the honesty error)
// lands on an approval card: an approval runs the command once; the ask
// itself is redacted.
func TestGateAskApprovesAndRuns(t *testing.T) {
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
	tools := gateTools(t, t.TempDir(), ModeAgent, nil)
	if _, isErr := callTool(t, tools, "bash", `{"command":"echo x"}`); !isErr {
		t.Fatal("nil OnApproval must deny an ask")
	}
}

// A decision that arrives after the deadline is not a decision.
func TestGateApprovalTimeoutDenies(t *testing.T) {
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

// --- the sandbox wiring ---

// failingSandbox is the no-backend stand-in: it refuses to build any
// boundary the way a platform without an OS sandbox does (the honesty
// error).
type failingSandbox struct{}

func (failingSandbox) Command(ctx context.Context, b sandbox.Boundary, name string, arg ...string) (*exec.Cmd, func(), error) {
	return nil, nil, errors.New("sandboxed execution is not implemented on this platform")
}

// fakeSandbox execs without confinement, so the sandboxed-bash wiring
// (Dir, HOME/TMPDIR into the scratch, output capture, details) is
// testable on every platform.
type fakeSandbox struct{}

func (fakeSandbox) Command(ctx context.Context, b sandbox.Boundary, name string, arg ...string) (*exec.Cmd, func(), error) {
	return exec.CommandContext(ctx, name, arg...), nil, nil
}

// Agent-mode bash with a working sandbox backend routes through the
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
	if !strings.Contains(res.text, "crux-sandbox-") {
		t.Fatalf("child HOME is not the scratch: %q", res.text)
	}
	if !strings.Contains(res.details, `"sandboxed":true`) {
		t.Fatalf("details missing sandboxed flag: %s", res.details)
	}
}

// With the sandbox provider off (nil), agent-mode bash runs plainly —
// the user turned the boundary off in settings.
func TestGateSandboxOffRunsPlain(t *testing.T) {
	tools := Tools(t.TempDir(), Gate{Mode: ModeAgent})
	res := bashResult(t, tools, `{"command":"echo plain"}`)
	if res.isErr || !strings.Contains(res.text, "plain") {
		t.Fatalf("plain agent bash failed: err=%v text=%q", res.isErr, res.text)
	}
	if strings.Contains(res.details, `"sandboxed":true`) {
		t.Fatalf("unsandboxed run reported itself sandboxed: %s", res.details)
	}
}

// TestToolsBashSandboxed asserts the wiring against the real provider:
// with the sandbox on, agent-mode bash still runs the command (inside
// the boundary) and reports exit codes; the platform's grant
// enforcement itself is covered by the sandbox package's live tests.
func TestToolsBashSandboxed(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("no sandbox backend on this platform")
	}
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("bwrap"); err != nil {
			t.Skip("no bwrap on PATH")
		}
	}
	dir := t.TempDir()
	tools := Tools(dir, Gate{Mode: ModeAgent, Sandbox: sandbox.New()})
	res, err := bashToolExecute(t, tools, `{"command":"echo sandbox-ok"}`)
	if err != nil {
		t.Fatalf("sandboxed bash execute: %v", err)
	}
	if res.isErr || !strings.Contains(res.text, "sandbox-ok") {
		t.Fatalf("sandboxed bash broken: isError=%v out=%s", res.isErr, res.text)
	}
	// A failing command still reports its exit code through the wrapper.
	res, err = bashToolExecute(t, tools, `{"command":"exit 7"}`)
	if err != nil || !res.isErr {
		t.Fatalf("sandboxed failing command: err=%v isError=%v", err, res.isErr)
	}
	var details struct {
		Exit int `json:"exit"`
	}
	if err := json.Unmarshal([]byte(res.details), &details); err != nil || details.Exit != 7 {
		t.Fatalf("sandboxed exit code not reported: details=%s err=%v", res.details, err)
	}
}

// TestSandboxProviderWiring asserts the switch: off is nil (plain child
// processes), on is the platform provider.
func TestSandboxProviderWiring(t *testing.T) {
	if SandboxProvider(false) != nil {
		t.Fatal("sandbox off must not install a provider")
	}
	if SandboxProvider(true) == nil {
		t.Fatal("sandbox on must install a provider")
	}
}

type toolOutcome struct {
	text    string
	details string
	isErr   bool
}

func bashResult(t *testing.T, tools []core.AgentTool, args string) toolOutcome {
	t.Helper()
	res, err := bashToolExecute(t, tools, args)
	if err != nil {
		t.Fatalf("bash execute: %v", err)
	}
	return res
}

func bashToolExecute(t *testing.T, tools []core.AgentTool, args string) (toolOutcome, error) {
	for _, tl := range tools {
		if tl.Name != "bash" {
			continue
		}
		res, err := tl.Execute(context.Background(), "sbx1", json.RawMessage(args), nil)
		if err != nil {
			return toolOutcome{}, err
		}
		return toolOutcome{text: resultText(t, res), details: string(res.Details), isErr: res.IsError}, nil
	}
	return toolOutcome{}, errors.New("the bash tool is missing")
}

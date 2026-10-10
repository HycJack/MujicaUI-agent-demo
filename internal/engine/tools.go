package engine

// tools.go defines the tools the agent loop can call, backed by pi-ai-go's
// DefaultExecutionEnv rooted at the workspace. bash runs a real shell
// command, read_file returns file contents, write_file writes a file and
// reports the before/after for the diff card. Everything is bounded —
// outputs and contents are capped so a runaway command cannot flood the
// model context. Tools(workdir, Gate) wraps the same three tools in the
// permission gate (spec §2.1): full access runs them ungated, read-only
// and agent modes deny or confine per the table, and agent-mode bash on
// a platform without an OS sandbox backend lands on an approval card.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/HycJack/pi-ai-go/core"
)

// Gate carries the permission context into the toolset (spec §2.2). The
// zero value fails closed to read-only: a caller that builds tools
// without thinking about the gate gets the safest mode.
type Gate struct {
	Mode            Mode
	Sandbox         Sandbox      // nil → treated as unavailable
	OnApproval      ApprovalFunc // nil → an ask settles as a denial
	ApprovalTimeout time.Duration
}

const (
	toolOutCap  = 8 * 1024  // per-tool result text cap
	toolFileCap = 64 * 1024 // read_file content cap
)

// ungatedTools is the original toolset: every call runs, nothing asks.
func ungatedTools(workdir string) []core.AgentTool {
	return []core.AgentTool{
		{
			Name:  "bash",
			Label: "Run command",
			Description: "Run a shell command in the workspace root and return its combined output. " +
				"Use for builds, tests, git and other inspection or build steps.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {"command": {"type": "string", "description": "The shell command line to run"}},
  "required": ["command"]
}`),
			Execute: func(ctx context.Context, _ string, params json.RawMessage, _ func(json.RawMessage)) (core.AgentToolResult, error) {
				return toolBash(ctx, workdir, params)
			},
		},
		{
			Name:  "read_file",
			Label: "Read file",
			Description: "Read a text file from the workspace and return its contents " +
				"(relative paths resolve against the workspace root).",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {"path": {"type": "string", "description": "File path, relative to the workspace root"}},
  "required": ["path"]
}`),
			Execute: func(ctx context.Context, _ string, params json.RawMessage, _ func(json.RawMessage)) (core.AgentToolResult, error) {
				return toolRead(workdir, params)
			},
		},
		{
			Name:  "write_file",
			Label: "Write file",
			Description: "Create or overwrite a text file in the workspace with the given content. " +
				"Always pass the complete final file content.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path, relative to the workspace root"},
    "content": {"type": "string", "description": "The complete new file content"}
  },
  "required": ["path", "content"]
}`),
			Execute: func(ctx context.Context, _ string, params json.RawMessage, _ func(json.RawMessage)) (core.AgentToolResult, error) {
				return toolWrite(workdir, params)
			},
		},
	}
}

// Tools builds the toolset the agent loop runs with, rooted at workdir
// and gated by g. ModeFull is the original ungated behavior.
func Tools(workdir string, g Gate) []core.AgentTool {
	if g.Mode == ModeFull {
		return ungatedTools(workdir)
	}
	sb := g.Sandbox
	sandboxOK := sb != nil && sb.Available()
	return []core.AgentTool{
		{
			Name:  "bash",
			Label: "Run command",
			Description: "Run a shell command in the workspace root and return its combined output. " +
				"Use for builds, tests, git and other inspection or build steps.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {"command": {"type": "string", "description": "The shell command line to run"}},
  "required": ["command"]
}`),
			Execute: func(ctx context.Context, _ string, params json.RawMessage, _ func(json.RawMessage)) (core.AgentToolResult, error) {
				return gatedBash(ctx, workdir, params, g, sb, sandboxOK)
			},
		},
		{
			Name:  "read_file",
			Label: "Read file",
			Description: "Read a text file from the workspace and return its contents " +
				"(relative paths resolve against the workspace root).",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {"path": {"type": "string", "description": "File path, relative to the workspace root"}},
  "required": ["path"]
}`),
			Execute: func(_ context.Context, _ string, params json.RawMessage, _ func(json.RawMessage)) (core.AgentToolResult, error) {
				return gatedRead(workdir, params)
			},
		},
		{
			Name:  "write_file",
			Label: "Write file",
			Description: "Create or overwrite a text file in the workspace with the given content. " +
				"Always pass the complete final file content.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path, relative to the workspace root"},
    "content": {"type": "string", "description": "The complete new file content"}
  },
  "required": ["path", "content"]
}`),
			Execute: func(_ context.Context, _ string, params json.RawMessage, _ func(json.RawMessage)) (core.AgentToolResult, error) {
				return gatedWrite(workdir, params, g)
			},
		},
	}
}

// ask settles one approval under its own timeout. A nil handler denies
// (headless fail-closed); an answer that arrives after the context ended
// is not an answer — the timeout wins.
func ask(ctx context.Context, g Gate, req ApprovalRequest) ApprovalDecision {
	if g.OnApproval == nil {
		return ApprovalDecision{Reason: "no approval handler is configured"}
	}
	actx, cancel := ApprovalContext(ctx, g.ApprovalTimeout)
	defer cancel()
	d := g.OnApproval(actx, req)
	if actx.Err() != nil {
		return ApprovalDecision{Reason: ApprovalReason(actx)}
	}
	return d
}

// gatedBash runs the bash tool through the gate: denied in read-only;
// asked in agent mode on platforms without an OS sandbox backend (the
// approval card IS the boundary there); sandboxed in agent mode where a
// backend exists; plain in full access.
func gatedBash(ctx context.Context, workdir string, params json.RawMessage, g Gate, sb Sandbox, sandboxOK bool) (core.AgentToolResult, error) {
	p, err := decodeParams(params)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	if strings.TrimSpace(p.Command) == "" {
		return textResult("a command is required", true), nil
	}
	switch gateResolve("bash", g.Mode, sandboxOK) {
	case PermDeny:
		return textResult(denialText("bash", g.Mode), true), nil
	case PermAsk:
		d := ask(ctx, g, ApprovalRequest{
			Tool:    "bash",
			Summary: "$ " + Trunc(Redact(p.Command), 160),
			Reason:  "agent mode asks before each command on this platform",
		})
		if !d.Approved {
			return textResult("permission denied (bash): "+d.Reason, true), nil
		}
		return toolBash(ctx, workdir, params)
	default:
		if g.Mode == ModeAgent && sandboxOK {
			return runSandboxed(ctx, sb, workdir, p.Command)
		}
		return toolBash(ctx, workdir, params)
	}
}

// runSandboxed executes one command inside the sandbox boundary: the
// workdir read-write, a fresh scratch as the child's HOME/TMPDIR,
// network denied. The child inherits the environment minus HOME and
// TMPDIR, which point into the scratch.
func runSandboxed(ctx context.Context, sb Sandbox, workdir, command string) (core.AgentToolResult, error) {
	scratch, err := os.MkdirTemp("", "crux-sbx-")
	if err != nil {
		return textResult("sandbox: "+err.Error(), true), nil
	}
	defer os.RemoveAll(scratch)
	_ = os.MkdirAll(filepath.Join(scratch, "tmp"), 0o700)
	shell, flag := ShellCommand()
	cmd, _, err := sb.Command(ctx, Boundary{Workdir: workdir, Scratch: scratch, Network: "deny"}, shell, flag, command)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), "HOME="+scratch, "TMPDIR="+filepath.Join(scratch, "tmp"))
	out, execErr := cmd.CombinedOutput()
	text := TrimOutput(string(out), toolOutCap)
	if ctx.Err() != nil {
		text += "\n(the command was stopped — its outcome is unknown)"
	}
	details, _ := json.Marshal(map[string]any{"command": command, "exit": ExitCode(execErr), "sandboxed": true})
	res := textResult(text, execErr != nil)
	res.Details = details
	return res, nil
}

// gatedRead lets reads through (both gated modes allow read_file), minus
// the credential stores — what is a secret as a file is a secret through
// a shell command too, and the sandbox masks the same list.
func gatedRead(workdir string, params json.RawMessage) (core.AgentToolResult, error) {
	p, err := decodeParams(params)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	abs, err := resolveToolPath(workdir, p.Path)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	if home, herr := os.UserHomeDir(); herr == nil && credentialDenied(abs, home) {
		return textResult("permission denied (read_file): "+p.Path+
			" is a credential store; ask the user to switch to Full Access if it is truly needed", true), nil
	}
	return toolRead(workdir, params)
}

// gatedWrite denies in read-only and confines agent-mode writes to the
// workspace (an absolute path outside it is refused).
func gatedWrite(workdir string, params json.RawMessage, g Gate) (core.AgentToolResult, error) {
	p, err := decodeParams(params)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	if gateResolve("write_file", g.Mode, false) == PermDeny {
		return textResult(denialText("write_file", g.Mode), true), nil
	}
	abs, err := resolveToolPath(workdir, p.Path)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	if !withinWorkdir(workdir, abs) {
		return textResult("permission denied (write_file): "+p.Path+
			" is outside the workspace; write inside it, or ask the user to switch the approval mode", true), nil
	}
	return toolWrite(workdir, params)
}

// withinWorkdir reports whether abs is the workdir or under it (both
// cleaned; the comparison is textual, so a path differing only by case
// on Windows reads as outside — a conservative reading).
func withinWorkdir(workdir, abs string) bool {
	w, a := filepath.Clean(workdir), filepath.Clean(abs)
	return a == w || strings.HasPrefix(a, w+string(filepath.Separator))
}

// toolParams is the decoded form of the tool arguments.
type toolParams struct {
	Command string `json:"command"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

func decodeParams(raw json.RawMessage) (toolParams, error) {
	var p toolParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, fmt.Errorf("bad arguments: %w", err)
	}
	return p, nil
}

// resolveToolPath resolves a tool path against the workspace root and
// rejects empty results.
func resolveToolPath(workdir, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("a file path is required")
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return filepath.Join(workdir, filepath.FromSlash(p)), nil
}

// textResult builds a one-block AgentToolResult.
func textResult(text string, isErr bool) core.AgentToolResult {
	if text == "" {
		text = "(no output)"
	}
	if len(text) > toolOutCap {
		text = text[:toolOutCap] + "\n… (truncated)"
	}
	return core.AgentToolResult{
		Content: []core.ContentBlock{core.TextContent{Type: "text", Text: text}},
		IsError: isErr,
	}
}

// toolBash runs a shell command in the workspace. The command line goes
// through the platform shell so pipes and redirections work.
func toolBash(_ context.Context, workdir string, params json.RawMessage) (core.AgentToolResult, error) {
	p, err := decodeParams(params)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	if strings.TrimSpace(p.Command) == "" {
		return textResult("a command is required", true), nil
	}
	env := core.NewDefaultExecutionEnvWithDir(workdir)
	shell, flag := ShellCommand()
	stdout, stderr, execErr := env.Exec(shell, []string{flag, p.Command}, workdir)
	out := strings.TrimRight(stdout, "\n")
	if strings.TrimSpace(stderr) != "" {
		if out != "" {
			out += "\n"
		}
		out += strings.TrimRight(stderr, "\n")
	}
	details, _ := json.Marshal(map[string]any{"command": p.Command, "exit": ExitCode(execErr)})
	res := textResult(out, execErr != nil)
	res.Details = details
	return res, nil
}

// ShellCommand picks the platform shell wrapper for the bash tool.
func ShellCommand() (shell, flag string) {
	if runtime.GOOS == "windows" {
		return "cmd", "/C"
	}
	return "sh", "-c"
}

// ExitCode maps an Exec error to a process exit code (1 when unknown).
// Anything in the error chain carrying an ExitCode() int (os/exec's
// ExitError and pi-ai-go's wrapped forms) counts.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee interface{ ExitCode() int }
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}

// toolRead returns a workspace file's contents (capped).
func toolRead(workdir string, params json.RawMessage) (core.AgentToolResult, error) {
	p, err := decodeParams(params)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	abs, err := resolveToolPath(workdir, p.Path)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	data, rerr := os.ReadFile(abs)
	if rerr != nil {
		return textResult("read failed: "+rerr.Error(), true), nil
	}
	text := string(data)
	note := ""
	if len(text) > toolFileCap {
		text, note = text[:toolFileCap], "\n… (truncated at 64 KiB)"
	}
	return textResult(fmt.Sprintf("%s (%d bytes)%s\n%s", p.Path, len(data), note, text), false), nil
}

// toolWrite writes a file and reports the before/after in Details so the
// transcript can show a reviewable diff card.
func toolWrite(workdir string, params json.RawMessage) (core.AgentToolResult, error) {
	p, err := decodeParams(params)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	abs, err := resolveToolPath(workdir, p.Path)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	old := ""
	if data, rerr := os.ReadFile(abs); rerr == nil {
		old = string(data)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return textResult("mkdir failed: "+err.Error(), true), nil
	}
	if err := os.WriteFile(abs, []byte(p.Content), 0o644); err != nil {
		return textResult("write failed: "+err.Error(), true), nil
	}
	details, _ := json.Marshal(map[string]string{"path": p.Path, "old": old, "new": p.Content})
	res := textResult(fmt.Sprintf("wrote %s (%d bytes)", p.Path, len(p.Content)), false)
	res.Details = details
	return res, nil
}

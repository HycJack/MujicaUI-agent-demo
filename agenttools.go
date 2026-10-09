package main

// agenttools.go defines the tools the agent loop can call, backed by
// pi-ai-go's DefaultExecutionEnv rooted at the workspace. bash runs a real
// shell command, read_file returns file contents, write_file writes a file
// and reports the before/after for the diff card. Everything is bounded —
// outputs and contents are capped so a runaway command cannot flood the
// model context.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/HycJack/pi-ai-go/core"
)

const (
	toolOutCap  = 8 * 1024  // per-tool result text cap
	toolFileCap = 64 * 1024 // read_file content cap
)

// agentTools builds the toolset the agent loop runs with, rooted at the
// current workspace.
func (a *app) agentTools() []core.AgentTool {
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
			Execute: a.toolBash,
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
			Execute: a.toolRead,
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
			Execute: a.toolWrite,
		},
	}
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
// rejects empty or directory-escaping results.
func (a *app) resolveToolPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("a file path is required")
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return filepath.Join(a.ws.root, filepath.FromSlash(p)), nil
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
func (a *app) toolBash(ctx context.Context, _ string, params json.RawMessage, _ func(json.RawMessage)) (core.AgentToolResult, error) {
	p, err := decodeParams(params)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	if strings.TrimSpace(p.Command) == "" {
		return textResult("a command is required", true), nil
	}
	env := core.NewDefaultExecutionEnvWithDir(a.ws.root)
	shell, flag := shellCommand()
	stdout, stderr, execErr := env.Exec(shell, []string{flag, p.Command}, a.ws.root)
	out := strings.TrimRight(stdout, "\n")
	if strings.TrimSpace(stderr) != "" {
		if out != "" {
			out += "\n"
		}
		out += strings.TrimRight(stderr, "\n")
	}
	details, _ := json.Marshal(map[string]any{"command": p.Command, "exit": exitCode(execErr)})
	res := textResult(out, execErr != nil)
	res.Details = details
	return res, nil
}

// shellCommand picks the platform shell wrapper for the bash tool.
func shellCommand() (shell, flag string) {
	if runtime.GOOS == "windows" {
		return "cmd", "/C"
	}
	return "sh", "-c"
}

// exitCode maps an Exec error to a process exit code (1 when unknown).
// Anything in the error chain carrying an ExitCode() int (os/exec's
// ExitError and pi-ai-go's wrapped forms) counts.
func exitCode(err error) int {
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
func (a *app) toolRead(_ context.Context, _ string, params json.RawMessage, _ func(json.RawMessage)) (core.AgentToolResult, error) {
	p, err := decodeParams(params)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	abs, err := a.resolveToolPath(p.Path)
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
func (a *app) toolWrite(_ context.Context, _ string, params json.RawMessage, _ func(json.RawMessage)) (core.AgentToolResult, error) {
	p, err := decodeParams(params)
	if err != nil {
		return textResult(err.Error(), true), nil
	}
	abs, err := a.resolveToolPath(p.Path)
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

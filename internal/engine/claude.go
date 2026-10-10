package engine

// claude.go drives one turn of the Claude Code CLI (spec §4): spawn the
// CLI in stream-json mode, write the prompt as one user message on
// stdin, fold the events into the engine's Callbacks, answer can_use_tool
// control requests through the approval flow, and settle the turn on
// the result line. One fresh CLI per turn; resume rides --resume. The
// event layer is claude_events.go; this file is the process.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// RunClaude drives one turn against the CLI at cfg.Bin. It returns nil
// for a clean turn or a cleanly stopped one (whatever arrived stays
// emitted); a non-nil error's message is the failure text for the reply
// card.
func RunClaude(ctx context.Context, cfg ClaudeConfig, prompt string, cb Callbacks) error {
	if strings.TrimSpace(cfg.Bin) == "" {
		return fmt.Errorf("claude: no claude CLI found — install Claude Code (npm i -g @anthropic-ai/claude-code) or check the settings")
	}
	// Mode mapping (spec §4.1): read-only and agent run in default mode
	// where the CLI asks before every mutating tool — the app denies, or
	// forwards to a card, per mode. Full access bypasses asking entirely.
	perm := "default"
	if cfg.Mode == ModeFull {
		perm = "bypassPermissions"
	}
	args := []string{"-p", "--output-format", "stream-json", "--verbose",
		"--input-format", "stream-json", "--include-partial-messages",
		"--permission-mode", perm}
	if m := strings.TrimSpace(cfg.Model); m != "" {
		args = append(args, "--model", m)
	}
	if cfg.SessionID != "" {
		args = append(args, "--resume", cfg.SessionID)
	}
	// The agent's own system prompt rides the CLI's append flag: the
	// CLI's base prompt stays, the agent's instructions join after it.
	if cfg.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", cfg.SystemPrompt)
	}

	cmd := exec.CommandContext(ctx, cfg.Bin, args...)
	cmd.Dir = cfg.Workdir
	ProcGroupAttr(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("claude: %s", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("claude: %s", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("claude: %s", err)
	}

	// The prompt: one user message on the input stream. Lines end with
	// CRLF: every line reader handles it (readline, \n-split + JSON.parse),
	// and strict readers that only speak \r\n still work — LF-only breaks
	// those.
	msg, _ := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]any{{"type": "text", "text": prompt}},
		},
	})
	if _, err := stdin.Write(append(msg, '\r', '\n')); err != nil {
		stdin.Close()
		return fmt.Errorf("claude: %s", err)
	}

	r := &claudeRun{cfg: cfg, ctx: ctx, stdin: stdin, cb: cb}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	for sc.Scan() {
		r.handle(strings.TrimSpace(sc.Text()))
		// The result line ends the turn, but the CLI stays alive waiting
		// for the next input message: check after every line and close
		// stdin ourselves — a check before Scan would block here forever
		// and the turn would never settle.
		if r.sawResult {
			break
		}
	}
	waitErr, killed := Reap(cmd, stdin)

	// A cancelled turn is only a clean stop when the CLI had actually got
	// on with it. A run that produced nothing at all and was then cut off
	// — a CLI that hung before its first line, or died without saying why
	// — reports itself as a finished turn otherwise, and the user is left
	// with an empty reply and no reason for it. The events that did arrive
	// still stand; what is missing is said out loud.
	if ctx.Err() != nil {
		if r.sawEvent {
			return nil // stopped by the user; keep whatever arrived
		}
		return fmt.Errorf("claude: stopped before it answered — %s", Trunc(claudeReason(stderr, waitErr, killed), 400))
	}
	if r.sawEvent && (waitErr == nil || killed) {
		// A CLI we had to stop after it already settled is not a failed
		// turn: the events it sent stand.
		return nil
	}
	tail := strings.TrimSpace(stderr.String())
	if tail == "" && waitErr != nil {
		tail = waitErr.Error()
	}
	if tail == "" {
		return nil
	}
	return fmt.Errorf("claude: %s", tail)
}

// claudeReason is the best available account of a run that produced
// nothing: the CLI's own words if it left any, then the wait error, then
// what is actually known. The last is not decoration — a CLI that hangs
// and is killed has no stderr and often no wait error either, and "it
// exited without answering" is the fact that has to reach the user.
func claudeReason(stderr strings.Builder, waitErr error, killed bool) string {
	if tail := strings.TrimSpace(stderr.String()); tail != "" {
		return tail
	}
	if waitErr != nil {
		return waitErr.Error()
	}
	if killed {
		return "it was still running and had to be stopped"
	}
	return "it exited without answering"
}

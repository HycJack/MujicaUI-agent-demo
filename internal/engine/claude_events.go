package engine

// claude_events.go is the Claude Code stream-json event layer: one claudeRun
// consumes the CLI's stdout lines and folds them into the engine's
// Callbacks. The wire contract is ported from mygo-agent's claude adapter,
// which learned it the hard way:
//
//   - the result event ends the turn but the CLI stays resident — sawResult
//     tells the runner to break the scan and close stdin;
//   - can_use_tool control requests are answered on stdin (writeControl)
//     with allow+updatedInput echo or deny+message — anything else stalls;
//   - other server-initiated subtypes get an error reply for the same reason;
//   - partial text deltas may duplicate the complete assistant message that
//     follows them, so the complete message only contributes its un-streamed
//     suffix;
//   - compact_metadata is snake_case, like the rest of claude's wire.
//
// Crux's own semantics sit on top: OnText carries each text SEGMENT's
// accumulated buffer (a tool_use starts a new segment — the interim reply
// before a tool call is not part of the reply after it), and OnToolStart
// passes the raw input JSON for the UI layer to parse per tool.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ClaudeConfig is everything RunClaude needs: where the CLI is, how to
// steer it, and the permission context its can_use_tool requests resolve
// against.
type ClaudeConfig struct {
	Bin             string
	Model           string // alias: sonnet/opus/…; empty = the CLI's default
	SystemPrompt    string
	Mode            Mode
	SessionID       string // --resume
	Workdir         string
	ApprovalTimeout time.Duration
	OnApproval      ApprovalFunc
}

// claudeRun carries one Claude Code run's state between lines.
type claudeRun struct {
	cfg   ClaudeConfig
	ctx   context.Context
	stdin io.WriteCloser // nil in unit tests; writeControl captures then
	cb    Callbacks
	// session is the CLI's session id for this run: cfg.SessionID until
	// an init event names this run's own.
	session string
	// sessionReported says OnSession already fired (init, or the
	// result-event fallback) — once is enough.
	sessionReported bool
	sawEvent        bool
	sawResult       bool
	// streamed is the text the partial deltas already emitted, so the
	// complete assistant message that follows them only contributes its
	// un-streamed suffix.
	streamed strings.Builder
	// text is the current segment's accumulated text (engine semantics).
	text strings.Builder
	// wrote records control responses when there is no CLI to write to
	// (unit tests); production always has stdin.
	wrote []map[string]any
}

// handle applies one stream-json line.
func (r *claudeRun) handle(line string) {
	if line == "" {
		return
	}
	r.sawEvent = true
	// The result event carries everything at the top level; "result"
	// itself is the final text as a string.
	var ev struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype"`
		SessionID string `json:"session_id"`
		// compact_metadata is snake_case, like the rest of claude's
		// stream-json wire. Reading it as `compactMetadata` decodes to
		// nothing and silently costs the note its counts.
		CompactMetadata *struct {
			Trigger    string `json:"trigger"`
			PreTokens  int    `json:"pre_tokens"`
			PostTokens int    `json:"post_tokens"`
		} `json:"compact_metadata"`
		Message *struct {
			Content []block `json:"content"`
		} `json:"message"`
		ResultText string  `json:"result"`
		IsError    bool    `json:"is_error"`
		Duration   float64 `json:"duration_ms"`
		Cost       float64 `json:"total_cost_usd"`
		// The turn's token totals, when the CLI reports them; the note
		// carries their sum.
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal([]byte(line), &ev) != nil {
		return
	}

	switch ev.Type {
	case "control_request":
		r.controlRequest(line)
		return
	case "result":
		r.sawResult = true
	}

	switch ev.Type {
	case "system":
		if ev.Subtype == "init" && ev.SessionID != "" {
			r.session = ev.SessionID
			if !r.sessionReported && r.cb.OnSession != nil {
				r.sessionReported = true
				r.cb.OnSession(ev.SessionID)
			}
		}
		// claude summarises its own context when the conversation
		// outgrows the window, and says so on this wire as a system
		// frame with subtype compact_boundary. Without it the only
		// symptom is the model quietly forgetting an earlier
		// instruction. A frame with no metadata still counts: the
		// event happened, the counts are a bonus.
		if ev.Subtype == "compact_boundary" {
			var before, after int
			if ev.CompactMetadata != nil {
				before, after = ev.CompactMetadata.PreTokens, ev.CompactMetadata.PostTokens
			}
			if r.cb.OnNote != nil {
				r.cb.OnNote(CompactedNotice("claude", before, after))
			}
		}

	case "stream_event":
		// With --include-partial-messages the CLI wraps its inner
		// Anthropic stream in these frames: text arrives as deltas as
		// it is generated, instead of one whole assistant message at
		// the end. The complete assistant message still follows — the
		// code below emits only the suffix the deltas have not covered.
		var se struct {
			Event struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			} `json:"event"`
		}
		if json.Unmarshal([]byte(line), &se) != nil {
			return
		}
		if se.Event.Type == "content_block_delta" && se.Event.Delta.Type == "text_delta" && se.Event.Delta.Text != "" {
			r.streamed.WriteString(se.Event.Delta.Text)
			r.text.WriteString(se.Event.Delta.Text)
			r.emitText()
		}

	case "assistant":
		if ev.Message == nil {
			return
		}
		for _, blk := range ev.Message.Content {
			switch blk.Type {
			case "text":
				// Deduplicate against the streamed deltas: the CLI
				// warns that partial messages may duplicate the
				// assistant message, so only the un-streamed suffix
				// goes out.
				whole := r.streamed.String()
				text := blk.Text
				if text == "" {
					continue
				}
				if len(text) >= len(whole) {
					if suffix, ok := strings.CutPrefix(text, whole); ok {
						text = suffix
					} else if whole != "" {
						text = "" // diverged; trust the deltas already sent
					}
				} else if strings.HasPrefix(whole, text) {
					text = ""
				}
				r.streamed.Reset()
				if text != "" {
					r.text.WriteString(text)
					r.emitText()
				}
			case "tool_use":
				r.streamed.Reset()
				// A tool call ends the current text segment: what the
				// model writes after the results is its next thought.
				r.text.Reset()
				if r.cb.OnToolStart != nil {
					r.cb.OnToolStart(blk.ID, blk.Name, string(blk.RawInput))
				}
			}
		}

	case "user":
		if ev.Message == nil {
			return
		}
		for _, blk := range ev.Message.Content {
			if blk.Type == "tool_result" {
				if r.cb.OnToolEnd != nil {
					r.cb.OnToolEnd(blk.ToolUseID, TrimOutput(contentText(blk.RawContent), 16<<10), blk.IsError, 0)
				}
			}
		}

	case "result":
		// A run that somehow skipped its init frame still gets its
		// session named: the result event carries session_id too. It
		// must reach OnSession as well — the id is what rides --resume
		// next turn; losing it here would silently fork the thread's
		// conversation into a fresh session.
		if ev.SessionID != "" && r.session == "" {
			r.session = ev.SessionID
			if !r.sessionReported && r.cb.OnSession != nil {
				r.sessionReported = true
				r.cb.OnSession(ev.SessionID)
			}
		}
		verb := "Done"
		if ev.IsError || ev.Subtype != "success" {
			verb = "Stopped"
		}
		note := fmt.Sprintf("%s in %.1fs · $%.4f", verb, ev.Duration/1000, ev.Cost)
		if ev.Usage != nil {
			if tokens := ev.Usage.InputTokens + ev.Usage.OutputTokens; tokens > 0 {
				note += fmt.Sprintf(" · %d tokens", tokens)
			}
		}
		note += " · session " + ShortSession(r.sessionLabel())
		if r.cb.OnNote != nil {
			r.cb.OnNote(note)
		}
	}
}

// emitText reports the current segment's accumulated text.
func (r *claudeRun) emitText() {
	if r.cb.OnText != nil {
		r.cb.OnText(r.text.String())
	}
}

// sessionLabel is the session id a note names: this run's own id once an
// init event has named it, the resumed id until then.
func (r *claudeRun) sessionLabel() string {
	if r.session != "" {
		return r.session
	}
	return r.cfg.SessionID
}

// controlRequest answers one CLI control request: can_use_tool decisions
// ride the approval callback; every other server-initiated subtype gets
// an error reply so the CLI never stalls on an unanswered request.
func (r *claudeRun) controlRequest(line string) {
	var req struct {
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype  string          `json:"subtype"`
			ToolName string          `json:"tool_name"`
			Input    json.RawMessage `json:"input"`
		} `json:"request"`
	}
	if json.Unmarshal([]byte(line), &req) != nil {
		return
	}
	if req.Request.Subtype != "can_use_tool" {
		r.writeControl(map[string]any{
			"subtype": "error", "request_id": req.RequestID, "error": "unsupported by crux",
		})
		return
	}
	// Read-only: the mode denies every tool the CLI asks about; the
	// denial names the mode so the model can stop trying. Full access
	// never gets here — the CLI runs --permission-mode bypassPermissions
	// and does not ask.
	if r.cfg.Mode == ModeReadOnly {
		r.writeControl(map[string]any{
			"subtype": "success", "request_id": req.RequestID,
			"response": map[string]any{
				"behavior": "deny",
				"message":  denialText(req.Request.ToolName, ModeReadOnly),
			},
		})
		return
	}
	decision := ApprovalDecision{Reason: "no approval handler is configured"}
	if r.cfg.OnApproval != nil {
		// Every approval wait is bounded: a card on a live run must
		// never sit unanswered. Expiry settles the call as a denial
		// inside the stream — not a failed turn — and the reason tells
		// the model (and the card) why.
		actx, cancel := ApprovalContext(r.ctx, r.cfg.ApprovalTimeout)
		decision = r.cfg.OnApproval(actx, ApprovalRequest{
			Tool:    req.Request.ToolName,
			Summary: approvalSummary(req.Request.ToolName, req.Request.Input),
			Reason:  "claude asks to use this tool",
		})
		if actx.Err() != nil {
			// A decision that arrives after the deadline is not a decision.
			decision = ApprovalDecision{Reason: ApprovalReason(actx)}
		}
		cancel()
	}
	if decision.Approved {
		var input any
		_ = json.Unmarshal(req.Request.Input, &input)
		if input == nil {
			input = map[string]any{}
		}
		r.writeControl(map[string]any{
			"subtype": "success", "request_id": req.RequestID,
			"response": map[string]any{"behavior": "allow", "updatedInput": input},
		})
		return
	}
	reason := decision.Reason
	if reason == "" {
		reason = "the user denied this call"
	}
	r.writeControl(map[string]any{
		"subtype": "success", "request_id": req.RequestID,
		"response": map[string]any{"behavior": "deny", "message": reason},
	})
}

// approvalSummary renders the bounded, redacted one-line ask for a
// can_use_tool request: the command for bash, the path for file tools,
// the name and short arguments otherwise.
func approvalSummary(tool string, raw json.RawMessage) string {
	var in struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
	}
	_ = json.Unmarshal(raw, &in)
	switch {
	case tool == "Bash" || tool == "bash":
		if in.Command != "" {
			return "$ " + Trunc(Redact(in.Command), 160)
		}
	case in.FilePath != "":
		return tool + " " + Trunc(Redact(in.FilePath), 160)
	case in.Path != "":
		return tool + " " + Trunc(Redact(in.Path), 160)
	}
	return Trunc(tool+" "+ShortArgs(raw), 160)
}

// writeControl writes one control_response line to the CLI's stdin
// (CRLF-terminated; see RunClaude).
func (r *claudeRun) writeControl(response map[string]any) {
	if r.stdin == nil {
		r.wrote = append(r.wrote, response)
		return
	}
	line, _ := json.Marshal(map[string]any{"type": "control_response", "response": response})
	_, _ = r.stdin.Write(append(line, '\r', '\n'))
}

// block mirrors one content block of a Claude Code message.
type block struct {
	Type       string          `json:"type"`
	Text       string          `json:"text"`
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	RawInput   json.RawMessage `json:"input"`
	ToolUseID  string          `json:"tool_use_id"`
	RawContent json.RawMessage `json:"content"`
	IsError    bool            `json:"is_error"`
}

// contentText flattens a tool result's content — a string, or a list of
// text blocks — into plain text.
func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) == nil {
		var b strings.Builder
		for _, blk := range blocks {
			if blk.Type == "text" {
				b.WriteString(blk.Text)
				b.WriteString("\n")
			}
		}
		return strings.TrimRight(b.String(), "\n")
	}
	return string(raw)
}

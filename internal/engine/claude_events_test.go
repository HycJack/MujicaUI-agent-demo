package engine

// claude_events_test.go drives the stream-json event handler line by
// line, the way the reference's line tests do — no process, no network,
// so the wire contract is pinned on every platform including Windows.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// collected records every callback a run fires.
type collected struct {
	texts    []string // per-segment accumulated texts, in order
	tools    []string // "name args" per OnToolStart
	ends     map[string]string
	endsErr  map[string]bool
	sess     []string
	notes    []string
	approve  ApprovalFunc
	approval []ApprovalRequest
}

func newCollected(approve ApprovalFunc) (*collected, *claudeRun) {
	cc := &collected{ends: map[string]string{}, endsErr: map[string]bool{}, approve: approve}
	if approve != nil {
		inner := approve
		approve = func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
			cc.approval = append(cc.approval, req)
			return inner(ctx, req)
		}
	}
	cb := Callbacks{
		OnText:      func(t string) { cc.texts = append(cc.texts, t) },
		OnToolStart: func(id, name, args string) { cc.tools = append(cc.tools, name+" "+args) },
		OnToolEnd:   func(id, res string, isErr bool, _ time.Duration) { cc.ends[id], cc.endsErr[id] = res, isErr },
		OnSession:   func(id string) { cc.sess = append(cc.sess, id) },
		OnNote:      func(t string) { cc.notes = append(cc.notes, t) },
	}
	r := &claudeRun{cfg: ClaudeConfig{Mode: ModeAgent, OnApproval: approve}, cb: cb, ctx: context.Background()}
	return cc, r
}

func TestClaudeSessionAndText(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"system","subtype":"init","session_id":"sess-abc123"}`)
	r.handle(`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello "}]}}`)
	if len(cc.sess) != 1 || cc.sess[0] != "sess-abc123" {
		t.Fatalf("session: %v", cc.sess)
	}
	if len(cc.texts) != 1 || cc.texts[0] != "Hello " {
		t.Fatalf("text: %v", cc.texts)
	}
}

// A run that never saw its init frame still reports the session id on
// the result line — and it must reach OnSession, not just the note:
// the id is what rides --resume next turn, and losing it silently
// forks the thread into a fresh session. Report once, even if both
// frames name it.
func TestClaudeSessionFromResultFallback(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"result","subtype":"success","is_error":false,"duration_ms":1000,"total_cost_usd":0.0,"result":"done","session_id":"sess-fallback"}`)
	if len(cc.sess) != 1 || cc.sess[0] != "sess-fallback" {
		t.Fatalf("session from result fallback: %v", cc.sess)
	}
	// A late init after the result fallback does not double-report.
	r.handle(`{"type":"system","subtype":"init","session_id":"sess-fallback"}`)
	if len(cc.sess) != 1 {
		t.Fatalf("session double-reported: %v", cc.sess)
	}
	// An init-first run reports exactly once too.
	cc2, r2 := newCollected(nil)
	r2.handle(`{"type":"system","subtype":"init","session_id":"sess-init"}`)
	r2.handle(`{"type":"result","subtype":"success","is_error":false,"duration_ms":1000,"total_cost_usd":0.0,"result":"done","session_id":"sess-init"}`)
	if len(cc2.sess) != 1 || cc2.sess[0] != "sess-init" {
		t.Fatalf("init-first session reporting: %v", cc2.sess)
	}
}

// Partial deltas then the complete assistant message: only the
// un-streamed suffix may go out (the CLI warns partials may duplicate
// the message). Crux's OnText carries the segment's accumulated buffer,
// so the LAST emission must be the whole text.
func TestClaudePartialDedup(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hel"}}}`)
	r.handle(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"lo"}}}`)
	r.handle(`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}`)
	if len(cc.texts) == 0 {
		t.Fatal("no text events")
	}
	if got := cc.texts[len(cc.texts)-1]; got != "Hello world" {
		t.Fatalf("last emission = %q, want the whole text", got)
	}
	if got := cc.texts[0]; got != "Hel" {
		t.Fatalf("first emission = %q, want the first delta", got)
	}
}

// A tool_use starts a new text segment: the text after tool results is
// the model's next thought, not a continuation of the interim reply.
func TestClaudeToolUseResetsText(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"interim"}}}`)
	r.handle(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls"}}]}}`)
	r.handle(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"file"}]}}`)
	r.handle(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"final"}}}`)
	if len(cc.texts) != 2 || cc.texts[0] != "interim" || cc.texts[1] != "final" {
		t.Fatalf("segments: %v", cc.texts)
	}
	if len(cc.tools) != 1 || !strings.Contains(cc.tools[0], "Bash") {
		t.Fatalf("tools: %v", cc.tools)
	}
	if cc.ends["tu1"] != "file" {
		t.Fatalf("ends: %v", cc.ends)
	}
}

// A tool_result carrying a list of text blocks flattens to plain text.
func TestClaudeToolResultBlocks(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu2","content":[{"type":"text","text":"one"},{"type":"text","text":"two"}],"is_error":true}]}}`)
	if cc.ends["tu2"] != "one\ntwo" {
		t.Fatalf("end: %q", cc.ends["tu2"])
	}
	if !cc.endsErr["tu2"] {
		t.Fatal("is_error not carried")
	}
}

func TestClaudeResultNote(t *testing.T) {
	cc, r := newCollected(nil)
	r.cfg.SessionID = "sess-abc123"
	r.handle(`{"type":"result","subtype":"success","is_error":false,"duration_ms":12000,"total_cost_usd":0.0042,"result":"done"}`)
	if len(cc.notes) != 1 {
		t.Fatalf("notes: %v", cc.notes)
	}
	n := cc.notes[0]
	for _, want := range []string{"Done in 12.0s", "$0.0042", "sess-abc"} {
		if !strings.Contains(n, want) {
			t.Fatalf("note %q missing %q", n, want)
		}
	}
	if !r.sawResult {
		t.Fatal("result line did not set sawResult 鈥?the run would never settle")
	}
}

// An errored result says Stopped, not Done.
func TestClaudeResultNoteError(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"result","subtype":"error_during_execution","is_error":true,"duration_ms":500,"total_cost_usd":0.0,"result":"x"}`)
	if !strings.Contains(cc.notes[0], "Stopped") {
		t.Fatalf("note: %q", cc.notes[0])
	}
}

func TestClaudeResultNoteTokens(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"result","subtype":"success","is_error":false,"duration_ms":2345,"total_cost_usd":0.0042,"usage":{"input_tokens":110,"output_tokens":17}}`)
	if !strings.Contains(cc.notes[0], "127 tokens") {
		t.Fatalf("note: %q", cc.notes[0])
	}
}

// compact_metadata is snake_case; reading it as compactMetadata decodes
// to nothing and the note silently loses its counts.
func TestClaudeCompactNote(t *testing.T) {
	cc, r := newCollected(nil)
	r.handle(`{"type":"system","subtype":"compact_boundary","compact_metadata":{"trigger":"auto","pre_tokens":25900,"post_tokens":5253}}`)
	if len(cc.notes) != 1 || !strings.Contains(cc.notes[0], "25.9k") {
		t.Fatalf("compact: %v", cc.notes)
	}
}

// Read-only answers can_use_tool itself with a mode-named denial 鈥?no
// human is asked.
func TestClaudeReadOnlyDeniesTool(t *testing.T) {
	_, r := newCollected(func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		t.Fatal("read-only must not ask")
		return ApprovalDecision{}
	})
	r.cfg.Mode = ModeReadOnly
	r.handle(`{"type":"control_request","request_id":"rq1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)
	if len(r.wrote) != 1 {
		t.Fatalf("wrote: %v", r.wrote)
	}
	resp := r.wrote[0]
	if resp["subtype"] != "success" || resp["request_id"] != "rq1" {
		t.Fatalf("resp: %v", resp)
	}
	inner := resp["response"].(map[string]any)
	if inner["behavior"] != "deny" || !strings.Contains(inner["message"].(string), "read-only") {
		t.Fatalf("inner: %v", inner)
	}
}

// Agent mode forwards to the approval callback; an approved call
// answers allow + updatedInput echo.
func TestClaudeApprovalRoundTrip(t *testing.T) {
	_, r := newCollected(func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		if req.Tool != "Bash" || !strings.Contains(req.Summary, "ls") {
			t.Fatalf("req: %+v", req)
		}
		return ApprovalDecision{Approved: true}
	})
	r.handle(`{"type":"control_request","request_id":"rq2","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)
	inner := r.wrote[0]["response"].(map[string]any)
	if inner["behavior"] != "allow" || inner["updatedInput"] == nil {
		t.Fatalf("inner: %v", inner)
	}
}

// A denial carries the reason back to the model.
func TestClaudeApprovalDenied(t *testing.T) {
	_, r := newCollected(func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		return ApprovalDecision{Reason: "the user denied this call"}
	})
	r.handle(`{"type":"control_request","request_id":"rq3","request":{"subtype":"can_use_tool","tool_name":"Edit","input":{"file_path":"a.go"}}}`)
	inner := r.wrote[0]["response"].(map[string]any)
	if inner["behavior"] != "deny" || inner["message"] != "the user denied this call" {
		t.Fatalf("inner: %v", inner)
	}
}

// A decision that arrives after the deadline is not a decision.
func TestClaudeApprovalTimeoutIsDenial(t *testing.T) {
	_, r := newCollected(func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		<-ctx.Done()
		return ApprovalDecision{Approved: true} // too late
	})
	r.cfg.ApprovalTimeout = 20 * time.Millisecond
	r.handle(`{"type":"control_request","request_id":"rq4","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)
	inner := r.wrote[0]["response"].(map[string]any)
	if inner["behavior"] != "deny" || !strings.Contains(inner["message"].(string), "timed out") {
		t.Fatalf("inner: %v", inner)
	}
}

// Nil OnApproval: the ask settles as a denial (headless fail-closed).
func TestClaudeNilApproval(t *testing.T) {
	_, r := newCollected(nil)
	r.handle(`{"type":"control_request","request_id":"rq6","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)
	inner := r.wrote[0]["response"].(map[string]any)
	if inner["behavior"] != "deny" {
		t.Fatalf("inner: %v", inner)
	}
}

// The ask's summary is redacted: a token-shaped secret never lands on a
// card or a transcript.
func TestClaudeApprovalSummaryRedacts(t *testing.T) {
	cc, r := newCollected(func(ctx context.Context, req ApprovalRequest) ApprovalDecision {
		return ApprovalDecision{Approved: false}
	})
	r.handle(`{"type":"control_request","request_id":"rq7","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"curl -H \"Authorization: Bearer sk-abcdef1234567890\" host"}}}`)
	if len(cc.approval) != 1 {
		t.Fatalf("approval: %+v", cc.approval)
	}
	if strings.Contains(cc.approval[0].Summary, "sk-abcdef1234567890") {
		t.Fatalf("summary leaked: %q", cc.approval[0].Summary)
	}
}

// Any other server-initiated request gets an error reply 鈥?leaving one
// unanswered stalls the CLI forever.
func TestClaudeUnsupportedControlAnswered(t *testing.T) {
	_, r := newCollected(nil)
	r.handle(`{"type":"control_request","request_id":"rq5","request":{"subtype":"set_chat_model","model":"x"}}`)
	if len(r.wrote) != 1 || r.wrote[0]["subtype"] != "error" {
		t.Fatalf("wrote: %v", r.wrote)
	}
}

package engine

// gate.go is the three-level approval gate (spec §2.1, DSH-style): the
// mode selector itself is the "does this need approval" switch. The types
// here are the whole app's permission vocabulary — the builtin tools wrap
// their Execute with gateResolve, and the claude adapter maps its
// can_use_tool requests through the same approval flow.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Mode is the three-level approval selector. ParseMode tolerates the
// strings settings.json stores; a typo fails closed to read-only.
type Mode int

const (
	ModeReadOnly Mode = iota
	ModeAgent
	ModeFull
)

// ParseMode maps a stored mode string to its Mode. Empty means agent (the
// default a settings file without the field upgrades to); anything
// unrecognized is read-only — a typo is not permission.
func ParseMode(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "agent":
		return ModeAgent
	case "read-only", "readonly":
		return ModeReadOnly
	case "full", "full-access", "fullaccess":
		return ModeFull
	default:
		return ModeReadOnly
	}
}

// String renders the settings.json spelling.
func (m Mode) String() string {
	switch m {
	case ModeReadOnly:
		return "read-only"
	case ModeFull:
		return "full"
	}
	return "agent"
}

// Permission is what the gate decided for one prepared call.
type Permission string

const (
	PermAllow Permission = "allow"
	PermDeny  Permission = "deny"
	PermAsk   Permission = "ask"
)

// gateResolve is spec §2.1's table. sandboxOK says an OS sandbox backend
// exists on this platform — the input that turns agent-mode bash from
// "sandboxed, no ask" into "the approval card is the sandbox" (the
// Windows reading the design chose over the reference's hard error).
func gateResolve(tool string, mode Mode, sandboxOK bool) Permission {
	switch tool {
	case "read_file":
		return PermAllow
	case "write_file":
		switch mode {
		case ModeReadOnly:
			return PermDeny
		}
		return PermAllow
	case "bash":
		switch mode {
		case ModeReadOnly:
			return PermDeny
		case ModeAgent:
			if !sandboxOK {
				return PermAsk
			}
			return PermAllow
		}
		return PermAllow
	default:
		return PermDeny // unknown tool fails closed
	}
}

// denialText is the model-readable refusal (never a panic, never a silent
// no-op): the model can read it, change course, and ask the user to move
// the mode.
func denialText(tool string, mode Mode) string {
	return fmt.Sprintf(
		"%s mode: the %s tool is not available; ask the user to change the approval mode",
		mode, tool)
}

// --- approval plumbing (ported from mygo-agent's harness/approvals.go) ---

// DefaultApprovalTimeout bounds every wait on an approval card: long
// enough for a human to notice an ask, short enough that an unattended
// run cannot sit on a card forever.
const DefaultApprovalTimeout = 10 * time.Minute

// ErrApprovalTimedOut is the cause an approval's context carries when its
// deadline expires, so the host can tell "timed out" from "cancelled".
var ErrApprovalTimedOut = errors.New("approval timed out")

// ApprovalRequest is one ask: what tool, the bounded redacted one-line
// summary of the call, and the fixed-vocabulary reason it asked at all.
type ApprovalRequest struct {
	Tool    string
	Summary string
	Reason  string
}

// ApprovalDecision settles an ask. Reason is the denial's cause, fed back
// to the model verbatim; approved decisions carry no reason.
type ApprovalDecision struct {
	Approved bool
	Reason   string
}

// ApprovalFunc decides one ask under its own context: a nil handler
// denies (headless fail-closed), a late answer is not an answer.
type ApprovalFunc func(ctx context.Context, req ApprovalRequest) ApprovalDecision

// ApprovalContext derives the context one approval wait runs under; a
// non-positive timeout falls back to DefaultApprovalTimeout.
func ApprovalContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = DefaultApprovalTimeout
	}
	return context.WithTimeoutCause(ctx, timeout, ErrApprovalTimedOut)
}

// ApprovalReason reports the settled reason for a wait that ended because
// its context fired: a timeout, or a cancellation of some other origin —
// a stopped run, or a parent deadline the adapter inherited.
func ApprovalReason(ctx context.Context) string {
	if errors.Is(context.Cause(ctx), ErrApprovalTimedOut) {
		return "approval timed out"
	}
	return "cancelled"
}

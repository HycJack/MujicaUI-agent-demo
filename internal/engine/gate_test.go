package engine

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"":            ModeAgent, // 缺省 = agent(旧 settings 无此字段)
		"agent":       ModeAgent,
		"read-only":   ModeReadOnly,
		"readonly":    ModeReadOnly,
		"full":        ModeFull,
		"full-access": ModeFull,
		"nonsense":    ModeReadOnly, // fail closed:拼错不是许可
	}
	for in, want := range cases {
		if got := ParseMode(in); got != want {
			t.Errorf("ParseMode(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestModeString(t *testing.T) {
	if ModeReadOnly.String() != "read-only" || ModeAgent.String() != "agent" || ModeFull.String() != "full" {
		t.Fatal("Mode.String round-trip broken")
	}
	for _, s := range []string{"read-only", "agent", "full"} {
		if ParseMode(s).String() != s {
			t.Fatalf("ParseMode(%q).String() != %q", s, s)
		}
	}
}

// TestGateResolve pins the three-mode table (spec §2.1): sandboxOK is the
// "OS sandbox backend exists" input that decides agent-mode bash.
func TestGateResolve(t *testing.T) {
	type tc struct {
		tool      string
		mode      Mode
		sandboxOK bool
		want      Permission
	}
	cases := []tc{
		{"read_file", ModeReadOnly, false, PermAllow},
		{"read_file", ModeAgent, true, PermAllow},
		{"read_file", ModeFull, true, PermAllow},
		{"write_file", ModeReadOnly, false, PermDeny},
		{"write_file", ModeAgent, true, PermAllow},
		{"write_file", ModeFull, true, PermAllow},
		{"bash", ModeReadOnly, false, PermDeny},
		{"bash", ModeAgent, true, PermAllow}, // mac/linux: OS sandbox
		{"bash", ModeAgent, false, PermAsk},  // windows: 审批卡即沙箱
		{"bash", ModeFull, false, PermAllow},
		{"nonesuch", ModeFull, true, PermDeny}, // unknown tool fails closed
	}
	for _, c := range cases {
		if got := gateResolve(c.tool, c.mode, c.sandboxOK); got != c.want {
			t.Errorf("gateResolve(%s,%v,%v) = %v, want %v", c.tool, c.mode, c.sandboxOK, got, c.want)
		}
	}
}

func TestDenialText(t *testing.T) {
	s := denialText("bash", ModeReadOnly)
	if !strings.Contains(s, "read-only") || !strings.Contains(s, "bash") {
		t.Fatalf("denialText: %q", s)
	}
}

func TestApprovalContext(t *testing.T) {
	ctx, cancel := ApprovalContext(context.Background(), 20*time.Millisecond)
	defer cancel()
	<-ctx.Done()
	if ApprovalReason(ctx) != "approval timed out" {
		t.Fatalf("reason: %q", ApprovalReason(ctx))
	}
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	ctx2, _ := ApprovalContext(parent, 0) // 0 → DefaultApprovalTimeout
	cancelParent()
	<-ctx2.Done()
	if ApprovalReason(ctx2) != "cancelled" {
		t.Fatalf("reason: %q", ApprovalReason(ctx2))
	}
}

func TestDefaultApprovalTimeout(t *testing.T) {
	if DefaultApprovalTimeout != 10*time.Minute {
		t.Fatalf("DefaultApprovalTimeout = %v", DefaultApprovalTimeout)
	}
}

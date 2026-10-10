//go:build windows

package engine

import (
	"context"
	"fmt"
	"os/exec"
)

type windowsSandbox struct{}

// NewSandbox returns the Windows provider: one that reports no backend
// (the honesty rule, spec §3). The gate reads Available() and downgrades
// agent-mode bash to an approval card — the approval card IS the
// boundary on Windows.
func NewSandbox() Sandbox { return windowsSandbox{} }

func (windowsSandbox) Available() bool { return false }

// Command is never reached: the gate checks Available() first. If
// something calls it anyway, the answer names the remedy instead of
// silently running unsandboxed.
func (windowsSandbox) Command(ctx context.Context, b Boundary, name string, arg ...string) (*exec.Cmd, func(), error) {
	return nil, nil, fmt.Errorf("no OS sandbox backend on Windows; agent mode asks per command — switch to Full Access to run unsandboxed")
}

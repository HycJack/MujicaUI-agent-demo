//go:build !darwin && !linux

package sandbox

import (
	"context"
	"fmt"
	"os/exec"
)

// New returns the platform's sandbox provider: one that always reports
// the missing backend (the honesty rule).
func New() Sandbox { return sandbox{} }

type sandbox struct{}

// Command reports that this platform has no sandbox backend: agent
// commands never fall back to unsandboxed execution — the error names
// the remedy instead.
func (sandbox) Command(ctx context.Context, b Boundary, name string, arg ...string) (*exec.Cmd, func(), error) {
	return nil, nil, fmt.Errorf("sandboxed execution is not implemented on this platform; turn off the sandbox in settings to run unsandboxed")
}

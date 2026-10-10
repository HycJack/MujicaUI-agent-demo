// Package sandbox is the local execution boundary for the bash tool:
// the command may run, but only inside a workspace-scoped boundary —
// the workdir read-write, a fresh scratch directory as the child's
// HOME and TMPDIR, the network denied, and a fixed list of credential
// stores excluded even from the broad reads. It is the codex
// workspace-write contract enforced with the platform's native
// primitive: Seatbelt on macOS, bubblewrap on Linux.
//
// The honesty rule: a platform without a backend reports an error
// naming the remedy — it never falls back to running unsandboxed.
package sandbox

import (
	"context"
	"os/exec"
	"path/filepath"
)

// Boundary is the effective execution boundary for one sandboxed
// command: the workdir read-write, a fresh scratch directory
// read-write (it becomes the child's HOME and TMPDIR), network denied.
type Boundary struct {
	Workdir string
	Scratch string
	Network string // "deny" — no other mode exists
}

// Sandbox wraps one command invocation in a boundary. A platform
// without a backend returns an error naming the remedy — never a
// silently weaker boundary. Implementations live in the platform
// files beside this one.
type Sandbox interface {
	Command(ctx context.Context, b Boundary, name string, arg ...string) (*exec.Cmd, func(), error)
}

// Canonical absolutizes and resolves a grant path: grants enter
// profiles and mounts as real paths, never through a symlink the
// child could sway.
func Canonical(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

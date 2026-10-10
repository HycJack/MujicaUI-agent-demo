package engine

// sandbox.go holds the sandbox protocol the engine knows: the boundary
// for one sandboxed command, the provider interface, and the credential
// denylist every backend masks. Ported from mygo-agent's harness/sandbox.go
// and providers/sandbox; the one protocol change is Available() — the
// gate reads it to decide whether agent-mode bash runs sandboxed or
// lands on an approval card (the Windows reading).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Boundary is the effective execution boundary for one sandboxed command
// (spec §3): the workdir read-write, a fresh scratch directory
// read-write (it becomes the child's HOME and TMPDIR), network denied.
type Boundary struct {
	Workdir string
	Scratch string
	Network string // "deny" — "inherit" is reserved for a future mode
}

// Sandbox wraps one command invocation in a boundary. A platform without
// a backend reports Available() == false — the gate then downgrades
// agent-mode bash to an approval card, never silently unsandboxed
// execution (the honesty rule). Implementations live in the per-platform
// sandbox_*.go files; the engine knows the protocol only.
type Sandbox interface {
	// Available reports whether this platform's backend is installed
	// (sandbox-exec on macOS, bwrap on Linux).
	Available() bool
	// Command wraps argv in the boundary; the second value is a cleanup
	// the caller runs when the command is done (nil when there is none).
	Command(ctx context.Context, b Boundary, name string, arg ...string) (*exec.Cmd, func(), error)
}

// credentialStores are the credential stores a sandboxed command must
// not read even though reads elsewhere are broad (the codex
// workspace-write contract: read the disk, write the workspace, no
// network). It is the same list the file tools refuse — what is a secret
// to the model as a file is a secret through a shell command too.
// .netrc is a bare file; the rest are directories.
func credentialStores() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	var out []string
	for _, sub := range []string{".ssh", ".aws", ".gnupg", ".kube", ".docker", ".config/gh", ".netrc"} {
		out = append(out, filepath.Join(home, sub))
	}
	return out
}

// credentialDenied reports whether path falls inside one of the
// credential stores (or is one) under home — the gate's read refusal.
func credentialDenied(path, home string) bool {
	p := filepath.Clean(path)
	for _, sub := range []string{".ssh", ".aws", ".gnupg", ".kube", ".docker", ".config/gh", ".netrc"} {
		store := filepath.Clean(filepath.Join(home, sub))
		if p == store || strings.HasPrefix(p, store+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

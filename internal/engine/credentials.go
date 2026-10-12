package engine

// credentials.go is the gate's read refusal list: the credential
// stores a gated read_file must not return even though reads elsewhere
// are broad (the codex workspace-write contract: read the disk, write
// the workspace, no network). It is the same list the OS sandbox masks
// (internal/sandbox) — what is a secret to the model as a file is a
// secret through a shell command too.

import (
	"os"
	"path/filepath"
	"strings"
)

// credentialDenied reports whether path falls inside one of the
// credential stores (or is one) under home.
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

// homeDir is os.UserHomeDir with an empty-string fallback.
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

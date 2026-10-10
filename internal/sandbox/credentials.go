package sandbox

import (
	"os"
	"path/filepath"
)

// credentialStores are the credential stores a sandboxed command must
// not read even though reads elsewhere are broad (the codex
// workspace-write contract: read the disk, write the workspace, no
// network). .netrc is a bare file; the rest are directories — the
// backends mask them differently for that reason.
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

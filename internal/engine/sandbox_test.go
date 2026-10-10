package engine

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// On every platform the provider must answer Available() honestly:
// windows says no (the gate then turns agent-mode bash into an ask), a
// unix says yes only when its backend binary is actually installed.
func TestSandboxAvailabilityHonest(t *testing.T) {
	sb := NewSandbox()
	switch runtime.GOOS {
	case "darwin":
		_, err := exec.LookPath("sandbox-exec")
		if sb.Available() != (err == nil) {
			t.Fatalf("darwin sandbox.Available()=%v but sandbox-exec lookup err=%v", sb.Available(), err)
		}
	case "linux":
		_, err := exec.LookPath("bwrap")
		if sb.Available() != (err == nil) {
			t.Fatalf("linux sandbox.Available()=%v but bwrap lookup err=%v", sb.Available(), err)
		}
	default:
		if sb.Available() {
			t.Fatalf("%s claims a sandbox backend it does not have", runtime.GOOS)
		}
	}
}

// The boundary's fields ride the interface unchanged (spec §3).
func TestBoundaryShape(t *testing.T) {
	b := Boundary{Workdir: "/w", Scratch: "/s", Network: "deny"}
	if b.Network != "deny" || b.Workdir == "" || b.Scratch == "" {
		t.Fatal("boundary shape")
	}
}

// Credential stores are the paths a sandbox must mask and the file tools
// must refuse: home-anchored, one entry per store.
func TestCredentialStores(t *testing.T) {
	stores := credentialStores()
	if len(stores) == 0 {
		t.Fatal("no credential stores on this machine?")
	}
	for _, s := range stores {
		if !filepath.IsAbs(s) {
			t.Fatalf("store not absolute: %q", s)
		}
	}
}

// credentialDenied pins the same denylist the sandbox masks: what is a
// secret as a file is a secret through a shell command too.
func TestCredentialDenied(t *testing.T) {
	home := t.TempDir()
	if !credentialDenied(filepath.Join(home, ".ssh", "id_rsa"), home) {
		t.Fatal("credential path not denied")
	}
	if !credentialDenied(filepath.Join(home, ".netrc"), home) {
		t.Fatal("netrc not denied")
	}
	if credentialDenied(filepath.Join(home, "notes.txt"), home) {
		t.Fatal("ordinary path denied")
	}
	// The store directory itself, not just its contents.
	if !credentialDenied(home+".ssh", home) && !credentialDenied(filepath.Join(home, ".ssh"), home) {
		t.Fatal("the .ssh directory itself should be denied")
	}
}

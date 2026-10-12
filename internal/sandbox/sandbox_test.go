package sandbox

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
)

// TestSandboxUnavailableIsReported asserts the honesty rule: a platform
// without a sandbox errors instead of running unsandboxed.
func TestSandboxUnavailableIsReported(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin has a sandbox backend")
	}
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("bwrap"); err == nil {
			t.Skip("bwrap available; the live test covers it")
		}
	}
	_, _, err := New().Command(context.Background(), Boundary{Workdir: t.TempDir(), Scratch: t.TempDir(), Network: "deny"}, "/bin/sh", "-c", "true")
	if err == nil {
		t.Fatal("a missing sandbox must be reported, not run unsandboxed")
	}
}

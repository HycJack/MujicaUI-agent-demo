//go:build !windows

package engine

import (
	"os/exec"
	"syscall"
	"time"
)

// ProcGroupAttr guards one CLI process: it runs in its own process group
// and a stop becomes a group SIGTERM (escalating per the exec package's
// WaitDelay), so the CLI and any tool processes it spawned actually die.
func ProcGroupAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		return nil
	}
	cmd.WaitDelay = 3 * time.Second
}

// terminateTree sends the group SIGTERM (cmd.Cancel from ProcGroupAttr);
// the leader kill in Reap covers whatever survives it.
func terminateTree(cmd *exec.Cmd) {
	if cmd.Cancel != nil {
		_ = cmd.Cancel()
		return
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

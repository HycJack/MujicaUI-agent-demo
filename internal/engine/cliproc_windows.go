//go:build windows

package engine

import (
	"os/exec"
	"strconv"
	"time"
)

// ProcGroupAttr guards one CLI process on Windows, where there are no
// process groups: a context cancellation becomes a whole-tree taskkill
// (a claude installed through npm runs as a .cmd wrapper — killing only
// the wrapper leaves its node child alive, still holding the output
// pipe, and the turn hangs on Wait). WaitDelay bounds the wait if even
// the tree kill fails.
func ProcGroupAttr(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		terminateTree(cmd)
		return nil
	}
	cmd.WaitDelay = 3 * time.Second
}

// terminateTree kills a lingering CLI's whole process tree with
// taskkill /T /F.
func terminateTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
}

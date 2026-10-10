//go:build windows

package engine

import (
	"os/exec"
	"strconv"
)

// ProcGroupAttr is a no-op on Windows: there are no process groups. The
// stop path is terminateTree below instead.
func ProcGroupAttr(cmd *exec.Cmd) {}

// terminateTree kills a lingering CLI's whole process tree with
// taskkill /T /F. A claude installed through npm runs as a .cmd wrapper:
// killing only the wrapper leaves its node child — still holding the
// output pipe — running. The tree kill is the one place Crux improves on
// the reference's Windows story.
func terminateTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
}

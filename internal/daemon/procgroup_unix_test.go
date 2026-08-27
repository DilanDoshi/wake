//go:build unix

package daemon

import (
	"os/exec"
	"syscall"
)

// leaveProcessGroup puts the fake agent's grandchild in a group of its own.
//
// It is what makes fakeHold the hard case rather than the easy one: a
// grandchild that stayed in the agent's group would be reached by the group
// kill and would close stdout on its way out, so the scan would end for a
// reason that has nothing to do with the daemon. Outside the group, only
// core's closeOnCancel can end it - which is what the kill verb has to reach.
func leaveProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = 0
}

// processAlive asks the OS rather than inferring from anything Wake holds:
// signal 0 checks for a process without sending one.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

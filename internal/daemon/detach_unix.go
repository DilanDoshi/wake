//go:build unix

package daemon

import (
	"os/exec"
	"syscall"
)

// detach puts the forked daemon in a session of its own, with no controlling
// terminal.
//
// This is the flag the product is made of. A daemon left in the TUI's process
// group dies with the ^C that is supposed to mean "detach, keep working", and
// with the SIGHUP of the terminal window closing - so the fleet would end
// exactly when the user did the thing the daemon exists to survive.
//
// Setsid, not the Setpgid core uses for agents. An agent needs a group Wake
// can signal as a unit; a daemon needs to be out of reach of the terminal's
// signals altogether, which is a session and not just a group.
func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}

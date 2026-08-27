//go:build !unix

// The half of the bang's process-group kill that admits it cannot. Wake is
// built for macOS and Linux; this file exists so the package still compiles
// elsewhere, and it is honest about the cost: with no group to signal, a
// timeout reaches the shell and nothing the shell started, so `!sleep 600 | cat`
// leaves the pipeline running. exec.Cmd.WaitDelay still bounds the *wait* -
// see bang.go - so the UI is never held; what is given up is reclaiming the
// processes. See bangproc_unix.go for what that half does.

package ui

import (
	"os"
	"os/exec"
)

func bangSetGroup(*exec.Cmd) {}

func bangKillGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return cmd.Process.Kill()
}

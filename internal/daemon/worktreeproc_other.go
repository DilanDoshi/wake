//go:build !unix

// The half that admits it cannot. Without process groups there is nothing to
// put git in and nothing to signal a tree with, so Cancel falls back to killing
// the one process os/exec holds - which is all os/exec's own default cancel
// does anyway, and all a non-unix build could. WaitDelay still bounds the wait.
// See worktreeproc_unix.go for what is being given up.

package daemon

import (
	"os"
	"os/exec"
)

func worktreeSetGroup(*exec.Cmd) {}

func worktreeKillGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return cmd.Process.Kill()
}

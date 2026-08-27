//go:build unix

// The platform half of bounding the git commands that create a worktree. A
// `git worktree add` runs the repository's post-checkout hook, and a hook that
// backgrounds a process leaves that process in git's group holding git's stdout
// - so Cancel has to reach the group, not the one pid os/exec holds, or a
// deadline reaps the leader and orphans the rest. worktreeproc_other.go is the
// half that admits it cannot.
//
// This is core's setProcessGroup/killProcessGroup restated for a command core
// does not own, the way internal/ui's bangproc_unix.go restates it for a shell
// line: the signal itself is core.KillGroup - which refuses group 0, 1, a
// negative id and Wake's own - and only the spawn flags are repeated here,
// because core's setProcessGroup is unexported and exporting it is a change to a
// file this does not own.

package daemon

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/DilanDoshi/wake/internal/core"
)

// worktreeSetGroup puts git in a new process group it leads, so
// worktreeKillGroup can reach the hook and anything it started with one signal.
//
// Pgid is set explicitly: Setpgid with a non-zero Pgid means "join group N"
// instead of "lead a new one", and then git's pid is not its group at all -
// which would make the kill below signal an arbitrary group. Stated so the pair
// below can check for it rather than assume it.
func worktreeSetGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = 0
}

// worktreeKillGroup SIGKILLs git's whole process group, and is what
// exec.Cmd.Cancel runs when a git command's deadline passes.
//
// The group id is git's own pid, because worktreeSetGroup made it the leader.
// The Setpgid check is not a formality: without a group of its own git sits in
// the daemon's, and signalling that group would take down the daemon and every
// session it holds - so the single-process fallback keeps anything without the
// flags off the group path. os.ErrProcessDone is os/exec's vocabulary for "it
// had already finished", which core.KillGroup already answers an empty group
// with.
func worktreeKillGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid || cmd.SysProcAttr.Pgid != 0 {
		return cmd.Process.Kill()
	}
	return core.KillGroup(cmd.Process.Pid)
}

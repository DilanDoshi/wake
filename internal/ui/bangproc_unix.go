//go:build unix

// The platform half of bounding a bang. A shell line is not a leaf: `!ls | wc`
// is two processes, `!make test` is a tree, and killing the one pid os/exec
// holds leaves the rest running - holding the pipe Wake is reading, which is
// how a bounded command becomes an unbounded wait. bangproc_other.go is the
// half that admits it cannot.
//
// internal/core does this for agents and its procgroup_unix.go carries the full
// argument for every line below. The signal itself is core.KillGroup rather
// than a second syscall.Kill here: it refuses group 0, 1, a negative id and
// Wake's own group, and those refusals are worth more than the import costs.
// Only the spawn flags are restated, because core's setProcessGroup is
// unexported and exporting it is a change to a file this task does not own.

package ui

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/DilanDoshi/wake/internal/core"
)

// bangSetGroup puts the shell in a new process group it leads, so bangKillGroup
// can reach everything the command started with one signal.
//
// Pgid is set explicitly rather than left to the zero value: Setpgid with a
// non-zero Pgid means "join group N" instead of "lead a new one", and then the
// child's pid is not its group at all - which would make the kill below signal
// an arbitrary group. Nothing here sets it; it is stated so the pair below can
// check for it rather than assume it.
func bangSetGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = 0
}

// bangKillGroup SIGKILLs the shell's whole process group, and is what
// exec.Cmd.Cancel runs when the bang's deadline passes.
//
// The group id is the shell's own pid, because bangSetGroup made it the leader.
// It is deliberately not read back with syscall.Getpgid: a shell that exited
// while what it started keeps running is a zombie until os/exec reaps it,
// Getpgid on a zombie fails, and the fallback would kill something already dead
// while its children carried on - which is the exact case this exists for.
//
// The Setpgid check is not a formality. Without a group of its own the shell
// sits in Wake's, and signalling that group would take down the TUI, the
// terminal job it runs in and every session started from it. Requiring the
// flags bangSetGroup wrote keeps anything else on the single-process path.
//
// os.ErrProcessDone is the vocabulary os/exec's cancel path reads as "it had
// already finished" rather than as a failure to cancel, and core.KillGroup
// already answers an empty group that way.
func bangKillGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid || cmd.SysProcAttr.Pgid != 0 {
		return cmd.Process.Kill()
	}
	return core.KillGroup(cmd.Process.Pid)
}

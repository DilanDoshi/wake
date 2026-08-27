//go:build unix

// Ending an agent means ending everything it spawned. A claude process is not
// a leaf: it starts stdio MCP servers, hooks and shells, and killing the one
// process Wake holds a handle to leaves those running with nobody left who
// knows their names. At 15-30 sessions that is how a laptop fills up with
// claude processes nobody can account for.
//
// So Wake gives each agent its own process group and signals the group. This
// is the platform half of that; procgroup_other.go is the half that admits it
// cannot.

package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup puts the child in a new process group it leads, so
// killProcessGroup can reach it and every descendant that stayed in it with
// one signal.
//
// Two consequences, both wanted. Wake can reap what an agent abandoned. And
// the agents leave Wake's own foreground process group, so a ^C in the
// terminal Wake was started from no longer reaches them - which is what the
// spec's "detach" verb means: the TUI exits, every session keeps working.
//
// # What this costs, which Task 6 has to pay
//
// The same separation means the terminal's SIGHUP and SIGINT no longer sweep
// the agents either. Before this, a hangup took the fleet with it. Now, if the
// daemon is SIGKILLed or crashes, 15-30 claude processes and their whole trees
// survive with nobody holding a handle to them - the exact end state this file
// exists to prevent, arrived at from the other direction.
//
// That is the trade the spec asks for: sessions must outlive the TUI. It is
// not a defect, but it is not free either, and nothing else pays it. **The
// daemon needs a reaper on restart**, and the session UUIDs are on disk
// precisely so a fleet whose daemon is gone can be found again (spec §5).
//
// Pgid is set explicitly rather than left to the zero value. Setpgid with a
// non-zero Pgid means "join group N" instead of "lead a new one", and then the
// child's pid is not its group at all - which would make killProcessGroup
// signal an arbitrary group id. Nothing in Wake sets it, but SysProcAttr
// arrives from whatever execCommand returned, and that is a mutable seam.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = 0
}

// killProcessGroup SIGKILLs the child's whole process group. It reports
// os.ErrProcessDone when there is nothing left to signal, which is the error
// os/exec reads as "it had already finished" rather than as a failure to
// cancel.
//
// SIGKILL rather than a signal the process could handle, because every caller
// is a path where the agent has already stopped behaving: the spec's gentle
// ending is Stop, which closes stdin and lets the in-flight turn finish, and
// Stop must never reach this function.
//
// The group id is the child's own pid, because setProcessGroup made the child
// its leader. It is deliberately *not* read back with Getpgid, which is how
// this was written first and was wrong in exactly the case it exists for: an
// agent that exits while what it spawned keeps running is a zombie until Wake
// reaps it, Getpgid on a zombie fails, and the lookup then fell back to
// killing a process that was already dead while its children carried on. A
// process group outlives its leader - it exists while any member does - so the
// pid recorded at spawn stays the right thing to signal.
//
// The Setpgid check is not a formality. Without a group of its own the child
// sits in Wake's, and kill(-pgid) there would take down Wake, the terminal job
// it runs in and every other session at once. Requiring the flags Wake set -
// Setpgid on, Pgid zero, so the child leads a group and its pid *is* that
// group - keeps every other child on the single-process path, and means this
// function never computes Wake's own pgid at all.
//
// One hazard is real and is named rather than argued away: this can run
// against a pid that has already been reaped. cmd.Cancel is not a call site
// Wake sequences - os/exec's watchCtx invokes it from its own goroutine, and
// Cmd.Wait calls Process.Wait, which reaps, *before* it receives from
// ctxResult. So the two can overlap, and syscall.Kill carries none of the
// reuse protection os.Process.Kill has. The window is a few instructions wide
// and the pid would have to be recycled inside it *and* be leading a group, so
// this is accepted rather than fixed - but it is not impossible, and a comment
// that said it could not happen would be worse than the hazard.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	pgid := processGroup(cmd)
	if pgid == 0 {
		return cmd.Process.Kill()
	}
	return KillGroup(pgid)
}

// processGroup reports the group this child leads, or 0 if it leads none.
//
// Leading one is the whole question. Setpgid with a non-zero Pgid means "join
// group N", and then the child's pid is not its group at all - see the note on
// setProcessGroup. A child that leads no group of its own is in Wake's, and
// its pid must never be handed to anything that signals a group.
func processGroup(cmd *exec.Cmd) int {
	if cmd.Process == nil || cmd.SysProcAttr == nil {
		return 0
	}
	if !cmd.SysProcAttr.Setpgid || cmd.SysProcAttr.Pgid != 0 {
		return 0
	}
	return cmd.Process.Pid
}

// KillGroup SIGKILLs a process group by id, for a caller that holds no handle
// on anything in it.
//
// That caller is the daemon's reaper. Setpgid detached the agents from Wake's
// death, so a daemon that was SIGKILLed leaves 15-30 trees running with no
// *exec.Cmd anywhere - and killProcessGroup, which takes one, cannot express
// that at all. The group id written down at spawn is the only handle left,
// which is why Session.Pgid keeps it after the process is gone.
//
// It refuses four ids rather than trusting its caller, because its caller is
// reading them off a file written by a process that has since died: 0 means
// "the caller's own group" to kill(2), negative ids are not groups, 1 is
// init's, and the caller's own group would take down the daemon, its terminal
// job and every session it had just started. A stale or truncated roster entry
// is a realistic way to arrive here with any of them.
func KillGroup(pgid int) error {
	if pgid <= 1 {
		return fmt.Errorf("refusing to signal process group %d", pgid)
	}
	if pgid == syscall.Getpgrp() {
		return fmt.Errorf("refusing to signal process group %d: it is Wake's own", pgid)
	}
	return killGroupError(syscall.Kill(-pgid, syscall.SIGKILL))
}

// killGroupError puts a kill(2) result into the vocabulary os/exec's cancel
// path reads: os.ErrProcessDone means "it had already finished" and is passed
// over, anything else means the cancel failed and is reported against the
// session. An empty group is the first of those, not the second - it is the
// outcome that was asked for.
func killGroupError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

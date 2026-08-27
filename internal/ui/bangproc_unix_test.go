//go:build unix

package ui

import (
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

// The guard that stops a bang's deadline from signalling Wake's own process
// group - the TUI, the terminal job it runs in, and every session started from
// it, all at once.
//
// A child with no group of its own sits in Wake's, so its pid is not a group id
// and must never be handed to something that signals one. It is killed as a
// single process instead, which this asserts by requiring it to actually die:
// without the check, the group signal goes to a group id that happens to equal
// a pid, finds nothing, reports "already finished" and leaves the child running.
func TestAChildWithNoGroupOfItsOwnIsKilledAsAProcess(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the fixture: %v", err)
	}
	// Deliberately no bangSetGroup: this is the case the guard exists for.

	if err := bangKillGroup(cmd); err != nil {
		t.Fatalf("killing a child that leads no group: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("the fixture exited cleanly, so it was never killed and this asserted nothing")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a child that leads no group survived its kill: the signal went to a group id that is only a pid, and Wake's own group is the one it would reach if that id ever matched")
	}
}

// Nothing to signal is the answer os/exec's cancel path reads as "it had
// already finished" rather than as a failure to cancel. Anything else there
// turns a command that ended on its own into a reported error.
func TestKillingABangThatNeverStartedIsNotAFailure(t *testing.T) {
	if err := bangKillGroup(&exec.Cmd{}); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("killing a bang with no process = %v, want os.ErrProcessDone", err)
	}
}

// The spawn flags the kill checks for, stated where a change to either has to
// be made against the other: leading a new group is what makes the child's pid
// the group id, and joining an existing one would make it something else
// entirely.
func TestABangLeadsItsOwnProcessGroup(t *testing.T) {
	cmd := exec.Command("true")
	bangSetGroup(cmd)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid || cmd.SysProcAttr.Pgid != 0 {
		t.Errorf("SysProcAttr = %+v, want Setpgid with Pgid 0: anything else means the child joins a group rather than leading one, and its pid is then not the group the deadline signals", cmd.SysProcAttr)
	}
}

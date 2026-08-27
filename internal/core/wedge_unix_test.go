//go:build unix

// The wedge, and the session detecting it on its own. A grandchild that
// inherited the agent's stdout write end and outlived the agent holds Wake's
// read end open, so the scan never sees EOF. Before awaitExit that meant
// cmd.Wait never ran, the events channel never closed, and the session was an
// unreaped zombie holding a live-cap slot - broken only by a ctx-cancel or the
// daemon's ps-watchdog, both reporting a FAILURE. This is the Unix-only proof
// that the session now ends itself and calls the ending clean.

package core

import (
	"context"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// A leader that exits while a grandchild holds its stdout is the wedge, and the
// session has to end itself: no cancel, no ps-watchdog, no operator.
//
// awaitExit reaps the leader directly - cmd.Wait sees it go even though the
// pipe never EOFs, because Wake owns the read end and os/exec runs no copier
// over it. A grace after Wait returns, Wake force-closes its own read end, the
// scan ends on ErrClosed, and endErr reads that as the clean ending it is.
//
// The order is the safety property: the force-close happens only after Wait has
// confirmed the process gone, so a merely-quiet live agent is never truncated.
// The pre-wedge turn proves the other half - what was already read is kept.
func TestAWedgedAgentWhoseGrandchildHoldsStdoutEndsItself(t *testing.T) {
	base := runtime.NumGoroutine()
	pidPath := filepath.Join(t.TempDir(), "orphan.pid")
	t.Setenv("WAKE_HELPER_SCRIPT", "orphan-stdout")
	t.Setenv("WAKE_HELPER_PIDFILE", pidPath)
	withFakeExec(t)

	// A plain context, deliberately never cancelled: the whole point is that
	// nothing outside the session breaks the wedge.
	s := NewSession(Config{SessionID: "s1", Dir: t.TempDir()})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The turn before the wedge has to survive. The scan reads it before the
	// pipe stops EOFing, and the force-close after Wait must not truncate what
	// was already read.
	if got := waitForKind(t, s, KindAssistantText); got != "pong 0" {
		t.Fatalf("assistant text = %q, want pong 0 - the turn before the wedge was lost", got)
	}
	grandchild := waitForPid(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(grandchild, syscall.SIGKILL) })

	// Before awaitExit this hangs forever: the scan never sees EOF, cmd.Wait is
	// never reached, and the events channel never closes.
	select {
	case <-drainedAsync(s):
	case <-time.After(waitDelay + 10*time.Second):
		t.Fatal("the session never ended itself: a leader that exited while a grandchild held stdout wedged the pump, and nothing self-detected it")
	}

	// The clean ending, not scanStopError. The process exited on its own;
	// Wake's force-close after Wait is not a stream that outran its process.
	if err := s.Err(); err != nil {
		t.Errorf("Err = %v, want nil - a leader that exited cleanly is a clean ending even when a grandchild held its stdout", err)
	}

	// A closed channel says the teardown ran; only this says nothing the
	// session started - awaitExit included - is still running.
	waitForGoroutines(t, base)
}

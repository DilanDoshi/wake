// How a session ends, and what it reports when it does: the bound on Wait, the
// close that ends a scan nothing else can reach, and the two errors that say
// what became of the stream and what became of the process.
//
// Gathered out of session.go because they are one story told in four places,
// and it is the story this package has been wrong about four separate times.
// session.go keeps the lifecycle; the endings live here, which is where anyone
// building a teardown path on this package is looking.

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// waitDelay bounds how long Wait may spend on I/O once the process is gone.
//
// Capturing stderr costs a pipe: a writer that is not an *os.File makes
// os/exec create one plus a copying goroutine, and with WaitDelay zero Wait
// reads it to EOF - which arrives only when the last descriptor closes,
// including any the agent handed to something it spawned. A stdio MCP server
// outliving its claude process holds exactly that. Wait would never return,
// the events channel would never close, the pump goroutine would leak along
// with every consumer ranging over Events(), and the session would be
// indistinguishable from a healthy idle one while holding a live-cap slot.
//
// It does NOT cover an orphan holding *stdout*: os/exec force-closes the
// pipes only when its own copying goroutines are still blocked (exec.go, the
// default branch of watchCtx's goroutineErr select), and ours is not - the
// stderr copier finishes the moment the process does. Worse, a scan blocked
// on stdout never reaches Wait at all. closeOnCancel is what covers that.
//
// Two seconds is far longer than a dying process needs to flush, and far
// shorter than an operator should wait on a session that is already over.
const waitDelay = 2 * time.Second

// closeOnCancel closes the process's stdout when ctx is cancelled, and
// otherwise retires when the scan finishes.
//
// This is what makes cancelling the context an actual kill. A grandchild that
// inherits stdout and outlives the process holds that pipe open, so Scan
// never returns EOF and cmd.Wait is never reached - and killing the process
// does not close a descriptor somebody else is holding. WaitDelay cannot
// rescue it either, for the reason recorded there. Killing the agent's process
// group reclaims a grandchild that stayed in it; for one that did not - a
// setsid daemon, or something the agent merely handed the descriptor to -
// closing the read end here is the only thing left that can end the scan.
func closeOnCancel(ctx context.Context, stdout io.Closer, scanDone <-chan struct{}) {
	select {
	case <-ctx.Done():
		_ = stdout.Close()
	case <-scanDone:
	}
}

// scanStopError reports a stream that ended before its process did.
//
// The scan failure is the headline because it caused the kill in pump, so it
// outranks the exit status that kill produced - but it does not replace
// either. This branch is *every* cancel, since closeOnCancel makes the scan
// fail, and every oversized line: exactly the wedged endings where stderr is
// the best surviving clue about what the agent was doing. Overwriting the
// whole error with the scan reason threw that away on the endings that need
// it most, which is the same defect exitError had one frame down.
func scanStopError(id string, scanErr, waitErr error, stderrTail string) error {
	err := fmt.Errorf("session %s stopped reading stdout: %w", id, scanErr)
	if waitErr != nil {
		// %v, not %w: the headline is what a caller would test for, and this
		// is usually just the shape of the kill Wake issued a moment ago.
		err = fmt.Errorf("%w; it then exited (%v)", err, waitErr)
	}
	if tail := strings.TrimSpace(stderrTail); tail != "" {
		return fmt.Errorf("%w: %s", err, tail)
	}
	return err
}

// interruptedExitCode is the status a claude process leaves behind when the
// last turn it ran was one Wake interrupted.
//
// `[testimony]` in the findings note (§6): read from cmd.Wait at recording
// time across seven runs, not from a byte on any stream. Two runs whose last
// turn was interrupted exited 1; five whose last turn completed exited 0,
// including two that had been interrupted earlier in their lives. So the code
// follows the last turn, not the interrupt.
const interruptedExitCode = 1

// interruptedExit reports whether an ending has the exact shape a deliberate
// abort leaves behind: exit status 1 with **nothing at all** on stderr.
//
// Both halves are the guard. Every other exit-1 this package has ever seen
// says why on stderr - a missing --verbose, a session id already in use, a
// line claude could not parse - and that text is the whole account of the
// failure, so an ending that carries it is an ending worth reporting whatever
// else happened. A signalled process is excluded by the code check rather than
// by a case of its own: os/exec still reports it as an *ExitError, and
// ExitCode is -1 for one.
//
// The wait-delay ending needs no case either, and the first draft of this
// function had one that could never fire. os/exec's Wait prefers the exit
// status: watch.err (ErrWaitDelay) is taken **only if** Process.Wait produced
// no error, so an ending carrying ErrWaitDelay is never also an *ExitError and
// errors.As has already excluded it. The table below still holds that ending
// to "not suppressed", because what must stay true is the outcome - its empty
// stderr means something the agent spawned held the pipe, which exitError has
// its own sentence about - not the branch that used to claim to produce it.
//
// What it does not model, and the caller narrows: *which* turn was last. The
// recorded mechanism is that the exit code follows the last turn's is_error,
// and is_error is not exclusive to interrupts - a turn can fail on its own. So
// the flag this is paired with is cleared by a successful Send (see
// Session.interrupted): once Wake asks for another turn, the aborted one is no
// longer last and the excuse expires. What remains unmodelled is a turn the
// *agent* started on its own - --brief allows it - failing after an interrupt
// Wake never followed with a send. Closing that needs an aborted KindTurnEnd
// told from a quiet one, which means decoding terminal_reason; nothing does
// yet, and whoever does should replace this pairing rather than add to it.
func interruptedExit(waitErr error, stderrTail string) bool {
	if waitErr == nil || strings.TrimSpace(stderrTail) != "" {
		return false
	}
	var exit *exec.ExitError
	if !errors.As(waitErr, &exit) {
		return false
	}
	return exit.ExitCode() == interruptedExitCode
}

// exitError pairs a non-zero exit with the tail of stderr. A startup
// rejection prints its reason there and nothing on stdout, so this is the
// only place that reason exists.
//
// The stderr clause is appended to every ending, the wait-delay one included.
// What that bound costs is the *trailing* bytes - whatever the process wrote
// after Wake stopped reading - not the bytes already captured, and that
// ending is precisely the one where the process is gone and stderr is the
// whole account of why.
func exitError(id string, waitErr error, stderrTail string) error {
	if waitErr == nil {
		return nil
	}
	err := fmt.Errorf("session %s exited (%w)", id, waitErr)
	if errors.Is(waitErr, exec.ErrWaitDelay) {
		// The process itself exited; something it spawned inherited its
		// stderr and outlived it, so Wake closed the pipe on waitDelay rather
		// than waiting on a grandchild. Said plainly because "exit status 0
		// with an error" reads as a Wake bug.
		err = fmt.Errorf("session %s exited, but something it spawned held its output open past %v (%w)", id, waitDelay, waitErr)
	}
	if tail := strings.TrimSpace(stderrTail); tail != "" {
		return fmt.Errorf("%w: %s", err, tail)
	}
	return err
}

// awaitExit is the session's one cmd.Wait, on its own goroutine so it can reap a
// leader that exited even while the scan is still blocked on a stdout a
// grandchild inherited - the wedge. os/exec runs no copier over Wake's owned
// read end, so Wait returns when the leader goes, whatever still holds the write
// end.
//
// Once Wait has returned - and only then - a grace decides the ending. If the
// scan has not finished, the process is gone but its output is held open: Wake
// tags the ending and closes its own read end to end the scan, and endErr reads
// the ErrClosed that follows as the clean ending it is. If the scan already
// finished - a clean EOF, a cancel, an oversized line - nothing is tagged.
//
// The order is the safety property: the force-close cannot run while the
// process is alive, because this goroutine is still parked in Wait, so a merely
// quiet agent's stream is never truncated. finish reads waitErr back through the
// field once procGone closes.
//
// The grace is a flat waitDelay after Wait, not a measure of scan progress, so
// a consumer that parked the pump in send past it would have an already-exited
// agent's still-buffered tail force-closed unread. Whoever ranges Events() owns
// keeping up (see eventBuffer), that tail is on the leader's transcript to read
// back, and gating the grace on per-read progress is the per-frame cost this
// package refuses - so the flat delay stands.
func (s *Session) awaitExit(cmd *exec.Cmd, stdout io.Closer, scanDone <-chan struct{}, procGone chan<- struct{}) {
	waitErr := cmd.Wait()
	s.mu.Lock()
	s.waitErr = waitErr
	s.mu.Unlock()

	select {
	case <-scanDone:
	case <-time.After(waitDelay):
		s.mu.Lock()
		s.stdoutClosedByExit = true
		s.mu.Unlock()
	}
	// Wake owns this read end, so Wake closes it: on the wedge path this is what
	// ends the blocked scan, and on every other path it is the cleanup os/exec
	// does for a pipe it made but never does for one it was handed. A second
	// close from closeOnCancel on the cancel path is a harmless no-op.
	_ = stdout.Close()
	close(procGone)
}

// exitClosed reports whether the read end was force-closed inside awaitExit's
// post-Wait grace - the wedge. pump reads it to gate its group kill, finish to
// classify the ending.
//
// The bit is set only inside that grace, so only for an already-exited process:
// a live agent's Wait has not returned, the grace timer has not started, and a
// cancel of it leaves this false (scanStopError, unchanged). It is deliberately
// NOT a claim that awaitExit's own Close is what ended the scan - a cancel that
// closes in the same instant could, since the bit is set just before the Close.
// That race is benign because the bit gates two *post-exit* decisions and both
// are right however the close was won: the ending carries the leader's real
// exit status (exitError), and any in-group survivor is retire's park-aware
// KillGroup sweep either way. Setting the bit before the Close is what makes it
// reliably visible once the scan unblocks; setting it after would race pump the
// other way and mis-report a genuine wedge as scanStopError.
func (s *Session) exitClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stdoutClosedByExit
}

// endErr classifies how a session ended into the error Err reports, so finish
// stays a lock and a channel close. It is the ending vocabulary assembled in one
// place, which is what this file is for.
//
// The exitClosed arm is the wedge: awaitExit force-closed Wake's own read end
// after cmd.Wait had confirmed the process gone, so the scan's ErrClosed is not
// a stream that outran its process - it is the process already reaped while a
// grandchild held the write end. Classified as the clean ending it is, carrying
// whatever the leader exited with. Every other scanErr - a cancel's close, an
// oversized line - is a stream that stopped before its process, which is
// scanStopError's headline.
func endErr(id string, scanErr, waitErr error, stderrTail string, interrupted, exitClosed bool, logs *logSink) error {
	switch {
	case scanErr != nil && exitClosed:
		return exitError(id, waitErr, stderrTail)
	case scanErr != nil:
		return scanStopError(id, scanErr, waitErr, stderrTail)
	case interrupted && interruptedExit(waitErr, stderrTail):
		// Not swallowed - said once, where a session's diagnostics go. The
		// status itself is not reported: doing so turns the most ordinary thing
		// an operator does into a phantom crash on the roster. See
		// interruptedExit for how narrow this is.
		logs.printf("wake: session %s exited (%v) with nothing on stderr after Wake interrupted a turn; recorded as an ordinary ending", id, waitErr)
		return nil
	default:
		return exitError(id, waitErr, stderrTail)
	}
}

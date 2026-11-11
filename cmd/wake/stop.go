// `wake stop`: end every session and the daemon, and do not return until that
// has actually happened.

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// stopCeiling bounds the wait for the daemon to let go.
//
// The daemon bounds its own shutdown - a grace for in-flight turns, then a
// group kill for whatever is left, then a bounded wait on its own goroutines -
// and it closes every client unconditionally after all of that. So on a daemon
// that is behaving, the end of this connection is guaranteed to arrive and
// this ceiling is never reached. It is for one that is not, and past it this
// says it could not confirm rather than claiming the fleet is down, which is
// the one thing `wake stop` must never get wrong.
// It is a var only so tests can compress it; nothing outside a test assigns it.
// That is unsynchronised, and safe only because no test in this package calls
// t.Parallel() - the first one that does has to make this a parameter.
var stopCeiling = defaultStopCeiling

const defaultStopCeiling = 2 * time.Minute

// releaseCeiling bounds the wait for the daemon to unlink its socket, and
// releasePoll is how often that is looked for.
//
// After closing its clients a healthy daemon has one bounded step left - a wait
// on its own goroutines - so this is normally over in milliseconds. The ceiling
// is what a daemon that died abnormally costs, because it never unlinks
// anything; past it the report is taken anyway, and it is still correct, only
// later.
//
// releaseCeiling is a var only so tests can compress it; nothing outside a test
// assigns it. Unsynchronised, on the same terms as stopCeiling: safe only
// because no test in this package calls t.Parallel().
var releaseCeiling = defaultReleaseCeiling

const (
	defaultReleaseCeiling = 10 * time.Second
	releasePoll           = 20 * time.Millisecond
)

// stopFleet stops every session, ends the daemon, and waits for it.
//
// # Why it waits
//
// FrameQuit starts a graceful shutdown that lets every in-flight turn finish.
// It does not signal anything. A `wake stop` that wrote the frame and exited
// would return the shell prompt while 20 agents were still mid-Edit, and the
// outgoing daemon holds its listener bound for the whole of that - so the next
// `wake` in that window waits on a daemon that is on its way out, correctly
// and without explanation. The wait is what makes the prompt mean something.
//
// # What the end of the connection does and does not prove
//
// It waits for its own connection to end, because the daemon's shutdown closes
// its clients only after every session has ended or been killed. Polling by
// re-dialling would be both a lie and a hazard: a dial against a
// bound-but-not-accepting listener succeeds until the backlog is exhausted and
// then reports ECONNREFUSED, which is what the daemon's own listen() reads as a
// crashed daemon's stale socket.
//
// But that EOF has two producers, and only one of them is evidence:
//
//   - closeClients, after every session ended - the case above;
//   - the kernel closing the fd because the daemon was SIGKILLed, OOM-killed
//     or panicked. Serve calls shutdown() in its return expression rather than
//     a defer, so a panic in the accept loop skips the stopping entirely while
//     the deferred ln.Close still unlinks the socket.
//
// Nothing on the connection separates them, so the connection ending is
// treated as the end of the *wait* and never as the answer. The answer comes
// from confirmFleetDown, which asks what is actually still running.
//
// # What it claims
//
// That the fleet is down, when the fleet is provably down. Not that the daemon
// process has exited: the socket stays bound briefly after the clients are
// closed while the daemon retires its own goroutines, and `wake` covers that
// window by waiting for hello-or-EOF. And not how many sessions ended, only
// how many it set out to stop.
func stopFleet(socket string, out io.Writer) error {
	conn, err := daemon.Dial(socket)
	if err != nil {
		// Nothing is listening. That is not the same as nothing running:
		// Status falls back to the roster the last daemon left on disk. Its
		// error is discarded rather than elided - the dial having just failed
		// is exactly the path on which Status cannot fail, because it takes
		// the same failure as its signal to read the roster instead.
		st, _ := daemon.Status(socket)
		return reportNothingToStop(st, socket, out)
	}
	defer func() { _ = conn.Close() }()

	frames, errs := rpc.ReadFrames(conn)
	attached, herr := waitForHello(frames, errs, out)
	if herr != nil {
		return herr
	}
	if !attached {
		// The connection ended without a handshake: this dial landed in the
		// listen backlog of a daemon that was already shutting down, and that
		// daemon has now closed its listener. Which of the two EOFs that was
		// is no more knowable here than at the end of the wait below, so it
		// goes through the same confirmation.
		if err := say(out, "A daemon was already stopping."); err != nil {
			return err
		}
		if err := confirmFleetDown(socket, out); err != nil {
			return err
		}
		return endFleet(socket, true, out)
	}

	// Only now, with the daemon provably accepting. The count comes from
	// daemon.Status rather than from a status frame written on the connection
	// above: FrameStatusReply is both a reply and an unsolicited push the
	// daemon sends on every state change, with nothing on the frame to tell
	// them apart, so a client that asks and reads the next reply can be handed
	// one that predates its own question. Asked before the handshake instead,
	// it would also spend its whole timeout in the backlog of exactly the
	// daemon this command exists to wait for. An error costs the count, not
	// the stop.
	st, sterr := daemon.Status(socket)
	if err := say(out, "%s", stoppingLine(st, sterr)); err != nil {
		return err
	}

	// Bounded. rpc's write lock is process-wide and held across the socket
	// write, so a daemon that has stopped reading would park this here with no
	// deadline at all - the SetReadDeadline below bounds only the wait that
	// follows, and never runs. A `wake stop` that hangs before it has asked for
	// anything is the worst shape this command has. See rpc.WriteFrameTo.
	if werr := rpc.WriteFrameTo(conn, rpc.Frame{Kind: rpc.FrameQuit}); werr != nil {
		return fmt.Errorf("asking the daemon to stop: %w", werr)
	}
	if derr := conn.SetReadDeadline(time.Now().Add(stopCeiling)); derr != nil {
		return fmt.Errorf("bounding the wait: %w", derr)
	}

	if werr := waitForTheDaemonToLetGo(frames, errs, daemon.FleetName(socket)); werr != nil {
		return werr
	}
	if err := confirmFleetDown(socket, out); err != nil {
		return err
	}
	// Down, and its daemon cleared the park book: a named fleet ends with it.
	return endFleet(socket, true, out)
}

// confirmFleetDown establishes what is actually still running, now that this
// connection has ended, and says so.
//
// # Why it waits for the socket to go rather than dialling
//
// The answer wanted is daemon.Status's third case: no daemon answering, and
// the roster on disk filtered by which of those processes still exist. Asked
// too early it is not that answer at all - right after closeClients the
// listener is still bound, so the dial lands in the backlog and burns the whole
// status timeout for an i/o error rather than a roster.
//
// So it first waits for the socket file to disappear, which is Serve's last
// act before it releases its lock. That wait costs no connection: os.Stat is
// not a dial, so it consumes no backlog slot, and it cannot produce the
// ECONNREFUSED that a fresh daemon would read as a crashed daemon's debris.
// The price is paid only in the abnormal case - a SIGKILLed daemon never
// unlinks anything, so that wait runs to its ceiling before the report - and
// paying latency there to avoid dialling a listener that may still be bound is
// the right way round.
//
// The unlink is a hurry-up, not a discriminator. A panicking daemon unlinks the
// socket without stopping anything, so the decision rests entirely on what
// Status then reports.
func confirmFleetDown(socket string, out io.Writer) error {
	released := awaitSocketRelease(socket, releaseCeiling)

	st, serr := daemon.Status(socket)
	line, err := stoppedOutcome(st, serr, released)
	if line != "" {
		if werr := say(out, "%s", line); werr != nil {
			return werr
		}
	}
	return err
}

// stoppedOutcome turns the fleet report taken after the daemon let go into
// what to print and what to return. It takes daemon.Status's two results
// directly so the call site reads as one statement.
//
// Anything other than "no daemon, nothing alive" is a failure of this
// command's purpose and returns an error, so a script that ran `wake stop`
// before shutting a laptop finds out from the exit code.
//
// released says whether the daemon unlinked its socket, and it is carried into
// the failures rather than dropped because it is the one fact that separates
// the two ways a daemon can die badly: a panic unwinds through the deferred
// ln.Close and unlinks, a SIGKILL cannot. It changes nothing about the verdict
// - the roster decides that - and everything about what to go and look at.
func stoppedOutcome(st rpc.Status, err error, released bool) (string, error) {
	switch {
	case err != nil:
		return "", fmt.Errorf("the daemon let go, but what is still running could not be established%s: %w",
			unlinkNote(released), err)
	case st.Running:
		return "", fmt.Errorf("a daemon is still answering on that socket, so the fleet was not stopped%s: check `%s`",
			unlinkNote(released), rpc.FleetCommand(st.Fleet, cmdStatus))
	case runningCount(st) > 0:
		return "", fmt.Errorf("the daemon is gone but %s still running with nothing holding them%s: check `%s`",
			agents(runningCount(st)), unlinkNote(released), rpc.FleetCommand(st.Fleet, cmdStatus))
	case st.ProbeIncomplete:
		// runningCount is zero, but the liveness sweep timed out before every
		// record was checked - so zero is an under-count, not an answer. Reported
		// after the runningCount>0 branch so a positive report keeps its specific
		// "still running" error; only the would-be-default false-down is caught.
		return "", fmt.Errorf("could not confirm the fleet is down: the liveness sweep timed out before every session was checked%s: check `%s`",
			unlinkNote(released), rpc.FleetCommand(st.Fleet, cmdStatus))
	default:
		return "Stopped. The fleet is down.", nil
	}
}

// unlinkNote reports a socket the daemon never removed, which is what a killed
// daemon leaves and a panicking one does not.
func unlinkNote(released bool) string {
	if released {
		return ""
	}
	return " (it never unlinked its socket, which is what a killed daemon leaves)"
}

// awaitSocketRelease waits for the daemon to unlink its socket, reporting
// whether it did. It never dials.
func awaitSocketRelease(socket string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		if _, err := os.Stat(socket); errors.Is(err, os.ErrNotExist) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(releasePoll)
	}
}

// waitForTheDaemonToLetGo drains this connection until the daemon closes it,
// naming fleet in the command that says whether it did.
//
// Draining rather than waiting: the daemon keeps broadcasting while it stops,
// and rpc.ReadFrames has no cancellation - abandoning its channel parks that
// goroutine on a send for good.
func waitForTheDaemonToLetGo(frames <-chan rpc.Frame, errs <-chan error, fleet string) error {
	for range frames {
	}
	err := <-errs
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrDeadlineExceeded):
		return fmt.Errorf("the daemon did not let go within %v, so the fleet may still be running: check `%s`",
			stopCeiling, rpc.FleetCommand(fleet, cmdStatus))
	default:
		return fmt.Errorf("waiting for the daemon to stop: %w", err)
	}
}

// stoppingLine says what is being waited for, before the wait starts - and,
// now, what the stop is about to forget.
//
// The parked half is not decoration. Stop is the ending there is no way back
// from, and the daemon clears the park book on the quit frame this line
// precedes, so a fleet somebody parked on purpose stops being offered back a
// moment after this is printed. Saying so before the wait is the only place it
// can be said in time.
func stoppingLine(st rpc.Status, err error) string {
	if err != nil {
		return "Stopping the fleet. Waiting for in-flight turns to finish…"
	}
	n, parked := runningCount(st), parkedCount(st)
	switch {
	case n == 0 && parked == 0:
		return "Stopping the daemon. Nothing is running."
	case n == 0:
		return fmt.Sprintf("Stopping the daemon and forgetting %s parked.", agents(parked))
	case parked == 0:
		return fmt.Sprintf("Stopping %s. Waiting for in-flight turns to finish…", agents(n))
	default:
		return fmt.Sprintf("Stopping %s and forgetting %s parked. Waiting for in-flight turns to finish…", agents(n), agents(parked))
	}
}

// parkedCount is every parked session a report names, all of which a stop
// forgets: one a live daemon parked with ⌃C stays in Sessions, and the park
// book is on Parked, disjoint from them (with no daemon, every parked one is).
// Separate from runningCount because a parked session is not running, and it is
// not gone either.
func parkedCount(st rpc.Status) int {
	n := len(st.Parked)
	for _, s := range st.Sessions {
		if s.State == rpc.StateParked {
			n++
		}
	}
	return n
}

// reportNothingToStop covers the two ways there is no daemon to talk to, and
// they are not the same thing - one of them exits zero and the other must not.
//
// An empty roster is a genuine no-op: nothing was running, nothing failed, and
// the caller's postcondition already holds. That exits zero.
//
// Live orphans are the same state stoppedOutcome treats as a failure, reached
// through a different door, and this branch has *positively established* it:
// daemon.FleetOnDisk's roster half is already filtered by whether each process still
// exists. One command must not give one state two answers, and `wake stop &&
// rm -rf worktrees` is what agreeing costs.
//
// **The count is runningCount and no longer len(st.Sessions)**, because that
// report now carries a third kind of row. daemon.FleetOnDisk reads the park book as
// well as the roster, and a parked session has no process at all - so a machine
// whose whole fleet was parked would have exited non-zero here saying those
// sessions were "left behind with nothing holding them". Nothing is holding
// them; that is what parking is.
//
// The printed lines stay as they are - they carry the advice, which an error
// line cannot - and the error is a short summary for the exit code.
func reportNothingToStop(st rpc.Status, socket string, out io.Writer) error {
	if st.ProbeIncomplete {
		// The liveness sweep timed out before every roster record was verified,
		// so runningCount is an under-count and "nothing is running" would be a
		// guess in the one direction `wake stop` must never guess. Say so and
		// fail, the way stoppedOutcome does for the same signal on the live path.
		if err := say(out, "Could not confirm the fleet is down: the liveness sweep timed out before every session was checked. Check `%s`.",
			rpc.FleetCommand(st.Fleet, cmdStatus)); err != nil {
			return err
		}
		return fmt.Errorf("could not confirm the fleet is down: the liveness sweep timed out before every session was checked")
	}
	if runningCount(st) == 0 {
		if parked := parkedCount(st); parked > 0 && (st.Fleet == "" || st.Fleet == daemon.LegacyFleet) {
			// A named fleet's removal forgets its parked sessions and names them
			// (endFleet); the unnamed fleet and a socket set by hand have no
			// directory to remove, so their park book waits for a daemon's stop.
			return say(out, "No daemon is running. %s parked, with their transcripts still on disk; "+
				"`%s` starts a daemon that holds them again, and `%s` run while that daemon is up forgets them for good.",
				agents(parked), rpc.FleetCommand(st.Fleet), rpc.FleetCommand(st.Fleet, cmdStop))
		}
		if err := say(out, "No daemon is running."); err != nil {
			return err
		}
		return endFleet(socket, false, out)
	}
	if err := say(out, "No daemon is running, but a previous one left %s behind.", agents(runningCount(st))); err != nil {
		return err
	}
	if err := say(out, "`wake stop` cannot reach them - there is nothing to ask. Starting a daemon with `%s` reaps what it can identify.",
		rpc.FleetCommand(st.Fleet)); err != nil {
		return err
	}
	return fmt.Errorf("the fleet is not down: %s still running with nothing holding them", agents(runningCount(st)))
}

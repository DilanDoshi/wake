// Starting a real daemon and attaching a real client to it. Every test in
// this package goes through these, because a test that starts a daemon,
// attaches over a socket and drives a whole lifecycle constrains far more
// than a dozen asserting on internals.

package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// waitFor bounds every wait in these tests, so a daemon bug fails a test
// rather than hanging the package until its timeout.
//
// It is also what waitForExit gives Serve to return, which couples it to how
// long a shutdown may legitimately take. shutdownSlack is that relation, and
// TestTheShutdownBudgetKeepsItsMargin holds it.
const testTimeout = 15 * time.Second

// testQuitGrace is the grace every daemon this binary runs shuts down at.
// TestMain installs it before anything else.
//
// Production's defaultQuitGrace is 30s, which is twice testTimeout. So a test
// that aborts while still holding an agent stop cannot end - a `hold`, a
// `deaf`, a long `slow` - fails twice: once for whatever actually went wrong,
// and once more with "Serve did not return within 15s of being cancelled" and
// a full stack dump. The second one names a daemon bug that is not there and
// scrolls the real one off the top, which is why two sightings of this never
// looked like the same defect. Measured at the default, holding one wedged
// agent: Serve returned after 30.015s against a 15s bound.
//
// Compressed for the whole binary rather than opted into per test. The opt-in
// existed, three tests that needed it did not call it, and nothing in the
// failure points at it. Two seconds is what the tests that did opt in chose,
// and nothing here wants a daemon to spend longer than that letting a fake
// agent finish a turn.
//
// Raising testTimeout is the other way to make the arithmetic work and it is
// the wrong one: the bound is already below defaultQuitGrace, so raising it to
// 31s would hide the cascade rather than end it.
const testQuitGrace = 2 * time.Second

// restartGuardGrace is the one grace in this package that is not
// testQuitGrace. TestARestartDuringShutdownDoesNotReapTheFleetItIsWaitingFor
// holds the shutdown window open across a dial, a roster read and a whole
// second Serve, and buys a second of headroom for a loaded machine.
//
// Stated here rather than beside that test so every grace this suite runs at
// sits next to the arithmetic that has to bound it.
const restartGuardGrace = 3 * time.Second

// shutdownBudgetMargin is the slack that must be left between the longest
// shutdown a daemon here may legitimately take and the testTimeout that
// waitForExit bounds it with.
//
// It is generous on purpose. The two halves of the budget are wall-clock
// timers, so they do not shrink on a fast machine or stretch on a slow one -
// but everything around them does, and this bound exists to catch a daemon
// that cannot exit at all rather than one that is merely late.
const shutdownBudgetMargin = 5 * time.Second

// shutdownSlack is what is left of waitForExit's bound after a daemon has
// spent its entire grace and then waited out shutdownWait.
//
// It is the one arithmetic relation the timeouts in this package have to
// satisfy, written once so that the guard below and the seam that changes a
// grace cannot drift apart.
func shutdownSlack(grace time.Duration) time.Duration {
	return testTimeout - (grace + shutdownWait)
}

// TestTheShutdownBudgetKeepsItsMargin asserts the coupling that decides
// whether a failing daemon test tells you what went wrong.
//
// # Why this reads the variable and not the constants behind it
//
// The version of this guard that shipped last round asserted
// restartGuardGrace + shutdownWait against testTimeout - both compressed
// values, from the single test that compressed them. The relation it existed
// to protect was violated by a factor of two at the grace the other fifty
// tests ran at, 30s against a 15s bound, and this test was green through all
// of it. Reintroducing the bug on purpose - running any test that holds an
// un-stoppable agent to an early abort - left it passing.
//
// Reading quitGrace is what fixes that. Deleting TestMain's compression now
// fails here, in arithmetic that costs nothing to run and names the numbers,
// instead of somewhere else fifteen seconds and a stack dump at a time. Any
// other grace enters through shortQuitGrace, which refuses one that would
// blow the same budget.
func TestTheShutdownBudgetKeepsItsMargin(t *testing.T) {
	for _, tc := range []struct {
		what  string
		grace time.Duration
	}{
		// The live variable, deliberately: what this binary is running at,
		// rather than what a constant says it ought to be.
		{what: "the grace this binary runs at", grace: quitGrace},
		{what: "restartGuardGrace, which one test opts into", grace: restartGuardGrace},
	} {
		slack := shutdownSlack(tc.grace)
		if slack < shutdownBudgetMargin {
			t.Errorf("%s is %v, so the worst legitimate shutdown is %v (that grace plus shutdownWait %v) "+
				"against a testTimeout of %v - %v of slack, less than the %v this suite needs.\n"+
				"Compress the grace or lower shutdownWait. Do not raise testTimeout: it is already "+
				"below defaultQuitGrace (%v), so raising it to fit hides the cascade instead of "+
				"ending it.\nThe failure this prevents is a daemon test timing out inside "+
				"waitForExit, which reads as a daemon that cannot shut down and is not one.",
				tc.what, tc.grace, tc.grace+shutdownWait, shutdownWait, testTimeout, slack,
				shutdownBudgetMargin, defaultQuitGrace)
		}
	}
}

// The session ids these tests spawn with.
//
// They are UUIDs because the daemon requires them to be, and the daemon
// requires it because the reaper decides whether to SIGKILL a process group by
// looking for the id in a live process's command line. Readable ids like "s1"
// were what these tests used first, and they are exactly what makes that match
// somebody's shell job - so the tests now spawn what production spawns.
//
// # Why they are per-process rather than constants
//
// They were three fixed constants, and that made two test binaries running at
// once fail each other's wake tests - deterministically, and with a message
// that accuses the code under test:
//
//	a process is still running under session a11a0000-...-00000000a11a,
//	so resuming it would put two processes on one transcript
//
// which is `resumeSafe` being *right*. It asks the whole machine (`ps -Aww`,
// matching core.SessionArgvMarkers) because a stray `claude --resume` is a real
// hazard and the OS is the only thing that can see one. With a fixed fixture id
// the other binary's fake agent *is* that stray process.
//
// This is not hypothetical and it is not only about two people: `go test ./...`
// runs packages in parallel by default, and `internal/daemon` and `cmd/wake`
// both spawn agents under these ids. It cost a long afternoon of diagnosing a
// "flake" that was two sessions testing at the same time.
//
// So the node field carries a per-process nonce. The tag stays at both ends, so
// a failure message still reads a11a...a11a and is recognisable at a glance,
// and the shape stays a valid v4 UUID because only the last field moves -
// uuid.Parse is what mintedByWake uses, and every one of these has to pass it.
var (
	idAlpha = testSessionID("a11a")
	idBeta  = testSessionID("b22b")
	idGamma = testSessionID("c33c")
)

// soleAsk is the one ask a session reports, or "" for none. It fails when a
// session names more than one, since every caller here expects zero or one -
// the concurrent case has a test of its own that reads RequestIDs directly.
func soleAsk(t *testing.T, s rpc.SessionStatus) string {
	t.Helper()
	switch len(s.RequestIDs) {
	case 0:
		return ""
	case 1:
		return s.RequestIDs[0]
	default:
		t.Fatalf("session names %d asks, want at most one: %v", len(s.RequestIDs), s.RequestIDs)
		return ""
	}
}

// testRunNonce is eight hex digits that no other test binary on this machine
// shares. Random rather than the pid, because pids are reused within seconds
// and a rerun that collides with the run before it is the same bug with a
// harder-to-see cause.
//
// math/rand/v2 rather than crypto/rand: this seeds itself differently per
// process, which is the whole requirement, and nothing here is a secret - the
// ids end up in an argv that `ps` prints to anyone on the machine.
var testRunNonce = fmt.Sprintf("%08x", rand.Uint32())

// testSessionID is one fixture role's id for this process.
func testSessionID(tag string) string {
	return tag + "0000-0000-4000-8000-" + testRunNonce + tag
}

// testDaemon is a Serve running in this process on its own socket.
type testDaemon struct {
	socket string
	err    <-chan error
	cancel context.CancelFunc
	exited bool
}

// startDaemon runs Serve on a socket of its own until the test ends, and fails
// the test with Serve's own error rather than with a timeout if it never gets
// listening.
func startDaemon(t *testing.T) *testDaemon {
	t.Helper()
	return startDaemonOn(t, tempSocket(t))
}

// startDaemonOn is startDaemon on a socket the caller chose, and it exists for
// one kind of test: a daemon that succeeds another one.
//
// Everything a daemon leaves for the next daemon sits beside the socket - the
// lock, the roster, and now the park book - so two daemons over *one* socket is
// the only way for a test to be the same machine twice. Two entry points and
// one launcher, rather than a second copy: a restart test that started its
// daemons differently would be asserting about a shutdown path nothing else in
// this package uses.
func startDaemonOn(t *testing.T, socket string) *testDaemon {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- Serve(ctx, socket) }()

	d := &testDaemon{socket: socket, err: errc, cancel: cancel}
	t.Cleanup(func() { d.stop(t) })
	d.waitForListening(t)
	return d
}

// stop ends this daemon and waits for Serve to return, which is what makes the
// socket, the lock and the park book available to a successor.
//
// It is the cleanup every daemon here already had, named so a test can ask for
// it early. Idempotent through waitForExit, so a test that stops its own daemon
// and the cleanup that follows do not both go looking for Serve's error.
func (d *testDaemon) stop(t *testing.T) {
	t.Helper()
	d.cancel()
	d.waitForExit(t)
}

func (d *testDaemon) waitForListening(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if info, err := os.Lstat(d.socket); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		select {
		case err := <-d.err:
			// Marked exited before failing, because this read *is* the read
			// waitForExit would make: d.err is buffered once and consumed
			// here, so the cleanup would go looking for a value that is gone
			// and block the full testTimeout, burying the real failure under a
			// second misleading one. Same one-shot hazard as lingerer.died.
			d.exited = true
			t.Fatalf("Serve returned before it was listening: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("daemon never started listening")
}

func (d *testDaemon) waitForExitWithin(t *testing.T, within time.Duration) bool {
	t.Helper()
	if d.exited {
		return true
	}
	select {
	case err := <-d.err:
		d.exited = true
		if err != nil {
			t.Errorf("Serve: %v", err)
		}
		return true
	case <-time.After(within):
		return false
	}
}

// waitForExit insists Serve actually returns. A daemon that cannot be shut
// down is the bug this package is most at risk of, so it is checked after
// every single test rather than in one of them.
func (d *testDaemon) waitForExit(t *testing.T) {
	t.Helper()

	// Idempotent: a test that shuts the daemon down itself calls this, and
	// so does the cleanup. Reading a channel twice would block on the second.
	if d.exited {
		return
	}
	d.exited = true

	select {
	case err := <-d.err:
		if err != nil {
			t.Errorf("Serve: %v", err)
		}
	case <-time.After(testTimeout):
		t.Errorf("Serve did not return within %v of being cancelled: %s", testTimeout, allStacks())
	}
}

// testClient is one attached TUI.
type testClient struct {
	t      *testing.T
	conn   net.Conn
	frames <-chan rpc.Frame
	errs   <-chan error
	seen   []rpc.Frame
	// held is what earlier waits read past and did not want. See await.
	held []rpc.Frame
}

// attach dials the daemon and consumes the hello, so every test starts from
// the same place.
func attach(t *testing.T, socket string) *testClient {
	t.Helper()
	return attachConn(t, mustDial(t, socket))
}

// attachConn is attach for a connection somebody else already opened -
// EnsureRunning hands one back, and dialling a second time would be a
// different client.
func attachConn(t *testing.T, conn net.Conn) *testClient {
	t.Helper()

	frames, errs := rpc.ReadFrames(conn)
	c := &testClient{t: t, conn: conn, frames: frames, errs: errs}
	t.Cleanup(c.close)

	if f := c.await("hello", func(f rpc.Frame) bool { return f.Kind == rpc.FrameHello }); f.Kind != rpc.FrameHello {
		t.Fatalf("first frame = %+v, want a hello", f)
	}
	return c
}

// listenRaw binds a socket with no daemon behind it, for the tests about what
// a client does when the far side does not behave.
func listenRaw(t *testing.T, socket string) net.Listener {
	t.Helper()

	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen %s: %v", socket, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func mustDial(t *testing.T, socket string) net.Conn {
	t.Helper()

	conn, err := Dial(socket)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return conn
}

// close hangs up and drains. Draining is not optional: rpc.ReadFrames has no
// cancellation, and abandoning its channel parks that goroutine on a send
// forever - which would show up as a leak in whichever test ran next.
func (c *testClient) close() {
	_ = c.conn.Close()
	for range c.frames {
	}
	<-c.errs
}

func (c *testClient) send(f rpc.Frame) {
	c.t.Helper()
	if err := rpc.WriteFrame(c.conn, f); err != nil {
		c.t.Fatalf("write %s frame: %v", f.Kind, err)
	}
}

// await returns a matching frame from anywhere in this client's stream -
// including one that arrived before the wait began - and fails with everything
// it saw. The transcript is the useful half: a daemon that answered the wrong
// thing is far more common than one that answered nothing.
//
// # Why the frames it reads past are kept
//
// This read used to be destructive. A frame that did not match was dropped,
// so a wait whose frame had already been eaten by an earlier wait sat out the
// full testTimeout for something that had come and gone - a lost wakeup, in
// the harness rather than in the daemon. Every frame these tests wait for is
// broadcast to every attached client, and the order two sessions' frames
// arrive in is not a property any test here is about, so a wait has no
// business consuming another wait's evidence.
//
// It bit under load rather than at rest, which is what made it look like a
// daemon that was merely slow: spawning forks and execs a real process, and a
// loaded machine widens the window in which one session's first event
// overtakes another session's spawn confirmation. Measured on 18 cores against
// 24 busy loops, load average 22-57: 13 failures in 6 runs of this package
// before, 6 in 3 runs under -race; 0 and 0 after, with the daemon untouched.
// Every one of them printed the frame it was waiting for in its own transcript.
//
// The production client keeps every frame too - ui.App.apply has a branch for
// every kind and a default that reports the ones it does not know - so this is
// the harness catching up with its client, not an indulgence.
//
// A reply is never left behind by this. Every helper here asks a question and
// then waits for its answer before asking another, so replies are one to one
// with questions and each is matched by the wait that asked for it; what ends
// up in held is events, pushes and errors, which nobody asked for.
func (c *testClient) await(what string, match func(rpc.Frame) bool) rpc.Frame {
	c.t.Helper()

	if f, ok := c.takeHeld(match); ok {
		return f
	}

	deadline := time.After(testTimeout)
	for {
		select {
		case f, open := <-c.frames:
			if !open {
				c.t.Fatalf("the daemon hung up while waiting for %s\nsaw: %s", what, c.transcript())
			}
			c.seen = append(c.seen, f)
			if match(f) {
				return f
			}
			c.held = append(c.held, f)
		case err := <-c.errs:
			c.t.Fatalf("read while waiting for %s: %v\nsaw: %s", what, err, c.transcript())
		case <-deadline:
			c.t.Fatalf("waited %v for %s and it never arrived\nsaw: %s", testTimeout, what, c.transcript())
		}
	}
}

// awaitClose waits for the daemon to hang up on this client, draining whatever
// is still in flight on the way.
//
// It marks an *ordering* rather than a duration, which is why it is not a
// sleep: the daemon closes its clients at one exact point in shutdown, and
// anything asserted after this returns is asserted about a daemon that has
// reached that point. A sleep would assert about a clock.
//
// Bounded like every other wait here, and deliberately not the bare `for range
// c.frames` that reads as three obvious lines. An unbounded drain turns "the
// daemon never quit" into a package timeout with no failure message and no
// transcript - the shape docs/notes/decisions.md names, where a mutation that
// hangs and a mutation that fails look identical in a summary line.
//
// It selects on frames alone. rpc.ReadFrames closes both of its channels from
// the same goroutine, errs first and frames second, so frames closing is the
// complete signal - and leaving errs alone keeps the one value it may hold for
// close(), which reads it.
func (c *testClient) awaitClose() {
	c.t.Helper()

	deadline := time.After(testTimeout)
	for {
		select {
		case f, open := <-c.frames:
			if !open {
				return
			}
			c.seen = append(c.seen, f)
		case <-deadline:
			c.t.Fatalf("waited %v for the daemon to close this connection and it never did\nsaw: %s",
				testTimeout, c.transcript())
		}
	}
}

// takeHeld returns the first frame an earlier wait read past that satisfies
// this one, and removes it.
//
// The remainder is built into a fresh slice rather than shifted in place.
// held is the one piece of state every wait on a client shares, and a helper
// that reused its backing array would be one aliasing mistake away from a test
// failing because of what some other wait did to a slice header.
func (c *testClient) takeHeld(match func(rpc.Frame) bool) (rpc.Frame, bool) {
	for i, f := range c.held {
		if !match(f) {
			continue
		}
		rest := make([]rpc.Frame, 0, len(c.held)-1)
		rest = append(rest, c.held[:i]...)
		rest = append(rest, c.held[i+1:]...)
		c.held = rest
		return f, true
	}
	return rpc.Frame{}, false
}

// awaitEvent waits for one session's event carrying text.
func (c *testClient) awaitEvent(sessionID, text string) rpc.Frame {
	c.t.Helper()
	return c.await(fmt.Sprintf("an event for %s containing %q", sessionID, text), func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == sessionID &&
			f.Event != nil && strings.Contains(f.Event.Text, text)
	})
}

// awaitState waits for an unsolicited status *push* in which one session is in
// a given state.
//
// FrameStatusPush and not FrameStatusReply, and that is the whole assertion: a
// reply would prove only that the daemon answered a question, and the property
// under test is that it speaks without being asked.
func (c *testClient) awaitState(sessionID, state string) rpc.SessionStatus {
	c.t.Helper()
	var got rpc.SessionStatus
	c.await(fmt.Sprintf("session %s pushed in state %q", sessionID, state), func(f rpc.Frame) bool {
		if f.Kind != rpc.FrameStatusPush || f.Status == nil {
			return false
		}
		for _, s := range f.Status.Sessions {
			if s.ID == sessionID && s.State == state {
				got = s
				return true
			}
		}
		return false
	})
	return got
}

// pollState asks for status until a session reaches a state.
//
// Deliberately not awaitState: that one waits for the daemon to *push* a
// status reply, which happens on a liveness tick, and a test that polls is
// testing the state rather than the announcement. The two are separate
// properties and awaitState is for the other one.
func (c *testClient) pollState(sessionID, state string) rpc.SessionStatus {
	c.t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		// Through sessionRow, so a parked record on Status.Parked counts. A scan
		// of Sessions alone waits out the whole timeout for every parked session
		// a previous daemon left, which is now every one of them.
		if s := sessionRow(c.status(), sessionID); s.State == state {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.t.Fatalf("session %s never reached state %q\nsaw: %s", sessionID, state, c.transcript())
	return rpc.SessionStatus{}
}

// watchAsksPerWindow is how many times a watched window asks for status.
//
// Enough that a state the daemon entered briefly in the middle of a window is
// sampled rather than missed between two questions, and few enough that the
// asking is not itself the load these tests are measuring the daemon under.
const watchAsksPerWindow = 8

// watchStates reads for a while and returns every state the daemon reported
// for one session, in the order it reported them.
//
// It exists because "it never became X" cannot be asserted by asking once. A
// test that slept and then called status() read whichever frame was at the
// front of the queue - which, while replies and announcements shared one kind,
// could be an announcement made before the state it was asserting about had
// changed. The mutation that flips a living agent to silent left it passing,
// with the kill it causes printed in the log above the PASS. Splitting the
// kinds settled that for status(); this still has to read the window, because
// the question is about a whole interval rather than one moment.
//
// Both kinds count. What is being observed is everything the daemon said about
// the session, and it says it both ways.
//
// It reads the socket and does not consult await's held frames, and that is
// the difference between the two helpers rather than an oversight. await
// answers "did this ever happen", so it has to consider a frame that arrived
// before the wait started; this answers "what did the daemon say over the next
// d", which begins now by definition. A frame an earlier wait read past
// belongs to the interval before the window, and folding it in would make the
// answer about a different interval than the one the caller named.
//
// The guarantee is unconditional only because a dropped frame is not silent.
// client.enqueue drops when a client's queue is full, and a dropped push would
// be a state this never saw and the daemon did enter - so the gap notice is a
// failure here rather than something to read past.
//
// # Why it asks rather than only listening
//
// It used to do nothing but read, and that made its non-vacuity check a race.
// The daemon pushes only when a state has *moved* (server.statesChanged), and
// every caller of stayedIn is asserting that a state does not move - so the
// window contained a frame only when some announcement left over from the
// setup happened to land inside it. Whether it did was decided by whether the
// agent's last state change was announced before or after the client read the
// event the setup waited for, which are two unrelated timings: the agent's own
// output racing the fan-out. On a loaded machine the announcement comes first
// and the window is empty, which fails as "this observed nothing" - a green
// test that was green by luck. Recorded in CI on 2026-08-10 and reproduced
// on demand by draining the socket quiet before opening the window.
//
// So the window carries its own evidence. Asking is consistent with what this
// helper is for: its question is what the daemon said about the session over
// an interval, and it counts both kinds already.
//
// Every question is answered before this returns. await matches a reply to the
// wait that asked for it - "replies are one to one with questions" is the
// invariant its doc rests on - so a reply left in flight here would be taken
// by some later helper's question and answered with a snapshot from before it
// was asked.
func (c *testClient) watchStates(sessionID string, d time.Duration) []string {
	c.t.Helper()

	var states []string
	outstanding := 0

	record := func(f rpc.Frame) {
		c.seen = append(c.seen, f)
		if f.Kind == rpc.FrameError && strings.Contains(f.Text, gapNotice) {
			c.t.Fatalf("the daemon dropped frames for this client (%q) while watching %s, so a state this never saw is no longer evidence the daemon never entered it", f.Text, sessionID)
		}
		if f.Status == nil || (f.Kind != rpc.FrameStatusReply && f.Kind != rpc.FrameStatusPush) {
			return
		}
		if f.Kind == rpc.FrameStatusReply {
			outstanding--
		}
		for _, s := range f.Status.Sessions {
			if s.ID == sessionID {
				states = append(states, s.State)
			}
		}
	}
	ask := func() {
		c.send(rpc.Frame{Kind: rpc.FrameStatus})
		outstanding++
	}

	ask()
	asks := time.NewTicker(max(d/watchAsksPerWindow, time.Millisecond))
	defer asks.Stop()

	deadline := time.After(d)
	for {
		select {
		case <-asks.C:
			ask()
		case f, open := <-c.frames:
			if !open {
				c.t.Fatalf("the daemon hung up while watching %s\nsaw: %s", sessionID, c.transcript())
			}
			record(f)
		case err := <-c.errs:
			c.t.Fatalf("read while watching %s: %v\nsaw: %s", sessionID, err, c.transcript())
		case <-deadline:
			late := time.After(testTimeout)
			for outstanding > 0 {
				select {
				case f, open := <-c.frames:
					if !open {
						c.t.Fatalf("the daemon hung up while watching %s\nsaw: %s", sessionID, c.transcript())
					}
					record(f)
				case err := <-c.errs:
					c.t.Fatalf("read while watching %s: %v\nsaw: %s", sessionID, err, c.transcript())
				case <-late:
					c.t.Fatalf("the daemon left %d status questions unanswered after watching %s for %v\nsaw: %s", outstanding, sessionID, d, c.transcript())
				}
			}
			return states
		}
	}
}

// stayedIn asserts that every state the daemon reported for a session over a
// window was the one named, and that it reported the session at all - an empty
// window would otherwise pass by having observed nothing.
//
// The empty case is a real failure rather than a timing one: watchStates asks
// for status through the whole window, so reaching here means the daemon
// answered a question without naming a session it is holding.
func (c *testClient) stayedIn(sessionID, state string, d time.Duration, why string) {
	c.t.Helper()

	states := c.watchStates(sessionID, d)
	if len(states) == 0 {
		c.t.Fatalf("the daemon answered every status question over %v without naming session %s, so this observed nothing\nsaw: %s", d, sessionID, c.transcript())
	}
	for _, got := range states {
		if got != state {
			c.t.Fatalf("the daemon reported session %s as %q within %v, want %q throughout: %s\nstates: %v", sessionID, got, d, state, why, states)
		}
	}
}

// enterShutdownWindow quits the daemon and returns once it is provably inside
// the grace - the accept loop finished, sessions still ending.
//
// The barrier is a frame rather than a poll, and it costs one round trip. A
// spawn is refused with exactly this text only after beginQuit has closed
// s.quit (maySpawn reads it through stopping), and serveClient keeps
// dispatching for the whole grace because clients are not closed until
// shutdown is nearly over - so the refusal arriving is proof the daemon
// entered the window and has not left it.
//
// Nothing else in this package is such a barrier. The daemon writes its roster
// during spawn, so waiting for the roster is already satisfied before the quit
// is sent; the socket file's disappearance is the wrong edge for the opposite
// reason. Both were tried and both were races.
func (c *testClient) enterShutdownWindow() {
	c.t.Helper()

	c.send(rpc.Frame{Kind: rpc.FrameQuit})
	// A name the daemon would accept, deliberately. The refusal this waits for
	// has to be the *shutdown* one, and maySpawn checks stopping before it
	// claims a name - so an illegal name here would still produce the right text
	// today and would produce a name refusal, and a fifteen-second wait for a
	// frame that never comes, the day those two checks swap places.
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idBeta, Text: "intruder"})
	c.await("the refusal that proves the daemon is shutting down", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && strings.Contains(f.Text, "shutting down")
	})
}

// pollQuietFor asks for status until a session has been quiet for at least d,
// and returns that row.
//
// It exists because "two equally quiet sessions" is a precondition no test gets
// merely by spawning two: the second one's clock starts later, so at the moment
// the first crosses a threshold the second is behind it by however long the two
// spawns took. A test that compares them there is comparing a session past the
// limit with one that has not reached it, and whether that happens to hold
// depends on how fast the daemon answered - which is not a property anything
// here is about. Waiting for the slower one makes the comparison real.
func (c *testClient) pollQuietFor(sessionID string, d time.Duration) rpc.SessionStatus {
	c.t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		for _, s := range c.status().Sessions {
			if s.ID == sessionID && s.QuietMS >= d.Milliseconds() {
				return s
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.t.Fatalf("session %s was never reported quiet for %v, so nothing was ever compared against an equally quiet session\nsaw: %s", sessionID, d, c.transcript())
	return rpc.SessionStatus{}
}

// status asks and waits for the answer.
func (c *testClient) status() rpc.Status {
	c.t.Helper()
	c.send(rpc.Frame{Kind: rpc.FrameStatus})
	f := c.await("a status reply", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameStatusReply && f.Status != nil
	})
	return *f.Status
}

// spawn starts a session and waits until the daemon reports holding it.
//
// "Holding it" excludes an ended row on purpose. A status reply now replays
// recent endings, so a remembered StateEnded for this id - from an earlier
// lifecycle in the same test, or from the reply that announced it - satisfies
// a bare id match and this would return before the daemon had started
// anything. soak_test.go's holds predicate was corrected for exactly this;
// this one had the same hole.
func (c *testClient) spawn(id, name string) {
	c.t.Helper()
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: id, Text: name})
	c.await("the spawned session in a status reply", func(f rpc.Frame) bool {
		if f.Kind != rpc.FrameStatusReply || f.Status == nil {
			return false
		}
		for _, s := range f.Status.Sessions {
			if s.ID == id && s.State != rpc.StateEnded {
				return true
			}
		}
		return false
	})
}

// TestAWaitIsSatisfiedByAFrameAnEarlierWaitReadPast is the harness testing
// itself, because the harness had the bug.
//
// Under the destructive await this is a fifteen-second timeout on a frame
// printed in its own failure message, which is exactly what three tests in
// this package produced under load - and it happens here on demand rather than
// on a loaded machine, because the fake agent's own order guarantees the
// setup: it emits "ready" as its first act, before it ever reads stdin, so an
// echo can only follow it.
func TestAWaitIsSatisfiedByAFrameAnEarlierWaitReadPast(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "the second turn"})

	// Reads past "ready" to get here.
	c.awaitEvent(idAlpha, "the second turn")

	// The precondition, asserted rather than assumed: this proves nothing
	// unless the frame the wait below wants really did arrive first. Without
	// it the test would pass by having observed nothing the day the fake
	// agent's order changed.
	readyAt, echoAt := -1, -1
	for i, f := range c.seen {
		if f.Kind != rpc.FrameEvent || f.SessionID != idAlpha || f.Event == nil {
			continue
		}
		if readyAt < 0 && strings.Contains(f.Event.Text, "ready") {
			readyAt = i
		}
		if echoAt < 0 && strings.Contains(f.Event.Text, "the second turn") {
			echoAt = i
		}
	}
	if readyAt < 0 || echoAt < 0 || readyAt > echoAt {
		t.Fatalf("this client saw ready at %d and the echo at %d, want ready strictly first - "+
			"there is then no frame that was read past and nothing here is being tested\nsaw: %s",
			readyAt, echoAt, c.transcript())
	}

	got := c.awaitEvent(idAlpha, "ready")
	if got.Event == nil || !strings.Contains(got.Event.Text, "ready") {
		t.Fatalf("await returned %+v, want the ready event it had already read past", got)
	}
}

func (c *testClient) transcript() string {
	var b strings.Builder
	for _, f := range c.seen {
		fmt.Fprintf(&b, "\n  %s", f.Kind)
		if f.SessionID != "" {
			fmt.Fprintf(&b, " session=%s", f.SessionID)
		}
		if f.Event != nil {
			fmt.Fprintf(&b, " %s %q", f.Event.Kind, f.Event.Text)
		}
		if f.Text != "" {
			fmt.Fprintf(&b, " text=%q", f.Text)
		}
		if f.Status != nil {
			for _, s := range f.Status.Sessions {
				fmt.Fprintf(&b, " [%s %s quiet=%dms]", s.ID, s.State, s.QuietMS)
			}
		}
	}
	if b.Len() == 0 {
		return " nothing"
	}
	return b.String()
}

// Every socket this package *binds* comes from one of the four helpers below,
// and TestEverySocketPathThisSuiteBuildsFitsInSunPath measures what all of them
// produce against maxSocketPath. A fifth way to build one is how this suite
// starts failing on a machine with a longer $TMPDIR, for a reason no failure
// message mentions - so add a helper here instead, put it in that test's table,
// and the budget stays honest.
//
// "Binds" and not "builds": several tests name a socket under t.TempDir() that
// nothing ever listens on - logfile_test.go and ensure_test.go want a path that
// is merely absent, and daemon_test.go's refusal test deliberately builds one
// past the limit. None of those is bound, so none belongs in the budget, and a
// reader who goes looking for a fifth builder should find those and conclude
// this comment is right rather than that it is stale.

// tempRootPrefix and tempRootDigits name a temporary directory. Both are as
// short as they can usefully be: every byte spent here is a byte the deepest
// path below cannot spend, and the tightest of them is a stand-in $HOME with
// production's own /.wake/daemon.sock underneath it.
const (
	tempRootPrefix = "wk"
	tempRootDigits = 8
)

// tempRoot returns a temporary directory whose name is a fixed width.
//
// Fixed rather than os.MkdirTemp's, which appends a decimal uint32 and so
// varies from 1 to 10 digits. Under MkdirTemp the deepest path in this package
// differs by up to nine bytes between runs, which would leave the budget
// assertion passing or failing on a random draw - and a guard that reports on
// luck is worse than no guard, because it is believed.
func tempRoot(t *testing.T) string {
	t.Helper()

	for range 100 {
		name := fmt.Sprintf("%s%0*d", tempRootPrefix, tempRootDigits, rand.IntN(1e8))
		dir := filepath.Join(os.TempDir(), name)
		err := os.Mkdir(dir, 0o700)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			t.Fatalf("Mkdir %s: %v", dir, err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(dir); err != nil {
				t.Errorf("RemoveAll: %v (left behind: %s)", err, listDir(dir))
			}
		})
		return dir
	}
	t.Fatalf("no free name under %s after 100 tries", os.TempDir())
	return ""
}

// tempSocket returns a short path for a unix socket. It is short on purpose:
// sun_path is capped near 104 bytes on darwin, and t.TempDir() plus a long
// test name pushes a socket past it - which fails as a bind error and looks
// exactly like a daemon that never started.
func tempSocket(t *testing.T) string {
	t.Helper()
	return filepath.Join(tempRoot(t), "s")
}

// tempSocketUnderMissingDir returns a socket path whose parent does not exist,
// for the tests that need SocketPath to create it.
func tempSocketUnderMissingDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(tempRoot(t), "n", "chosen.sock")
}

// tempHome returns a stand-in $HOME short enough that the default socket
// derived under it can still be bound. A test must never touch the operator's
// own ~/.wake.
func tempHome(t *testing.T) string {
	t.Helper()
	return tempRoot(t)
}

// TestEverySocketPathThisSuiteBuildsFitsInSunPath is the check that keeps this
// package runnable on a machine whose $TMPDIR is longer than the author's.
//
// sun_path is a fixed 104-byte field in the kernel's address struct, so a
// socket past it fails at bind with EINVAL - reported as a bare "invalid
// argument" by whichever test happened to bind first, with nothing pointing at
// path length. Measured on darwin before this test existed: the package is
// clean at len($TMPDIR) == 68, loses one test at 69, two at 85 and thirty-nine
// at 86. The 17-byte window between the first failure and the obvious one is
// the whole problem: a CI guard calibrated on the obvious number is green
// across all of it.
//
// It is deliberately arithmetic on real paths rather than a formula. A formula
// is a second copy of what the helpers do, and the copy is what goes stale the
// day somebody nests one directory deeper.
//
// # Why one package's guard covers the tree
//
// Two other packages bind sockets and neither has a budget test:
// cmd/wake/main_test.go's tempSocket is $TMPDIR/waketNNNNNNNNNN/s and
// internal/rpc/conn_test.go's is $TMPDIR/wakerpcNNNNNNNNNN/s. Both use
// os.MkdirTemp, whose suffix is 1 to 10 digits, so their worst cases are +18 and
// +20 past $TMPDIR - against the +29 measured here, which is fixed rather than
// drawn. This package is therefore the binding constraint at every draw, and a
// $TMPDIR short enough for it is short enough for them by 9 bytes or more.
//
// That ordering is load-bearing and nothing else states it. If a socket in
// either of those packages ever grows past +29 - a longer prefix, a nested
// directory, a longer filename - it fails on machines where this test still
// passes, and it needs its own budget test rather than this one's coverage.
func TestEverySocketPathThisSuiteBuildsFitsInSunPath(t *testing.T) {
	built := []struct {
		what string
		path string
		err  error
	}{
		{what: "tempSocket", path: tempSocket(t)},
		{what: "tempSocketUnderMissingDir", path: tempSocketUnderMissingDir(t)},
	}

	// The third is production's own derivation rather than a copy of it, so
	// that changing what lives under $HOME changes this measurement too.
	home := tempHome(t)
	t.Setenv("HOME", home)
	t.Setenv(SocketEnv, "")
	fromHome, err := SocketPath()
	built = append(built, struct {
		what string
		path string
		err  error
	}{"SocketPath under tempHome", fromHome, err})

	// Cleaned because os.TempDir returns $TMPDIR verbatim, trailing slash and
	// all, while filepath.Join has already collapsed it in every path above.
	root := filepath.Clean(os.TempDir())
	// What keeps this test from silently measuring nothing is the prefix check
	// below, which is reachable and has fired. A `len(built) == 0` guard was
	// here too and could not: built is a two-element literal plus an
	// unconditional append, so the only way to empty it is to delete code the
	// compiler would then complain about.
	for _, c := range built {
		// SocketPath refuses a path it can already tell will not bind, so an
		// over-long $TMPDIR arrives as an error rather than as a long string.
		// That is this test's verdict too - report it as one and keep going,
		// rather than aborting before the other paths are measured.
		if c.err != nil {
			t.Errorf("%s could not be built under a %d-byte $TMPDIR: %v", c.what, len(root), c.err)
			continue
		}
		// This test is about paths whose length $TMPDIR decides. One that is
		// not under it is not covered here and must not be counted as if it
		// were - the failure this whole test exists to prevent is a check that
		// looks at the wrong thing and reports green.
		if !strings.HasPrefix(c.path, root+string(filepath.Separator)) {
			t.Errorf("%s built %q, which is not under os.TempDir() (%q).\n"+
				"This test only bounds paths $TMPDIR decides the length of; a helper that "+
				"moved off it needs its own check rather than this one's silence.", c.what, c.path, root)
			continue
		}
		if len(c.path) > maxSocketPath {
			t.Errorf("%s built a %d-byte path and maxSocketPath is %d:\n  %s\n"+
				"$TMPDIR is %d bytes, which is %d too many for this suite. Nothing here can be "+
				"bound, and every daemon test will fail as `bind: invalid argument` - which reads "+
				"as a daemon that never started. Point $TMPDIR at a shorter root.",
				c.what, len(c.path), maxSocketPath, c.path, len(root), len(c.path)-maxSocketPath)
		}
	}
}

// TestMaxSocketPathIsWhereTheKernelActuallyStops binds at the constant and one
// byte past it.
//
// maxSocketPath is load-bearing twice over - checkSocketPath refuses on it, and
// the budget test above certifies paths against it - and until this existed,
// nothing anywhere proved the number. Someone raising it to make a failure go
// away would un-calibrate both at once and silently: the refusal would start
// admitting paths the kernel rejects, and the budget test would start passing
// paths that cannot be bound. The comment on the constant explains why it is
// 103 rather than 104 or 107; this is the part a comment cannot do.
func TestMaxSocketPathIsWhereTheKernelActuallyStops(t *testing.T) {
	root := tempRoot(t)
	// One separator and at least one byte of name, at the longer of the two
	// lengths below.
	if len(root)+2 > maxSocketPath+1 {
		t.Fatalf("$TMPDIR leaves no room to build a %d-byte path under %s (%d bytes).\n"+
			"TestEverySocketPathThisSuiteBuildsFitsInSunPath has the same cause and the better message.",
			maxSocketPath+1, root, len(root))
	}

	for _, tc := range []struct {
		n        int
		bindable bool
	}{
		{n: maxSocketPath, bindable: true},
		{n: maxSocketPath + 1, bindable: false},
	} {
		path := filepath.Join(root, strings.Repeat("s", tc.n-len(root)-1))
		if len(path) != tc.n {
			t.Fatalf("built a %d-byte path while trying to build a %d-byte one: %s", len(path), tc.n, path)
		}

		ln, err := net.Listen("unix", path)
		if err == nil {
			if cerr := ln.Close(); cerr != nil {
				t.Errorf("close: %v", cerr)
			}
		}

		switch {
		case tc.bindable && err != nil:
			t.Errorf("binding a %d-byte path failed: %v\n"+
				"maxSocketPath is %d, so this length is supposed to be usable. If the kernel "+
				"disagrees, maxSocketPath is too high and checkSocketPath is admitting paths "+
				"that cannot be bound.", tc.n, err, maxSocketPath)
		case !tc.bindable && err == nil:
			t.Errorf("binding a %d-byte path succeeded, one past maxSocketPath (%d).\n"+
				"The constant is lower than it needs to be. That is safe, but it is no longer "+
				"the kernel's limit, and the budget test above is rejecting paths that work.",
				tc.n, maxSocketPath)
		case !tc.bindable && !errors.Is(err, syscall.EINVAL):
			t.Errorf("binding a %d-byte path failed with %v, not EINVAL.\n"+
				"Over-long sun_path is supposed to fail at bind with EINVAL; a different errno "+
				"means this test is measuring something other than the length limit.", tc.n, err)
		}
	}
}

// shortQuitGrace runs one test at a grace other than the testQuitGrace the
// rest of this binary uses.
//
// It refuses a value that would put waitForExit back out of budget rather than
// letting the test run and report "Serve did not return" fifteen seconds
// later, which is the misdiagnosis the compression exists to prevent and the
// thing a grace raised to make some other failure go away would reintroduce.
// This is the only seam a grace can enter through, so the refusal covers every
// value the guard's own table does not name.
func shortQuitGrace(t *testing.T, d time.Duration) {
	t.Helper()

	if slack := shutdownSlack(d); slack < shutdownBudgetMargin {
		t.Fatalf("a quitGrace of %v leaves waitForExit %v of slack against a testTimeout of %v "+
			"(the shutdown is that grace plus shutdownWait %v), and this suite needs %v.\n"+
			"A daemon holding an agent stop cannot end would return after the bound, and the test "+
			"would fail a second time with \"Serve did not return\" - which names a daemon bug "+
			"that is not there and buries the real one. See TestTheShutdownBudgetKeepsItsMargin.",
			d, slack, testTimeout, shutdownWait, shutdownBudgetMargin)
	}

	prev := quitGrace
	quitGrace = d
	t.Cleanup(func() { quitGrace = prev })
}

// shortSilence compresses the liveness limit for one test. Five minutes is
// the right production value and an impossible test.
func shortSilence(t *testing.T, d time.Duration) {
	t.Helper()
	prev := silenceLimit
	silenceLimit = d
	t.Cleanup(func() { silenceLimit = prev })
}

// smallCap lowers the live cap so a test can reach it without starting thirty
// processes. Set before the daemon runs, for shortSilence's reason.
func smallCap(t *testing.T, n int) {
	t.Helper()
	prev := liveCap
	liveCap = n
	t.Cleanup(func() { liveCap = prev })
}

// listDir names what is still in a directory, and how recently, for the
// cleanup that could not remove it. The age is the useful half: a file
// written moments ago means something is still running that should not be.
func listDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err.Error()
	}
	var names []string
	for _, e := range entries {
		info, _ := e.Info()
		age := time.Duration(0)
		if info != nil {
			age = time.Since(info.ModTime())
		}
		names = append(names, fmt.Sprintf("%s written %v ago", e.Name(), age.Round(time.Millisecond)))
	}
	return strings.Join(names, ", ")
}

// sessionRow is the row a report gives one session, or the zero row if it names
// none. A zero row has an empty State, which no report ever carries, so "the
// report does not name it" is distinguishable from every state it could be in.
//
// Here rather than beside the wake tests it was written for, because the park
// book's round trip needs it and that test is not unix-only: what it asserts is
// a file, and a file has no process in it.
// Both lists, because a report carries a session in one or the other and the
// question here is "what does this report say about this id". A parked record
// left by a previous daemon is on Status.Parked and is deliberately not in the
// fleet; scanning only Sessions would answer "nothing at all" for every one of
// them, which reads as a session that was lost rather than one that is waiting.
// They are disjoint by construction - the daemon takes a record out of the book
// as it launches - so there is nothing to prefer between them.
func sessionRow(st rpc.Status, id string) rpc.SessionStatus {
	for _, s := range append(append([]rpc.SessionStatus(nil), st.Sessions...), st.Parked...) {
		if s.ID == id {
			return s
		}
	}
	return rpc.SessionStatus{}
}

// stateOf is sessionRow's State, for the assertions that want only that.
func stateOf(st rpc.Status, id string) string { return sessionRow(st, id).State }

// live is the sessions a status report says are still running. A report also
// carries recent endings - that is what makes an ending recoverable by a
// client that missed the announcement - so "how many are running" is a filter
// rather than a length.
func live(st rpc.Status) []rpc.SessionStatus {
	var out []rpc.SessionStatus
	for _, s := range st.Sessions {
		if s.State != rpc.StateEnded {
			out = append(out, s)
		}
	}
	return out
}

// parkedAgentRow is a row with no process behind it.
//
// It was daemon.parkedAgent until the park book stopped being restored into the
// fleet. Nothing in production builds one any more, and these tests and benchmarks still
// need a daemon holding N agents without N claude processes behind them. It
// lives here rather than in park.go because a production helper whose only
// callers are benchmarks is the dead code this project deletes.
//
// The core.Session is real and unstarted, which is what lets every caller stay
// as it is rather than growing a nil check: Pgid() reports 0, Stop() finds no
// stdin, and no goroutine exists because the pump is started by Start.
func parkedAgentRow(rec parkedRecord, name string) *agent {
	a := newAgent(rec.ID, name, rec.Label, rec.Dir, "", core.NewSession(core.Config{SessionID: rec.ID}), func() {})
	a.finish(nil)
	a.markParked()
	a.markWakeable(rec, true)
	return a
}

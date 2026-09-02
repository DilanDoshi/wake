package main

import (
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// helloOutcome runs waitForHello on a connection and reports what it returned,
// off the test goroutine so the test can observe that it has *not* returned.
type helloOutcome struct {
	attached bool
	err      error
}

func waitInBackground(conn net.Conn, out io.Writer) <-chan helloOutcome {
	done := make(chan helloOutcome, 1)
	frames, errs := rpc.ReadFrames(conn)
	go func() {
		attached, err := waitForHello(frames, errs, out)
		// Reported before the drain, not after: draining runs until the
		// connection closes, and a helper that answered only then would make
		// every one of these tests wait for its own cleanup.
		done <- helloOutcome{attached: attached, err: err}
		for range frames {
		}
	}()
	return done
}

func TestAHelloMeansAttached(t *testing.T) {
	d := startFakeDaemon(t, 0, rpc.Status{Running: true})

	conn, err := daemon.Dial(d.socket)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	select {
	case got := <-waitInBackground(conn, io.Discard):
		if got.err != nil {
			t.Fatalf("waitForHello: %v", got.err)
		}
		if !got.attached {
			t.Error("a daemon that sent a hello was not treated as attached")
		}
	case <-time.After(testTimeout):
		t.Fatal("waitForHello never returned against a daemon that says hello")
	}
}

// The sharpest thing in this wiring. A daemon in graceful shutdown keeps its
// listener bound and stops accepting; the kernel completes connections into
// the backlog throughout, so the dial succeeds against a daemon that will
// never read a byte. The handshake is the only discriminator, and the EOF when
// that listener finally closes is the exact edge at which starting a fresh
// daemon is safe.
//
// Both halves are asserted, and the first is the one that matters: an
// implementation that gave up on a timer and reported "attached" would spawn
// into the void, and one that gave up and reported "gone" would fork a second
// daemon whose reaper SIGKILLs the fleet the first is still stopping.
func TestASilentListenerIsWaitedOutRatherThanTimedOut(t *testing.T) {
	socket := tempSocket(t)
	ln := listenSilently(t, socket)

	conn, err := daemon.Dial(socket)
	if err != nil {
		t.Fatalf("Dial against a bound-but-never-accepting listener: %v", err)
	}
	defer func() { _ = conn.Close() }()

	var out syncBuffer
	done := waitInBackground(conn, &out)

	// Long enough to outlast any plausible timeout somebody might reach for,
	// and long enough for the patience message to have been printed.
	select {
	case got := <-done:
		t.Fatalf("waitForHello returned %+v against a listener that has said nothing; it must wait for the EOF", got)
	case <-time.After(2 * time.Second):
	}
	if !strings.Contains(out.String(), waitingLine) {
		t.Errorf("nothing explained the wait; output was %q", out.String())
	}

	// Closing the listener is what an outgoing daemon does last, and it is
	// what delivers EOF into the backlog.
	_ = ln.Close()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("waitForHello: %v", got.err)
		}
		if got.attached {
			t.Error("a connection that never saw a hello was reported as attached")
		}
	case <-time.After(testTimeout):
		t.Fatal("waitForHello did not return after the listener closed")
	}
}

// A real daemon, reached the way `wake` reaches it.
func TestConnectAttachesToARealDaemon(t *testing.T) {
	d := startRealDaemon(t)

	conn, stream, err := connect(d.socket, io.Discard)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		for range stream.Frames {
		}
		<-stream.Errs
	})

	if stream.Frames == nil || stream.Errs == nil {
		t.Fatal("connect returned no stream, so the model would read nothing")
	}
	// The reader connect handed back is the one the handshake was read on. A
	// second reader over the same connection would lose whatever the first
	// buffered, which is why this is not opened by the model.
	if err := rpc.WriteFrameTo(conn, rpc.Frame{Kind: rpc.FrameStatus}); err != nil {
		t.Fatalf("write status: %v", err)
	}
	select {
	case f, ok := <-stream.Frames:
		if !ok {
			t.Fatal("the stream closed instead of answering")
		}
		if f.Kind != rpc.FrameStatusReply {
			t.Errorf("first frame after the handshake = %q, want a status reply", f.Kind)
		}
	case <-time.After(testTimeout):
		t.Fatal("nothing arrived on the stream connect returned")
	}
}

func TestDetachLineCountsWhatIsStillRunning(t *testing.T) {
	tests := []struct {
		name string
		st   rpc.Status
		err  error
		want string
	}{
		{
			name: "a running fleet",
			st: rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
				{ID: "a", State: rpc.StateWorking},
				{ID: "b", State: rpc.StateIdle},
				{ID: "c", State: rpc.StateBlocked},
			}},
			want: "Detached. 3 agents still running.",
		},
		{
			name: "endings do not count as running",
			st: rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
				{ID: "a", State: rpc.StateWorking},
				{ID: "b", State: rpc.StateEnded},
			}},
			want: "Detached. 1 agent still running.",
		},
		{
			name: "nothing running",
			st:   rpc.Status{Running: true},
			want: "Detached. No agents are running.",
		},
		{
			name: "a fleet whose daemon died",
			st: rpc.Status{Sessions: []rpc.SessionStatus{
				{ID: "a", State: rpc.StateOrphaned},
				{ID: "b", State: rpc.StateOrphaned},
			}},
			want: "left 2 agents running with nothing holding them",
		},
		{
			name: "no daemon at all",
			st:   rpc.Status{},
			want: "Detached. The daemon is no longer running.",
		},
		{
			name: "the count could not be taken",
			err:  errTest,
			want: "could not count what is still running",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := detachLine(tc.st, tc.err); !strings.Contains(got, tc.want) {
				t.Errorf("detachLine = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

// The line has to carry the two commands that reach a fleet with no window
// open, or the count is a fact with nothing to do about it.
func TestTheDetachLineSaysHowToReachTheFleet(t *testing.T) {
	got := detachLine(rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "a", State: rpc.StateIdle}}}, nil)
	if !strings.Contains(got, "wake status") || !strings.Contains(got, "wake stop") {
		t.Errorf("detachLine = %q, want it to name wake status and wake stop", got)
	}
}

// ⌃Q's exit line comes off the model, and it does not ask the daemon.
//
// The daemon it would ask is the one ⌃Q just told to shut down: a quitting
// daemon keeps its listener bound until every session has ended, so the kernel
// completes the dial into a backlog nobody is accepting from and daemon.Status
// spends its whole timeout to come back with an i/o error. The line an operator
// would then read, after the one key whose entire point was counting, is
// "Detached, but could not count what is still running".
//
// The socket here answers with a fleet of two, which is what makes the
// assertion a discriminator rather than a coincidence: falling through to
// detachLine would produce a *successful* count and the word "Detached", so a
// fall-through fails on the wording and on the verb rather than on neither.
//
// The key is delivered through Update because that is the only door this
// package has - App.key is unexported, and cmd/wake driving the model the way
// Bubble Tea does is the right shape anyway.
//
// # What this used to assert, and why it is narrower now
//
// It required the line to read *"Parking 2 agents"* off a model built with a
// **nil connection** - a ⌃Q whose frame provably went nowhere - because the
// sentence was unconditional. That is outstanding bug 3 with a guard holding it
// in place: the promise was a function of the keypress, so writing the fix meant
// failing here first.
//
// The property it was written for is untouched and is what is kept: the line is
// ⌃Q's, the number is the *model's own* count, and nothing dials the daemon that
// is already shutting down. What is dropped is the clause that read a promise
// out of a model that had nothing to promise on; that is now
// TestTheExitLineOnlyPromisesAParkedFleetWhenTheDaemonConfirmedIt, per outcome.
func TestCtrlQsExitLineIsCountedByTheModelRatherThanAskedOfTheDaemon(t *testing.T) {
	d := startFakeDaemon(t, 0, twoLiveAgents())

	// Two ⌃Q, because the first only arms now - a single accidental or
	// auto-repeated press must not park the fleet. The second confirms. See
	// internal/ui/park.go's armParkFleet.
	armed, _ := ui.NewRoomApp(nil, ui.Stream{}, statusOf(twoLiveAgents())).
		Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	m, _ := armed.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})

	got := leavingLine(m, d.socket)
	if !strings.Contains(got, "2 agents") {
		t.Errorf("after ⌃Q, `wake` printed %q. The fleet was parked, not detached, and the count is the "+
			"model's own - taken before the frame went out, because after the program ends there is no "+
			"model anybody reads", got)
	}
	// The hedge, and it is not a style choice. shutdown kills whatever has not
	// ended by the end of the quit grace, kill clears the park flag, and a
	// killed session is dropped from the book - so a long build outlasting the
	// grace means this count is larger than what comes back. This process is
	// leaving and cannot learn which, so it may not print the outcome as done.
	if strings.Contains(got, "offers them back") || strings.Contains(got, "Parked 2") {
		t.Errorf("after ⌃Q, `wake` printed %q, which states an outcome this client cannot know. Three "+
			"agents mid-build are SIGKILLed at the end of the grace and are absent from the park book; "+
			"the operator finds out by coming back and counting", got)
	}
	if strings.Contains(got, "could not count") || strings.Contains(got, "Detached") {
		t.Errorf("after ⌃Q, `wake` printed %q - which is the line that asks a daemon. That daemon is the "+
			"one ⌃Q just told to shut down, so the dial lands in its listen backlog and burns the whole "+
			"status timeout for an i/o error", got)
	}
	if !strings.Contains(got, "`wake`") {
		t.Errorf("after ⌃Q, `wake` printed %q and does not say how to get the fleet back. `wake` reopens "+
			"the room and offers them back, and that sentence is the whole reason ⌃Q parks rather than "+
			"abandons", got)
	}
}

// The promise is made only where it was earned.
//
// **This is the surface outstanding bug 3 is actually read on.** The notice row
// lives inside an alt screen that is torn down a frame after ⌃Q settles, so a
// refusal drawn there is a refusal nobody sees; this line is printed to the
// terminal the operator is left looking at. Park exists so that stopping an
// agent is recoverable, and the one thing that must never happen is being told a
// conversation was parked when it was not.
//
// The failure sentence is checked for what it does **not** say as well as what
// it does. Only one of the four ways an ask goes unconfirmed knows the fleet is
// untouched - a write that failed may have failed after a partial write, and a
// daemon that went quiet may have taken the verb and died parking - so this line
// reports what is not known and names the verbs that answer it, which is `wake
// stop`'s own rule about never claiming more than you can see.
func TestTheExitLineOnlyPromisesAParkedFleetWhenTheDaemonConfirmedIt(t *testing.T) {
	confirmed := parkAllLine(2, nil)
	if !strings.Contains(confirmed, "Parking 2 agents") {
		t.Errorf("a ⌃Q the daemon confirmed printed %q, and the promise is what the key is for", confirmed)
	}

	unconfirmed := parkAllLine(2, errors.New("the daemon did not answer within 3s"))
	if strings.Contains(unconfirmed, "Parking 2 agents") {
		t.Errorf("a ⌃Q nothing confirmed printed %q - the same sentence a confirmed one prints. That is "+
			"the whole of outstanding bug 3: an operator is told a fleet is parked, closes the window, "+
			"and the conversations are still running under a daemon that never heard the verb", unconfirmed)
	}
	if !strings.Contains(unconfirmed, "did not answer within 3s") {
		t.Errorf("a ⌃Q nothing confirmed printed %q and does not say why. A failure with no reason is a "+
			"failure nobody can act on, which is the silent drop this replaced", unconfirmed)
	}
	for _, verb := range []string{"wake status", "`wake`"} {
		if !strings.Contains(unconfirmed, verb) {
			t.Errorf("the unconfirmed line %q does not name %s. It is the last thing on the terminal and "+
				"the operator's next move has to be in it", unconfirmed, verb)
		}
	}
	for _, claim := range []string{"was not parked", "were not parked", "Nothing was parked", "Parked"} {
		if strings.Contains(unconfirmed, claim) {
			t.Errorf("the unconfirmed line %q asserts %q. It does not know that in either direction: a "+
				"write can fail after a partial write, and a daemon can take the verb and then die "+
				"parking. `wake status` is what answers it, which is why the line names that instead",
				unconfirmed, claim)
		}
	}
}

// And every other way out still asks, which is the arm ⌃Q must not have taken
// with it.
func TestLeavingWithoutCtrlQStillCountsWhatIsRunning(t *testing.T) {
	d := startFakeDaemon(t, 0, twoLiveAgents())

	got := leavingLine(ui.NewRoomApp(nil, ui.Stream{}, nil), d.socket)
	if !strings.Contains(got, "Detached") {
		t.Errorf("leaving without ⌃Q printed %q, want the detach line: ⌃O, a hang-up and a crashed "+
			"daemon all leave the fleet running, and a background process nobody can see is a "+
			"liability - 20 sessions is about 16GB with no window open", got)
	}
	if !strings.Contains(got, "2 agents") {
		t.Errorf("leaving without ⌃Q printed %q, which does not count what is still running", got)
	}
}

// twoLiveAgents is a fleet with something in it to park, so a count of two is
// the model reading the fleet rather than any constant that happens to be right.
func twoLiveAgents() rpc.Status {
	return rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "alex", State: rpc.StateIdle},
		{ID: idBeta, Name: "sydney", State: rpc.StateWorking},
	}}
}

func statusOf(st rpc.Status) *rpc.Status { return &st }

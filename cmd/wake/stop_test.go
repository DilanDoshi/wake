package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// quitDelay is how long the fake daemon takes to stop its fleet. Long enough
// that returning early is unambiguous, short enough to run in a suite.
const quitDelay = 300 * time.Millisecond

// The property the whole command turns on. FrameQuit starts a graceful
// shutdown and does not signal anything, so a `wake stop` that wrote the frame
// and exited would hand back the shell prompt while 20 agents were still
// mid-Edit - and the daemon holds its listener bound throughout, so the next
// `wake` would wait on a daemon that is on its way out with no explanation.
func TestStopDoesNotReturnUntilTheDaemonHasLetGo(t *testing.T) {
	d := startFakeDaemon(t, quitDelay, rpc.Status{
		Running:  true,
		Sessions: []rpc.SessionStatus{{ID: "a", State: rpc.StateWorking}, {ID: "b", State: rpc.StateIdle}},
	})

	var out bytes.Buffer
	if err := stopFleet(d.socket, &out); err != nil {
		t.Fatalf("stopFleet: %v", err)
	}

	if !d.hasClosed() {
		t.Error("stopFleet returned while the daemon was still stopping its fleet")
	}
	if !strings.Contains(out.String(), "fleet is down") {
		t.Errorf("stopFleet said nothing about the outcome: %q", out.String())
	}
}

// "says what it is waiting for" is half the requirement: a prompt that hangs
// for thirty seconds with no explanation is indistinguishable from a hang.
func TestStopSaysWhatItIsWaitingFor(t *testing.T) {
	d := startFakeDaemon(t, quitDelay, rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "a", State: rpc.StateWorking},
			{ID: "b", State: rpc.StateIdle},
			{ID: "c", State: rpc.StateEnded},
		},
	})

	var out bytes.Buffer
	if err := stopFleet(d.socket, &out); err != nil {
		t.Fatalf("stopFleet: %v", err)
	}

	// Two running and one ended: the count is what is still alive, not the
	// length of the report.
	if !strings.Contains(out.String(), "Stopping 2 agents") {
		t.Errorf("stopFleet did not say what it was stopping: %q", out.String())
	}
	if !strings.Contains(out.String(), "in-flight turns") {
		t.Errorf("stopFleet did not say why it was waiting: %q", out.String())
	}
}

// The other end of the same claim: against the real daemon, the moment this
// returns is a moment at which Serve has finished the work `wake stop` names.
func TestStopEndsARealDaemon(t *testing.T) {
	d := startRealDaemon(t)

	var out bytes.Buffer
	if err := stopFleet(d.socket, &out); err != nil {
		t.Fatalf("stopFleet: %v", err)
	}
	if !strings.Contains(out.String(), "fleet is down") {
		t.Errorf("stopFleet said nothing about the outcome: %q", out.String())
	}

	// Serve has one bounded step left after it closes its clients - retiring
	// its own goroutines - so this is a short wait rather than an assertion
	// that it has already returned.
	if !d.exited(t, testTimeout) {
		t.Error("the daemon was still serving after stopFleet said the fleet was down")
	}
}

// A dial that lands in the backlog of a daemon which is already shutting down
// never sees a hello. Waiting for the EOF is the same evidence `wake stop`
// sets out to gather, so it is reported as success rather than as an error.
func TestStopAgainstADaemonThatIsAlreadyStoppingWaitsForIt(t *testing.T) {
	socket := tempSocket(t)
	ln := listenSilently(t, socket)

	done := make(chan error, 1)
	var out bytes.Buffer
	go func() { done <- stopFleet(socket, &out) }()

	select {
	case err := <-done:
		t.Fatalf("stopFleet returned (%v) while the outgoing daemon still held its listener", err)
	case <-time.After(time.Second):
	}

	_ = ln.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stopFleet: %v", err)
		}
		if !strings.Contains(out.String(), "fleet is down") {
			t.Errorf("stopFleet did not report the outcome: %q", out.String())
		}
	case <-time.After(testTimeout):
		t.Fatal("stopFleet never returned after the outgoing daemon closed its listener")
	}
}

func TestStopWithNoDaemonSaysSo(t *testing.T) {
	var out bytes.Buffer
	if err := stopFleet(tempSocket(t), &out); err != nil {
		t.Fatalf("stopFleet: %v", err)
	}
	if !strings.Contains(out.String(), "No daemon is running") {
		t.Errorf("stopFleet said %q", out.String())
	}
}

func TestStoppingLineCountsOnlyWhatIsAlive(t *testing.T) {
	tests := []struct {
		name string
		st   rpc.Status
		err  error
		want string
	}{
		{
			name: "one agent",
			st:   rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "a", State: rpc.StateWorking}}},
			want: "Stopping 1 agent.",
		},
		{
			name: "an empty fleet",
			st:   rpc.Status{Running: true},
			want: "Nothing is running",
		},
		{
			name: "no count available",
			err:  errTest,
			want: "Stopping the fleet",
		},
		// The four arms of stoppingLine's switch, and the last two are the ones
		// the park book added. A stop clears that book, so a fleet somebody
		// parked on purpose stops being offered back a moment after this line is
		// printed - which is a thing to be told before the wait rather than to
		// discover on the next `wake`.
		{
			name: "only parked",
			st:   rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "a", State: rpc.StateParked}}},
			want: "forgetting 1 agent parked",
		},
		{
			// Both halves true at once, which is the case two tests each about
			// one half never reach.
			name: "one running and two parked",
			st: rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
				{ID: "a", State: rpc.StateWorking},
				{ID: "b", State: rpc.StateParked},
				{ID: "c", State: rpc.StateParked},
			}},
			want: "Stopping 1 agent and forgetting 2 agents parked",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := stoppingLine(tc.st, tc.err); !strings.Contains(got, tc.want) {
				t.Errorf("stoppingLine = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

// The one thing `wake stop` must never get wrong. A daemon that takes the
// quit frame and then does nothing has not stopped anything, and saying "the
// fleet is down" there sends somebody away from 20 agents that are still
// running.
func TestStopSaysItCouldNotConfirmRatherThanClaimingTheFleetIsDown(t *testing.T) {
	shortCeiling(t, 300*time.Millisecond)
	d := startFakeDaemon(t, neverStops, rpc.Status{
		Running:  true,
		Sessions: []rpc.SessionStatus{{ID: "a", State: rpc.StateWorking}},
	})

	var out bytes.Buffer
	err := stopFleet(d.socket, &out)

	if err == nil {
		t.Fatalf("stopFleet reported success against a daemon that never let go: %q", out.String())
	}
	if !strings.Contains(err.Error(), "may still be running") {
		t.Errorf("stopFleet failed with %v, want it to say the fleet may still be running", err)
	}
	if strings.Contains(out.String(), "fleet is down") {
		t.Errorf("stopFleet claimed the fleet was down anyway: %q", out.String())
	}
}

// The decision the connection ending cannot make on its own.
func TestStoppedOutcomeOnlyConfirmsWhatItCanSee(t *testing.T) {
	tests := []struct {
		name     string
		st       rpc.Status
		err      error
		released bool
		wantOut  string
		wantErr  string
	}{
		{
			name:     "no daemon and nothing alive",
			st:       rpc.Status{},
			released: true,
			wantOut:  "fleet is down",
		},
		{
			name: "endings on the roster are not survivors",
			st: rpc.Status{Sessions: []rpc.SessionStatus{
				{ID: "a", State: rpc.StateEnded},
			}},
			released: true,
			wantOut:  "fleet is down",
		},
		{
			name: "a fleet that outlived its daemon",
			st: rpc.Status{Sessions: []rpc.SessionStatus{
				{ID: "a", State: rpc.StateOrphaned},
				{ID: "b", State: rpc.StateOrphaned},
			}},
			released: true,
			wantErr:  "2 agents still running",
		},
		{
			// The socket surviving is what separates a killed daemon from one
			// that panicked, and it is the difference between "go and look at
			// the log" and "there is no log".
			name: "a fleet whose daemon was killed rather than unwound",
			st: rpc.Status{Sessions: []rpc.SessionStatus{
				{ID: "a", State: rpc.StateOrphaned},
			}},
			released: false,
			wantErr:  "never unlinked its socket",
		},
		{
			name:     "a daemon still answering",
			st:       rpc.Status{Running: true},
			released: true,
			wantErr:  "still answering",
		},
		{
			name:     "no answer at all",
			err:      errTest,
			released: true,
			wantErr:  "could not be established",
		},
		{
			// A wedged ps dropped every live record, so the roster came back
			// empty and runningCount is zero - but ProbeIncomplete says the sweep
			// never finished. The would-be-default "fleet is down" here is the
			// false-down `wake stop && rm -rf` is written to survive, so a report
			// that could not verify every session errs instead of confirming.
			name:     "a sweep the liveness budget cut short",
			st:       rpc.Status{ProbeIncomplete: true},
			released: true,
			wantErr:  "could not confirm",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := stoppedOutcome(tc.st, tc.err, tc.released)

			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("stoppedOutcome returned %v, want success", err)
				}
				if !strings.Contains(out, tc.wantOut) {
					t.Errorf("stoppedOutcome printed %q, want it to contain %q", out, tc.wantOut)
				}
				return
			}
			if err == nil {
				t.Fatalf("stoppedOutcome succeeded with %q, want an error", out)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("stoppedOutcome failed with %q, want it to contain %q", err, tc.wantErr)
			}
			if strings.Contains(out, "fleet is down") {
				t.Errorf("stoppedOutcome claimed the fleet was down while failing: %q", out)
			}
			if tc.released && strings.Contains(err.Error(), "never unlinked") {
				t.Errorf("stoppedOutcome blamed a socket that was unlinked: %q", err)
			}
		})
	}
}

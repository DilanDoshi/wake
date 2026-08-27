package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// Three answers, not two. A daemon that answered, a machine with no daemon,
// and a machine whose daemon died leaving its agents behind - the last is not
// "nothing is running", it is 15-30 processes nobody is holding, and it is the
// case the on-disk roster exists for.
func TestStatusAnswersAllThreeCases(t *testing.T) {
	running := formatStatus(rpc.Status{
		Running: true, PID: 4242, Socket: "/tmp/wake.sock",
		Sessions: []rpc.SessionStatus{{ID: "a11a0000-0000-4000-8000-00000000a11a", Name: "alex", State: rpc.StateWorking}},
	})
	if !strings.Contains(running, "pid 4242") || !strings.Contains(running, "alex") {
		t.Errorf("a running daemon reads as %q", running)
	}

	none := formatStatus(rpc.Status{})
	if !strings.Contains(none, "No daemon is running") {
		t.Errorf("an empty machine reads as %q", none)
	}
	if strings.Contains(none, "left") {
		t.Errorf("an empty machine claimed something was left behind: %q", none)
	}

	orphaned := formatStatus(rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "a11a0000-0000-4000-8000-00000000a11a", Name: "alex", State: rpc.StateOrphaned, PID: 900},
		{ID: "b22b0000-0000-4000-8000-00000000b22b", Name: "sam", State: rpc.StateOrphaned, PID: 901},
	}})
	if !strings.Contains(orphaned, "left 2 agents behind") {
		t.Errorf("a fleet whose daemon died reads as %q", orphaned)
	}
	if orphaned == none {
		t.Error("a fleet whose daemon died is reported the same as no daemon at all")
	}
	for _, want := range []string{"alex", "sam", rpc.StateOrphaned} {
		if !strings.Contains(orphaned, want) {
			t.Errorf("the orphan report is missing %q:\n%s", want, orphaned)
		}
	}
}

// A blocked session is stopped dead until somebody answers, and the request id
// is the only correlator an answer has. An ended one carries why.
func TestASessionLineCarriesWhatItsStateOwes(t *testing.T) {
	tests := []struct {
		name    string
		session rpc.SessionStatus
		want    []string
	}{
		{
			name:    "blocked",
			session: rpc.SessionStatus{ID: "abcdef1234", Name: "alex", State: rpc.StateBlocked, RequestIDs: []string{"req-77"}},
			want:    []string{"alex", rpc.StateBlocked, "req-77"},
		},
		{
			name:    "ended with a reason",
			session: rpc.SessionStatus{ID: "abcdef1234", Name: "alex", State: rpc.StateEnded, Error: "exit status 1"},
			want:    []string{rpc.StateEnded, "exit status 1"},
		},
		{
			name:    "silent for a while",
			session: rpc.SessionStatus{ID: "abcdef1234", Name: "alex", State: rpc.StateSilent, QuietMS: 90_000},
			want:    []string{rpc.StateSilent, "quiet 1m30s"},
		},
		{
			name:    "unnamed",
			session: rpc.SessionStatus{ID: "abcdef1234", State: rpc.StateIdle},
			want:    []string{unnamed, rpc.StateIdle},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// nil, because these four rows are about what a *state* owes an
			// explanation for and none of them is a fork. A nil map reads as
			// empty, and forkedFrom returns "" for a row with no ParentID
			// whatever it is handed.
			got := sessionLine(tc.session, nil)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("sessionLine = %q, want it to contain %q", got, want)
				}
			}
			if strings.Count(got, "\n") != 1 || !strings.HasSuffix(got, "\n") {
				t.Errorf("sessionLine = %q, want exactly one line", got)
			}
		})
	}
}

// A running daemon with nothing on it must not print an empty report that
// reads as broken.
func TestAnIdleDaemonSaysItHasNoSessions(t *testing.T) {
	got := formatStatus(rpc.Status{Running: true, PID: 1, Socket: "/tmp/s"})
	if !strings.Contains(got, "No sessions") {
		t.Errorf("formatStatus = %q", got)
	}
}

func TestStatusAgainstARealDaemon(t *testing.T) {
	d := startRealDaemon(t)

	var out bytes.Buffer
	if err := printStatus(d.socket, &out); err != nil {
		t.Fatalf("printStatus: %v", err)
	}
	if !strings.Contains(out.String(), "wake daemon running") {
		t.Errorf("printStatus = %q", out.String())
	}
	if !strings.Contains(out.String(), d.socket) {
		t.Errorf("printStatus did not name the socket it asked: %q", out.String())
	}
}

// --- dispatch -----------------------------------------------------------

func TestUnknownCommandsAreRefused(t *testing.T) {
	// So resolving the socket cannot create ~/.wake on whoever runs this.
	t.Setenv(daemon.SocketEnv, tempSocket(t))

	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"stat"}, want: "unknown command"},
		{args: []string{"--help"}, want: "unknown command"},
		// The verb is checked before the arity. The other way round this
		// reports that "bogus" takes no arguments, which quietly asserts that
		// bogus is a command.
		{args: []string{"bogus", "arg"}, want: "unknown command"},
		{args: []string{"stop", "everything"}, want: "takes no arguments"},
	}

	for _, tc := range tests {
		err := run(tc.args, io.Discard)
		if err == nil {
			t.Errorf("run(%q) was accepted; a typo must not spawn an agent", tc.args)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("run(%q) = %q, want it to say %q", tc.args, err, tc.want)
		}
		if !strings.Contains(err.Error(), "wake status") {
			t.Errorf("run(%q) = %v, want the usage with it", tc.args, err)
		}
	}
}

// Two of the four dispatch arms, named for the two rather than for "each".
//
// Nothing exercised the dispatch end to end: the stop tests call stopFleet
// directly, so a `case` lost from the second switch routed `wake stop`
// somewhere else with every test still green.
//
// The other two arms: `wake daemon` is pinned end to end by TestMain, which is
// what daemon.EnsureRunning's fork execs, so every test in detach_unix_test.go
// depends on that arm working. The no-argument attach arm is **not** pinned -
// it ends in tea.NewProgram taking over stdin, which a test has no terminal to
// give it. That is the gap, stated rather than implied by a name.
func TestStatusAndStopReachTheirOwnCommands(t *testing.T) {
	statusDaemon := startRealDaemon(t)
	t.Setenv(daemon.SocketEnv, statusDaemon.socket)

	var status bytes.Buffer
	if err := run([]string{cmdStatus}, &status); err != nil {
		t.Fatalf("wake status: %v", err)
	}
	if !strings.Contains(status.String(), "wake daemon running") {
		t.Errorf("wake status printed %q", status.String())
	}

	// A separate live daemon is the precondition for the stop dispatch arm.
	// Status disconnecting from an empty daemon is now itself an exit event,
	// so sharing one fixture makes this assertion depend on which goroutine wins.
	stopDaemon := startRealDaemon(t)
	t.Setenv(daemon.SocketEnv, stopDaemon.socket)
	var stop bytes.Buffer
	if err := run([]string{cmdStop}, &stop); err != nil {
		t.Fatalf("wake stop: %v", err)
	}
	if !strings.Contains(stop.String(), "fleet is down") {
		t.Errorf("wake stop printed %q", stop.String())
	}
	if !stopDaemon.exited(t, testTimeout) {
		t.Error("wake stop returned without the daemon having exited")
	}
}

// A parked session is neither running nor gone, and the listing says so.
//
// A **regression pin rather than a driver**: sessionLine already prints s.State,
// so this passes without a production change and was run before one was made.
// It is worth pinning because the state word is the whole of what the listing
// tells somebody about a parked session, and `ended` is the one thing park
// exists not to be - a row that read ended would say an hour of context is gone
// when it is on disk and one --resume away.
func TestStatusListsAParkedSessionAsParked(t *testing.T) {
	out := formatStatus(rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "alex", Label: "dev-5748", State: rpc.StateParked},
	}})
	if !strings.Contains(out, rpc.StateParked) {
		t.Errorf("the listing does not say a parked session is parked:\n%s", out)
	}
	if strings.Contains(out, rpc.StateEnded) {
		t.Errorf("the listing calls a parked session ended, which is the one thing park exists not to be:\n%s", out)
	}
}

// docs/notes/bugs.md BUG-9, on the surface outside the TUI. SessionStatus.Error
// is a stderr tail and it can be a *grandchild's* bytes - something the agent
// spawned, holding stderr past core's bound - so `wake status` is a place a
// model-controlled process gets its escape sequences printed to a terminal that
// executes them exactly as the alt screen would.
func TestAStatusRowCannotDriveTheTerminal(t *testing.T) {
	got := sessionLine(rpc.SessionStatus{
		ID:    idAlpha,
		Name:  "sydney",
		State: rpc.StateEnded,
		Error: "exit 1: before\x1b]52;c;cHduZWQ=\amiddle\x1b[2J\u009b2Jafter",
	}, nil)

	for _, r := range strings.TrimSuffix(got, "\n") {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Errorf("the status row kept %#x: %q", r, got)
		}
	}
	for _, want := range []string{"sydney", "before", "after"} {
		if !strings.Contains(got, want) {
			t.Errorf("containment ate %q out of the row: %q", want, got)
		}
	}
}

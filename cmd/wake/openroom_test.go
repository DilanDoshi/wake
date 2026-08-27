package main

// Bare `wake`: which of the two things it does, and the offer the room opens
// with.
//
// # Why none of these run the program
//
// Both halves of the branch end in a Bubble Tea program, which wants a terminal
// `go test` does not have and would write an alt screen over the test binary's
// own output. So each test here drives `run` against a daemon that **answers**
// - with a refusal, or with a hang-up - which turns the path into an error this
// package can assert on while leaving the decision under test exactly where it
// is. The assertions are on the frames the daemon received, which is where the
// difference between the two branches actually shows: one asks what is running,
// the other asks for a session.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// answersEveryStatus is statusesBeforeHangUp's "no budget", spelled so a caller
// is not passing an unexplained zero.
const answersEveryStatus = 0

// refusedSpawn is what the fake daemon says when a client asks for a session.
// Its content does not matter; that it *arrives* is what lets the first-run
// path return instead of waiting out a confirmation with no deadline.
const refusedSpawn = "not right now"

// startBareWakeDaemon is the fake both branch tests drive.
//
// It refuses every spawn and answers a bounded number of status requests, which
// is what makes each branch terminate: the first-run path gets its refusal, and
// the room path gets a hang-up where its own status reply would have been. Both
// then return an error rather than opening a terminal.
func startBareWakeDaemon(t *testing.T, status rpc.Status, statuses int) *fakeDaemon {
	t.Helper()
	return listenAs(t, &fakeDaemon{status: status, spawnRefusal: refusedSpawn, statusesBeforeHangUp: statuses})
}

// bareWake runs `wake` with no arguments and insists it came back.
//
// Off the test goroutine because the failure being watched for is a wait with
// no deadline - awaitSpawn's and awaitFleet's are both events rather than
// timers - and a package that hangs reports nothing at all.
func bareWake(t *testing.T) error {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- run(nil, io.Discard) }()
	select {
	case err := <-done:
		return err
	case <-time.After(testTimeout):
		t.Fatal("bare `wake` never returned against a daemon that answered it")
		return nil
	}
}

// noSpawnReached fails if any client in this test asked for a session.
func noSpawnReached(t *testing.T, d *fakeDaemon) {
	t.Helper()

	for _, f := range d.frames() {
		if f.Kind == rpc.FrameSpawn {
			t.Fatalf("bare `wake` against an existing fleet asked for a new session (%+v). The room "+
				"comes back whole when you attach to it, so this is a missing front door rather than "+
				"a missing feature - and spawning is what made `wake` unusable as one", f)
		}
	}
}

// Bare `wake` with a fleet opens the room on it and spawns nothing.
//
// The old behaviour always spawned, which made "get back into a detached fleet"
// mean naming a person - and every `wake` typed to look at the room a new agent
// nobody asked for.
func TestBareWakeWithAFleetOpensTheRoomAndSpawnsNothing(t *testing.T) {
	// One status answered: daemon.Status's, which is the question the branch is
	// taken on. The room's own request for the fleet then gets the hang-up, so
	// this returns instead of running a TUI.
	d := startBareWakeDaemon(t, rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "alex", Label: "dev-5748", State: rpc.StateIdle},
	}}, 1)
	t.Setenv(daemon.SocketEnv, d.socket)

	err := bareWake(t)

	noSpawnReached(t, d)
	// And it did take the room path rather than failing before it got there: the
	// daemon was asked what it is holding **twice** - once by daemon.Status,
	// which decides the branch, and once by the room on its own connection.
	if n := countKind(d.frames(), rpc.FrameStatus); n < 2 {
		t.Errorf("the daemon was asked what is running %d times, want at least 2: bare `wake` decided "+
			"the branch and then never opened the room on the connection it holds", n)
	}
	if err == nil {
		t.Error("bare `wake` reported success against a daemon that hung up without saying what it is holding")
	}
}

// Bare `wake` with nothing running is first run, and first run spawns.
//
// The case a new user hits exactly once. Making them type two commands to get an
// agent is the version of this nobody forgives.
func TestBareWakeWithNoFleetIsFirstRunAndStillSpawns(t *testing.T) {
	d := startBareWakeDaemon(t, rpc.Status{Running: true}, answersEveryStatus)
	t.Setenv(daemon.SocketEnv, d.socket)

	err := bareWake(t)
	if err == nil || !strings.Contains(err.Error(), refusedSpawn) {
		t.Fatalf("bare `wake` against an empty fleet failed with %v, want the daemon's refusal: "+
			"it never asked for a session, so a new user's first command produced no agent", err)
	}

	f := d.lastSpawn()
	if f.SessionID == "" {
		t.Error("first-run `wake` asked for a session with no id. Wake originates identity - the " +
			"reaper's only proof of a process group is that UUID in an argv")
	}
	if f.Dir == "" {
		t.Error("first-run `wake` asked for a session with no directory, so the agent runs wherever " +
			"the daemon happens to be and claude persists the transcript there too")
	}
}

// A fleet parked by ⌃Q has no daemon, and bare `wake` must still find it.
//
// # Why this is the test the two above cannot be
//
// Both of those hand `hasFleet` an answer a fake daemon was told to give, which
// proves the branch and not the question. **"A fleet exists" is a premise**, and
// on the one path that matters it is answered by a *file*: ⌃Q parks the fleet
// and then exits, so there is no daemon to ask, and daemon.Status falls through
// to daemon.FleetOnDisk - which Task 4 taught to read the park book. Nothing here
// stubs that. The book is written the way the daemon writes it and read back
// through the production question.
//
// The mutant is "a fleet means something is running", and it is invisible to
// every other test here: this is the only configuration where the fleet exists
// and nothing is alive. Getting it wrong spawns a fresh agent beside twenty
// parked ones and offers nothing.
//
// The second half is why the first half is worth anything. A branch that opens
// the room on rows the next daemon will not hold is a branch that opens an empty
// room, so the daemon bare `wake` is about to start is started - through the
// same EnsureRunning the client path uses - and asked whether it holds them.
func TestBareWakeFindsAFleetThatIsParkedAndHasNoDaemon(t *testing.T) {
	socket := tempSocket(t)
	t.Setenv(daemon.SocketEnv, socket)
	writeParkBook(t, socket, idAlpha, "alex")

	// (i) With no daemon at all, the question bare `wake` branches on says yes.
	st, err := daemon.Status(socket)
	if err != nil {
		t.Fatalf("status with no daemon: %v", err)
	}
	if st.Running {
		t.Fatal("a daemon answered on a socket nothing was listening on, so this test never reached " +
			"the path it is about: the whole point is that ⌃Q's fleet has no daemon behind it")
	}
	if hasFleet(st) {
		t.Fatalf("a parked session with no daemon reads as a live fleet (%+v): counting one is what "+
			"used to hand back the whole roster after a ⌃Q", st)
	}
	if len(st.Parked) != 1 {
		t.Fatalf("a fleet of one parked session with no daemon reads as an empty park book (%+v). "+
			"Bare `wake` would take the first-run path and spawn a fresh agent beside it", st)
	}

	// (ii) And the daemon this branch is about to start holds it, so the room it
	// opens is a room with something in it.
	t.Cleanup(func() { _ = stopFleet(socket, io.Discard) })
	conn, stream, err := connect(socket, io.Discard)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		drain(stream)
	})

	restored, err := daemon.Status(socket)
	if err != nil {
		t.Fatalf("status from the daemon that was just started: %v", err)
	}
	if !restored.Running {
		t.Fatal("connect returned without a daemon behind it")
	}
	if len(restored.Sessions) != 0 {
		t.Errorf("the daemon bare `wake` starts is holding %+v, want nothing: the park book is "+
			"addressable, not restored", restored.Sessions)
	}
	if names := parkedNames(&restored); len(names) != 1 || names[0] != "alex" {
		t.Errorf("the daemon bare `wake` starts reports %v as parked, want [alex]: /resume resolves "+
			"a name against this list and nothing else", names)
	}
}

// The window ⌃Q's own exit line invites somebody to type into, and the one
// configuration where the two orders disagree.
//
// # The situation
//
// ⌃Q parks the fleet and the daemon then holds its listener for the whole quit
// grace, because unlinking earlier is what let a second daemon's reaper SIGKILL
// a fleet mid-Edit. The operator reads *"`wake` reopens the room and offers back
// whatever finished in time"* and types `wake` in another terminal. The kernel
// completes that dial into the listen backlog, so daemon.Status writes its
// question, waits out statusTimeout and returns an **error** with a zero report.
//
// # Why an error may not be read as "no fleet"
//
// Because the zero report has no sessions in it, so the branch takes the
// first-run path and spawns - a twenty-first agent beside twenty parked ones,
// with no offer, which is the exact failure hasFleet's own comment says this
// branch exists to prevent, arriving through a timeout rather than through the
// branch. The answer is the one Status already gives when it cannot reach a
// daemon: the disk. A daemon that accepted the dial and will not answer is
// unreachable in every sense that matters here, and shutdown writes the book
// **before** it closes its clients, so the book is complete throughout the
// window.
//
// # Why it also separates ⌃Q from `wake stop`
//
// The disk is what tells them apart. Both leave a daemon that will not answer;
// only one leaves a park book, because the quit verb clears it. So the same
// clause sends ⌃Q to the room and `wake stop` to first run, which is what each
// of them means.
//
// # Two mutants, one test
//
// Reading the error as no fleet fails the assertion. Asking **after** dialling -
// which this task shipped believing to be an equivalent mutation - fails the
// deadline instead: connect() has no deadline by design, so a dial-first version
// blocks in waitForHello until the outgoing daemon lets go, and cannot decide
// anything at all for as long as the shutdown lasts.
func TestBareWakeFindsTheParkedFleetWhileTheDaemonIsStillShuttingDown(t *testing.T) {
	socket := tempSocket(t)
	// The shape of a daemon in graceful shutdown, which is this helper's own
	// documented reason to exist: bound, accepting nothing, hanging up on
	// nobody.
	listenSilently(t, socket)
	writeParkBook(t, socket, idAlpha, "alex")

	// The premise, asserted before the behaviour and through the call the code
	// under test makes only indirectly. Without it a listener that had stopped
	// being bound would make daemon.Status fall through to the disk with no
	// error at all - the path the other tests already cover - and this one would
	// pass having observed nothing.
	start := time.Now()
	if _, err := daemon.Status(socket); err == nil {
		t.Fatal("daemon.Status answered on a socket nothing is accepting on, so this test never " +
			"reached the shutdown window it is about")
	}
	t.Logf("MEASURED: daemon.Status against a bound-but-never-accepting listener took %v", time.Since(start))

	// Off the test goroutine, because the second mutant does not answer wrongly
	// - it does not answer.
	decided := make(chan rpc.Status, 1)
	go func() { decided <- fleetToReopen(socket) }()

	select {
	case st := <-decided:
		if names := parkedNames(&st); len(names) != 1 || names[0] != "alex" {
			t.Errorf("bare `wake` inside ⌃Q's shutdown window read %+v, and names %v as parked, want "+
				"[alex]: it did not come from the park book, so `wake` takes the first-run path and "+
				"spawns a fresh agent beside the parked one - the motion ⌃Q's own exit line invites", st, names)
		}
	case <-time.After(testTimeout):
		t.Fatal("bare `wake` never decided which branch to take. connect() has no deadline by design " +
			"- its two outcomes are events - so a branch taken after dialling waits out the whole of " +
			"somebody else's shutdown before it can answer a question the disk already answers")
	}
}

// writeParkBook leaves behind what a daemon that parked its fleet on the way out
// left: parked.json beside the socket, which is the one file Wake reads back
// into live state.
//
// Written by hand rather than through internal/daemon, which does not export it
// - and the hand-written shape is checked rather than trusted: part (ii) of the
// test above starts a real daemon on this socket and requires it to report the
// row, so a field name that drifted makes the book unreadable and fails there
// rather than passing as "nothing was parked".
func writeParkBook(t *testing.T, socket, sessionID, name string) {
	t.Helper()

	dir := t.TempDir()
	body := fmt.Sprintf(`[{"id":%q,"name":%q,"label":"dev-5748","dir":%q,"parked":%q}]`,
		sessionID, name, dir, time.Now().Format(time.RFC3339Nano))
	path := filepath.Join(filepath.Dir(socket), "parked.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write park book: %v", err)
	}
	// A previous daemon only wrote a row for a session that had run, so its
	// transcript is on disk too - and parkedStatuses now drops a record with
	// none, a wake of which opens an empty conversation under a live id.
	plantTranscript(t, sessionID)
}

// awaitFleet takes the reply and never a push, which is what the two status
// kinds exist for.
//
// A push announcing some other client's state change can be sitting in the
// socket already - the daemon broadcasts one the moment anything changes - and
// reading it as this question's answer seeds the room from a report assembled
// before it was asked. The fixture puts one in front of the reply, which is the
// ordering the fake daemon controls and a real one produces.
//
// The frames it read past come back, for awaitSpawn's reason: they are the
// fleet's transcript, and dropping them loses the opening of whatever was being
// said - silently, and only sometimes.
func TestAwaitFleetTakesTheReplyAndNotAPushThatCameFirst(t *testing.T) {
	pushed := &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idBeta, Name: "marco", State: rpc.StateIdle},
	}}
	answered := &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "alex", State: rpc.StateParked},
	}}

	fleet, read, err := awaitFleet(streamOf(
		rpc.Frame{Kind: rpc.FrameStatusPush, Status: pushed},
		rpc.Frame{Kind: rpc.FrameEvent, SessionID: idBeta},
		rpc.Frame{Kind: rpc.FrameStatusReply, Status: answered},
	))
	if err != nil {
		t.Fatalf("awaitFleet: %v", err)
	}
	if fleet == nil || len(fleet.Sessions) != 1 || fleet.Sessions[0].ID != idAlpha {
		t.Fatalf("the room was seeded from %+v, want the reply naming %s: a push is somebody else's "+
			"state change, and it was assembled before this question was asked", fleet, idAlpha)
	}
	if len(read) != 2 {
		t.Errorf("awaitFleet handed back %d frames it read past, want 2: they are the fleet's "+
			"transcript, and losing them loses the opening of whatever was being said", len(read))
	}
}

// And a daemon that hangs up without answering is an error rather than a wait.
//
// The wait has no deadline by design - both outcomes are events - so the arm
// that turns a closed stream into a sentence is the only thing between this and
// `wake` sitting on a blank terminal forever.
func TestAwaitFleetReportsADaemonThatHungUpWithoutAnswering(t *testing.T) {
	if _, _, err := awaitFleet(streamOf()); err == nil {
		t.Fatal("awaitFleet reported success against a daemon that said nothing at all")
	}

	frames := make(chan rpc.Frame)
	errs := make(chan error, 1)
	errs <- errTest
	close(frames)
	close(errs)
	_, _, err := awaitFleet(ui.Stream{Frames: frames, Errs: errs})
	if !errors.Is(err, errTest) {
		t.Errorf("a read failure reached the caller as %v, want it to carry %v", err, errTest)
	}
}

// countKind is how many frames of one kind a daemon was sent.
func countKind(fs []rpc.Frame, kind string) int {
	n := 0
	for _, f := range fs {
		if f.Kind == kind {
			n++
		}
	}
	return n
}

// What bare `wake` does with each thing it can find, which is the whole of the
// branch a test can reach.
//
// The row that is the point of this table is the third: a park book and nothing
// running. It used to be indistinguishable from a live fleet, because the daemon
// restored every record into s.agents and hasFleet counted them - so ⌃Q then
// `wake` handed back the roster and the conversations somebody had just quit.
// Now the book is addressable and invisible, and the one thing that must not
// happen is falling through to first run and spawning beside it.
func TestBareWakeOpensARoomForAnythingItFindsAndSpawnsOnlyForNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		st   rpc.Status
		want bool
		why  string
	}{
		{
			name: "a live fleet",
			st:   rpc.Status{Sessions: []rpc.SessionStatus{{ID: idAlpha, Name: "alex", State: rpc.StateIdle}}},
			want: true,
			why:  "there is a fleet to reopen the room over",
		},
		{
			name: "a park book and nothing running",
			st:   rpc.Status{Parked: []rpc.SessionStatus{{ID: idAlpha, Name: "alex", State: rpc.StateParked}}},
			want: true,
			why: "⌃Q left these; spawning a fresh agent beside them is the failure this branch " +
				"exists to prevent, and the room over them is empty until /resume asks",
		},
		{
			name: "nothing at all",
			st:   rpc.Status{},
			want: false,
			why:  "first run, and a new user's first command has to produce an agent",
		},
		{
			name: "nothing but endings",
			st:   rpc.Status{Sessions: []rpc.SessionStatus{{ID: idAlpha, Name: "alex", State: rpc.StateEnded}}},
			want: false,
			why:  "a fleet of nothing but endings is a fleet of nothing",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reopensRoom(tc.st); got != tc.want {
				t.Errorf("bare `wake` over %+v opens a room = %v, want %v: %s", tc.st, got, tc.want, tc.why)
			}
		})
	}
}

// A parked session is not a live one, and the two lists may never be confused.
//
// hasFleet is what decides "is there something to reopen the room *over*", and
// a park book row answering yes is the whole bug: it put a roster row and an
// openable conversation in front of somebody whose last act was ⌃Q.
func TestAParkBookIsNotAFleet(t *testing.T) {
	st := rpc.Status{Parked: []rpc.SessionStatus{
		{ID: idAlpha, Name: "alex", State: rpc.StateParked},
	}}
	if hasFleet(st) {
		t.Error("a report carrying only park book records reads as a live fleet, so the room draws " +
			"rows for sessions nothing is holding")
	}
}

// parkedNames is who a report says is parked, by name.
//
// It reads Status.Parked, which is where a park book record lives now: a parked
// session is deliberately **not** in Sessions, so a projection over that list
// would be empty on every report and every assertion using it would pass by
// finding nothing.
func parkedNames(st *rpc.Status) []string {
	if st == nil {
		return nil
	}
	out := make([]string, 0, len(st.Parked))
	for _, s := range st.Parked {
		out = append(out, displayName(s))
	}
	return out
}

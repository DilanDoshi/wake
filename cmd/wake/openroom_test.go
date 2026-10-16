package main

// Bare `wake`: what it asks the daemon for on the way into the room.
//
// # Why none of these run the program
//
// Bare `wake` ends in a Bubble Tea program, which wants a terminal `go test`
// does not have and would write an alt screen over the test binary's own
// output. So each test here drives `run` against a daemon that hangs up on the
// room's own question, which turns the path into an error this package can
// assert on. The assertions are on the frames the daemon received.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// hangsUpOnEveryStatus is statusesBeforeHangUp's "answers none".
const hangsUpOnEveryStatus = -1

// refusedSpawn is what the fake daemon says when a client asks for a session.
// Its content does not matter; that it *arrives* is what lets a client that
// spawned return instead of waiting out a confirmation with no deadline.
const refusedSpawn = "not right now"

// startBareWakeDaemon is the fake bare `wake` is driven against: it refuses
// every spawn and hangs up on every status request, so either path returns an
// error rather than opening a terminal.
func startBareWakeDaemon(t *testing.T, status rpc.Status) *fakeDaemon {
	t.Helper()
	return listenAs(t, &fakeDaemon{status: status, spawnRefusal: refusedSpawn, statusesBeforeHangUp: hangsUpOnEveryStatus})
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

// Bare `wake` on an empty fleet asks what is running and spawns nothing.
//
// That case used to be first run, which spawned a pooled agent beside the
// manager. Now the manager is the only session a bare `wake` starts, and it is
// asked for by ensureManager once the room's question is answered - so before
// that answer nothing may be spawned at all.
func TestBareWakeOnAnEmptyFleetAsksWhatIsRunningAndSpawnsNothing(t *testing.T) {
	d := startBareWakeDaemon(t, rpc.Status{Running: true})
	t.Setenv(daemon.SocketEnv, d.socket)

	if err := bareWake(t); err == nil {
		t.Error("bare `wake` reported success against a daemon that hung up without saying what it is holding")
	}
	for _, f := range d.frames() {
		if f.Kind == rpc.FrameSpawn {
			t.Fatalf("bare `wake` on an empty fleet asked for a session (%+v) before the room knew "+
				"what was running: the manager is the only session it starts", f)
		}
	}
	if n := countKind(d.frames(), rpc.FrameStatus); n != 1 {
		t.Errorf("the daemon was asked what is running %d times, want once: the room's own question", n)
	}
}

// ⌃Q then bare `wake` is a fresh room, and the parked fleet is still reachable.
//
// ⌃Q parks the fleet and exits, so the daemon bare `wake` dials is one connect
// forks over the park book. It must hold none of the book - a parked session
// comes back only when /resume asks - and report every record parked, which is
// the list /resume resolves a name against.
func TestTheDaemonBareWakeStartsOverAParkBookHoldsItParked(t *testing.T) {
	socket := tempSocket(t)
	t.Setenv(daemon.SocketEnv, socket)
	writeParkBook(t, socket, idAlpha, "alex")

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

// writeParkBook leaves behind what a daemon that parked its fleet on the way out
// left: parked.json beside the socket, which is the one file Wake reads back
// into live state.
//
// Written by hand rather than through internal/daemon, which does not export it
// - and the hand-written shape is checked rather than trusted: the test above
// starts a real daemon on this socket and requires it to report the row, so a
// field name that drifted makes the book unreadable and fails there rather than
// passing as "nothing was parked".
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

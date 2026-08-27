//go:build unix

// The lifecycle, end to end, from a keyboard: park an agent, park the fleet and
// quit, start again, read the offer, bring it back, and be answered.
//
// # Why this is here and why it is one test
//
// The eight tasks that built park, wake, the park book, ⌃Q, the slash layer, the
// rebinding and bare `wake` each had their own suite, and each proved its own
// hop. **Nothing proved the chain.** Every hop crosses a package boundary that
// one of those suites is on exactly one side of: ⌃C's frame is written in
// internal/ui and read in internal/daemon; the park book is written by one
// daemon process and read by the next; the offer is composed in cmd/wake out of
// a report internal/daemon assembled from a file internal/daemon wrote. A seam
// where two tasks agree about a field name is invisible to both of their tests
// and fatal to the product.
//
// cmd/wake is the only package that may import both sides, which is
// converse_unix_test.go's own argument for existing, and this is that argument
// carried across a **restart**: nothing is faked but the model. A real daemon on
// a real socket, a real forked `claude`, real bytes on a terminal pipe
// ("\x03" and "\x11" are what a keyboard sends), a real second daemon started
// through the same EnsureRunning the client path uses, and the real front door
// deciding which branch to take with nothing listening.
//
// It is one test rather than ten because the subject is the chain. Split into
// hops, each half would rebuild the state the previous one left - and rebuilding
// it is precisely the step that cannot be trusted here, since a fixture written
// by the test is a fixture that agrees with the test about the format.

package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The whole chain, in the order an operator walks it.
//
// # What this kills, measured rather than claimed
//
// Each of these was applied and the test run; each failed here. They are the
// hops, and every one is a place two tasks had to agree with no test able to
// see both sides:
//
//   - `⌃C` writing no `FramePark` - the key does nothing and the room looks fine.
//   - `completePark` writing no book entry - parking works and nothing survives.
//   - `openRoom` not asking about the park book - bare `wake` takes the
//     first-run path and spawns a fresh agent beside the parked fleet.
//   - `fleet()` not carrying the book onto `Status.Parked` - `/resume` has
//     nothing to resolve a name against and the fleet is unreachable.
//   - a record losing its `Dir` - the wake is refused, or worse resumes
//     somewhere with no transcript.
//   - `/resume` writing no `FrameWake`.
//   - `unpark` leaving the woken session in the book - a later daemon offers to
//     resume an id something is running.
//
// # What it does not kill, and where those live
//
// Written down because a test whose reach is assumed is how a guard comes to
// read as coverage, and three of these were in the first draft of the list
// above before they were run.
//
//   - **`bookParked` deleted from `shutdown`.** The session here was parked by
//     `⌃C` first, so `completePark` had already written the record and `⌃Q`'s
//     write is a duplicate. That is exactly the ordering
//     `daemon.TestTheParkBookIsWrittenEarlyAndForgottenLate` reads out of the
//     AST, for the reason it says: an end-state assertion cannot see which of
//     two writers got there first.
//   - **`fleetToReopen` trusting an errored `Status`.** By the time this asks,
//     the daemon has fully exited, so there is no error to mishandle. The window
//     where there is one is a daemon still in graceful shutdown, which
//     `TestBareWakeFindsTheParkedFleetWhileTheDaemonIsStillShuttingDown` builds
//     deliberately.
//   - **`unpark` without `resumeSafe`.** Nothing here holds the id, so the check
//     has nothing to find. It needs a real bystander process and no daemon, which
//     is `daemon.TestWakingARestoredSessionStillProvesNothingHoldsItsId`.
func TestTheWholeLifecycleComposesFromAKeyboard(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)

	withScriptedAgent(t, "")
	socket := tempSocket(t)
	t.Setenv(daemon.SocketEnv, socket)
	// A projects tree the daemon and its agent share, plus the opt-in that has
	// the fake agent leave a transcript there, so the spawned session's parked
	// record is offered back below: parkedStatuses drops a parked record with no
	// transcript on disk.
	t.Setenv("WAKE_PROJECTS", t.TempDir())
	t.Setenv(fakeTranscriptEnv, "1")
	first := serveOn(t, socket)

	// --- ⌃C parks the conversation that has the keys ------------------------
	d := startConversation(t, socket)
	d.awaitView("the agent's opening turn", "ready")

	st, err := daemon.Status(socket)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(st.Sessions) != 1 {
		t.Fatalf("the daemon holds %+v, want the one session this conversation spawned", st.Sessions)
	}
	id, name := st.Sessions[0].ID, st.Sessions[0].Name

	d.typeKeys("\x03")
	awaitSessionState(t, socket, id, rpc.StateParked)
	awaitParkBook(t, socket, id)

	// --- ⌃Q parks the fleet and the daemon exits ----------------------------
	d.typeKeys("\x11")
	awaitProgramExit(t, d)
	first.awaitExit(t, "after ⌃Q")

	if recs := parkBookRecords(t, socket); len(recs) != 1 {
		t.Fatalf("the park book holds %+v once the daemon has gone, want the parked session to have "+
			"survived it: the book is the one thing that outlives a daemon", recs)
	}

	// --- bare `wake` finds a book with no daemon and no fleet behind it -------
	reopen := fleetToReopen(socket)
	if hasFleet(reopen) {
		t.Fatalf("bare `wake` reads %+v as a live fleet: a parked session is not one, and counting it "+
			"is what used to put the whole roster back in front of somebody who had just quit", reopen)
	}
	if len(reopen.Parked) != 1 {
		t.Fatalf("bare `wake` reads %+v as an empty park book, so it takes the first-run path and "+
			"spawns a fresh agent beside the parked one", reopen)
	}

	// --- the next daemon holds nothing, and /resume brings it back -----------
	second := serveOn(t, socket)
	t.Cleanup(func() { second.cancel(); second.awaitExit(t, "cleanup") })

	conn, stream, err := connect(socket, io.Discard)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	held := &connection{}
	held.replace(conn)
	t.Cleanup(held.close)

	if err := requestFleet(conn); err != nil {
		t.Fatalf("requestFleet: %v", err)
	}
	fleet, readPast, err := awaitFleet(stream)
	if err != nil {
		t.Fatalf("awaitFleet: %v", err)
	}
	// The claim this whole change exists for: the room opens holding **nothing**.
	// A daemon that restored the book put a roster row and an openable
	// conversation in front of somebody whose last act was to quit the fleet.
	if len(fleet.Sessions) != 0 {
		t.Fatalf("the daemon bare `wake` starts is holding %+v, want nothing: ⌃Q then `wake` is a fresh "+
			"room, and a session comes back only when /resume asks for it", fleet.Sessions)
	}
	if len(fleet.Parked) != 1 || fleet.Parked[0].Name != name {
		t.Fatalf("that daemon reports %+v as parked, want the one record named %s: /resume resolves a "+
			"name against this list and nothing else, so an empty one is a fleet nobody can reach", fleet.Parked, name)
	}

	room := conversationRoom(socket, fleet, conn, resume(stream, readPast), held)
	sized, _ := room.Update(tea.WindowSizeMsg{Width: viewWidth, Height: viewHeight})
	r := driveModel(t, sized)
	t.Cleanup(func() { r.stop() })

	r.typeKeys("/resume " + name + "\r")
	awaitSessionState(t, socket, id, rpc.StateIdle)

	// And out of the book, because a book entry for a session that is running is
	// what would offer a later daemon an id something already holds.
	if recs := parkBookRecords(t, socket); len(recs) != 0 {
		t.Errorf("the park book still holds %+v after the session was woken", recs)
	}

	// --- and it answers ------------------------------------------------------
	//
	// heardPrefix is the part only the far side can produce: App.submit echoes a
	// sent message locally, so asserting on what was typed proves only that it
	// was typed. This is the assertion that the session on the other end of
	// `--resume` is a process that reads its stdin.
	r.typeKeys("@" + name + " zebrafish\r")
	r.awaitViewAny("the woken agent's answer", heardPrefix+"zebrafish")
}

// serveOn is internal/daemon's own Serve on a named socket, startable twice.
//
// startRealDaemon mints its own socket and cannot be restarted on the one the
// last daemon left a book beside, which is the whole subject here.
func serveOn(t *testing.T, socket string) *chainDaemon {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- daemon.Serve(ctx, socket) }()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if info, err := os.Lstat(socket); err == nil && info.Mode()&os.ModeSocket != 0 {
			return &chainDaemon{err: errc, cancel: cancel}
		}
		select {
		case err := <-errc:
			cancel()
			t.Fatalf("Serve returned before it was listening: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	t.Fatal("the daemon never started listening")
	return nil
}

type chainDaemon struct {
	err    <-chan error
	cancel context.CancelFunc
}

// awaitExit insists the daemon actually returns. ⌃Q's promise is that the
// process ends, and a test that only checked the book would pass against a
// daemon that wrote it and then stayed up holding the fleet.
func (d *chainDaemon) awaitExit(t *testing.T, why string) {
	t.Helper()
	select {
	case err := <-d.err:
		if err != nil {
			t.Errorf("Serve returned %v (%s)", err, why)
		}
	case <-time.After(testTimeout):
		t.Fatalf("the daemon never exited (%s)", why)
	}
}

// awaitProgramExit waits for the program to end **by itself**, which is what ⌃Q
// does and what makes it different from every other way out of this harness.
//
// drivenApp.stop sends a Quit, and a Quit races the ⌃Q still sitting unread on
// the pipe: whichever Bubble Tea folds first decides, and if it is the Quit then
// FrameParkAll is never written and the daemon never hears anything. That failed
// as "the daemon never exited", which reads as a defect in shutdown.
func awaitProgramExit(t *testing.T, d *drivenApp) tea.Model {
	t.Helper()
	select {
	case final := <-d.done:
		d.mu.Lock()
		d.closed, d.final = true, final
		d.mu.Unlock()
		_ = d.keys.Close()
		return final
	case <-time.After(testTimeout):
		t.Fatalf("the program never exited on its own after ⌃Q:\n%s", d.snapshot())
		return nil
	}
}

// driveModel is startConversation's tail over a model this test built: a real
// program reading real bytes off a pipe.
func driveModel(t *testing.T, model tea.Model) *drivenApp {
	t.Helper()

	d := &drivenApp{t: t, done: make(chan tea.Model, 1)}
	reader, writer := io.Pipe()
	d.keys = writer
	d.p = tea.NewProgram(model,
		tea.WithInput(reader),
		tea.WithOutput(io.Discard),
		tea.WithFilter(func(m tea.Model, msg tea.Msg) tea.Msg {
			d.record(m)
			return msg
		}))
	go func() {
		final, err := d.p.Run()
		if err != nil {
			t.Errorf("the program failed: %v", err)
		}
		d.done <- final
	}()
	return d
}

// parkBookRecords reads parked.json as an outside reader would.
//
// Decoded into a map rather than into internal/daemon's own struct, which is
// not exported anyway: the point of reading it here is that this is the shape
// **another process** sees, and a reader that shared the writer's type could not
// notice a field being renamed.
func parkBookRecords(t *testing.T, socket string) []map[string]any {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(filepath.Dir(socket), "parked.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read the park book: %v", err)
	}
	var recs []map[string]any
	if err := json.Unmarshal(data, &recs); err != nil {
		t.Fatalf("the park book is not JSON another build could read: %v (%s)", err, data)
	}
	return recs
}

// awaitParkBook waits for the book on disk to hold exactly the given ids.
//
// **A wait rather than a read, and the sentence it replaces is why.** That
// sentence said the record exists by the time the state is reported - "written
// in retire, after core's Wait returned" - and it is false in the one direction
// a test cannot see. `completePark` calls `markParked` **first**, which is what
// makes `agent.stateLocked` answer `parked`, and only then rewrites the roster
// file and adds to the book. Both of those are a create-temp/write/chmod/rename
// each, so `daemon.Status` reports the park a whole atomic file rewrite before
// the record it is being asked about exists.
//
// The read that was here therefore raced a window it had reasoned itself out of,
// and lost on a loaded machine roughly one run in five - failing at step 2 of
// ten, which is why it failed *fast* (4.8s against 8.2s green) and read as an
// early error rather than as the timing bug it is.
//
// **The mutant is still killed.** `completePark` writing no book entry fails
// here on the deadline rather than on the first look, which is the same
// failure one poll later.
func awaitParkBook(t *testing.T, socket string, ids ...string) {
	t.Helper()

	want := append([]string(nil), ids...)
	slices.Sort(want)

	deadline := time.Now().Add(testTimeout)
	var last []map[string]any
	for time.Now().Before(deadline) {
		last = parkBookRecords(t, socket)
		got := make([]string, 0, len(last))
		for _, rec := range last {
			id, _ := rec["id"].(string)
			got = append(got, id)
		}
		slices.Sort(got)
		if slices.Equal(got, want) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("the park book holds %+v, want one record for each of %v: the book is what a later daemon "+
		"reads to offer a parked session back, so a park that is not written down is a park nothing survives",
		last, want)
}

// awaitSessionState waits for the daemon's own account of one session to reach a
// state, and fails naming the last one it saw.
//
// Asked of daemon.Status rather than read off the rendered frame, because what
// is under test at each of these points is what the *daemon* did with a frame
// the keyboard produced. The frame is asserted on separately, at the end, where
// the claim is about a reader.
func awaitSessionState(t *testing.T, socket, id, want string) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	last := "no row at all"
	for time.Now().Before(deadline) {
		if st, err := daemon.Status(socket); err == nil {
			for _, s := range st.Sessions {
				if s.ID != id {
					continue
				}
				last = s.State
				if s.State == want {
					return
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("session %s never reached %q; the daemon last reported %q", id, want, last)
}

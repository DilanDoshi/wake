// Parking a session at the daemon: what it keeps, what it gives up, and the
// one ending it must never be confused with.
//
// Split off lifecycle_test.go by subject. That file holds the four endings a
// session does not come back from; park is the one that is meant to, so every
// assertion here is about something *surviving* rather than about something
// being cleaned up.

package daemon

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// Parking keeps the session and loses only the process.
//
// The claim is per field rather than "it is still there": what park exists to
// preserve is exactly the set --resume needs (the id) plus the set the room
// needs to draw it (the name, the label, the directory). A park that kept the
// row and dropped the directory would look right in every listing and resume
// into the wrong project slug, which is the one thing 2026-08-10 findings §12
// says is completely unrecorded.
//
// Four independent claims and four `if`s rather than a `switch`: they are about
// different fields with different consequences, and short-circuiting to the
// first would report a park that dropped the directory *and* the label as one
// finding about the label.
func TestParkingKeepsTheSessionsIdentityAndLosesOnlyItsProcess(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	before := c.awaitState(idAlpha, rpc.StateIdle)

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	after := c.awaitSettled(idAlpha)

	if after.State != rpc.StateParked {
		t.Fatalf("parking settled the session as %q, want %q", after.State, rpc.StateParked)
	}
	if after.ID != before.ID {
		t.Errorf("parking changed the session id from %q to %q: --resume reuses the id it is given, so an id that moves is a transcript nobody can get back to", before.ID, after.ID)
	}
	if after.Name != before.Name {
		t.Errorf("parking changed the name from %q to %q: the name is still claimed, so a spawn cannot take it while the session is away", before.Name, after.Name)
	}
	if after.Label != before.Label {
		t.Errorf("parking dropped the label (%q -> %q)", before.Label, after.Label)
	}
	if after.Dir != before.Dir {
		t.Errorf("parking dropped the directory (%q -> %q): claude derives the project slug from it, and resuming somewhere else is unrecorded", before.Dir, after.Dir)
	}

	// And the half of the title nothing else observes. The process group the
	// daemon recorded at spawn is asked about through the daemon's own detector,
	// so this is the OS's answer rather than an inference from the state word.
	// A non-nil error is a ps that could not tell, which is not evidence either
	// way and is the only reason this is not a bare equality.
	gctx, gcancel := context.WithTimeout(context.Background(), probeTimeout)
	defer gcancel()
	gone, gerr := goneNow(gctx, []watched{{id: after.ID, pid: after.PID}})
	if gerr == nil && !gone[after.ID] {
		t.Errorf("session %s is reported parked and its process group %d is still running: waking a session "+
			"resumes its id, and two live processes on one id branch the transcript with no error and no frame",
			after.ID, after.PID)
	}
}

// A park is a park however the process ended, which is the one claim
// completePark makes that nothing else here reaches.
//
// Every other fake in this package exits 0, so every park in the suite has a
// nil `sess.Err()` - and both places the code can be narrowed to judge that
// error (`retire`'s branch and `stateLocked`'s arm) survive the whole suite
// without this. The narrowing is not adversarial: "only park a session that
// ended cleanly" is a sentence somebody writes, and `core.waitDelay` makes exit
// 0 an error whenever anything the agent spawned held stderr past the bound,
// which is the routine case for an agent running a stdio MCP server.
//
// What it would cost is the whole feature inverted: the name goes back to the
// pool, the id leaves s.agents so `holds` stops refusing a respawn under it,
// and the operator is told their ⌃C destroyed an hour of context - which is
// exactly the harm stateLocked's ordering exists to prevent, arriving through
// the one field that says nothing about the transcript.
func TestASessionThatEndedBadlyIsStillParked(t *testing.T) {
	fakeClaudeOnPath(t, "noisyexit")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	after := c.awaitSettled(idAlpha)

	// The vacuity floor, and it is doing real work: if the fake ever starts
	// exiting cleanly this test passes while asserting nothing, which is the
	// state the whole suite was in before it existed.
	if after.Error == "" {
		t.Fatalf("the parked session reports no error, so this fixture did not end badly and the assertion "+
			"below is over the same clean exit every other park test drives: %+v", after)
	}
	if after.State != rpc.StateParked {
		t.Errorf("a session that ended with %q was settled as %q, want %q: how a process ended says nothing "+
			"about the transcript --resume reads, and the one ending that refuses a park is a kill - which "+
			"refuses through the park label, not through this error",
			after.Error, after.State, rpc.StateParked)
	}
}

// The three things completePark deliberately does *not* do, asserted rather
// than described.
//
// Its doc comment states all three, and this project's own catalogue says a
// promise made in prose is not a feature - the fork snapshot line was written
// three times in comments before anything said it. Each of these is separately
// falsifiable and each has a different consequence:
//
//   - The name stays claimed. Released, a later spawn takes `alex` and the
//     parked session is un-nameable the moment its owner comes back.
//   - The id stays in s.agents, so `holds` still refuses a respawn under it -
//     which is what stops two processes reaching one transcript, the state
//     2026-08-09 findings §5 records as branching it silently.
//   - It is not an ending, so it is not in the recent endings a client polls
//     for what it missed.
func TestAParkedSessionKeepsItsNameAndItsRowRatherThanBecomingAnEnding(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	if got := c.awaitSettled(idAlpha); got.State != rpc.StateParked {
		t.Fatalf("the session settled as %q rather than parked, so nothing below is about a parked session", got.State)
	}

	// The name, from the far side: a spawn asking for it is refused.
	why, started := c.spawnOutcome(idBeta, "alex")
	switch {
	case started:
		t.Errorf("a spawn asking for a parked session's name was started: spec §5 frees a name when a "+
			"session *ends*, and a parked one has not - so %s comes back to find somebody else answering to it", "alex")
	case !strings.Contains(why, "alex"):
		t.Errorf("the spawn was refused with %q, which does not name the name it asked for", why)
	}

	// The id: a respawn under it is refused, because the row is still held.
	why, started = c.spawnOutcome(idAlpha, "sydney")
	switch {
	case started:
		t.Error("a spawn under a parked session's id was started: two live processes on one id branch the " +
			"transcript with no error, no frame and last-writer-wins, which is what waking one has to be able to rule out")
	case !strings.Contains(why, "already exists"):
		t.Errorf("the respawn was refused with %q, want the id already existing", why)
	}

	// And exactly one row for it, in exactly one state. A park that also
	// remembered an ending would report the same session parked and ended at
	// once, which is what a client reads to decide the conversation is over.
	rows := 0
	for _, s := range c.status().Sessions {
		if s.ID != idAlpha {
			continue
		}
		rows++
		if s.State != rpc.StateParked {
			t.Errorf("the parked session is also reported %q: park is not an ending and must not enter the recent endings", s.State)
		}
	}
	if rows != 1 {
		t.Errorf("the parked session has %d rows in the report, want 1: a second row is the remembered ending park must not create", rows)
	}
}

// The roster record goes, and that is not tidiness.
//
// roster.go is "the minimum needed to find a process again" and the reaper
// SIGKILLs the process groups it can verify from that file. A parked session
// has no process, so an entry left behind names a pgid that is gone and may
// since have been recycled - a later daemon's reaper aiming a group kill at
// whatever inherited the number.
func TestParkingTakesTheSessionOutOfTheOnDiskRoster(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)

	if recs := loadRoster(rosterPath(d.socket)); len(recs) != 1 {
		t.Fatalf("the roster holds %d records before the park, want 1: this test cannot show a removal it never saw an addition for", len(recs))
	}

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	if got := c.awaitSettled(idAlpha); got.State != rpc.StateParked {
		t.Fatalf("the session settled as %q rather than parked, so a roster with nothing in it would prove nothing", got.State)
	}

	if recs := loadRoster(rosterPath(d.socket)); len(recs) != 0 {
		t.Errorf("the roster still holds %v after the park: the reaper signals what it can verify from that file, and a parked session's pgid is gone and may have been recycled", recs)
	}
}

// A killed session is never a parked one, and this is the arm the whole
// park/wake feature is unsafe without.
//
// What a --resume of a transcript a SIGKILL cut mid-turn loads is unrecorded,
// and this project's rule is that unrecorded behaviour is refused rather than
// designed around. kill() clears the parking flag for exactly that, and nothing
// else in the suite reaches those three lines: every other kill test drives an
// agent nobody asked to park.
//
// The wedged agent is the realistic way here rather than a contrivance. Its
// process is gone while a grandchild holds its stdout, so park cannot end it -
// stop closes a stdin nothing is left to read - and kill is what an operator
// reaches for next. That is a park request and a SIGKILL on one session, in the
// order they actually happen.
func TestAWedgedSessionKilledAfterAParkRequestIsReportedEndedAndNeverParked(t *testing.T) {
	fakeClaudeOnPath(t, "hold")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	// Spoken by the grandchild after its parent exited, so the wedge is
	// established rather than assumed.
	c.awaitEvent(idAlpha, "held")

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	// A second frame through the same queue, and its failure is the barrier:
	// the queue is FIFO and one goroutine deep, so an error saying this write
	// found no stdin is proof the park ahead of it has already been applied.
	// A sleep here would be a guess about somebody else's machine.
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "after the park"})
	c.awaitErrorFor(idAlpha)

	c.send(rpc.Frame{Kind: rpc.FrameKill, SessionID: idAlpha})

	got := c.awaitSettled(idAlpha).State
	if got != rpc.StateEnded {
		t.Errorf("a session SIGKILLed after a park request settled as %q, want %q: a transcript a kill cut "+
			"mid-anything has no recorded --resume behind it, so it must not be offered back as a park",
			got, rpc.StateEnded)
	}
}

// awaitSettled waits for the unsolicited status *push* in which one session
// stops running, and hands back the row it arrived in - whichever of the two
// terminal states it reached.
//
// **The wait spans the whole outcome space, and that is the point.**
// `awaitState(id, StateParked)` waits for the good answer, so every way of
// getting the wrong one is a fifteen-second timeout naming the state that did
// not happen instead of a failure naming the one that did. Three tests here did
// that, and the ordering mutation cost 45 seconds to learn what this reports in
// 0.02s - the rule this task minted in docs/notes/decisions.md, applied to the
// tests that minted it.
//
// A push and not a poll, for `awaitState`'s own reason: a reply proves only that
// the daemon answered a question, and what park owes a client is that it says so
// unasked. Nothing else broadcasts one in these tests' lifetime - the liveness
// tick is clamped to 30s - so the first push after the process goes is the one
// under test.
func (c *testClient) awaitSettled(sessionID string) rpc.SessionStatus {
	c.t.Helper()

	var got rpc.SessionStatus
	c.await(fmt.Sprintf("session %s pushed in a state it has stopped running in", sessionID), func(f rpc.Frame) bool {
		if f.Kind != rpc.FrameStatusPush || f.Status == nil {
			return false
		}
		for _, s := range f.Status.Sessions {
			if s.ID == sessionID && (s.State == rpc.StateEnded || s.State == rpc.StateParked) {
				got = s
				return true
			}
		}
		return false
	})
	return got
}

// spawnOutcome asks for a session and reports which of the two things happened:
// it was refused, and why, or it started.
//
// One wait over both outcomes rather than a wait for the refusal, because those
// are not the same test. Waiting only for the error frame turns "the spawn
// succeeded" - which is the defect - into a fifteen-second timeout whose message
// names a frame that never came instead of the name that was handed away. A
// mutation that hangs and a mutation that fails look identical in a summary
// line; docs/notes/decisions.md names that one specifically.
func (c *testClient) spawnOutcome(id, name string) (why string, started bool) {
	c.t.Helper()

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: id, Text: name})
	f := c.await("the daemon's answer to a spawn of "+id, func(f rpc.Frame) bool {
		if f.Kind == rpc.FrameError && f.SessionID == id {
			return true
		}
		if f.Kind != rpc.FrameStatusReply || f.Status == nil {
			return false
		}
		for _, s := range f.Status.Sessions {
			if s.ID == id {
				return true
			}
		}
		return false
	})
	return f.Text, f.Kind != rpc.FrameError
}

// awaitErrorFor is the daemon's refusal addressed to one session id.
func (c *testClient) awaitErrorFor(sessionID string) string {
	c.t.Helper()
	f := c.await("an error frame for "+sessionID, func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && f.SessionID == sessionID
	})
	return f.Text
}

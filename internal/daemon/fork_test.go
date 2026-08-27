// Forking one session into another: what the daemon refuses, what it starts,
// and the edge nothing on Claude's wire will ever tell it.
//
// Everything here runs against a real socket and a real fake `claude`, which
// is this package's house style: a test that drives a whole lifecycle
// constrains far more than a dozen asserting on internals.
//
// The gate itself is held from the other side in forkgate_test.go - the pure
// function over its whole input, and a static assertion about what it may read.
// Split by subject rather than by size: a lifecycle test proves the gate is
// wired to something, and only the other file can prove it is total.

package daemon

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// forkOf asks the daemon to fork one session into another and waits for the
// confirmation, which is the same FrameStatusReply a spawn is confirmed with -
// a fork is an ordinary session from the moment it starts.
//
// **The wait spans both outcomes**, and the refusal is reported rather than
// waited through. A fork has exactly two answers and the daemon addresses both
// to the fork's own id; waiting only for the good one turns every way of being
// refused into a fifteen-second timeout naming a reply that never came, with
// the daemon's own sentence sitting unread in the frames the failure prints.
// Measured on the mutation that flips the parked verdict: 15.02s and "waited
// 15s for the forked session in a status reply" against 0.05s and the refusal
// itself. See docs/notes/decisions.md - the wait has to span the whole outcome
// space, or the assertion is not about the thing that went wrong.
func forkOf(c *testClient, parentID, forkID, requested string) rpc.SessionStatus {
	c.t.Helper()

	c.send(rpc.Frame{Kind: rpc.FrameFork, SessionID: forkID, ParentID: parentID, Text: requested})
	var got rpc.SessionStatus
	f := c.await("the daemon's answer to a fork of "+parentID, func(f rpc.Frame) bool {
		if f.Kind == rpc.FrameError && f.SessionID == forkID {
			return true
		}
		if f.Kind != rpc.FrameStatusReply || f.Status == nil {
			return false
		}
		for _, s := range f.Status.Sessions {
			if s.ID == forkID && s.State != rpc.StateEnded {
				got = s
				return true
			}
		}
		return false
	})
	if f.Kind == rpc.FrameError {
		c.t.Fatalf("the daemon refused this fork: %s", f.Text)
	}
	return got
}

// The whole point of --fork-session: the fork is a Wake session in its own
// right - its own UUID, its own name from the pool, its own roster row - which
// is what makes it nameable, DM-able and re-forkable with no new bookkeeping.
//
// And the edge, which is the half nothing else can supply. Neither stream says
// a session was forked or from what: the fork's init carries 23 keys and not
// one of them names an ancestor, and the parent emits nothing at all while a
// fork is taken (2026-08-10 findings §6).
func TestAForkIsAnOrdinarySessionThatRemembersItsParent(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	parent := spawnFor(c, idAlpha, "alex", t.TempDir())
	c.pollState(idAlpha, rpc.StateIdle)

	got := forkOf(c, idAlpha, idGamma, "")

	if got.Name == "" || got.Name == parent.Name {
		t.Errorf("the fork is called %q and its parent %q: a fork draws its own name from the pool, "+
			"because no two live sessions may share one", got.Name, parent.Name)
	}
	if got.ParentID != idAlpha {
		t.Errorf("the fork reports ParentID %q, want %q - nothing on either stream carries this, "+
			"so a daemon that does not record it has thrown the relationship away", got.ParentID, idAlpha)
	}
	if parent.ParentID != "" {
		t.Errorf("the parent reports ParentID %q, want empty: it is nobody's fork", parent.ParentID)
	}
}

// The argv the daemon actually spawns, on a real process, not the one
// buildArgs says it would. This is the end-to-end half of core's unit test:
// the triple has to survive Config, launch and exec, in order and adjacent.
func TestAForkedAgentIsStartedWithTheRecordedTriple(t *testing.T) {
	fakeClaudeOnPath(t, "argv")
	d := startDaemon(t)
	c := attach(t, d.socket)

	spawnFor(c, idAlpha, "alex", t.TempDir())
	c.pollState(idAlpha, rpc.StateIdle)
	forkOf(c, idAlpha, idGamma, "")

	c.awaitEvent(idGamma, "--resume "+idAlpha+" --fork-session --session-id "+idGamma)
}

// The parent here was idle - turn finished, result arrived, transcript
// flushed. A parent that is mid-turn is a different recording and it was not
// made, so it is refused rather than guessed at.
//
// The refusal has to say *when*, because a key that does nothing and explains
// nothing is the failure the legend rule exists for, arriving at runtime.
//
// The ask comes before the wait, which is not a style point: awaitRefusal
// blocks for testTimeout, so a wait written above the frame that provokes it is
// a fifteen-second failure naming nothing.
//
// The turn is held open by a fake that never ends one, rather than by a slow
// one: `slow` finishes its turn after a delay, so the window in which the
// parent is provably mid-turn closes on a timer and the frame below would race
// it. `mute` owes a turn end forever, so the state this asserts about is a
// state the parent is still in when the daemon reads it.
func TestForkingAParentThatIsMidTurnIsRefusedAndSaysWhen(t *testing.T) {
	fakeClaudeOnPath(t, "mute")
	d := startDaemon(t)
	c := attach(t, d.socket)

	spawnFor(c, idAlpha, "alex", t.TempDir())
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "take a while"})
	c.pollState(idAlpha, rpc.StateWorking)

	c.send(rpc.Frame{Kind: rpc.FrameFork, SessionID: idGamma, ParentID: idAlpha})

	// awaitRefusal matches on the frame's SessionID, so this also holds the
	// property both clients depend on: a refusal is addressed to the fork's own
	// id, never the parent's. Neither client's wait has a deadline, so a
	// misaddressed refusal is a client that waits for good.
	why := awaitRefusal(c, idGamma)
	if !strings.Contains(why, "alex") {
		t.Errorf("the refusal is %q and does not name the parent", why)
	}
	if !strings.Contains(why, "turn ends") {
		t.Errorf("the refusal is %q and does not say when a fork would be accepted - "+
			"a refusal an operator cannot act on is a key that does nothing", why)
	}
}

// The parent's transcript is on disk whether or not a process holds it, and
// the 2026-08-09 spike forked exited parents throughout. So an ended parent is
// forkable, for as long as this daemon still remembers the session at all.
func TestAnEndedParentCanStillBeForked(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	spawnFor(c, idAlpha, "alex", t.TempDir())
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	if got := forkOf(c, idAlpha, idGamma, ""); got.ParentID != idAlpha {
		t.Errorf("forking an ended parent produced %+v, want a session whose ParentID is %s", got, idAlpha)
	}
}

// And a **parked** parent can be forked, against a parent this daemon actually
// parked rather than against a status row somebody typed.
//
// This is the marquee verdict of park, and it was reachable only through
// hand-built `rpc.SessionStatus{State: "parked"}` rows: forkVerdicts is a table
// over a pure function, and cmd/wake's is a table against a fake daemon. Neither
// can see a park whose *row* is right and whose parent is not forkable for some
// other reason - and one is now promised in the sentence `wake attach` prints to
// an operator who is already stuck, which tells them to run `wake fork <name>`.
// A string grep proves the sentence names the verb. This proves the verb works,
// which is rung 5 read one level out: guard the unit the property belongs to.
//
// The directory is asserted with it because that is the whole of the cwd
// objection. Forking from a different working directory is completely
// unrecorded (2026-08-10 findings §12), a fork runs in the parent's own
// directory, and a parked session keeping that field is what makes the two
// sentences compose - stated in three doc comments and, until now, nowhere a
// change could break.
//
// Its sibling above is the argument: an ended parent and a parked one are the
// same physical object at retire - `cmd.Wait()` has returned, the transcript is
// flushed, the directory is untouched - which is why the corpus's exited
// parents are evidence about this at all.
func TestAParkedParentCanStillBeForked(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	dir := t.TempDir()
	spawnFor(c, idAlpha, "alex", dir)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	if got := c.awaitSettled(idAlpha); got.State != rpc.StateParked {
		t.Fatalf("the parent settled as %q rather than parked, so this is the ended case again", got.State)
	}

	got := forkOf(c, idAlpha, idGamma, "")
	if got.ParentID != idAlpha {
		t.Fatalf("forking a parked parent produced %+v, want a session whose ParentID is %s: a parked "+
			"process has exited and the transcript is on disk, which is the state every recorded fork "+
			"resumed - and `wake attach` now tells an operator to run exactly this", got, idAlpha)
	}
	if got.Dir != dir {
		t.Errorf("the fork of a parked parent runs in %q, want the parent's own %q: claude derives the "+
			"project slug from the working directory, and forking from a different one is unrecorded",
			got.Dir, dir)
	}
}

// Wake does not fork a UUID it has never held. It would not know which
// directory the session ran in - and claude locates a transcript by the
// project slug it derives from that directory, which makes forking from
// anywhere else completely unrecorded.
func TestForkingSomethingThisDaemonNeverHeldIsRefused(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameFork, SessionID: idGamma, ParentID: idBeta})

	if why := awaitRefusal(c, idGamma); !strings.Contains(why, idBeta) {
		t.Errorf("the refusal is %q and does not name the session that could not be found", why)
	}
}

// A fork frame that lost its parent id starts nothing. The whole argument for
// FrameFork being its own kind is that "spawn fresh" is the wrong answer to a
// fork whose ParentID was dropped in transit - so the frame that expresses it
// has to be refused rather than defaulted, and refused to the fork's own id.
func TestAForkFrameWithNoParentIsRefusedRatherThanSpawningFresh(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameFork, SessionID: idGamma})

	// The message, and not only the refusal. Deleting forkSource's
	// `parentID == ""` arm leaves the safety property intact - an empty id
	// matches no live or remembered row, so the loop falls through to the
	// bottom error and nothing starts - which means a test asserting only
	// "refused, nothing started" holds this guard's *conclusion* and not the
	// guard. What is actually lost is the sentence: the operator gets
	// "this daemon is not holding session , so there is nothing to fork",
	// with a hole in it where the id should be, instead of being told what
	// the frame was missing.
	why := awaitRefusal(c, idGamma)
	if !strings.Contains(why, "needs a session") {
		t.Errorf("the refusal is %q, and does not say what the frame was missing - "+
			"an empty parent id falls through to the not-held error, whose sentence "+
			"has a hole where the id would be", why)
	}
	for _, s := range c.status().Sessions {
		if s.ID == idGamma {
			t.Fatalf("a fork with no parent started a session anyway: %+v - "+
				"an empty parent id has to be refused, because the fresh session it would "+
				"otherwise start is a plausible-looking agent with none of the context "+
				"somebody asked for", s)
		}
	}
}

// A fork runs where its parent ran, and f.Dir is ignored. The directory
// decides the project slug claude derives the transcript path from, so the
// parent's own is the only one with a recording behind it.
func TestAForkRunsWhereItsParentRan(t *testing.T) {
	fakeClaudeOnPath(t, "cwd")
	d := startDaemon(t)
	c := attach(t, d.socket)

	dir := t.TempDir()
	spawnFor(c, idAlpha, "alex", dir)
	c.pollState(idAlpha, rpc.StateIdle)

	// No Dir on the fork frame at all: the client does not get to choose.
	forkOf(c, idAlpha, idGamma, "")

	// Resolved, because a temp directory on darwin is reached through a
	// symlink and the agent reports where it actually is - the same correction
	// TestASessionRunsInTheDirectoryTheClientAsksFor carries.
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	c.awaitEvent(idGamma, "cwd: "+want)
}

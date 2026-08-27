//go:build unix

// What a restored row is allowed to assert, and what a wake still has to prove.
//
// Unix-only for unpark_unix_test.go's reason: the subject is a real process on
// a real machine. idsInUse is what turns a claim on disk back into a fact, and
// liveid_other.go cannot look at all - so on any other platform every wake here
// is refused for a reason that has nothing to do with the book.

package daemon

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// Waking takes the session out of the book, so a restart does not offer back
// something that is running.
//
// The book and the fleet disagreeing is invisible until a restart, which is
// what makes this its own test rather than a line in the wake tests: a stale
// entry costs nothing today and offers a later daemon an id something already
// holds. The visible failure is then an operator being told their session
// cannot come back while it is running in front of them - or, on a machine
// whose ps cannot answer, a second process on one transcript.
func TestWakingASessionTakesItOutOfTheParkBook(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 1 {
		t.Fatalf("the park book holds %d records after the park, want 1: this test cannot show a removal "+
			"it never saw an addition for", len(recs))
	}

	if got := wakeOutcome(c, idAlpha); !got.woke {
		t.Fatalf("the parked session was not woken, so nothing here is about a session that came back: %s", got.why)
	}
	if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 0 {
		t.Errorf("the park book still holds %+v after the session was woken. A later daemon reads that "+
			"file back into its fleet, so it would offer to resume an id this daemon is running a "+
			"process under - two live processes on one transcript, with no error on any wire", recs)
	}
}

// **A restored row is a claim, not a proof, and this is the test that says so.**
//
// completePark writes the entry in retire - after core's Wait has returned - so
// the daemon that wrote it really had watched its own process go. That proof
// does not travel with the file. Between the two daemons anything can have
// started on the id: a stray `claude --resume` an operator ran by hand, an
// orphan a crashed daemon left, a second Wake. The book cannot know, and the
// failure it would cause has no symptom on any wire - both processes answer
// correctly from their own history and the transcript branches in place with
// last-writer-wins.
//
// So a restore may assert identity and location and nothing about liveness, and
// unpark re-proves the rest through resumeSafe exactly as it does for a session
// this daemon parked itself. The mutation this kills is the tempting one:
// letting a restored agent skip the check because "the book says it is parked".
func TestWakingARestoredSessionStillProvesNothingHoldsItsId(t *testing.T) {
	fakeClaudeOnPath(t, "")
	socket := tempSocket(t)
	plantTranscript(t, idAlpha) // the session ran; a resumable record has a transcript

	first := startDaemonOn(t, socket)
	c := attach(t, socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)
	c.close()
	first.stop(t)

	// Started while no daemon is running, which is the honest shape of it: the
	// window this covers is exactly the one in which Wake is not watching.
	if argv := bystander(t, "--session-id", idAlpha); !holdsTheSession(argv, idAlpha) {
		t.Fatalf("the bystander runs as %q and core would not read that as a process holding session %s: "+
			"nothing is holding it, so a refusal below would be about something else and its absence "+
			"about nothing", argv, idAlpha)
	}

	startDaemonOn(t, socket)
	back := attach(t, socket)
	if row := sessionRow(back.status(), idAlpha); row.State != rpc.StateParked {
		t.Fatalf("the second daemon reports session %s as %q, want %q: there is no restored row here and "+
			"a refusal below would be about an unknown session instead", idAlpha, row.State, rpc.StateParked)
	}

	got := wakeOutcome(back, idAlpha)
	if got.woke {
		t.Fatalf("a restored session was woken while another process held its id, and came back as %+v. "+
			"A park book entry is a claim by a daemon that is no longer running - it says a process was "+
			"gone once, not that the id is free now - and resuming an id a second process holds branches "+
			"the transcript with no error anywhere", got.row)
	}
	if !strings.Contains(got.why, "still running") {
		t.Errorf("the refusal is %q and does not say a process is still running under that id. Nothing on "+
			"claude's wire reports the collision, so this sentence is the only account anyone gets", got.why)
	}
}

// A restored session wakes in the directory it was recorded in, and never in
// whatever directory happens to be current.
//
// claude derives the project slug from the working directory and locates a
// transcript under it, so a wake anywhere else opens a session that is empty
// and looks fine. Resuming in a different working directory is completely
// unrecorded (2026-08-10 findings §12), which is why the daemon carries the
// directory rather than guessing it - and across a restart the only thing that
// remembers is the book.
//
// The daemon's own working directory is the trap, and the floor below names it:
// cwdOrHome is what spawnDir falls back to, so a restore that dropped Dir would
// resume the whole fleet wherever the terminal that first forked the daemon
// happened to be.
func TestARestoredSessionWakesInTheDirectoryItWasRecordedIn(t *testing.T) {
	fakeClaudeOnPath(t, "cwd")
	socket := tempSocket(t)
	dir := t.TempDir()

	// Resolved, because a temp directory on darwin is reached through a symlink
	// and the agent reports where it actually is.
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if here, _ := filepath.EvalSymlinks(cwdOrHome()); here == want {
		t.Fatalf("the session's directory is the daemon's own (%s), so a wake that resumed in $PWD would "+
			"land in the right place by accident and this test would assert nothing", want)
	}

	first := startDaemonOn(t, socket)
	c := attach(t, socket)
	spawnFor(c, idAlpha, "alex", dir)
	c.awaitEvent(idAlpha, "cwd: "+want)
	c.pollState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)
	c.close()
	first.stop(t)

	startDaemonOn(t, socket)
	back := attach(t, socket)
	if got := wakeOutcome(back, idAlpha); !got.woke {
		t.Fatalf("the restored session was not woken, so there is no working directory to read: %s", got.why)
	}
	// This client has never seen a cwd line - the first daemon's went to a
	// connection that is closed - so the next one is the woken process's.
	got := back.await("the woken agent reporting where it is running", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == idAlpha && f.Event != nil &&
			strings.Contains(f.Event.Text, "cwd: ")
	})
	if text := got.Event.Text; text != "cwd: "+want {
		t.Errorf("a restored session was woken in %q, want %q. claude locates a transcript by the "+
			"directory it was started in, so a wake anywhere else opens an empty session under the right "+
			"id and nothing says the history is missing", strings.TrimPrefix(text, "cwd: "), want)
	}
}

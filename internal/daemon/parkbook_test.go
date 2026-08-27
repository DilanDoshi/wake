// The park book: the one thing a daemon writes down that a later daemon reads
// back into its own fleet.
//
// Every test here is about a *file* rather than about a process, so the file is
// this package's most exposed artefact: it outlives the build that wrote it.
// That is why the format is asserted against bytes as well as through a round
// trip - a reader and a writer that agree on the wrong key round-trip perfectly
// and are unreadable by anything else, including the next version of this file.

package daemon

import (
	"encoding/json"
	"github.com/DilanDoshi/wake/internal/core"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A parked session outlives the daemon that parked it.
//
// This is the one thing the on-disk roster deliberately cannot do, so it is a
// second file rather than a field: loadRoster's consumers are reapOrphans,
// which SIGKILLs what it can verify, and FleetOnDisk, which builds a report.
// Neither turns a record into an agent, and a parked session put in there would
// name a process group that is gone and may since have been recycled.
//
// The second daemon is asked rather than waited on. restoreParked runs before
// the accept loop, so the row is there the instant a client can connect - and a
// wait would turn the defect into a fifteen-second timeout naming a push that
// never came instead of the report that was handed over. Nothing pushes here
// anyway: the liveness tick clamps to 30s, twice testTimeout.
func TestAParkedSessionSurvivesTheDaemonThatParkedIt(t *testing.T) {
	fakeClaudeOnPath(t, "")
	socket := tempSocket(t)
	// The session ran, so it has a transcript; parkedStatuses offers back only a
	// record that reaches one.
	plantTranscript(t, idAlpha)

	first := startDaemonOn(t, socket)
	c := attach(t, socket)
	c.spawn(idAlpha, "alex")
	before := c.awaitState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)
	c.close()
	first.stop(t)

	startDaemonOn(t, socket)
	back := attach(t, socket)
	after := sessionRow(back.status(), idAlpha)

	switch {
	case after.State == "":
		t.Fatalf("the second daemon holds nothing for session %s. The transcript is still on disk and "+
			"--resume needs the id to reach it, so an id nobody wrote down is a conversation nobody "+
			"can get back to", idAlpha)
	case after.State != rpc.StateParked:
		t.Fatalf("the restored session came back as %q, want %q", after.State, rpc.StateParked)
	case after.Name != before.Name:
		t.Errorf("the parked session came back named %q, want %q: the name is what /resume is typed against", after.Name, before.Name)
	case after.Dir != before.Dir:
		t.Errorf("the parked session came back with directory %q, want %q. claude derives the project "+
			"slug from it, so a wake that resumed anywhere else would be looking for a transcript that "+
			"is not there - and resuming from a different working directory is completely unrecorded",
			after.Dir, before.Dir)
	case after.Label != before.Label:
		t.Errorf("the parked session came back labelled %q, want %q", after.Label, before.Label)
	}
}

// `wake stop` forgets the parked fleet, and that is what keeps it the one
// irreversible verb.
//
// Spec §2 names stop as the deliberate ending and `⌃Q` as the recoverable one.
// A stop that left twenty sessions for the next `wake` to offer back would make
// the irreversible verb reversible by accident, in the direction nobody checks.
// What goes is Wake's memory of them; the transcripts stay exactly where claude
// put them.
//
// Keyed on the *quit verb* rather than on shutdown, which is the distinction
// the test below holds from the other side: a daemon that was signalled decided
// nothing.
func TestStoppingTheDaemonForgetsTheParkedFleet(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 1 {
		t.Fatalf("the park book holds %d records after the park, want 1: a clear proves nothing over an "+
			"empty book", len(recs))
	}

	c.send(rpc.Frame{Kind: rpc.FrameQuit})
	d.waitForExit(t)

	if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 0 {
		t.Errorf("the park book still holds %+v after the quit verb. Stop is the ending there is no way "+
			"back from, and a book that survives it makes the next `wake` offer to resume what somebody "+
			"deliberately ended", recs)
	}
}

// A daemon that was signalled keeps the book, and a daemon that was asked to
// quit does not.
//
// The two endings look identical from inside shutdown - every session stopped,
// every client closed, the roster cleared - and only one of them is a decision.
// `wake stop` writes FrameQuit; a SIGTERM, a laptop shutting down or a crashed
// terminal cancels the context that serveDaemon built from signal.NotifyContext,
// and nobody in that story said to forget anything. Without the distinction the
// whole feature is one signal away from being lost, and this is the assertion
// that fires when the clear is moved into shutdown where it looks tidier.
func TestADaemonThatWasSignalledRatherThanStoppedKeepsTheParkedFleet(t *testing.T) {
	fakeClaudeOnPath(t, "")
	socket := tempSocket(t)

	d := startDaemonOn(t, socket)
	c := attach(t, socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)
	c.close()

	// The context cancellation Serve is given, which is what signal.NotifyContext
	// delivers on SIGINT and SIGTERM in cmd/wake's serveDaemon.
	d.stop(t)

	if recs := loadParkBook(parkBookPath(socket)); len(recs) != 1 {
		t.Fatalf("the park book holds %+v after the daemon was signalled, want the one session it "+
			"parked: a signal is not somebody deciding a fleet is over, and the transcripts are still "+
			"on disk with nothing left naming the ids that reach them", recs)
	}
}

// Nothing is started by a restore, and this is the assertion that says so.
//
// A restore that spawned N claude processes would put them in front of the
// FrameHello that EnsureRunning's discriminator waits for, so `wake status`,
// `wake attach` and `wake fork` would each relaunch a fleet. It would also
// resume every one of those ids without ever asking whether anything already
// holds them, which is the collision resumeSafe exists to prevent - at fleet
// scale, on a machine where the previous daemon may still be finishing.
//
// Two independent claims. The state word says no process was started at all -
// launch is the only thing that starts one and a session it started is never
// reported parked, because stateLocked's parked arm reads a flag only
// markParked sets and markParked runs after the process has gone. The pgid says
// the row carries no process group either way, which is the half the reaper
// cares about: it signals what it can verify from a number, and a parked
// session's number is gone and may have been recycled.
func TestARestoredSessionHasNoProcessBehindIt(t *testing.T) {
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

	startDaemonOn(t, socket)
	back := attach(t, socket)
	row := sessionRow(back.status(), idAlpha)

	if row.State != rpc.StateParked {
		t.Fatalf("the restored session is reported %q, want %q: a restore starts nothing, so anything "+
			"else here is a process this daemon started without proving the id was free", row.State, rpc.StateParked)
	}
	if row.PID != 0 {
		t.Errorf("the restored session reports process group %d. A parked session has no process, and a "+
			"pgid on it is either a group that is gone - which is what the reaper must never be handed - "+
			"or one this restore started", row.PID)
	}
}

// A parked session still holds its **id** across a restart, and no longer holds
// its **name**.
//
// The id is the half that matters and the half that is load-bearing. Nothing is
// in s.agents for a parked session any more, so register's own check no longer
// covers it - admit asks the park book instead, because a second process under
// a parked id branches its transcript with last-writer-wins and no error on any
// wire, which is the one collision this project cannot detect afterwards.
//
// The name is the half that was given up deliberately, and this records the
// trade rather than mourning it. A daemon that claimed every parked name at
// startup is a daemon holding the whole fleet, which is what ⌃Q is for getting
// rid of. So `@alex` is free the moment nothing is running as alex, and a
// resume whose name has since been taken comes back under a pooled one - the
// same fallback a book with two records under one name has always used. The
// transcript is reached by id, so nothing is lost but the word.
func TestAParkedSessionHoldsItsIdAcrossARestartAndNoLongerItsName(t *testing.T) {
	fakeClaudeOnPath(t, "")
	socket := tempSocket(t)

	first := startDaemonOn(t, socket)
	c := attach(t, socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)
	c.close()
	first.stop(t)

	startDaemonOn(t, socket)
	back := attach(t, socket)

	// The id: refused, and the refusal says what to do instead. "already
	// exists" would be true and useless - nothing on this path would tell
	// somebody the session is one command away.
	why, started := back.spawnOutcome(idAlpha, "sydney")
	switch {
	case started:
		t.Error("a spawn under a parked session's id was started: two live processes on one id branch " +
			"the transcript with no error, no frame and last-writer-wins, and nothing holds that id " +
			"in the fleet any more")
	case !strings.Contains(why, "parked"):
		t.Errorf("the respawn was refused with %q, which does not say the id belongs to a parked session", why)
	case !strings.Contains(why, "/resume"):
		t.Errorf("the respawn was refused with %q, which does not name the command that does what the "+
			"caller was trying to do", why)
	}

	// The name: free, because nothing is running under it.
	if _, started := back.spawnOutcome(idBeta, "alex"); !started {
		t.Error("a spawn asking for a parked session's name was refused: no process holds that name, " +
			"and a daemon that reserved every parked name at startup would be holding the whole fleet " +
			"- which is what ⌃Q exists to clear")
	}
}

// The bytes on disk, read the way anything other than this file would read
// them.
//
// A test that writes with the writer and reads with the reader proves that the
// pair agree with each other and nothing about what is in the file. Both halves
// below go around one of them: the key set is read out of the raw JSON, and a
// record is decoded from a literal somebody typed. Renaming `dir` to
// `directory` on both sides passes every other test in this package and makes
// every book already on disk resume in the wrong project.
func TestTheParkBookOnDiskIsTheShapeAnotherBuildWouldRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), parkBookName)
	book := newParkBook(path)
	if err := book.add(parkedRecord{ID: idAlpha, Name: "alex", Label: "dev-5748", Dir: "/tmp/repo"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the park book back: %v", err)
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("the park book is not a JSON array of objects: %v\n%s", err, data)
	}
	if len(raw) != 1 {
		t.Fatalf("the park book holds %d objects, want 1:\n%s", len(raw), data)
	}

	// Exactly these, in both directions. A missing key is a field that does not
	// survive a restart; an extra one is state Wake has quietly started owning,
	// and `pid` is the specific extra that matters - a process group in this
	// file is one a later reaper could be handed for a session that has none.
	want := map[string]bool{"id": true, "name": true, "label": true, "dir": true, "parked": true}
	for key := range raw[0] {
		if !want[key] {
			t.Errorf("the park book carries %q, which is not one of the five things a parked session is. "+
				"A parked session has no process, and that absence is the whole type-level statement: "+
				"anything naming one here is either gone or recycled\n%s", key, data)
		}
	}
	for key := range want {
		if _, ok := raw[0][key]; !ok {
			t.Errorf("the park book does not carry %q. Every one of the five is read back into an agent - "+
				"the id --resume is given, the directory it has to run in, and the two display halves\n%s", key, data)
		}
	}

	// And the other direction: a book somebody else wrote decodes into the same
	// record. The literal is the format, stated once in bytes.
	//
	// Its id is a constant of its own rather than idAlpha, and that is the
	// point rather than an oversight: these are bytes *another build* wrote, so
	// nothing about them may move when this build's fixtures do. idAlpha is now
	// minted per test binary (see harness_test.go), and a format anchor that
	// followed it would assert that this build can read what it just generated
	// - which is the round trip above, already covered, and not what a book
	// already on disk is.
	const writtenID = "a11a0000-0000-4000-8000-00000000a11a"
	literal := filepath.Join(t.TempDir(), parkBookName)
	const written = `[{"id":"` + writtenID + `","name":"alex","label":"dev-5748",` +
		`"dir":"/tmp/repo","parked":"2026-08-10T09:00:00Z"}]`
	if err := os.WriteFile(literal, []byte(written), parkBookPerm); err != nil {
		t.Fatalf("write a park book by hand: %v", err)
	}
	got := loadParkBook(literal)
	if len(got) != 1 {
		t.Fatalf("a hand-written park book decoded to %d records, want 1", len(got))
	}
	if got[0].ID != writtenID || got[0].Name != "alex" || got[0].Label != "dev-5748" || got[0].Dir != "/tmp/repo" {
		t.Errorf("a hand-written park book decoded to %+v, want the session it names. These are the bytes "+
			"a book already on disk is written in, and a reader that cannot read them loses every parked "+
			"session the last build recorded", got[0])
	}
}

// The book names every session on the machine and the directory each ran in,
// so it is as private as the directory holding it.
func TestTheParkBookIsPrivateToItsOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), parkBookName)
	book := newParkBook(path)
	if err := book.add(parkedRecord{ID: idAlpha, Dir: "/tmp/repo"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the park book: %v", err)
	}
	if perm := info.Mode().Perm(); perm != parkBookPerm {
		t.Errorf("the park book is mode %04o, want %04o: it lists every session on this machine and the "+
			"directories they ran in, and a readable one is a map of somebody's work", perm, parkBookPerm)
	}
}

// A book nobody can read costs the offers in it and never the fleet.
//
// Refusing to start would take the live sessions down with the unreadable file,
// which is the wrong end of the trade by a wide margin: what a corrupt book
// loses is Wake's memory of transcripts that are still on disk, and what
// refusing loses is every agent somebody has running.
func TestAnUnreadableParkBookIsReportedAndDoesNotStopTheDaemon(t *testing.T) {
	socket := tempSocket(t)
	if err := os.WriteFile(parkBookPath(socket), []byte("{not json"), parkBookPerm); err != nil {
		t.Fatalf("write a broken park book: %v", err)
	}

	startDaemonOn(t, socket)
	c := attach(t, socket)
	if st := c.status(); !st.Running {
		t.Fatalf("the daemon did not come up behind an unreadable park book: %+v", st)
	}
}

// An id Wake did not mint cannot be resumed under, so it is not restored.
//
// It is maySpawn's rule arriving at the other door. The reaper's whole proof
// that a process group is an agent it recorded is the session UUID appearing in
// an argv, and a short or ordinary id - "build", "s1" - matches any process
// whose command line happens to contain it. maySpawn establishes that for ids
// arriving on the wire; this file is the other source, and a file on disk
// outlives whichever build wrote it.
//
// The well-formed record beside it is not decoration: it is what says the
// refusal is about that entry rather than about the book.
func TestAParkBookEntryThatIsNotAWakeIdIsNotRestored(t *testing.T) {
	socket := tempSocket(t)
	writeParkBook(t, socket, []parkedRecord{
		{ID: "build", Name: "alex", Dir: "/tmp/repo"},
		{ID: idAlpha, Name: "sydney", Dir: "/tmp/repo"},
	})

	startDaemonOn(t, socket)
	c := attach(t, socket)
	st := c.status()

	if row := sessionRow(st, "build"); row.State != "" {
		t.Errorf("the daemon restored session %q as %q. Nothing can be resumed under an id Wake did not "+
			"mint, and the reaper's only proof that a process group belongs to a session is that id "+
			"appearing in an argv - which an ordinary word matches by accident", "build", row.State)
	}
	if row := sessionRow(st, idAlpha); row.State != rpc.StateParked {
		t.Errorf("the well-formed record beside it came back as %q, want %q: one unusable entry must not "+
			"cost the rest of the book", row.State, rpc.StateParked)
	}
}

// A parked record is offered back only when its transcript is on disk.
//
// mintedByWake, the only filter before this one, proves the id is well formed
// and nothing more. A book routinely also holds ids that reach no file: a
// session parked before its first turn, or one whose transcript was deleted
// since. The two are indistinguishable here and resolve the same way - a wake of
// either resumes an empty conversation under a live id, which is the branch the
// park path exists to avoid, so neither is offered. transcriptPath is the exact
// lookup a wake would use, reused rather than a slug rebuilt here.
func TestParkedStatusesOffersOnlyRecordsWithATranscript(t *testing.T) {
	// (a) has run: a transcript sits under a projects tree this test owns.
	plantTranscript(t, idAlpha)
	// (b) idBeta reaches no file - nothing is planted for it.
	recs := []parkedRecord{
		{ID: idAlpha, Name: "alex", Dir: "/tmp/repo"},
		{ID: idBeta, Name: "sydney", Dir: "/tmp/repo"},
	}

	got := parkedStatuses(recs)
	if len(got) != 1 || got[0].ID != idAlpha {
		t.Fatalf("parkedStatuses offered %+v, want only %s: a record with no transcript reaches no "+
			"conversation, and offering it back to /resume opens an empty session under a live id",
			got, idAlpha)
	}
}

// Two records under one name is a book somebody edited, and the session is
// worth more than the name.
//
// A display name is never allowed to be the reason somebody cannot start an
// agent (names.go), and it is not allowed to be the reason one cannot come back
// either. Both sessions are restored; one of them draws another name.
func TestTwoParkedSessionsUnderOneNameAreBothOfferedBack(t *testing.T) {
	socket := tempSocket(t)
	writeParkBook(t, socket, []parkedRecord{
		{ID: idAlpha, Name: "alex", Dir: "/tmp/repo"},
		{ID: idBeta, Name: "alex", Dir: "/tmp/repo"},
	})

	startDaemonOn(t, socket)
	c := attach(t, socket)
	st := c.status()

	alpha, beta := sessionRow(st, idAlpha), sessionRow(st, idBeta)
	if alpha.State != rpc.StateParked || beta.State != rpc.StateParked {
		t.Fatalf("a book with two records under one name offers back %q and %q, want both parked: a "+
			"name collision must not cost a session, because the transcript is the thing and the name "+
			"is a word", alpha.State, beta.State)
	}
	// Both keep the recorded name *in the book*, and that is not a collision:
	// nothing is running, nothing is claimed, and the two rows are told apart
	// by the id /resume actually sends. The rename happens at resume, where
	// something is finally holding a name - see restoredName's fallback.
	if alpha.Name != "alex" || beta.Name != "alex" {
		t.Errorf("the book reports the records as %q and %q, want both %q: a park book record is what "+
			"was written down, and nothing has renamed either of them yet", alpha.Name, beta.Name, "alex")
	}
}

// writeParkBook puts a book on disk beside a socket, the way a previous daemon
// would have left one.
//
// It writes through the production writer rather than marshalling here, so a
// test setting up a book cannot disagree with the daemon about the format - and
// TestTheParkBookOnDiskIsTheShapeAnotherBuildWouldRead is what holds that format
// to something other than itself.
//
// It also lays a transcript on disk for each record, because a previous daemon
// only wrote a row for a session that had run - and parkedStatuses now drops a
// record with no transcript (a wake of one opens an empty conversation under a
// live id). A test that wants a record deliberately *without* one builds the
// book directly, the way TestParkedStatusesOffersOnlyRecordsWithATranscript does.
func writeParkBook(t *testing.T, socket string, recs []parkedRecord) {
	t.Helper()

	book := newParkBook(parkBookPath(socket))
	for _, rec := range recs {
		if err := book.add(rec); err != nil {
			t.Fatalf("write the park book: %v", err)
		}
		plantTranscript(t, rec.ID)
	}
}

// `wake status` on a machine with no daemon says what is parked, as well as
// what was left running.
//
// The two lists answer different questions and both are the reason that command
// has a third answer at all. Orphans are 15-30 process trees nobody is holding;
// parked sessions are the opposite - nothing is running, and what is at stake is
// whether the operator can find the ids that reach their transcripts. A fleet
// nobody can see is the liability `wake status` exists for, and until the book
// existed a parked fleet was invisible the moment its daemon exited.
func TestAFleetWithNoDaemonStillReportsWhatIsParked(t *testing.T) {
	fakeClaudeOnPath(t, "")
	socket := tempSocket(t)
	plantTranscript(t, idAlpha) // the session ran; a resumable record has a transcript

	d := startDaemonOn(t, socket)
	c := attach(t, socket)
	before := spawnFor(c, idAlpha, "alex", t.TempDir())
	c.pollState(idAlpha, rpc.StateIdle)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)
	c.close()
	d.stop(t)

	st, err := Status(socket)
	if err != nil {
		t.Fatalf("Status with no daemon listening: %v", err)
	}
	if st.Running {
		t.Fatalf("Status reports a daemon running after the daemon exited: %+v", st)
	}

	row := sessionRow(st, idAlpha)
	switch {
	case row.State == "":
		t.Fatalf("the report names no session %s at all. Nothing is running, so this is the only surface "+
			"that can tell somebody a transcript is still reachable and which id reaches it", idAlpha)
	case row.State != rpc.StateParked:
		t.Errorf("the parked session is reported %q, want %q: it is not an orphan - there is no process "+
			"group behind it and nothing for the reaper to signal", row.State, rpc.StateParked)
	case row.PID != 0:
		t.Errorf("the parked row carries process group %d. FleetOnDisk's other list is read by a reader "+
			"that treats a pgid as something to go and look at, and this one is gone", row.PID)
	case row.Name != before.Name || row.Dir != before.Dir:
		t.Errorf("the parked row came back as %+v, want the name and directory it parked with (%q, %q)",
			row, before.Name, before.Dir)
	}
}

// A restored session has been quiet since it parked, not since the daemon
// started.
//
// agent.parked's own doc comment is why this matters and why there is no
// parkedAt beside it: *"a row's QuietMS already says how long it has been since
// the session spoke"*. That is true of an agent this daemon watched go quiet
// and false of one it read off a file - newAgent starts the clock at now, so a
// session parked yesterday would be reported `quiet 0.0s` on every surface that
// prints it, which is a confident wrong answer rather than a missing one. The
// timestamp in the book is what closes the gap, and it is the only reader that
// field has.
func TestARestoredSessionHasBeenQuietSinceItParkedRatherThanSinceTheDaemonStarted(t *testing.T) {
	socket := tempSocket(t)
	parkedAt := time.Now().Add(-90 * time.Minute)
	writeParkBook(t, socket, []parkedRecord{{ID: idAlpha, Name: "alex", Dir: "/tmp/repo", Parked: parkedAt}})

	startDaemonOn(t, socket)
	c := attach(t, socket)
	row := sessionRow(c.status(), idAlpha)

	if row.State != rpc.StateParked {
		t.Fatalf("the session was not restored (%q), so there is no row to read a clock off", row.State)
	}
	if quiet := time.Duration(row.QuietMS) * time.Millisecond; quiet < time.Hour {
		t.Errorf("the restored session reports %v quiet, want at least the %v since it parked. Starting a "+
			"daemon does not make a parked session recent, and `wake status` prints this number beside a "+
			"row somebody is deciding what to do with", quiet, time.Since(parkedAt).Round(time.Minute))
	}
}

// A book with no timestamp on an entry is a book an older build wrote, and it
// must not read as a session parked in 1970.
//
// `parked` is the one field with no omitempty, so this shape needs somebody to
// have written the file by hand or a build that predates the field - both of
// which are exactly what a durable format has to survive. Two thousand years of
// quiet on a status row is a worse answer than none.
func TestARestoredSessionWithNoRecordedParkTimeDoesNotReportAncientQuiet(t *testing.T) {
	socket := tempSocket(t)
	writeParkBook(t, socket, []parkedRecord{{ID: idAlpha, Name: "alex", Dir: "/tmp/repo"}})

	startDaemonOn(t, socket)
	c := attach(t, socket)
	row := sessionRow(c.status(), idAlpha)

	if row.State != rpc.StateParked {
		t.Fatalf("the session was not restored (%q), so there is no row to read a clock off", row.State)
	}
	if quiet := time.Duration(row.QuietMS) * time.Millisecond; quiet > testTimeout {
		t.Errorf("a record with no park time reports %v quiet. A zero time.Time is 1970 and the subtraction "+
			"is a number nobody can read; an entry that does not say when it parked has to fall back to "+
			"when this daemon picked it up", quiet)
	}
}

// Nothing may be sent to a session with no process, and a restored one is the
// case where that is easiest to get wrong.
//
// The refusal comes from the same place it comes from for an ended session -
// agent.submit selects on a.gone - rather than from a second check, which is
// what parkedAgent's finish(nil) buys. Without it the daemon would accept the
// frame, write it to a stdin that is nil, and every shutdown would wait out the
// whole grace for a session that was never running.
func TestASendToARestoredSessionIsRefusedRatherThanAccepted(t *testing.T) {
	socket := tempSocket(t)
	writeParkBook(t, socket, []parkedRecord{{ID: idAlpha, Name: "alex", Dir: "/tmp/repo"}})

	startDaemonOn(t, socket)
	c := attach(t, socket)
	if row := sessionRow(c.status(), idAlpha); row.State != rpc.StateParked {
		t.Fatalf("the session was not restored (%q), so there is nothing here to send to", row.State)
	}

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "are you there"})
	// Both outcomes, because the defect is the silent one: a frame that is
	// accepted produces no error, and waiting only for the refusal would turn
	// that into a fifteen-second timeout naming a frame that did not arrive.
	f := c.await("the daemon's answer to a message sent to a parked session", func(f rpc.Frame) bool {
		switch {
		case f.Kind == rpc.FrameError && f.SessionID == idAlpha:
			return true
		case f.Kind == rpc.FrameEvent && f.SessionID == idAlpha:
			return true
		default:
			return false
		}
	})
	if f.Kind != rpc.FrameError {
		t.Errorf("a message to a restored parked session produced %s rather than a refusal: there is no "+
			"process behind that row, so the message went nowhere and the operator was told nothing", f.Kind)
	}
}

// A session that ended badly is still offered back after a restart.
//
// This is TestASessionThatEndedBadlyIsStillParked's claim carried across the
// restart, and it is here because the narrowing that beats that test survives
// it: *"only write down a park that ended cleanly"* is a sentence somebody
// writes, and it left every other test in this package green. What it costs is
// the whole feature for the ordinary case - core's WaitDelay turns exit 0 into
// an error whenever anything the agent spawned held stderr past the bound,
// which is routine for an agent running a stdio MCP server.
//
// How a process ended says nothing about the transcript --resume reads. The one
// ending that refuses a park is a kill, and it refuses through the park label
// rather than through this error.
func TestASessionThatEndedBadlyIsStillOfferedBackAfterARestart(t *testing.T) {
	fakeClaudeOnPath(t, "noisyexit")
	socket := tempSocket(t)
	plantTranscript(t, idAlpha) // the session ran; a resumable record has a transcript

	first := startDaemonOn(t, socket)
	c := attach(t, socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	settled := c.awaitSettled(idAlpha)
	if settled.Error == "" {
		t.Fatalf("the parked session reports no error, so this fixture did not end badly and nothing "+
			"below is about a bad ending: %+v", settled)
	}
	c.close()
	first.stop(t)

	startDaemonOn(t, socket)
	back := attach(t, socket)
	if row := sessionRow(back.status(), idAlpha); row.State != rpc.StateParked {
		t.Errorf("a session that ended with %q came back as %q after a restart, want %q. How a process "+
			"ended says nothing about the transcript --resume reads, and the one ending that refuses a "+
			"park is a kill - which refuses through the park label rather than through this error",
			settled.Error, row.State, rpc.StateParked)
	}
}

// A record with no name is restored and drawn one.
//
// The most literal case of names.go's rule: a display name is never allowed to
// be the reason somebody cannot start an agent, and it is not allowed to be the
// reason one cannot come back. `claim("")` is what bare `wake` sends and it
// means "pick one" here for the same reason.
//
// It closes a narrowing that survives everything else - skipping records with
// no name - which would drop exactly the entries a book written by hand or by
// an older build carries.
func TestAParkedRecordWithNoNameIsStillOfferedBack(t *testing.T) {
	socket := tempSocket(t)
	writeParkBook(t, socket, []parkedRecord{{ID: idAlpha, Dir: "/tmp/repo"}})

	startDaemonOn(t, socket)
	c := attach(t, socket)
	row := sessionRow(c.status(), idAlpha)

	if row.State != rpc.StateParked {
		t.Fatalf("a record with no name is offered back as %q, want %q: the transcript is the thing "+
			"and the name is a word", row.State, rpc.StateParked)
	}
	// It gets a name when it is resumed and not before, which is claim("")'s
	// own meaning - "pick one" - moved to the moment something is actually
	// running under it. Until then there is nothing to hold a name.
	if row.ID != idAlpha {
		t.Errorf("the record is offered back under id %q, want %q: the id is the whole of what --resume "+
			"needs, and it is the only thing this record has", row.ID, idAlpha)
	}
}

// A record with no directory is restored and refused a wake, rather than woken
// into whatever directory the daemon happens to be in.
//
// Before the book there was no such record: every agent this daemon starts gets
// a directory, because spawnDir falls back to the daemon's own. A file on disk
// outlives whichever build wrote it, so the refusal is forkSource's, for
// forkSource's reason - claude locates a transcript by the working directory it
// was started in, and resuming in a different one is completely unrecorded.
//
// The row is kept rather than dropped: the id is still true, and after a restart
// it is the only thing anywhere that reaches the transcript.
func TestARestoredRecordWithNoDirectoryIsRefusedAWakeRatherThanResumedInThePwd(t *testing.T) {
	fakeClaudeOnPath(t, "")
	socket := tempSocket(t)
	writeParkBook(t, socket, []parkedRecord{{ID: idAlpha, Name: "alex"}})

	startDaemonOn(t, socket)
	c := attach(t, socket)
	if row := sessionRow(c.status(), idAlpha); row.State != rpc.StateParked {
		t.Fatalf("the record was not restored (%q), so the refusal below would be about an unknown "+
			"session instead", row.State)
	}

	c.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: idAlpha})
	f := c.await("the daemon's answer to a wake of a record with no directory", func(f rpc.Frame) bool {
		if f.Kind == rpc.FrameError && f.SessionID == idAlpha {
			return true
		}
		return f.Kind == rpc.FrameStatusPush && f.Status != nil && stateOf(*f.Status, idAlpha) != "" &&
			stateOf(*f.Status, idAlpha) != rpc.StateParked
	})
	if f.Kind != rpc.FrameError {
		t.Fatalf("a record with no directory was woken. It resumed in whichever directory the terminal " +
			"that forked this daemon happened to be in, which opens an empty session under the right id " +
			"with nothing saying the history is missing")
	}
	if !strings.Contains(f.Text, "where session") {
		t.Errorf("the refusal is %q and does not say the daemon has no idea where that session ran, which "+
			"is the only thing an operator could act on", f.Text)
	}
	if row := sessionRow(c.status(), idAlpha); row.State != rpc.StateParked {
		t.Errorf("a refused wake left session %s in state %q, want parked: the id is still the only thing "+
			"that reaches the transcript", idAlpha, row.State)
	}
}

// A stop forgets a fleet this daemon inherited, not only one it parked itself.
//
// The distinction is invisible from inside shutdown and it is the whole point of
// a restore: after a restart every parked session in the fleet came off the
// book, so a clear that only forgot what *this* daemon watched go quiet would
// forget nothing at all. `wake stop` after a restart would silently leave the
// fleet to be offered back by the next `wake`.
func TestAStopForgetsAFleetItInheritedRatherThanOnlyOneItParked(t *testing.T) {
	socket := tempSocket(t)
	writeParkBook(t, socket, []parkedRecord{
		{ID: idAlpha, Name: "alex", Dir: "/tmp/repo", Parked: time.Now().Add(-48 * time.Hour)},
	})

	d := startDaemonOn(t, socket)
	c := attach(t, socket)
	if row := sessionRow(c.status(), idAlpha); row.State != rpc.StateParked {
		t.Fatalf("the record was not restored (%q), so there is no inherited fleet here to forget", row.State)
	}

	c.send(rpc.Frame{Kind: rpc.FrameQuit})
	d.waitForExit(t)

	if recs := loadParkBook(parkBookPath(socket)); len(recs) != 0 {
		t.Errorf("the park book still holds %+v after the quit verb. Every parked session in a restarted "+
			"fleet came off this file, so a clear that only forgot what this daemon parked itself would "+
			"forget nothing at all", recs)
	}
}

// FleetOnDisk reports a parked record that carries nothing but its id.
//
// The report is assembled straight off the file, so it meets whatever a
// previous build wrote - and a row that names a session with no name and no
// label is still the id that reaches a transcript, which is the only thing on
// that surface anybody can act on. Dropping it would be a narrowing keyed on a
// display field, which every other test here is blind to because every other
// test spawns through a daemon that fills both in.
func TestAFleetWithNoDaemonReportsAParkedRecordThatCarriesOnlyItsId(t *testing.T) {
	socket := tempSocket(t)
	writeParkBook(t, socket, []parkedRecord{{ID: idAlpha}})

	st, err := Status(socket)
	if err != nil {
		t.Fatalf("Status with no daemon listening: %v", err)
	}
	if row := sessionRow(st, idAlpha); row.State != rpc.StateParked {
		t.Errorf("a record carrying only its id is reported %q, want %q. The name and the label are "+
			"display; the id is the whole of what --resume needs, and this is the surface that prints it",
			row.State, rpc.StateParked)
	}
}

// Books written by builds that are not this one.
//
// The format is pinned from both directions already: `openroom_test.go`
// hand-writes the JSON and has a real daemon in another process read it back,
// and the chain test decodes this daemon's own output into `map[string]any`
// rather than into the writer's struct, so a renamed field is visible. What
// neither reaches is a **schema change** - a book written before a field
// existed, or after one this build has never heard of.
//
// That is the shape a park book actually meets in the wild: it outlives the
// binary that wrote it by design, which is the whole reason it exists. An
// operator who parks a fleet, upgrades Wake, and starts it again is running
// exactly this test.
//
// The property is one sentence: **a book from another build loses only what it
// does not carry.** Never the whole file, and never a record - dropping a fleet
// because one field moved is the failure mode this is written against, and
// `loadParkBook` returning nil is precisely how that would look.
func TestAParkBookFromAnotherBuildLosesOnlyWhatItDoesNotCarry(t *testing.T) {
	const id = "a11a0000-0000-4000-8000-00000000a11a"

	for _, tc := range []struct {
		what    string
		written string
		want    parkedRecord
	}{
		{
			what:    "a build before `parked` existed",
			written: `[{"id":"` + id + `","name":"alex","label":"dev-5748","dir":"/tmp/repo"}]`,
			// The zero time rather than nothing: noteQuietSince is its only
			// reader and already refuses to report ancient quiet from one.
			want: parkedRecord{ID: id, Name: "alex", Label: "dev-5748", Dir: "/tmp/repo"},
		},
		{
			what:    "a build before `label` existed",
			written: `[{"id":"` + id + `","name":"alex","dir":"/tmp/repo"}]`,
			want:    parkedRecord{ID: id, Name: "alex", Dir: "/tmp/repo"},
		},
		{
			what: "a build before `dir` existed",
			// The one that matters most, and it is not hypothetical: the
			// directory was added *because* resuming anywhere else is
			// unrecorded. Such a record is restored and refused a wake with a
			// sentence - which is only possible if it is read at all.
			written: `[{"id":"` + id + `","name":"alex","label":"dev-5748"}]`,
			want:    parkedRecord{ID: id, Name: "alex", Label: "dev-5748"},
		},
		{
			what: "a build that carries a field this one has never heard of",
			written: `[{"id":"` + id + `","name":"alex","label":"dev-5748","dir":"/tmp/repo",` +
				`"worktree":"/tmp/wt","budget":12.5}]`,
			want: parkedRecord{ID: id, Name: "alex", Label: "dev-5748", Dir: "/tmp/repo"},
		},
	} {
		t.Run(tc.what, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), parkBookName)
			if err := os.WriteFile(path, []byte(tc.written), parkBookPerm); err != nil {
				t.Fatalf("write the book: %v", err)
			}
			got := loadParkBook(path)
			if len(got) != 1 {
				t.Fatalf("%s decoded to %d records, want 1. A book from another build that reads as "+
					"empty is a parked fleet nothing anywhere reports losing", tc.what, len(got))
			}
			if got[0].ID != tc.want.ID || got[0].Name != tc.want.Name ||
				got[0].Label != tc.want.Label || got[0].Dir != tc.want.Dir {
				t.Errorf("%s decoded to %+v, want %+v", tc.what, got[0], tc.want)
			}
		})
	}

	// And a book with no records at all is not an error - a fleet where nothing
	// was parked writes exactly this.
	t.Run("a book with nothing in it", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), parkBookName)
		if err := os.WriteFile(path, []byte(`[]`), parkBookPerm); err != nil {
			t.Fatalf("write the book: %v", err)
		}
		if got := loadParkBook(path); len(got) != 0 {
			t.Errorf("an empty book decoded to %+v, want nothing", got)
		}
	})
}

// Effort is the one piece of configuration the book carries, and it has to
// survive the round trip on disk like every other field: a level the operator
// chose and a wake dropped is a silent downgrade, which is the whole reason it
// is written down. Asserted through the on-disk bytes rather than the struct,
// for the reason the test above this one gives.
func TestAParkedSessionKeepsTheEffortItRanAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), parkBookName)
	book := newParkBook(path)
	if err := book.add(parkedRecord{ID: idAlpha, Name: "alex", Dir: "/tmp/repo", Effort: core.EffortMax}); err != nil {
		t.Fatalf("add: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the park book back: %v", err)
	}
	if !strings.Contains(string(data), `"effort":"max"`) {
		t.Errorf("the effort is not on disk under the key another build would read:\n%s", data)
	}

	back := newParkBook(path)
	rows := back.records()
	if len(rows) != 1 || rows[0].Effort != core.EffortMax {
		t.Fatalf("the effort did not survive the round trip: %+v", rows)
	}
}

// A session Wake chose no effort for writes no key at all, so a book from this
// build is byte-identical to one from before the field existed.
func TestASessionWithNoEffortWritesNoEffort(t *testing.T) {
	path := filepath.Join(t.TempDir(), parkBookName)
	if err := newParkBook(path).add(parkedRecord{ID: idAlpha, Name: "alex", Dir: "/tmp/repo"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(data), "effort") {
		t.Errorf("a session with no chosen effort wrote the key anyway:\n%s", data)
	}
}

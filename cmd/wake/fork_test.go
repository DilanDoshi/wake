package main

// `wake fork <who>`: which session it branches, the frame it writes, and the
// words it takes.

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// idGammaCLI is the id this package's fork tests mint. It is a UUID because
// maySpawn refuses anything else, for the reason the reaper needs it to be one.
const idGammaCLI = "c33c0000-0000-4000-8000-00000000c33c"

// forkableFleet has one live agent and one that has finished. Both are
// forkable: the parent's transcript is on disk whether or not a process holds
// it, and the recordings forked exited parents throughout.
func forkableFleet() rpc.Status {
	return rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "sydney", Label: "dev-5748", State: rpc.StateIdle},
		{ID: idBeta, Name: "marco", Label: "main", State: rpc.StateEnded},
	}}
}

// liveSession refuses an ended session, and it is right to: there is nothing
// to attach to. Fork is the one verb for which an ended session is a perfectly
// good answer, so it resolves through the same matcher and applies a different
// rule on top rather than duplicating the search.
func TestForkResolvesAParentThatHasEnded(t *testing.T) {
	d := startFakeDaemon(t, 0, forkableFleet())

	got, err := forkParent(d.socket, "marco")
	if err != nil {
		t.Fatalf("forkParent on an ended session: %v", err)
	}
	if got.ID != idBeta {
		t.Errorf("forking marco reached %s, want %s", got.ID, idBeta)
	}
}

// The frame is where the two ids stop being interchangeable. SessionID is the
// id the *new* agent will run under and ParentID is the conversation it
// inherits, and a build that swapped them would ask the daemon to fork a
// session into itself - which core refuses, but only after a round trip and
// with a message about ids rather than about what was pressed.
func TestForkAsksForAFreshIdAndNamesTheParent(t *testing.T) {
	mine, theirs := net.Pipe()
	t.Cleanup(func() { _ = mine.Close(); _ = theirs.Close() })

	frames, errs := rpc.ReadFrames(theirs)
	go func() {
		if err := requestFork(mine, idGammaCLI, idAlpha, "twin"); err != nil {
			t.Errorf("requestFork: %v", err)
		}
	}()

	f := <-frames
	if f.Kind != rpc.FrameFork {
		t.Fatalf("wrote a %q frame, want %q", f.Kind, rpc.FrameFork)
	}
	if f.SessionID != idGammaCLI {
		t.Errorf("the frame's SessionID is %q, want the fork's own id %q", f.SessionID, idGammaCLI)
	}
	if f.ParentID != idAlpha {
		t.Errorf("the frame's ParentID is %q, want the parent %q", f.ParentID, idAlpha)
	}
	if f.Text != "twin" {
		t.Errorf("the frame's Text is %q, want the requested name", f.Text)
	}
	if f.Dir != "" {
		t.Errorf("the frame carries Dir %q: a fork runs where its parent ran, and the client does not get to choose", f.Dir)
	}

	_ = mine.Close()
	for range frames {
	}
	<-errs
}

// The verb is checked before the arity, so a typo is reported as a typo -
// and `wake fork` with nothing after it is not "fork whichever one is
// running", because that guess is wrong the moment there are two.
//
// Both assert the fork arm's *own* sentence rather than only that something was
// refused, and that is what makes the pair falsifiable. Deleting `case cmdFork`
// from checkArity drops both invocations into the `default` arm, which refuses
// four words with "takes no arguments" - so an assertion of `err != nil` alone
// passes over a build where the verb has no arity rule at all, and the one-word
// case reaches `args[1]` on a one-element slice and panics.
//
// forkArity is the half of that message no other arm spells: `wake attach`
// says "one session id or name" too, and matching on that would be satisfied by
// the wrong verb's refusal.
func TestForkNeedsToBeToldWhichSession(t *testing.T) {
	// Asked of checkArity directly, and that is not a shortcut - it is what
	// makes this test kill its mutant by *asserting* rather than by crashing.
	// run's dispatch reads args[1] with no guard of its own, so with the fork
	// arm deleted from checkArity the one-word invocation reaches a one-element
	// slice and panics, which takes the package down and tells whoever ran the
	// battery nothing about which assertion fired. The four-word case below is
	// what proves run reaches checkArity at all; this one is about the rule.
	if err := checkArity([]string{"fork"}); err == nil {
		t.Fatal("`wake fork` with no session passed the arity check: that guess is wrong the moment there are two")
	} else if !strings.Contains(err.Error(), forkArity) {
		t.Fatalf("`wake fork` with no session was refused with %q, which is not the fork arm's own refusal", err)
	}

	// So resolving the socket cannot create ~/.wake on whoever runs this.
	t.Setenv(daemon.SocketEnv, tempSocket(t))

	var b strings.Builder
	err := run([]string{"fork"}, &b)
	if err == nil {
		t.Fatal("`wake fork` with no session was accepted")
	}
	if !strings.Contains(err.Error(), forkArity) {
		t.Errorf("`wake fork` with no session was refused with %q, which is not the fork arm's own refusal", err)
	}
}

func TestForkTakesAtMostAParentAndAName(t *testing.T) {
	t.Setenv(daemon.SocketEnv, tempSocket(t))

	var b strings.Builder
	err := run([]string{"fork", "sydney", "twin", "extra"}, &b)
	if err == nil {
		t.Fatal("`wake fork` accepted four words")
	}
	if !strings.Contains(err.Error(), forkArity) {
		t.Errorf("`wake fork` with four words was refused with %q, which is not the fork arm's own refusal", err)
	}
}

// forkArity is the part of `wake fork`'s arity refusal that belongs to no other
// verb.
const forkArity = "optionally a name for the fork"

// The sharpest thing in this verb, and the one failure worth a test of its own:
// the wait after a fork frame is keyed on the **fork's** id.
//
// awaitSpawn matches a refusal on `f.SessionID == sessionID` and has no
// deadline by design - both outcomes are events, so a timer here would be a
// guess about how long forking a claude process takes on somebody else's
// machine. The daemon addresses every one of its fork refusals to the fork's
// own id for exactly that reason. Waiting on the parent's would therefore not
// fail: it would leave `wake fork` parked on a blank terminal forever with
// nothing printed, which is the worst shape this failure has, because it looks
// exactly like a daemon that is thinking.
//
// Three assertions, and the last two are what stop the first being satisfied by
// accident. The fake echoes back whichever id arrived on the frame, so a build
// that put the *parent's* id in both places - on the frame and in the wait -
// would match itself and return the refusal on time. So the frame is read
// afterwards and required to name a fresh id in SessionID and the parent in
// ParentID, which is the same pair TestForkAsksForAFreshIdAndNamesTheParent
// pins about requestFork, asserted here about what forkSession wires into it.
func TestForkWaitsOnTheForksOwnIdRatherThanItsParents(t *testing.T) {
	const why = "sydney is in the middle of a turn. Fork it when the turn ends, or stop the turn first."
	d := startForkRefusingDaemon(t, forkableFleet(), why)

	// Off the test goroutine so a wait on the wrong id is a failure rather than
	// a package that hangs until the whole suite times out.
	done := make(chan error, 1)
	go func() { done <- forkSession(d.socket, "sydney", "twin", io.Discard) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("`wake fork` reported success against a daemon that refused the fork")
		}
		if !strings.Contains(err.Error(), why) {
			t.Errorf("the refusal reached the caller as %q, want the daemon's own sentence: it is the half that says when to try again", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("`wake fork` never returned against a daemon that refused it immediately. " +
			"The refusal is addressed to the fork's id, so a client waiting on the parent's " +
			"never matches it - and that wait has no deadline, so this is a `wake fork` that " +
			"hangs on a blank terminal rather than one that fails")
	}

	sent := d.lastFork()
	if sent.SessionID == "" {
		t.Fatal("no fork frame reached the daemon")
	}
	if sent.SessionID == idAlpha {
		t.Errorf("the frame asks the daemon to run the fork under the parent's own id %s: "+
			"core refuses that shape, but only after a round trip", idAlpha)
	}
	if sent.ParentID != idAlpha {
		t.Errorf("the frame's ParentID is %q, want sydney's id %q - the fork would inherit nothing", sent.ParentID, idAlpha)
	}
}

// `wake status` is where sessions are *listed*, which is where the labelled
// form belongs and where ids are already printed. So it is where a fork says
// what it came from - by name while the report still holds the parent, and by
// the same eight characters the id column prints once it does not.
func TestStatusSaysWhatASessionWasForkedFrom(t *testing.T) {
	st := rpc.Status{Running: true, PID: 42, Socket: "/tmp/wake.sock", Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "alex", State: rpc.StateIdle},
		{ID: idBeta, Name: "sydney", State: rpc.StateIdle, ParentID: idAlpha},
	}}

	out := formatStatus(st)
	if !strings.Contains(out, "forked from alex") {
		t.Errorf("the listing does not say sydney was forked from alex:\n%s", out)
	}
}

// The parent falls out of the report after recentEndings rotations, and the
// fork is still a fork. The short id is what `wake status` prints in its own id
// column and what `wake attach` resolves, so it is the honest fallback - unlike
// the DM header, where a hex string is exactly the thing names exist to
// replace.
func TestStatusFallsBackToTheParentsShortIdWhenTheReportHasLostIt(t *testing.T) {
	st := rpc.Status{Running: true, PID: 42, Socket: "/tmp/wake.sock", Sessions: []rpc.SessionStatus{
		{ID: idBeta, Name: "sydney", State: rpc.StateIdle, ParentID: idAlpha},
	}}

	out := formatStatus(st)
	if !strings.Contains(out, "forked from "+shortID(idAlpha)) {
		t.Errorf("the listing does not fall back to the parent's short id:\n%s", out)
	}
}

// The verb reaches its own command, and the name reaches the frame.
//
// Both halves are one test because the dispatch arm is where they meet:
// `forkSession(socket, args[1], forkName(args), out)` reads the argv twice, and
// nothing else in this package runs it. docs/notes/decisions.md names exactly
// this hole - a `case cmdStop:` was mutated away with the whole suite green,
// because the stop tests called stopFleet directly and no test asserted that
// any verb reached its own command. `wake fork` arrived with two ways to be
// wrong here: the wrong index for the parent, and a name that is read but never
// sent.
//
// It is asserted against the frame the daemon received rather than against the
// error, because the name is the half that is invisible from this side: a
// forkName that always returned "" would produce exactly the same refusal and
// leave the daemon drawing a pooled name for a fork somebody named.
func TestForkReachesItsOwnCommandAndCarriesTheNameItWasGiven(t *testing.T) {
	const why = "sydney is in the middle of a turn. Fork it when the turn ends, or stop the turn first."

	for _, tc := range []struct {
		what string
		args []string
		want string
	}{
		{what: "with a name", args: []string{cmdFork, "sydney", "twin"}, want: "twin"},
		// Nothing, which the daemon reads as "draw one" - the same thing bare
		// `wake` sends, which is what makes the optional word optional rather
		// than a second verb.
		//
		// **This case is load-bearing beyond its name, so do not drop it.** It is
		// the only test in the tree that reaches requestFork with an empty
		// requested name, and requestFork is closed by fixtures alone - neither
		// static guard applies to it, because it assigns nothing and has no row
		// to read. `if requested == "" { sessionID = parentID }` inside it
		// survives every other test in this package and is killed only here, by
		// the daemon's refusal arriving addressed to an id nothing is waiting on
		// and this subtest hanging to its timeout.
		{what: "without one", args: []string{cmdFork, "sydney"}, want: ""},
	} {
		t.Run(tc.what, func(t *testing.T) {
			d := startForkRefusingDaemon(t, forkableFleet(), why)
			t.Setenv(daemon.SocketEnv, d.socket)
			refuseFork(t, d, tc.args, why)

			sent := d.lastFork()
			if sent.ParentID != idAlpha {
				t.Errorf("the frame's ParentID is %q, want sydney's id %q: the dispatch arm read the wrong word", sent.ParentID, idAlpha)
			}
			if sent.Text != tc.want {
				t.Errorf("the frame asks for the name %q, want %q", sent.Text, tc.want)
			}
		})
	}
}

// A name is never an address, and this listing is where that bites.
//
// The pool releases a name when its session ends, while the ending stays in the
// report for up to recentEndings rotations - so a fork's parent can end, a new
// session can draw the same name, and both rows sit in one report under it.
// `forked from sydney` would then name a live agent that is not the parent, and
// `wake attach sydney` reaches that live one, because pickOne prefers live over
// ended. There would be no way from the listing back to the actual parent.
//
// So a reused name is not used at all and the row falls through to the short id,
// which is the unambiguous form the id column already prints.
func TestStatusWillNotNameAParentByANameSomethingElseNowHolds(t *testing.T) {
	st := rpc.Status{Running: true, PID: 42, Socket: "/tmp/wake.sock", Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "sydney", State: rpc.StateEnded},
		{ID: idGammaCLI, Name: "sydney", State: rpc.StateIdle},
		{ID: idBeta, Name: "marco", State: rpc.StateIdle, ParentID: idAlpha},
	}}

	out := formatStatus(st)
	if strings.Contains(out, "forked from sydney") {
		t.Errorf("the listing names marco's parent by a name a *different* live session now holds, "+
			"and `wake attach sydney` reaches that one:\n%s", out)
	}
	if !strings.Contains(out, "forked from "+shortID(idAlpha)) {
		t.Errorf("the listing does not fall back to the parent's short id:\n%s", out)
	}
}

// refuseFork runs one invocation against a daemon that refuses every fork, and
// insists the refusal came back rather than the wait hanging on it.
//
// Off the test goroutine, because the failure this is watching for is a wait on
// an id nothing will ever address - which is not a wrong answer but no answer,
// and a package that hangs reports nothing at all.
func refuseFork(t *testing.T, d *fakeDaemon, args []string, why string) {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- run(args, io.Discard) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("`wake fork` reported success against a daemon that refused the fork")
		}
		if !strings.Contains(err.Error(), why) {
			t.Errorf("`wake fork` failed with %q rather than the daemon's refusal, so it did not reach forkSession", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("`wake fork` never returned")
	}
}

// `wake fork` says a fork is a snapshot, because there are two fork surfaces and
// the promise is about both.
//
// It was true for ⌃F and false here, while `forkRefusal`'s comment, `v1_goals.md`
// and `deferred.md` all asserted it without qualification - the fifth appearance
// of a claim that had been implemented once. This verb ends in `converse`, so
// the notice row of the DM it opens is the only surface it has, and that is the
// same row ⌃F uses.
func TestAConfirmedForkSaysItIsASnapshot(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)

	fleet := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "sydney", State: rpc.StateIdle},
		{ID: idGammaCLI, Name: "twin", State: rpc.StateIdle, ParentID: idAlpha},
	}}
	announceFork(fleet.Sessions[1], &fleet)

	n, ok := notice.Latest()
	if !ok {
		t.Fatal("a confirmed fork said nothing: the operator is now one keystroke from typing to the parent, which is the case this sentence exists for")
	}
	if want := "nothing @sydney does next reaches it"; !strings.Contains(n.Text, want) {
		t.Errorf("the confirmation reads %q, want it to contain %q", n.Text, want)
	}
	if !strings.Contains(n.Text, agentPrefix+"twin") {
		t.Errorf("the confirmation reads %q and does not name the fork", n.Text)
	}
}

// And the same reused-name rule the listing and the DM header both keep: a name
// two rows hold is not an address, so it is not printed as one. The listing
// falls back to the short id because that column prints ids; a sentence about
// what @sydney *does next* has no such column, so it drops the name and keeps
// the claim.
func TestAConfirmedForkWillNotNameAParentByANameSomethingElseNowHolds(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)

	fleet := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "sydney", State: rpc.StateEnded},
		{ID: idBeta, Name: "sydney", State: rpc.StateIdle},
		{ID: idGammaCLI, Name: "twin", State: rpc.StateIdle, ParentID: idAlpha},
	}}
	announceFork(fleet.Sessions[2], &fleet)

	n, _ := notice.Latest()
	if strings.Contains(n.Text, agentPrefix+"sydney") {
		t.Errorf("the confirmation reads %q and names @sydney, which now resolves to a live session that is not the parent", n.Text)
	}
	if !strings.Contains(n.Text, "is a fork") {
		t.Errorf("the confirmation reads %q: losing the parent's name lost the snapshot promise with it", n.Text)
	}
}

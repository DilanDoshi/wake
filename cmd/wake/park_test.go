package main

// What the shell verbs do with a session that has been parked.
//
// Four surfaces and one rule underneath all of them: a parked session is still
// a session - it keeps its id, its name, its label and its directory - and it
// has no process. Every verb here is one that acts on a process, so every one
// of them has to say so rather than treat the surviving row as evidence of a
// living agent.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func parkedFleet() rpc.Status {
	return rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "sydney", Label: "dev-5748", State: rpc.StateParked},
		{ID: idBeta, Name: "marco", Label: "main", State: rpc.StateIdle},
	}}
}

// `wake attach` refuses a parked session, and names something that works.
//
// The refusal is the whole of it: attaching would open a conversation whose
// composer swallows every keystroke, which is the failure reattach exists to
// prevent and the reason liveSession refuses an ended session too.
//
// What separates it from ended is what the sentence may offer, and that changed
// under this test rather than in it. When it was written, waking was not built,
// so the sentence carried `wake fork` alone and this guard FORBADE the word
// "resume" - correctly, then.
//
// Task 3 built unpark, Task 6 built /resume and Task 7 gave FramePark a sender,
// and the guard went on enforcing its expired premise: writing the correct
// refusal became a build failure, and an operator who wanted their conversation
// back was pointed at the verb that makes a SECOND session, leaves the original
// parked, spends another name and writes another row.
//
// So it is inverted: require the resume route rather than forbid it. There is
// still no `wake resume` SHELL verb, which is why the sentence names the in-TUI
// command - `wake`, then /resume - and still offers fork as the other thing you
// might have meant.
//
// The general shape is worth more than this instance: a guard can be perfectly
// able to fail and still be about the wrong thing, once the premise it was
// written under stops holding. Rungs 1-6 in decisions.md all ask whether an
// assertion CAN fail. None of them asks whether it is still about the right
// thing.
func TestAttachRefusesAParkedSessionAndNamesAVerbThatWorks(t *testing.T) {
	d := startFakeDaemon(t, 0, parkedFleet())

	_, _, err := liveSession(d.socket, "sydney")
	if err == nil {
		t.Fatal("liveSession opened a parked session: its process has gone, so the composer would swallow every keystroke")
	}
	why := err.Error()
	switch {
	case !strings.Contains(why, "parked"):
		t.Errorf("the refusal is %q and does not say the session is parked - which is the one word that "+
			"says the context is still there", why)
	case !strings.Contains(why, "/resume"):
		t.Errorf("the refusal is %q and does not name /resume - which is the built route back to THIS "+
			"conversation, and the whole reason park exists", why)
	case !strings.Contains(why, "fork"):
		t.Errorf("the refusal is %q and does not offer fork - the other thing an operator might have "+
			"meant, and the one that makes a second session instead of reviving this one", why)
	}
	// **Derived rather than spelled**, which is the half the inversion did not
	// pay. This used to forbid the literal `wake resume`, which is a claim about
	// cmd/wake's verb set made by a test that read none of it - the same shape
	// as the premise that expired above, one level in. It now holds every
	// `wake <verb>` this sentence names to the verbs this command actually
	// dispatches, so the day somebody adds a shell resume the refusal is free to
	// name it, and the day one is renamed the refusal fails rather than lying.
	// docs/notes/deferred.md carried this as the half still owed.
	if requireEveryVerbNamedIsDispatched(t, "wake attach's refusal of a parked session", why, quotedShellVerb) == 0 {
		t.Errorf("the refusal is %q and names no `wake <verb>` at all: it offers fork as the other thing "+
			"an operator might have meant, so a version naming none has stopped offering it", why)
	}
}

// A parked session beats a remembered ending that shares its name.
//
// **This is the collision that can actually happen**, and getting to it took
// correcting the one this test used to drive. A live agent and a parked one
// cannot share a name at all: completePark never releases it and
// nameRegistry.claim refuses a name in `taken`, which the daemon-side test
// asserts directly - so a fixture with both was a verdict over an input no
// producer can make, which is this repository's own rung 4 arriving inside the
// change that celebrated catching it.
//
// What is reachable is this: a name goes back to the pool when a session ends,
// the ending stays in the report for a while, a later spawn takes the name, and
// that one is parked. Two rows, one word.
//
// It resolves to the parked one, by pickOne's own argument one tier down. A
// remembered ending is not something anybody can act on - it is in the report so
// a refusal can say "that one ended" - while a parked session can be forked
// today and woken later. Refusing the pair as ambiguous would be the failure
// pickOne exists to prevent: something unambiguous to the person typing it,
// refused because of a row they cannot use, over a listing that shows neither.
func TestAParkedSessionBeatsARememberedEndingThatSharesItsName(t *testing.T) {
	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "sydney", State: rpc.StateEnded},
		{ID: idBeta, Name: "sydney", State: rpc.StateParked},
	}}

	got, err := matchSession(st, "sydney")
	if err != nil {
		t.Fatalf("matchSession refused a name held by one parked session and one remembered ending: %v\n"+
			"An ending is not something anybody can act on, so this is not an ambiguity - and the listing "+
			"the refusal prints contains neither row", err)
	}
	if got.ID != idBeta {
		t.Errorf("matchSession reached %s, want the parked session %s: an ending is in the report so a "+
			"refusal can name it, and a parked session is one somebody can still fork and later wake",
			got.ID, idBeta)
	}
}

// And a live agent still beats both, which is the tier above it.
func TestALiveAgentWinsANameAParkedOneAlsoHolds(t *testing.T) {
	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "sydney", State: rpc.StateParked},
		{ID: idBeta, Name: "sydney", State: rpc.StateIdle},
	}}

	got, err := matchSession(st, "sydney")
	if err != nil {
		t.Fatalf("matchSession refused a name held by one live agent and one parked one: %v", err)
	}
	if got.ID != idBeta {
		t.Errorf("matchSession reached %s, want the live agent %s: only one of the two can be attached to",
			got.ID, idBeta)
	}
}

// The listing under a refusal says "running now", so it may not list a session
// that is not running.
//
// It is what somebody reads to pick something to attach to. A parked row there
// is an offer the very next command refuses, and `wake status` is the surface
// that lists every session whatever its state.
func TestTheListingOfWhatIsRunningLeavesOutAParkedSession(t *testing.T) {
	got := runningSessions(parkedFleet())
	if strings.Contains(got, "sydney") {
		t.Errorf("the running list offers a parked session:\n%s\nEverything on it is something attach "+
			"accepts, and attach refuses this one", got)
	}
	if !strings.Contains(got, "marco") {
		t.Fatalf("the running list lost the live agent too, so this test is asserting nothing:\n%s", got)
	}
}

// And the detach line does not count one as running.
//
// `wake stop`'s exit code is built on this count, so a parked session counted
// here is a fleet that reports as still up with nothing running in it - which
// is the one thing wake stop exists to be able to deny.
func TestTheDetachLineDoesNotCountAParkedSessionAsRunning(t *testing.T) {
	if n := runningCount(parkedFleet()); n != 1 {
		t.Errorf("runningCount = %d over one parked and one idle session, want 1: a parked session has no "+
			"process, and this count is what `wake stop` reports on", n)
	}
}

// `wake stop` with nothing but parked sessions is not a fleet left behind.
//
// reportNothingToStop covers the two ways there is no daemon to talk to, and
// they differ in exactly one thing: whether anything is still running. Its
// count was len(st.Sessions), which daemon.FleetOnDisk now fills with parked rows as
// well as orphaned ones - so a machine whose whole fleet was parked would exit
// **non-zero** saying those sessions were "left behind with nothing holding
// them". Nothing is holding them because nothing is running, which is the
// normal end of a park rather than the failure that sentence names, and
// `wake stop && rm -rf worktrees` is what agreeing costs.
func TestStopWithOnlyParkedSessionsIsNotAFleetLeftBehind(t *testing.T) {
	var out bytes.Buffer
	err := reportNothingToStop(rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "sydney", Label: "dev-5748", State: rpc.StateParked},
	}}, &out)

	if err != nil {
		t.Errorf("`wake stop` failed with %v over a fleet that is only parked. Nothing is running, so the "+
			"caller's postcondition already holds and the exit code is what a script reads", err)
	}
	got := out.String()
	switch {
	case !strings.Contains(got, "1 agent parked"):
		t.Errorf("the report is %q and does not say a session is parked. It is the only surface that can "+
			"tell somebody a transcript is still reachable with no daemon to ask", got)
	case strings.Contains(got, "left"):
		t.Errorf("the report is %q and says something was left behind. That sentence is about process "+
			"trees nobody is holding, and a parked session has no process at all", got)
	}
}

// And an orphan beside a parked session is still a failure, counted correctly.
//
// The two rows come back in one report from daemon.FleetOnDisk and mean opposite
// things. Getting this wrong in the other direction - counting only the parked
// one - would exit zero over a live process tree nobody is holding, which is
// the state `wake stop` exists to be able to deny.
func TestStopStillFailsOverAnOrphanBesideAParkedSession(t *testing.T) {
	var out bytes.Buffer
	err := reportNothingToStop(rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "sydney", State: rpc.StateParked},
		{ID: idBeta, Name: "marco", State: rpc.StateOrphaned, PID: 4242},
	}}, &out)

	if err == nil {
		t.Fatal("`wake stop` exited zero with an orphaned agent still running: the fleet is not down, and " +
			"a script that ran this before shutting a laptop finds out from the exit code")
	}
	if !strings.Contains(err.Error(), "1 agent") {
		t.Errorf("the failure is %q and does not count exactly the one agent that is running: the parked "+
			"row beside it has no process", err)
	}
}

// A `wake stop` that reaches no daemon reads daemon.FleetOnDisk, and a wedged ps
// can drop every live record from it - leaving runningCount zero over a fleet
// that is still up. ProbeIncomplete is the fail-closed signal that says the
// sweep never finished, and reportNothingToStop must refuse to print "No daemon
// is running." and exit zero when it is set: runningCount is an under-count
// nobody may trust here, and `wake stop && rm -rf worktrees` is what trusting it
// costs.
func TestReportNothingToStopWillNotConfirmAnIncompleteSweep(t *testing.T) {
	var out bytes.Buffer
	err := reportNothingToStop(rpc.Status{ProbeIncomplete: true}, &out)

	if err == nil {
		t.Fatal("`wake stop` exited zero on a sweep the liveness budget cut short: runningCount is an " +
			"under-count when the probe timed out, and a script that ran this before an rm reads the exit code")
	}
	if strings.Contains(out.String(), "No daemon is running.") {
		t.Errorf("the report is %q and claims no daemon is running: a wedged ps that dropped every record "+
			"cannot tell an empty machine from a full one", out.String())
	}
}

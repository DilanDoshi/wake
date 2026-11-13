package ui

// The bug this file pins: a subagent's forwarded frames used to reach nowhere
// but DM.subs, which App.observe only fills for a conversation already in
// App.dms. Opening a dispatch under an agent nobody had watched live drew an
// empty transcript - indistinguishable from the wire's own floor for a
// dispatch that truly forwarded nothing (dm_subagent_test.go's
// TestOpeningADispatchWithNothingInItIsEmptyRatherThanTheConversation).

import (
	"slices"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// A forwarded frame folds into the fleet's own backlog for a session nobody
// has opened, the same way foldTask already does for the row that names it.
func TestAFleetFoldsASubagentsSpeechForASessionNobodyHasOpened(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(spoke(subDispatch, subSaid), "sess-1")

	got := f.SubBacklog("sess-1", subDispatch)
	if len(got) != 1 || got[0].Text != subSaid {
		t.Fatalf("SubBacklog = %+v, want the one forwarded frame", got)
	}
}

// Each session keeps its own backlog, the way each keeps its own task rows -
// a fleet-wide fold would let one agent's subagent answer for another's.
func TestOneSessionsSubagentBacklogDoesNotReachAnother(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(spoke(subDispatch, subSaid), "sess-1")

	if got := f.SubBacklog("sess-2", subDispatch); got != nil {
		t.Errorf("sess-2 answered for a dispatch it never forwarded: %+v", got)
	}
}

// A receipt and an ask are not forwarded speech (forwardedTo's own rule,
// dm.go) and must not take a slot in the backlog either - the fleet's fold has
// to agree with the live one about what a dispatch "said".
func TestTheFleetBacklogSkipsWhatIsNotForwardedSpeech(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(core.Event{
		Kind: core.KindToolResult, Text: "the report",
		Subagent: &core.Subagent{Dispatch: subDispatch, Agent: "a1", Result: core.SubagentFinished},
	}, "sess-1")
	f, _ = f.Observe(core.Event{
		Kind:     core.KindPermissionRequest,
		Subagent: &core.Subagent{Agent: "ab1b72d53680ae187"},
	}, "sess-1")

	if got := f.SubBacklog("sess-1", subDispatch); got != nil {
		t.Errorf("a receipt reached the backlog: %+v", got)
	}
}

// A dispatch nothing has forwarded answers nil, same as Fleet.tasks answers an
// empty list for a session with no dispatches - a caller must not have to
// guard against a non-nil empty value one way and nil the other.
func TestASubBacklogForNothingForwardedIsNil(t *testing.T) {
	if got := NewFleet().SubBacklog("nobody", "toolu_nothing"); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

// /clear empties the model's conversation and nothing else: claude 2.1.319 no
// longer kills the background agents, shells and workflows that were running,
// and their frames go on arriving. So what is still running keeps its row and
// its backlog, and what had finished is the conversation's history and goes.
// Dropping a running row left its ending nameless and the done line drawn
// while the work ran.
func TestASessionResetKeepsWhatIsRunningAndDropsWhatFinished(t *testing.T) {
	const finishedDispatch = "toolu_finished"
	f := NewFleet()
	f, _ = f.Observe(started("a1", subDispatch, "Auditing", "code-reviewer", core.TaskAgent), "sess-1")
	f, _ = f.Observe(spoke(subDispatch, subSaid), "sess-1")
	f, _ = f.Observe(started("a2", finishedDispatch, "Reading", "general-purpose", core.TaskAgent), "sess-1")
	f, _ = f.Observe(spoke(finishedDispatch, "done reading"), "sess-1")
	f, _ = f.Observe(ended("a2", core.TaskDone), "sess-1")
	f, _ = f.Observe(started("s1", "", "Run the dev server", "", core.TaskShell), "sess-1")

	f, _ = f.Observe(core.Event{Kind: core.KindSessionReset, SessionID: "sess-1"}, "sess-1")

	var kept []string
	for _, row := range f.tasks["sess-1"].Rows() {
		kept = append(kept, row.ID)
	}
	if !slices.Equal(kept, []string{"a1", "s1"}) {
		t.Errorf("rows after the reset = %v, want the two still running, a1 and s1", kept)
	}
	if got := len(f.RunningTasks("sess-1")); got != 2 {
		t.Errorf("%d rows listed as running, want 2", got)
	}
	if got := f.SubBacklog("sess-1", subDispatch); len(got) != 1 {
		t.Errorf("the running dispatch's backlog = %+v, want what it said kept", got)
	}
	if got := f.SubBacklog("sess-1", finishedDispatch); got != nil {
		t.Errorf("the finished dispatch's backlog survived the reset: %+v", got)
	}
}

// A reset with nothing running leaves no empty entry behind.
func TestASessionResetWithNothingRunningLeavesNoTaskEntry(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(started("a1", subDispatch, "Auditing", "code-reviewer", core.TaskAgent), "sess-1")
	f, _ = f.Observe(ended("a1", core.TaskDone), "sess-1")

	f, _ = f.Observe(core.Event{Kind: core.KindSessionReset, SessionID: "sess-1"}, "sess-1")

	if _, held := f.tasks["sess-1"]; held {
		t.Errorf("a reset with nothing running left tasks %+v", f.tasks["sess-1"])
	}
}

// A Fleet is a value: a caller holding an older one keeps the backlog it had,
// the same contract Fleet.tasks already keeps.
func TestAnOlderFleetKeepsTheSubagentBacklogItHad(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(spoke(subDispatch, "first"), "sess-1")

	older := f
	f, _ = f.Observe(spoke(subDispatch, "second"), "sess-1")

	if got := older.SubBacklog("sess-1", subDispatch); len(got) != 1 {
		t.Errorf("the older fleet has %d frames, want the 1 it was holding", len(got))
	}
	if got := f.SubBacklog("sess-1", subDispatch); len(got) != 2 {
		t.Errorf("the newer fleet has %d frames, want 2", len(got))
	}
}

// The whole point end to end: a DM opened for the first time under an agent
// whose subagent this client never watched live still shows what it said,
// because viewingPicked seeds it from the fleet before Viewing renders.
func TestOpeningADispatchUnderAnAgentNeverWatchedLiveShowsWhatItSaid(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withAgents("alex")

	// The dispatch and its speech both arrive while alex's DM has never been
	// opened by this client - App.dms holds nothing for "s1" yet.
	if _, held := a.dms["s1"]; held {
		t.Fatalf("alex's DM is already open, so this test does not exercise the bug")
	}
	a = a.applyFrame(taskFrame("s1", started("a1", subDispatch, "Count lines", "general-purpose", core.TaskAgent)))
	a = a.applyFrame(taskFrame("s1", spoke(subDispatch, subSaid)))

	a.roster = Roster{Selected: "s1", SelectedTask: subDispatch}
	a = a.openDMWith("s1", "alex").viewingPicked("s1")

	d, ok := a.dms["s1"]
	if !ok {
		t.Fatal("opening the dispatch did not open a conversation for alex")
	}
	if d.Viewed() != subDispatch {
		t.Fatalf("the pane is viewing %q, want the dispatch %q", d.Viewed(), subDispatch)
	}
	assertShows(t, *d, 200, 40, subSaid)
}

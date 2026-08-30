package ui

// The manager as a switch: the frames that leave a fleet with one running, and
// the command that turns it off again.
//
// The frames are asserted separately from the command because they have two
// callers with nothing else in common - cmd/wake writes them before a model
// exists, and `/manager` writes them from a keystroke - and a second copy of
// "which frame ensures a manager" is the parallel implementation this project
// forbids.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// managerDir is the directory a room-started manager runs in, in these tests.
const managerDir = "/tmp/wake-service-test"

// managerNewID is the id a caller mints for a manager it is about to start.
const managerNewID = "11111111-2222-3333-4444-555555555555"

// A fleet with agents and no manager gets one started.
func TestManagerFramesStartsOneWhenTheFleetHasNone(t *testing.T) {
	got := ManagerFrames(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", State: rpc.StateIdle},
	}}, managerDir, managerNewID)

	if len(got) != 1 {
		t.Fatalf("%d frames for a fleet with no manager, want exactly one spawn: %+v", len(got), got)
	}
	f := got[0]
	if f.Kind != rpc.FrameSpawn {
		t.Errorf("the frame is a %q, want a %q", f.Kind, rpc.FrameSpawn)
	}
	if f.Role != rpc.RoleManager {
		t.Errorf("the spawn carries Role %q, want %q. Without it the daemon draws an ordinary name from "+
			"the pool and the session gets no tools and no scope", f.Role, rpc.RoleManager)
	}
	if f.SessionID != managerNewID {
		t.Errorf("the spawn is under %q, want the id the caller minted (%q): maySpawn refuses anything "+
			"that is not one, because the reaper's only proof of a process group is that id in an argv",
			f.SessionID, managerNewID)
	}
	if f.Dir != managerDir {
		t.Errorf("the spawn runs in %q, want %q. An absent Dir is not refused - it is silently answered "+
			"with the daemon's own directory, which is whichever repository forked it", f.Dir, managerDir)
	}
	if f.Text != "" {
		t.Errorf("the spawn asks for the name %q. There is one manager name and the daemon owns it, so a "+
			"client asking for another is asking for something that would not be the manager", f.Text)
	}
}

// A manager that is already running is left alone.
func TestManagerFramesLeavesALiveManagerAlone(t *testing.T) {
	got := ManagerFrames(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", State: rpc.StateIdle},
		{ID: "s2", Name: core.ManagerName, State: rpc.StateIdle},
	}}, managerDir, managerNewID)

	if len(got) != 0 {
		t.Errorf("%d frames for a fleet that already has a manager: %+v. A second spawn is refused by "+
			"name, so this would be a refusal in the notice row for something nobody asked for", len(got), got)
	}
}

// A manager parked with ⌃C is woken rather than replaced.
//
// Starting a second one is not merely wasteful - it is refused, because a
// parked session keeps its name claimed. So the only thing that gets a manager
// back here is a wake, and it has to be addressed to the id the park left
// behind.
func TestManagerFramesWakesAParkedRowRatherThanStartingASecond(t *testing.T) {
	got := ManagerFrames(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", State: rpc.StateIdle},
		{ID: "s2", Name: core.ManagerName, State: rpc.StateParked},
	}}, managerDir, managerNewID)

	if len(got) != 1 {
		t.Fatalf("%d frames for a parked manager, want exactly one wake: %+v", len(got), got)
	}
	if got[0].Kind != rpc.FrameWake {
		t.Errorf("the frame is a %q, want a %q: a parked manager still holds its name, so a spawn is refused", got[0].Kind, rpc.FrameWake)
	}
	if got[0].SessionID != "s2" {
		t.Errorf("the wake is addressed to %q, want the parked manager's own id", got[0].SessionID)
	}
}

// And a manager left in the park book by ⌃Q is woken too.
//
// This is the case that has no live row at all: a daemon restores nothing, so
// after ⌃Q the manager is on rpc.Status.Parked and nowhere else. It is also the
// only path across a restart, which is why it is asserted separately rather
// than assumed to be the same code.
func TestManagerFramesWakesAParkBookRecordAfterARestart(t *testing.T) {
	got := ManagerFrames(&rpc.Status{Running: true, Parked: []rpc.SessionStatus{
		{ID: "booked", Name: core.ManagerName, State: rpc.StateParked},
	}}, managerDir, managerNewID)

	if len(got) != 1 {
		t.Fatalf("%d frames for a manager in the park book, want exactly one wake: %+v", len(got), got)
	}
	if got[0].Kind != rpc.FrameWake || got[0].SessionID != "booked" {
		t.Errorf("the frame is %+v, want a %q addressed to the booked id. Status.Parked is disjoint from "+
			"Sessions, so a reader that only walks Sessions starts a second manager under a claimed name",
			got[0], rpc.FrameWake)
	}
}

// A nil report starts nothing.
//
// Nil is legitimate - NewRoomApp documents it as "wait for the first push" - and
// the safe reading is that nothing is known rather than that nothing is running.
// Spawning here would put a manager on a fleet that already has one every time a
// caller had no seed.
func TestManagerFramesStartsNothingWithNoReportAtAll(t *testing.T) {
	if got := ManagerFrames(nil, managerDir, managerNewID); len(got) != 0 {
		t.Errorf("%d frames from a nil report: %+v. Nothing is known here, and a spawn on no evidence is "+
			"a second manager whenever a caller had no seed", len(got), got)
	}
}

// managerFrameStates is what ManagerFrames does about a manager in each state,
// with the reason per cell.
//
// A table rather than examples for serviceStates' reason, which applies harder
// here: that filter decides where a message goes, and this one decides whether a
// **process starts**. A state filter with no totality guard fails open, and the
// open direction here spends a name, a process and somebody's money.
var managerFrameStates = map[string]struct {
	kind string
	why  string
}{
	rpc.StateIdle:    {kind: "", why: "a manager with a process, between turns: it is on, and on is what this ensures"},
	rpc.StateWorking: {kind: "", why: "mid-turn is on. Starting a second would be refused by name, and parking it here is not what an ensure means"},
	rpc.StateSilent:  {kind: "", why: "silent is a guess about a working agent that has not spoken, not a state of the process. Its stdin is open"},
	rpc.StateBlocked: {kind: "", why: "blocked is an outstanding permission ask. The process is alive and the name is claimed; what it needs is an answer, not a restart"},
	rpc.StateEnded:   {kind: rpc.FrameSpawn, why: "no process, and the name went back to the pool when the session ended - so a spawn gets it again and a wake has nothing to address"},
	rpc.StateParked:  {kind: rpc.FrameWake, why: "no process, and the name is still claimed - so a spawn is refused and a wake is the only thing that brings it back"},
}

// The verdict is total over the states a running daemon can report.
//
// Rung 4, reusing the scan ⌃C, ⌃F and service() already run: the domain is
// agent.stateLocked's own range, so a seventh state is a build failure here
// until somebody says whether a manager in it should be started, woken or left
// alone.
func TestManagerFramesHasAVerdictForEveryStateARunningDaemonCanReport(t *testing.T) {
	declared := sessionStateConstants(t)
	reachable := statesARunningDaemonReports(t)

	for name, state := range declared {
		_, decided := managerFrameStates[state]
		why, excused := unreachableInAForkReport[state]
		switch {
		case decided && excused:
			t.Errorf("rpc.%s = %q is both decided here and excused as unreachable (%s): one of the two is wrong", name, state, why)
		case !decided && !excused:
			t.Errorf("rpc.%s = %q is a state a session can be reported in, and nothing here says what "+
				"ManagerFrames does about a manager in it. This filter decides whether a process starts, "+
				"and the open direction spends a name, a process and somebody's money", name, state)
		case decided && !reachable[state]:
			t.Errorf("rpc.%s = %q has a verdict here, but agent.stateLocked never returns it - so the cell "+
				"asserts something about an input no daemon can produce, which reads as coverage", name, state)
		}
	}
	for state := range reachable {
		if _, decided := managerFrameStates[state]; !decided {
			t.Errorf("agent.stateLocked can report %q and nothing here says what ManagerFrames does about "+
				"a manager in it", state)
		}
	}
}

// And the behaviour, per member of that table.
func TestManagerFramesDoesWhatTheTableSaysPerState(t *testing.T) {
	checked := 0
	for state, want := range managerFrameStates {
		t.Run(state, func(t *testing.T) {
			got := ManagerFrames(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
				{ID: "s1", Name: "sydney", State: rpc.StateIdle},
				{ID: "s2", Name: core.ManagerName, State: state},
			}}, managerDir, managerNewID)

			if want.kind == "" {
				if len(got) != 0 {
					t.Errorf("a %q manager produced %+v, want nothing (%s)", state, got, want.why)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("a %q manager produced %d frames, want one %q (%s)", state, len(got), want.kind, want.why)
			}
			if got[0].Kind != want.kind {
				t.Errorf("a %q manager produced a %q, want a %q (%s)", state, got[0].Kind, want.kind, want.why)
			}
		})
		checked++
	}
	if checked != len(managerFrameStates) {
		t.Fatalf("checked %d of %d states", checked, len(managerFrameStates))
	}
}

// --- /manager, the command ------------------------------------------------

// The router takes /manager and starts one when there is none.
func TestSlashManagerStartsOneWhenThereIsNone(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")

	next, cmd, handled := a.slash(managerVerb)
	if !handled {
		t.Fatal("the router did not take /manager, so it went to the agent as a message")
	}
	f := sentFrame(t, next, cmd)
	if f.Kind != rpc.FrameSpawn || f.Role != rpc.RoleManager {
		t.Errorf("/manager wrote %+v, want a spawn carrying Role %q", f, rpc.RoleManager)
	}
}

// /manager parks a manager that is running. That is the whole of what makes it
// a switch rather than a start.
func TestSlashManagerParksALiveOne(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", core.ManagerName)
	managerID := idOfAgentNamed(t, a, core.ManagerName)

	next, cmd, handled := a.slash(managerVerb)
	if !handled {
		t.Fatal("the router did not take /manager")
	}
	f := sentFrame(t, next, cmd)
	if f.Kind != rpc.FramePark {
		t.Errorf("/manager against a live manager wrote a %q, want a %q: with one already running there "+
			"is nothing to start, and turning it off is the other half of a toggle", f.Kind, rpc.FramePark)
	}
	if f.SessionID != managerID {
		t.Errorf("the park is addressed to %q, want the manager %q", f.SessionID, managerID)
	}
}

// And wakes a parked one.
func TestSlashManagerWakesAParkedOne(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{
		Kind: rpc.FrameStatusPush,
		Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "sydney", State: rpc.StateIdle},
			{ID: "s2", Name: core.ManagerName, State: rpc.StateParked},
		}},
	})

	next, cmd, handled := a.slash(managerVerb)
	if !handled {
		t.Fatal("the router did not take /manager")
	}
	f := sentFrame(t, next, cmd)
	if f.Kind != rpc.FrameWake || f.SessionID != "s2" {
		t.Errorf("/manager against a parked manager wrote %+v, want a %q addressed to s2", f, rpc.FrameWake)
	}
}

// It refuses to park a manager that is blocked on a permission ask.
//
// Park closes stdin, and an ask that dies that way is recorded as a denial the
// operator never made - which survives the wake. This is park()'s own refusal
// reused rather than restated: one rule, one sentence, one place to correct it.
func TestSlashManagerRefusesToParkABlockedManager(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{
		Kind: rpc.FrameStatusPush,
		Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
			{ID: "s2", Name: core.ManagerName, State: rpc.StateBlocked},
		}},
	})

	next, cmd, handled := a.slash(managerVerb)
	if !handled {
		t.Fatal("the router did not take /manager")
	}
	if cmd != nil {
		t.Fatalf("/manager parked a blocked manager: %+v. Parking closes stdin under the ask, and the "+
			"session comes back believing it was told no about a question nobody saw", sentFrames(t, next, cmd))
	}
	// The sentence is built from ⌃C's own constant rather than matched loosely,
	// which is what holds the two refusals to one rule: a /manager that grew its
	// own wording would be a second place to correct the day the rule changes.
	want := fmt.Sprintf(parkWouldDeny, agentPrefix, core.ManagerName)
	if notice.Count(want) != 1 {
		got, _ := notice.Latest()
		t.Errorf("/manager on a blocked manager said %q, want ⌃C's own refusal %q", got.String(), want)
	}
}

// An argument is refused rather than ignored.
//
// /mcp's rule one command over: somebody typing `/manager off` means something
// specific, and a toggle that fired anyway would look like it worked while doing
// the opposite half the time.
func TestSlashManagerRefusesAnArgumentRatherThanIgnoringIt(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")

	next, cmd, handled := a.slash(managerVerb + " off")
	if !handled {
		t.Fatal("the router did not take /manager with an argument")
	}
	if cmd != nil {
		t.Fatalf("/manager off started or stopped something: %+v", sentFrames(t, next, cmd))
	}
	if notice.Count(managerTakesNoArgument) != 1 {
		t.Error("/manager off did nothing and said nothing")
	}
}

// The refusals name the command that works, built from the command's own
// constant so a sentence cannot advertise one that does not exist.
func TestTheRoomsManagerAdviceNamesTheCommandThatWorks(t *testing.T) {
	for what, sentence := range map[string]string{"NoAddressee": NoAddressee, "managerParked": managerParked} {
		if !strings.Contains(sentence, managerVerb) {
			t.Errorf("%s is %q and does not name %s, which is the one command that gets a manager back "+
				"in every state", what, sentence, managerVerb)
		}
	}
}

// --- /manager-stop, the ending --------------------------------------------

// /manager-stop ends the manager rather than parking it.
//
// The word is the one this project already uses for it: "park is recoverable;
// stop is not". A stop releases the name back to the pool, which is why
// /manager afterwards starts a fresh one rather than waking anything.
func TestSlashManagerStopEndsALiveManager(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", core.ManagerName)
	managerID := idOfAgentNamed(t, a, core.ManagerName)

	next, cmd, handled := a.slash(managerStopVerb)
	if !handled {
		t.Fatal("the router did not take /manager-stop, so it went to the agent as a message")
	}
	f := sentFrame(t, next, cmd)
	if f.Kind != rpc.FrameStop {
		t.Errorf("/manager-stop wrote a %q, want a %q: park is the recoverable ending and this is the "+
			"other one", f.Kind, rpc.FrameStop)
	}
	if f.SessionID != managerID {
		t.Errorf("the stop is addressed to %q, want the manager %q", f.SessionID, managerID)
	}
}

// It reaches only the manager, never whichever row the roster cursor rests on.
//
// The command is addressed to Wake rather than to a session, so nothing about
// the cursor may select its target - a /manager-stop that ended the focused
// agent would be an irreversible verb aimed by a highlight.
func TestSlashManagerStopEndsTheManagerRatherThanTheSelectedAgent(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", core.ManagerName)
	managerID := idOfAgentNamed(t, a, core.ManagerName)
	sydneyID := idOfAgentNamed(t, a, "sydney")

	next, cmd, _ := a.slash(managerStopVerb)
	f := sentFrame(t, next, cmd)
	if f.SessionID == sydneyID {
		t.Fatalf("/manager-stop ended %q, the ordinary agent. Stop is the one ending nothing brings back", sydneyID)
	}
	if f.SessionID != managerID {
		t.Errorf("the stop is addressed to %q, want the manager %q", f.SessionID, managerID)
	}
}

// With no manager at all there is nothing to end, and it says so.
//
// Fleet.manager treats an ended manager as absent - the name went back to the
// pool - so this is the arm both "never started one" and "already stopped it"
// arrive on.
func TestSlashManagerStopRefusesWhenThereIsNoManager(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")

	next, cmd, handled := a.slash(managerStopVerb)
	if !handled {
		t.Fatal("the router did not take /manager-stop")
	}
	if cmd != nil {
		t.Fatalf("/manager-stop wrote %+v with no manager in the fleet", sentFrames(t, next, cmd))
	}
	if notice.Count(managerNotRunning) != 1 {
		got, _ := notice.Latest()
		t.Errorf("/manager-stop with no manager said %q, want %q", got.String(), managerNotRunning)
	}
}

// A parked manager is refused, and the refusal names the command that gets it
// back.
//
// This is mechanism rather than taste: the daemon refuses a stop at a session
// with no process, in two different shapes - "has ended" for a row parked by ⌃C
// and "unknown session" for one parked across a restart, which has no row at
// all. Writing it anyway would put one of two unrelated daemon sentences in the
// notice row depending on when the operator last quit, for a command that could
// have explained itself once. Both shapes are pinned by
// internal/daemon's TestAStopAtAParkedSessionIsRefusedInBothOfItsShapes.
func TestSlashManagerStopRefusesAParkedManagerAndNamesTheCommandThatWakesIt(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{
		Kind: rpc.FrameStatusPush,
		Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
			{ID: "s2", Name: core.ManagerName, State: rpc.StateParked},
		}},
	})

	next, cmd, handled := a.slash(managerStopVerb)
	if !handled {
		t.Fatal("the router did not take /manager-stop")
	}
	if cmd != nil {
		t.Fatalf("/manager-stop wrote %+v at a parked manager, which the daemon answers with "+
			"\"unknown session\"", sentFrames(t, next, cmd))
	}
	if notice.Count(managerStopParked) != 1 {
		got, _ := notice.Latest()
		t.Errorf("/manager-stop on a parked manager said %q, want %q", got.String(), managerStopParked)
	}
	if !strings.Contains(managerStopParked, managerVerb) {
		t.Errorf("the refusal is %q and does not name %s, which is what wakes it. A refusal that is only "+
			"\"no\" leaves an operator with a command that does nothing and no idea when it would",
			managerStopParked, managerVerb)
	}
}

// A blocked manager is ended rather than refused, and that inverts ⌃C's rule on
// purpose.
//
// park refuses this state because closing stdin under an outstanding ask is
// recorded as a denial the operator never made - and the whole weight of that
// argument is that the false "no" **survives the wake**. A stop has no wake:
// the session ends, the name is released, and nothing is ever built on the
// belief. Refusing here would leave an operator unable to get rid of the one
// session they are trying to be rid of.
func TestSlashManagerStopEndsABlockedManagerRatherThanRefusing(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{
		Kind: rpc.FrameStatusPush,
		Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
			{ID: "s2", Name: core.ManagerName, State: rpc.StateBlocked},
		}},
	})

	next, cmd, handled := a.slash(managerStopVerb)
	if !handled {
		t.Fatal("the router did not take /manager-stop")
	}
	if cmd == nil {
		t.Fatal("/manager-stop refused a blocked manager. Park refuses this state because the denial " +
			"nobody made survives the wake; a stop has no wake, so the operator is left unable to end " +
			"the session they are trying to end")
	}
	f := sentFrame(t, next, cmd)
	if f.Kind != rpc.FrameStop || f.SessionID != "s2" {
		t.Errorf("/manager-stop on a blocked manager wrote %+v, want a %q addressed to s2", f, rpc.FrameStop)
	}
	if notice.Count(fmt.Sprintf(parkWouldDeny, agentPrefix, core.ManagerName)) != 0 {
		t.Error("/manager-stop borrowed ⌃C's park refusal, which is about a state it does not have")
	}
}

// An argument is refused rather than ignored, for /manager's own reason.
func TestSlashManagerStopRefusesAnArgumentRatherThanIgnoringIt(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", core.ManagerName)

	next, cmd, handled := a.slash(managerStopVerb + " now")
	if !handled {
		t.Fatal("the router did not take /manager-stop with an argument")
	}
	if cmd != nil {
		t.Fatalf("/manager-stop now ended something: %+v", sentFrames(t, next, cmd))
	}
	if notice.Count(managerStopTakesNoArgument) != 1 {
		t.Error("/manager-stop now did nothing and said nothing")
	}
}

// managerStopStates is what /manager-stop does about a manager in each state.
//
// A table for managerFrameStates' reason, and one sharper: this filter decides
// whether a session is **ended**, which is the one thing in this project that
// nothing brings back. A state that falls through it unruled is a stop nobody
// designed.
var managerStopStates = map[string]struct {
	kind string
	why  string
}{
	rpc.StateIdle:    {kind: rpc.FrameStop, why: "a manager with a process, between turns: there is something to end and stdin is open"},
	rpc.StateWorking: {kind: rpc.FrameStop, why: "mid-turn. The daemon closes stdin and lets the in-flight turn finish, which is what stop has always meant here"},
	rpc.StateSilent:  {kind: rpc.FrameStop, why: "silent is a guess about a working agent that has not spoken, not a state of the process. Its stdin is open, so a stop reaches it"},
	rpc.StateBlocked: {kind: rpc.FrameStop, why: "park refuses this because the denial nobody made survives the wake. A stop has no wake, and refusing would leave the operator unable to end it"},
	rpc.StateEnded:   {kind: "", why: "already over, and Fleet.manager reads it as absent because the name went back to the pool. There is nothing to address"},
	rpc.StateParked:  {kind: "", why: "no process, and the daemon refuses a stop at one - \"has ended\" for a ⌃C row, \"unknown session\" for a book record after a restart. /manager wakes it first"},
}

// The verdict is total over the states a running daemon can report.
func TestManagerStopHasAVerdictForEveryStateARunningDaemonCanReport(t *testing.T) {
	declared := sessionStateConstants(t)
	reachable := statesARunningDaemonReports(t)

	for name, state := range declared {
		_, decided := managerStopStates[state]
		why, excused := unreachableInAForkReport[state]
		switch {
		case decided && excused:
			t.Errorf("rpc.%s = %q is both decided here and excused as unreachable (%s): one of the two is wrong", name, state, why)
		case !decided && !excused:
			t.Errorf("rpc.%s = %q is a state a session can be reported in, and nothing here says what "+
				"/manager-stop does about a manager in it. This filter decides whether a session is ended, "+
				"which is the one thing nothing in this project brings back", name, state)
		case decided && !reachable[state]:
			t.Errorf("rpc.%s = %q has a verdict here, but agent.stateLocked never returns it - so the cell "+
				"asserts something about an input no daemon can produce, which reads as coverage", name, state)
		}
	}
	for state := range reachable {
		if _, decided := managerStopStates[state]; !decided {
			t.Errorf("agent.stateLocked can report %q and nothing here says what /manager-stop does about "+
				"a manager in it", state)
		}
	}
}

// And the behaviour, per member of that table.
func TestManagerStopDoesWhatTheTableSaysPerState(t *testing.T) {
	for state, want := range managerStopStates {
		t.Run(state, func(t *testing.T) {
			a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{
				Kind: rpc.FrameStatusPush,
				Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
					{ID: "s1", Name: "sydney", State: rpc.StateIdle},
					{ID: "s2", Name: core.ManagerName, State: state},
				}},
			})

			next, cmd, handled := a.slash(managerStopVerb)
			if !handled {
				t.Fatal("the router did not take /manager-stop")
			}
			if want.kind == "" {
				if cmd != nil {
					t.Errorf("a %q manager was sent %+v, want nothing (%s)", state, sentFrames(t, next, cmd), want.why)
				}
				return
			}
			f := sentFrame(t, next, cmd)
			if f.Kind != want.kind {
				t.Errorf("a %q manager was sent a %q, want a %q (%s)", state, f.Kind, want.kind, want.why)
			}
			if f.SessionID != "s2" {
				t.Errorf("a %q manager was addressed as %q, want s2", state, f.SessionID)
			}
		})
	}
}

// The ending claims no key either, for the switch's own reason.
func TestTheManagerStopCommandClaimsNoKey(t *testing.T) {
	for _, e := range legendEntries {
		if strings.Contains(strings.ToLower(e.what), managerStopCommand) {
			t.Errorf("the legend advertises %q %q. Stop is irreversible and the rarest verb in the build; "+
				"a chord for it is a key somebody presses by accident", e.glyph, e.what)
		}
	}
}

// --- what arrives ---------------------------------------------------------

// A manager this client started reports and opens no pane.
//
// cmd/wake/manager.go's ruling, and it is a layout fact rather than a
// preference: the manager is a service, the place you talk to it is the room's
// own composer, and a pane opened on it would put the operator in a composer
// whose unaddressed text goes to the session they are looking at.
func TestAnArrivingManagerReportsAndOpensNoPane(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).awaitingStart("m1")

	a = a.applyFrame(rpc.Frame{
		Kind: rpc.FrameStatusPush,
		Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
			{ID: "m1", Name: core.ManagerName, State: rpc.StateIdle},
		}},
	})

	if len(a.dms) != 0 {
		t.Errorf("a manager that arrived opened %d conversation(s). It is a service with no surface of "+
			"its own; ⌃D on its roster row is how somebody who wants one gets it", len(a.dms))
	}
	if _, waiting := a.pendingStarts["m1"]; waiting {
		t.Error("the wait was never settled, so a later session given that id would be reported as this one")
	}
}

// A fresh spawn - `/new`'s own arrival, ParentID empty like the manager's -
// opens no pane either, and for a related reason: replacing whatever pane was
// on screen for one that has said nothing yet is the wrong trade at fleet
// scale. Unlike the manager it drafts a mention, so the room's composer names
// the agent that just started rather than leaving the operator to spell it.
func TestAnArrivingFreshSpawnPrefillsAMentionAndOpensNoPane(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).awaitingStart("a1")

	a = a.applyFrame(rpc.Frame{
		Kind: rpc.FrameStatusPush,
		Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
			{ID: "a1", Name: "sydney", State: rpc.StateIdle},
		}},
	})

	if len(a.dms) != 0 {
		t.Errorf("a fresh spawn opened %d conversation(s), want 0: it drafts a mention instead", len(a.dms))
	}
	if want, got := "@sydney ", a.room.Composer().Value(); got != want {
		t.Errorf("the room's draft is %q after a fresh spawn arrived, want %q", got, want)
	}
}

// A fork or an import still opens its pane: ParentID is what tells it apart
// from a fresh spawn, and daemon.launch writes a non-empty one for both.
func TestAnArrivingForkStillOpensItsPane(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).awaitingStart("a1")

	a = a.applyFrame(rpc.Frame{
		Kind: rpc.FrameStatusPush,
		Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
			{ID: "a1", Name: "sydney", State: rpc.StateIdle, ParentID: "s0"},
		}},
	})

	if len(a.dms) != 1 {
		t.Errorf("a fork this client asked for opened %d conversation(s), want 1", len(a.dms))
	}
}

// A key press is not how /manager is reached, and nothing in the legend claims
// otherwise.
//
// The command exists instead of a chord because with the manager on by default
// this is the rarest verb in the build, and every remaining ctrl key either
// shadows a composer binding or is a terminal control code. This holds the
// decision: a glyph appearing here later has to be argued rather than added.
func TestTheManagerSwitchClaimsNoKey(t *testing.T) {
	for _, e := range legendEntries {
		if strings.Contains(strings.ToLower(e.what), managerCommand) {
			t.Errorf("the legend advertises %q %q. The manager switch is a command, and a legend entry "+
				"for it is a key somebody will press", e.glyph, e.what)
		}
	}
}

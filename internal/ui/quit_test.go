package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// oneAgent is a status push naming a single agent in one state, so a test can
// put an agent into any state the daemon can report.
func oneAgent(id, name, state string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running:  true,
		Sessions: []rpc.SessionStatus{{ID: id, Name: name, State: state}},
	}}
}

// /quit in a conversation ends that conversation's agent: the bare form is the
// pane you are in, the same grammar /name and /color read.
func TestQuitInAConversationEndsThatAgent(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(200, 40)

	next, cmd, handled := a.slash(quitVerb)
	if !handled {
		t.Fatal("the router did not take /quit, so it went to the agent as a message")
	}
	f := sentFrame(t, next, cmd)
	if f.Kind != rpc.FrameStop {
		t.Errorf("/quit wrote a %q, want a %q: quit is the per-agent ending", f.Kind, rpc.FrameStop)
	}
	if f.SessionID != "s1" {
		t.Errorf("/quit in alex's conversation ended %q, want the focused agent s1", f.SessionID)
	}
}

// @who /quit in the room ends that agent, the mention→target bridge /color, /name
// and /task already ride. The composer's own conversation is not the target - the
// mention is.
func TestMentionedQuitInTheRoomEndsThatAgent(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(200, 40).showRoom()

	_, cmd := typeAndSubmit(a, "@sydney "+quitVerb)
	go func() { _ = runCmdQuietly(cmd) }()
	f := awaitFrame(t, sent)

	if f.Kind != rpc.FrameStop {
		t.Fatalf("@sydney /quit wrote a %q frame, want %q", f.Kind, rpc.FrameStop)
	}
	if f.SessionID != "s2" {
		t.Errorf("@sydney /quit ended %q, want sydney (s2)", f.SessionID)
	}
}

// Once a report confirms the quit agent ended, it leaves the fleet: no roster
// row, no DM pane, no place in addressing. The daemon keeps ended sessions in its
// recent ring so a client learns of an ending it missed; a session this operator
// asked to quit is one they already know ended, so this window removes it.
func TestAQuitAgentLeavesTheFleetOnceItEnds(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(200, 40)

	next, _, _ := a.slash(quitVerb) // focus is s1 (alex)
	if next.focus != "s1" {
		t.Fatalf("test precondition: /quit was not typed in alex's pane (focus %q)", next.focus)
	}

	// The daemon closes stdin, the turn finishes, and the next report names it ended.
	after := next.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", State: rpc.StateEnded},
			{ID: "s2", Name: "sydney", State: rpc.StateIdle},
		},
	}})

	if _, ok := after.fleet.Agent("s1"); ok {
		t.Error("the quit agent is still in the fleet after it ended, so it still draws a roster row")
	}
	for _, agent := range after.fleet.Agents() {
		if agent.ID == "s1" {
			t.Error("the quit agent is still ranked into the roster after ending")
		}
	}
	if after.grid.Has("s1") {
		t.Error("the quit agent's DM pane is still open after it ended")
	}
	if after.focus == "s1" {
		t.Error("the keys are still on the quit agent's pane after it ended")
	}
	if slices.Contains(after.dmOrder, "s1") {
		t.Error("the quit agent is still in the ⇥ ring after it ended")
	}
}

// An agent still finishing its in-flight turn when /quit lands stays visible: the
// stop lets the turn finish, and the row leaves only when the ending is confirmed.
func TestAQuitAgentStaysUntilTheEndingIsConfirmed(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(200, 40)

	next, _, _ := a.slash(quitVerb)
	working := next.applyFrame(oneAgent("s1", "alex", rpc.StateWorking))

	if _, ok := working.fleet.Agent("s1"); !ok {
		t.Error("the quit agent left the fleet while still finishing its turn; the stop lets the turn finish first")
	}
}

// The room with no handle refuses rather than guessing: quit is irreversible, so
// it may not be aimed by whichever row the roster cursor rests on.
func TestQuitInTheRoomRefusesWithoutATarget(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")

	next, cmd, handled := a.slash(quitVerb)
	if !handled {
		t.Fatal("the router did not take /quit")
	}
	if cmd != nil {
		t.Fatalf("/quit in the room ended something with no target: %+v", sentFrames(t, next, cmd))
	}
	if !strings.Contains(shown(next), noQuitTarget) {
		t.Errorf("/quit in the room did not refuse with %q:\n%s", noQuitTarget, shown(next))
	}
}

// A parked agent is refused, and the refusal names the command that wakes it: the
// daemon refuses a stop at a session with no process, so a stop written here comes
// back as an unrelated daemon sentence. managerStopParked's own reasoning.
func TestQuitRefusesAParkedAgentAndNamesTheCommandThatWakesIt(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(oneAgent("s1", "sydney", rpc.StateParked))

	next, cmd, handled := a.slash(quitVerb + " @sydney")
	if !handled {
		t.Fatal("the router did not take /quit @sydney")
	}
	if cmd != nil {
		t.Fatalf("/quit ended a parked agent, which the daemon answers \"unknown session\": %+v", sentFrames(t, next, cmd))
	}
	if !strings.Contains(shown(next), resumeVerb) {
		t.Errorf("the parked refusal does not name %s, which is what wakes it:\n%s", resumeVerb, shown(next))
	}
}

// quitStates is what /quit does about an agent in each state, the totality table
// managerStopStates is for the manager: this filter decides whether a session is
// **ended**, the one thing in this project nothing brings back, so a state that
// falls through it unruled is a stop nobody designed.
var quitStates = map[string]struct {
	kind string
	why  string
}{
	rpc.StateIdle:    {kind: rpc.FrameStop, why: "a process between turns: stdin is open and there is something to end"},
	rpc.StateWorking: {kind: rpc.FrameStop, why: "mid-turn. The daemon closes stdin and lets the in-flight turn finish, which is what stop has meant here"},
	rpc.StateSilent:  {kind: rpc.FrameStop, why: "a guess about a working agent that has not spoken, not a state of the process. Its stdin is open, so a stop reaches it"},
	rpc.StateBlocked: {kind: rpc.FrameStop, why: "park refuses this because the denial nobody made survives the wake. A stop has no wake, and refusing would leave the operator unable to end it"},
	rpc.StateEnded:   {kind: "", why: "already over, and nothing is lost, so this is silent - interrupt's own trade for an ended session"},
	rpc.StateParked:  {kind: "", why: "no process, and the daemon refuses a stop at one. /resume wakes it first"},
}

// The verdict is total over the states a running daemon can report, the same
// producer-derived check managerStopStates carries and for the same reason.
func TestQuitStatesAreTotalOverWhatCanArrive(t *testing.T) {
	declared := sessionStateConstants(t)
	reachable := statesARunningDaemonReports(t)

	for name, state := range declared {
		_, decided := quitStates[state]
		why, excused := unreachableInAForkReport[state]
		switch {
		case decided && excused:
			t.Errorf("rpc.%s = %q is both decided here and excused as unreachable (%s): one of the two is wrong", name, state, why)
		case !decided && !excused:
			t.Errorf("rpc.%s = %q is a state a session can be reported in, and nothing here says what /quit "+
				"does about an agent in it. This filter decides whether a session is ended, the one thing "+
				"nothing in this project brings back", name, state)
		case decided && !reachable[state]:
			t.Errorf("rpc.%s = %q has a verdict here, but agent.stateLocked never returns it, so the cell "+
				"asserts something about an input no daemon can produce", name, state)
		}
	}
	for state := range reachable {
		if _, decided := quitStates[state]; !decided {
			t.Errorf("agent.stateLocked can report %q and nothing here says what /quit does about an agent in it", state)
		}
	}
}

// And the behaviour, per member of that table.
func TestQuitDoesWhatTheTableSaysPerState(t *testing.T) {
	for state, want := range quitStates {
		t.Run(state, func(t *testing.T) {
			a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(200, 40).
				applyFrame(oneAgent("s1", "alex", state))

			next, cmd, handled := a.slash(quitVerb)
			if !handled {
				t.Fatalf("the router did not take /quit for a %s agent", state)
			}
			if want.kind == "" {
				if cmd != nil {
					t.Fatalf("/quit on a %s agent wrote %+v, want nothing: %s", state, sentFrames(t, next, cmd), want.why)
				}
				return
			}
			f := sentFrame(t, next, cmd)
			if f.Kind != want.kind {
				t.Errorf("/quit on a %s agent wrote %q, want %q: %s", state, f.Kind, want.kind, want.why)
			}
			if f.SessionID != "s1" {
				t.Errorf("/quit on a %s agent addressed %q, want s1", state, f.SessionID)
			}
		})
	}
}

// /quit refuses the manager and points at /manager-stop, on both reachable
// routes: `/quit @manager` as a slash, and a bare /quit typed in the manager's
// own DM. The manager has one ending path on purpose, and a second verb that
// also dropped its row would be a parallel implementation.
func TestQuitRefusesTheManagerAndNamesManagerStop(t *testing.T) {
	t.Run("@manager", func(t *testing.T) {
		a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{
			Kind: rpc.FrameStatusPush,
			Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
				{ID: "s1", Name: "alex", State: rpc.StateIdle},
				{ID: "s2", Name: core.ManagerName, State: rpc.StateIdle},
			}},
		})

		next, cmd, handled := a.slash(quitVerb + " " + agentPrefix + core.ManagerName)
		if !handled {
			t.Fatal("the router did not take /quit @manager")
		}
		if cmd != nil {
			t.Fatalf("/quit ended the manager, a second path past /manager-stop: %+v", sentFrames(t, next, cmd))
		}
		if !strings.Contains(shown(next), managerStopVerb) {
			t.Errorf("the manager refusal does not name %s:\n%s", managerStopVerb, shown(next))
		}
	})

	t.Run("bare in the manager's DM", func(t *testing.T) {
		a := dmApp(newRecorder(t), Stream{}, "s2", core.ManagerName).withSize(200, 40).
			applyFrame(oneAgent("s2", core.ManagerName, rpc.StateIdle))

		next, cmd, handled := a.slash(quitVerb)
		if !handled {
			t.Fatal("the router did not take /quit in the manager's DM")
		}
		if cmd != nil {
			t.Fatalf("a bare /quit in the manager's DM ended it: %+v", sentFrames(t, next, cmd))
		}
		if !strings.Contains(shown(next), managerStopVerb) {
			t.Errorf("the manager refusal does not name %s:\n%s", managerStopVerb, shown(next))
		}
	})
}

// Quitting an agent whose DM is open but NOT focused still resizes the survivors.
// forgetConversation can close a background column - unlike ⌃W, which only closes
// the focused one - and a column freed anywhere widens the rest; a stored width
// left stale re-wraps that pane every frame.
func TestQuittingABackgroundPaneResizesTheSurvivors(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(200, 40).
		openRight("s2", "sydney").showRoom()
	if a.focus != "" {
		t.Fatalf("precondition: the room is not focused (focus %q)", a.focus)
	}
	before, ok := a.dms["s1"]
	if !ok {
		t.Fatal("precondition: alex's DM is not open")
	}
	beforeWidth := before.width

	next, _ := a.quitAgent(agentPrefix + "sydney")
	after := next.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", State: rpc.StateIdle},
			{ID: "s2", Name: "sydney", State: rpc.StateEnded},
		},
	}})

	if after.grid.Has("s2") {
		t.Fatal("sydney's column is still open after the quit was confirmed")
	}
	d, ok := after.dms["s1"]
	if !ok {
		t.Fatal("alex's DM was dropped; only the quit agent should leave")
	}
	if d.width <= beforeWidth {
		t.Errorf("alex's pane did not widen when sydney's background column closed (%d → %d): "+
			"forgetConversation skipped resizePanes, so the survivor re-wraps every frame", beforeWidth, d.width)
	}
}

// The watch set is pruned once the daemon stops reporting a quit agent (it has
// left the recent ring), so `quitting` stays bounded rather than holding a dead
// id for the window's life. It is kept while the daemon still reports the ending,
// because the drop has to be re-applied until then.
func TestQuittingIsPrunedWhenTheDaemonForgetsTheAgent(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(200, 40)

	next, _, _ := a.slash(quitVerb) // quits alex (s1), the focused pane
	if _, watching := next.quitting["s1"]; !watching {
		t.Fatal("awaitingQuit did not record the quit")
	}

	// Ended and still in the daemon's recent ring: kept, so the drop re-applies.
	ended := next.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", State: rpc.StateEnded},
			{ID: "s2", Name: "sydney", State: rpc.StateIdle},
		},
	}})
	if _, watching := ended.quitting["s1"]; !watching {
		t.Error("quitting dropped s1 while the daemon still reports it ended: the drop would stop re-applying and the row would return")
	}

	// The daemon forgets it (out of the recent ring): a report that no longer names s1.
	forgotten := ended.applyFrame(oneAgent("s2", "sydney", rpc.StateIdle))
	if _, watching := forgotten.quitting["s1"]; watching {
		t.Error("quitting still holds s1 after the daemon stopped reporting it: the watch set grows unbounded")
	}
}

// The keypress says what it asked, so an operator whose agent is still finishing a
// turn is told the quit was read even though the row has not gone yet.
func TestQuitReportsOnTheKeypress(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)

	next, _, _ := a.slash(quitVerb)
	if got := shown(next); !strings.Contains(got, fmt.Sprintf(quitAsked, agentPrefix, "alex")) {
		t.Errorf("/quit said nothing about what it asked:\n%s", got)
	}
}

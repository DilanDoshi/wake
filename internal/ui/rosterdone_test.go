package ui

// The roster's "done": an agent that finished a turn this client watched, with
// nothing of it still running. An annotation over idle, never a state - so
// stateGlyph, stateLabel and attentionRank do not know it - and the same
// predicate the DM's `✻ … done` line draws on, so the two surfaces agree.

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestTurnDoneIsAWitnessedFinishWithNothingStillRunning(t *testing.T) {
	finished := Agent{State: rpc.StateIdle, doneAt: time.Now()}
	for _, tc := range []struct {
		name       string
		a          Agent
		subRunning bool
		want       bool
	}{
		{"idle after a witnessed turn", finished, false, true},
		{"idle with no witnessed turn", Agent{State: rpc.StateIdle}, false, false},
		{"a subagent still running", finished, true, false},
		{"a loop waiting for its next wakeup", Agent{State: rpc.StateIdle, doneAt: time.Now(), loop: LoopState{Active: true}}, false, false},
		{"working again", Agent{State: rpc.StateWorking, doneAt: time.Now()}, false, false},
		{"blocked", Agent{State: rpc.StateBlocked, doneAt: time.Now()}, false, false},
	} {
		if got := turnDone(tc.a, tc.subRunning); got != tc.want {
			t.Errorf("%s: turnDone = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// finishedFleet is john having finished a turn this client watched start.
func finishedFleet() Fleet {
	return NewFleet().
		WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "john", State: rpc.StateIdle}}}).
		WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "john", State: rpc.StateWorking}}}).
		WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "john", State: rpc.StateIdle}}})
}

func rosterOf(f Fleet) string {
	return stripANSI(Roster{}.View(f.OnRoster(), f.RunningTasks, rosterWidth, 10))
}

// The roster draws a finished agent as ✔, not as the ○ of an idle one, and goes
// back to ○ the moment the agent says something new (notDone).
//
// Mutation check: have rowGlyph ignore done and the ✔ never draws.
func TestTheRosterMarksAnAgentThatFinishedItsTurnDone(t *testing.T) {
	f := finishedFleet()
	if row := rosterOf(f); !strings.Contains(row, turnDoneGlyph+" john") {
		t.Errorf("an agent that finished a witnessed turn is not marked done in the roster:\n%s", row)
	}
	f, _ = f.Observe(core.Event{Kind: core.KindAssistantText, Text: "one more thing"}, "s1")
	if row := rosterOf(f); strings.Contains(row, turnDoneGlyph) || !strings.Contains(row, glyphOf(rpc.StateIdle)+" john") {
		t.Errorf("an agent that spoke again after its turn still reads done:\n%s", row)
	}
}

// An idle agent whose turn this client never watched is idle, not done: the
// witnessed-only limit, which is the done line's own.
func TestAnIdleAgentWithNoWitnessedTurnIsNotDone(t *testing.T) {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "john", State: rpc.StateIdle}}})
	if row := rosterOf(f); strings.Contains(row, turnDoneGlyph) {
		t.Errorf("an agent this client never watched work was marked done:\n%s", row)
	}
}

// The strip counts done agents as their own cross-cutting figure and takes them
// out of idle, so the counts still sum to the fleet.
func TestTheStripCountsDoneAgentsApartFromIdle(t *testing.T) {
	agents := inState(rpc.StateIdle, 3)
	agents[0].doneAt = time.Now()
	out := stripANSI(awarenessStrip(agents, nil, "", 200))
	if !strings.Contains(out, turnDoneGlyph+" 1 done") {
		t.Errorf("the strip does not count the finished agent: %q", out)
	}
	if !strings.Contains(out, "2 "+stateLabel[rpc.StateIdle]) {
		t.Errorf("the strip counted a done agent under idle as well: %q", out)
	}
}

// And a running subagent withholds it on the strip too - the same predicate.
func TestTheStripDoesNotCountAnAgentWithARunningSubagentAsDone(t *testing.T) {
	agents := inState(rpc.StateIdle, 1)
	agents[0].doneAt = time.Now()
	running := func(string) []Task { return []Task{{Status: core.TaskRunning}} }
	if out := stripANSI(awarenessStrip(agents, running, "", 200)); strings.Contains(out, " done") {
		t.Errorf("an agent with a running subagent was counted done: %q", out)
	}
}

// The roster's ✔ and the DM's done line are one predicate: whenever the DM draws
// `✻ … done`, the roster says done, and never otherwise.
func TestTheRosterAndTheDMDoneLineAgree(t *testing.T) {
	f := finishedFleet()
	agent, _ := f.Agent("s1")
	dm := NewDM("s1", "john").WithRunningSub(len(f.RunningTasks("s1")) > 0)
	dm.Agent = agent
	if dm.showsDone() != f.done("s1") || !f.done("s1") {
		t.Errorf("the DM done line (%v) and the roster's done (%v) disagree for a finished agent", dm.showsDone(), f.done("s1"))
	}
	f, _ = f.Observe(core.Event{Kind: core.KindThinking, Text: "hm"}, "s1")
	agent, _ = f.Agent("s1")
	dm.Agent = agent
	if dm.showsDone() != f.done("s1") || f.done("s1") {
		t.Errorf("after new content the DM done line (%v) and the roster's done (%v) disagree", dm.showsDone(), f.done("s1"))
	}
}

// The mark is one column, like every state glyph, and none of them - nor a
// heartbeat frame: a glyph shared with either would say a done agent is
// something it is not.
func TestTheDoneGlyphIsOneColumnAndNoStateOrHeartbeatGlyph(t *testing.T) {
	if w := lipgloss.Width(turnDoneGlyph); w != 1 {
		t.Errorf("the done glyph is %d columns: a wide glyph shifts every name beside it", w)
	}
	for state, g := range stateGlyph {
		if g == turnDoneGlyph {
			t.Errorf("the done glyph is %q's glyph too", state)
		}
	}
	for _, g := range heartbeatFrames {
		if g == turnDoneGlyph {
			t.Errorf("the done glyph is a heartbeat frame, so a working row draws it too")
		}
	}
}

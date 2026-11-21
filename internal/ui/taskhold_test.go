package ui

// An agent's ending that is not its last: held until it is, or until it is clear
// it never will be. See taskhold.go.

import (
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// replayTasks folds events into an open conversation the way frames arrive, and
// reads the conversation back.
func replayTasks(t *testing.T, evs ...core.Event) (App, string) {
	t.Helper()
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withSize(120, 60)
	for _, ev := range evs {
		a = a.applyFrame(taskFrame("s1", ev))
	}
	return a, conversationRegion(t, *a.dms["s1"], 120, 60)
}

// agentEndings is every ending line a conversation draws for a subagent.
func agentEndings(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, taskLineGlyph+" "+taskLineAgent) {
			lines = append(lines, strings.TrimSpace(l))
		}
	}
	return lines
}

// owned is a task an agent started for itself: a shell, as the recordings show.
func owned(id, parent string) core.Event {
	return core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
		ID: id, Parent: parent, Dispatch: "toolu_" + id, Kind: core.TaskShell,
		Phase: core.TaskStarted, Status: core.TaskRunning, Label: "background shell",
	}}
}

// ending is a task_notification, the frame a line is drawn from.
func ending(id string, status core.TaskStatus, elapsed time.Duration) core.Event {
	return core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
		ID: id, Dispatch: "toolu_" + id, Kind: core.TaskKindUnknown, Phase: core.TaskEnded, Status: status, Elapsed: elapsed,
	}}
}

var turnEnd = core.Event{Kind: core.KindTurnEnd}

// agentWithShell is the 2.1.329 shape up to the agent's early ending: a subagent
// that has started a shell of its own, and then reports done.
func agentWithShell(label string, early time.Duration) []core.Event {
	return []core.Event{
		started("a1", "toolu_a1", label, "general-purpose", core.TaskAgent),
		owned("c1", "a1"),
		ending("a1", core.TaskDone, early),
	}
}

// withoutRestart is a recording of a subagent that hands back before its own
// shell ends, less the restart: the frames of the agent's task id from its second
// start on. The shell was killed, or claude never woke the agent - a case no
// recording has, synthesised from the one that has the rest.
func withoutRestart(t *testing.T, evs []core.Event) []core.Event {
	t.Helper()
	var agent string
	var out []core.Event
	starts := 0
	for _, ev := range evs {
		u := ev.Task
		if u != nil && u.Phase == core.TaskStarted && u.Kind == core.TaskAgent {
			agent = u.ID
			if starts++; starts == 2 {
				break
			}
		}
		out = append(out, ev)
	}
	if starts != 2 {
		t.Fatalf("the recording starts its agent %d times, want a restart to take out", starts)
	}
	rest := evs[len(out):]
	for _, ev := range rest {
		if ev.Task != nil && ev.Task.ID == agent {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// Whether an ending settles its dispatch is asked once. The frame's own words
// answer for the first shape (it says it has not reported yet: core reads it as
// progress); what the fold knows answers for the second (a task the agent owns is
// still running) - and for a killed one, the ending still draws, once.
func TestAnEndingDrawsOnlyWhenItSettlesTheDispatch(t *testing.T) {
	early := decodeWorkflowFixture(t, "subagent-handback-then-rewoken.jsonl")
	for name, tc := range map[string]struct {
		evs  []core.Event
		want time.Duration
	}{
		"a plain ending": {[]core.Event{
			started("a1", "toolu_a1", "Count lines", "general-purpose", core.TaskAgent),
			ending("a1", core.TaskDone, 24*time.Second),
		}, 24 * time.Second},
		"claude says the agent has not reported (2.1.322)": {
			decodeWorkflowFixture(t, "subagent-waits-background.jsonl"), 29272 * time.Millisecond},
		"a hand-back ahead of the agent's own shell (2.1.329)":            {early, 11375 * time.Millisecond},
		"the same, the shell killed and no restart (synthesised from it)": {withoutRestart(t, early), 3546 * time.Millisecond},
	} {
		t.Run(name, func(t *testing.T) {
			_, out := replayTasks(t, tc.evs...)
			lines := agentEndings(out)
			if len(lines) != 1 || !strings.Contains(lines[0], taskLineFinished+metaSep+elapsedText(tc.want)) {
				t.Errorf("%d ending lines %q, want one finished after %s:\n%s", len(lines), lines, elapsedText(tc.want), out)
			}
		})
	}
}

// The sidebar reads the rows, which a held ending does not touch: the agent's row
// leaves the list at its early ending and comes back at its restart, as it did.
func TestAHeldEndingLeavesTheSidebarAsItWas(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withSize(120, 60)
	rows := func() (agent, shell int) {
		for _, row := range a.fleet.RunningTasks("s1") {
			switch row.Kind {
			case core.TaskAgent:
				agent++
			case core.TaskShell:
				shell++
			}
		}
		return agent, shell
	}
	for _, ev := range decodeWorkflowFixture(t, "subagent-handback-then-rewoken.jsonl") {
		a = a.applyFrame(taskFrame("s1", ev))
		if u := ev.Task; u != nil && u.Phase == core.TaskEnded && u.Dispatch != "" && u.Elapsed == 3546*time.Millisecond {
			if agent, shell := rows(); agent != 0 || shell != 2 {
				t.Errorf("after the early ending the sidebar lists %d agents and %d shells, want none and the two shells", agent, shell)
			}
		}
	}
	if agent, shell := rows(); agent != 0 || shell != 0 {
		t.Errorf("at the end the sidebar lists %d agents and %d shells, want none", agent, shell)
	}
}

func TestAnEarlyEndingIsHeldUntilTheAgentReallyEnds(t *testing.T) {
	cases := map[string]struct {
		evs   []core.Event
		lines int // drawn after the early ending and what follows
	}{
		"nothing but the early ending": {agentWithShell("Count", 3*time.Second), 0},
		"the restart, then the real ending": {append(agentWithShell("Count", 3*time.Second),
			started("a1", "toolu_a1", "Count", "general-purpose", core.TaskAgent),
			ending("c1", core.TaskDone, 0),
			ending("a1", core.TaskDone, 11*time.Second)), 1},
		"the shell ending, no restart yet":         {append(agentWithShell("Count", 3*time.Second), ending("c1", core.TaskDone, 0)), 0},
		"a turn ending while the shell still runs": {append(agentWithShell("Count", 3*time.Second), turnEnd), 0},
		"a stopped agent is no early ending": {[]core.Event{
			started("a1", "toolu_a1", "Count", "general-purpose", core.TaskAgent),
			owned("c1", "a1"),
			ending("a1", core.TaskStopped, 3*time.Second)}, 1},
		"a later ending replaces the held one, no restart announced": {append(agentWithShell("Count", 3*time.Second),
			ending("c1", core.TaskDone, 0),
			ending("a1", core.TaskDone, 11*time.Second)), 1},
		"a shell that ended first holds nothing": {[]core.Event{
			started("a1", "toolu_a1", "Count", "general-purpose", core.TaskAgent),
			owned("c1", "a1"),
			ending("c1", core.TaskDone, 0),
			ending("a1", core.TaskDone, 3*time.Second)}, 1},
		"another agent's shell holds nothing": {[]core.Event{
			started("a1", "toolu_a1", "Count", "general-purpose", core.TaskAgent),
			owned("c2", "a2"),
			ending("a1", core.TaskDone, 3*time.Second)}, 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, out := replayTasks(t, tc.evs...)
			if n := len(agentEndings(out)); n != tc.lines {
				t.Errorf("%d ending lines, want %d:\n%s", n, tc.lines, out)
			}
		})
	}
}

// With no restart coming, the held ending is not lost: it is drawn, with its own
// usage, at the first thing that says so - and never twice.
func TestAHeldEndingDrawsWhenNoRestartIsComing(t *testing.T) {
	shellEnded := append(agentWithShell("Count", 3*time.Second), ending("c1", core.TaskDone, 0))
	drawn := func(t *testing.T, a App) []string {
		t.Helper()
		return agentEndings(conversationRegion(t, *a.dms["s1"], 120, 60))
	}
	for name, step := range map[string]func(App) App{
		"the session's next turn ends":         func(a App) App { return a.applyFrame(taskFrame("s1", turnEnd)) },
		"a report no longer lists the session": func(a App) App { return a.applyStatus(&rpc.Status{}) },
		"a report says it ended": func(a App) App {
			return a.applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateEnded}}})
		},
		"the client reattaches after a gap": func(a App) App { m, _ := a.reattached(reattachedMsg{}); return m.(App) },
	} {
		t.Run(name, func(t *testing.T) {
			a, _ := replayTasks(t, shellEnded...)
			if n := len(drawn(t, a)); n != 0 {
				t.Fatalf("%d ending lines before the trigger, want it held", n)
			}
			a = step(a)
			lines := drawn(t, a)
			if len(lines) != 1 || !strings.Contains(lines[0], taskLineFinished+metaSep+elapsedText(3*time.Second)) {
				t.Fatalf("%d ending lines %q after the trigger, want the early ending once", len(lines), lines)
			}
			if again := drawn(t, step(a)); len(again) != 1 {
				t.Errorf("%d lines after a second trigger, want it drawn once", len(again))
			}
		})
	}

	t.Run("a report that still lists the session releases nothing", func(t *testing.T) {
		a, _ := replayTasks(t, shellEnded...)
		a = a.applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateWorking}}})
		if n := len(drawn(t, a)); n != 0 {
			t.Errorf("%d ending lines, want it still held", n)
		}
	})
}

// A /clear empties the conversation the held ending belonged to, as it drops a
// finished row (Fleet.keepRunning): nothing draws into the new one.
func TestAHeldEndingIsDroppedByAClear(t *testing.T) {
	evs := append(agentWithShell("Count", 3*time.Second), core.Event{Kind: core.KindSessionReset},
		ending("c1", core.TaskDone, 0), turnEnd)
	_, out := replayTasks(t, evs...)
	if lines := agentEndings(out); len(lines) != 0 {
		t.Errorf("%d ending lines after a /clear, want none: %q", len(lines), lines)
	}
}

// Held endings come back in the order they were held, whatever else the map of
// agents does.
func TestHeldEndingsAreDrawnInTheOrderTheyWereHeld(t *testing.T) {
	evs := []core.Event{
		started("a1", "toolu_a1", "first", "general-purpose", core.TaskAgent),
		started("a2", "toolu_a2", "second", "general-purpose", core.TaskAgent),
		owned("c1", "a1"), owned("c2", "a2"),
		ending("a2", core.TaskDone, 2*time.Second),
		ending("a1", core.TaskDone, 1*time.Second),
		ending("c1", core.TaskDone, 0), ending("c2", core.TaskDone, 0),
		turnEnd,
	}
	_, out := replayTasks(t, evs...)
	lines := agentEndings(out)
	if len(lines) != 2 || !strings.Contains(lines[0], `"second"`) || !strings.Contains(lines[1], `"first"`) {
		t.Errorf("endings drawn %q, want second then first, the order they were held", lines)
	}
}

// An ending that is not `completed` is never held and ends the hold, so what draws is
// the stop - once, and not the `finished` it follows.
func TestAStoppedEndingAfterAHeldOneIsTheOneDrawn(t *testing.T) {
	_, out := replayTasks(t, append(agentWithShell("Count", 3*time.Second),
		ending("a1", core.TaskStopped, 5*time.Second), ending("c1", core.TaskDone, 0), turnEnd)...)
	lines := agentEndings(out)
	if len(lines) != 1 || !strings.Contains(lines[0], taskLineHalted+metaSep+elapsedText(5*time.Second)) {
		t.Errorf("%d ending lines %q, want the stop once", len(lines), lines)
	}
}

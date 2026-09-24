package ui

// x then ↵ stops the open run: the arm, what cancels it, what the ending does
// to it, and the one frame the confirm writes.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const stopCue = "↵ stop count-lines · any key cancels"

// stopsWritten is the FrameStopRun frames a command tree wrote.
func stopsWritten(t *testing.T, a App, cmd tea.Cmd) []rpc.Frame {
	t.Helper()
	var out []rpc.Frame
	for _, f := range writtenFrames(t, a, cmd) {
		if f.Kind == rpc.FrameStopRun {
			out = append(out, f)
		}
	}
	return out
}

// drawnView is the view as its pane draws it, title row first.
func drawnView(t *testing.T, a App) []string {
	t.Helper()
	w, h, ok := a.paneSize(a.workflow.view.Pane)
	if !ok {
		t.Fatal("the view's pane is not drawn")
	}
	return wfLines(a.workflowPane(w, h))
}

func armed(t *testing.T, a App) App {
	t.Helper()
	a, cmd := pressKey(a, wfRune('x'))
	if !a.workflow.view.Armed {
		t.Fatalf("x on a running run did not arm the stop: %+v", a.workflow.view)
	}
	if got := stopsWritten(t, a, cmd); len(got) != 0 {
		t.Fatalf("x alone wrote %d stops, want none until ↵", len(got))
	}
	return a
}

func TestXArmsTheStopAndTheKeyLineSaysSo(t *testing.T) {
	a := armed(t, runOpen(t))
	if keys := keyRow(drawnView(t, a)); keys != stopCue {
		t.Errorf("the armed key line is %q, want %q", keys, stopCue)
	}
}

func TestEnterOnAnArmedStopWritesOneStopAndDisarms(t *testing.T) {
	a := armed(t, runOpen(t))
	a, cmd := pressKey(a, wfKey(tea.KeyEnter))
	got := stopsWritten(t, a, cmd)
	if len(got) != 1 {
		t.Fatalf("↵ on the armed stop wrote %d stops, want 1", len(got))
	}
	if f := got[0]; f.SessionID != "s1" || f.Workflow == nil || f.Workflow.Task != wfTask {
		t.Errorf("the stop is %+v, want alex's count-lines by task id", f)
	}
	if v := a.workflow.view; v.Armed || !v.Open() || v.Level != levelRun {
		t.Errorf("after the stop the view is %+v, want it disarmed and still on the run", v)
	}
}

// The room's view names no agent of its own: the stop goes to the run's.
func TestAStopFromTheRoomsViewGoesToTheRunsAgent(t *testing.T) {
	a, _ := openedWorkflows(t, workflowFleet(t))
	if v := a.workflow.view; v.Pane != "" || v.Level != levelRun {
		t.Fatalf("the room's one run did not open at its run level: %+v", v)
	}
	a, cmd := pressKey(armed(t, a), wfKey(tea.KeyEnter))
	if got := stopsWritten(t, a, cmd); len(got) != 1 || got[0].SessionID != "s1" {
		t.Errorf("the room's stop wrote %+v, want one addressed to alex", got)
	}
}

func TestAnyOtherKeyDisarmsWithoutWritingAndDoesNothingElse(t *testing.T) {
	for name, k := range map[string]tea.KeyMsg{
		"↓": wfKey(tea.KeyDown), "esc": wfKey(tea.KeyEsc), "x": wfRune('x'), "f": wfRune('f'), "s": wfRune('s'),
	} {
		a := armed(t, runOpen(t))
		before := a.workflow.view
		a, cmd := pressKey(a, k)
		if got := stopsWritten(t, a, cmd); len(got) != 0 {
			t.Errorf("%s on the armed stop wrote %d stops", name, len(got))
		}
		before.Armed = false
		if a.workflow.view != before {
			t.Errorf("%s on the armed stop left %+v, want only the arm taken back: %+v", name, a.workflow.view, before)
		}
		if keys := keyRow(drawnView(t, a)); strings.Contains(keys, "↵ stop") {
			t.Errorf("%s left the armed cue drawn: %q", name, keys)
		}
	}
}

// ⌃C keeps its meaning under an arm: it closes the view and parks.
func TestCtrlCOnAnArmedStopClosesTheViewAndParks(t *testing.T) {
	a, cmd := pressKey(armed(t, runOpen(t)), wfKey(tea.KeyCtrlC))
	if a.workflow.view.Open() {
		t.Error("⌃C left the view open")
	}
	if _, parking := a.parking["s1"]; !parking {
		t.Error("⌃C under the arm did not park alex")
	}
	if got := stopsWritten(t, a, cmd); len(got) != 0 {
		t.Errorf("⌃C under the arm wrote %d stops", len(got))
	}
}

func TestXOnAnEndedRunDoesNothing(t *testing.T) {
	a := runOpen(t).applyFrame(taskFrame("s1", ended(wfTask, core.TaskDone)))
	a, cmd := pressKey(a, wfRune('x'))
	if a.workflow.view.Armed {
		t.Error("x on an ended run armed a stop")
	}
	a, more := pressKey(a, wfKey(tea.KeyEnter))
	if got := stopsWritten(t, a, tea.Batch(cmd, more)); len(got) != 0 {
		t.Errorf("x then ↵ on an ended run wrote %d stops", len(got))
	}
}

// Review Focus 2: the run ends under the arm. The view stays on the ended run
// showing its final state, the arm is gone, and ↵ writes nothing.
func TestTheRunEndingUnderTheArmDisarmsIt(t *testing.T) {
	a := armed(t, runOpen(t))
	a = a.applyFrame(taskFrame("s1", ended(wfTask, core.TaskStopped)))
	v := a.workflow.view
	if v.Armed {
		t.Fatal("the run's ending left the stop armed")
	}
	if !v.Open() || v.Level != levelRun || v.Task != wfTask {
		t.Errorf("the ending moved the view to %+v, want it on the ended run", v)
	}
	lines := drawnView(t, a)
	if keys := keyRow(lines); strings.Contains(keys, "stop") {
		t.Errorf("the ended run still offers a stop: %q", keys)
	}
	if !strings.Contains(lines[1], string(core.TaskStopped)) {
		t.Errorf("the header does not show the run's final state: %q", lines[1])
	}
	a, cmd := pressKey(a, wfKey(tea.KeyEnter))
	if got := stopsWritten(t, a, cmd); len(got) != 0 {
		t.Errorf("↵ after the ending wrote %d stops", len(got))
	}
}

// A /clear takes the run away with no task frame of its own, so no ending folds
// in: the next key finds the arm stale, writes nothing, and does its own job
// rather than being spent cancelling an arm nobody can see.
func TestARunClearedUnderTheArmWritesNothingAndSwallowsNoKey(t *testing.T) {
	cleared := func() App {
		return armed(t, runOpen(t)).applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1",
			Event: &core.Event{Kind: core.KindSessionReset, SessionID: "s1"}})
	}
	a := cleared()
	if keys := keyRow(drawnView(t, a)); strings.Contains(keys, "stop") {
		t.Errorf("a cleared run still draws a stop: %q", keys)
	}
	a, cmd := pressKey(a, wfKey(tea.KeyEnter))
	if got := stopsWritten(t, a, cmd); len(got) != 0 {
		t.Errorf("↵ after the clear wrote %d stops", len(got))
	}
	if a, _ = pressKey(cleared(), wfKey(tea.KeyEsc)); a.workflow.view.Open() {
		t.Errorf("esc on the cleared run was swallowed: the view is %+v, want it closed", a.workflow.view)
	}
}

// Another agent's ending is not this run's: the arm stands.
func TestAnotherRunsEndingLeavesTheArm(t *testing.T) {
	a := armed(t, runOpen(t))
	a = a.applyFrame(taskFrame("s2", workflowStarted("w9", "toolu_w9", "Lint", "lint-all")))
	a = a.applyFrame(taskFrame("s2", ended("w9", core.TaskDone)))
	if !a.workflow.view.Armed {
		t.Error("another agent's run ending took this arm back")
	}
}

// A press on the view is an input that is not the confirm, so it takes the arm
// back - the card keys' rule, one surface over.
func TestAPressOnTheViewDisarms(t *testing.T) {
	a := armed(t, runOpen(t))
	x, y, ok := viewCell(a, "2 Sum")
	if !ok {
		t.Fatalf("the Sum phase is not on screen:\n%s", stripANSI(a.View()))
	}
	a, _ = a.mouse(pressAt(x, y))
	if a.workflow.view.Armed {
		t.Error("a press on the view left the stop armed")
	}
}

// Through Update, where every mouse message takes every arm back first, a press
// on the view still does the view's own job: the row under it is the cursor.
func TestAPressOnTheViewThroughUpdateStillSelectsItsRow(t *testing.T) {
	a := armed(t, runOpen(t))
	x, y, ok := viewCell(a, "2 Sum")
	if !ok {
		t.Fatalf("the Sum phase is not on screen:\n%s", stripANSI(a.View()))
	}
	m, _ := a.Update(pressAt(x, y))
	a = m.(App)
	if v := a.workflow.view; v.Armed || !v.Open() || v.Column != 0 || v.Cursor != 1 || a.focus != "s1" {
		t.Errorf("a press on the view left %+v with the keys on %q, want it disarmed on the Sum phase in alex's pane",
			v, a.focus)
	}
}

// requireDisarmedOnReturn holds a view the keys have come back to: no cue
// drawn, and ↵ is the view's own - it opens, and stops nothing.
func requireDisarmedOnReturn(t *testing.T, a App, how string) {
	t.Helper()
	if a.focus != "s1" || !a.workflow.view.Open() {
		t.Fatalf("%s: the keys are on %q with the view %+v, want them back on alex's open view", how, a.focus, a.workflow.view)
	}
	if keys := keyRow(drawnView(t, a)); strings.Contains(keys, "↵ stop") {
		t.Errorf("%s: the cue is still drawn: %q", how, keys)
	}
	a, cmd := pressKey(a, wfKey(tea.KeyEnter))
	if got := stopsWritten(t, a, cmd); len(got) != 0 {
		t.Errorf("%s: a ↵ after coming back wrote %d stops - the run was stopped by a key meant to open", how, len(got))
	}
}

// The reviewer's probe: x arms, a click takes the keys to the room, something
// is typed and cleared there, and ⇥ comes back. Every one of those was an input
// that was not the confirm, so the ↵ that follows opens rather than stops.
func TestAnArmLeftForAnotherPaneIsGoneOnReturn(t *testing.T) {
	a := armed(t, runOpen(t))
	r := a.regions()
	m, _ := a.Update(pressAt(midOf(r, 0), textRow))
	m, _ = m.(App).Update(tea.MouseMsg{Action: tea.MouseActionRelease, X: midOf(r, 0), Y: textRow})
	a = m.(App)
	if a.focus != "" {
		t.Fatalf("the click left the keys on %q, want the room", a.focus)
	}
	a = a.withDraft("hi")
	a, _ = pressKey(a, wfKey(tea.KeyEsc))
	a, _ = pressKey(a, wfKey(tea.KeyTab))
	requireDisarmedOnReturn(t, a, "a click away, typing, ⇥ back")
}

// A wheel over the room moves no keys, so the focus never leaves the view - but
// it is still an input that is not the confirm, and it takes the arm back.
func TestAWheelOverAnotherPaneTakesTheArmBack(t *testing.T) {
	a := armed(t, runOpen(t))
	r := a.regions()
	m, _ := a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: midOf(r, 0), Y: textRow})
	requireDisarmedOnReturn(t, m.(App), "a wheel over the room")
}

// ⇧←→ are the view's while it holds the keys, so they take the arm back like
// any other key rather than moving it with the focus.
func TestShiftArrowsUnderTheArmTakeItBack(t *testing.T) {
	a := armed(t, runOpen(t))
	a, _ = pressKey(a, wfKey(tea.KeyShiftRight))
	a, _ = pressKey(a, wfKey(tea.KeyShiftLeft))
	requireDisarmedOnReturn(t, a, "⇧→ then ⇧←")
}

// A focus move no key or click made - an arriving spawn or fork takes the keys
// through refocus - still leaves the pane, so it takes the arm back too.
func TestAFocusMoveNoInputMadeTakesTheArmBack(t *testing.T) {
	a := armed(t, runOpen(t))
	a = a.refocus("s2").refocus("s1")
	requireDisarmedOnReturn(t, a, "the keys moved away and back")
}

func TestTheArmedKeyLineRendersWithinThePane(t *testing.T) {
	v := runLevel(midRun())
	v.Armed = true
	for _, w := range []int{1, 8, 20, 60} {
		for _, h := range []int{1, 3, 7, 20} {
			requireFits(t, v.render([]workflowRunView{midRun()}, w, h), w, h)
		}
	}
	if keys := keyRow(wfLines(v.render([]workflowRunView{midRun()}, 90, 20))); keys != stopCue {
		t.Errorf("the armed key line is %q, want %q", keys, stopCue)
	}
}

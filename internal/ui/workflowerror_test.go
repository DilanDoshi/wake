package ui

// A workflow whose agents failed, drawn from the recording of one
// (workflow-agent-error.jsonl): "bad model" started and failed, "bad schema"
// was refused before it started - no agentId - and "fine" finished.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const errTask = "wppv13f2b"

// erroredSnap is the recording's last snapshot, decoded as the daemon hands it on.
func erroredSnap(t *testing.T) core.WorkflowSnapshot {
	t.Helper()
	var last *core.WorkflowSnapshot
	for _, ev := range decodeWorkflowFixture(t, "workflow-agent-error.jsonl") {
		if ev.Task != nil && ev.Task.Workflow != nil && ev.Task.Workflow.Progress != nil {
			last = ev.Task.Workflow.Progress
		}
	}
	if last == nil || len(last.Agents) != 3 {
		t.Fatalf("workflow-agent-error.jsonl: last snapshot %+v, want three agents", last)
	}
	return *last
}

// erroredRun is alex's pane at the run level on the recorded two-fail run.
func erroredRun(t *testing.T) App {
	t.Helper()
	a := newRoomApp(t).withRoster(rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateWorking}).withSize(160, 40)
	a = a.applyFrame(taskFrame("s1", workflowStarted(errTask, "toolu_err", "Two agents fail", "two-fail")))
	a = a.applyFrame(taskFrame("s1", workflowProgressed(errTask, "toolu_err", erroredSnap(t))))
	a, _ = openedWorkflows(t, a.openDMWith("s1", "alex"))
	if v := a.workflow.view; v.Level != levelRun || v.Task != errTask {
		t.Fatalf("the errored run did not open at its run level: %+v", v)
	}
	a, _ = pressKey(a, wfKey(tea.KeyRight)) // onto the agents
	return a
}

func TestAFailedAgentDrawsACrossAndLeavesItsPhaseUnfinished(t *testing.T) {
	lines := wfLines(erroredRun(t).View())
	for _, want := range []string{"✗ bad model", "✗ bad schema", "✔ fine"} {
		if _, ok := wfLine(lines, want); !ok {
			t.Errorf("the agents column does not draw %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	if _, ok := wfLine(lines, "1 Try 1/3"); !ok {
		t.Errorf("the Try phase is not drawn by its number: two of its agents failed\n%s", strings.Join(lines, "\n"))
	}
}

func TestTheFailedFilterShowsTheFailedAgents(t *testing.T) {
	a := erroredRun(t)
	for range 3 { // all → running → done → failed
		a, _ = pressKey(a, wfRune('f'))
	}
	lines := wfLines(a.View())
	if _, ok := wfLine(lines, "fine"); ok {
		t.Errorf("the failed filter shows the agent that finished:\n%s", strings.Join(lines, "\n"))
	}
	for _, want := range []string{"✗ bad model", "✗ bad schema"} {
		if _, ok := wfLine(lines, want); !ok {
			t.Errorf("the failed filter hides %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
}

// enterErrored opens the agent on row (0 bad model, 1 bad schema) and returns
// the drawn lines and every frame the drill wrote, the ask drain included.
func enterErrored(t *testing.T, row int) ([]string, []rpc.Frame) {
	t.Helper()
	a := erroredRun(t)
	for range row {
		a, _ = pressKey(a, wfKey(tea.KeyDown))
	}
	a, cmd := pressKey(a, wfKey(tea.KeyEnter))
	if a.workflow.view.Level != levelAgent {
		t.Fatalf("↵ left %+v, want the agent level", a.workflow.view)
	}
	frames := writtenFrames(t, a, cmd)
	a, more := agentAsks(t, a)
	return wfLines(a.View()), append(frames, more...)
}

func TestAFailedAgentsOutcomeIsItsError(t *testing.T) {
	lines, _ := enterErrored(t, 0)
	if _, ok := wfLine(lines, "✗ Failed"); !ok {
		t.Errorf("the agent level does not say it failed:\n%s", strings.Join(lines, "\n"))
	}
	if _, ok := wfLine(lines, "Outcome"); !ok {
		t.Errorf("a failed agent with no result draws no Outcome:\n%s", strings.Join(lines, "\n"))
	}
	if _, ok := wfLine(lines, "There's an issue with the selected model"); !ok {
		t.Errorf("the Outcome does not carry the agent's error:\n%s", strings.Join(lines, "\n"))
	}
}

// An agent refused before it started has no id, so there is no transcript to
// ask for: its detail is the snapshot's own prompt and error.
func TestAnAgentThatNeverStartedAsksForNothing(t *testing.T) {
	lines, frames := enterErrored(t, 1)
	if asks := kindsFor(frames, rpc.FrameWorkflowAgent); len(asks) != 0 {
		t.Errorf("entering an agent with no id wrote %d FrameWorkflowAgent", len(asks))
	}
	for _, want := range []string{"Prompt", "Reply with only: hi", "Activity unavailable", "Outcome", "unusable JSON Schema"} {
		if _, ok := wfLine(lines, want); !ok {
			t.Errorf("the never-started agent does not draw %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
}

// A word no recording has carried is drawn as itself, never as "unknown".
func TestAnUnrecordedStateDrawsItsOwnWord(t *testing.T) {
	ag := core.WorkflowAgent{Index: 1, Phase: 1, Label: "later", State: core.WorkflowAgentUnknown, StateWord: "queued"}
	run := midRun()
	run.Snap = core.WorkflowSnapshot{Phases: []core.WorkflowPhase{{Index: 1, Title: "Try"}}, Agents: []core.WorkflowAgent{ag}}
	row := strings.Join(wfLines(runLevel(run).render([]workflowRunView{run}, 120, 20)), "\n")
	detail := strings.Join(wfLines(layAgent(ag, nil, false, 90).render(90, 20, 0)), "\n")
	for name, out := range map[string]string{"the agents column": row, "the agent level": detail} {
		if !strings.Contains(out, "queued") || strings.Contains(out, "unknown") {
			t.Errorf("%s does not name the word itself:\n%s", name, out)
		}
	}
}

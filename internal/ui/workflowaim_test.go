package ui

// /workflows aimed at one agent: `@alex /workflows` in the room, and
// `/workflows @alex` typed anywhere, open alex's runs in the pane it was typed in.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// aimedWorkflows submits draft from a's composer and returns the App and what it wrote.
func aimedWorkflows(t *testing.T, a App, draft string) (App, []rpc.Frame) {
	t.Helper()
	m, cmd := a.withDraft(draft).submit(false)
	next := m.(App)
	return next, writtenFrames(t, next, cmd)
}

// The reported bug: a room mention aimed /workflows at nobody - the literal
// text reached alex as a message. It opens alex's runs in the room's pane,
// asks for alex's alone, and the title says whose they are.
func TestAMentionAimsWorkflowsAtOneAgentFromTheRoom(t *testing.T) {
	a, frames := aimedWorkflows(t, workflowFleet(t), "@alex /workflows")
	v := a.workflow.view
	if !v.Open() || v.Pane != "" || v.Session != "s1" {
		t.Fatalf("@alex /workflows opened %+v, want alex's runs in the room's pane", v)
	}
	if got := kindsFor(frames, rpc.FrameWorkflows); len(got) != 1 || got[0] != "s1" {
		t.Errorf("@alex /workflows asked for runs of %v, want exactly [s1]", got)
	}
	if got := kindsFor(frames, rpc.FrameSend); len(got) != 0 {
		t.Errorf("@alex /workflows was also sent to %v as a message", got)
	}
	if !strings.Contains(shown(a), "@alex"+workflowTitleSuffix) {
		t.Errorf("the room's view does not name whose runs it draws:\n%s", shown(a))
	}
	if v.Level != levelRun || v.Task != wfTask {
		t.Errorf("alex's one run did not open its run level: %+v", v)
	}
}

// Typed by hand the target reads the same, in a conversation too: /mcp's rule.
func TestWorkflowsAtAnotherAgentOpensItsRunsInThisPane(t *testing.T) {
	a, frames := aimedWorkflows(t, workflowFleet(t).openDMWith("s2", "sydney"), "/workflows @alex")
	if v := a.workflow.view; !v.Open() || v.Pane != "s2" || v.Session != "s1" {
		t.Fatalf("/workflows @alex in sydney's pane opened %+v, want alex's runs there", v)
	}
	if got := kindsFor(frames, rpc.FrameWorkflows); len(got) != 1 || got[0] != "s1" {
		t.Errorf("/workflows @alex asked for runs of %v, want exactly [s1]", got)
	}
	if !strings.Contains(shown(a), "@alex"+workflowTitleSuffix) {
		t.Errorf("sydney's pane does not say it is drawing alex's runs:\n%s", shown(a))
	}
}

func TestWorkflowsAtNobodyIsRefusedByName(t *testing.T) {
	for _, draft := range []string{"/workflows @nobody", "/workflows @alex now", "/workflows now"} {
		t.Run(draft, func(t *testing.T) {
			notice.Reset()
			a, frames := aimedWorkflows(t, workflowFleet(t), draft)
			if a.workflow.view.Open() || len(frames) != 0 {
				t.Errorf("%q opened %+v and wrote %+v, want a refusal", draft, a.workflow.view, frames)
			}
			if n, ok := notice.Latest(); !ok || !strings.Contains(n.String(), "@") {
				t.Errorf("%q was refused as %q, want a sentence naming the @who form", draft, n.String())
			}
		})
	}
}

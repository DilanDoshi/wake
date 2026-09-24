package rpc

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

func TestValidWorkflowNameAcceptsAWorkflowFileName(t *testing.T) {
	for _, name := range []string{"count-lines", "deep_research2", "a216187ba41a1087e"} {
		if err := ValidWorkflowName(name); err != nil {
			t.Errorf("ValidWorkflowName(%q) refused a valid name: %v", name, err)
		}
	}
}

func TestValidWorkflowNameRefusesWhatIsNotOneFileSegment(t *testing.T) {
	for _, bad := range []string{"", "../x", "a/b", ".hidden", "Upper", "x y", strings.Repeat("a", maxWorkflowName+1)} {
		if err := ValidWorkflowName(bad); err == nil {
			t.Errorf("ValidWorkflowName(%q) was accepted; a workflow name is one lower-case file segment", bad)
		}
	}
}

func TestValidWorkflowAgentIDAcceptsClaudesOwnShape(t *testing.T) {
	for _, id := range []string{"a216187ba41a1087e", "abc123", "x"} {
		if err := ValidWorkflowAgentID(id); err != nil {
			t.Errorf("ValidWorkflowAgentID(%q) refused a valid id: %v", id, err)
		}
	}
}

// Refuses everything ValidWorkflowName refuses, plus the two workflow-name
// examples that carry a dash or underscore - punctuation no recorded
// workflow agent id has ever carried.
func TestValidWorkflowAgentIDRefusesWhatIsNotLowerCaseAlphanumeric(t *testing.T) {
	for _, bad := range []string{
		"", "../x", "a/b", ".hidden", "Upper", "x y", strings.Repeat("a", maxWorkflowAgentID+1),
		"count-lines", "deep_research2",
	} {
		if err := ValidWorkflowAgentID(bad); err == nil {
			t.Errorf("ValidWorkflowAgentID(%q) was accepted; an agent id is lower-case alphanumeric only", bad)
		}
	}
}

// A WorkflowFrame survives the wire exactly as Frame.Event and Frame.Status
// already do - WriteFrameTo over a real net.Conn (net.Pipe), read back with
// ReadFrames, the transport this package's clients actually use.
func TestWorkflowFrameRoundTrips(t *testing.T) {
	mine, theirs := net.Pipe()
	t.Cleanup(func() { _ = mine.Close(); _ = theirs.Close() })

	want := Frame{
		Kind: FrameWorkflowsReply,
		Workflow: &WorkflowFrame{
			Runs: []core.WorkflowRun{
				{TaskID: "wsmc7r0xw", Name: "count-lines", Status: core.TaskDone, Tokens: 15194},
			},
		},
	}

	frames, errs := drained(t, theirs)
	go func() {
		if err := WriteFrameTo(mine, want); err != nil {
			t.Errorf("WriteFrameTo: %v", err)
		}
	}()

	select {
	case got := <-frames:
		if got.Kind != want.Kind {
			t.Fatalf("Kind = %q, want %q", got.Kind, want.Kind)
		}
		if got.Workflow == nil || len(got.Workflow.Runs) != 1 {
			t.Fatalf("Workflow = %+v, want one run", got.Workflow)
		}
		run := got.Workflow.Runs[0]
		if run.TaskID != "wsmc7r0xw" || run.Name != "count-lines" || run.Status != core.TaskDone || run.Tokens != 15194 {
			t.Fatalf("run = %+v, not round-tripped", run)
		}
	case err := <-errs:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(recvTimeout):
		t.Fatal("frame never arrived")
	}
}

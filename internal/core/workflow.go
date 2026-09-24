package core

// A dynamic Workflow() run's own vocabulary - the shape TaskUpdate.Workflow
// carries when a dispatch is a workflow rather than a subagent or a shell.
// See docs/superpowers/notes/2026-09-23-workflow-findings.md for the wire
// this resolves.
//
// It is not an airlock file. It holds no wire spelling and no JSON tags read
// from Claude - workflowSnapshotOf and workflowOf in encode.go do that
// reading and build these values.

import "time"

// WorkflowUpdate is a workflow task's own half of a TaskUpdate.
//
// Every field is what its frame carried: Name and Script arrive once, on
// task_started; Progress is the latest task_progress snapshot, replaced
// wholesale each time rather than merged, because Claude sends the whole
// state rather than a delta; Error arrives on the task_updated that ends a
// failed run.
type WorkflowUpdate struct {
	Name     string            `json:"name,omitempty"`     // meta.name, on task_started
	Script   string            `json:"script,omitempty"`   // the script, on task_started
	Error    string            `json:"error,omitempty"`    // a failed run's error, on task_updated
	Progress *WorkflowSnapshot `json:"progress,omitempty"` // latest snapshot, on task_progress
}

// WorkflowSnapshot is one task_progress's workflow_progress array, resolved:
// every phase the script has declared, and every agent it has started so
// far - a full replacement each time, never a delta.
type WorkflowSnapshot struct {
	Phases []WorkflowPhase `json:"phases,omitempty"`
	Agents []WorkflowAgent `json:"agents,omitempty"`
}

// Done counts the agents that have finished. Phases run in order but the
// agents inside one can run in parallel, so this is what a header's "N/M
// agents" reads rather than a count derived from Phase.
func (s WorkflowSnapshot) Done() int {
	n := 0
	for _, a := range s.Agents {
		if a.State == WorkflowAgentDone {
			n++
		}
	}
	return n
}

// PhaseAgents returns the agents of one phase, in the snapshot's own arrival
// order.
func (s WorkflowSnapshot) PhaseAgents(phase int) []WorkflowAgent {
	var out []WorkflowAgent
	for _, a := range s.Agents {
		if a.Phase == phase {
			out = append(out, a)
		}
	}
	return out
}

// WorkflowPhase is one phase() the script declared - a workflow_phase
// entry's 1-based index and title.
type WorkflowPhase struct {
	Index int    `json:"index"`
	Title string `json:"title"`
}

// WorkflowAgentState is a workflow_agent entry's state, resolved from
// Claude's own words. Only "start" and "done" are recorded; failed is the
// SDK's documented word for a run this corpus has not seen fail an
// individual agent, and unknown is every other word - see
// workflowAgentStates.
type WorkflowAgentState string

const (
	WorkflowAgentRunning WorkflowAgentState = "running"
	WorkflowAgentDone    WorkflowAgentState = "done"
	WorkflowAgentFailed  WorkflowAgentState = "failed"
	WorkflowAgentUnknown WorkflowAgentState = "unknown"
)

// WorkflowAgent is one workflow_agent entry: one agent() call inside the
// script, at whatever state its latest snapshot reported.
//
// AgentID is the only decoded route to that agent's own words: a workflow
// agent forwards nothing (no parent_tool_use_id on either recording), so its
// transcript lives only on disk, keyed by this id.
type WorkflowAgent struct {
	Index     int                `json:"index"`
	Phase     int                `json:"phase"`
	Label     string             `json:"label"`
	AgentID   string             `json:"agent"`
	Model     string             `json:"model,omitempty"`
	State     WorkflowAgentState `json:"state"`
	Attempt   int                `json:"attempt,omitempty"`
	Tokens    int                `json:"tokens,omitempty"`
	ToolCalls int                `json:"tool_calls,omitempty"`
	Duration  time.Duration      `json:"duration,omitempty"`
	Prompt    string             `json:"prompt,omitempty"` // promptPreview
	Result    string             `json:"result,omitempty"` // resultPreview
}

// WorkflowRun is one finished (or killed) run, read back off its own
// wf_*.json record on disk - a second source from the live task_progress
// snapshot, for a run whose session has moved on or ended. DecodeWorkflowRun
// in encode.go builds one; internal/daemon reads the file.
type WorkflowRun struct {
	TaskID   string            `json:"task"`
	Name     string            `json:"name"`
	Summary  string            `json:"summary,omitempty"`
	Status   TaskStatus        `json:"status"`
	Error    string            `json:"error,omitempty"`
	Started  time.Time         `json:"started"`
	Duration time.Duration     `json:"duration,omitempty"`
	Tokens   int               `json:"tokens,omitempty"`
	Script   string            `json:"script,omitempty"`
	Progress *WorkflowSnapshot `json:"progress,omitempty"`
}

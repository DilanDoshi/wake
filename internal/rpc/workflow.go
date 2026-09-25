package rpc

// A dynamic Workflow() run's own frames and their fence, declared here
// rather than in wire.go for team.go's reason: wire.go is at the file-size
// hard max, so the constants sit with the payload type and the functions
// that check it.
//
// FrameStopRun ends a running workflow dispatch.
// FrameWorkflows/FrameWorkflowsReply ask for and answer a session's own
// runs - core.WorkflowRun read back off disk, on WorkflowFrame.Runs.
// FrameWorkflowAgent/FrameWorkflowAgentReply ask for and answer one workflow
// agent's own transcript, named "workflow_transcript" on the wire rather
// than Claude's own "workflow_agent" - internal/core/airlock_test.go polices
// that word, and this package is not one of the four airlock files, so it
// may not spell it. FrameSaveWorkflow/FrameWorkflowSaved write a run's
// script to disk and confirm the path it landed at.

import (
	"fmt"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	FrameStopRun            = "stop_run"                  // client → daemon: stop a running workflow (WorkflowFrame.Task)
	FrameWorkflows          = "workflows"                 // client → daemon: this session's runs on disk
	FrameWorkflowsReply     = "workflows_reply"           // daemon → client: WorkflowFrame.Runs
	FrameWorkflowAgent      = "workflow_transcript"       // client → daemon: one agent's transcript (WorkflowFrame.Agent)
	FrameWorkflowAgentReply = "workflow_transcript_reply" // daemon → client: Frame.Events, WorkflowFrame.Agent echoed
	FrameSaveWorkflow       = "save_workflow"             // client → daemon: WorkflowFrame{Task, Name, Scope}
	FrameWorkflowSaved      = "workflow_saved"            // daemon → client: WorkflowFrame.Path
)

// ScopeProject and ScopeUser are the two places FrameSaveWorkflow may write a
// script: .claude/workflows/<name>.js under the session's own directory, or
// the operator's own ~/.claude/workflows/<name>.js. ScopeUser is spelled
// "personal" on the wire rather than Claude's own "user" - the message
// role's word, policed by internal/core/airlock_test.go - because the two
// ideas share nothing but an English word and this package may not spell
// Claude's own.
const (
	ScopeProject = "project"
	ScopeUser    = "personal"
)

// WorkflowFrame is the payload every frame above carries, on rpc.Frame.Workflow.
// Every field belongs to one or two of the seven kinds, said on the field.
type WorkflowFrame struct {
	// Task is a workflow dispatch's own id - FrameStopRun's target, and
	// FrameSaveWorkflow's source (the dispatch whose script is being saved).
	Task string `json:"task,omitempty"`

	// Agent is one workflow agent's own id, FrameWorkflowAgent's target and
	// FrameWorkflowAgentReply's echo of it - core.WorkflowAgent.AgentID, the
	// only decoded route to that agent's own transcript, since a workflow
	// agent forwards nothing.
	Agent string `json:"agent,omitempty"`

	// Name is the file name FrameSaveWorkflow writes under, fenced by
	// ValidWorkflowName.
	Name string `json:"name,omitempty"`

	// Scope is ScopeProject or ScopeUser, FrameSaveWorkflow only.
	Scope string `json:"scope,omitempty"`

	// Path is where FrameSaveWorkflow's script landed, FrameWorkflowSaved
	// only - the wire carries a name and a scope and the daemon owns the
	// directory (--debug-file's own ruling), so the path is its answer.
	Path string `json:"path,omitempty"`

	// Runs is a session's own workflow runs read back off disk,
	// FrameWorkflowsReply only.
	Runs []core.WorkflowRun `json:"runs,omitempty"`
}

// maxWorkflowName bounds a workflow's file name: one path segment under
// .claude/workflows or ~/.claude/workflows.
const maxWorkflowName = 64

// ValidWorkflowName refuses every name that would not be one file under a
// workflows directory: lower-case letters and digits, with dash and
// underscore allowed after the first character - closed on purpose, the way
// ValidWorktreeName's set is, because what is not obviously safe is refused.
func ValidWorkflowName(name string) error {
	if name == "" {
		return fmt.Errorf("a workflow needs a name")
	}
	if len(name) > maxWorkflowName {
		return fmt.Errorf("a workflow name is at most %d characters, got %d", maxWorkflowName, len(name))
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case i > 0 && (r == '-' || r == '_'):
		default:
			return fmt.Errorf("%q cannot be a workflow name: it becomes one file under a workflows directory, "+
				"so lower-case letters and digits, with dash and underscore after the first character", name)
		}
	}
	return nil
}

// maxWorkflowAgentID bounds an id this package never generates - Claude's
// own workflow agent id, read back off a task_progress snapshot.
const maxWorkflowAgentID = 64

// ValidWorkflowAgentID refuses every id that is not lower-case alphanumeric,
// the shape every recorded workflow agent id has taken.
func ValidWorkflowAgentID(id string) error {
	if id == "" {
		return fmt.Errorf("a workflow agent id is empty")
	}
	if len(id) > maxWorkflowAgentID {
		return fmt.Errorf("a workflow agent id is at most %d characters, got %d", maxWorkflowAgentID, len(id))
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return fmt.Errorf("%q cannot be a workflow agent id: lower-case letters and digits only", id)
		}
	}
	return nil
}

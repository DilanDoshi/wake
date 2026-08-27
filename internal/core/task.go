package core

// A dispatch's lifecycle, in Wake's vocabulary.
//
// Claude reports it on five system subtypes that carry between two and nine
// fields each and agree on almost nothing except the id. Above the airlock
// there is one record and three closed enumerations, so a view draws a row
// without ever seeing a wire word. The resolution itself is in vocabulary.go,
// beside every other place Claude's English becomes Wake's.
//
// This is what makes a subagent a conversation rather than a paragraph: the
// forwarded frames say what a subagent *said*, and these say that it exists,
// what it is doing now, and when it stopped. Without them the speech has no
// row to belong to and nothing but a transcript to land in.
//
// It is not an airlock file. It holds no wire spelling and no JSON tags read
// from Claude - only the words Wake uses for what arrived.

import "time"

// TaskKind is what a task *is*, and the reason this enumeration exists at all
// is that the answer is not always "a subagent".
//
// `task_type` is `local_agent` on nine of the ten recorded task_started frames
// and `local_bash` on the tenth, which is a background shell: it carries no
// subagent_type, its id is nine characters where an agent's is seventeen, and
// no frame anywhere is forwarded from it. Claude Code counts the two
// separately in its own strip ("1 shell · 6 agents"), which is the same
// distinction read from the other end.
//
// A view must not offer to open a shell's transcript, because there is none.
type TaskKind string

const (
	// TaskAgent is a subagent. Its forwarded frames carry a
	// parent_tool_use_id equal to this task's Dispatch, which is the whole
	// join - see TaskUpdate.Dispatch.
	TaskAgent TaskKind = "agent"

	// TaskShell is a background shell command.
	TaskShell TaskKind = "shell"

	// TaskKindUnknown is a task_type this decoder does not model. Two are
	// recorded and the CLI's own wording mentions monitors and workflows, so
	// a third is expected rather than hypothetical. It must degrade to shown
	// and not enterable: guessing "agent" opens an empty transcript, and
	// guessing "shell" hides a subagent's work entirely.
	TaskKindUnknown TaskKind = "unknown"
)

// TaskPhase is which of the three moments in a dispatch's life this frame
// reports. Four subtypes collapse to three because task_updated and
// task_notification both report the same ending and differ only in their key
// sets - one names the dispatch and carries a usage, the other does not.
type TaskPhase string

const (
	TaskStarted  TaskPhase = "started"
	TaskProgress TaskPhase = "progress"
	TaskEnded    TaskPhase = "ended"
)

// TaskStatus is how a task ended, and TaskRunning until one says.
//
// Four words are recorded across the two terminal frames - `completed` nine
// times, and `stopped` and `killed` once each, both from the one background
// shell that was interrupted. Nothing records how a *subagent* fails, so
// TaskStatusUnknown is not a placeholder: it is the honest reading of a word
// this corpus has never seen, and a row drawing "done" for one would be
// claiming something no frame said.
type TaskStatus string

// "halted" rather than Claude's own "stopped"/"killed": both are policed
// words, and this file is not an airlock file. A Wake constant whose value is
// Claude's word reads as a passthrough and would have to be exempted from the
// rule that keeps the two vocabularies apart.
const (
	TaskRunning       TaskStatus = "running"
	TaskDone          TaskStatus = "done"
	TaskStopped       TaskStatus = "halted"
	TaskStatusUnknown TaskStatus = "unknown"
)

// TaskUpdate is one report about one task.
//
// Every field is what the frame carried, and the zero value of each is "this
// frame did not say" rather than a default. That matters most for Label: it
// changes through a dispatch - task_started carries what the subagent was
// asked to do and task_progress carries what it is doing *now* - so a
// consumer folds these in arrival order rather than building a row once.
type TaskUpdate struct {
	// ID is the task's own id, and the only field on every one of the five
	// frames. It is the id a subagent's permission ask carries as agent_id,
	// so it is what points an operator at the row that is blocked.
	ID string `json:"id"`

	// Dispatch is the parent tool call that started this task, and the join
	// to everything the subagent says: it equals the parent_tool_use_id on
	// every frame the subagent forwards, which is core.Subagent.Dispatch.
	//
	// Empty on task_updated, whose keys are exactly task_id and patch. That
	// is why ID exists as a separate field and why a consumer keys on ID:
	// an ending arrives that cannot name the dispatch, and inventing one
	// from the other id space would be a fabricated join. Verified in
	// TestTaskStartedJoinsBothIdentifierSpaces.
	Dispatch string `json:"dispatch,omitempty"`

	Kind  TaskKind  `json:"kind"`
	Phase TaskPhase `json:"phase"`

	// Tagged task_status for the reason Event.Tasks is tagged task_set: the
	// bare word is Claude's and policed outside the airlock.
	Status TaskStatus `json:"task_status"`

	// Label is the description: the only human-readable name on the frame,
	// and absent from task_updated.
	Label string `json:"label,omitempty"`

	// Type is the subagent_type. Empty on a shell, on task_updated and on
	// task_notification, and unverified for any subagent type other than
	// general-purpose, so a consumer tolerates "".
	Type string `json:"type,omitempty"`

	// Tool is the last_tool_name, on task_progress alone.
	Tool string `json:"tool,omitempty"`

	// Tokens and Elapsed are the usage, which rides task_progress and all
	// but one task_notification. Both are cumulative for the task and both
	// are Claude's own numbers - Wake times nothing itself, so a row shows
	// what the last frame said rather than a clock this process started.
	Tokens  int           `json:"tokens,omitempty"`
	Elapsed time.Duration `json:"elapsed,omitempty"`
}

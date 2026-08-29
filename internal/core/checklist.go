package core

// The live checklist an agent keeps for itself: the ops TaskCreate/TaskUpdate
// carry (ChecklistOp) and the item snapshot a renderer folds them into (Todo).
// Split out of event.go, which was at the file-size hard max; the fold above the
// airlock lives in internal/ui/checklist.go.

// TodoStatus is what has become of one task, in Wake's vocabulary. Claude's
// three words are resolved in the airlock; see todoStatus for why an
// unrecognised one is pending rather than a state of its own.
type TodoStatus string

const (
	TodoPending TodoStatus = "pending"
	TodoActive  TodoStatus = "active"
	TodoDone    TodoStatus = "done"
)

// ChecklistOp is one create-or-update of a live checklist item, as
// TaskCreate/TaskUpdate carries it. A fold above the airlock turns a run of
// these into the [Todo] snapshot a renderer draws - see internal/ui/checklist.go.
//
// The id is claude's own: TaskCreate assigns a sequential integer ("1", "2",
// ...) reported in its tool_result text, and TaskUpdate names one back in its
// taskId. Create carries no id on the wire (it has not been assigned yet), so
// Update is the only op that sets ID, and the fold reconstructs a create's id
// from its position - the same order claude counts in.
type ChecklistOp struct {
	// Update is false for a TaskCreate and true for a TaskUpdate; the two
	// shapes are one concept and the distinction is which the fold does.
	Update bool `json:"update,omitempty"`

	// ID is the taskId a TaskUpdate names, and "" on a create.
	ID string `json:"id,omitempty"`

	// Text is the item's subject, present on a create and on an update that
	// renames. "" on an update that only moves the status.
	Text string `json:"text,omitempty"`

	// ActiveForm is the present-tense label, set at create and carried by the
	// fold onto the item the working line reads while it is in progress.
	ActiveForm string `json:"active_form,omitempty"`

	// Status is the state an update moves the item to, resolved like Todo's.
	// Empty on a create, which lands the item at pending.
	Status TodoStatus `json:"state,omitempty"`

	// Deleted is a status of "deleted", which drops the item. Binary-sourced
	// and unrecorded: 2.1.240 lists it beside the three drawn states, and a
	// dropped task lingering as "pending" would be a visibly wrong list.
	Deleted bool `json:"dropped,omitempty"`
}

// Todo is one item of an agent's task list.
type Todo struct {
	// Text is the item as written, imperative - Claude's `content`.
	Text string `json:"text"`

	// Status is Wake's own word for the state, resolved in the airlock.
	Status TodoStatus `json:"state,omitempty"`

	// ActiveForm is the present-tense label Claude shows while this item is the
	// one being worked on: "Building the airlock" against "Build the airlock".
	ActiveForm string `json:"active_form,omitempty"`
}

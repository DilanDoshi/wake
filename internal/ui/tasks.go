package ui

// What a conversation's dispatches are doing: the rows under a pane, folded
// from core.Event.Task.
//
// "Task" is core's word for what Claude dispatches - a subagent or a
// background shell - and **not** `/task`, which sets a session's label. The
// two meanings live in different files and this is the one that came across
// the airlock.
//
// Pure, for attention.go's reason: events in, rows out, no processes and no
// I/O, so the whole policy is one function a table test can drive. It is also
// the only thing that decides which rows can be opened, because a row with no
// transcript behind it must never offer one.

import (
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// Task is one row: a dispatch, and the newest thing said about it.
type Task struct {
	// ID is core.TaskUpdate.ID, and what rows are keyed on - the only field
	// on all four lifecycle frames.
	ID string

	// Dispatch is the id the subagent's own frames carry, which is the key
	// its transcript is filed under. Empty until a frame names one, and an
	// ending never does - see Observe.
	Dispatch string

	Kind   core.TaskKind
	Status core.TaskStatus

	// Name is what the dispatch was asked to do, taken from task_started and
	// never rewritten. Label is what it is doing *now*, and task_progress
	// rewrites it on every frame - measured over the corpus, all 9 dispatches
	// carrying both end with a progress description different from the one
	// they started with.
	//
	// Two fields because two surfaces ask different questions: the row asks
	// what is happening, and taskline.go's ending asks what the work was for.
	// One field answers whichever was written last, which is wrong for one of
	// them by construction.
	Name string

	Label   string
	Type    string
	Tool    string
	Tokens  int
	Elapsed time.Duration
}

// Openable says whether this row has a conversation behind it.
//
// Only an agent does: a background shell forwards no frames at all, and an
// unrecorded kind has not earned the assumption. Both are still listed - what
// is running is worth showing whether or not it can be read - and the dispatch
// is required because it is the key the transcript is filed under, so a row
// without one would open an empty pane.
func (t Task) Openable() bool {
	return t.Kind == core.TaskAgent && t.Dispatch != ""
}

// Tasks is a conversation's dispatches, in the order they started.
//
// Order is start order and never activity order: two subagents interleave line
// by line, so a list sorted by whoever spoke last would reorder under somebody
// reading it - and the cursor sits on a row by index, so a reorder moves the
// selection onto a different agent between the keypress and the draw.
//
// The zero value is a conversation that has dispatched nothing.
type Tasks struct {
	rows []Task
}

// Observe folds one event in and returns the result. It ignores everything
// that is not a lifecycle frame, including the forwarded frames the rows
// describe - those are a transcript's business, not a row's.
//
// Nothing here retires a row. A dispatch ends when a frame says it ended, and
// notably not on KindTurnEnd: an async subagent streams past its own result
// and past stdin closing, so a set cleared at a turn boundary - the way
// Agent.Tool is - would retire a row for work still running.
func (t Tasks) Observe(ev core.Event) Tasks {
	u := ev.Task
	if u == nil {
		return t
	}
	rows := make([]Task, len(t.rows), len(t.rows)+1)
	copy(rows, t.rows)
	for i, row := range rows {
		if row.ID == u.ID {
			rows[i] = row.updated(u)
			return Tasks{rows: rows}
		}
	}
	return Tasks{rows: append(rows, Task{ID: u.ID}.updated(u))}
}

// updated applies one frame to one row.
//
// Every field but the status is applied only when the frame carried it,
// because a zero here means "this frame did not say" rather than a new value.
// The four frames disagree about almost everything they carry: an ending names
// no dispatch, no label and no kind, and 1 of the 10 recorded ones carries no
// usage either. Applied unconditionally, a finished row loses the key its
// transcript is filed under, forgets what it was doing, and reports zero
// tokens for work that had already been counted.
//
// The status is the exception and is always applied: it is the one thing every
// frame states outright, and TaskRunning on a progress frame is a statement
// rather than an absence.
func (t Task) updated(u *core.TaskUpdate) Task {
	t.Status = u.Status
	if u.Dispatch != "" {
		t.Dispatch = u.Dispatch
	}
	// TaskKindUnknown reaches here from two different frames and means the
	// same thing in both: a progress frame names no task_type, and an
	// unrecorded type resolves to it too. Neither may overwrite a kind
	// task_started established - the first is not a statement, and the second
	// only ever arrives on the frame that sets the row up.
	if u.Kind != core.TaskKindUnknown {
		t.Kind = u.Kind
	}
	if u.Label != "" {
		t.Label = u.Label
		// Set once, by the frame that opens the dispatch. Every later
		// description is a status rather than a name - see Task.Name.
		if u.Phase == core.TaskStarted {
			t.Name = u.Label
		}
	}
	if u.Type != "" {
		t.Type = u.Type
	}
	if u.Tool != "" {
		t.Tool = u.Tool
	}
	if u.Tokens > 0 {
		t.Tokens = u.Tokens
	}
	if u.Elapsed > 0 {
		t.Elapsed = u.Elapsed
	}
	return t
}

// Rows is the list in start order. A finished dispatch stays in it: its
// transcript is already held, and dropping the row at the moment it becomes
// readable is the complaint this whole surface exists to answer.
func (t Tasks) Rows() []Task { return t.rows }

// Running is how many have not ended - the count a strip shows.
func (t Tasks) Running() int {
	var n int
	for _, row := range t.rows {
		if row.Status == core.TaskRunning {
			n++
		}
	}
	return n
}

// named fills in what an ending frame does not carry about itself.
//
// task_notification's keys are exactly the id, the dispatch, the status, the
// output file, the summary and the usage - **no description and no task_type**
// - so the frame that ends a dispatch cannot say what the dispatch was or what
// kind of thing it was. Both were established by task_started and are held on
// the row.
//
// It returns a new TaskUpdate rather than editing the one it was given: the
// event's Task is a pointer, shared with whatever else holds that event.
//
// An ending that arrives before its own task_started is left unnamed, and that
// is deliberate. Every recording orders them the other way, the live stream is
// read from the process's first byte, and history comes off claude's on-disk
// transcript where DecodeTranscriptLine drops these frames entirely - so there
// is no path that produces one. Backfilling a stored event afterwards would
// cost the property this enrichment exists to protect: that a block is a pure
// function of the event it is stored against. The line degrades to naming no
// dispatch, which is the safe direction.
func (t Tasks) named(u *core.TaskUpdate) *core.TaskUpdate {
	row, ok := t.forID(u.ID)
	if !ok {
		return u
	}
	filled := *u
	if filled.Label == "" {
		// The dispatch's name, never the row's live status: an ending is not
		// a status line, and the two diverge on every recorded dispatch.
		filled.Label = row.Name
	}
	if filled.Kind == core.TaskKindUnknown {
		filled.Kind = row.Kind
	}
	if filled.Elapsed <= 0 {
		filled.Elapsed = row.Elapsed
	}
	return &filled
}

// forID is the row a lifecycle frame is about. Every one of the four carries a
// task id and the rows are keyed on it, so this is exact where a match on the
// dispatch id is a scan that returns the first row carrying it.
func (t Tasks) forID(id string) (Task, bool) {
	for _, row := range t.rows {
		if row.ID == id {
			return row, true
		}
	}
	return Task{}, false
}

package ui

// The task board pinned above the composer: the live TaskCreate/TaskUpdate
// checklist an agent keeps for itself, drawn where Claude Code draws it and
// where the dispatch list used to sit.
//
// It is the *only* place the checklist shows. The ops themselves are folded
// into d.checklist and never drawn in the transcript, so a run of forty updates
// is one board rather than forty blocks - and a subagent's own status lives in
// the right sidebar (see rostersubs.go), which is what the dispatch list this
// replaced was for.
//
// It reads d.checklist, a held field for chromeHeight's reason: the board's
// rows come out of the transcript's height, and a source projected per draw
// would leave DM.chrome's memo disagreeing with what View measures - re-wrapping
// the whole transcript on every frame. See the DM.checklist field.

import (
	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
)

// checklistPin is the board at a width, and "" for a conversation whose agent
// has written no list - which is most of them, and a row saying so is chrome
// nobody asked for.
func (d DM) checklistPin(width int) string {
	return render.TodoList(d.checklistItems(), width)
}

// checklistRows is how many rows checklistPin draws, counted rather than drawn
// for baseChrome, which runs on every re-lay.
func (d DM) checklistRows() int {
	return render.TodoRows(d.checklistItems())
}

// resettleBoard re-sizes the pane once when a checklist op moved the board's
// height, keeping DM.chrome current off the draw path - the rule the removed
// dispatch list's projection kept. The width is unchanged, so it re-measures the
// chrome and the transcript's window rather than re-wrapping through glamour.
// Called only for a checklist op - a create or delete changes the row count -
// and a no-op when the count did not move, so a status-only update costs one
// chromeHeight and no re-size.
func (d DM) resettleBoard() DM {
	if d.width <= 0 || d.height <= 0 || d.chromeHeight() == d.chrome {
		return d
	}
	return d.SetSize(d.width, d.height)
}

// checklistItems is the pinned list in render's vocabulary. The conversion is a
// copy rather than a shared type: internal/render takes plain values and never
// imports core.
func (d DM) checklistItems() []render.Todo {
	todos := d.checklist.snapshot()
	items := make([]render.Todo, len(todos))
	for i, t := range todos {
		items[i] = render.Todo{Text: t.Text, Status: todoState(t.Status)}
	}
	return items
}

// todoState maps core's vocabulary onto render's. Two enumerations rather than a
// shared one because internal/render imports no Wake package by design; a status
// core grows is a compile error here, which is where the decision belongs.
func todoState(s core.TodoStatus) render.TodoState {
	switch s {
	case core.TodoActive:
		return render.TodoActive
	case core.TodoDone:
		return render.TodoDone
	default:
		return render.TodoPending
	}
}

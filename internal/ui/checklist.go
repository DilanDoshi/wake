package ui

import (
	"slices"
	"strconv"

	"github.com/DilanDoshi/wake/internal/core"
)

// The live checklist fold: a run of TaskCreate/TaskUpdate ops accumulated into
// the list a renderer draws.
//
// Claude 2.1.240 retired TodoWrite, whose one call carried the whole list, for
// TaskCreate/TaskUpdate, which build the list across many calls. So the
// accumulation the renderer used to get for free is done here, and the snapshot
// attached to the call that produced it - so the renderer (render.TodoList) and
// the working line (activeForm) read a finished list off the event, unchanged.
//
// It is folded in two places that share this one type: the Fleet, for the
// working line an agent shows while it has no pane open, and the DM, so a
// checklist built before this client attached comes back off disk - the reason
// a slice keyed on position could not: restore replays the whole conversation
// through DM.Before, and the live path continues from there.

// checklist is a session's accumulated task list, keyed on the id claude
// assigns each item.
//
// The id is the crux. Claude counts creates with a monotonic integer - "1",
// "2", "3", ... reported in the create's tool_result text - and a TaskUpdate
// names one back by that id. It is *not* a position: a delete removes an item
// without renumbering the survivors, and the next create takes the next counter
// value rather than a vacated one. So the fold keys on that id, reconstructed
// from the create counter, and never on slice position - which would silently
// misapply every update after the first delete.
type checklist struct {
	// created is how many items this session has ever created, which is the id
	// claude gives the next one. It never decrements, so a delete does not let
	// a later create reuse an id.
	created int

	// order is the live ids in creation order; item maps an id to its todo.
	// Two structures because the snapshot needs order and an update needs a
	// lookup, and a delete has to drop from both.
	order []string
	item  map[string]core.Todo
}

// apply folds one op into the list and returns a new checklist, never mutating
// the receiver - a prior value is held by a transcript line already drawn, and
// a mutation here would rewrite it. A nil op is a no-op.
func (c checklist) apply(op *core.ChecklistOp) checklist {
	if op == nil {
		return c
	}
	next := checklist{
		created: c.created,
		order:   append([]string(nil), c.order...),
		item:    make(map[string]core.Todo, len(c.item)+1),
	}
	for k, v := range c.item {
		next.item[k] = v
	}

	if !op.Update {
		next.created++
		id := strconv.Itoa(next.created)
		next.order = append(next.order, id)
		next.item[id] = core.Todo{Text: op.Text, Status: op.Status, ActiveForm: op.ActiveForm}
		return next
	}

	// An update names an id by claude's counter. One with no create - dropped
	// before this client attached, or never made - is nothing to apply rather
	// than a row to invent.
	t, ok := next.item[op.ID]
	if !ok {
		return next
	}
	if op.Deleted {
		delete(next.item, op.ID)
		next.order = slices.DeleteFunc(next.order, func(id string) bool { return id == op.ID })
		return next
	}
	t.Status = op.Status
	if op.Text != "" {
		t.Text = op.Text
	}
	if op.ActiveForm != "" {
		t.ActiveForm = op.ActiveForm
	}
	next.item[op.ID] = t
	return next
}

// snapshot is the list a renderer draws, in creation order, and nil for an
// empty checklist so a pane with none grows no block.
func (c checklist) snapshot() []core.Todo {
	if len(c.order) == 0 {
		return nil
	}
	out := make([]core.Todo, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, c.item[id])
	}
	return out
}

// foldChecklist accumulates a TaskCreate/TaskUpdate op into the session's live
// list and attaches the snapshot to the event, so the working line reads the
// item in flight off it. Every other event passes through untouched.
//
// State on Fleet rather than Agent for the reason Fleet.tasks is: a map on
// Agent would break the `now == was` comparison Observe turns on. A new
// ToolCall rather than a write into the shared one, so a later op cannot rewrite
// a line the transcript already drew.
func (f Fleet) foldChecklist(sessionID string, ev core.Event) (Fleet, core.Event) {
	if ev.Tool == nil || ev.Tool.Checklist == nil {
		return f, ev
	}
	cl := f.checklists[sessionID].apply(ev.Tool.Checklist)
	f = f.copy()
	f.checklists[sessionID] = cl

	tool := *ev.Tool
	tool.Todos = cl.snapshot()
	ev.Tool = &tool
	return f, ev
}

// foldChecklist is the DM's own accumulation, so a checklist survives a restore
// off disk and continues across the attach boundary. It folds one op into the
// conversation's running list and returns the event with its whole-list snapshot
// stripped, so the event stored in d.events (and the *ToolCall d.calls shares
// with it) carries only the compact op.
//
// The snapshot the renderer draws is rebuilt from those ops at render time -
// off d.checklist for the newest event live (withSnapshot in dm.go's Append) and
// off a running list on a re-wrap or a restore (renderAll) - never stored on the
// event. Storing a fresh copy of the whole list on every op was O(ops x items):
// a run of 400 updates over 40 items pinned 16,820 Todos (BUG-26). The Fleet
// fold set Todos upstream for the working line and it has already been read, so
// clearing it here loses nothing.
func (d DM) foldChecklist(ev core.Event) (DM, core.Event) {
	// A subagent's checklist op is its own board, not this conversation's: it is
	// routed to d.subs rather than d.events (see forwardedTo), so folding it into
	// d.checklist would commingle it into the parent's list and make the live
	// render diverge from renderAll's rebuild, which walks only d.events. Left
	// untouched, it keeps the snapshot the Fleet fold gave it and draws in its
	// dispatch's own transcript.
	if ev.Tool == nil || ev.Tool.Checklist == nil || ev.Subagent != nil {
		return d, ev
	}
	d.checklist = d.checklist.apply(ev.Tool.Checklist)
	if ev.Tool.Todos != nil {
		tool := *ev.Tool
		tool.Todos = nil
		ev.Tool = &tool
	}
	return d, ev
}

// withSnapshot attaches c's current list to a TaskCreate/TaskUpdate event for
// rendering, and passes every other event through unchanged - a TodoWrite, which
// carries its own whole list, included. c must already hold this event's op:
// d.checklist does after foldChecklist, and renderAll folds it in as it walks.
//
// The list is a fresh copy the caller draws and drops; it is never stored, which
// is the whole of the fix - N ops no longer pin N copies of the list.
func (c checklist) withSnapshot(ev core.Event) core.Event {
	if ev.Tool == nil || ev.Tool.Checklist == nil {
		return ev
	}
	tool := *ev.Tool
	tool.Todos = c.snapshot()
	ev.Tool = &tool
	return ev
}

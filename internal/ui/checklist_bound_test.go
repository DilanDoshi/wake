package ui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// taskCreateID and taskUpdateID are the e2e helpers with a caller-chosen tool
// id, so a synthesized load produces distinct tool_use ids the way a real
// stream does - which is what makes d.calls, not only d.events, hold one entry
// per op.
func taskCreateID(toolID, subject, active string) core.Event {
	return core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
		ID: toolID, Name: "TaskCreate",
		Checklist: &core.ChecklistOp{Text: subject, ActiveForm: active, Status: core.TodoPending},
	}}
}

func taskUpdateID(toolID, itemID string, status core.TodoStatus) core.Event {
	return core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
		ID: toolID, Name: "TaskUpdate",
		Checklist: &core.ChecklistOp{Update: true, ID: itemID, Status: status},
	}}
}

// pinnedTodos is the number of core.Todo values the DM's stored events hold
// alive - the quantity BUG-26 is about. The transcript is the source of truth
// (d.events); d.calls shares those same *ToolCall pointers, so counting the
// events counts the pinning.
func pinnedTodos(d DM) int {
	var n int
	for _, ev := range d.events.slice(0, d.events.len()) {
		if ev.Tool != nil {
			n += len(ev.Tool.Todos)
		}
	}
	return n
}

// The whole-list snapshot the renderer draws is not stored on every historical
// event: a run of many ops over many items must not pin O(ops x items) Todos.
//
// The recorded fixture carries five ops, so the load is synthesized here. Before
// the fix each of the 40 creates pinned a growing prefix (1..40) and each of the
// 400 updates pinned the whole 40-item list, for 820 + 16,000 = 16,820 Todos
// held alive. After it the stored events carry only the compact op and the count
// is bounded by a single list.
func TestChecklistEventsDoNotPinAListPerOp(t *testing.T) {
	fresh(t)
	const items, updates = 40, 400
	d := NewDM("s1", "alex").SetSize(80, 40)
	for i := 0; i < items; i++ {
		d = d.Append(taskCreateID("c"+strconv.Itoa(i), "task "+strconv.Itoa(i), "doing "+strconv.Itoa(i)))
	}
	for i := 0; i < updates; i++ {
		id := strconv.Itoa(i%items + 1) // claude's 1-based monotonic ids
		d = d.Append(taskUpdateID("u"+strconv.Itoa(i), id, core.TodoActive))
	}

	// The live list is intact - the fix bounds what is *stored*, not what is
	// tracked: the accumulator still holds every item.
	if got := len(d.checklist.snapshot()); got != items {
		t.Fatalf("the live checklist lost items: have %d, want %d", got, items)
	}

	// The pinned count must scale with the list, not with the number of ops.
	if got := pinnedTodos(d); got > items {
		t.Errorf("stored events pin %d Todos across %d ops over %d items; want <= %d (one list)",
			got, items+updates, items, items)
	}
}

// A representative sequence draws the same pane whether built incrementally
// (Append) or rebuilt whole (renderAll, forced by a width round-trip). The
// checklist ops draw nothing in the transcript - they are the board pinned above
// the composer - and the board is a fold of d.checklist, so both paths must
// arrive at the same board and the same (empty of checklist) transcript.
func TestChecklistRenderIsIdenticalIncrementalAndRebuilt(t *testing.T) {
	fresh(t)
	ops := []core.Event{
		taskCreateID("c1", "explore the code", "Exploring the code"),
		taskCreateID("c2", "write the patch", "Writing the patch"),
		taskCreateID("c3", "run the tests", "Running the tests"),
		taskUpdateID("u1", "1", core.TodoDone),
		taskUpdateID("u2", "2", core.TodoActive),
		taskCreateID("c4", "delete me", "Deleting"),
		// A delete: id 4 removed, so the final board must not show "delete me".
		{Kind: core.KindToolUse, Tool: &core.ToolCall{
			ID: "u3", Name: "TaskUpdate",
			Checklist: &core.ChecklistOp{Update: true, ID: "4", Deleted: true},
		}},
	}

	d := NewDM("s1", "alex").SetSize(80, 40)
	for _, ev := range ops {
		d = d.Append(ev)
	}
	incremental := d.View(80, 40)

	// A width round-trip re-derives the whole transcript through renderAll at 80.
	rebuilt := d.SetSize(120, 40).SetSize(80, 40).View(80, 40)

	if incremental != rebuilt {
		t.Errorf("incremental and rebuilt views differ:\n--- incremental ---\n%s\n--- rebuilt ---\n%s",
			stripANSI(incremental), stripANSI(rebuilt))
	}

	// The board carries the final list - first-done/second-active/third-pending -
	// and never the deleted item. It is the one place the checklist shows.
	board := stripANSI(d.checklistPin(80))
	for _, want := range []string{"☑ explore the code", "■ write the patch", "☐ run the tests"} {
		if !strings.Contains(board, want) {
			t.Errorf("the board is missing %q:\n%s", want, board)
		}
	}
	if strings.Contains(board, "delete me") {
		t.Errorf("the board shows a deleted item:\n%s", board)
	}

	// And the transcript itself holds none of it: the ops draw nothing there.
	if region := stripANSI(conversationRegion(t, d, 80, 40)); strings.Contains(region, "explore the code") {
		t.Errorf("a checklist op drew a block in the transcript instead of vanishing:\n%s", region)
	}
}

// The adversarial case for the reconstruction: an item re-activated after it was
// completed, a text rewrite, and a folded Bash run (a use+result rollup) sitting
// between checklist ops. If the running list mis-folded around the folded run or
// mis-ordered the ops, the rebuilt transcript would drift from the live one.
func TestChecklistReconstructionSurvivesInterleavingAndReactivation(t *testing.T) {
	fresh(t)
	bashUse := core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{ID: "b1", Name: "Bash", Display: "go test ./..."}}
	bashRes := core.Event{Kind: core.KindToolResult, Tool: &core.ToolCall{ID: "b1"}, Text: "ok"}
	ops := []core.Event{
		taskCreateID("c1", "first", "Firsting"),
		taskCreateID("c2", "second", "Seconding"),
		taskUpdateID("u1", "1", core.TodoDone),
		bashUse, bashRes, // a folded run between checklist ops
		taskUpdateID("u2", "1", core.TodoActive), // re-activate a completed item
		// A text rewrite on an existing item.
		{Kind: core.KindToolUse, Tool: &core.ToolCall{ID: "u3", Name: "TaskUpdate",
			Checklist: &core.ChecklistOp{Update: true, ID: "2", Status: core.TodoActive, Text: "second, renamed"}}},
	}

	d := NewDM("s1", "alex").SetSize(72, 40)
	for _, ev := range ops {
		d = d.Append(ev)
	}
	incremental := d.View(72, 40)
	rebuilt := d.SetSize(100, 40).SetSize(72, 40).View(72, 40)
	if incremental != rebuilt {
		t.Errorf("incremental and rebuilt differ after interleaving/reactivation:\n--- incremental ---\n%s\n--- rebuilt ---\n%s",
			stripANSI(incremental), stripANSI(rebuilt))
	}
	// The rename and the re-activation both landed on the right item.
	view := stripANSI(incremental)
	for _, want := range []string{"■ first", "■ second, renamed"} {
		if !strings.Contains(view, want) {
			t.Errorf("the final list is missing %q:\n%s", want, view)
		}
	}
}

// forwardedChecklist is a subagent's own TaskCreate: a checklist op that also
// carries a dispatch, the way a real one arrives (parent_tool_use_id set), with
// the Fleet fold's snapshot already on it from upstream.
func forwardedChecklist(dispatch, toolID, subject string, todos []core.Todo) core.Event {
	return core.Event{
		Kind:     core.KindToolUse,
		Subagent: &core.Subagent{Dispatch: dispatch},
		Tool: &core.ToolCall{ID: toolID, Name: "TaskCreate",
			Checklist: &core.ChecklistOp{Text: subject, Status: core.TodoActive},
			Todos:     todos},
	}
}

// A subagent's checklist op belongs to the subagent, not the parent: it must not
// enter the parent's accumulator, so the parent's transcript never shows the
// subagent's item and its live and rebuilt renders cannot diverge. This is the
// case the byte-identical guard above cannot see - the forwarded op folds into
// d.checklist (read live) but never into d.events (walked by renderAll).
func TestASubagentChecklistDoesNotCommingleIntoTheParent(t *testing.T) {
	fresh(t)
	d := NewDM("s1", "alex").SetSize(80, 40)
	d = d.Append(taskCreateID("c1", "parent one", "Parent one"))
	d = d.Append(forwardedChecklist("disp1", "sc1", "SUBAGENT ONLY",
		[]core.Todo{{Text: "SUBAGENT ONLY", Status: core.TodoActive}}))
	d = d.Append(taskCreateID("c2", "parent two", "Parent two"))

	live := d.View(80, 40)
	rebuilt := d.SetSize(120, 40).SetSize(80, 40).View(80, 40)
	if live != rebuilt {
		t.Errorf("a subagent checklist op made the parent's live and rebuilt transcripts diverge:\n--live--\n%s\n--rebuilt--\n%s",
			stripANSI(live), stripANSI(rebuilt))
	}
	view := stripANSI(live)
	if strings.Contains(view, "SUBAGENT ONLY") {
		t.Errorf("the subagent's checklist item leaked into the parent transcript:\n%s", view)
	}
	for _, want := range []string{"parent one", "parent two"} {
		if !strings.Contains(view, want) {
			t.Errorf("the parent transcript is missing %q:\n%s", want, view)
		}
	}
}

// And the subagent keeps its own list where it is drawn - its dispatch's
// transcript - rather than losing it to the parent's strip.
func TestAForwardedChecklistKeepsItsListInTheSubagentView(t *testing.T) {
	fresh(t)
	d := NewDM("s1", "alex").SetSize(80, 40)
	d = d.Append(forwardedChecklist("disp1", "sc1", "subagent task",
		[]core.Todo{{Text: "subagent task", Status: core.TodoActive}}))
	d = d.Viewing("disp1")
	if view := stripANSI(d.View(80, 40)); !strings.Contains(view, "subagent task") {
		t.Errorf("a subagent's own checklist rendered without its list:\n%s", view)
	}
}

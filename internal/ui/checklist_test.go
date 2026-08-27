package ui

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

func create(subject, active string) *core.ChecklistOp {
	return &core.ChecklistOp{Text: subject, ActiveForm: active, Status: core.TodoPending}
}
func update(id string, status core.TodoStatus) *core.ChecklistOp {
	return &core.ChecklistOp{Update: true, ID: id, Status: status}
}
func del(id string) *core.ChecklistOp {
	return &core.ChecklistOp{Update: true, ID: id, Deleted: true}
}

func foldAll(ops ...*core.ChecklistOp) []core.Todo {
	var c checklist
	for _, op := range ops {
		c = c.apply(op)
	}
	return c.snapshot()
}

// A run of creates then updates reconstructs the list claude drew: three items
// in creation order, the first done and the second in flight - the sequence the
// recorded session emitted.
func TestChecklistFoldReplaysCreatesThenUpdates(t *testing.T) {
	list := foldAll(
		create("explore the code", "Exploring the code"),
		create("write the patch", "Writing the patch"),
		create("run the tests", "Running the tests"),
		update("1", core.TodoDone),
		update("2", core.TodoActive),
	)
	if len(list) != 3 {
		t.Fatalf("list has %d items, want 3: %+v", len(list), list)
	}
	want := []core.TodoStatus{core.TodoDone, core.TodoActive, core.TodoPending}
	for i, w := range want {
		if list[i].Status != w {
			t.Errorf("item %d status %q, want %q", i, list[i].Status, w)
		}
	}
	if list[0].Text != "explore the code" || list[2].Text != "run the tests" {
		t.Errorf("order not preserved: %+v", list)
	}
	if list[1].ActiveForm != "Writing the patch" {
		t.Errorf("in-flight active form lost: %q", list[1].ActiveForm)
	}
}

// The id is claude's monotonic counter, not a slice position: a delete does not
// renumber the survivors, and a later update still lands on the item it names.
// This is the case a position-based fold silently corrupts.
func TestAnUpdateAfterADeleteLandsOnTheRightItem(t *testing.T) {
	// Create a, b, c (ids 1,2,3); delete b (id 2); then mark c (id 3) done.
	// Position-based folding would drop the id-3 update as out of range once the
	// list compacted to two items.
	list := foldAll(
		create("a", ""),
		create("b", ""),
		create("c", ""),
		del("2"),
		update("3", core.TodoDone),
	)
	if len(list) != 2 || list[0].Text != "a" || list[1].Text != "c" {
		t.Fatalf("delete did not drop the middle item cleanly: %+v", list)
	}
	if list[1].Status != core.TodoDone {
		t.Errorf("the update to id 3 missed the surviving item c: %+v", list)
	}
}

// A create after a delete takes the next counter value, never a vacated id, so
// a later update to that new item is not confused with the deleted one.
func TestACreateAfterADeleteDoesNotReuseAnID(t *testing.T) {
	list := foldAll(
		create("a", ""), // id 1
		create("b", ""), // id 2
		del("2"),        // remove b
		create("d", ""), // id 3, not a reused 2
		update("3", core.TodoDone),
	)
	if len(list) != 2 || list[1].Text != "d" || list[1].Status != core.TodoDone {
		t.Errorf("a create after a delete reused an id or missed the update: %+v", list)
	}
}

// The fold never mutates a prior snapshot: the value handed to a transcript line
// stays what it was when that line was drawn.
func TestChecklistFoldDoesNotMutateThePriorSnapshot(t *testing.T) {
	var c checklist
	c = c.apply(create("a", "Aing"))
	base := c.snapshot()
	_ = c.apply(update("1", core.TodoDone))
	if base[0].Status != core.TodoPending {
		t.Errorf("a prior snapshot was mutated to %q", base[0].Status)
	}
}

// An update to an id that was never created is dropped rather than inventing a
// row, and a nil op is a no-op.
func TestChecklistFoldIgnoresUnknownIDs(t *testing.T) {
	if list := foldAll(create("a", ""), update("9", core.TodoDone)); len(list) != 1 || list[0].Status != core.TodoPending {
		t.Errorf("an update to an unknown id changed the list: %+v", list)
	}
	var c checklist
	c = c.apply(create("a", ""))
	if got := c.apply(nil); len(got.snapshot()) != 1 {
		t.Errorf("a nil op changed the list")
	}
}

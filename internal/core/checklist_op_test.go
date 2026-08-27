package core

import "testing"

// TaskCreate decodes to a create op carrying the subject and its present-tense
// label, and no id - claude assigns that server-side and reports it in the
// result text, so the fold reconstructs it from position.
func TestTaskCreateDecodesToACreateOp(t *testing.T) {
	op := toolChecklistOp("TaskCreate", map[string]any{
		"subject":     "explore the code",
		"description": "look around",
		"activeForm":  "Exploring the code",
	})
	if op == nil {
		t.Fatal("TaskCreate decoded to no op")
	}
	if op.Update {
		t.Error("a create decoded as an update")
	}
	if op.ID != "" {
		t.Errorf("a create carried an id %q; the id is not assigned until the result", op.ID)
	}
	if op.Text != "explore the code" || op.ActiveForm != "Exploring the code" {
		t.Errorf("create op = %+v", op)
	}
	if op.Status != TodoPending {
		t.Errorf("a create lands at pending, got %q", op.Status)
	}
}

// TaskUpdate decodes to an update naming the item by claude's taskId and the
// state it moves to.
func TestTaskUpdateDecodesToAnUpdateOp(t *testing.T) {
	op := toolChecklistOp("TaskUpdate", map[string]any{
		"taskId": "2",
		"status": "in_progress",
	})
	if op == nil {
		t.Fatal("TaskUpdate decoded to no op")
	}
	if !op.Update || op.ID != "2" || op.Status != TodoActive {
		t.Errorf("update op = %+v", op)
	}
	if op.Deleted {
		t.Error("an in_progress update read as deleted")
	}
}

// A "deleted" status is carried as a distinct signal rather than folded into
// pending, so the list can drop the item instead of showing it as still-to-do.
func TestTaskUpdateDeletedIsADrop(t *testing.T) {
	op := toolChecklistOp("TaskUpdate", map[string]any{"taskId": "1", "status": "deleted"})
	if op == nil || !op.Deleted {
		t.Fatalf("a deleted update did not mark the drop: %+v", op)
	}
}

// A TaskUpdate with no taskId is nothing to apply, and a nameless TaskCreate is
// a row nobody named - both decode to no op rather than a blank item.
func TestAMalformedChecklistOpIsNoOp(t *testing.T) {
	if op := toolChecklistOp("TaskUpdate", map[string]any{"status": "completed"}); op != nil {
		t.Errorf("an update with no taskId decoded to %+v", op)
	}
	if op := toolChecklistOp("TaskCreate", map[string]any{"description": "x"}); op != nil {
		t.Errorf("a create with no subject decoded to %+v", op)
	}
}

// Every other tool carries no checklist op, so nothing else grows one.
func TestOnlyTheTaskToolsDecodeToAChecklistOp(t *testing.T) {
	for _, name := range []string{"Bash", "Read", "TodoWrite", "Task", "TaskList"} {
		if op := toolChecklistOp(name, map[string]any{"subject": "x", "taskId": "1"}); op != nil {
			t.Errorf("%s decoded a checklist op: %+v", name, op)
		}
	}
}

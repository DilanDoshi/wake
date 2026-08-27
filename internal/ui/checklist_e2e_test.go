package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

func taskCreate(subject, active string) core.Event {
	return core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
		ID: "toolu_c", Name: "TaskCreate",
		Checklist: &core.ChecklistOp{Text: subject, ActiveForm: active, Status: core.TodoPending},
	}}
}
func taskUpdate(id string, status core.TodoStatus) core.Event {
	return core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
		ID: "toolu_u", Name: "TaskUpdate",
		Checklist: &core.ChecklistOp{Update: true, ID: id, Status: status},
	}}
}

// The recorded sequence a live session emits - three TaskCreate then two
// TaskUpdate - reaches the DM transcript as the checklist claude drew, and the
// working line as the label of the item in flight. This is the whole fix: the
// renderer never changed, the fold feeds it.
func TestALiveChecklistRendersAndDrivesTheWorkingLine(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex")
	for _, ev := range []core.Event{
		taskCreate("explore the code", "Exploring the code"),
		taskCreate("write the patch", "Writing the patch"),
		taskCreate("run the tests", "Running the tests"),
		taskUpdate("1", core.TodoDone),
		taskUpdate("2", core.TodoActive),
	} {
		a = a.observe("s1", ev)
	}

	// The working line reads the in-flight item's present-tense label.
	if got := a.fleet.agents["s1"].Doing; got != "Writing the patch" {
		t.Errorf("working line label = %q, want %q", got, "Writing the patch")
	}

	// The transcript's latest checklist block is the finished list: first done,
	// second in flight, third still to do, in creation order.
	view := stripANSI(a.dms["s1"].View(80, 40))
	for _, want := range []string{"☑ explore the code", "■ write the patch", "☐ run the tests"} {
		if !strings.Contains(view, want) {
			t.Errorf("the DM transcript is missing %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "explore the code") > strings.Index(view, "run the tests") {
		t.Error("the checklist lost creation order")
	}
}

// A checklist built before this client attached comes back off disk: the
// restore path (DM.Before) re-derives the accumulated list, where the live-only
// Fleet fold saw none of it. Without this a reopened conversation - the ordinary
// case at 15-30 agents - showed bare TaskCreate/TaskUpdate headers and no list.
func TestARestoredChecklistRenders(t *testing.T) {
	d := NewDM("s1", "alex").Before([]core.Event{
		taskCreate("explore the code", "Exploring the code"),
		taskCreate("write the patch", "Writing the patch"),
		taskCreate("run the tests", "Running the tests"),
		taskUpdate("1", core.TodoDone),
		taskUpdate("2", core.TodoActive),
	})
	view := stripANSI(d.View(80, 40))
	for _, want := range []string{"☑ explore the code", "■ write the patch", "☐ run the tests"} {
		if !strings.Contains(view, want) {
			t.Errorf("a restored conversation is missing %q:\n%s", want, view)
		}
	}
}

// The end-to-end proof against real bytes: the recorded fixture, decoded through
// the airlock, folded, and rendered - so "recorded against 2.1.240" means the
// specific bytes produce the specific list, not merely that they decode.
func TestTheRecordedFixtureFoldsToTheListItDrew(t *testing.T) {
	path := "../../testdata/stream/task-checklist.jsonl"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var c checklist
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		evs, err := core.DecodeLine([]byte(line))
		if err != nil {
			continue
		}
		for _, ev := range evs {
			if ev.Tool != nil && ev.Tool.Checklist != nil {
				c = c.apply(ev.Tool.Checklist)
			}
		}
	}
	list := c.snapshot()
	want := []core.Todo{
		{Text: "explore the code", Status: core.TodoDone, ActiveForm: "Exploring the code"},
		{Text: "write the patch", Status: core.TodoActive, ActiveForm: "Writing the patch"},
		{Text: "run the tests", Status: core.TodoPending, ActiveForm: "Running the tests"},
	}
	if len(list) != len(want) {
		t.Fatalf("the fixture folded to %d items, want %d: %+v", len(list), len(want), list)
	}
	for i, w := range want {
		if list[i] != w {
			t.Errorf("item %d = %+v, want %+v", i, list[i], w)
		}
	}
}

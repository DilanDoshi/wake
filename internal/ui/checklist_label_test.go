package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The label of the item in flight reaches the conversation's working line, the
// room's and the /board row: its activeForm, else its subject (activeForm is
// optional, see activeForm).
func TestAnInProgressItemsLabelReachesEachRenderer(t *testing.T) {
	for _, c := range []struct {
		name, subject, active, want string
	}{
		{"its own activeForm", "write the patch", "Writing the patch", "Writing the patch"},
		{"no activeForm: the subject", "write the patch", "", "write the patch"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			a := dmApp(nil, Stream{}, "s1", "alex")
			a = a.observe("s1", taskCreate(c.subject, c.active))
			a = a.observe("s1", taskUpdate("1", core.TodoActive))

			ag := a.fleet.agents["s1"]
			ag.ID, ag.Name, ag.State, ag.startedAt = "s1", "alex", rpc.StateWorking, clock()
			if ag.Doing != c.want {
				t.Errorf("the agent's label is %q, want %q", ag.Doing, c.want)
			}

			dm := stripANSI(workingLine("s1", rpc.StateWorking, ag.Doing, clock(), 0, 80))
			room := stripANSI(roomWorkingLine([]Agent{ag}, nil, 80))
			row := stripANSI(boardRow(ag, false, 6, 8, 120, false))
			for surface, got := range map[string]string{"conversation line": dm, "room line": room, "board row": row} {
				if !strings.Contains(got, c.want) {
					t.Errorf("the %s is %q, want it to name %q", surface, got, c.want)
				}
			}
		})
	}
}

// The first item in flight names itself, and only an item in flight does: a
// pending one has not started, and a done one has finished.
func TestActiveFormIsTheFirstInFlightItemsLabelOrItsSubject(t *testing.T) {
	for _, c := range []struct {
		name  string
		todos []core.Todo
		want  string
	}{
		{"none", nil, ""},
		{"nothing in flight", []core.Todo{
			{Text: "later", Status: core.TodoPending, ActiveForm: "Doing it later"},
			{Text: "earlier", Status: core.TodoDone, ActiveForm: "Doing it earlier"},
		}, ""},
		{"in flight with a label", []core.Todo{{Text: "encode", Status: core.TodoActive, ActiveForm: "Encoding"}}, "Encoding"},
		{"in flight with none", []core.Todo{{Text: "encode the receipt", Status: core.TodoActive}}, "encode the receipt"},
		{"the first one wins, labelled or not", []core.Todo{
			{Text: "first", Status: core.TodoActive},
			{Text: "second", Status: core.TodoActive, ActiveForm: "Doing the second"},
		}, "first"},
		{"a pending item's label is not borrowed", []core.Todo{
			{Text: "later", Status: core.TodoPending, ActiveForm: "Doing it later"},
			{Text: "now", Status: core.TodoActive},
		}, "now"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := activeForm(c.todos); got != c.want {
				t.Errorf("activeForm = %q, want %q", got, c.want)
			}
		})
	}
}

// A TodoWrite list reads the same way, through the one function: an item in
// flight with no activeForm names itself by its text.
func TestATodoWriteItemWithoutALabelNamesItselfByItsText(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(todoCall(core.Todo{Text: "encode the receipt", Status: core.TodoActive}), "a")
	if got := f.agents["a"].Doing; got != "encode the receipt" {
		t.Errorf("the label is %q, want the item's text", got)
	}
}

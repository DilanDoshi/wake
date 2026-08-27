package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func todoCall(items ...core.Todo) core.Event {
	return core.Event{
		Kind: core.KindToolUse,
		Tool: &core.ToolCall{ID: "toolu_1", Name: "TodoWrite", Todos: items},
	}
}

// The list is drawn under its own tool call, so a reader sees what the agent
// is doing and the plan it is doing it against in one block.
func TestATaskListIsDrawnUnderItsToolCall(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 20)
	block := stripANSI(d.eventBlock(todoCall(
		core.Todo{Text: "encode the receipt", Status: core.TodoActive},
		core.Todo{Text: "refuse the mode verb", Status: core.TodoPending},
		core.Todo{Text: "wire the daemon", Status: core.TodoDone},
	)))

	if !strings.Contains(block, "TodoWrite") {
		t.Errorf("the tool call's header is missing:\n%s", block)
	}
	for _, want := range []string{"■ encode the receipt", "☐ refuse the mode verb", "☑ wire the daemon"} {
		if !strings.Contains(block, want) {
			t.Errorf("the block is missing %q:\n%s", want, block)
		}
	}
	if head, _, _ := strings.Cut(block, "\n"); !strings.Contains(head, "TodoWrite") {
		t.Errorf("the list is drawn above its own call:\n%s", block)
	}
}

// core's vocabulary maps onto render's, and an unruled status lands on pending
// rather than on whatever iota happens to be zero by accident.
func TestEveryCoreTodoStatusMapsToARenderState(t *testing.T) {
	for _, tc := range []struct {
		in   core.TodoStatus
		want render.TodoState
	}{
		{core.TodoActive, render.TodoActive},
		{core.TodoDone, render.TodoDone},
		{core.TodoPending, render.TodoPending},
		{core.TodoStatus("something new"), render.TodoPending},
		{core.TodoStatus(""), render.TodoPending},
	} {
		if got := todoState(tc.in); got != tc.want {
			t.Errorf("todoState(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// A tool call with no list is the ordinary case and must not grow a blank body.
func TestAToolCallWithNoListDrawsNoList(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 20)
	block := d.eventBlock(core.Event{
		Kind: core.KindToolUse,
		Tool: &core.ToolCall{ID: "toolu_1", Name: "Bash", Display: "go test ./..."},
	})
	for _, glyph := range []string{"■", "☐", "☑"} {
		if strings.Contains(stripANSI(block), glyph) {
			t.Errorf("a Bash call drew a checklist glyph %q:\n%s", glyph, stripANSI(block))
		}
	}
}

// The block is bounded like every other one in the pane.
func TestATaskListBlockIsBoundedToTheBlockWidth(t *testing.T) {
	long := strings.Repeat("a very long task description ", 12)
	items := make([]core.Todo, 12)
	for i := range items {
		items[i] = core.Todo{Text: long, Status: core.TodoPending}
	}
	for _, width := range []int{24, 40, 80} {
		d := NewDM("s1", "alex").SetSize(width, 20)
		for _, line := range strings.Split(d.eventBlock(todoCall(items...)), "\n") {
			if w := ansi.StringWidth(line); w > d.blockWidth() {
				t.Errorf("width %d: a row is %d cells, over the block's %d: %q",
					width, w, d.blockWidth(), stripANSI(line))
			}
		}
	}
}

// The agent's own present-tense label reaches the working line.
//
// A review found ActiveForm decoded in the airlock and dropped by every reader,
// which CLAUDE.md counts as a defect rather than slack. It is what claude puts
// on its own working line, so that is where it goes: `✻ Encoding the receipt…`
// rather than a word from a pool.
func TestTheWorkingLineSaysWhatTheAgentSaysItIsDoing(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(todoCall(
		core.Todo{Text: "encode the receipt", Status: core.TodoActive, ActiveForm: "Encoding the receipt"},
		core.Todo{Text: "refuse the verb", Status: core.TodoPending, ActiveForm: "Refusing the verb"},
	), "a")

	if got := f.agents["a"].Doing; got != "Encoding the receipt" {
		t.Fatalf("the agent's active form is %q, want %q", got, "Encoding the receipt")
	}
	line := stripANSI(workingLine("a", rpc.StateWorking, f.agents["a"].Doing, clock(), 0, 80))
	if !strings.Contains(line, "Encoding the receipt…") {
		t.Errorf("the working line is %q, want the agent's own label", line)
	}
}

// An agent that keeps no task list still gets a word, or the line goes blank
// for the ordinary case.
func TestTheWorkingLineFallsBackToThePool(t *testing.T) {
	line := stripANSI(workingLine("a", rpc.StateWorking, "", clock(), 0, 80))
	if !strings.Contains(line, "…") || len(strings.TrimSpace(line)) < 4 {
		t.Errorf("an agent with no task list drew %q", line)
	}
}

// A list with nothing in flight says nothing, rather than borrowing a pending
// item's label for work that has not started.
func TestNoActiveItemMeansNoLabel(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(todoCall(core.Todo{Text: "later", Status: core.TodoPending, ActiveForm: "Doing it later"}), "a")
	if got := f.agents["a"].Doing; got != "" {
		t.Errorf("a list with nothing in flight reported %q", got)
	}
}

// The label is what the agent is doing *now*. Carried into a later turn it
// would be a claim about work that is not happening, which is the rule Tool
// already keeps one field over.
func TestTheActiveLabelIsEmptyBetweenTurns(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(todoCall(core.Todo{Text: "x", Status: core.TodoActive, ActiveForm: "Doing x"}), "a")
	if f.agents["a"].Doing == "" {
		t.Fatal("the label was never recorded")
	}
	f, _ = f.Observe(core.Event{Kind: core.KindTurnEnd, SessionID: "a"}, "a")
	if got := f.agents["a"].Doing; got != "" {
		t.Errorf("the label survived the turn as %q", got)
	}
}

package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// distinctForegrounds counts the different 24-bit foreground SGR sequences in s.
func distinctForegrounds(s string) int {
	seen := map[string]bool{}
	for _, m := range ansiPattern.FindAllString(s, -1) {
		if strings.Contains(m, "38;2;") {
			seen[m] = true
		}
	}
	return len(seen)
}

func todos(n int, status TodoState) []Todo {
	out := make([]Todo, n)
	for i := range out {
		out[i] = Todo{Text: "item", Status: status}
	}
	return out
}

// The three glyphs, and the one that is the point: a reader scanning a list
// wants the row in flight, and it is the only one drawn in the accent.
func TestTheTaskListDrawsAGlyphPerStatus(t *testing.T) {
	out := stripANSI(TodoList([]Todo{
		{Text: "in flight", Status: TodoActive},
		{Text: "to come", Status: TodoPending},
		{Text: "behind us", Status: TodoDone},
	}, 60))

	for _, want := range []string{activeGlyph + " in flight", pendingGlyph + " to come", doneGlyph + " behind us"} {
		if !strings.Contains(out, want) {
			t.Errorf("the list is missing %q:\n%s", want, out)
		}
	}
}

func TestOnlyTheItemInFlightIsAccented(t *testing.T) {
	forceTrueColour(t)
	out := TodoList([]Todo{
		{Text: "in flight", Status: TodoActive},
		{Text: "to come", Status: TodoPending},
	}, 60)

	lines := strings.Split(out, "\n")
	if distinctForegrounds(lines[0]) == 0 {
		t.Error("the item in flight is not coloured at all")
	}
	if lines[0] == lines[1] {
		t.Error("both rows render identically; the accent says nothing")
	}
}

// The block opens under the tool call the way a result does, so a list and a
// result do not sit in different columns.
func TestTheTaskListOpensWithTheResultBullet(t *testing.T) {
	out := stripANSI(TodoList(todos(2, TodoPending), 60))
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], resultBullet) {
		t.Errorf("the first row is %q, want it to open with %q", lines[0], resultBullet)
	}
	if !strings.HasPrefix(lines[1], todoLead) {
		t.Errorf("a later row is %q, want it indented to %q", lines[1], todoLead)
	}
}

// A plan is arbitrary length and this is one block beside a conversation.
func TestALongTaskListIsBounded(t *testing.T) {
	out := TodoList(todos(40, TodoPending), 60)
	if got := len(strings.Split(out, "\n")); got > maxTodoLines+1 {
		t.Errorf("a 40-item list drew %d rows, want at most %d plus the count", got, maxTodoLines)
	}
	if !strings.Contains(stripANSI(out), "pending") {
		t.Errorf("nothing said how many were cut:\n%s", stripANSI(out))
	}
}

// What survives the cut is the work in flight and what is next - never the
// finished half, which is the part that only grows.
func TestTheItemInFlightSurvivesTheCut(t *testing.T) {
	list := append(todos(maxTodoLines+4, TodoDone), Todo{Text: "the live one", Status: TodoActive})
	out := stripANSI(TodoList(list, 60))
	if !strings.Contains(out, "the live one") {
		t.Errorf("a long finished list pushed the item in flight off the block:\n%s", out)
	}
}

// The agent's own sequence is part of what a plan says.
func TestTheTaskListKeepsTheAgentsOrder(t *testing.T) {
	out := stripANSI(TodoList([]Todo{
		{Text: "first", Status: TodoDone},
		{Text: "second", Status: TodoActive},
		{Text: "third", Status: TodoPending},
	}, 60))
	if a, b, c := strings.Index(out, "first"), strings.Index(out, "second"), strings.Index(out, "third"); a >= b || b >= c {
		t.Errorf("the list was reordered:\n%s", out)
	}
}

func TestTheTaskListIsBoundedToWidth(t *testing.T) {
	list := []Todo{{Text: strings.Repeat("verylongword", 20), Status: TodoActive}}
	for _, width := range []int{10, 20, 40, 80} {
		for _, line := range strings.Split(TodoList(list, width), "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width %d: a row is %d cells: %q", width, w, stripANSI(line))
			}
		}
	}
}

func TestAnEmptyTaskListDrawsNothing(t *testing.T) {
	if got := TodoList(nil, 60); got != "" {
		t.Errorf("an empty list drew %q", got)
	}
}

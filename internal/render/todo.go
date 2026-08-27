package render

// An agent's task list, the way Claude Code draws one.
//
// The glyphs are Claude's: ☐ and ☑ are in the shipped binary, and the filled
// square marking the item in progress is measured off a side-by-side. Colour
// carries the same split - the item being worked on is the accent, everything
// else recedes - because the question this block answers at a glance is "what
// is it doing now", and a checklist where every row is equally bright answers
// it no better than no checklist.
//
// The list is bounded. An agent's plan is arbitrary length and this block sits
// in a transcript beside thirty others; what is kept is the work in flight and
// what is coming, because what is already done is the half a reader can
// reconstruct and the half that only grows.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	pendingGlyph = "☐"
	activeGlyph  = "■"
	doneGlyph    = "☑"

	// todoLead indents the list under the ⎿ that opens it, so the items line
	// up with a tool result's body rather than with the tool's name.
	todoLead = "     "

	// maxTodoLines bounds one list. Chosen as what fits beside a conversation
	// rather than derived: a plan is arbitrary length and this is one block in
	// a transcript.
	maxTodoLines = 6

	// todoMoreFormat reports what was cut, and says *pending* rather than
	// "more" because that is what a reader is deciding about.
	todoMoreFormat = "%s +%d pending"
)

var (
	// The item in flight is the accent, and bold on top of it: this is the one
	// row the block exists to point at.
	activeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#d77757", Dark: "#d77757"}).
			Bold(true)

	// Everything not in flight recedes to Claude's `inactive`.
	restStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"})
)

// TodoState is what a drawn item is: this package's own three, mapped from
// core's by the caller. Neither side names Claude's words - the airlock
// resolved those already.
type TodoState int

const (
	TodoPending TodoState = iota
	TodoActive
	TodoDone
)

// Todo is one item as this package draws it. It mirrors core.Todo without
// importing it: this package takes plain values and never imports core.
type Todo struct {
	Text   string
	Status TodoState
}

// TodoList renders an agent's task list under a tool call, bounded to width.
//
// The order is the agent's own, never sorted: the list is a plan, and the
// sequence is part of what it says.
func TodoList(todos []Todo, width int) string {
	if len(todos) == 0 || width < 1 {
		return ""
	}
	shown, hidden := budget(todos)

	var b strings.Builder
	for i, t := range shown {
		prefix := todoLead
		if i == 0 {
			prefix = resultBullet
		}
		style, glyph := restStyle, glyphFor(t.Status)
		if t.Status == TodoActive {
			style = activeStyle
		}
		b.WriteString(style.Render(ansi.Truncate(prefix+glyph+" "+collapseWhitespace(t.Text), width, ellipsis)))
		b.WriteString("\n")
	}
	if hidden > 0 {
		more := fmt.Sprintf(todoMoreFormat, ellipsis, hidden)
		b.WriteString(restStyle.Render(ansi.Truncate(todoLead+more, width, ellipsis)))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// glyphFor is the box a status is drawn with. An unrecognised status gets
// pending's: the set is Claude's and can grow, and an item nobody has ruled on
// is one still to do rather than one to hide.
func glyphFor(status TodoState) string {
	switch status {
	case TodoActive:
		return activeGlyph
	case TodoDone:
		return doneGlyph
	default:
		return pendingGlyph
	}
}

// budget picks which items are drawn and reports how many pending ones were
// cut.
//
// Completed items are dropped first and the in-progress one is never dropped,
// which is the whole of the policy: a list is worth a block because it says
// what is happening and what is next, and a long plan's finished half pushes
// both off the screen. Nothing is reordered - the survivors keep the agent's
// own sequence.
func budget(todos []Todo) (shown []Todo, hiddenPending int) {
	if len(todos) <= maxTodoLines {
		return todos, 0
	}
	keep := make([]bool, len(todos))
	room := maxTodoLines

	// In flight first, then what is coming, then what is done - and each pass
	// walks the list in the agent's order so the kept rows stay in sequence.
	for _, want := range []TodoState{TodoActive, TodoPending, TodoDone} {
		for i, t := range todos {
			if room == 0 {
				break
			}
			if !keep[i] && statusMatches(t.Status, want) {
				keep[i], room = true, room-1
			}
		}
	}
	for i, t := range todos {
		if keep[i] {
			shown = append(shown, t)
			continue
		}
		if statusMatches(t.Status, TodoPending) {
			hiddenPending++
		}
	}
	return shown, hiddenPending
}

// statusMatches folds anything that is not active or done in with pending, so
// the budget treats an item the way glyphFor draws it.
func statusMatches(status, want TodoState) bool {
	if want == TodoPending {
		return status != TodoActive && status != TodoDone
	}
	return status == want
}

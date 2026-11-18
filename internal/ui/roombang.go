package ui

// A `!cmd` in the room: the operator's message on its own shaded row, and what the
// command printed under it in the peer fold's Muted body (peerfold.go) - peerFoldRows
// rows and a pointer, opened by ⌃E or a click. The rows Wake put under the output
// (`… exited 1`, `… timed out`) stay drawn under the pointer, so a fold never hides
// how a command ended. A live bang and a reopened room's read-back are one line
// (bangLine), so they draw alike.

import (
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
)

// bangLine is a bang as the room's one line of text: who it was aimed at (name, empty
// for nobody), the command, then what it printed, unfenced since the room draws no
// markdown there.
func bangLine(ev core.Event, name string) core.Event {
	ev.Text = core.Unfence(ev.Text)
	if name != "" {
		ev.Text = agentPrefix + name + " " + ev.Text
	}
	return ev
}

// bangParts splits a room bang's text into its message row, what the command
// printed, and the rows of Wake's own notes under that.
func bangParts(text string) (head, out, notes string) {
	head, rest, _ := strings.Cut(text, "\n")
	lines := strings.Split(rest, "\n")
	n := len(lines)
	for n > 0 && strings.HasPrefix(lines[n-1], bangNoteLead) {
		n--
	}
	return head, strings.Join(lines[:n], "\n"), strings.Join(lines[n:], "\n")
}

// bangBody is what a command printed as the room draws it, tabs at their columns.
func bangBody(out string, width int) string {
	return crossSessionBody(render.ExpandTabs(out), width)
}

// bangSaid draws a room bang: the message, what it printed folded, then the notes.
func bangSaid(text string, width int, expanded bool) string {
	head, out, notes := bangParts(text)
	return joinBlock(youSaid(head, width), foldRows(bangBody(out, width), width, expanded), crossSessionBody(notes, width))
}

// bangFolds reports whether a room bang's output draws as a pointer at this width.
func bangFolds(text string, width int) bool {
	_, out, _ := bangParts(text)
	return renderedRows(bangBody(out, width)) > peerFoldRows
}

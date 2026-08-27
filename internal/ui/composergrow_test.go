package ui

// The box grows with the draft, so what you are typing is what you can see.
//
// It shipped one row high and two separate things kept a long draft off screen:
// the box never grew, and the text area's viewport never followed the cursor -
// Composer.Update inserts typed runes directly, which is the one path that does
// not end in bubbles' repositionView.

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// composerRows is how many rows the box itself spends, borders included.
func composerRows(c Composer, width int) int {
	rows := strings.Split(stripANSI(c.box(width)), "\n")
	return len(rows)
}

// A draft with explicit newlines is visible in full.
func TestTheComposerShowsEveryLineOfAMultiLineDraft(t *testing.T) {
	a := newRoomApp(t).withSize(80, 30).withDraft("first line\nsecond line\nthird line")
	out := stripANSI(a.room.Composer().View(60))

	for _, want := range []string{"first line", "second line", "third line"} {
		if !strings.Contains(out, want) {
			t.Errorf("the composer does not show %q - a draft you cannot see is one you cannot correct:\n%s", want, out)
		}
	}
}

// And so is one long enough to wrap, which is the case that needs no newline
// key to reach.
func TestTheComposerShowsADraftThatWrapped(t *testing.T) {
	a := newRoomApp(t).withSize(80, 30).withDraft(
		"the quick brown fox jumps over the lazy dog and keeps running well past the edge of the box")
	out := stripANSI(a.room.Composer().View(60))

	if !strings.Contains(out, "past the edge") {
		t.Errorf("the composer cut the draft at one row, so the end of what was typed is invisible:\n%s", out)
	}
}

// manyLines is a draft of n numbered lines, which is the shape that reaches the
// cap without depending on how anything wraps.
func manyLines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "line %d", i)
	}
	return b.String()
}

// The box stops growing at the cap, and what you are typing stays visible past
// it - the draft scrolls under the cursor instead.
func TestTheComposerStopsGrowingAtTheCapAndKeepsTheCursorVisible(t *testing.T) {
	a := newRoomApp(t).withSize(120, 60).withDraft(manyLines(20))
	c := a.room.Composer()

	if got, want := composerRows(c, 100), maxComposerRows+boxBorderRows; got != want {
		t.Errorf("a 20-line draft draws a box of %d rows, want %d - the cap is what stops one draft taking the pane", got, want)
	}
	out := stripANSI(c.View(100))
	if !strings.Contains(out, "line 20") {
		t.Errorf("the last line typed is not on screen, so past the cap you are typing blind again:\n%s", out)
	}
}

// A pane too short for the cap bounds it further, and the frame still fits the
// terminal. A frame one row too tall scrolls the alt screen away on every draw.
//
// **The row count below cannot fail on its own**, which is why it is no longer
// the only assertion here: App.View ends in firstRows, so the frame is cut to
// the terminal whatever the panes did. It is kept because it states the
// invariant; the strip is what makes the statement checkable, since the
// overflow never appears on screen - it pushes the bottom rows off it.
func TestALongDraftInAShortPaneStillFitsTheTerminal(t *testing.T) {
	const rows = 14
	a := newRoomApp(t).withSize(100, rows).withAgents("sydney").withDraft(manyLines(20))

	if got := len(strings.Split(a.View(), "\n")); got > rows {
		t.Errorf("the frame is %d rows in a %d-row terminal: a frame taller than the terminal scrolls the alt screen away on every draw.\n%s", got, rows, stripANSI(a.View()))
	}
	if want := labelOf(rpc.StateIdle); !strings.Contains(stripANSI(a.View()), want) {
		t.Errorf("the awareness strip does not say %q with a long draft open: the room drew past the height it was given and the cut took the bottom of the frame.\n%s", want, stripANSI(a.View()))
	}
}

// The room draws exactly the height it was given while a draft grows in it.
//
// A keystroke grows the box and re-sizes nothing, so the transcript is still
// laid out for the chrome the composer had before it grew, and the pane draws
// taller than the height it was handed. The symptom is not a box that looks
// wrong: firstRows cuts the overflow off the *bottom* of the frame, so what
// goes missing is the awareness strip, the notice row, and past those the box's
// own last rows and the hint under it.
//
// DM.chrome is this same rule, and the room never had its half - which is why a
// draft in a conversation stays in bounds while the same draft in the room does
// not.
func TestTheRoomIsTheHeightItWasGivenWhileItsComposerGrows(t *testing.T) {
	a := newRoomApp(t).withSize(100, 30).withAgents("sydney").withDraft(manyLines(6))

	w, h := a.regions().Room(), a.paneHeight()
	if got := len(strings.Split(a.room.View(w, h), "\n")); got != h {
		t.Errorf("the room draws %d rows in a pane %d tall: every row past the height it was given is a row cut off the bottom of the frame", got, h)
	}
}

// And it shrinks back, or every draft you ever typed leaves the box tall.
func TestTheComposerShrinksBackWhenTheDraftDoes(t *testing.T) {
	const draft = "first\nsecond\nthird"
	a := newRoomApp(t).withSize(80, 30).withDraft(draft)
	grown := composerRows(a.room.Composer(), 60)
	if grown <= 1+boxBorderRows {
		t.Fatalf("the box is %d rows with a three-line draft, so this test has nothing to shrink", grown)
	}

	for range draft {
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if got := composerRows(a.room.Composer(), 60); got != 1+boxBorderRows {
		t.Errorf("the box was %d rows with a draft and is %d with none, want it back to %d - a box that only grows is one every long draft leaves tall", grown, got, 1+boxBorderRows)
	}
}

// An empty composer is still one row. The box growing is what makes a long
// draft readable; a box that started tall would spend the transcript's rows on
// nothing.
func TestAnEmptyComposerIsStillOneRow(t *testing.T) {
	a := newRoomApp(t).withSize(80, 30)
	if got := composerRows(a.room.Composer(), 60); got != 1+boxBorderRows {
		t.Errorf("an empty composer draws %d rows, want %d", got, 1+boxBorderRows)
	}
}

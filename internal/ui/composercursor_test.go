package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// caretTestRows renders a composer's drawn draft rows exactly as composerRegion
// captures them at a press: the box's interior only, dropping both the top and
// bottom borders, so len(rows) == draftRows. That count is what composerRowStarts
// uses as its scroll-decline bound, so the helper must match what clickedComposer
// really passes (a.cdrag.rows) rather than carry an extra border row.
func caretTestRows(c Composer, width int) []string {
	return strings.Split(c.box(width), "\n")[1 : 1+c.ta.Height()]
}

// A click lands the caret onto the clicked character - the block cursor sits on
// it, which an inserted marker before it proves. The marker is the only
// unambiguous read of where the caret went.
func TestCaretAtPointOnASingleLineDraft(t *testing.T) {
	width := 30
	c := NewComposer().SetWidth(width)
	c.ta.InsertString("hello world")
	c = c.fit()

	c, ok := c.caretAtPoint(point{0, 6}, caretTestRows(c, width), width) // the 'w'
	if !ok {
		t.Fatal("caretAtPoint declined a plain single-line click")
	}
	c.ta.InsertRune('|')
	if got := c.ta.Value(); got != "hello |world" {
		t.Errorf("a click on the 'w' placed the caret so a marker landed %q, want %q", got, "hello |world")
	}
}

// A click on the second line moves the caret down a row and into that line, not
// the first - the row navigation, done through the text area's own wrap.
func TestCaretAtPointOnASecondLine(t *testing.T) {
	width := 30
	c := NewComposer().SetWidth(width)
	c.ta.InsertString("hello\nworld")
	c = c.fit()

	c, ok := c.caretAtPoint(point{1, 2}, caretTestRows(c, width), width) // "wo|rld"
	if !ok {
		t.Fatal("caretAtPoint declined a two-line click")
	}
	c.ta.InsertRune('|')
	if got := c.ta.Value(); got != "hello\nwo|rld" {
		t.Errorf("a click on row 1 col 2 placed the caret so a marker landed %q, want %q", got, "hello\nwo|rld")
	}
}

// A click at column 0 lands the caret at the row's start, and a click past the
// end clamps to the end of that row's text - both stay within the typed
// characters. A fresh composer per click, because bubbles shares the draft
// buffer by pointer, so a marker inserted into one copy would change the other.
func TestCaretAtPointClampsToTheRowsText(t *testing.T) {
	width := 30
	clickMarker := func(col int) string {
		c := NewComposer().SetWidth(width)
		c.ta.InsertString("hi")
		c = c.fit()
		c, ok := c.caretAtPoint(point{0, col}, caretTestRows(c, width), width)
		if !ok {
			t.Fatalf("caretAtPoint declined a click at column %d", col)
		}
		c.ta.InsertRune('|')
		return c.ta.Value()
	}

	if got := clickMarker(0); got != "|hi" {
		t.Errorf("a click at column 0 landed the marker %q, want %q", got, "|hi")
	}
	if got := clickMarker(40); got != "hi|" { // far past the two characters
		t.Errorf("a click well past the text landed the marker %q, want %q", got, "hi|")
	}
}

// A draft taller than the box has scrolled inside it, so a drawn-row index is no
// longer a visual-row index and there is no public way to read the scroll
// offset. caretAtPoint declines rather than placing the caret on the wrong row;
// the arrow keys still position in that case.
func TestCaretAtPointDeclinesAScrolledDraft(t *testing.T) {
	width := 30
	// Three lines in a two-row box is the tightest scrolled case - one row off
	// screen. It is the boundary where an inflated row count (a helper that kept a
	// border row) would wrongly accept, so caretTestRows must feed exactly the
	// draftRows production passes for this to prove the decline.
	c := NewComposer().SetWidth(width).WithMaxRows(2)
	c.ta.InsertString("l1\nl2\nl3")
	c = c.fit()
	rows := caretTestRows(c, width)

	wasRow, wasCol := c.ta.Line(), c.ta.LineInfo().ColumnOffset
	if got, ok := c.caretAtPoint(point{0, 0}, rows, width); ok {
		t.Fatalf("caretAtPoint placed the caret in a scrolled draft (now line %d): it should decline", got.ta.Line())
	}
	if c.ta.Line() != wasRow || c.ta.LineInfo().ColumnOffset != wasCol {
		t.Error("a declined placement still moved the caret")
	}
}

// End to end through the real mouse pipeline: a click (press then release, no
// motion) on a typed character places the caret onto it and leaves no selection.
func TestAClickOnQueryBoxTextPlacesTheCaret(t *testing.T) {
	a := splitApp(t, 200, 40, 4).withDraft("hello world")
	r := a.regions()
	w, h := r.Room(), a.paneHeight()
	draftTop, _, _, _, ok := a.composerRegion("", w, 0, h)
	if !ok {
		t.Fatal("the room drew no composer region to click in")
	}
	left := a.layout.PaneLeft(r, 0) + composerTextLeft

	a, _ = a.mouse(pressAt(left+6, draftTop))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: left + 6, Y: draftTop})

	c := a.composer()
	c.ta.InsertRune('|')
	if got := c.ta.Value(); got != "hello |world" {
		t.Errorf("after clicking the 'w', a typed marker landed %q, want %q", got, "hello |world")
	}
	if !a.sel.empty() {
		t.Errorf("a click on the query box left a selection behind: %+v", a.sel)
	}
}

// A click that places the caret must not highlight the row: it is a click, not a
// drag, so composerSelectionIn stays empty and nothing is copied.
func TestAClickToPlaceTheCaretCopiesNothing(t *testing.T) {
	a := splitApp(t, 200, 40, 4).withDraft("hello world")
	r := a.regions()
	w, h := r.Room(), a.paneHeight()
	draftTop, _, _, _, _ := a.composerRegion("", w, 0, h)
	left := a.layout.PaneLeft(r, 0) + composerTextLeft

	a, _ = a.mouse(pressAt(left+6, draftTop))
	got, _ := a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: left + 6, Y: draftTop})

	if got.composerSelectionIn("") != (marked{}) {
		t.Errorf("a caret click highlighted the row: %+v", got.composerSelectionIn(""))
	}
}

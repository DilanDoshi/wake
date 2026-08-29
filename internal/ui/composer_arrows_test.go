package ui

// CanCursorUp/CanCursorDown are the predicate App.key reads to decide whether a
// plain ↑/↓ moves the text cursor or the roster: the cursor when it has a
// display row to move into, the roster when it does not. See keys.go.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// An empty or single-logical-line draft has nowhere for ↑/↓ to move the cursor,
// so both predicates are false and the arrow falls to the roster.
func TestCursorCannotMoveInEmptyOrSingleLineDraft(t *testing.T) {
	for _, tc := range []struct {
		what  string
		draft string
	}{
		{"empty", ""},
		{"single line", "hello"},
	} {
		c := NewComposer().SetWidth(80).WithDraft(tc.draft)
		if c.CanCursorUp() {
			t.Errorf("%s draft: CanCursorUp is true, so ↑ would edit text where it should move the roster", tc.what)
		}
		if c.CanCursorDown() {
			t.Errorf("%s draft: CanCursorDown is true, so ↓ would edit text where it should move the roster", tc.what)
		}
	}
}

// A multi-line draft lets the cursor move toward the line it is not on, and
// falls through to the roster only at the edge it is already against.
func TestCursorMovesWithinMultiLineDraftAndYieldsAtTheEdges(t *testing.T) {
	// Cursor starts at the end - the last line's last column.
	c := NewComposer().SetWidth(80).WithDraft("first\nsecond")
	if !c.CanCursorUp() {
		t.Fatal("bottom of a two-line draft: CanCursorUp is false, so ↑ cannot reach the line above")
	}
	if c.CanCursorDown() {
		t.Fatal("bottom of a two-line draft: CanCursorDown is true, so ↓ would edit text at the last line instead of moving the roster")
	}

	// Move the cursor onto the top line. Now ↑ has nowhere to go and ↓ does.
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyUp})
	if c.ta.Line() != 0 {
		t.Fatalf("cursor did not move to the top line after ↑: on line %d", c.ta.Line())
	}
	if c.CanCursorUp() {
		t.Error("top of a two-line draft: CanCursorUp is true, so ↑ would edit text instead of moving the roster")
	}
	if !c.CanCursorDown() {
		t.Error("top of a two-line draft: CanCursorDown is false, so ↓ cannot reach the line below")
	}
}

// A single logical line that soft-wraps is genuinely multi-row on screen, so
// ↑/↓ move the cursor between its wrapped rows even though it holds no newline.
// This is why the predicate reads the actual move rather than the logical line.
func TestCursorMovesBetweenSoftWrappedRows(t *testing.T) {
	// Narrow pane, one long word: one logical line wrapped across several rows.
	c := NewComposer().SetWidth(12).WithDraft("aaaaaaaaaaaaaaaaaaaa")
	if !c.CanCursorUp() {
		t.Error("cursor at the end of a soft-wrapped line: CanCursorUp is false, so ↑ cannot climb the wrap")
	}
	if c.CanCursorDown() {
		t.Error("cursor at the end of a soft-wrapped line: CanCursorDown is true, so ↓ would edit text at the last row")
	}
}

// The predicate must equal what a real ↓/↑ actually does, at every width and
// length - not what LineInfo's row count implies. bubbles' wrap adds a synthetic
// trailing row when a line's width is exactly the wrap width (its `>=`), which
// Height counts but the cursor cannot occupy: a count-based predicate returns
// true there while CursorDown is a no-op, so ↓ moves neither the cursor nor the
// roster. Sweeping the length past the wrap boundary at several widths lands on
// that exact-width case and pins the predicate to the movement.
func TestCanCursorMatchesRealMovementAcrossWidths(t *testing.T) {
	for _, w := range []int{10, 12, 16, 20} {
		for length := 1; length <= 44; length++ {
			line := strings.Repeat("a", length)
			base := NewComposer().SetWidth(w).WithDraft(line)
			// Walk the cursor across every column so a wrapped line is probed
			// from its first row, its middle, and its end.
			for col := length; col >= 0; col-- {
				predictedDown := base.CanCursorDown()
				predictedUp := base.CanCursorUp()

				down, _ := base.Update(tea.KeyMsg{Type: tea.KeyDown})
				if movedDown := cursorRow(down.ta) != cursorRow(base.ta); movedDown != predictedDown {
					t.Fatalf("width %d, len %d, col %d: CanCursorDown()=%v but a real ↓ moved=%v — ↓ is swallowed (neither cursor nor roster)",
						w, length, col, predictedDown, movedDown)
				}
				up, _ := base.Update(tea.KeyMsg{Type: tea.KeyUp})
				if movedUp := cursorRow(up.ta) != cursorRow(base.ta); movedUp != predictedUp {
					t.Fatalf("width %d, len %d, col %d: CanCursorUp()=%v but a real ↑ moved=%v — ↑ is swallowed",
						w, length, col, predictedUp, movedUp)
				}
				base, _ = base.Update(tea.KeyMsg{Type: tea.KeyLeft})
			}
		}
	}
}

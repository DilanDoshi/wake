package ui

import (
	"testing"
)

func TestSelectedTextTakesWhatIsUnderTheSelectionAndNothingElse(t *testing.T) {
	lines := []string{"alpha bravo", "charlie", "delta echo"}
	for _, c := range []struct {
		name string
		sel  selection
		want string
	}{
		{"within one line", selection{anchor: point{0, 6}, head: point{0, 10}}, "bravo"},
		{"across three", selection{anchor: point{0, 6}, head: point{2, 4}}, "bravo\ncharlie\ndelta"},
		{"a whole line", selection{anchor: point{1, 0}, head: point{1, 6}}, "charlie"},
	} {
		if got := selectedText(lines, 0, c.sel.marked()); got != c.want {
			t.Errorf("%s: selectedText = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSelectedTextDropsStylingAndTheTrailingPad(t *testing.T) {
	// Every line in a pane is padded to the pane width by lipgloss, and the
	// blocks are styled. Neither belongs on the clipboard.
	lines := []string{"\x1b[31mred\x1b[0m        "}
	if got := selectedText(lines, 0, selection{anchor: point{0, 0}, head: point{0, 10}}.marked()); got != "red" {
		t.Errorf("selectedText = %q, want %q: ANSI stripped and the pad trimmed", got, "red")
	}
}

func TestSelectedTextIsOffsetByTheFirstLineOnScreen(t *testing.T) {
	// A selection is anchored to absolute indices; the caller passes the slice
	// it has and says where that slice starts.
	lines := []string{"charlie", "delta echo"}
	if got := selectedText(lines, 1, selection{anchor: point{1, 0}, head: point{1, 6}}.marked()); got != "charlie" {
		t.Errorf("selectedText = %q, want %q", got, "charlie")
	}
}

func TestASpanOrdersItsEndsWhicheverWayTheDragWent(t *testing.T) {
	forward := selection{anchor: point{2, 3}, head: point{5, 7}}
	backward := selection{anchor: point{5, 7}, head: point{2, 3}}
	if forward.marked() != backward.marked() {
		t.Errorf("a drag up and the same drag down are one span: %+v vs %+v", forward.marked(), backward.marked())
	}
}

func TestASpanIncludesTheCellUnderThePointer(t *testing.T) {
	// Anchor on column 3, pointer on column 6: four cells are under the drag.
	got := selection{anchor: point{1, 3}, head: point{1, 6}}.marked()
	if want := (marked{from: point{1, 3}, to: point{1, 7}}); got != want {
		t.Errorf("marked() = %+v, want %+v: the end is half-open and the pointer's own cell is inside", got, want)
	}
}

func TestADragThatNeverMovedSelectsNothing(t *testing.T) {
	if !(selection{anchor: point{4, 9}, head: point{4, 9}}).empty() {
		t.Error("a press and release on one cell is a click, not a selection")
	}
}

func TestCoversAnswersTheColumnsOfEachLineItCrosses(t *testing.T) {
	sp := selection{anchor: point{2, 4}, head: point{4, 6}}.marked()
	for _, c := range []struct {
		line   int
		c0, c1 int
		ok     bool
		why    string
	}{
		{1, 0, 0, false, "before the span"},
		{2, 4, lineEnd, true, "the first line runs from the anchor to the end of the line"},
		{3, 0, lineEnd, true, "a middle line is whole"},
		{4, 0, 7, true, "the last line stops after the pointer's cell"},
		{5, 0, 0, false, "after the span"},
	} {
		c0, c1, ok := sp.covers(c.line)
		if ok != c.ok || (ok && (c0 != c.c0 || c1 != c.c1)) {
			t.Errorf("covers(%d) = %d, %d, %v; want %d, %d, %v: %s", c.line, c0, c1, ok, c.c0, c.c1, c.ok, c.why)
		}
	}
}

func TestASelectionIsClampedToItsOwnPane(t *testing.T) {
	got := selection{anchor: point{1, -4}, head: point{2, 99}}.clampedTo(40)
	if want := (selection{anchor: point{1, 0}, head: point{2, 39}}); got != want {
		t.Errorf("clampedTo(40) = %+v, want %+v: a drag into the next column continues at this pane's edge", got, want)
	}
}

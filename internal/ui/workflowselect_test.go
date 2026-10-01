package ui

// The /workflows view's text is selectable like any chrome: a drag over it is a
// frame-wide screen selection, copied on release, while a press still moves the
// view's cursor as it always did.

import (
	"strings"
	"testing"
)

// sumCell is the Sum phase's row on the run level alex's pane opens at.
func sumCell(t *testing.T, a App) (x, y int) {
	t.Helper()
	x, y, ok := viewCell(a, "2 Sum")
	if !ok {
		t.Fatalf("the Sum phase is not on screen:\n%s", stripANSI(a.View()))
	}
	return x, y
}

// The reported gap: the view was the one drawn surface a drag could not copy.
func TestADragAcrossTheViewCopiesWhatItCrossed(t *testing.T) {
	a := runOpen(t)
	x, y := sumCell(t, a)
	got, cmd := drag(a, x, x+len("Sum")-1, y)
	if !got.sel.onScreen {
		t.Fatalf("sel = %+v: a drag over the view is a screen selection", got.sel)
	}
	if text := got.screenSelectedText(); text != "Sum" {
		t.Errorf("the drag copied %q, want the cells it crossed, %q", text, "Sum")
	}
	if cmd == nil {
		t.Error("the release copied nothing")
	}
	if v := got.workflow.view; !v.Open() || v.Cursor != 1 {
		t.Errorf("the press under the drag left %+v, want the Sum phase the cursor", v)
	}
}

// A click is still the view's own gesture: it moves the cursor and copies nothing.
func TestAClickOnTheViewMovesItsCursorAndCopiesNothing(t *testing.T) {
	a := runOpen(t)
	x, y := sumCell(t, a)
	got, cmd := click(a, x, y)
	if cmd != nil || !got.sel.empty() {
		t.Errorf("a click on the view copied (%v) or highlighted %+v, want neither", cmd != nil, got.sel)
	}
	if v := got.workflow.view; !v.Open() || v.Cursor != 1 {
		t.Errorf("a click on the Sum phase left %+v, want it the cursor", v)
	}
}

// A press on the view is a new gesture, so a highlight left elsewhere goes.
func TestAPressOnTheViewTakesAnEarlierHighlightDown(t *testing.T) {
	a := runOpen(t)
	a.layout.ShowRoster = true
	a = a.resizePanes()
	ry, ok := rosterRow(a)
	if !ok {
		t.Fatal("no roster row on screen to drag across")
	}
	a, _ = drag(a, rosterLeft(a), rosterLeft(a)+5, ry)
	if a.sel.empty() {
		t.Fatal("the roster drag highlighted nothing, so its going proves nothing")
	}
	x, y := sumCell(t, a)
	a, _ = click(a, x, y)
	if !a.sel.empty() {
		t.Errorf("a click on the view left the roster's highlight standing: %+v", a.sel)
	}
}

// A double-click selects the word under it, a triple-click its row, on the
// view as on every other selectable surface.
func TestADoubleClickOnTheViewSelectsAWordAndATripleItsRow(t *testing.T) {
	frozenClock(t)
	a := runOpen(t)
	x, y := sumCell(t, a)
	a, cmd := clicks(a, x, y, 2)
	if got := selectedNow(a); got != "Sum" || cmd == nil {
		t.Errorf("a double-click selected %q (copied %v), want %q copied", got, cmd != nil, "Sum")
	}
	a, _ = clicks(a, x, y, 1)
	if got := selectedNow(a); !strings.Contains(got, "2 Sum") {
		t.Errorf("a triple-click selected %q, want the row holding %q", got, "2 Sum")
	}
}

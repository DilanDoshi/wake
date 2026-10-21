package ui

// The page and the selection code each read the pane's rows, so a gesture has to
// land on the rows the operator is looking at.

import "testing"

// Dragging a selection past the pane's edge scrolls the transcript it is taken
// in, whatever the wheel would do: the page is not the thing being read.
func TestAnEdgeDragScrollsTheTranscriptNotThePage(t *testing.T) {
	a := askingPage(t, 40)
	was := a.transcriptIn("s1").scroll

	a = a.press(pageWheelX, 12)
	a, _ = a.extendSelection(pageWheelX, 0)

	if page := a.dms["s1"].page; page.set || page.up != 0 {
		t.Errorf("the drag moved the page: %+v", page)
	}
	if got := a.transcriptIn("s1").scroll; got >= was {
		t.Errorf("the transcript's top line went from %d to %d: the drag did not scroll it", was, got)
	}
}

// A card taller than its pane opens on its own top, and the composer is not
// under it: a press where the composer would sit is on the card, and takes the
// screen as drawn rather than moving a caret in a box nobody can see.
func TestAPressOnTheCardOfAShortPaneDoesNotSelectTheHiddenComposer(t *testing.T) {
	a := askingWith(t, pageSetup{height: 10, history: pageHistory})
	if hasRow(pageRows(a), "@john") {
		t.Fatal("the composer is on screen, so there is nothing hidden to select")
	}

	a = a.press(pageWheelX+10, a.paneHeight()-2)
	a, _ = a.extendSelection(pageWheelX+30, a.paneHeight()-2)

	if a.sel.inComposer {
		t.Errorf("a press on the card took a selection in the composer: %+v", a.sel)
	}
	if !a.sel.onScreen {
		t.Errorf("the press took no screen selection of what is drawn: %+v", a.sel)
	}
}

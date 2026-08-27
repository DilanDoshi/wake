package ui

// Where the completion menu sits in a pane, and the two things that move when
// it moves.
//
// It used to go up through App.paneChrome, which pins above the transcript, so
// it was drawn ~20 rows from the word it was completing. It is now Room.menu and
// DM.menu, drawn directly above the composer - which changes who is responsible
// for its height and takes it out of the arithmetic the mouse depends on.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// menuApp is a conversation with a menu up over a draft that begins a command.
func menuApp(t *testing.T) App {
	t.Helper()
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.advertising("s1", "clear", "compact", "context", "cost", "config").withDraft("/c")
	if a.completionView(60, a.focus) == "" {
		t.Fatal("no menu is drawn over `/c`, so nothing here is exercising one")
	}
	return a
}

// The menu is drawn between the transcript and the composer.
//
// Asserted as an ordering within the pane rather than as a row number: what the
// bug was is that the menu was *above* the transcript, and the pane's own first
// row is a function of the grid.
func TestTheMenuIsDrawnDirectlyAboveTheComposer(t *testing.T) {
	a := menuApp(t)

	rows := strings.Split(a.dmPane("s1", 60, 30), "\n")
	menu, box := -1, -1
	for i, row := range rows {
		if strings.Contains(row, "⇥") && strings.Contains(row, "complete") {
			menu = i
		}
		if box < 0 && strings.Contains(row, "@alex") {
			box = i
		}
	}
	if menu < 0 || box < 0 {
		t.Fatalf("menu row %d, composer row %d: one of them is not in the pane\n%s", menu, box, strings.Join(rows, "\n"))
	}
	if menu != box-1 {
		t.Errorf("the menu's last row is %d and the composer's border is %d, want the menu directly above it", menu, box)
	}
}

// A menu changes what a pane draws and never how tall it is.
//
// This is the failure the whole height accounting exists for: a frame one row
// taller than it was given scrolls the alt screen away on every draw. The menu
// runs to completionRows+1 rows, which is more than a short pane - or one of
// four grid panes - has spare, so DM.menuRows clips it against the transcript's
// floor, the composer's rule for the composer's reason.
//
// Measured against the *same pane with the draft taken back* rather than against
// the height asked for, because a pane already refuses to shrink below its own
// floor and that behaviour predates the menu. Swept rather than sampled: the
// bug this caught was one row too tall across a six-height band and correct at
// both ends of it, which one height would have missed.
func TestAMenuNeverMakesAPaneTallerThanItWasGiven(t *testing.T) {
	a := menuApp(t)
	bare := a.withComposer(a.composer().WithDraft(""))
	if bare.completionView(60, bare.focus) != "" {
		t.Fatal("clearing the draft left a menu up, so the two sides of this are the same")
	}

	floor := a.dmFor("s1").minHeight()
	squeezed := false
	for h := floor; h <= floor+completionRows+2; h++ {
		pane := a.dmPane("s1", 60, h)
		got, want := lipgloss.Height(pane), lipgloss.Height(bare.dmPane("s1", 60, h))
		if got != want {
			t.Fatalf("given %d rows the pane drew %d with a menu up and %d without: a frame taller than its pane scrolls the alt screen away on every draw\n%s",
				h, got, want, pane)
		}
		if !strings.Contains(pane, "⇥ complete") {
			squeezed = true
		}
	}
	if !squeezed {
		t.Error("the sweep never reached a height that clipped the menu, so it is not exercising the bound")
	}
}

// The menu is not rows the mouse has to know about, and that is the half of
// this change that could break silently.
//
// mouse.go's startSelection sets a drag's first line to the pane's top, with
// nothing to offset it by: the completion menu is drawn above the *composer*,
// not above the transcript. While it went through paneChrome it was above the
// transcript, so every drag in a pane moved by up to completionRows+1 the moment
// somebody typed `@`. Measured through a real drag, comparing the same pane with
// the draft taken back, so the claim is about the selection the mouse produces
// rather than about arithmetic that no longer exists.
func TestAMenuDoesNotMoveWhereASelectionStarts(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(narrowColumns, 40).withAgents("alex")
	a = pick(a, "s1").openDMWith("s1", "alex").applyGeometry()
	a = a.advertising("s1", "clear", "compact", "context").withDraft("/c")
	if a.completionView(narrowColumns, a.focus) == "" {
		t.Fatal("no menu is drawn over `/c`, so nothing here is exercising one")
	}

	withMenu, _ := drag(a, 2, 10, 3)
	if withMenu.sel.empty() {
		t.Fatal("the drag selected nothing, so there is no first line to compare")
	}

	// The same pane with the draft taken back, so the comparison is against the
	// menu this build actually draws rather than a number written here.
	bare := a.withComposer(a.composer().WithDraft(""))
	if bare.completionView(narrowColumns, bare.focus) != "" {
		t.Fatal("clearing the draft left a menu up, so the two sides of this are the same")
	}
	bareDrag, _ := drag(bare, 2, 10, 3)

	if withMenu.selTop != bareDrag.selTop {
		t.Errorf("a menu changed where a selection starts, from %d to %d: it is drawn above the composer now, "+
			"so a drag must land on the same line whether or not somebody is typing", bareDrag.selTop, withMenu.selTop)
	}
}

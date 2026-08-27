package ui

import (
	"slices"
	"testing"
)

// panesOf flattens a grid to "col.top/col.bottom" strings, which is what most
// of these assertions are about: where a conversation sits, not what it holds.
func panesOf(g Grid) []string {
	out := make([]string, 0, len(g.Cols))
	for _, c := range g.Cols {
		if c.Bottom == "" {
			out = append(out, c.Top)
			continue
		}
		out = append(out, c.Top+"/"+c.Bottom)
	}
	return out
}

func TestANewGridIsTheRoomAlone(t *testing.T) {
	g := NewGrid()
	if got := panesOf(g); !slices.Equal(got, []string{""}) {
		t.Fatalf("NewGrid() = %v, want one column holding the room", got)
	}
	if !g.Has("") {
		t.Error("NewGrid() does not hold the room, which is the one pane that is always there")
	}
}

func TestOpenRightPutsAConversationInItsOwnColumn(t *testing.T) {
	g := NewGrid().OpenRight("", "alex")
	if got := panesOf(g); !slices.Equal(got, []string{"", "alex"}) {
		t.Fatalf("OpenRight after the room = %v, want the room then alex", got)
	}
}

// The whole point of the key: a third conversation lands beside the second
// rather than replacing it.
func TestOpenRightInsertsImmediatelyRightOfTheColumnAskedFor(t *testing.T) {
	g := NewGrid().OpenRight("", "alex").OpenRight("alex", "sydney")
	if got := panesOf(g); !slices.Equal(got, []string{"", "alex", "sydney"}) {
		t.Fatalf("two OpenRights = %v, want room, alex, sydney", got)
	}
	// And from the middle, so it is an insert rather than an append.
	g = g.OpenRight("alex", "john")
	if got := panesOf(g); !slices.Equal(got, []string{"", "alex", "john", "sydney"}) {
		t.Fatalf("OpenRight from the middle = %v, want john between alex and sydney", got)
	}
}

func TestOpenBelowSplitsTheColumnTheConversationIsIn(t *testing.T) {
	g := NewGrid().OpenRight("", "alex").OpenBelow("alex", "sydney")
	if got := panesOf(g); !slices.Equal(got, []string{"", "alex/sydney"}) {
		t.Fatalf("OpenBelow alex = %v, want sydney under alex in one column", got)
	}
}

// The room's own column splits like any other: ⌃B from the room is a
// conversation under the room, which is what "below the panel I am in" means
// when the panel you are in is the room.
func TestOpenBelowSplitsTheRoomsColumnToo(t *testing.T) {
	g := NewGrid().OpenBelow("", "alex")
	if got := panesOf(g); !slices.Equal(got, []string{"/alex"}) {
		t.Fatalf("OpenBelow the room = %v, want alex under the room", got)
	}
}

// Bounded: one split per column, spec §8. A second OpenBelow takes the slot
// rather than growing a third row, and the conversation it displaces is still
// in App.dms - the grid says what is on screen, not what exists.
func TestOpenBelowOnAnAlreadySplitColumnTakesTheLowerSlot(t *testing.T) {
	g := NewGrid().OpenRight("", "alex").OpenBelow("alex", "sydney").OpenBelow("alex", "john")
	if got := panesOf(g); !slices.Equal(got, []string{"", "alex/john"}) {
		t.Fatalf("a second OpenBelow = %v, want john to have taken the lower slot", got)
	}
}

// Nothing sits below a lower pane, and the grid says so rather than guessing.
// The caller turns this into the notice that names ⌃Y instead.
func TestOpenBelowFromALowerPaneIsRefused(t *testing.T) {
	g := NewGrid().OpenRight("", "alex").OpenBelow("alex", "sydney")
	next, ok := g.CanOpenBelow("sydney")
	if ok {
		t.Fatalf("CanOpenBelow from a lower pane reported ok, giving %v - there is no row under the bottom one", panesOf(next))
	}
}

// Opening one that is already on screen moves nothing: the same conversation in
// two panes is two readers of one transcript and two composers for one agent.
func TestOpeningAConversationTheGridAlreadyHoldsChangesNothing(t *testing.T) {
	g := NewGrid().OpenRight("", "alex").OpenRight("alex", "sydney")
	if got := panesOf(g.OpenRight("", "sydney")); !slices.Equal(got, []string{"", "alex", "sydney"}) {
		t.Errorf("OpenRight of an open conversation = %v, want it left where it was", got)
	}
	if got := panesOf(g.OpenBelow("", "alex")); !slices.Equal(got, []string{"", "alex", "sydney"}) {
		t.Errorf("OpenBelow of an open conversation = %v, want it left where it was", got)
	}
}

func TestClosingALowerPaneUnsplitsItsColumn(t *testing.T) {
	g := NewGrid().OpenRight("", "alex").OpenBelow("alex", "sydney").Close("sydney")
	if got := panesOf(g); !slices.Equal(got, []string{"", "alex"}) {
		t.Fatalf("closing the lower pane = %v, want alex alone in its column", got)
	}
}

// Closing the upper half of a split column promotes the lower one rather than
// taking both: the pane that was not closed is still a conversation somebody
// wanted open.
func TestClosingAnUpperPanePromotesTheOneUnderIt(t *testing.T) {
	g := NewGrid().OpenRight("", "alex").OpenBelow("alex", "sydney").Close("alex")
	if got := panesOf(g); !slices.Equal(got, []string{"", "sydney"}) {
		t.Fatalf("closing the upper pane = %v, want sydney promoted into the column", got)
	}
}

func TestClosingTheOnlyPaneInAColumnTakesTheColumn(t *testing.T) {
	g := NewGrid().OpenRight("", "alex").OpenRight("alex", "sydney").Close("alex")
	if got := panesOf(g); !slices.Equal(got, []string{"", "sydney"}) {
		t.Fatalf("closing a whole column = %v, want the room and sydney", got)
	}
}

// The room is the one pane that is always there. Close is where that is
// structural rather than a convention the callers remember.
func TestTheRoomCannotBeClosed(t *testing.T) {
	g := NewGrid().OpenRight("", "alex")
	if got := panesOf(g.Close("")); !slices.Equal(got, []string{"", "alex"}) {
		t.Fatalf("Close(room) = %v, want the grid untouched", got)
	}
	if got := panesOf(NewGrid().Close("")); !slices.Equal(got, []string{""}) {
		t.Fatalf("Close(room) on a room-only grid = %v, want the room still there", got)
	}
}

// Where the keys go when the pane holding them closes. Always somewhere drawn:
// a focus on a pane that is not on screen is the split-brain composer
// App.withFocus exists to prevent.
func TestAfterCloseTheFocusLandsOnWhatTookTheSpace(t *testing.T) {
	for _, tc := range []struct {
		what  string
		grid  Grid
		close string
		want  string
	}{
		{
			"the lower pane, whose space goes back to the upper one",
			NewGrid().OpenRight("", "alex").OpenBelow("alex", "sydney"),
			"sydney", "alex",
		},
		{
			"the upper pane, whose space goes to the one promoted into it",
			NewGrid().OpenRight("", "alex").OpenBelow("alex", "sydney"),
			"alex", "sydney",
		},
		{
			"a whole column, whose space goes to the column on its left",
			NewGrid().OpenRight("", "alex").OpenRight("alex", "sydney"),
			"sydney", "alex",
		},
		{
			"the last conversation, which leaves only the room",
			NewGrid().OpenRight("", "alex"),
			"alex", "",
		},
	} {
		if got := tc.grid.Neighbour(tc.close); got != tc.want {
			t.Errorf("closing %s: Neighbour(%q) = %q, want %q", tc.what, tc.close, got, tc.want)
		}
	}
}

func TestPanesReadsLeftToRightAndTopToBottom(t *testing.T) {
	g := NewGrid().OpenRight("", "alex").OpenBelow("alex", "sydney").OpenRight("alex", "john")
	want := []string{"", "alex", "sydney", "john"}
	if got := g.Panes(); !slices.Equal(got, want) {
		t.Fatalf("Panes() = %v, want %v", got, want)
	}
}

// The ⇥ ring and the close path both ask "is this on screen", and a conversation
// in a lower slot is as on-screen as one in an upper.
func TestHasFindsAConversationInEitherSlot(t *testing.T) {
	g := NewGrid().OpenRight("", "alex").OpenBelow("alex", "sydney")
	for _, id := range []string{"", "alex", "sydney"} {
		if !g.Has(id) {
			t.Errorf("Has(%q) = false, want true", id)
		}
	}
	if g.Has("nobody") {
		t.Error(`Has("nobody") = true, want false`)
	}
}

// Immutability, the way this package means it: the grid a caller held before an
// open is the grid it holds after one. Both fields are slices or would be
// aliased through one, so this is the append-into-a-shared-array bug App.dms and
// Fleet both carry a comment about.
func TestOpeningAndClosingLeaveTheOriginalGridAlone(t *testing.T) {
	g := NewGrid().OpenRight("", "alex")
	before := panesOf(g)

	g.OpenRight("alex", "sydney")
	g.OpenBelow("alex", "sydney")
	g.Close("alex")

	if got := panesOf(g); !slices.Equal(got, before) {
		t.Fatalf("the original grid became %v after operations on copies of it, want %v", got, before)
	}
}

// Two columns built from the same parent must not share a backing array: the
// second append would write into the first's row.
func TestTwoGridsBranchedFromOneDoNotShareColumns(t *testing.T) {
	base := NewGrid().OpenRight("", "alex")
	left, right := base.OpenRight("alex", "sydney"), base.OpenRight("alex", "john")

	if got := panesOf(left); !slices.Equal(got, []string{"", "alex", "sydney"}) {
		t.Errorf("the first branch = %v, want it to still hold sydney", got)
	}
	if got := panesOf(right); !slices.Equal(got, []string{"", "alex", "john"}) {
		t.Errorf("the second branch = %v, want it to hold john", got)
	}
}

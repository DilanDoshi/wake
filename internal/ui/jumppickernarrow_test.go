package ui

// In a window that carries one column the jump picker's ↵ is a click on the row:
// the one placement function replaces the pane on screen, and the picker adds no
// rule of its own.

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// jumpTo types a few letters of a name into the picker and presses ↵.
func jumpTo(t testing.TB, a App, letters string) App {
	t.Helper()
	a, _ = pressKey(jumpTyped(jumpOpened(t, a), letters), kmsg(tea.KeyEnter))
	if a.jumpPicker.Open() {
		t.Fatal("the picker stayed up after ↵")
	}
	return a
}

// From the room the agent takes the column beside it, the room is drawn no more,
// and the result is the click's, region for region.
func TestEnterInASingleColumnLayoutTakesTheColumnBesideTheRoom(t *testing.T) {
	a := jumpTo(t, blueFleet(t, 110, 30), "web-b")
	if want := []string{"", "s2"}; !slices.Equal(tops(a), want) || a.focus != "s2" {
		t.Fatalf("columns %q focus %q, want web-blur in the column beside the room", tops(a), a.focus)
	}
	if got := a.regions().Cols; got[0] != 0 || got[1] == 0 {
		t.Errorf("regions %v: want the agent drawn alone and the room off screen", got)
	}
	click := clickAgent(t, blueFleet(t, 110, 30), "s2")
	if !slices.Equal(a.regions().Cols, click.regions().Cols) || !slices.Equal(tops(a), tops(click)) {
		t.Errorf("↵ gave %v %q, a click gives %v %q", a.regions().Cols, tops(a), click.regions().Cols, tops(click))
	}
}

// From a conversation it replaces that one, which keeps its transcript, and ten
// jumps leave one conversation column beside the room, not ten.
func TestEnterInASingleColumnLayoutReplacesTheConversationOnScreen(t *testing.T) {
	a := jumpTo(t, blueFleet(t, 110, 30), "pay-l")
	a = jumpTo(t, a, "scratch")
	if want := []string{"", "s7"}; !slices.Equal(tops(a), want) || a.focus != "s7" {
		t.Fatalf("columns %q focus %q after a second jump, want scratch in the first one's place", tops(a), a.focus)
	}
	if _, kept := a.dms["s1"]; !kept {
		t.Error("the replaced conversation lost its transcript")
	}
	for range 10 {
		a = jumpTo(t, a, "notes")
		a = jumpTo(t, a, "scratch")
	}
	if want := []string{"", "s7"}; !slices.Equal(tops(a), want) || a.grid.Cols[1].Bottom != "" {
		t.Errorf("columns %+v after twenty jumps, want the room and scratch alone", a.grid.Cols)
	}
}

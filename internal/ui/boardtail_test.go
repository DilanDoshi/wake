package ui

import (
	"fmt"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

func partialEv(text string) core.Event {
	return core.Event{Kind: core.KindPartialText, Text: text}
}

func TestTheTailAccumulatesOnlyWhileTheTiledBoardIsUp(t *testing.T) {
	base := App{}
	cases := []struct {
		name string
		up   bool
		tile bool
		want string
	}{
		{"board down", false, false, ""},
		{"rows up, not tiled", true, false, ""},
		{"tiled up", true, true, "hello"},
	}
	for _, tc := range cases {
		a := base
		a.board = Board{Up: tc.up, Tiled: tc.tile}
		a = a.foldTail("s1", partialEv("hello"))
		if got := a.tails["s1"].text; got != tc.want {
			t.Errorf("%s: tail = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTheTailClearsWhenTheBlockLands(t *testing.T) {
	a := App{board: Board{Up: true, Tiled: true}}
	a = a.foldTail("s1", partialEv("half a sen"))
	a = a.foldTail("s1", core.Event{Kind: core.KindAssistantText, Text: "half a sentence"})
	if got := a.tails["s1"].text; got != "" {
		t.Errorf("tail after the block landed = %q, want empty", got)
	}
}

func TestTheTailClearsWhenTheTurnEnds(t *testing.T) {
	a := App{board: Board{Up: true, Tiled: true}}
	a = a.foldTail("s1", partialEv("interrupted mid"))
	a = a.foldTail("s1", core.Event{Kind: core.KindTurnEnd})
	if got := a.tails["s1"].text; got != "" {
		t.Errorf("tail after turn end = %q, want empty", got)
	}
}

func TestFoldingATailDoesNotTouchTheFleet(t *testing.T) {
	a := App{board: Board{Up: true, Tiled: true}, fleet: NewFleet()}
	before := a.fleet
	a = a.foldTail("s1", partialEv("tokens"))
	// The fleet value is unchanged: foldTail writes only App.tails, so a
	// streamed token costs no fleet-sized copy. Same maps, same pointers.
	if !sameFleet(before, a.fleet) {
		t.Fatal("foldTail copied or mutated the fleet; tails must live off it")
	}
}

func TestClosingTheBoardDropsTheTails(t *testing.T) {
	a := App{board: Board{Up: true, Tiled: true}}
	a = a.foldTail("s1", partialEv("some output"))
	a = a.closeBoard()
	if len(a.tails) != 0 {
		t.Errorf("tails after close = %d entries, want 0", len(a.tails))
	}
}

func sameFleet(x, y Fleet) bool {
	return fmt.Sprintf("%p", x.agents) == fmt.Sprintf("%p", y.agents)
}

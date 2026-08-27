package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// fullClick is a press and the release that follows it on one cell - the gesture
// endSelection reads as a click when no drag moved the head.
func fullClick(a App, x, y int) App {
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
	a, _ = a.endSelection()
	return a
}

// rowOf is the screen row a substring is drawn on.
func rowOf(t *testing.T, a App, sub string) int {
	t.Helper()
	for y, row := range strings.Split(stripANSI(a.View()), "\n") {
		if strings.Contains(row, sub) {
			return y
		}
	}
	t.Fatalf("%q is not on screen:\n%s", sub, stripANSI(a.View()))
	return -1
}

func s1WithFoldedRun(t *testing.T, a App) App {
	t.Helper()
	frame := func(ev core.Event) rpc.Frame {
		ev.SessionID = "s1"
		return rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev}
	}
	a = a.applyFrame(frame(prose("working through the ticket")))
	a = a.applyFrame(frame(bashCall("t1")))
	a = a.applyFrame(frame(result("t1", "out", false)))
	if a.dms["s1"].tr.runHead("t1") < 0 {
		t.Fatalf("the run did not fold to a rollup:\n%s", shown(a))
	}
	return a
}

// The bug: a click that moves the keys to another pane also opens whatever run
// sits under the pointer, so looking at a conversation expands it. A click that
// changes focus should only focus.
func TestAFocusChangingClickFocusesWithoutOpeningTheRun(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a = pick(a, "s1").openRight("s1", "sydney")
	a = a.refocus("").applyGeometry() // the room holds the keys, s1 is beside it
	a = s1WithFoldedRun(t, a)
	if a.focus != "" {
		t.Fatalf("focus is %q before the click, want the room", a.focus)
	}

	a = fullClick(a, midOf(a.regions(), 1), rowOf(t, a, "tool use"))

	if a.focus != "s1" {
		t.Fatalf("the click did not focus s1 (focus=%q)", a.focus)
	}
	if a.dms["s1"].runOpen["t1"] {
		t.Errorf("a click that moved focus to s1 also opened the run; it should only focus:\n%s", shown(a))
	}
	// The rollup is still folded rather than blown open to its per-call blocks.
	// ("1 tool use" is unique to the transcript's rollup line; the activity
	// sidebar's own ⏺ indicator is not it.)
	if !strings.Contains(shown(a), "1 tool use") {
		t.Errorf("the run is no longer folded after a focus-changing click:\n%s", shown(a))
	}
}

// And the deliberate gesture still works: once the pane holds the keys, a click
// on its rollup opens the run, the way Claude Code spends that same click.
func TestAClickInTheFocusedPaneStillOpensTheRun(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a = pick(a, "s1").openRight("s1", "sydney").applyGeometry() // s1 holds the keys
	a = s1WithFoldedRun(t, a)
	if a.focus != "s1" {
		t.Fatalf("focus is %q, want s1 to hold the keys", a.focus)
	}

	a = fullClick(a, midOf(a.regions(), 1), rowOf(t, a, "tool use"))

	if !a.dms["s1"].runOpen["t1"] {
		t.Errorf("a click in the already-focused pane did not open the run:\n%s", shown(a))
	}
}

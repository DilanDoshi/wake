package ui

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// What the mouse costs when it is not doing anything, and what a click has to
// land on before a drag starts.
//
// Mode 1002 is the only one bubbletea v1.3.10 offers that reports a click at
// all, and it reports motion whenever a button is held. Wake cannot turn that
// half off, so the guards here are about what those messages must *not* cost.

// Mutation check: dropping the `&& a.dragAt != noDrag` from the motion case fails this
// at "a bare motion changed the geometry".
func TestMotionWithNoButtonHeldDoesNothingAtAll(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	before := a.pending

	a, cmd := a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, X: 50})
	if !samePending(a.pending, before) || cmd != nil {
		t.Error("a bare motion changed the geometry. 1002 reports motion while a button is held and Wake cannot turn that off, so every motion that is not a divider drag must cost nothing")
	}
	if a.dragAt != noDrag {
		t.Error("a bare motion took hold of the divider")
	}
}

// The case mode 1002 actually produces, and the expensive one: the button is
// down, the pointer is moving, and the press that started it landed in a pane
// rather than on the divider. There is one of these per cell crossed, so
// dragging a selection across a conversation is hundreds of them.
//
// Mutation check: dropping `&& a.dragAt != noDrag` from the motion case fails this at
// "moving the pointer to column 11 with a button held scheduled a settle". The
// test above cannot see that mutation - a motion with no button is absorbed by
// the lost-release guard - which is why both exist.
func TestMotionAfterAPressThatMissedTheDividerCostsNothing(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	a = a.press(10, 0) // in the room, nowhere near the divider
	before := a.pending

	renders := countRenders(t, func() {
		for x := 11; x < 60; x++ {
			m, cmd := a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x})
			a = m
			if cmd != nil {
				t.Fatalf("moving the pointer to column %d with a button held scheduled a settle: nothing was being dragged", x)
			}
		}
	})
	if !samePending(a.pending, before) {
		t.Errorf("a selection drag across the room moved the geometry from %+v to %+v", before, a.pending)
	}
	if renders != 0 {
		t.Errorf("it cost %d re-wraps", renders)
	}
	if a.dragAt != noDrag {
		t.Error("it also took hold of the divider")
	}
}

// The divider is one column wide and the panes either side of it are eighty.
// A press that armed a drag anywhere in a pane would make every click in a
// conversation a divider drag.
func TestOnlyThePressOnTheDividerTakesHoldOfIt(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	divider := dividerColumnOf(a)

	for _, x := range []int{0, 1, divider - 1, divider + 1, 199, 400} {
		if got := a.press(x, 0); got.dragAt != noDrag {
			t.Errorf("pressing column %d (%v) took hold of the divider at %d", x, hitRegion(a, x), divider)
		}
	}
	if a.press(divider, 0).dragAt == noDrag {
		t.Errorf("pressing the divider's own column %d did not take hold of it", divider)
	}
}

// Letting go ends the drag. Without it the divider follows the pointer around
// the terminal forever, which is a window somebody has to restart to escape.
//
// The button is checked as reported rather than assumed: X10 encoding reports a
// release as MouseButtonNone, and only SGR names the button that was let go of,
// so a release case that required the left button would never fire on half the
// terminals in use.
func TestLettingGoEndsTheDrag(t *testing.T) {
	for _, button := range []tea.MouseButton{tea.MouseButtonNone, tea.MouseButtonLeft} {
		a := splitApp(t, 200, 40, 4)
		a = grab(t, a, dividerColumnOf(a))

		a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: button, X: 120})
		if a.dragAt != noDrag {
			t.Fatalf("a release reported as %v left the drag in flight", button)
		}

		before := a.pending
		a, cmd := a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 60})
		if !samePending(a.pending, before) || cmd != nil {
			t.Errorf("moving the pointer after a %v release still moved the divider", button)
		}
	}
}

// A motion carrying no button cannot happen under 1002 - that is the mode's
// whole definition - so one arriving means the release was lost, under 1003 or
// from a terminal that swallowed it. Ending the drag on it is what stops a lost
// release from leaving the divider stuck to the pointer.
func TestAMotionWithNothingHeldEndsADragThatLostItsRelease(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	a = grab(t, a, dividerColumnOf(a))
	before := a.pending

	a, cmd := a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonNone, X: 60})
	if a.dragAt != noDrag {
		t.Error("a motion with no button held left the drag in flight")
	}
	if !samePending(a.pending, before) || cmd != nil {
		t.Errorf("it also moved the divider, from %+v to %+v", before, a.pending)
	}
}

// The wheel is what the mouse was already for, and it keeps working with a
// button down: the two are different messages and the wheel case is answered
// first, so a notch during a drag scrolls rather than being read as a drag to
// wherever the pointer happens to be.
func TestTheWheelStillScrollsDuringADrag(t *testing.T) {
	a := splitApp(t, 200, 40, 40)
	a = grab(t, a, dividerColumnOf(a))
	before := a.pending

	scrolled, cmd := a.mouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, X: 20})
	if cmd != nil {
		t.Errorf("a wheel notch produced a command: %v", cmd())
	}
	if !samePending(scrolled.pending, before) {
		t.Errorf("a wheel notch moved the divider, from %+v to %+v", before, scrolled.pending)
	}
	// The room, because that is the pane splitApp focuses and the one holding
	// the conversation. Scrolling an empty pane would compare equal whatever the
	// wheel did.
	w := a.regions().Room()
	if scrolled.room.View(w, scrolled.paneHeight()) == a.room.View(w, a.paneHeight()) {
		t.Error("the wheel did not move the reader while a button was held")
	}
}

// Nothing the mouse can produce reaches the composer. Forwarding motion into a
// text area is work per mouse pixel for no behaviour, and at 1002's rate that
// is the difference between a drag being free and a drag being the most
// expensive thing on screen.
func TestNoMouseMessageReachesTheComposer(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	a = a.withComposer(a.composer().Reset())

	msgs := []tea.MouseMsg{
		{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 30, Y: 3},
		{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 31, Y: 3},
		{Action: tea.MouseActionRelease, X: 31, Y: 3},
		{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: 31, Y: 3},
		{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: 31, Y: 3},
	}
	var m tea.Model = a
	for _, msg := range msgs {
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		if cmd != nil {
			t.Errorf("%v produced a command: %v", msg, cmd())
		}
	}
	if got := m.(App).composer().Value(); got != "" {
		t.Errorf("the composer holds %q after a mouse gesture", got)
	}
}

// samePending compares two geometries. The weights are a slice, so a geometry is
// no longer comparable with == and a test that used one would not compile - but
// a test that compared only width and height would silently stop noticing a
// divider that moved.
func samePending(a, b geometry) bool {
	return a.width == b.width && a.height == b.height && slices.Equal(a.weights, b.weights)
}

// hitRegion names the region under a column, for a failure message.
func hitRegion(a App, x int) Region {
	r, _ := a.layout.Hit(a.regions(), x)
	return r
}

package ui

import (
	"fmt"
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
)

// What a window drag costs, and what is on screen while it is happening.
//
// Re-wrapping the transcript is the most expensive thing this model does - 248ms
// at 3,000 events, measured in resize_bench_test.go - and a drag delivers a new
// width as fast as this loop will take one. These are the guards that keep a
// drag from costing one of those per column.

// wrapped is a sized App carrying enough conversation for a re-wrap to be the
// expensive thing it is.
// The pane it fills is the room, and both sidebars are closed. Every width in
// this file is a pane width rather than a terminal width, and a sidebar is 16
// or 20 columns of chrome that would move all of them - so they are turned off
// here for the same reason the transcript is filled: to leave exactly the thing
// being measured.
func wrapped(t testing.TB, w, h, events int) tea.Model {
	t.Helper()
	fresh(t)
	a := NewRoomApp(nil, Stream{}, nil)
	a.layout.ShowGroups, a.layout.ShowRoster = false, false
	var m tea.Model = a
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	for i := range events {
		m, _ = m.Update(eventMsg{Event: core.Event{
			Kind: core.KindAssistantText, SessionID: "s1",
			Text: fmt.Sprintf("turn %d. %s", i, wrappingSentence),
		}})
	}
	return m
}

// A resize is a drag, not a step. Re-wrapping the transcript is the most
// expensive thing this model does - 248ms at 3,000 events, measured - and every
// intermediate width of a drag throws the previous one away. So the drag costs
// one re-wrap, not one per column.
//
// Mutation check: applying the width in the WindowSizeMsg branch as it was
// before fails this at "a 40-column drag cost 40 re-wraps of the transcript,
// want 0 until it settles".
func TestADragCostsOneReWrapRatherThanOnePerColumn(t *testing.T) {
	m := wrapped(t, 120, 30, 20)

	n := countRoomRenders(t, func() {
		for w := 119; w >= 80; w-- {
			m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		}
	})
	if n != 0 {
		t.Errorf("a 40-column drag cost %d re-wraps of the room, want 0 until it settles", n)
	}

	settled := countRoomRenders(t, func() {
		m, _ = m.Update(geometrySettledMsg{gen: m.(App).geoGen})
	})
	if settled != 1 {
		t.Errorf("the drag settling cost %d re-wraps, want exactly 1", settled)
	}
	if got := m.(App).room.width; got != 80 {
		t.Errorf("the transcript settled at width %d, want the 80 the drag ended on", got)
	}
}

// The frame is drawn at every intermediate width even though it is not
// re-wrapped for one, and it must never be wider than the window it is drawn
// in: a frame one column too wide wraps every row and scrolls the alt screen on
// every draw, which is the same failure the notice row's one-row rule exists
// for.
//
// Mutation check: dropping the clip in View fails this at "mid-drag the frame
// is 120 columns wide in an 80-column terminal".
func TestTheFrameIsNeverWiderThanTheTerminalMidDrag(t *testing.T) {
	m := wrapped(t, 120, 30, 20)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})

	if got := widest(m.View()); got > 80 {
		t.Errorf("mid-drag the frame is %d columns wide in an 80-column terminal:\n%s", got, shown(m))
	}
}

// A height change re-lays nothing - only width re-wraps - so it does not wait
// for a quiet period, and a shorter window takes effect on the next frame
// rather than 80ms later.
func TestAHeightChangeIsAppliedImmediately(t *testing.T) {
	m := wrapped(t, 100, 30, 20)

	n := countRoomRenders(t, func() {
		m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 18})
	})
	if n != 0 {
		t.Errorf("a height change re-rendered the transcript %d times", n)
	}
	if got := lipgloss.Height(m.View()); got != 18 {
		t.Errorf("the frame is %d rows after a height change, want 18", got)
	}
}

// The same, with a drag in flight: the width is still waiting for quiet and the
// height must not wait with it.
func TestAHeightChangeDuringADragStillLands(t *testing.T) {
	m := wrapped(t, 120, 30, 20)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})

	if got := lipgloss.Height(m.View()); got != 20 {
		t.Errorf("the frame is %d rows, want 20: a pending width change swallowed the height", got)
	}
}

// A timer belonging to a width the terminal has already left must not re-wrap
// for it. Without the generation check, the first tick of a drag lands mid-drag
// and re-wraps for a width nobody is looking at - which is the cost the
// debounce exists to avoid, paid anyway.
func TestAStaleResizeTimerIsIgnored(t *testing.T) {
	m := wrapped(t, 120, 30, 20)

	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	stale := m.(App).geoGen
	m, _ = m.Update(tea.WindowSizeMsg{Width: 70, Height: 30})

	n := countRoomRenders(t, func() { m, _ = m.Update(geometrySettledMsg{gen: stale}) })
	if n != 0 {
		t.Errorf("a superseded resize timer re-wrapped the room %d times", n)
	}
	if got := m.(App).room.width; got != 120 {
		t.Errorf("a superseded resize timer moved the transcript to width %d", got)
	}

	m, _ = m.Update(geometrySettledMsg{gen: m.(App).geoGen})
	if got := m.(App).room.width; got != 70 {
		t.Errorf("the current resize settled at width %d, want 70", got)
	}
}

// The first size a model is ever given has no drag behind it and no previous
// width to draw at, so it is applied whole.
func TestTheFirstSizeIsNotDebounced(t *testing.T) {
	fresh(t)
	app := dmApp(nil, Stream{}, "s1", "alex")
	m, cmd := app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if cmd != nil {
		t.Error("the first size scheduled a timer instead of being applied")
	}
	if got, want := m.(App).dm().width, 100-rosterWidth; got != want {
		t.Errorf("the DM is %d columns after the first size, want %d", got, want)
	}
}

// --- the divider, through the same settle ------------------------------

// splitApp is a sized App with the second pane open, which is the only geometry
// a divider divides anything in.
//
// The focus goes back to the room, because the room is where wrapped put the
// conversation: a drag has to change the width of a pane with something in it,
// or the guard that a drag costs one re-wrap passes against an implementation
// that re-wraps on every column.
func splitApp(t testing.TB, w, h, events int) App {
	t.Helper()
	a := wrapped(t, w, h, events).(App)
	a = a.WithOpenDM("s1", "alex")
	a.layout.ShowRoster = false // a drag is about the divider, not the sidebar
	a.focus = ""
	return a.applyGeometry()
}

// dividerColumn is the terminal column the divider is drawn in.
func dividerColumnOf(a App) int {
	r := a.regions()
	return r.Groups + r.Room()
}

// grab presses the left button on a column, which is what arms a drag.
func grab(t testing.TB, a App, x int) App {
	t.Helper()
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x})
	if a.dragAt == noDrag {
		t.Fatalf("pressing at column %d did not take hold of the divider (regions %+v): every drag below would then be measuring nothing", x, a.regions())
	}
	return a
}

// dragTo is one cell of motion with the button still down - the message mode
// 1002 delivers per column crossed.
func dragTo(a App, x int) App {
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x})
	return a
}

// settle delivers the tick the newest geometry change is waiting on.
func settle(a App) App { return a.settled(a.geoGen) }

// --- the settle, with the loop actually running the commands -----------

// heldSettles is a fake clock. It takes every settle a geometry change
// schedules and hands none of them back until the test says the drag is over,
// which is what "after 80ms of quiet" means with no wall clock in it.
//
// The delay each one asked for is recorded too: a settle scheduled for zero is
// a settle that does not wait, and it would otherwise look identical here to
// one that does.
type heldSettles struct {
	delays  []time.Duration
	pending []tea.Msg
}

// holdSettles installs the fake for one test.
func holdSettles(t testing.TB) *heldSettles {
	t.Helper()
	h := &heldSettles{}
	original := settleTimer
	settleTimer = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		h.delays = append(h.delays, d)
		h.pending = append(h.pending, fn(time.Time{}))
		return nil // the timer has not fired; nothing is delivered yet
	}
	t.Cleanup(func() { settleTimer = original })
	return h
}

// fire delivers every message the held timers were holding, oldest first, the
// order real timers scheduled at the same interval would land in.
func (h *heldSettles) fire() []tea.Msg {
	msgs := h.pending
	h.pending = nil
	return msgs
}

// drive is Bubble Tea's loop: apply a message, run whatever command comes back,
// apply that, and keep going until the model asks for nothing more.
//
// This is the half every earlier test in this file was missing. Delivering the
// settle by hand asserts what the model does with a message that has *already*
// arrived, and says nothing about whether the model asked for it to arrive
// eighty milliseconds later or immediately - which is the entire difference
// between a 93ms drag and a 13.4-second one.
func drive(t testing.TB, m tea.Model, msg tea.Msg) tea.Model {
	t.Helper()
	for depth := 0; ; depth++ {
		if depth > commandChainLimit {
			t.Fatalf("a message produced a chain of more than %d commands: this loop is not terminating", commandChainLimit)
		}
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		if cmd == nil {
			return m
		}
		if msg = cmd(); msg == nil {
			return m
		}
	}
}

// commandChainLimit bounds drive. A test that hangs reports nothing at all, and
// one mutation in this project's history produced a 138-second package timeout
// instead of a failure.
const commandChainLimit = 100

// What a drag costs when the loop runs the commands the model hands it.
//
// This is the guard the report's 93ms rests on, and the one `make test` runs.
// The benchmark measures the same property and cannot enforce it: `make test`
// runs no benchmarks, so a number in a report is not a guard.
//
// Mutation check: replacing settleTimer with an immediate `func() tea.Msg`
// fails this at "a 40-column divider drag re-wrapped 40 times - once per
// column". Scheduling it for zero fails at the delay assertion instead.
func TestADragCostsOneReWrapWhenTheLoopRunsEveryCommand(t *testing.T) {
	h := holdSettles(t)
	a := splitApp(t, 200, 40, 20)
	a = grab(t, a, dividerColumnOf(a))
	var m tea.Model = a

	const columns = 40
	scheduled := 0
	renders := countRoomRenders(t, func() {
		for x := 100; x < 100+columns; x++ {
			m = drive(t, m, tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x})
		}
		scheduled = len(h.pending)
		// The hand comes off the mouse and the timers land.
		for _, msg := range h.fire() {
			m = drive(t, m, msg)
		}
	})

	// The harm first, and the mechanism after it. A drag that scheduled
	// nothing is the usual way to arrive here - the settle was not deferred at
	// all, so every motion applied itself on the spot.
	switch {
	case renders > 1:
		t.Errorf("a %d-column divider drag re-wrapped the room %d times - once per column rather than once per drag. That is the measured 4.5x draw-path win handed straight back: every column crossed puts the whole conversation through glamour behind internal/render's process-global mutex, 13.4s against 93ms for this same drag. %d of those columns deferred a settle; the rest applied on the spot",
			columns, renders, scheduled)
	case scheduled == 0:
		t.Errorf("a %d-column drag deferred no settle and re-wrapped %d times: nothing about the settle is being measured here, so check that splitApp still opens the second pane", columns, renders)
	case renders != 1:
		t.Errorf("a %d-column divider drag re-wrapped %d times, want exactly 1: the settle landed and re-wrapped nothing", columns, renders)
	}

	for i, d := range h.delays {
		if d != resizeQuiet {
			t.Errorf("settle %d was scheduled for %v, want %v: a settle that does not wait is not a settle, and every intermediate width gets its own re-wrap", i, d, resizeQuiet)
		}
	}
}

// The same, for the window drag, which is the one that already had this
// property and had no test that could see it lost either.
func TestAWindowDragCostsOneReWrapWhenTheLoopRunsEveryCommand(t *testing.T) {
	h := holdSettles(t)
	m := wrapped(t, 120, 30, 20)

	renders := countRoomRenders(t, func() {
		for w := 119; w >= 80; w-- {
			m = drive(t, m, tea.WindowSizeMsg{Width: w, Height: 30})
		}
		for _, msg := range h.fire() {
			m = drive(t, m, msg)
		}
	})

	if renders != 1 {
		t.Errorf("a 40-column window drag re-wrapped %d times, want 1", renders)
	}
	if got := m.(App).room.width; got != 80 {
		t.Errorf("the transcript settled at width %d, want the 80 the drag ended on", got)
	}
}

// And the seam is a real wait rather than a name for one.
//
// The only assertion here is a *lower* bound, which is what makes it safe on a
// loaded machine: load can make a timer late and cannot make it early, so the
// failure this can report - the message arrived before the quiet was up - is
// not something a busy CI box can manufacture. It is the one thing the fake
// clock above cannot check, because it replaces the very thing under test.
func TestTheSettleTimerActuallyWaits(t *testing.T) {
	got := make(chan time.Duration, 1)
	start := time.Now()
	cmd := settleTimer(resizeQuiet, func(time.Time) tea.Msg {
		got <- time.Since(start)
		return geometrySettledMsg{}
	})
	go func() { _ = cmd() }()

	select {
	case waited := <-got:
		if waited < resizeQuiet {
			t.Errorf("the settle fired after %v, before the %v of quiet it is meant to wait out: a drag then re-wraps once per column", waited, resizeQuiet)
		}
	case <-time.After(resizeQuiet + cmdTimeout):
		t.Fatalf("the settle never fired within %v", resizeQuiet+cmdTimeout)
	}
}

// The whole point of putting the divider and the window through one settle.
//
// Mutation check: applying the geometry in changeGeometry rather than ticking
// fails this at "a 40-column divider drag cost 40 re-wraps, want 1".
func TestADividerDragCostsOneReWrapPerPaneAndNotOnePerColumn(t *testing.T) {
	a := splitApp(t, 200, 40, 20)
	before := a.room.width
	a = grab(t, a, dividerColumnOf(a))

	// Both panes, because a divider drag changes both widths and the name of
	// this test claims per-pane coverage. Counting only the room would leave a
	// DM that re-wrapped once per column invisible.
	room, dm := countPaneRenders(t, func() {
		// Forty columns of drag, delivered as fast as the loop will take them -
		// which is what a hand on a mouse produces.
		for x := 100; x < 140; x++ {
			a = dragTo(a, x)
		}
		a = settle(a)
	})
	for _, got := range []struct {
		pane string
		n    int
	}{{"room", room}, {"DM", dm}} {
		if got.n > 1 {
			t.Errorf("a 40-column divider drag cost the %s %d re-wraps, want 1. This is the measured 4.5x draw-path win being spent: the window drag was taken from 40 re-wraps to 1 by waiting out 80ms of quiet, and a divider that does not go through the same settle gives it straight back", got.pane, got.n)
		}
	}
	if room != 1 {
		t.Errorf("the drag settled and re-wrapped the room %d times, want exactly 1", room)
	}
	if a.room.width == before {
		t.Fatalf("the room is still %d columns after a 40-column drag, so a re-wrap per column was never on the table and the count above proves nothing", before)
	}
}

// countPaneRenders reports how many times f sent each pane's whole transcript
// back through glamour, through the two seams rather than by timing anything.
func countPaneRenders(t testing.TB, f func()) (room, dm int) {
	t.Helper()
	wasRoom, wasDM := renderRoom, renderTranscript
	renderRoom = func(r Room, lines []roomLine) []block { room++; return wasRoom(r, lines) }
	renderTranscript = func(d DM) []block { dm++; return wasDM(d) }
	t.Cleanup(func() { renderRoom, renderTranscript = wasRoom, wasDM })
	f()
	return room, dm
}

// One pending geometry, not two. The later change wins and the earlier one does
// not re-apply late - and the half of the earlier one that is still true, where
// the operator put the divider, survives the resize rather than being reset by
// it.
//
// Mutation check: building the resize's geometry from the applied split instead
// of the pending one fails this at "the divider went back to 0.0000 after the
// window moved, want the 0.6030 the drag left it at".
func TestAWindowDragAndADividerDragShareOneSettleRatherThanRacing(t *testing.T) {
	a := splitApp(t, 200, 40, 20)
	a = grab(t, a, dividerColumnOf(a))

	var dragged []float64
	renders := countRoomRenders(t, func() {
		a = dragTo(a, 120)
		dragged = a.pending.weights
		a, _ = a.resized(180, 40) // the window moves mid-drag
		a = settle(a)
	})

	if got := a.layout.Width; got != 180 {
		t.Errorf("width settled at %d, want 180: two debounces would let an older timer land after a newer change and re-wrap for a size the terminal has left", got)
	}
	if got := a.layout.Weights; !slices.Equal(got, dragged) {
		t.Errorf("the divider went back to %v after the window moved, want the %v the drag left it at: a resize that rebuilds the geometry from the applied one throws away the drag it interrupted", got, dragged)
	}
	if renders != 1 {
		t.Errorf("a drag and a resize sharing one settle cost %d re-wraps, want exactly 1", renders)
	}
}

// A resize keeps the *proportion* rather than a pane's column count, which is
// the whole reason Split is a fraction. Pinning one pane's width would give
// every new column to the other, so widening the terminal by 60 would widen one
// pane by 60 and the other by nothing.
func TestAResizeKeepsTheProportionTheDividerWasDraggedTo(t *testing.T) {
	a := splitApp(t, 200, 40, 20)
	a = grab(t, a, dividerColumnOf(a))
	a = settle(dragTo(a, 60)) // the room made deliberately narrow

	narrow := a.regions()
	a, _ = a.resized(260, 40)
	a = settle(a)
	wide := a.regions()

	if wide.Room() <= narrow.Room() || wide.Cols[1] <= narrow.Cols[1] {
		t.Errorf("60 more columns went from %+v to %+v: both panes share what a wider terminal brings, or the divider is a column rather than a proportion", narrow, wide)
	}
	got := float64(wide.Room()) / float64(wide.Room()+wide.Cols[1])
	want := float64(narrow.Room()) / float64(narrow.Room()+narrow.Cols[1])
	if diff := got - want; diff < -0.02 || diff > 0.02 {
		t.Errorf("the room held %.3f of the space and holds %.3f after the resize", want, got)
	}
}

// The terminal can cross the takeover width while a button is held, and the
// motion events keep arriving after the second pane has gone. There is no
// divider to move then, and moving one anyway would re-wrap the one pane that
// is left for a split nothing is drawing.
func TestADragThatOutlivesItsSecondPaneChangesNothing(t *testing.T) {
	a := splitApp(t, 200, 40, 20)
	a = grab(t, a, dividerColumnOf(a))
	a, _ = a.resized(100, 40) // below dmTakeoverColumns: the DM takes the pane
	a = settle(a)

	before := a.pending
	renders := countRoomRenders(t, func() { a = dragTo(a, 40) })

	if !samePending(a.pending, before) {
		t.Errorf("a drag in a single-pane terminal moved the geometry from %+v to %+v", before, a.pending)
	}
	if renders != 0 {
		t.Errorf("it also cost %d re-wraps", renders)
	}
}

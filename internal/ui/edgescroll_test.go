package ui

import (
	"fmt"
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// A drag held at a pane's edge keeps scrolling - see edgescroll.go.

// motion is the pointer crossing a cell with the left button still down.
func motion(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x, Y: y}
}

// countEdgeTicks replaces the edge timer with a counter, and puts it back. The
// command it returns is never run here: each test delivers the tick itself.
func countEdgeTicks(t *testing.T) *int {
	t.Helper()
	n := 0
	prev := edgeScrollTimer
	edgeScrollTimer = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd {
		n++
		return func() tea.Msg { return edgeScrollMsg{} }
	}
	t.Cleanup(func() { edgeScrollTimer = prev })
	return &n
}

// edgeTick lands one tick through Update, the way the program delivers it.
func edgeTick(t *testing.T, a App) (App, tea.Cmd) {
	t.Helper()
	m, cmd := a.Update(edgeScrollMsg{})
	next, ok := m.(App)
	if !ok {
		t.Fatalf("Update returned %T", m)
	}
	return next, cmd
}

func roomScroll(a App) int { return a.transcriptIn("").scroll }

// The reported bug. The room starts on the window's first row, so no pointer
// can ever be above it: a drag to the top of the window selected up to the
// first line on screen and never scrolled back past it.
//
// Mutation check: making the top edge exclusive again (y < selTop) fails this
// at "stayed on line".
func TestADragToTheTopRowOfTheWindowScrollsBack(t *testing.T) {
	countEdgeTicks(t)
	a := splitApp(t, 200, 40, 40)
	was := roomScroll(a)
	a, _ = a.mouse(pressAt(10, textRow))
	a, _ = a.mouse(motion(10, 0))
	now := roomScroll(a)
	if now >= was {
		t.Fatalf("the reader stayed on line %d: a drag onto the window's top row scrolls back", was)
	}
	if a.sel.head.line != now {
		t.Errorf("the highlight ends on line %d, want the top line on screen, %d", a.sel.head.line, now)
	}
}

// A drag that never leaves the row it was pressed on is selecting within that
// row, so the top row does not pull it - or a word on the first line on screen
// could not be selected without the conversation sliding out from under it.
//
// Mutation check: dropping the head/anchor test from edgePull fails this.
func TestADragAlongTheTopRowItWasPressedOnDoesNotScroll(t *testing.T) {
	ticks := countEdgeTicks(t)
	a := splitApp(t, 200, 40, 40).scrollPane("", 5)
	was := roomScroll(a)
	a, _ = a.mouse(pressAt(10, 0))
	for x := 11; x <= 30; x++ {
		a, _ = a.mouse(motion(x, 0))
	}
	if now := roomScroll(a); now != was {
		t.Errorf("a drag along the top row moved the reader from line %d to %d", was, now)
	}
	if *ticks != 0 {
		t.Errorf("a drag along the top row armed %d edge ticks, want none", *ticks)
	}
	if a.sel.empty() {
		t.Error("the drag along the top row selected nothing")
	}
}

// Coming back to the top row after leaving it is reaching for the edge, not
// selecting within the row, so it pulls even where the drag began.
//
// Mutation check: dropping the head test from edgePull fails this.
func TestADragReturningToTheTopRowItBeganOnScrollsBack(t *testing.T) {
	countEdgeTicks(t)
	a := splitApp(t, 200, 40, 40).scrollPane("", 5)
	was := roomScroll(a)
	a, _ = a.mouse(pressAt(10, 0))
	a, _ = a.mouse(motion(10, 3))
	a, cmd := a.mouse(motion(10, 0))
	if now := roomScroll(a); now >= was || cmd == nil {
		t.Errorf("returning to the top row left the reader on line %d (armed %v): it has left its row, so the edge pulls", now, cmd != nil)
	}
}

// A pointer resting at the edge sends no motion, so without a tick the scroll
// stopped the moment the hand did.
//
// Mutation check: returning no command from holdAtEdge fails this at "armed
// nothing".
func TestAPointerHeldAtTheTopEdgeKeepsScrolling(t *testing.T) {
	ticks := countEdgeTicks(t)
	a := splitApp(t, 200, 40, 40)
	a, _ = a.mouse(pressAt(10, textRow))
	a, cmd := a.mouse(motion(10, 0))
	if cmd == nil || *ticks != 1 {
		t.Fatalf("reaching the edge armed nothing (%d ticks): a pointer held there would stop scrolling", *ticks)
	}
	for i := range 3 {
		before := roomScroll(a)
		a, cmd = edgeTick(t, a)
		if now := roomScroll(a); now >= before {
			t.Fatalf("tick %d left the reader on line %d", i, before)
		}
		if cmd == nil {
			t.Fatalf("tick %d scrolled and armed no next one", i)
		}
		if a.sel.head.line != roomScroll(a) {
			t.Errorf("after tick %d the highlight ends on line %d, want the top line on screen, %d",
				i, a.sel.head.line, roomScroll(a))
		}
	}
}

// Held below the transcript it scrolls forward, and stops arming ticks once
// the newest line is on screen: an idle Wake schedules nothing, even under a
// hand that never lets go.
//
// Mutation check: re-arming whether or not the pane moved fails this at "still
// armed".
func TestAPointerHeldPastTheBottomScrollsToTheNewestLineAndStops(t *testing.T) {
	countEdgeTicks(t)
	a := splitApp(t, 200, 40, 40).scrollPane("", 3)
	a, _ = a.mouse(pressAt(10, textRow))
	a, cmd := a.mouse(motion(10, a.selTop+a.selRows))
	for i := 0; cmd != nil; i++ {
		if i == 10 {
			t.Fatalf("still armed after %d ticks with three lines to reach: the edge scroll never stops", i)
		}
		a, cmd = edgeTick(t, a)
	}
	tr := a.transcriptIn("")
	if !tr.atBottom() {
		t.Errorf("the ticks stopped on line %d, short of the newest line", tr.scroll)
	}
	if last := tr.lines.len() - 1; a.sel.head.line != last {
		t.Errorf("the highlight ends on line %d, want the newest line, %d", a.sel.head.line, last)
	}
}

// Letting go ends the scroll; the tick already in flight lands on nothing.
func TestReleasingAHeldEdgeStopsTheScroll(t *testing.T) {
	countEdgeTicks(t)
	a := splitApp(t, 200, 40, 40)
	a, _ = a.mouse(pressAt(10, textRow))
	a, _ = a.mouse(motion(10, 0))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: 10, Y: 0})
	was := roomScroll(a)
	a, cmd := edgeTick(t, a)
	if now := roomScroll(a); now != was {
		t.Errorf("a tick after the release moved the reader from line %d to %d", was, now)
	}
	if cmd != nil {
		t.Error("a tick after the release armed another")
	}
}

// Moving back off the edge ends the scroll, though the button is still down.
func TestLeavingTheEdgeStopsTheScroll(t *testing.T) {
	countEdgeTicks(t)
	a := splitApp(t, 200, 40, 40)
	a, _ = a.mouse(pressAt(10, textRow))
	a, _ = a.mouse(motion(10, 0))
	a, _ = a.mouse(motion(10, textRow))
	was := roomScroll(a)
	a, cmd := edgeTick(t, a)
	if now := roomScroll(a); now != was {
		t.Errorf("a tick with the pointer back inside moved the reader from line %d to %d", was, now)
	}
	if cmd != nil {
		t.Error("a tick with the pointer back inside armed another")
	}
}

// Motion along the edge arrives per cell; each must not start its own ticker.
func TestOneEdgeTickIsInFlightAtATime(t *testing.T) {
	ticks := countEdgeTicks(t)
	a := splitApp(t, 200, 40, 40)
	a, _ = a.mouse(pressAt(10, textRow))
	for x := 10; x <= 20; x++ {
		a, _ = a.mouse(motion(x, 0))
	}
	if *ticks != 1 {
		t.Errorf("eleven motions along the edge armed %d ticks, want one", *ticks)
	}
}

// A tick armed by one drag can land in the next. It serves that drag from
// where *its* pointer is - never from where the last one let go.
func TestATickLeftByAnEarlierDragDoesNotMoveTheNextOne(t *testing.T) {
	ticks := countEdgeTicks(t)
	a := splitApp(t, 200, 40, 40)
	a, _ = a.mouse(pressAt(10, textRow))
	a, _ = a.mouse(motion(10, 0))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: 10, Y: 0})
	a, _ = a.mouse(pressAt(10, textRow+2))
	anchor, was := a.sel.anchor, roomScroll(a)

	a, cmd := edgeTick(t, a)
	if a.sel.head != anchor || roomScroll(a) != was || cmd != nil {
		t.Fatalf("the old drag's tick moved the new one: head %+v (anchor %+v), line %d -> %d, rearmed %v",
			a.sel.head, anchor, was, roomScroll(a), cmd != nil)
	}
	if a, _ = a.mouse(motion(10, 0)); *ticks != 2 || roomScroll(a) >= was {
		t.Errorf("the new drag reaching the edge armed %d ticks in all and left line %d at %d",
			*ticks, was, roomScroll(a))
	}
}

// A tick landing on a frame-wide selection moves nothing: the chrome does not
// scroll, and the pointer it remembers is a transcript drag's.
func TestATickDoesNotMoveAFrameWideSelection(t *testing.T) {
	countEdgeTicks(t)
	a := splitApp(t, 200, 40, 40)
	a, _ = a.mouse(pressAt(10, textRow))
	a, _ = a.mouse(motion(10, 0))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: 10, Y: 0})
	a, _ = a.mouse(pressAt(10, queryBarRows(a)[0]))
	if !a.sel.onScreen {
		t.Fatalf("the press on the query bar took %+v, want a frame-wide selection", a.sel)
	}
	sel := a.sel
	a, cmd := edgeTick(t, a)
	if a.sel != sel || cmd != nil {
		t.Errorf("a tick moved a frame-wide selection from %+v to %+v (rearmed %v)", sel, a.sel, cmd != nil)
	}
}

// Nor on a query-box drag: a draft is all on screen, and its point is a draft
// row, not a line of scrollback.
func TestATickDoesNotMoveAQueryBoxSelection(t *testing.T) {
	countEdgeTicks(t)
	a := splitApp(t, 200, 40, 40)
	a, _ = a.mouse(pressAt(10, textRow))
	a, _ = a.mouse(motion(10, 0))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: 10, Y: 0})
	a = a.withDraft("hello world")
	r := a.regions()
	draftTop, _, _, _, ok := a.composerRegion("", r.Room(), 0, a.paneHeight())
	if !ok {
		t.Fatal("the room drew no composer region")
	}
	a, _ = a.mouse(pressAt(a.layout.PaneLeft(r, 0)+composerTextLeft+1, draftTop))
	if !a.sel.inComposer {
		t.Fatalf("the press on the draft took %+v, want a query-box selection", a.sel)
	}
	sel := a.sel
	a, cmd := edgeTick(t, a)
	if a.sel != sel || cmd != nil {
		t.Errorf("a tick moved a query-box selection from %+v to %+v (rearmed %v)", sel, a.sel, cmd != nil)
	}
}

// Past an edge the highlight ends on the last line on screen, not on a line as
// far below it as the pointer is - which was copied without ever being seen.
//
// Mutation check: dropping the clamp from pointIn fails this.
func TestPastTheBottomTheHighlightEndsOnTheLastLineOnScreen(t *testing.T) {
	countEdgeTicks(t)
	a := splitApp(t, 200, 40, 40).scrollPane("", 20)
	a, _ = a.mouse(pressAt(10, textRow))
	a, _ = a.mouse(motion(10, a.selTop+a.selRows+3))
	if want := roomScroll(a) + a.selRows - 1; a.sel.head.line != want {
		t.Errorf("the highlight ends on line %d, want the last line on screen, %d", a.sel.head.line, want)
	}
}

// The same above a pane: a pointer over the pane stacked on top of this one
// ends the highlight on this pane's own first line on screen.
func TestAboveAStackedPaneTheHighlightEndsOnItsFirstLineOnScreen(t *testing.T) {
	countEdgeTicks(t)
	a, top := stackedApp(t)
	a, _ = a.mouse(pressAt(10, top+2))
	if a.sel.pane != "s2" {
		t.Fatalf("the press took %+v, want a selection in the lower pane", a.sel)
	}
	a, _ = a.mouse(motion(10, top-3))
	if want := a.transcriptIn("s2").scroll; a.sel.head.line != want {
		t.Errorf("the highlight ends on line %d, want the lower pane's top line on screen, %d", a.sel.head.line, want)
	}
}

// stackedApp is splitApp with s2 open under the room and long enough to scroll,
// and the screen row s2's pane starts on.
func stackedApp(t *testing.T) (App, int) {
	t.Helper()
	a := splitApp(t, 200, 40, 40).openBelow("s2", "jesse")
	for i := range 60 {
		a = said(a, "s2", fmt.Sprintf("line %d", i))
	}
	_, top, height, ok := a.paneAt(0, a.paneHeight()-1)
	if !ok || top == 0 {
		t.Fatalf("no pane stacked under the room: top %d, height %d", top, height)
	}
	return a, top
}

// heldInDM is a drag in s1's conversation brought to its top row and held, with
// a tick in flight.
func heldInDM(t *testing.T) App {
	t.Helper()
	a := splitApp(t, 200, 40, 4)
	for i := range 60 {
		a = said(a, "s1", fmt.Sprintf("line %d", i))
	}
	x := midOf(a.regions(), 1)
	a, _ = a.mouse(pressAt(x, 10))
	a, cmd := a.mouse(motion(x, 0))
	if a.sel.pane != "s1" || cmd == nil {
		t.Fatalf("no held drag in s1: sel %+v, armed %v", a.sel, cmd != nil)
	}
	return a
}

// A conversation can leave mid-drag - a /quit confirmed by the daemon - and the
// tick in flight, or the release, then reached for a DM that is gone: a nil
// dereference that took Wake down with no key pressed.
//
// Mutation check: dropping the selection clear from forgetConversation panics
// here.
func TestAConversationLeavingMidDragEndsTheDrag(t *testing.T) {
	countEdgeTicks(t)
	a := heldInDM(t).forgetConversation("s1")
	a, cmd := edgeTick(t, a)
	if cmd != nil {
		t.Error("a tick for a conversation that has gone armed another")
	}
	if _, cmd = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: 10, Y: 0}); cmd != nil {
		t.Error("the release copied out of a conversation that has gone")
	}
}

// A fork arriving replaces the pane it was asked from, and the drag held in it
// must not go on scrolling a conversation that is no longer drawn.
//
// Mutation check: dropping the grid check from extendSelection fails this.
func TestAPaneReplacedMidDragIsNotScrolled(t *testing.T) {
	countEdgeTicks(t)
	a := heldInDM(t)
	a = a.openDMWith("s9", "sam")
	if a.grid.Has("s1") {
		t.Fatalf("s1 is still drawn; the replace did not happen: %+v", a.grid)
	}
	was := a.transcriptIn("s1").scroll
	a, cmd := edgeTick(t, a)
	if now := a.transcriptIn("s1").scroll; now != was || cmd != nil {
		t.Errorf("a tick scrolled a pane no longer drawn from line %d to %d (rearmed %v)", was, now, cmd != nil)
	}
}

// A window too short for two stacked panes stops drawing the lower one but
// keeps it in the grid, and a drag held in it must stop with it - not go on
// scrolling a conversation nobody can see.
//
// Mutation check: guarding on grid.Has rather than drawn fails this.
func TestAPaneHiddenByAShorterWindowMidDragIsNotScrolled(t *testing.T) {
	countEdgeTicks(t)
	a, top := stackedApp(t)
	a, _ = a.mouse(pressAt(10, top+2))
	a, cmd := a.mouse(motion(10, top-3))
	if a.sel.pane != "s2" || cmd == nil {
		t.Fatalf("no held drag in s2: sel %+v, armed %v", a.sel, cmd != nil)
	}
	a, _ = a.resized(200, 12)
	if slices.Contains(a.drawn(), "s2") || !a.grid.Has("s2") {
		t.Fatalf("want s2 in the grid but not drawn at 12 rows: drawn %v", a.drawn())
	}
	was := a.transcriptIn("s2").scroll
	a, cmd = edgeTick(t, a)
	if now := a.transcriptIn("s2").scroll; now != was || cmd != nil {
		t.Errorf("a tick scrolled a pane the window no longer draws from line %d to %d (rearmed %v)", was, now, cmd != nil)
	}
}

// A width change settles 80ms later, and until it does drawn() reads the old
// layout - a pane the narrower frame already clips passes it. The settle clears
// the selection anyway, so a drag with one on its way extends nothing.
//
// Mutation check: dropping the pending-width test from extendSelection fails
// this.
func TestADragHeldThroughAWidthChangeIsNotScrolled(t *testing.T) {
	countEdgeTicks(t)
	a := heldInDM(t)
	a, _ = a.resized(80, 40)
	if a.pending.width == a.layout.Width {
		t.Fatal("the width change applied at once; there is no pending window to test")
	}
	was := a.transcriptIn("s1").scroll
	a, cmd := edgeTick(t, a)
	if now := a.transcriptIn("s1").scroll; now != was || cmd != nil {
		t.Errorf("a tick scrolled a pane mid-resize from line %d to %d (rearmed %v)", was, now, cmd != nil)
	}
}

// A terminal can swallow a release. The next press begins a new gesture, so a
// divider press must not leave the lost drag live - scrolled on by its tick,
// and copied by the divider's own release.
//
// Mutation check: dropping the selecting reset from press fails this.
func TestAPressOnTheDividerEndsADragWhoseReleaseWasLost(t *testing.T) {
	countEdgeTicks(t)
	a := splitApp(t, 200, 40, 40)
	a, _ = a.mouse(pressAt(10, textRow))
	a, _ = a.mouse(motion(10, 0))
	a = grab(t, a, dividerColumnOf(a))
	was := roomScroll(a)
	a, cmd := edgeTick(t, a)
	if now := roomScroll(a); now != was || cmd != nil {
		t.Errorf("a divider press left the lost drag live: line %d -> %d (rearmed %v)", was, now, cmd != nil)
	}
	if _, cmd = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: dividerColumnOf(a)}); cmd != nil {
		t.Error("the divider's release copied the drag whose release was lost")
	}
}

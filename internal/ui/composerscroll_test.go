package ui

// The wheel over a draft taller than its box scrolls the draft's rows in the box
// (the owner's 2026-11-06 ask): the view only, never the caret, and the next key
// brings the view back to the caret. Everywhere else the wheel does what it did.

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// boxIn is where pane id's query box is drawn: a column inside it, the screen row
// of its first draft row, and how many draft rows it draws.
func boxIn(t *testing.T, a App, id string) (x, draftTop, draftRows int) {
	t.Helper()
	r := a.regions()
	col := a.columnOf(id)
	for y := range a.paneHeight() {
		pid, top, height, ok := a.paneAt(col, y)
		if !ok || pid != id {
			continue
		}
		dt, dr, _, _, ok := a.composerRegion(id, r.Cols[col], top, height)
		if !ok {
			t.Fatalf("pane %q drew no query box", id)
		}
		return a.layout.PaneLeft(r, col) + composerTextLeft + 2, dt, dr
	}
	t.Fatalf("pane %q is not drawn", id)
	return 0, 0, 0
}

// boxText is the typed text of each draft row pane id's box draws.
func boxText(t *testing.T, a App, id string) []string {
	t.Helper()
	r := a.regions()
	col := a.columnOf(id)
	_, top, height, _ := a.paneAt(col, a.paneHeight()-1)
	if id != a.grid.Cols[col].Bottom {
		_, top, height, _ = a.paneAt(col, 0)
	}
	draftTop, _, boxWidth, drawn, ok := a.composerRegion(id, r.Cols[col], top, height)
	if !ok {
		t.Fatalf("pane %q drew no query box", id)
	}
	frameDrawsTheBox(t, a, id, col, draftTop, boxWidth, drawn)
	if len(drawn.starts) != len(drawn.rows) {
		t.Fatalf("pane %q: %d starts for %d drawn rows", id, len(drawn.starts), len(drawn.rows))
	}
	out := make([]string, len(drawn.rows))
	lines := strings.Split(mustComposer(t, a, id).Value(), "\n")
	for i, row := range drawn.rows {
		out[i] = strings.TrimRight(ansi.Strip(ansi.Cut(row, composerTextLeft, boxWidth-composerRightInset)), " ")
		// What a click, a drag and ⌫ map through must read what is drawn.
		if s := drawn.starts[i]; !strings.HasPrefix(string([]rune(lines[s.line])[s.col:]), out[i]) {
			t.Fatalf("pane %q: drawn row %d reads %q, but its start (line %d, rune %d) does not", id, i, out[i], s.line, s.col)
		}
	}
	return out
}

// frameDrawsTheBox fails unless the frame App.View draws has pane id's box where
// the mouse reads it: the borders on the rows either side, and each draft row's
// text on the row the mouse maps it to.
func frameDrawsTheBox(t *testing.T, a App, id string, col, draftTop, boxWidth int, drawn drawnDraft) {
	t.Helper()
	frame := strings.Split(ansi.Strip(a.View()), "\n")
	left := a.layout.PaneLeft(a.regions(), col)
	cell := func(y, from, to int) string { return strings.TrimRight(ansi.Cut(frame[y], left+from, left+to), " ") }
	bottom := draftTop + len(drawn.rows)
	if draftTop < 1 || bottom >= len(frame) || !strings.HasPrefix(cell(draftTop-1, 0, 1), "╭") || !strings.HasPrefix(cell(bottom, 0, 1), "╰") {
		t.Fatalf("pane %q: the frame does not draw the box's borders around rows %d-%d where the mouse reads it", id, draftTop, bottom-1)
	}
	for i, row := range drawn.rows {
		if got, want := cell(draftTop+i, composerTextLeft, boxWidth-composerRightInset), rowText(row, boxWidth); got != want {
			t.Fatalf("pane %q: the frame draws %q on draft row %d, the mouse reads %q", id, got, i, want)
		}
	}
}

// wheelAt is n notches of the wheel at one cell, delivered as Bubble Tea does.
func wheelAt(a App, button tea.MouseButton, x, y, n int) App {
	for range n {
		m, _ := a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: button, X: x, Y: y})
		a = m.(App)
	}
	return a
}

// caretIn is where pane id's caret is: its logical line and rune column. Read
// without typing a marker, which would edit the draft the composer shares.
func caretIn(t *testing.T, a App, id string) rowStart {
	t.Helper()
	ta := mustComposer(t, a, id).ta
	info := ta.LineInfo()
	return rowStart{line: ta.Line(), col: info.StartColumn + info.ColumnOffset}
}

// linesFrom is n lines of tallDraft starting at line first, as the box draws them.
func linesFrom(first, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("line %02d", first+i)
	}
	return out
}

// A draft of twenty lines in a ten-row box shows its last ten. The wheel over the
// box brings the earlier ones in, three rows a notch, and stops at the first;
// back down it returns to the caret's rows. The caret never moves.
func TestTheWheelOverATallDraftScrollsItsRows(t *testing.T) {
	draft := tallDraft(20)
	a := roomDraftApp(t, draft)
	x, top, rows := boxIn(t, a, "")
	if got := boxText(t, a, ""); !slices.Equal(got, linesFrom(10, 10)) || rows != 10 {
		t.Fatalf("want the box drawing lines 10-19 of the draft, got %d rows: %q", rows, got)
	}
	steps := []struct {
		button tea.MouseButton
		first  int
	}{
		{tea.MouseButtonWheelUp, 7},
		{tea.MouseButtonWheelUp, 4},
		{tea.MouseButtonWheelUp, 1},
		{tea.MouseButtonWheelUp, 0}, // the draft's first row ends the wheel there
		{tea.MouseButtonWheelUp, 0},
		{tea.MouseButtonWheelDown, 3},
		{tea.MouseButtonWheelDown, 6},
		{tea.MouseButtonWheelDown, 9},
		{tea.MouseButtonWheelDown, 10}, // and the caret's rows, at the bottom
		{tea.MouseButtonWheelDown, 10},
	}
	end := rowStart{line: 19, col: len("line 19")}
	for i, s := range steps {
		a = wheelAt(a, s.button, x, top+rows/2, 1)
		if got := boxText(t, a, ""); !slices.Equal(got, linesFrom(s.first, 10)) {
			t.Fatalf("notch %d: the box draws %q, want lines %02d-%02d", i+1, got, s.first, s.first+9)
		}
		if got := caretIn(t, a, ""); got != end {
			t.Fatalf("notch %d moved the caret to %+v, want it left at the draft's end %+v", i+1, got, end)
		}
	}
	if got := a.composer().Value(); got != draft {
		t.Errorf("the wheel changed the draft:\n%q", got)
	}
}

// The box is the whole of it, its borders included; the row above it is not.
func TestTheWheelScrollsTheDraftAnywhereOnItsBox(t *testing.T) {
	for _, c := range []struct {
		name   string
		dy     func(top, rows int) int
		scroll bool
	}{
		{"top border", func(top, _ int) int { return top - 1 }, true},
		{"first draft row", func(top, _ int) int { return top }, true},
		{"bottom border", func(top, rows int) int { return top + rows }, true},
		{"the row above the box", func(top, _ int) int { return top - 2 }, false},
	} {
		a := roomDraftApp(t, tallDraft(20))
		x, top, rows := boxIn(t, a, "")
		a = wheelAt(a, tea.MouseButtonWheelUp, x, c.dy(top, rows), 1)
		got := boxText(t, a, "")
		if moved := got[0] != "line 10"; moved != c.scroll {
			t.Errorf("%s: the wheel moved the draft %v, want %v (box draws %q)", c.name, moved, c.scroll, got)
		}
	}
}

// Any key that reaches the box brings the view back to the caret first, as an
// editor's does - and lands where the caret was, not where the view had gone.
func TestAKeyBringsAScrolledDraftBackToTheCaret(t *testing.T) {
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("Z")},
		{Type: tea.KeyLeft},
	} {
		a := roomDraftApp(t, tallDraft(20))
		x, top, _ := boxIn(t, a, "")
		a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 4)
		if got := boxText(t, a, ""); got[0] != "line 00" {
			t.Fatalf("the wheel did not scroll the draft to its top: %q", got)
		}
		m, _ := a.Update(k)
		a = m.(App)
		got := boxText(t, a, "")
		if got[0] != "line 10" || !strings.HasPrefix(got[9], "line 19") {
			t.Errorf("%s left the box drawing %q, want it back on the caret's rows 10-19", k, got)
		}
	}
}

// The caret's blink arrives twice a second and is not a key: it must not drag the
// view back to the caret under somebody reading the top of their draft.
func TestTheCaretsBlinkLeavesAScrolledDraftWhereItIs(t *testing.T) {
	a := roomDraftApp(t, tallDraft(20))
	x, top, _ := boxIn(t, a, "")
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 4)
	for range 3 {
		m, _ := a.Update(cursor.BlinkMsg{})
		a = m.(App)
	}
	if got := boxText(t, a, ""); got[0] != "line 00" {
		t.Errorf("a blink moved the scrolled draft back to the caret: the box draws %q", got)
	}
}

// A draft the box shows whole leaves the wheel to the transcript, as it always has.
func TestTheWheelOverADraftThatFitsScrollsTheTranscript(t *testing.T) {
	a := splitApp(t, 200, 40, 40).withDraft("a short draft")
	x, top, _ := boxIn(t, a, "")
	before := a.room.tr.scroll
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 1)
	if a.room.tr.scroll == before {
		t.Errorf("the wheel over a draft that fits did not scroll the room's transcript (scroll stayed %d)", before)
	}
}

// Past the draft's first row the wheel over the box stays with the box: it does
// not carry on into the transcript (the orchestrator's 2026-11-06 ruling).
func TestTheWheelStopsAtTheDraftsFirstRow(t *testing.T) {
	a := splitApp(t, 200, 40, 40).withDraft(tallDraft(20))
	x, top, _ := boxIn(t, a, "")
	before := a.room.tr.scroll
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 8)
	if got := boxText(t, a, ""); got[0] != "line 00" {
		t.Fatalf("the draft did not reach its first row: %q", got)
	}
	if a.room.tr.scroll != before {
		t.Errorf("the wheel carried on past the draft into the transcript: scroll %d -> %d", before, a.room.tr.scroll)
	}
}

// A conversation's box, and the lower half of a stacked column, scroll the same
// way, each under its own pointer and nobody else's.
func TestTheWheelScrollsTheDraftInTheConversationUnderIt(t *testing.T) {
	a, _ := stackedApp(t)
	draft := tallDraft(20)
	a = a.withComposerFor("s2", mustComposer(t, a, "s2").InsertText(draft))
	a = a.withComposerFor("", a.room.Composer().InsertText(draft))
	x, top, _ := boxIn(t, a, "s2")
	roomBefore := boxText(t, a, "")
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 1)
	if got := boxText(t, a, "s2"); len(got) == 0 || got[0] == strings.Split(draft, "\n")[20-len(got)] {
		t.Errorf("the wheel over s2's box did not scroll its draft: %q", got)
	}
	if got := boxText(t, a, ""); !slices.Equal(got, roomBefore) {
		t.Errorf("the wheel over s2's box scrolled the room's draft: %q, was %q", got, roomBefore)
	}
}

// mustComposer is pane id's composer.
func mustComposer(t *testing.T, a App, id string) Composer {
	t.Helper()
	c, ok := a.composerFor(id)
	if !ok {
		t.Fatalf("no pane %q to type into", id)
	}
	return c
}

// A click in a draft the wheel scrolled lands on the character drawn under it,
// and leaves the view where the reader put it.
func TestAClickInAScrolledDraftLandsOnTheDrawnCharacter(t *testing.T) {
	draft := tallDraft(20)
	a := roomDraftApp(t, draft)
	x, top, _ := boxIn(t, a, "")
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 1) // lines 07-16
	a = clickDraftRow(t, a, 2, 5)
	if got := boxText(t, a, ""); !slices.Equal(got, linesFrom(7, 10)) {
		t.Errorf("the click moved the scrolled view: the box draws %q, want lines 07-16", got)
	}
	if got, want := caretMarked(a), markedAt(draft, 9, 5); got != want {
		t.Errorf("clicking drawn row 2 put the marker at\n%q\nwant\n%q", got, want)
	}
}

// A drag in a scrolled draft copies the text drawn under it.
func TestADragInAScrolledDraftCopiesTheDrawnText(t *testing.T) {
	a := roomDraftApp(t, tallDraft(20))
	x, top, _ := boxIn(t, a, "")
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 4) // lines 00-09
	left := a.layout.PaneLeft(a.regions(), 0) + composerTextLeft
	a, _ = a.mouse(pressAt(left, top+3))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: left + 6, Y: top + 3})
	if got := a.composerSelectedText(); got != "line 03" {
		t.Errorf("dragging across drawn row 3 copies %q, want %q", got, "line 03")
	}
}

// A wheel that moves the draft drops a query-box selection in that box: the rows
// it captured at its press are no longer the rows drawn.
func TestAWheelThatMovesTheDraftDropsAQueryBoxSelection(t *testing.T) {
	a := roomDraftApp(t, tallDraft(20))
	x, top, _ := boxIn(t, a, "")
	left := a.layout.PaneLeft(a.regions(), 0) + composerTextLeft
	a, _ = a.mouse(pressAt(left, top+3))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: left + 6, Y: top + 3})
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: left + 6, Y: top + 3})
	if a.sel.empty() || !a.sel.inComposer {
		t.Fatalf("no query-box selection to drop: %+v", a.sel)
	}
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 1)
	if !a.sel.empty() {
		t.Errorf("the wheel moved the draft and left the query-box selection up: %+v", a.sel)
	}
}

// A re-lay that keeps the box filled keeps the reader's place; one that changes
// the width re-wraps the draft, so the view goes back to the caret.
func TestAResizeKeepsTheScrolledDraftUnlessTheWidthChanges(t *testing.T) {
	a := roomDraftApp(t, tallDraft(20))
	x, top, _ := boxIn(t, a, "")
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 2) // lines 04-13
	taller := a.withSize(120, 44)
	if got := boxText(t, taller, ""); !slices.Equal(got, linesFrom(4, 10)) {
		t.Errorf("a taller window lost the reader's place: the box draws %q, want lines 04-13", got)
	}
	narrower := a.withSize(100, 40).applyGeometry() // a width lands after the settle
	if got := boxText(t, narrower, ""); got[0] != "line 10" {
		t.Errorf("a narrower window kept a view of rows it re-wrapped: the box draws %q, want lines 10-19", got)
	}
}

// Every start drawnRowStarts reports while the wheel holds the view begins with
// the text drawn on that row, across tall, wrapped, full-width and wide drafts -
// what a click, a drag and ⌫ over a selection all map through.
func TestDrawnRowStartsMatchTheDrawnRowsOfAScrolledDraft(t *testing.T) {
	const width = 40
	w := NewComposer().SetWidth(width).ta.Width()
	full := strings.Repeat("x", w)
	words := make([]string, 60)
	for i := range words {
		words[i] = fmt.Sprintf("t%02d", i)
	}
	drafts := map[string]string{
		"tall":       tallDraft(20),
		"wrapped":    strings.Join(words, " "),
		"full-width": tallDraft(6) + "\n" + full + "\nmid\n" + full + "\n" + tallDraft(6),
		"wide":       "a0\n" + wideRunes(0, 40) + "\nb1 " + wideRunes(100, 30) + "\n" + wideRunes(200, 17) + "\nc2",
	}
	for name, draft := range drafts {
		whole := wholeDraft(t, draft, width)
		for notches := 1; notches <= 8; notches++ {
			c := NewComposer().SetWidth(width).WithMaxRows(5)
			c.ta.InsertString(draft)
			c = c.fit().reposition()
			for range notches {
				c, _ = c.Wheel(wheelLines)
			}
			for range notches / 3 {
				c, _ = c.Wheel(-wheelLines)
			}
			d := caretTestDraft(c, width)
			if len(d.starts) != len(d.rows) {
				t.Fatalf("%s, %d notches: %d starts for %d drawn rows", name, notches, len(d.starts), len(d.rows))
			}
			first := slices.Index(whole.starts, d.starts[0])
			if first < 0 {
				t.Fatalf("%s, %d notches: the first start %+v is no row of the draft", name, notches, d.starts[0])
			}
			for i, s := range d.starts {
				drawn, want := rowText(d.rows[i], width), rowText(whole.rows[first+i], width)
				if s != whole.starts[first+i] || drawn != want {
					t.Errorf("%s, %d notches: drawn row %d reads %q from %+v, want draft row %d, %q from %+v",
						name, notches, i, drawn, s, first+i, want, whole.starts[first+i])
				}
			}
		}
	}
}

// wholeDraft is every row of draft as an unscrolled box at width draws it, and
// where each begins: the oracle a scrolled window is a slice of.
func wholeDraft(t *testing.T, draft string, width int) drawnDraft {
	t.Helper()
	c := NewComposer().SetWidth(width).WithMaxRows(99)
	c.ta.InsertString(draft)
	c = c.fit().reposition()
	d := caretTestDraft(c, width)
	if d.starts[0] != (rowStart{}) || len(d.starts) != len(d.rows) {
		t.Fatalf("the oracle box is not the whole draft: starts %+v", d.starts)
	}
	return d
}

// rowText is a drawn draft row's typed text.
func rowText(row string, width int) string {
	return strings.TrimRight(ansi.Strip(ansi.Cut(row, composerTextLeft, width-composerRightInset)), " ")
}

// With a card the pane holds up and its page at rest, the box is where it always
// is: the wheel over it scrolls a tall draft, and over the transcript it still
// slides the page. Once the page has moved - or a pane too short for its card cut
// it - the box is not where a press reads it, so the wheel stays the page's.
func TestTheWheelOverTheBoxUnderACardScrollsTheDraft(t *testing.T) {
	a := askingWith(t, pageSetup{height: 70, history: pageHistory, draft: tallDraft(20)})
	if moved, away := a.pageHides("s1"); moved || away {
		t.Fatalf("want a card up with its page at rest over the box: moved %v, box away %v", moved, away)
	}
	x, top, _ := boxIn(t, a, "s1")
	if got := boxText(t, a, "s1"); got[0] != "line 10" {
		t.Fatalf("want the box drawing the draft's last rows under the card, got %q", got)
	}
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 1)
	if got := boxText(t, a, "s1"); got[0] != "line 07" {
		t.Errorf("the wheel over the box under a card did not scroll the draft: %q", got)
	}
	if a.boxAway("s1") {
		t.Errorf("the wheel over the box under a card moved the page instead")
	}
	a = wheelAt(a, tea.MouseButtonWheelUp, pageWheelX, pageWheelY, 1)
	if !a.boxAway("s1") {
		t.Fatalf("the wheel over the transcript no longer slides the page")
	}
	before, _, _ := a.pageOf("s1")
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 1)
	if after, _, _ := a.pageOf("s1"); after.up <= before.up {
		t.Errorf("with the page moved, the wheel where the box was did not move the page: up %d -> %d", before.up, after.up)
	}
}

// A draft put in the box from anywhere else is not where the wheel left it, even
// the same words: SetValue moves the view back to the draft's top.
func TestADraftPutInTheBoxLetsTheWheelsViewGo(t *testing.T) {
	c := NewComposer().SetWidth(60).WithMaxRows(5)
	c.ta.InsertString(tallDraft(20))
	c = c.fit().reposition()
	c, _ = c.Wheel(wheelLines)
	if !c.held() {
		t.Fatal("the wheel did not hold a view to let go of")
	}
	if c = c.WithDraft(c.Value()); c.held() {
		t.Errorf("WithDraft kept the wheel's view over a draft it put back at the top")
	}
	// And a sent draft's box, typed full again with the same words, is a new draft.
	if c, _ = c.Wheel(wheelLines); !c.held() {
		t.Fatal("the wheel did not hold a view over the put-back draft")
	}
	words := c.Value()
	if c = c.Reset().InsertText(words); c.held() {
		t.Errorf("a reset box typed full again with the same words revived the wheel's old view")
	}
}

// shortRoom is a room too short for the box's ten-row cap, so chrome drawn above
// the box - a menu, a working line - changes the height it is drawn at.
func shortRoom(t *testing.T, height int) App {
	t.Helper()
	a := newRoomApp(t).withSize(120, height).withAgents("alex")
	a.layout.ShowRoster = false
	a.focus = ""
	return a.applyGeometry()
}

// A completion menu over the box drops the gap above it, so the box is drawn a
// row taller than the pane holds it. The wheel's view is the drawn box's: what a
// row draws is what a drag over it takes and what ⌫ then deletes.
func TestTheWheelUnderAMenuScrollsTheBoxAsDrawn(t *testing.T) {
	draft := tallDraft(20) + " @a"
	a := shortRoom(t, 14).withDraft(draft)
	if a.menuBlock("", a.regions().Room(), a.paneHeight()) == "" {
		t.Fatal("no completion menu is open over the draft")
	}
	x, top, rows := boxIn(t, a, "")
	if held := a.room.Composer().ta.Height(); rows == held {
		t.Fatalf("want the box drawn at another height than the pane holds it: both %d", rows)
	}
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 2)
	a = wheelAt(a, tea.MouseButtonWheelDown, x, top, 1)
	drawn := boxText(t, a, "") // fails on a row its start does not read
	line := slices.Index(strings.Split(draft, "\n"), drawn[0])
	if line < 0 || line == 20-rows {
		t.Fatalf("the wheel did not scroll the drawn box: %q", drawn)
	}
	left := a.layout.PaneLeft(a.regions(), 0) + composerTextLeft
	got := dragThenKey(t, a, left, left+len(drawn[0])-1, top, tea.KeyMsg{Type: tea.KeyBackspace})
	want := strings.Split(draft, "\n")
	want[line] = ""
	if got != strings.Join(want, "\n") {
		t.Errorf("⌫ over the drawn %q deleted something else:\n%q", drawn[0], got)
	}
}

// A working line drawn above the room's box makes it shorter than the pane holds
// it: a draft that fits the held box but not the drawn one still scrolls, three
// rows a notch, and the transcript stays where it was.
func TestTheWheelScrollsADraftTheWorkingLineLeavesTooTall(t *testing.T) {
	a := shortRoom(t, 17)
	a = a.applyStatus(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateWorking}}})
	for i := range 40 {
		a = said(a, "s1", fmt.Sprintf("said %d", i))
	}
	draft := tallDraft(12)
	a = a.withComposerFor("", a.room.Composer().InsertText(draft))
	x, top, rows := boxIn(t, a, "")
	if rows >= 12 || rows >= a.room.Composer().ta.Height() {
		t.Fatalf("want a drawn box shorter than both the draft and the box the pane holds: drawn %d, held %d", rows, a.room.Composer().ta.Height())
	}
	before, heldRows := a.room.tr.scroll, a.room.Composer().ta.Height()
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 1)
	if got := a.room.Composer().ta.Height(); got != heldRows {
		t.Errorf("the wheel left the room's own composer %d rows tall, the drawn box's height, not its %d", got, heldRows)
	}
	if got := boxText(t, a, ""); got[0] != fmt.Sprintf("line %02d", 12-rows-3) {
		t.Errorf("one notch drew from %q, want three rows up from line %02d", got[0], 12-rows)
	}
	if a.room.tr.scroll != before {
		t.Errorf("the wheel over the box scrolled the transcript: %d -> %d", before, a.room.tr.scroll)
	}
}

// A compacting conversation draws its bar a row taller, and in a short pane that
// row comes out of the box: a notch is still three of the rows it draws.
func TestTheWheelInACompactingConversationMovesThreeDrawnRows(t *testing.T) {
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(80, 16)
	a = a.withComposer(a.composer().InsertText(tallDraft(20)))
	a = a.observe("s1", core.Event{Kind: core.KindSystem, Notice: core.NoticeCompacting})
	x, top, rows := boxIn(t, a, "s1")
	if held := mustComposer(t, a, "s1").ta.Height(); rows >= held {
		t.Fatalf("want the compacting bar to take rows from the box: drawn %d, held %d", rows, held)
	}
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 1)
	if got := boxText(t, a, "s1"); got[0] != fmt.Sprintf("line %02d", 20-rows-3) {
		t.Errorf("one notch drew from %q, want line %02d", got[0], 20-rows-3)
	}
}

// At the draft's last row the wheel stays with the box too: it does not carry on
// into a transcript scrolled back above it.
func TestTheWheelStopsAtTheDraftsLastRow(t *testing.T) {
	a := splitApp(t, 200, 40, 40)
	a = wheelAt(a, tea.MouseButtonWheelUp, a.layout.PaneLeft(a.regions(), 0)+5, 2, 3)
	a = a.withDraft(tallDraft(20))
	x, top, _ := boxIn(t, a, "")
	before := a.room.tr.scroll
	a = wheelAt(a, tea.MouseButtonWheelDown, x, top, 2)
	if got := boxText(t, a, ""); got[0] != "line 10" {
		t.Fatalf("the draft is not at its last rows: %q", got)
	}
	if a.room.tr.scroll != before {
		t.Errorf("the wheel carried on past the draft's last row into the transcript: scroll %d -> %d", before, a.room.tr.scroll)
	}
}

// A box grown past the last row of the view the wheel left would draw blank rows
// under the draft, so it lets the view go and follows the caret again.
func TestABoxGrownPastAHeldViewFollowsTheCaretAgain(t *testing.T) {
	draft := tallDraft(20)
	c := NewComposer().SetWidth(60).WithMaxRows(5)
	c.ta.InsertString(draft)
	c = c.placeCursor(draft, 0).fit().reposition() // the caret on the first row
	for range 7 {
		c, _ = c.Wheel(-wheelLines)
	}
	if got := c.drawnRowStarts(5)[0]; !c.held() || got.line != 15 {
		t.Fatalf("want the wheel holding the draft's last five rows, held %v from %+v", c.held(), got)
	}
	c = c.WithMaxRows(10)
	d := caretTestDraft(c, 60)
	if len(d.starts) != len(d.rows) || d.starts[0] != (rowStart{}) {
		t.Errorf("a box grown to ten rows kept a view of five: %d starts for %d rows, from %+v", len(d.starts), len(d.rows), d.starts[0])
	}
}

// Text that reaches the draft without a key - an image's chip, a take-back -
// changes the draft, so the view goes back to the caret.
func TestADraftChangedWithoutAKeyBringsTheViewBack(t *testing.T) {
	a := roomDraftApp(t, tallDraft(20))
	x, top, _ := boxIn(t, a, "")
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 4)
	a = a.withComposerFor("", a.room.Composer().InsertText("ZZ"))
	if got := boxText(t, a, ""); got[0] != "line 10" || got[9] != "line 19ZZ" {
		t.Errorf("text inserted without a key left the box drawing %q, want the caret's rows", got)
	}
}

// ⌫ over a selection in a scrolled draft deletes the text highlighted, and the
// changed draft brings the view back to the caret.
func TestBackspaceOverASelectionInAScrolledDraft(t *testing.T) {
	draft := tallDraft(20)
	a := roomDraftApp(t, draft)
	x, top, _ := boxIn(t, a, "")
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 4) // lines 00-09
	left := a.layout.PaneLeft(a.regions(), 0) + composerTextLeft
	got := dragThenKey(t, a, left, left+6, top+3, tea.KeyMsg{Type: tea.KeyBackspace})
	want := strings.Split(draft, "\n")
	want[3] = ""
	if got != strings.Join(want, "\n") {
		t.Errorf("⌫ over the drawn \"line 03\" left\n%q", got)
	}
}

// A paste's text arrives after ⌃V as a message of its own, not a key. Landing on a
// view the wheel holds, it is inserted all the same, and brings the view back.
func TestAPasteLandingOnAHeldViewIsInserted(t *testing.T) {
	pasted, ok := pasteMsgOf("PASTED")
	if !ok {
		t.Skip("bubbles' paste message could not be built from this clipboard read")
	}
	a := roomDraftApp(t, tallDraft(20))
	x, top, _ := boxIn(t, a, "")
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 4)
	m, _ := a.Update(pasted)
	a = m.(App)
	if got := a.composer().Value(); !strings.HasSuffix(got, "line 19PASTED") {
		t.Fatalf("the paste was lost under the held view: the draft ends %q", got[max(len(got)-20, 0):])
	}
	if got, want := caretIn(t, a, ""), (rowStart{line: 19, col: len("line 19PASTED")}); got != want {
		t.Errorf("the caret is at %+v after the paste, want it after the pasted text, %+v", got, want)
	}
	if got := boxText(t, a, ""); got[0] != "line 10" {
		t.Errorf("the paste left the box drawing %q, want the caret's rows", got)
	}
}

// pasteMsgOf is bubbles' unexported paste message carrying text, its type read off
// what textarea.Paste returns. The read is of the clipboard, so a failed one - an
// error message rather than text - reports false.
func pasteMsgOf(text string) (tea.Msg, bool) {
	typ := reflect.TypeOf(textarea.Paste())
	if typ == nil || typ.Kind() != reflect.String {
		return nil, false
	}
	v := reflect.New(typ).Elem()
	v.SetString(text)
	msg, ok := v.Interface().(tea.Msg)
	return msg, ok
}

// A wheel at the draft's end moves nothing, so a selection in the box stays up.
func TestAWheelAtTheDraftsEndKeepsAQueryBoxSelection(t *testing.T) {
	a := roomDraftApp(t, tallDraft(20))
	x, top, _ := boxIn(t, a, "")
	left := a.layout.PaneLeft(a.regions(), 0) + composerTextLeft
	a, _ = a.mouse(pressAt(left, top+3))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: left + 6, Y: top + 3})
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: left + 6, Y: top + 3})
	if a.sel.empty() || !a.sel.inComposer {
		t.Fatalf("no query-box selection to keep: %+v", a.sel)
	}
	a = wheelAt(a, tea.MouseButtonWheelDown, x, top, 1) // the caret's rows are the draft's last
	if a.sel.empty() {
		t.Errorf("a wheel that moved nothing dropped the query-box selection")
	}
}

// The box the mouse reads is the box the frame draws, in every pane: a short
// conversation with a menu up, where a fresh layout would draw the box a row
// taller than the frame does; the lower half of a stacked column; a short room
// re-laid while its draft was tall. The wheel scrolls it, and ⌫ over a drawn row
// deletes that row.
func TestTheWheelMapsTheRowsTheFrameDraws(t *testing.T) {
	for _, c := range []struct {
		name  string
		draft string
		build func(t *testing.T) (App, string)
	}{
		{"a short conversation retrying the API", tallDraft(20), func(t *testing.T) (App, string) {
			a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(80, 16)
			return reportStates(a, map[string]string{"s1": rpc.StateWorking}).apply(retryFrame(3, 0, 30*time.Second)), "s1"
		}},
		{"a short conversation with a menu up", tallDraft(20) + " @b", func(t *testing.T) (App, string) {
			return dmApp(nil, Stream{}, "s1", "alex").withAgents("alex", "bo").withSize(80, 14), "s1"
		}},
		{"the lower half of a stacked column with a menu up", tallDraft(20) + " /", func(t *testing.T) (App, string) {
			return splitApp(t, 200, 24, 4).openBelow("s2", "jesse").refocus("s2"), "s2"
		}},
		{"a short room re-laid with its draft tall", tallDraft(20) + " @a", func(t *testing.T) (App, string) {
			return shortRoom(t, 14), ""
		}},
	} {
		a, id := c.build(t)
		a = a.withDraft(c.draft)
		if id == "" { // a re-lay with the draft tall: the room's stored chrome now counts it
			a = a.withSize(121, 14).applyGeometry().withSize(120, 14).applyGeometry()
		}
		r := a.regions()
		if strings.ContainsAny(c.draft[len(c.draft)-2:], "@/") && a.menuBlock(id, r.Cols[a.columnOf(id)], a.paneHeight()) == "" {
			t.Fatalf("%s: no menu is open over the draft", c.name)
		}
		x, top, _ := boxIn(t, a, id)
		before := boxText(t, a, id)
		a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 2)
		drawn := boxText(t, a, id)
		if drawn[0] == before[0] {
			t.Fatalf("%s: the wheel did not scroll the draft: %q", c.name, drawn)
		}
		line := slices.Index(strings.Split(c.draft, "\n"), drawn[0])
		left := a.layout.PaneLeft(a.regions(), a.columnOf(id)) + composerTextLeft
		got := dragThenKey(t, a, left, left+len(drawn[0])-1, top, tea.KeyMsg{Type: tea.KeyBackspace})
		want := strings.Split(c.draft, "\n")
		want[line] = ""
		if got != strings.Join(want, "\n") {
			t.Errorf("%s: ⌫ over the drawn %q deleted something else:\n%q", c.name, drawn[0], got)
		}
	}
}

// A prompt walked back with ↑ fills the room's box without a key reaching the
// composer, which leaves the composer the room holds sized for the empty box. The
// wheel reads the draft, not that size: the recalled draft scrolls.
func TestTheWheelScrollsADraftRecalledFromHistory(t *testing.T) {
	a := roomDraftApp(t, "")
	a.room.earlier = []string{tallDraft(20)}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	if got := a.composer().Value(); got != tallDraft(20) {
		t.Fatalf("↑ did not recall the tall draft: %q", got)
	}
	x, top, _ := boxIn(t, a, "")
	a = wheelAt(a, tea.MouseButtonWheelUp, x, top, 1)
	if got := boxText(t, a, ""); got[0] != "line 07" {
		t.Errorf("the wheel over a recalled draft drew from %q, want line 07", got[0])
	}
}

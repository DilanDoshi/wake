package ui

// The board scrolls: both views hold a window over a taller list, the wheel moves
// the window freely, and the arrow keys move the selection and bring it into the
// window. The tiled grid is three across at most and every tile keeps a height a
// reader can use (owner's 2026-10-12 ask).

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// minReadableTranscriptRows is the owner's "large height minimum so I can still
// read the information in each": a tile keeps at least this much transcript
// however many agents share the board.
const minReadableTranscriptRows = 15

// fleetBoard is a room over n idle agents (w00, w01, ...) with the board up at
// w x h, in tiles or in rows.
func fleetBoard(t *testing.T, n, w, h int, tiled bool) App {
	t.Helper()
	if !tiled && h > 24 {
		h = 24 // a list of one line a row needs a shorter frame than tiles to overflow
	}
	sessions := make([]rpc.SessionStatus, n)
	for i := range sessions {
		sessions[i] = rpc.SessionStatus{
			ID:    fmt.Sprintf("s%02d", i),
			Name:  fmt.Sprintf("w%02d", i),
			State: rpc.StateIdle,
			Dir:   "/repos/x",
		}
	}
	m, _ := typeAndSubmit(newRoomApp(t).withSize(w, h).withRoster(sessions...), boardVerb)
	a := m.(App)
	a.board.Tiled = tiled
	return a
}

func boardKeyPress(a App, k tea.KeyType) App {
	m, _ := a.Update(tea.KeyMsg{Type: k})
	return m.(App)
}

func boardWheel(a App, b tea.MouseButton) App {
	m, _ := a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: b, X: 10, Y: 10})
	return m.(App)
}

// cursorName is the agent the board's highlight (and so ⌃C and ↵) is on.
func cursorName(a App) string {
	ag, _ := a.fleet.Agent(a.drawnBoardCursor(a.boardAgents()))
	return ag.Name
}

func TestTilesAreThreeAcrossAtMostHoweverManyAgents(t *testing.T) {
	for _, tc := range []struct{ n, width, want int }{
		{1, 200, 1}, {2, 200, 2}, {3, 200, 3}, {4, 200, 3}, {7, 200, 3}, {30, 200, 3}, {64, 200, 3},
		{30, 70, 2}, {30, 40, 1}, // a narrow terminal still drops columns
	} {
		a := fleetBoard(t, tc.n, tc.width, 50, true)
		if got := a.boardTileLayout(a.boardAgents()).cols; got != tc.want {
			t.Errorf("%d agents at width %d drew %d columns, want %d", tc.n, tc.width, got, tc.want)
		}
	}
}

func TestATileKeepsAReadableHeightHoweverManyAgents(t *testing.T) {
	for _, n := range []int{6, 12, 30, 64} {
		a := fleetBoard(t, n, 200, 50, true)
		l := a.boardTileLayout(a.boardAgents())
		if rows := l.cellH - tileFrameRows; rows < minReadableTranscriptRows {
			t.Errorf("%d agents: a tile keeps %d transcript rows, want at least %d", n, rows, minReadableTranscriptRows)
		}
	}
	// A frame shorter than the floor gets one tile as tall as itself, never taller:
	// a tile taller than the frame would lose its status bar to the clip.
	a := fleetBoard(t, 30, 120, 16, true)
	l := a.boardTileLayout(a.boardAgents())
	if l.cellH != l.view {
		t.Errorf("a %d-row frame drew %d-row tiles, want one tile as tall as the frame", l.view, l.cellH)
	}
}

// A full screen of a few agents still fills the frame, as the screenshot does:
// the floor only bites when the fleet is bigger than a frame holds.
func TestAFewAgentsStillFillTheFrame(t *testing.T) {
	a := fleetBoard(t, 6, 200, 50, true)
	l := a.boardTileLayout(a.boardAgents())
	if got := 2 * l.cellH; got != l.view {
		t.Errorf("two rows of %d lines fill %d of the frame's %d lines", l.cellH, got, l.view)
	}
}

func TestEveryAgentCanBeScrolledToOnBothViews(t *testing.T) {
	for _, tiled := range []bool{true, false} {
		a := fleetBoard(t, 30, 200, 50, tiled)
		seen := map[string]bool{}
		note := func() {
			out := shown(a)
			for _, ag := range a.boardAgents() {
				if strings.Contains(out, ag.Name) {
					seen[ag.Name] = true
				}
			}
		}
		note()
		for range 60 {
			a = boardKeyPress(a, tea.KeyPgDown)
			if !a.board.Up {
				t.Fatalf("tiled=%v: a page key closed the board", tiled)
			}
			note()
		}
		if len(seen) != 30 {
			t.Errorf("tiled=%v: scrolling reached %d of 30 agents", tiled, len(seen))
		}
	}
}

func TestPageKeysScrollTheBoardInsteadOfClosingIt(t *testing.T) {
	for _, tiled := range []bool{true, false} {
		a := fleetBoard(t, 40, 200, 50, tiled)
		agents := a.boardAgents()
		first, last := agents[0].Name, agents[len(agents)-1].Name
		if !strings.Contains(shown(a), first) || strings.Contains(shown(a), last) {
			t.Fatalf("tiled=%v: precondition: the first frame should show the first agent and not the last", tiled)
		}

		a = boardKeyPress(a, tea.KeyEnd)
		if !a.board.Up || !strings.Contains(shown(a), last) {
			t.Errorf("tiled=%v: End did not bring the last agent into view (up=%v)", tiled, a.board.Up)
		}
		if cursorName(a) != last {
			t.Errorf("tiled=%v: End left the selection on %s, want %s", tiled, cursorName(a), last)
		}

		a = boardKeyPress(a, tea.KeyHome)
		if !a.board.Up || !strings.Contains(shown(a), first) || strings.Contains(shown(a), last) {
			t.Errorf("tiled=%v: Home did not return to the top (up=%v)", tiled, a.board.Up)
		}
		if cursorName(a) != first {
			t.Errorf("tiled=%v: Home left the selection on %s, want %s", tiled, cursorName(a), first)
		}

		a = boardKeyPress(a, tea.KeyPgDown)
		if !a.board.Up {
			t.Fatalf("tiled=%v: PgDn closed the board", tiled)
		}
		down := shown(a)
		a = boardKeyPress(a, tea.KeyPgUp)
		if !a.board.Up || !strings.Contains(shown(a), first) {
			t.Errorf("tiled=%v: PgUp did not scroll back to the first agent (up=%v)", tiled, a.board.Up)
		}
		if down == shown(a) {
			t.Errorf("tiled=%v: PgDn and PgUp drew the same frame, so one of them scrolled nothing", tiled)
		}
	}
}

// The wheel scrolls the view and leaves the selection where it is for as long as
// the selection is still mostly on screen: three lines of a 23-line tile do not
// take the highlight off it.
func TestTheWheelScrollsWithoutMovingASelectionThatIsStillInView(t *testing.T) {
	a := fleetBoard(t, 30, 200, 50, true)
	first := cursorName(a)
	before := shown(a)
	a = boardWheel(a, tea.MouseButtonWheelDown)
	if got := cursorName(a); got != first {
		t.Errorf("one wheel notch moved the selection from %s to %s", first, got)
	}
	if shown(a) == before {
		t.Error("one wheel notch scrolled nothing")
	}
}

// A selection the wheel scrolls out of the frame comes along: ⌃C parks and ↵
// opens the highlighted agent, so the highlight is never one nobody can see.
func TestTheWheelDragsTheSelectionAlongOnceItLeavesTheView(t *testing.T) {
	for _, tiled := range []bool{true, false} {
		a := fleetBoard(t, 40, 200, 50, tiled)
		start := cursorName(a)
		for range 12 {
			a = boardWheel(a, tea.MouseButtonWheelDown)
		}
		got := cursorName(a)
		if got == start {
			t.Errorf("tiled=%v: the selection stayed on %s after the view scrolled past it", tiled, start)
		}
		if !strings.Contains(shown(a), got) {
			t.Errorf("tiled=%v: the selection %s is not on screen:\n%s", tiled, got, shown(a))
		}
	}
}

func TestArrowKeysBringTheSelectionIntoView(t *testing.T) {
	for _, tiled := range []bool{true, false} {
		a := fleetBoard(t, 40, 200, 50, tiled)
		for range 35 {
			a = boardKeyPress(a, tea.KeyDown)
			if got := cursorName(a); !strings.Contains(shown(a), got) {
				t.Fatalf("tiled=%v: the selection %s walked off screen:\n%s", tiled, got, shown(a))
			}
		}
		for range 35 {
			a = boardKeyPress(a, tea.KeyUp)
			if got := cursorName(a); !strings.Contains(shown(a), got) {
				t.Fatalf("tiled=%v: the selection %s walked off screen going up:\n%s", tiled, got, shown(a))
			}
		}
	}
}

// A click lands on the tile it is drawn over, whatever the scroll: the hit walks
// the same offset the draw used.
func TestAClickAfterScrollingOpensTheTileUnderThePointer(t *testing.T) {
	a := fleetBoard(t, 12, 200, 50, true)
	for range 5 {
		a = boardWheel(a, tea.MouseButtonWheelDown)
	}
	clicked := 0
	for y, ln := range strings.Split(shown(a), "\n") {
		if !strings.HasPrefix(ln, "╭") {
			continue
		}
		for _, ag := range a.boardAgents() {
			i := strings.Index(ln, ag.Name)
			if i < 0 {
				continue
			}
			x := ansi.StringWidth(ln[:i]) // the glyphs before the name are wider in bytes than in cells
			m, _ := a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y + 1})
			if got := m.(App).focus; got != ag.ID {
				t.Errorf("a click under %s's title opened %q, want %q", ag.Name, got, ag.ID)
			}
			clicked++
		}
	}
	if clicked == 0 {
		t.Fatalf("no tile title was drawn to click:\n%s", shown(a))
	}
}

// The title names where the window is once it shows less than the fleet, and says
// nothing extra while it shows all of it.
func TestTheTitleNamesTheWindowOnlyWhenItIsNotTheWholeFleet(t *testing.T) {
	for _, tiled := range []bool{true, false} {
		all := fleetBoard(t, 3, 200, 50, tiled)
		if strings.Contains(shown(all), "showing") {
			t.Errorf("tiled=%v: a fleet that fits drew a window range:\n%s", tiled, shown(all))
		}
		part := fleetBoard(t, 40, 200, 50, tiled)
		if !strings.Contains(shown(part), "showing 1–") {
			t.Errorf("tiled=%v: a window over part of the fleet did not name its range:\n%s", tiled, shown(part))
		}
	}
}

func TestClampScrollKeepsTheWindowInsideTheList(t *testing.T) {
	for _, tc := range []struct{ off, total, view, want int }{
		{-5, 100, 20, 0}, {50, 100, 20, 50}, {95, 100, 20, 80}, {10, 15, 20, 0}, {0, 0, 20, 0},
	} {
		if got := clampScroll(tc.off, tc.total, tc.view); got != tc.want {
			t.Errorf("clampScroll(%d, %d, %d) = %d, want %d", tc.off, tc.total, tc.view, got, tc.want)
		}
	}
}

func TestBandOverlapAndMostlyIn(t *testing.T) {
	b := band{top: 10, h: 20}
	for _, tc := range []struct {
		off, view, overlap int
		mostly             bool
	}{
		{0, 12, 2, false},  // two lines of twenty: a sliver
		{12, 10, 10, true}, // the window is all band
		{25, 30, 5, false}, // the band's last five lines
		{20, 30, 10, true}, // exactly half
		{19, 30, 11, true},
		{40, 10, 0, false}, // below it
		{0, 10, 0, false},  // above it: [0,10) ends where the band starts
	} {
		if got := b.overlap(tc.off, tc.view); got != tc.overlap {
			t.Errorf("overlap(%d, %d) = %d, want %d", tc.off, tc.view, got, tc.overlap)
		}
		if got := b.mostlyIn(tc.off, tc.view); got != tc.mostly {
			t.Errorf("mostlyIn(%d, %d) = %v, want %v", tc.off, tc.view, got, tc.mostly)
		}
	}
}

func TestBandRevealMovesTheWindowByTheLeastThatShowsIt(t *testing.T) {
	for _, tc := range []struct {
		name            string
		b               band
		off, view, want int
	}{
		{"already in view", band{top: 10, h: 5}, 0, 20, 0},
		{"below the window", band{top: 30, h: 20}, 0, 46, 4},
		{"above the window", band{top: 3, h: 20}, 10, 46, 3},
		{"its header comes with it", band{top: 20, h: 5, lead: 2}, 21, 46, 18},
		{"its header is not worth the row", band{top: 10, h: 46, lead: 2}, 0, 46, 10},
		{"taller than the window aligns to its top", band{top: 100, h: 60}, 0, 46, 100},
		{"header already in view", band{top: 20, h: 5, lead: 2}, 5, 46, 5},
	} {
		if got := tc.b.reveal(tc.off, tc.view); got != tc.want {
			t.Errorf("%s: reveal(%d, %d) = %d, want %d", tc.name, tc.off, tc.view, got, tc.want)
		}
	}
}

// Where a selection a scroll left behind comes to rest.
func TestLandingAfterAScroll(t *testing.T) {
	five := []band{{top: 0, h: 20}, {top: 20, h: 20}, {top: 40, h: 20}, {top: 60, h: 20}, {top: 80, h: 20}}
	tall := []band{{top: 0, h: 23}, {top: 23, h: 23}}
	for _, tc := range []struct {
		name           string
		bands          []band
		cur, off, view int
		want           []int
	}{
		{"still mostly in view stays", five, 0, 10, 46, []int{0}},
		{"a sliver left at the top moves down", five, 0, 19, 46, []int{1}},
		{"scrolled past moves down to the first in view", five, 0, 30, 46, []int{1}},
		{"scrolled up past moves up to the last in view", five, 4, 0, 46, []int{1}},
		{"nothing mostly in view takes the most shown", tall, 1, 18, 10, []int{0, 1}},
		// Clipped at the bottom edge, with its top still in the window: the nearest
		// band is the one above it, not the first of the fully shown ones.
		{"a sliver at the bottom moves up one", []band{{top: 0, h: 20}, {top: 20, h: 20}, {top: 40, h: 20}, {top: 60, h: 20}}, 3, 6, 62, []int{2}},
		{"no bands", nil, 0, 0, 46, []int{-1}},
	} {
		got := landing(tc.bands, tc.cur, tc.off, tc.view)
		ok := false
		for _, w := range tc.want {
			ok = ok || got == w
		}
		if !ok {
			t.Errorf("%s: landing = %d, want one of %v", tc.name, got, tc.want)
		}
	}
}

// The list view's lines: a header band, an agent row and its subagent rows, and
// the bands the selection can rest on line up with walkable's stops one for one.
func TestRowLayoutBandsMatchTheWalkableStops(t *testing.T) {
	agents := []Agent{{ID: "a"}, {ID: "b", Team: "T"}, {ID: "c", Team: "T"}}
	subs := func(id string) []Task {
		if id == "b" {
			return []Task{
				{Dispatch: "d1", Kind: core.TaskAgent},
				{Dispatch: "sh", Kind: core.TaskShell}, // listed, never selectable
				{Dispatch: "d2", Kind: core.TaskAgent},
			}
		}
		return nil
	}
	l := newRowLayout(agents, subs, 4, 0, Roster{Selected: "b", SelectedTask: "d2"})
	if len(l.bands) != len(l.stops) {
		t.Fatalf("%d bands for %d stops", len(l.bands), len(l.stops))
	}
	// a (line 0); T's header (1-2); b (3), d1 (4), sh (5), d2 (6); c (7), in b's section.
	want := []band{{0, 1, 0}, {3, 1, 2}, {4, 1, 0}, {6, 1, 0}, {7, 1, 0}}
	for i, b := range l.bands {
		if b != want[i] {
			t.Errorf("band %d = %+v, want %+v (stop %+v)", i, b, want[i], l.stops[i])
		}
	}
	if l.total != 8 {
		t.Errorf("total = %d, want 8", l.total)
	}
	if l.stops[l.cur] != (Roster{Selected: "b", SelectedTask: "d2"}) {
		t.Errorf("cursor on %+v, want b's d2", l.stops[l.cur])
	}
}

// The list scrolls by lines: one wheel notch takes wheelLines rows off the top, and
// the selection that was on one of them lands on the first row still shown.
func TestTheListScrollsByLinesAndTheSelectionLandsOnTheFirstShownRow(t *testing.T) {
	a := fleetBoard(t, 40, 120, 24, false)
	a = boardWheel(a, tea.MouseButtonWheelDown)
	lines := strings.Split(shown(a), "\n")
	if want := fmt.Sprintf("w%02d", wheelLines); !strings.Contains(lines[1], want) {
		t.Errorf("the first row after one notch is %q, want %s", lines[1], want)
	}
	if got, want := cursorName(a), fmt.Sprintf("w%02d", wheelLines); got != want {
		t.Errorf("the selection landed on %s, want %s", got, want)
	}
	if a.board.Scroll != wheelLines {
		t.Errorf("Scroll = %d, want %d", a.board.Scroll, wheelLines)
	}
}

// ⇥ swaps the list for the grid, which is a different list: the scroll starts over
// rather than carrying a line count between two things it does not measure.
func TestTabStartsTheOtherViewAtTheTop(t *testing.T) {
	a := fleetBoard(t, 40, 200, 50, true)
	a = boardKeyPress(a, tea.KeyPgDown)
	if a.board.Scroll == 0 {
		t.Fatal("precondition: PgDn did not scroll the grid")
	}
	if got := boardKeyPress(a, tea.KeyTab).board.Scroll; got != 0 {
		t.Errorf("Scroll = %d after ⇥, want 0", got)
	}
}

// A fleet that fits has nothing to scroll: the wheel and the page keys leave the
// window at the top, and every agent stays on screen.
func TestAFleetThatFitsDoesNotScroll(t *testing.T) {
	for _, tiled := range []bool{true, false} {
		a := fleetBoard(t, 3, 200, 50, tiled)
		for _, k := range []tea.KeyType{tea.KeyPgDown, tea.KeyPgUp, tea.KeyEnd, tea.KeyHome} {
			a = boardKeyPress(a, k)
		}
		a = boardWheel(a, tea.MouseButtonWheelDown)
		if a.board.Scroll != 0 {
			t.Errorf("tiled=%v: Scroll = %d for a fleet that fits", tiled, a.board.Scroll)
		}
		for _, name := range []string{"w00", "w01", "w02"} {
			if !strings.Contains(shown(a), name) {
				t.Errorf("tiled=%v: %s is no longer on screen:\n%s", tiled, name, shown(a))
			}
		}
	}
}

// An arrow into a tile the free scroll left half clipped shows all of it: the
// pull-back only acts on a selection wholly off screen, so the key's own scroll is
// what finishes the job.
func TestAnArrowShowsTheWholeOfTheTileItLandsOn(t *testing.T) {
	a := fleetBoard(t, 12, 200, 50, true) // four rows of 23 lines in a 46-line frame
	for range 4 {
		a = boardWheel(a, tea.MouseButtonWheelDown) // 12 lines: row 0 is mostly gone, row 2 half in
	}
	a = boardKeyPress(a, tea.KeyDown)
	l := a.boardTileLayout(a.boardAgents())
	if got, want := l.bands[l.cur].overlap(l.off, l.view), l.cellH; got != want {
		t.Errorf("the selected tile shows %d of its %d lines after ↓", got, want)
	}
}

// A scroll that lands mid-tile draws the part that is on: the frame stays the
// height it was given and starts below the tile's top edge.
func TestAFreeScrollCutsATileMidway(t *testing.T) {
	a := fleetBoard(t, 12, 200, 50, true)
	before := strings.Split(shown(a), "\n")
	a = boardWheel(a, tea.MouseButtonWheelDown)
	after := strings.Split(shown(a), "\n")
	if len(after) != len(before) {
		t.Errorf("the frame is %d lines after a scroll, was %d", len(after), len(before))
	}
	if !strings.HasPrefix(before[1], "╭") || strings.HasPrefix(after[1], "╭") {
		t.Errorf("a %d-line scroll should take the tiles' top edge off the frame: %q then %q", wheelLines, before[1], after[1])
	}
}

// The list draws a block the scroll cut through from the line it was cut at: a team
// header's blank above the divider can scroll off while the divider stays.
func TestTheListCutsATeamHeaderBandMidway(t *testing.T) {
	sessions := make([]rpc.SessionStatus, 20)
	for i := range sessions {
		sessions[i] = rpc.SessionStatus{ID: fmt.Sprintf("s%02d", i), Name: fmt.Sprintf("w%02d", i), State: rpc.StateIdle, Dir: "/repos/x"}
		if i >= 2 {
			sessions[i].Team = "backend"
		}
	}
	a := newRoomApp(t).withSize(120, 14).withRoster(sessions...)
	a = a.applyStatus(&rpc.Status{Teams: []string{"backend"}, Sessions: sessions})
	m, _ := typeAndSubmit(a, boardVerb)
	a = m.(App)
	// w00, w01, a blank, backend's divider, w02...: three lines scrolled off leave
	// the divider first.
	a = boardWheel(a, tea.MouseButtonWheelDown)
	lines := strings.Split(shown(a), "\n")
	if !strings.Contains(lines[1], "backend") {
		t.Errorf("the first line after scrolling past w00, w01 and the blank is %q, want backend's divider\n%s", lines[1], shown(a))
	}
}

// A click on the list lands on the row it is drawn over, whatever the scroll.
func TestAListClickAfterScrollingOpensTheRowUnderThePointer(t *testing.T) {
	a := fleetBoard(t, 40, 120, 24, false)
	for range 5 {
		a = boardWheel(a, tea.MouseButtonWheelDown)
	}
	name := regexp.MustCompile(`w\d\d`)
	clicked := 0
	for y, ln := range strings.Split(shown(a), "\n") {
		n := name.FindString(ln)
		if y == 0 || n == "" {
			continue
		}
		m, _ := a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 4, Y: y})
		want := "s" + n[1:]
		if got := m.(App).focus; got != want {
			t.Errorf("a click on %s's row (y=%d) opened %q, want %q", n, y, got, want)
		}
		clicked++
	}
	if clicked == 0 {
		t.Fatalf("no row was drawn to click:\n%s", shown(a))
	}
}

// End is the bottom of the list, not just the last agent's own row: the subagent
// rows running under it come into view too.
func TestEndShowsTheLastAgentsSubagentRows(t *testing.T) {
	a := fleetBoard(t, 40, 120, 24, false)
	last := a.boardAgents()[39]
	for i, label := range []string{"sub-one", "sub-two", "sub-three"} {
		ev := started(fmt.Sprintf("t%d", i), fmt.Sprintf("d%d", i), label, "general-purpose", core.TaskAgent)
		a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: last.ID, Event: &ev})
	}
	a = boardKeyPress(a, tea.KeyEnd)
	if got := strings.Count(shown(a), subGlyph); got != 3 {
		t.Errorf("End drew %d of the last agent's 3 subagent rows:\n%s", got, shown(a))
	}
}

// A last block taller than the window: End is the bottom of the list, so the
// selection goes to the last stop there, which is on screen, and a second End
// leaves it where it is. On the last agent's own row it would be pulled back to
// the top of the block and the last subagent would stay out of view.
func TestEndReachesTheLastSubagentOfABlockTallerThanTheWindow(t *testing.T) {
	a := fleetBoard(t, 3, 120, 10, false) // a six-line list
	last := a.boardAgents()[2]
	for i := range 6 {
		ev := started(fmt.Sprintf("t%d", i), fmt.Sprintf("d%d", i), "sub", "general-purpose", core.TaskAgent)
		a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: last.ID, Event: &ev})
	}
	for range 2 {
		a = boardKeyPress(a, tea.KeyEnd)
		if got := strings.Count(shown(a), subGlyph); got != 6 {
			t.Fatalf("End drew %d of the last agent's 6 subagent rows:\n%s", got, shown(a))
		}
		if a.board.SelectedTask != "d5" {
			t.Errorf("End left the selection on dispatch %q, want the last, d5", a.board.SelectedTask)
		}
	}
}

// End is the bottom of the list even below the last stop: a background shell row
// is listed but never selectable, and it still comes into view.
func TestEndShowsARunningShellBelowTheLastStop(t *testing.T) {
	a := fleetBoard(t, 40, 120, 24, false)
	last := a.boardAgents()[39]
	ev := started("t0", "d0", "tail -f", "", core.TaskShell)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: last.ID, Event: &ev})
	a = boardKeyPress(a, tea.KeyEnd)
	if got := strings.Count(shown(a), subGlyph); got != 1 {
		t.Errorf("End drew %d of the last agent's rows beneath it, want its shell row:\n%s", got, shown(a))
	}
}

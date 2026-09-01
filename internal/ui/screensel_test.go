package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// rosterApp is a room with the right sidebar drawn and two agents on it, so
// there is chrome - a roster row - to drag across.
func rosterApp(t *testing.T) App {
	t.Helper()
	a := newRoomApp(t).withAgents("alex", "sydney").withSize(200, 40)
	a.layout.ShowRoster = true
	return a.resizePanes()
}

// rosterRow is the first screen row a roster agent is drawn on.
func rosterRow(a App) (int, bool) {
	for y := 0; y < a.paneHeight(); y++ {
		if _, _, ok := a.clickedAgent(y); ok {
			return y, true
		}
	}
	return 0, false
}

// rosterLeft is a screen column inside the right sidebar.
func rosterLeft(a App) int { return a.layout.Width - a.regions().Roster + 1 }

// The reported bug: every rendered surface must be highlightable and copied on
// release, not just the transcript and the query box. A drag across a roster
// row - chrome that took no selection at all before - now selects it as screen
// cells and copies on release.
func TestADragOverChromeSelectsAndCopies(t *testing.T) {
	a := rosterApp(t)
	y, ok := rosterRow(a)
	if !ok {
		t.Fatal("no roster row on screen to drag across")
	}
	x0 := rosterLeft(a)
	got, cmd := drag(a, x0, x0+10, y)
	if !got.sel.onScreen {
		t.Errorf("sel = %+v: a drag on chrome is a screen selection", got.sel)
	}
	if got.sel.empty() {
		t.Fatal("a drag across eleven cells of the roster selected nothing")
	}
	if cmd == nil {
		t.Error("a drag over the roster produced no copy: the release copies what was highlighted")
	}
	// A screen selection is nobody's transcript selection, so no pane highlights
	// it as one - that is what keeps it off the scroll-following machinery.
	if got.selectionIn("") != (marked{}) || got.selectionIn("s1") != (marked{}) {
		t.Error("a screen selection resolved as a transcript selection in a pane")
	}
}

// A click on a roster row still opens that conversation - the roster's own
// meaning for a click - now decided on release, so a drag copies and a click
// opens.
func TestAClickOnTheRosterStillOpensTheConversation(t *testing.T) {
	a := rosterApp(t)
	y, ok := rosterRow(a)
	if !ok {
		t.Fatal("no roster row on screen to click")
	}
	agent, _, _ := a.clickedAgent(y)
	x := a.layout.Width - 2
	got, _ := a.mouse(pressAt(x, y))
	got, cmd := got.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: x, Y: y})
	if got.focus != agent.ID {
		t.Errorf("focus = %q after clicking %q's roster row: a click opens the conversation", got.focus, agent.ID)
	}
	if cmd != nil {
		t.Error("a click on the roster copied something: a press and release on one cell takes no text")
	}
}

// The copy is exactly the cells the drag crossed on that screen row, styling
// stripped and the pad trimmed - the transcript's own rule, read off the
// assembled frame instead of a pane's scrollback.
func TestTheScreenSelectionCopiesTheCellsUnderIt(t *testing.T) {
	a := rosterApp(t)
	y, ok := rosterRow(a)
	if !ok {
		t.Fatal("no roster row on screen to drag across")
	}
	x0 := rosterLeft(a)
	x1 := x0 + 12
	got, _ := drag(a, x0, x1, y)
	lines := strings.Split(got.assembleFrame(), "\n")
	want := strings.TrimRight(ansi.Strip(ansi.Cut(lines[y], x0, x1+1)), " ")
	if gotText := got.screenSelectedText(); gotText != want {
		t.Errorf("screenSelectedText = %q, want %q (columns %d..%d of row %d)", gotText, want, x0, x1, y)
	}
}

// A roster click opens the agent that was under the pointer when it went down,
// not whoever the roster reordered onto that row before the button came up. The
// roster ranks by attention on every fleet report, so resolving the target on
// release would open the wrong conversation. (Codex adversarial finding.)
func TestARosterClickOpensThePressTimeAgentNotAReorderedOne(t *testing.T) {
	a := rosterApp(t)
	y, ok := rosterRow(a)
	if !ok {
		t.Fatal("no roster row on screen to press")
	}
	pressed, _, _ := a.clickedAgent(y)
	other := map[string]string{"s1": "s2", "s2": "s1"}[pressed.ID]

	// Press resolves the target at press time.
	a, _ = a.mouse(pressAt(a.layout.Width-2, y))
	if a.rosterHit.id != pressed.ID {
		t.Fatalf("press resolved %q, want the agent under the pointer %q", a.rosterHit.id, pressed.ID)
	}

	// A fleet report reorders the roster before release: the other agent blocks
	// and rises to the top by attention (rank 0), taking the pressed row.
	oname := ""
	if ag, ok := a.fleet.Agent(other); ok {
		oname = ag.Name
	}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: pressed.ID, Name: pressed.Name, State: rpc.StateIdle},
		{ID: other, Name: oname, State: rpc.StateBlocked},
	}}})
	if now, _, ok := a.clickedAgent(y); !ok || now.ID == pressed.ID {
		t.Fatalf("the reorder did not move a different agent onto row %d (still %q): the test proves nothing", y, pressed.ID)
	}

	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: a.layout.Width - 2, Y: y})
	if a.focus != pressed.ID {
		t.Errorf("release opened %q; want the press-time agent %q despite the roster reordering under the pointer", a.focus, pressed.ID)
	}
}

// A room update mid-drag must not touch a screen selection. It has pane == ""
// like a room selection, but its point is an absolute screen row, not a room
// transcript line - so withRoom's translate/bounds reconciliation would clear
// or corrupt it. The onScreen guard excludes it. (Codex adversarial finding.)
func TestARoomUpdateLeavesAChromeSelectionAlone(t *testing.T) {
	a := rosterApp(t)
	// A live chrome selection whose row is past the end of the room transcript,
	// so without the guard withRoom's bounds check clears it.
	a.sel = selection{onScreen: true, anchor: point{line: 1000, col: 5}, head: point{line: 1000, col: 12}}
	a.selecting = true

	got := a.withRoom(a.room)
	if !got.sel.onScreen || got.sel.empty() {
		t.Fatalf("a room update cleared the chrome selection: %+v (withRoom read its screen row as a room transcript line)", got.sel)
	}
	if got.sel.anchor.line != 1000 || got.sel.head.line != 1000 {
		t.Errorf("a room update translated the chrome selection's rows against the room transcript: %+v", got.sel)
	}
}

// The awareness strip and notice row are drawn below the panes, but Layout.Hit
// classifies by column alone, so a bottom-row press at a conversation divider's
// column would arm a resize. The divider is a handle only over the panes it
// separates; below them the same column is selectable chrome. (Codex finding.)
func TestABottomRowAtADividerColumnSelectsRatherThanResizes(t *testing.T) {
	a := splitApp(t, 200, 40, 4) // room and a DM, a divider between them
	div := dividerColumnOf(a)
	if region, _ := a.layout.Hit(a.regions(), div); region != RegionDivider {
		t.Fatalf("column %d is not the divider (%v): the test proves nothing", div, region)
	}
	strip := a.paneHeight() // the awareness strip, the first row below the panes
	a = a.press(div, strip)
	if a.dragAt != noDrag {
		t.Errorf("a press on the awareness strip at the divider column armed a resize (dragAt=%d): the divider ends at the panes", a.dragAt)
	}
	if !a.sel.onScreen {
		t.Errorf("a press on bottom chrome at the divider column started no screen selection: sel=%+v", a.sel)
	}
}

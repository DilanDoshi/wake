package ui

// The board: spec §8's "one row per agent with current task and progress",
// under the owner's 2026-08-12 ruling (phase-4 scope §2c) - an OVERVIEW, not
// panes. One row per agent, no transcripts, a dashboard you act from.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// boardApp is a room over three agents in three states, with the board up.
// Attention order puts the blocked one first, which is the board's own point.
//
// alex (s1) is given one running subagent before the board opens, so the
// tiled board (Task 3) has a real count to state rather than the zero every
// tile would otherwise show.
func boardApp(t *testing.T) App {
	t.Helper()
	a := newRoomApp(t).withSize(120, 30).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateWorking, Dir: "/repos/one"},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateBlocked, Dir: "/repos/two"},
		rpc.SessionStatus{ID: "s3", Name: "robin", State: rpc.StateIdle, Dir: "/repos/three"},
	)
	sub := started("t1", "d1", "counting lines", "general-purpose", core.TaskAgent)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &sub})
	m, _ := typeAndSubmit(a, boardVerb)
	return m.(App)
}

// ⇥ is the board's own toggle between rows and tiles, claimed above the
// roster's arrows the way every other board key is.
func TestTabTogglesRowsAndTiles(t *testing.T) {
	a := boardApp(t)
	if a.board.Tiled {
		t.Fatal("the board opened in tiles; rows are the default")
	}
	next, _, handled := a.boardKey(tea.KeyMsg{Type: tea.KeyTab})
	if !handled {
		t.Fatal("⇥ was not claimed by the board")
	}
	if !next.board.Tiled {
		t.Fatal("⇥ did not switch the board to tiles")
	}
	back, _, _ := next.boardKey(tea.KeyMsg{Type: tea.KeyTab})
	if back.board.Tiled {
		t.Fatal("a second ⇥ did not switch back to rows")
	}
}

func TestSlashBoardOpensTheOverview(t *testing.T) {
	a := boardApp(t)
	if !a.board.Up {
		t.Fatal("/board did not open the board")
	}
	out := shown(a)
	for _, name := range []string{"alex", "sydney", "robin"} {
		if !strings.Contains(out, name) {
			t.Errorf("the board draws no row for %s:\n%s", name, out)
		}
	}
	for _, word := range []string{"need you", "working", "idle"} {
		if !strings.Contains(out, word) {
			t.Errorf("the board does not say a row is %q:\n%s", word, out)
		}
	}
	// The board advertises its own keys on itself, the card's rule: an
	// affordance that comes and goes belongs on the thing that came and went.
	if !strings.Contains(out, boardKeyLine) {
		t.Errorf("the board does not advertise its keys:\n%s", out)
	}
	// And it is an overview, not panes: the pane legend is not on screen.
	if strings.Contains(out, escInterruptLabel) {
		t.Errorf("the pane legend is still drawn under the board:\n%s", out)
	}
}

// The command takes no argument, managerTakesNoArgument's rule: a verb firing
// under a word it did not read does something nobody typed.
func TestSlashBoardRefusesAnArgument(t *testing.T) {
	a := newRoomApp(t).withSize(120, 30).withAgents("alex")
	m, _ := typeAndSubmit(a, boardVerb+" all")
	if m.(App).board.Up {
		t.Fatal("/board with an argument opened the board anyway")
	}
}

// ↑↓ walk the rows and ↵ opens the cursored agent's conversation, closing the
// board - the jump is the dashboard's whole verb.
func TestTheBoardCursorMovesAndEnterOpens(t *testing.T) {
	a := boardApp(t)
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.(App).Update(tea.KeyMsg{Type: tea.KeyEnter})
	next := m.(App)
	if next.board.Up {
		t.Fatal("↵ opened a conversation and left the board up over it")
	}
	// Attention order: sydney (blocked) is row 0, alex (working) is row 1.
	if next.focus != "s1" {
		t.Errorf("↵ on the second row focused %q, want s1 (alex)", next.focus)
	}
}

// The room's placement keys act on the cursored row: ⌃Y opens it in a new
// column, ⌃B stacks it below, ⌃D is ↵'s synonym. Unclaimed, all three fell
// through to the roster's pick - a different agent from the row on screen.
func TestThePlacementKeysOpenTheCursoredRow(t *testing.T) {
	for _, tc := range []struct {
		key  tea.KeyType
		name string
		in   func(App) bool
	}{
		{tea.KeyCtrlY, "⌃Y", func(a App) bool {
			last := a.grid.Cols[len(a.grid.Cols)-1]
			return len(a.grid.Cols) == 2 && last.Top == "s1"
		}},
		{tea.KeyCtrlB, "⌃B", func(a App) bool {
			return len(a.grid.Cols) == 1 && a.grid.Cols[0].Bottom == "s1"
		}},
		{tea.KeyCtrlD, "⌃D", func(a App) bool { return a.focus == "s1" }},
	} {
		a := boardApp(t)
		m, _ := a.Update(tea.KeyMsg{Type: tea.KeyDown}) // row 1 is alex, s1
		m, _ = m.(App).Update(tea.KeyMsg{Type: tc.key})
		next := m.(App)
		if next.board.Up {
			t.Errorf("%s left the board up over the conversation it opened", tc.name)
		}
		if !tc.in(next) {
			t.Errorf("%s did not place the cursored row's conversation where it says: grid %+v focus %q",
				tc.name, next.grid.Cols, next.focus)
		}
	}
}

// esc closes the board and interrupts nothing: the board is a modal the
// operator opened, pickerKey's own precedent for the key.
func TestEscClosesTheBoardWithoutInterrupting(t *testing.T) {
	a := boardApp(t)
	m, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.(App).board.Up {
		t.Fatal("esc did not close the board")
	}
	if cmd != nil {
		t.Fatal("esc on the board produced a command: the modal's close must not reach any agent")
	}
}

// Every key the board does not claim closes it and then does its own job -
// the selection rule's shape: nothing decorative may swallow a keystroke.
func TestAnUnclaimedKeyClosesTheBoardAndDoesItsJob(t *testing.T) {
	a := boardApp(t)
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	next := m.(App)
	if next.board.Up {
		t.Fatal("typing did not close the board")
	}
	if got := next.composer().Value(); got != "x" {
		t.Errorf("the rune that closed the board was swallowed: draft is %q, want %q", got, "x")
	}
}

// ⌃C parks the cursored row without closing the board - "park a few" is the
// ruling's own verb - and it goes through parkTarget, so a blocked agent is
// refused for park.go's reason rather than a second one.
func TestCtrlCParksTheCursoredRowFromTheBoard(t *testing.T) {
	a := boardApp(t)
	// Row 0 is sydney, blocked: the park must refuse, board still up.
	m, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		t.Fatal("⌃C on a blocked row wrote a frame: parking one closes stdin on an unanswered ask")
	}
	if !m.(App).board.Up {
		t.Fatal("a refused park closed the board")
	}

	m, _ = m.(App).Update(tea.KeyMsg{Type: tea.KeyDown})
	m, cmd = m.(App).Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("⌃C on a working row parked nothing")
	}
	if !m.(App).board.Up {
		t.Fatal("parking a row closed the board: 'park a few' is one press per agent, not one per open")
	}
}

// A row carries the agent's last prose line, folded from the stream, and a
// subagent's prose does not move it - the room's own exclusion.
func TestARowCarriesTheAgentsLastLine(t *testing.T) {
	a := boardApp(t)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind: core.KindAssistantText, SessionID: "s1",
		Text: "First paragraph.\n\nThe tests are green now.",
	}})
	agent, _ := a.fleet.Agent("s1")
	if agent.LastLine != "The tests are green now." {
		t.Errorf("LastLine = %q, want the last non-blank line", agent.LastLine)
	}
	if !strings.Contains(shown(a), "The tests are green now.") {
		t.Errorf("the board does not draw the agent's last line:\n%s", shown(a))
	}

	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind: core.KindAssistantText, SessionID: "s1", Subagent: &core.Subagent{},
		Text: "A subagent said this.",
	}})
	agent, _ = a.fleet.Agent("s1")
	if agent.LastLine != "The tests are green now." {
		t.Errorf("a subagent's prose moved LastLine to %q", agent.LastLine)
	}
}

// The stored line is bounded: a fleet of thirty must not hold thirty
// paragraphs, and the draw truncates to the row anyway.
func TestTheLastLineIsBounded(t *testing.T) {
	if got := lastProseLine(strings.Repeat("long ", 200)); len([]rune(got)) > lastLineCap {
		t.Errorf("lastProseLine kept %d runes, cap is %d", len([]rune(got)), lastLineCap)
	}
	if got := lastProseLine("kept\n\n  \n"); got != "kept" {
		t.Errorf("lastProseLine = %q: trailing blank lines are not skipped", got)
	}
}

// The window follows the cursor, the sidebars' own rule: a cursor that walks
// off a fixed window is a key acting on a row nobody can see.
func TestTheBoardWindowFollowsTheCursor(t *testing.T) {
	sessions := make([]rpc.SessionStatus, 20)
	for i := range sessions {
		sessions[i] = rpc.SessionStatus{
			ID:    string(rune('a' + i)),
			Name:  "w" + string(rune('a'+i)),
			State: rpc.StateIdle,
			Dir:   "/repos/x",
		}
	}
	a := newRoomApp(t).withSize(120, 12).withRoster(sessions...)
	m, _ := typeAndSubmit(a, boardVerb)
	b := m.(App)
	for range 19 {
		next, _ := b.Update(tea.KeyMsg{Type: tea.KeyDown})
		b = next.(App)
	}
	out := shown(b)
	if !strings.Contains(out, "wt") {
		t.Errorf("the cursor walked to the last row and the window did not follow:\n%s", out)
	}
	if strings.Contains(out, "wa ") {
		t.Errorf("the window holds the first row while the cursor is on the last:\n%s", out)
	}
}

// A click below the drawn rows opens nothing. Without the upper bound a click
// on the key line - or the strip or the notice row under it - resolved to a
// valid index past the window and opened an agent that was never on screen;
// only a fleet larger than the window can see it, which is why the fixture is
// twenty agents in a short terminal.
func TestAClickBelowTheDrawnRowsOpensNothing(t *testing.T) {
	sessions := make([]rpc.SessionStatus, 20)
	for i := range sessions {
		sessions[i] = rpc.SessionStatus{
			ID: string(rune('a' + i)), Name: "w" + string(rune('a'+i)), State: rpc.StateIdle,
		}
	}
	a := newRoomApp(t).withSize(120, 12).withRoster(sessions...)
	m, _ := typeAndSubmit(a, boardVerb)
	b := m.(App)
	past, _ := b.Update(tea.MouseMsg{
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		X: 4, Y: boardChromeRows + b.boardRowsVisible(),
	})
	next := past.(App)
	if !next.board.Up || next.focus != "" {
		t.Errorf("a click on the key line opened %q - an agent the window never drew", next.focus)
	}
}

// An ended selection falls back to the top row rather than stranding the
// cursor off the list - the fallback board.go documents, exercised.
func TestTheCursorSurvivesItsAgentEnding(t *testing.T) {
	a := boardApp(t)
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyDown})
	b := m.(App)
	b = b.withRoster(
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateBlocked, Dir: "/repos/two"},
		rpc.SessionStatus{ID: "s3", Name: "robin", State: rpc.StateIdle, Dir: "/repos/three"},
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateEnded, Dir: "/repos/one"},
	)
	opened, _ := b.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next := opened.(App)
	if next.focus != "s2" {
		t.Errorf("with the selected agent ended, ↵ opened %q, want the top row s2", next.focus)
	}
}

// The cursor does not wrap - the picker's rule, and until now a comment
// nothing asserted: a wrapping mutant survived the whole suite, and with one,
// ↑ on the top row lands the next ⌃C on the bottom agent.
func TestTheBoardCursorDoesNotWrap(t *testing.T) {
	a := boardApp(t)
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyUp})
	agents := m.(App).fleet.OnRoster()
	if got := m.(App).boardCursor(agents); got != 0 {
		t.Errorf("↑ on the top row moved the cursor to %d: it wrapped", got)
	}
	b := m.(App)
	for range 5 {
		next, _ := b.Update(tea.KeyMsg{Type: tea.KeyDown})
		b = next.(App)
	}
	if got := b.boardCursor(agents); got != len(agents)-1 {
		t.Errorf("↓ past the bottom row moved the cursor to %d, want it held at %d", got, len(agents)-1)
	}
}

// A control byte in an agent's prose or task label may not reach the frame:
// a CR redraws the row from column zero and an ESC can move the cursor, so
// either forges a row that impersonates a fleet state - mcp.oneLine's lesson
// arriving at the board. Both the stored line and the drawn row are bounded.
func TestAgentAuthoredControlBytesCannotForgeABoardRow(t *testing.T) {
	a := boardApp(t)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind: core.KindAssistantText, SessionID: "s1",
		Text: "done\rFORGED\x1b[31mred",
	}})
	agent, _ := a.fleet.Agent("s1")
	if strings.ContainsAny(agent.LastLine, "\r\x1b") {
		t.Errorf("LastLine stored control bytes verbatim: %q", agent.LastLine)
	}
	row := boardRow(Agent{Name: "alex", State: rpc.StateWorking, Doing: "innocent\n■ need you"}, 6, 8, 80, false)
	if strings.Contains(row, "\n") {
		t.Errorf("a newline in an agent's Doing drew a second row:\n%q", row)
	}
}

// ⌃C on a parked row writes nothing. parkBoardRow used to reach parkTarget,
// which wrote a FramePark at a process that is gone and registered a wait the
// next report falsely settled as "parked" - park()'s own silent gate, skipped.
func TestCtrlCOnAParkedRowWritesNothing(t *testing.T) {
	a := newRoomApp(t).withSize(120, 30).withRoster(
		rpc.SessionStatus{ID: "p1", Name: "parky", State: rpc.StateParked},
	)
	m, _ := typeAndSubmit(a, boardVerb)
	next, cmd := m.(App).Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		t.Fatal("⌃C on a parked row produced a command: a park frame at a process that is gone")
	}
	if !next.(App).board.Up {
		t.Fatal("the refused park closed the board")
	}
}

// The strip and the notice row survive under the board: a board padded one
// row too tall was silently absorbed by firstRows cutting the notice row -
// the reserved failure row - and nothing asserted the frame's shape.
func TestTheBoardFrameKeepsTheStripAndTheNoticeRow(t *testing.T) {
	a := boardApp(t)
	lines := strings.Split(shown(a), "\n")
	if len(lines) != 30 {
		t.Fatalf("the board frame is %d rows in a 30-row terminal", len(lines))
	}
	strip := lines[len(lines)-2]
	if !strings.Contains(strip, "working") || !strings.Contains(strip, "need you") {
		t.Errorf("the second-to-last row is not the awareness strip: %q", strip)
	}
}

// A click opens the row it lands on - the roster's own meaning for a click -
// and the wheel moves the cursor. Everything else the mouse does is not the
// board's: a drag resizes panes that are not drawn.
func TestAClickOpensTheBoardRowItLandsOn(t *testing.T) {
	a := boardApp(t)
	m, _ := a.Update(tea.MouseMsg{
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 4, Y: boardChromeRows + 1,
	})
	next := m.(App)
	if next.board.Up {
		t.Fatal("a click on a row did not open it")
	}
	if next.focus != "s1" {
		t.Errorf("a click on the second row focused %q, want s1", next.focus)
	}
}

// The card's runes answer only a card that is drawn, and under the board no
// card is. 'a' is exactly the rune the unclaimed-key test above dodges: read
// by cardKey on the same keystroke that closed the board, it would arm a
// settle - confirmed by the next bare ↵ - on an ask nobody had on screen.
// The rune closes the board and lands in the draft like any other.
func TestAKeyThatClosesTheBoardCannotArmTheHiddenCard(t *testing.T) {
	// sydney's conversation is open, because that is the only surface her card
	// is drawn on - a room-focused App has no card in reach and the
	// counterfactual below would be green for the wrong reason.
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateWorking, Dir: "/repos/one"},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateBlocked, Dir: "/repos/two"},
	).openDMWith("s2", "sydney").opened(t).applyFrame(askOn("s2", "r1"))
	m, _ := typeAndSubmit(a, boardVerb)
	a = m.(App)
	if !a.board.Up {
		t.Fatal("precondition: the board is not up")
	}

	// The counterfactual first: the same state with the board closed arms on
	// this rune, or the assertion below is green with no card in reach.
	closed, _ := press(a.closeBoard(), cardAllowKey)
	if probe, ok := closed.cards.For("s2"); !ok {
		t.Fatal("precondition: no card for s2")
	} else if _, armed := closed.cards.armedKey(probe); !armed {
		t.Fatal("precondition: 'a' does not arm this card even without the board, so the board changes nothing here")
	}

	next, _ := press(a, cardAllowKey)
	if next.board.Up {
		t.Fatal("the rune did not close the board")
	}
	card, ok := next.cards.For("s2")
	if !ok {
		t.Fatal("precondition: the ask is gone")
	}
	if _, armed := next.cards.armedKey(card); armed {
		t.Error("the rune armed a settle on a card the board was hiding")
	}
	if got := next.composer().Value(); got != string(cardAllowKey) {
		t.Errorf("the rune that closed the board was swallowed: draft is %q, want %q", got, string(cardAllowKey))
	}
}

package ui

// The DM open beside the room: what opening one costs the room, what closing
// one keeps, and where a keystroke goes when there are two composers.
//
// These are the brief's own guards for this task. The behaviour under most of
// them landed with the room in Task 10 - openDMWith, closeDM, the focus and the
// per-pane sizing are all already there - so this file is a regression net over
// work that exists rather than a specification of work that does not. It is
// worth having anyway: nothing else asserts the *layout* consequences of
// opening a pane (the room surviving, the right sidebar staying, the takeover
// and the way back from it), and every one of those is a responsive rule that a
// later change to Layout could quietly break.
//
// Three of the brief's six live elsewhere and are named here so this file is
// not read as the whole set: which composer is accented and where a keystroke
// lands are in focus_test.go beside the rest of the two tab keys, and closing
// and reopening through the keys themselves is TestCtrlWClosesTheConversationAndKeepsIt.
//
// The sidebar rule and the takeover are already table-tested in layout_test.go,
// and these do not replace those. Layout is a pure value and those tests drive
// it directly; these drive the App - a real open, a real resize through the
// settle, and Regions read back off the model - so what they add is the wiring
// between the two, which is where a responsive rule that is right in the pure
// function still reaches the screen wrong.

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
)

// Opening a DM from the room is one keystroke and must lose neither the room
// nor the right sidebar: the room beside you is what does the awareness job,
// which is why no awareness strip is needed, and the sidebar is where the next
// agent is picked from - closing it under a selection makes picking two agents
// in a row two keystrokes plus a ⌃R nobody asked for.
func TestOpeningADMKeepsTheRoomAndTheRightSidebar(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex")
	a.roster.Selected = "s1"
	a = a.openDM()

	r := a.regions()
	if r.Room() == 0 {
		t.Error("opening a DM lost the room. Opening a DM from the room must be one keystroke and must not lose the room - the room beside you is what does the awareness job, which is why no awareness strip is needed")
	}
	if r.Roster == 0 {
		t.Error("opening a DM closed the right sidebar. Picking a row is a request to read that conversation, not a request to put the fleet away - only ⌃R closes the sidebar")
	}
	if r.Cols[1] < minPaneWidth {
		t.Errorf("the DM opened at %d columns", r.Cols[1])
	}
}

// The sidebar is the operator's, in both directions: ⌃R is the only thing that
// closes it and the only thing that reopens it. A layout that moved it on its
// own had to guess which of the two an operator meant, and guessed wrong in
// whichever direction it was not currently guessing.
func TestOnlyTheKeyMovesTheRightSidebar(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex")
	a.roster.Selected = "s1"

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlR})
	if a.regions().Roster != 0 {
		t.Fatal("⌃R did not close the right sidebar")
	}
	if a = a.openDM(); a.regions().Roster != 0 {
		t.Error("opening a conversation reopened a sidebar the operator had closed")
	}
	if a = a.closeDM(); a.regions().Roster != 0 {
		t.Error("closing the last conversation opened a sidebar the operator had closed")
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlR})
	if a.regions().Roster == 0 {
		t.Error("⌃R did not reopen the right sidebar")
	}
}

// An hour inside one conversation must not cost you what accumulated in the
// others, and opening one is what reads it.
func TestOpeningADMReadsWhatHadAccumulatedForThatAgent(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a = a.applyFrame(kindFrame("s1", core.KindAssistantText, "one"))
	a = a.applyFrame(kindFrame("s1", core.KindAssistantText, "two"))
	if got, _ := a.fleet.Agent("s1"); got.Unread != 2 {
		t.Fatalf("unread = %d before opening, want 2", got.Unread)
	}
	a.roster.Selected = "s1"
	a = a.openDM()
	if got, _ := a.fleet.Agent("s1"); got.Unread != 0 {
		t.Errorf("unread = %d after opening its DM, want 0", got.Unread)
	}
}

// Wake keeps no replay buffer by design and claude's own transcript is not
// reachable from here, so a dropped DM is a conversation gone.
//
// The event is a thinking block on purpose: the room filters those out, so what
// comes back has to be the DM's own unfiltered transcript rather than anything
// reconstructed from what the room kept.
func TestClosingADMKeepsItsTranscriptForWhenItComesBack(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a.roster.Selected = "s1"
	a = a.openDM()
	a = a.applyFrame(kindFrame("s1", core.KindThinking, "considering the token refresh"))
	if strings.Count(shown(a), "token refresh") != 1 {
		t.Fatalf("the DM did not draw a thinking block, so closing it can lose nothing:\n%s", shown(a))
	}

	a = a.closeDM()
	if strings.Contains(shown(a), "token refresh") {
		t.Fatalf("the room drew the thinking block too, so this test cannot tell a kept transcript from a rebuilt one:\n%s", shown(a))
	}
	a = a.openDM()
	if !strings.Contains(shown(a), "token refresh") {
		t.Error("closing and reopening a DM lost its transcript. Wake keeps no replay buffer by design and claude's own transcript is not reachable from here, so a dropped DM is a conversation gone")
	}
}

// Both panes are sized where the layout is applied, and nowhere else.
//
// This is the guard whose failure mode is a slow app rather than a wrong one,
// which is the one nobody notices. Room.View and DM.View re-lay for a size they
// were not given, so a pane that is never told its width silently re-runs its
// whole transcript through glamour on *every frame* - behind internal/render's
// one process-global mutex, which at two panes is the measured 4.5x draw-path
// win handed straight back. Bubble Tea draws on the cursor blink, so "every
// frame" means twice a second forever with nothing happening.
//
// A benchmark measures the same property and cannot enforce it: `make test`
// runs no benchmarks, and a benchmark reports a number rather than failing.
//
// Mutation check: making App.resizePanes a no-op fails this at "a steady frame
// re-wrapped the room 5 times and the DM 5 times", and takes
// BenchmarkViewTwoPanes from 303µs to 122ms a frame - 404x, and every one of
// those re-wraps holds internal/render's process-global mutex while it runs.
// Both benchmark geometries, so the claim the benchmark header makes about "no
// re-wrapping" is held by this test at the widths it is made about rather than
// at one of them. The event count is not part of the property - View re-lays on
// a width or height that differs from the one it was sized for, which does not
// know how long the conversation is - so twenty is enough and three thousand
// would only make the test slow.
func TestASteadyFrameReWrapsNeitherPane(t *testing.T) {
	for _, width := range []int{200, 201} {
		a := newRoomApp(t).withSize(width, 40).withAgents("sydney")
		a = a.openDMWith("s1", "sydney")
		for i := range 20 {
			a = said(a, "s1", fmt.Sprintf("turn %d. %s", i, wrappingSentence))
		}
		if a.regions().Cols[1] == 0 || a.regions().Room() == 0 {
			t.Fatalf("at %d columns only one pane is drawn, so this measures half of what it claims", width)
		}

		const frames = 20
		room, dm := countPaneRenders(t, func() {
			for range frames {
				_ = a.View()
			}
		})
		if room != 0 || dm != 0 {
			t.Errorf("at %d columns a steady frame re-wrapped the room %d times and the DM %d times over %d draws, want 0. A pane that is not told its width re-runs its whole transcript through glamour on every frame, behind one process-global mutex shared by the whole fleet", width, room, dm, frames)
		}
	}
}

// A split frame measures exactly the terminal, at every width a drag crosses
// and while the drag is still in flight.
//
// This is what "nothing flickers" means mechanically, and it is the half of the
// brief's Step 8 that a test can hold: a frame one row too tall or one column
// too wide wraps and scrolls the alt screen on *every draw*, which is the same
// failure the notice row's one-row rule exists for - now with two panes, a
// divider column between them and a takeover in the middle of the range, any of
// which can be off by one.
//
// Both halves are asserted because they fail differently. Settled, the frame
// must be exactly the terminal; mid-drag it is laid out for the width it has
// not been re-wrapped for yet, so it must be clipped rather than exact.
func TestASplitFrameMeasuresTheTerminalAtEveryWidthADragCrosses(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a.roster.Selected = "s1"
	a = a.openDM()
	for i := range 30 {
		a = said(a, "s1", fmt.Sprintf("turn %d. %s", i, wrappingSentence))
	}

	crossed := false
	// From 195 rather than 200. At the width the model already has, changeGeometry
	// takes the applyGeometry branch and clipMidDrag is a no-op - so the "mid-drag"
	// assertion there is made about a settled frame and its message would name a
	// state the test was not in.
	for w := 195; w >= 80; w -= 5 {
		mid := a
		mid, _ = mid.resized(w, 40)
		if got := widest(mid.View()); got > w {
			t.Fatalf("mid-drag at %d columns the frame is %d wide: it wraps every row and scrolls the alt screen on every draw", w, got)
		}

		a, _ = a.resized(w, 40)
		a = settle(a)
		if got := widest(a.View()); got != w {
			t.Errorf("at %d columns the settled frame is %d wide", w, got)
		}
		if got := lipgloss.Height(a.View()); got != 40 {
			t.Errorf("at %d columns the frame is %d rows, want the 40 the terminal reported", w, got)
		}
		if a.regions().Room() == 0 {
			crossed = true
		}
	}
	if !crossed {
		t.Error("the sweep never reached the takeover, so it measured one arrangement rather than both")
	}
}

// And the same for the drag Step 8 actually names first: the divider.
//
// The sweep above moves the *terminal*. This moves the column between the two
// panes, which is the one this task adds to the frame - and the cost half of a
// divider drag is already held elsewhere (one re-wrap per drag, one settle
// shared with the window drag). What nothing held is that the frame still
// measures the terminal exactly while the divider moves and after it stops,
// which is where a clamp in Layout.DragDivider that stopped Regions summing to
// Width would show up: as an alt screen scrolling on every draw.
func TestADividerDragLeavesTheFrameMeasuringTheTerminal(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a.roster.Selected = "s1"
	a = a.openDM()
	for i := range 30 {
		a = said(a, "s1", fmt.Sprintf("turn %d. %s", i, wrappingSentence))
	}

	start := dividerColumnOf(a)
	a = grab(t, a, start)
	for x := start; x > start-40; x-- {
		a = dragTo(a, x)
		if got := widest(a.View()); got != 200 {
			t.Fatalf("with the divider at column %d the frame is %d columns wide, want 200", x, got)
		}
		if got := lipgloss.Height(a.View()); got != 40 {
			t.Fatalf("with the divider at column %d the frame is %d rows, want 40", x, got)
		}
	}
	a = settle(a)

	if dividerColumnOf(a) == start {
		t.Fatalf("the divider is still at column %d after a 40-column drag, so nothing above was measuring a moved divider", start)
	}
	if got := widest(a.View()); got != 200 {
		t.Errorf("after the drag settled the frame is %d columns wide, want 200", got)
	}
	if got := lipgloss.Height(a.View()); got != 40 {
		t.Errorf("after the drag settled the frame is %d rows, want 40", got)
	}
}

// A takeover is a responsive rule, not a state change: below 120 columns a
// split leaves 42 columns a pane, so the DM takes it - and the room comes back
// the moment there is room for it, without anything being reopened.
func TestBelowOneHundredAndTwentyColumnsTheDMTakesThePaneAndTheRoomComesBackAbove(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a.roster.Selected = "s1"
	a = a.openDM()

	a, _ = a.resized(110, 40)
	a = settle(a)
	if a.regions().Room() != 0 {
		t.Error("at 110 columns the room and the DM were still splitting a pane 42 columns wide")
	}
	if a.focus != "s1" {
		t.Errorf("the takeover closed the conversation (open=%q): it is a width rule, and the DM is what keeps the pane", a.focus)
	}

	a, _ = a.resized(200, 40)
	a = settle(a)
	if a.regions().Room() == 0 {
		t.Error("widening the terminal did not bring the room back: a takeover is a responsive rule, not a state change")
	}
}

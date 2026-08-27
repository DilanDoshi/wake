package ui

// The awareness strip: the fleet in one row.

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func inState(state string, n int) []Agent {
	out := make([]Agent, n)
	for i := range out {
		out[i] = Agent{ID: fmt.Sprintf("%s%d", state, i), Name: fmt.Sprintf("a%d", i), State: state}
	}
	return out
}

// Every state the roster can draw is a state the strip counts.
//
// Derived from stateGlyph rather than from a list, so a seventh state is a build
// failure here rather than a fleet silently under-reported by the one row that
// claims to be the whole fleet.
func TestEveryStateTheRosterDrawsIsCountedInTheStrip(t *testing.T) {
	for state := range stateGlyph {
		if _, ok := stateLabel[state]; !ok {
			t.Errorf("the roster draws %q and the strip has no word for it: a count with no word reads as a fleet with a hole in it", state)
		}
		out := stripANSI(awarenessStrip(inState(state, 2), "", 200))
		if !strings.Contains(out, "2 "+stateLabel[state]) {
			t.Errorf("two agents in %q are not counted: %q", state, out)
		}
	}
	for state := range stateLabel {
		if _, ok := stateGlyph[state]; !ok {
			t.Errorf("the strip has a word for %q and the roster draws no such state: a label for a state nobody can be in is a row nobody can check", state)
		}
	}
}

// The thing worth stopping for is leftmost.
//
// Through the roster's own ranking, so a glance at the strip and a glance at the
// sidebar cannot disagree about what matters first.
func TestTheStripPutsWhatNeedsYouFirst(t *testing.T) {
	agents := append(inState(rpc.StateIdle, 3), inState(rpc.StateWorking, 17)...)
	agents = append(agents, inState(rpc.StateBlocked, 2)...)

	out := stripANSI(awarenessStrip(agents, "pufferfish", 200))
	need := strings.Index(out, "need you")
	working := strings.Index(out, "working")
	idle := strings.Index(out, "idle")
	switch {
	case need < 0 || working < 0 || idle < 0:
		t.Fatalf("a state is missing from %q", out)
	case need >= working || working >= idle:
		t.Errorf("the strip reads %q: blocked, working, idle is the roster's order and the strip has to agree with it", out)
	}
	if !strings.HasPrefix(out, " #pufferfish") {
		t.Errorf("the strip does not open with the workspace: %q", out)
	}
}

// A state this build does not know cannot add a row either.
//
// labelOf falls back to the state itself, which arrives off the wire - so the
// one-line promise has to cover it. The producer is Wake's own daemon, which is
// why this is defence in depth rather than a live path.
func TestAnUnknownStateCannotAddARowToTheStrip(t *testing.T) {
	agents := []Agent{{ID: "s1", Name: "a0", State: "working\nsurprise"}}
	if got := lipgloss.Height(awarenessStrip(agents, "", 100)); got != 1 {
		t.Errorf("an unknown state carrying a newline made the strip %d rows", got)
	}
}

// The counts are budgeted before the name is cut.
//
// A row truncated from the right cuts through the numbers it exists to show,
// and `◐ 1` where seventeen are working is not information missing - it is a
// plausible wrong number, at exactly the widths this exists for. Roster.headLine
// states the same rule for the unread badge.
func TestTheCountsSurviveAWidthTheNameDoesNot(t *testing.T) {
	agents := append(inState(rpc.StateBlocked, 2), inState(rpc.StateWorking, 17)...)
	agents = append(agents, inState(rpc.StateIdle, 3)...)

	for _, width := range []int{40, 50, 60, 80} {
		out := stripANSI(awarenessStrip(agents, "some-very-long-repository-name-indeed", width))
		for _, want := range []string{"2 need you", "17 working", "3 idle"} {
			if !strings.Contains(out, want) {
				t.Errorf("width %d: %q is missing from %q - the name was budgeted before the counts", width, want, out)
			}
		}
	}
}

// The order is total, so segments cannot swap between frames.
//
// sort.Slice is unstable and map iteration is random, so two states this build
// does not know - which rank equal - would trade places from one draw to the
// next without the tie-break.
func TestTheOrderIsStableAcrossFrames(t *testing.T) {
	agents := append(inState("mystery-a", 1), inState("mystery-b", 1)...)
	first := stripANSI(awarenessStrip(agents, "", 200))
	for range 20 {
		if got := stripANSI(awarenessStrip(agents, "", 200)); got != first {
			t.Fatalf("the strip drew %q then %q: two states that rank equal have to have an order", first, got)
		}
	}
}

// A state nobody is in is left out.
//
// "0 need you · 0 silent · 0 ended" is four words of chrome around the one
// number that matters.
func TestTheStripLeavesOutStatesNobodyIsIn(t *testing.T) {
	out := stripANSI(awarenessStrip(inState(rpc.StateWorking, 4), "", 200))
	if !strings.Contains(out, "4 working") {
		t.Fatalf("the strip does not count the fleet: %q", out)
	}
	for _, gone := range []string{"need you", "idle", "parked", "ended"} {
		if strings.Contains(out, gone) {
			t.Errorf("the strip says %q about a state nobody is in: %q", gone, out)
		}
	}
}

// An empty fleet says so rather than drawing an empty row.
//
// It is a real state: bare `wake` on a machine with nothing running draws one
// before its first agent answers.
func TestAnEmptyFleetSaysSo(t *testing.T) {
	if out := stripANSI(awarenessStrip(nil, "", 200)); !strings.Contains(out, noFleet) {
		t.Errorf("an empty fleet drew %q, want %q", out, noFleet)
	}
}

// The strip is one row and never wider than the frame.
//
// One row means one row: App.View joins it above the notice row, and a strip
// that wrapped would make the frame taller than the terminal, which scrolls the
// alt screen on every draw.
func TestTheStripIsOneRowAndFitsTheFrame(t *testing.T) {
	agents := append(inState(rpc.StateWorking, 17), inState(rpc.StateBlocked, 2)...)
	agents = append(agents, inState(rpc.StateIdle, 3)...)
	agents = append(agents, inState(rpc.StateParked, 8)...)

	for _, width := range []int{20, 40, 80, 120, 200} {
		out := awarenessStrip(agents, "a-very-long-workspace-name-indeed", width)
		if got := strings.Count(out, "\n"); got != 0 {
			t.Errorf("width %d: the strip is %d rows", width, got+1)
		}
		if got := lipgloss.Width(out); got > width {
			t.Errorf("width %d: the strip measured %d columns: %q", width, got, stripANSI(out))
		}
	}
}

// The workspace is the one the open conversation is in, and nothing in the
// room.
//
// Nothing rather than a guess: the room is not one workspace, and naming
// whichever agent ranks first would be a status bar asserting something no
// surface below it believes.
func TestTheStripNamesTheWorkspaceOfTheOpenConversationAndNothingInTheRoom(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex")
	a.fleet = a.fleet.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", Dir: "/repos/pufferfish", State: rpc.StateIdle},
	}})

	if got := a.stripWorkspace(); got != "pufferfish" {
		t.Errorf("a conversation in /repos/pufferfish named workspace %q", got)
	}
	// The keys on the room **while the conversation is still open beside it**,
	// which is the state that discriminates: at 120 columns and up both panes
	// are drawn, so the grid holds it and only the focus says which one you are in.
	// Asking showRoom() instead proves nothing - below the takeover it closes
	// the conversation, so the lookup fails for a reason that is not this rule.
	// An agent the fleet knows about but has no directory for - the state right
	// after `wake attach`, where Fleet.Observe builds a row from an event and
	// the first report has not arrived. `#(unknown)` is not a workspace.
	noDir := dmApp(nil, Stream{}, "s1", "alex")
	noDir.fleet = noDir.fleet.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
	}})
	if _, known := noDir.fleet.Agent("s1"); !known {
		t.Fatal("the fleet does not know this agent, so the check below passes for the wrong reason")
	}
	if got := noDir.stripWorkspace(); got != "" {
		t.Errorf("an agent with no directory named workspace %q", got)
	}
	if got := a.withFocus("").stripWorkspace(); got != "" {
		t.Errorf("the room named workspace %q while a conversation was open beside it: the room is not one workspace", got)
	}
	if !a.grid.Has("s1") {
		t.Fatal("the conversation is not open, so the check above passes for the wrong reason")
	}
}

// The frame measures the terminal at every height, including the ones below
// the panes' own floor.
//
// A review found the hole: at height 6 the panes get 4 rows, DM.View floors
// itself to 5, and the strip and notice row are appended after - a 7-row frame
// in a 6-row terminal, which scrolls the alt screen on every draw. It rendered
// exactly before the strip took a row, so this is the change's own defect.
func TestTheFrameMeasuresTheTerminalAtEveryHeight(t *testing.T) {
	fresh(t)
	// From zero, not from the floor: lipgloss reads a non-positive maximum as
	// *no* maximum, so the first cut was a no-op at exactly the heights it was
	// written for, and the table started at 4 where it says "every height".
	for _, height := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 24, 40} {
		for _, width := range []int{80, 140, 200} {
			a := dmApp(nil, Stream{}, "s1", "alex")
			m, _ := a.Update(tea.WindowSizeMsg{Width: width, Height: height})
			out := m.View()
			// An empty string is one row to lipgloss and a zero-row terminal is
			// not a thing, so one is the floor rather than a special case.
			if want, got := max(height, 1), lipgloss.Height(out); got != want {
				t.Errorf("%dx%d drew %d rows, want %d:\n%s", width, height, got, want, stripANSI(out))
			}
			if got := lipgloss.Width(out); got > width {
				t.Errorf("%dx%d drew %d columns", width, height, got)
			}
		}
	}
}

// A workspace name carrying a newline cannot add a row.
//
// A directory name is text Wake did not write and unix admits a newline in one,
// so this is a fleet report making the frame taller than the terminal - on
// every draw, for as long as that conversation is open.
func TestAWorkspaceNameCannotAddARowToTheFrame(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex")
	a.fleet = a.fleet.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", Dir: "/repos/one\ntwo\rthree\x1b[2J", State: rpc.StateIdle},
	}})

	m, _ := a.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	out := m.View()
	if got := lipgloss.Height(out); got != 24 {
		t.Errorf("a workspace name with a newline made the frame %d rows:\n%s", got, stripANSI(out))
	}
	// On the row rather than on the accessor: the one-line promise belongs to
	// the function that makes it, which is why the containment moved there and
	// now covers labelOf's unknown-state fallback too.
	strip := awarenessStrip(a.fleet.OnRoster(), a.stripWorkspace(), 100)
	if got := lipgloss.Height(strip); got != 1 {
		t.Errorf("the strip is %d rows with a newline in the workspace name: %q", got, stripANSI(strip))
	}
}

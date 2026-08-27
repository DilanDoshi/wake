package ui

// The two keys that place a conversation, and the one that takes it away.
//
// ⇧↵ and ⌃⇧↵ are what was asked for and are not bindable: bubbletea v1.3.10
// names neither, in either encoding, and a terminal with no keyboard protocol
// sends ⇧↵ as the byte it sends for ↵ - which is send. The probe is in
// keyprobe_test.go and is a guard rather than a note, so the day that changes is
// a failure with the table in front of whoever reads it.
//
// ⌃⇧→ and ⌃⇧↓ were the second answer and were worse: bindable, probed, and
// never delivered, because macOS spends all four ctrl+shift+arrows on spaces
// and Mission Control. ⌃Y and ⌃B are single bytes, which no window server can
// take.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
)

// gridApp is a room with three agents on the roster and nothing open.
func gridApp(t *testing.T) App {
	t.Helper()
	return newRoomApp(t).withSize(240, 40).withAgents("sydney", "john", "marcus")
}

// pick puts the roster cursor on an agent, which is what the open keys read.
func pick(a App, id string) App {
	a.layout.ShowRoster = true
	a.roster.Selected = id
	return a
}

func TestCtrlYOpensTheSelectedAgentInANewColumn(t *testing.T) {
	a := pick(gridApp(t), "s1")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlY})
	if got := panesOf(a.grid); len(got) != 2 || got[1] != "s1" {
		t.Fatalf("⌃Y gave %v, want a new column holding s1 beside the room", got)
	}
	if a.focus != "s1" {
		t.Errorf("⌃Y left the keys on %q, want the conversation it just opened", a.focus)
	}

	// And a second one lands beside the first rather than replacing it, which is
	// the whole difference from ⌃D.
	a, _ = pressKey(pick(a, "s2"), tea.KeyMsg{Type: tea.KeyCtrlY})
	if got := panesOf(a.grid); len(got) != 3 || got[2] != "s2" {
		t.Fatalf("a second ⌃Y gave %v, want s2 in a third column", got)
	}
}

func TestCtrlBOpensTheSelectedAgentBelowTheFocusedPane(t *testing.T) {
	a := pick(gridApp(t), "s1")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlY})
	a, _ = pressKey(pick(a, "s2"), tea.KeyMsg{Type: tea.KeyCtrlB})

	if got := panesOf(a.grid); len(got) != 2 || got[1] != "s1/s2" {
		t.Fatalf("⌃B gave %v, want s2 stacked under s1 in one column", got)
	}
	if a.focus != "s2" {
		t.Errorf("⌃B left the keys on %q, want the conversation it just opened", a.focus)
	}
}

// From the room, which is the case somebody reaches for first and the one a
// grid keyed on "the column I am in" could most easily get wrong.
func TestCtrlBFromTheRoomStacksUnderTheRoom(t *testing.T) {
	a := pick(gridApp(t), "s1")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlB})
	if got := panesOf(a.grid); len(got) != 1 || got[0] != "/s1" {
		t.Fatalf("⌃B in the room gave %v, want s1 under the room", got)
	}
}

// Nothing sits under the bottom of a column, and the key says so rather than
// opening somewhere the operator did not point at.
func TestCtrlBFromALowerPaneIsRefusedAndNamesTheOtherKey(t *testing.T) {
	a := pick(gridApp(t), "s1")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlB})
	before := panesOf(a.grid)

	notice.Reset()
	a, _ = pressKey(pick(a, "s2"), tea.KeyMsg{Type: tea.KeyCtrlB})

	if got := panesOf(a.grid); len(got) != len(before) || got[0] != before[0] {
		t.Errorf("⌃B from the bottom pane changed the grid to %v, want %v", got, before)
	}
	n, ok := notice.Latest()
	if !ok || !strings.Contains(n.String(), "⌃Y") {
		t.Errorf("the refusal was %q, want it to name ⌃Y: a key that declines has to say what to press instead", n)
	}
}

// ⌃D is unchanged and still opens *here*, which is what keeps a single-pane
// workflow exactly what it was.
func TestCtrlDStillReplacesTheFocusedPaneRatherThanAddingAColumn(t *testing.T) {
	a := pick(gridApp(t), "s1")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	a, _ = pressKey(pick(a, "s2"), tea.KeyMsg{Type: tea.KeyCtrlD})

	if got := panesOf(a.grid); len(got) != 2 || got[1] != "s2" {
		t.Fatalf("two ⌃Ds gave %v, want s2 to have taken s1's pane rather than opened a third column", got)
	}
}

func TestCtrlWClosesTheFocusedPaneAndLeavesTheRest(t *testing.T) {
	a := pick(gridApp(t), "s1")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlY})
	a, _ = pressKey(pick(a, "s2"), tea.KeyMsg{Type: tea.KeyCtrlY})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlW})

	if got := panesOf(a.grid); len(got) != 2 || got[1] != "s1" {
		t.Fatalf("⌃W gave %v, want only the focused conversation closed", got)
	}
	if a.focus != "s1" {
		t.Errorf("⌃W left the keys on %q, want the column that took the space", a.focus)
	}
}

// Closing the upper half of a stack promotes the lower one into the column
// rather than taking both: the pane nobody closed is still open.
func TestCtrlWOnAStackedUpperPanePromotesTheOneUnderIt(t *testing.T) {
	a := pick(gridApp(t), "s1")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlY})
	a, _ = pressKey(pick(a, "s2"), tea.KeyMsg{Type: tea.KeyCtrlB})

	// The keys are on the lower pane; ⇥ round to the upper one and close that.
	a = a.refocus("s1")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlW})

	if got := panesOf(a.grid); len(got) != 2 || got[1] != "s2" {
		t.Fatalf("⌃W on the upper pane gave %v, want s2 promoted into the column", got)
	}
	if a.focus != "s2" {
		t.Errorf("⌃W left the keys on %q, want the pane that took the space", a.focus)
	}
}

// The room is the one pane always drawn, and ⌃W in it names what the key is for
// instead of doing nothing.
func TestCtrlWInTheRoomIsRefusedAndSaysSo(t *testing.T) {
	a := pick(gridApp(t), "s1")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlY})
	a = a.refocus("")

	notice.Reset()
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlW})

	if got := panesOf(a.grid); len(got) != 2 {
		t.Errorf("⌃W in the room changed the grid to %v", got)
	}
	n, ok := notice.Latest()
	if !ok || !strings.Contains(n.String(), "room stays open") {
		t.Errorf("the refusal was %q, want it to say the room stays open", n)
	}
}

// Opening a conversation already on screen moves the keys to it rather than
// putting it in a second pane: one transcript, one composer.
func TestOpeningAConversationAlreadyOnScreenJustMovesTheKeys(t *testing.T) {
	a := pick(gridApp(t), "s1")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlY})
	a, _ = pressKey(pick(a, "s2"), tea.KeyMsg{Type: tea.KeyCtrlY})
	before := panesOf(a.grid)

	a, _ = pressKey(pick(a, "s1"), tea.KeyMsg{Type: tea.KeyCtrlY})
	if got := panesOf(a.grid); len(got) != len(before) {
		t.Errorf("⌃Y on an open conversation gave %v, want the %v it already was", got, before)
	}
}

// The frame still measures the terminal with three columns and a stack in one
// of them - the arithmetic every responsive rule rests on.
func TestTheFrameFitsTheTerminalWithSeveralColumnsAndAStack(t *testing.T) {
	a := pick(gridApp(t), "s1")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlY})
	a, _ = pressKey(pick(a, "s2"), tea.KeyMsg{Type: tea.KeyCtrlY})
	a, _ = pressKey(pick(a, "s3"), tea.KeyMsg{Type: tea.KeyCtrlB})

	frame := a.View()
	if got := widest(frame); got != 240 {
		t.Errorf("the frame is %d columns, want the 240 the terminal reported", got)
	}
	for _, line := range strings.Split(frame, "\n") {
		if w := widest(line); w != 0 && w != 240 {
			t.Fatalf("a row is %d columns rather than 240, so the frame is ragged:\n%q", w, line)
		}
	}
}

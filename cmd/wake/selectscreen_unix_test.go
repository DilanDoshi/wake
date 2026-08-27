//go:build unix

package main

// Selecting text with a real mouse on a real screen.
//
// The colour test here is the one that matters most and it looks like the least
// of them: Wake now hands Bubble Tea its own writer so it can put an OSC 52
// sequence on the terminal, and a writer that is not recognisably a file makes
// termenv decide it is not talking to a terminal. Everything keeps working and
// every style silently degrades to plain text. No assertion about *characters*
// can see that, which is every other test in this tree.

import (
	"fmt"
	"testing"

	"github.com/hinshun/vt10x"
)

// drag presses at one cell, moves across to another and releases, in the SGR
// encoding the terminal sends. 32 is the button-held motion code.
func (s *screen) drag(x0, x1, y int) {
	s.t.Helper()
	s.send(fmt.Sprintf("\x1b[<0;%d;%dM", x0+1, y+1))
	for x := x0 + 1; x <= x1; x++ {
		s.send(fmt.Sprintf("\x1b[<32;%d;%dM", x+1, y+1))
	}
	s.send(fmt.Sprintf("\x1b[<0;%d;%dm", x1+1, y+1))
}

// colouredCells counts the cells carrying a foreground or background that is
// not the terminal's default.
func (s *screen) colouredCells() int {
	s.term.Lock()
	defer s.term.Unlock()
	var n int
	for y := range s.rows {
		for x := range s.cols {
			if c := s.term.Cell(x, y); c.FG != vt10x.DefaultFG || c.BG != vt10x.DefaultBG {
				n++
			}
		}
	}
	return n
}

// The hazard named in the design doc's §9, and the whole reason this file
// exists. Wake passes tea.WithOutput so a clipboard sequence can reach the
// terminal; if that writer stops looking like a file, every colour goes.
func TestTheScreenStillHasColourThroughWakesOwnWriter(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()

	if n := s.colouredCells(); n == 0 {
		t.Fatalf("not one cell on screen carries a colour.\n"+
			"tea.WithOutput was given a writer termenv could not recognise as a terminal, "+
			"so every style in theme.go degraded to plain text with no error anywhere.\n%s", s.dump())
	}
}

// A drag puts a background on the cells it crossed and on no others.
func TestADragPutsAHighlightOnTheCellsItCrossed(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()

	row := s.rowOf("ready")
	if row < 0 {
		t.Fatalf("nothing to select on screen.\n%s", s.dump())
	}
	before := s.colouredCells()
	s.drag(0, 20, row)
	s.settle()

	if after := s.colouredCells(); after <= before {
		t.Errorf("a drag across 21 cells changed the coloured-cell count from %d to %d: "+
			"the highlight is not reaching the screen.\n%s", before, after, s.dump())
	}
}

// A click is not a drag: it moves the focus and leaves no highlight behind.
func TestAClickLeavesNoHighlight(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()

	row := s.rowOf("ready")
	before := s.colouredCells()
	s.click(10, row)
	s.settle()

	if after := s.colouredCells(); after != before {
		t.Errorf("a click changed the coloured-cell count from %d to %d: "+
			"a press and release on one cell selects nothing.\n%s", before, after, s.dump())
	}
}

// highlightedCells counts the cells carrying a background, which is what a
// selection puts on a row and what a colour alone cannot be told apart from:
// SelectionStyle strips the foreground it covers, so a highlight over
// already-coloured text leaves colouredCells unmoved.
func (s *screen) highlightedCells() int {
	s.term.Lock()
	defer s.term.Unlock()
	var n int
	for y := range s.rows {
		for x := range s.cols {
			if s.term.Cell(x, y).BG != vt10x.DefaultBG {
				n++
			}
		}
	}
	return n
}

// A drag across the query bar takes nothing: the composer is chrome under the
// transcript, not a row of the conversation. On a real screen the anchor used
// to clamp into a transcript line, so dragging across what was being typed put
// a highlight on the answer above it.
func TestADragOnTheQueryBarLeavesNoHighlight(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()

	box := s.rowOf("╭")
	if box < 0 {
		t.Fatalf("no composer on screen to drag across.\n%s", s.dump())
	}
	before := s.highlightedCells()
	s.drag(10, 30, box+1) // the draft's own row, inside the box
	s.settle()

	if after := s.highlightedCells(); after != before {
		t.Errorf("a drag across the query bar changed the highlighted-cell count from %d to %d: "+
			"the rows under a transcript are not lines of it.\n%s", before, after, s.dump())
	}
}

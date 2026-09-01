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
	"strings"
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

// colOf is the column a substring starts at on one screen row, or -1.
func (s *screen) colOf(y int, text string) int {
	if y < 0 || y >= s.rows {
		return -1
	}
	return strings.Index(s.lines()[y], text)
}

// A drag across text typed into the query box highlights exactly those cells,
// the way it does in the transcript. The box was unselectable before; now the
// words you typed can be copied back out.
func TestADragOverQueryBoxTextPutsAHighlightOnIt(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()

	s.send("hello selection")
	s.await("hello selection")
	s.settle()

	row := s.rowOf("hello selection")
	x := s.colOf(row, "hello")
	if row < 0 || x < 0 {
		t.Fatalf("the typed draft is not on screen to select.\n%s", s.dump())
	}
	// The focused composer already fills its interior with a background, so
	// counting cells that carry one cannot see the selection - it only swaps one
	// background for another. Compare each dragged cell's own background before
	// and after instead.
	bgBefore := s.rowBackgrounds(row)
	s.drag(x, x+4, row) // across "hello"
	s.settle()
	bgAfter := s.rowBackgrounds(row)

	changed := 0
	for cx := x; cx <= x+4; cx++ {
		if bgBefore[cx] != bgAfter[cx] {
			changed++
		}
	}
	if changed == 0 {
		t.Errorf("a drag over %d cells of the query box changed no cell's background: "+
			"the box's own text is not being highlighted.\n%s", 5, s.dump())
	}
}

// The reported bug: every rendered surface must be highlightable, not just the
// transcript and the query box. The awareness strip is chrome - neither a
// conversation nor a draft - and a drag across it now lands a background on the
// cells it crossed, drawn as a frame-wide overlay. See screensel.go.
func TestADragOverChromeHighlightsIt(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()

	// The strip's leading cell is the first non-space of its content (a
	// workspace or a state glyph), so a short drag from column 1 lands on text.
	// Short on purpose: a long drag is many separate mouse-escape writes, and
	// under -race the reader pools them and a read splits one mid-escape, which
	// leaks into the composer as text and a keystroke-clear wipes the selection.
	strip := s.rows - 2 // the awareness strip, above the notice row
	bgBefore := s.rowBackgrounds(strip)
	s.drag(1, 6, strip)
	s.settle()
	bgAfter := s.rowBackgrounds(strip)

	changed := 0
	for x := 1; x <= 6; x++ {
		if bgBefore[x] != bgAfter[x] {
			changed++
		}
	}
	if changed == 0 {
		t.Errorf("a drag across the awareness strip changed no cell's background: "+
			"chrome that was unselectable is not being highlighted.\n%s", s.dump())
	}
}

// rowBackgrounds is the background colour of every cell on one screen row.
func (s *screen) rowBackgrounds(y int) []vt10x.Color {
	s.term.Lock()
	defer s.term.Unlock()
	bg := make([]vt10x.Color, s.cols)
	for x := range s.cols {
		bg[x] = s.term.Cell(x, y).BG
	}
	return bg
}

// An empty query box has nothing to select: a drag across its blank interior
// takes no highlight, the same as the chrome above it. On a real screen the
// anchor used to clamp into a transcript line, so dragging across the box put a
// highlight on the answer above it; now it lands on the box and finds no text.
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

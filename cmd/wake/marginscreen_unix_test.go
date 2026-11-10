//go:build unix

package main

// The window's margins on a real screen: the frame one column in from either
// edge, and the mouse landing where the frame is drawn.

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hinshun/vt10x"

	"github.com/DilanDoshi/wake/internal/ui"
)

// margin is the blanks a screen row opens with before the frame's first column.
var margin = strings.Repeat(" ", ui.SideMargin)

// edgeCell is the cell at column x of row y, as text and background.
func (s *screen) edgeCell(x, y int) (rune, vt10x.Color) {
	s.term.Lock()
	defer s.term.Unlock()
	c := s.term.Cell(x, y)
	return c.Char, c.BG
}

// cellOf is the screen column a substring starts at on row y, or -1. Every rune
// before it on these rows is one cell wide.
func (s *screen) cellOf(y int, text string) int {
	if y < 0 || y >= s.rows {
		return -1
	}
	line := s.lines()[y]
	i := strings.Index(line, text)
	if i < 0 {
		return -1
	}
	return utf8.RuneCountInString(line[:i])
}

// The owner's ask: no text pressed against the window's left or right edge. A
// conversation beside the roster draws the composer's border, the transcript and
// the strip on the left and the roster on the right - every one of them one
// column in.
func TestNoTextTouchesTheWindowsEdges(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()

	if len(s.rosterNames()) == 0 {
		t.Fatalf("a 100-column window drew no roster: the margins cost it the sidebar.\n%s", s.dump())
	}
	for y := range s.rows {
		for _, x := range []int{0, s.cols - 1} {
			if ch, bg := s.edgeCell(x, y); (ch != ' ' && ch != 0) || bg != vt10x.DefaultBG {
				t.Fatalf("row %d draws %q in column %d, the window's edge.\n%s", y, ch, x, s.dump())
			}
		}
	}
	if x := s.cellOf(s.rowOf("╭"), "╭"); x != ui.SideMargin {
		t.Errorf("the composer's border starts in column %d, want %d: one margin in.\n%s", x, ui.SideMargin, s.dump())
	}
}

// A drag lands on the cells it was made over, margins and all: across "hello" in
// the query box exactly those five cells take the highlight, and the cells either
// side do not. A shift that is missing or doubled moves the highlight a cell.
func TestADragHighlightsTheCellsUnderThePointerInsideTheMargins(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()

	s.send("hello selection")
	s.await("hello selection")
	s.settle()

	row := s.rowOf("hello selection")
	x := s.cellOf(row, "hello")
	if row < 0 || x < 1 {
		t.Fatalf("the typed draft is not on screen to select.\n%s", s.dump())
	}
	before := s.rowBackgrounds(row)
	s.drag(x, x+4, row)
	s.settle()
	after := s.rowBackgrounds(row)

	for cx := x - 1; cx <= x+5; cx++ {
		inside := cx >= x && cx <= x+4
		if changed := before[cx] != after[cx]; changed != inside {
			t.Errorf("column %d changed background %v after a drag over columns %d..%d (want %v).\n%s", cx, changed, x, x+4, inside, s.dump())
		}
	}
}

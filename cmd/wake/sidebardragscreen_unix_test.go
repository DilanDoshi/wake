//go:build unix

package main

// A click on a sidebar agent opens it in a new column, a Shift-click opens it
// below the focused pane, and a drag places it, on a real screen: the real
// binary, the kill switch's pump, Bubble Tea's decoder and the margin all between
// the SGR report and the grid.

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// sidebarRowOf is the screen row drawing name inside the sidebar, or -1.
func sidebarRowOf(rows []string, cols int, name string) int {
	for y, ln := range rows {
		if strings.Contains(ansi.Strip(ansi.Cut(ln, cols-sidebarCols, cols)), name) {
			return y
		}
	}
	return -1
}

// sgr is one mouse report at a 0-based cell: kind 0 press, 4 press with Shift, 32
// motion with the button held, and the final byte M, or m for a release.
func sgr(kind int, final string, x, y int) string {
	return fmt.Sprintf("\x1b[<%d;%d;%d%s", kind, x+1, y+1, final)
}

// dragAgent presses a sidebar row, moves out through a few cells, and goes on to
// (x, y) without letting go.
func (s *screen) dragAgent(cols, fromY, x, y int) {
	s.t.Helper()
	fromX := cols - 4
	s.send(sgr(0, "M", fromX, fromY))
	for _, dx := range []int{3, 6, 12} {
		s.send(sgr(32, "M", fromX-dx, fromY))
	}
	s.send(sgr(32, "M", x, y))
}

// pane is a conversation's banner as drawn: the name it carries and the cell its
// logo starts at.
type pane struct {
	name string
	x, y int
}

var bannerName = regexp.MustCompile(`▀▄▄▄▄▀\s+(\S+)`)

// panesLeftOfTheSidebar is every conversation banner drawn in the columns
// beside the sidebar, the room's own (it draws no version) not among them.
func panesLeftOfTheSidebar(rows []string, cols int) []pane {
	var out []pane
	for y := 0; y+2 < len(rows); y++ {
		left := ansi.Strip(ansi.Cut(rows[y], 0, cols-sidebarCols))
		at := strings.Index(left, "▄▀▀▀▀▄   Wake  v") // the logo's first row, then the version
		if at < 0 {
			continue
		}
		name := bannerName.FindStringSubmatch(ansi.Strip(ansi.Cut(rows[y+2], 0, cols-sidebarCols)))
		if name != nil {
			out = append(out, pane{name: name[1], x: ansi.StringWidth(left[:at]), y: y})
		}
	}
	return out
}

func paneNamed(rows []string, cols int, name string) (pane, bool) {
	for _, p := range panesLeftOfTheSidebar(rows, cols) {
		if p.name == name {
			return p, true
		}
	}
	return pane{}, false
}

func TestAClickOnASidebarAgentOpensItAndADragPlacesIt(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	const cols, rows = 200, 40
	s := startWakeInAConversation(t, cols, rows)
	fleetOfNine(t, s) // the room alone, the nine agents on the roster
	frame := s.settle()
	if got := panesLeftOfTheSidebar(frame, cols); len(got) != 0 {
		t.Fatalf("panes %v are open before anything is clicked.\n%s", got, s.dump())
	}

	// A click opens a new column next to the sidebar: the room keeps its place.
	s.click(cols-4, sidebarRowOf(frame, cols, "w1"))
	frame = s.awaitFrame("w1's column", func(rows []string) bool { _, ok := paneNamed(rows, cols, "w1"); return ok })
	if p, _ := paneNamed(frame, cols, "w1"); p.x < cols/3 || p.y != 0 {
		t.Errorf("w1 opened at %+v, want a column right of the room.\n%s", p, s.dump())
	}

	// A Shift-click opens below the pane that holds the keys, which is w1's.
	s.send(sgr(4, "M", cols-4, sidebarRowOf(frame, cols, "w2")) + sgr(4, "m", cols-4, sidebarRowOf(frame, cols, "w2")))
	frame = s.awaitFrame("w2 under w1", func(rows []string) bool { _, ok := paneNamed(rows, cols, "w2"); return ok })
	w1, _ := paneNamed(frame, cols, "w1")
	if w2, _ := paneNamed(frame, cols, "w2"); w2.x != w1.x || w2.y <= w1.y {
		t.Errorf("w2 opened at %+v, want it under w1 at %+v.\n%s", w2, w1, s.dump())
	}

	// Mid-drag the tag follows the pointer and the room's lower half is outlined;
	// nothing has moved yet.
	row := sidebarRowOf(frame, cols, "w3")
	s.dragAgent(cols, row, 20, 30)
	frame = s.awaitFrame("w3's tag over the room", func(rows []string) bool {
		return strings.Contains(ansi.Strip(ansi.Cut(rows[30], 10, 40)), "w3") && strings.Contains(strings.Join(rows, "\n"), "┌")
	})
	if _, open := paneNamed(frame, cols, "w3"); open {
		t.Errorf("w3 opened before the button was let go.\n%s", s.dump())
	}

	// Letting go on the room's bottom half opens it below the room.
	s.send(sgr(0, "m", 20, 30))
	frame = s.awaitFrame("w3 under the room", func(rows []string) bool { _, ok := paneNamed(rows, cols, "w3"); return ok })
	w3, _ := paneNamed(frame, cols, "w3")
	if w3.x > 2 || w3.y < rows/3 { // the room's column, one margin in
		t.Errorf("w3 opened at %+v, want the lower half of the room's column.\n%s", w3, s.dump())
	}
	if strings.Contains(strings.Join(frame, "\n"), "┌") {
		t.Errorf("the outline outlived the drop.\n%s", s.dump())
	}

	// Esc cancels a drag in flight: nothing opens, and the release after it is nothing.
	s.dragAgent(cols, sidebarRowOf(frame, cols, "w4"), 60, 5)
	s.awaitFrame("the drag's outline", func(rows []string) bool { return strings.Contains(strings.Join(rows, "\n"), "┌") })
	s.send("\x1b")
	s.awaitFrame("the outline gone after esc", func(rows []string) bool { return !strings.Contains(strings.Join(rows, "\n"), "┌") })
	s.send(sgr(0, "m", 60, 5))
	time.Sleep(300 * time.Millisecond)
	if _, open := paneNamed(s.settle(), cols, "w4"); open {
		t.Errorf("a cancelled drag opened w4.\n%s", s.dump())
	}
}

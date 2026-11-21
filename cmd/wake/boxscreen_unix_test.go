//go:build unix

package main

// The sidebar's sections are boxes, on a real screen: a team's top edge, its
// members between walls and a closing row under the last, drawn in the channel's
// colour, clicked on the row they are drawn on, and cut by the wheel. internal/ui
// holds the row arithmetic; this is the half a person sees.

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/hinshun/vt10x"

	"github.com/DilanDoshi/wake/internal/ui"
)

const (
	boxCols = 100
	// boxDivider is the column of the rule beside the sidebar: the margin, then the
	// 24 columns the sidebar owns. The box rows are drawn after it.
	boxDivider = boxCols - 1 - sidebarCols
)

// sidebarRows is every screen row cut to the sidebar's box columns, trimmed.
func sidebarRows(rows []string) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = strings.TrimSpace(ansi.Cut(row, boxDivider+1, boxCols-1))
	}
	return out
}

// searchBarRows is the sidebar's search bar over the roster (ui.searchBarRows): its box
// has edges of its own, and none of them is a team's.
const searchBarRows = 3

// boxRowOf is the screen row of the sidebar row under the search bar satisfying match, or -1.
func boxRowOf(s *screen, match func(string) bool) int {
	if i := slices.IndexFunc(sidebarRows(s.lines())[searchBarRows:], match); i >= 0 {
		return i + searchBarRows
	}
	return -1
}

func hasEdge(name string) func(string) bool {
	return func(row string) bool {
		return strings.HasPrefix(row, "╭") && strings.HasSuffix(row, "╮") && strings.Contains(row, " "+name+" ")
	}
}

func hasMember(name string) func(string) bool {
	return func(row string) bool {
		return strings.HasPrefix(row, "│ ") && strings.HasSuffix(row, " │") && strings.Contains(row, " "+name)
	}
}

func isClosing(row string) bool {
	return strings.HasPrefix(row, "╰") && strings.HasSuffix(row, "╯")
}

// colourOf is a theme colour as vt10x reads it from a truecolor cell.
func colourOf(t *testing.T, hex string) vt10x.Color {
	t.Helper()
	var rgb uint32
	if _, err := fmt.Sscanf(hex, "#%06x", &rgb); err != nil {
		t.Fatalf("theme colour %q: %v", hex, err)
	}
	return vt10x.Color(rgb)
}

func TestTeamSectionsAreBoxesInTheRosterAndClicksFollowTheRows(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, boxCols, 30)
	teamedFleet(t, s)
	s.settle()

	rows := sidebarRows(s.lines())[searchBarRows:]
	for _, team := range []string{"all", "backend", "frontend"} {
		if !slices.ContainsFunc(rows, hasEdge(team)) {
			t.Fatalf("no top edge for %s in the sidebar:\n%s", team, s.dump())
		}
	}
	for _, name := range []string{"w1", "w2", "w3"} {
		if !slices.ContainsFunc(rows, hasMember(name)) {
			t.Fatalf("%s is not drawn between its box's walls:\n%s", name, s.dump())
		}
	}
	// One closing row for each of all's box, backend and frontend.
	if n := len(slices.DeleteFunc(slices.Clone(rows), func(r string) bool { return !isClosing(r) })); n != 3 {
		t.Errorf("the sidebar draws %d closing rows, want 3:\n%s", n, s.dump())
	}
	t.Logf("SCREENSHOT team boxes in the roster:\n%s", s.dump())

	// A click on a closing row opens nothing: the room keeps the keys.
	if y := boxRowOf(s, isClosing); y < 0 {
		t.Fatalf("no closing row to click:\n%s", s.dump())
	} else {
		s.click(boxDivider+8, y)
		s.settle()
		if !strings.Contains(s.text(), "group chat") {
			t.Errorf("a click on a closing row left the room.\n%s", s.dump())
		}
	}

	// A click on a member's wall is a click on its row, as it was on its name.
	y := boxRowOf(s, hasMember("w3"))
	s.click(boxDivider+1, y) // the left wall
	s.await("@w3")
}

func TestTheBoxWearsItsChannelsColour(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	t.Setenv("COLORTERM", "truecolor")

	s := startWakeInAConversation(t, boxCols, 30)
	teamedFleet(t, s)
	s.settle()

	// Pick backend: its edge is the row the click resolves to.
	edge := boxRowOf(s, hasEdge("backend"))
	s.click(boxDivider+8, edge)
	s.settle()

	wallAt := func(y int) vt10x.Color {
		s.term.Lock()
		defer s.term.Unlock()
		return s.term.Cell(boxDivider+1, y).FG
	}
	wall := func(name string) vt10x.Color { return wallAt(boxRowOf(s, hasMember(name))) }
	accent, muted := colourOf(t, ui.Accent.Dark), colourOf(t, ui.Muted.Dark)
	if got := wall("w1"); got != accent {
		t.Errorf("backend is the picked channel and its wall is %#x, want the accent %#x.\n%s", got, accent, s.dump())
	}
	if got := wall("w3"); got != muted {
		t.Errorf("frontend is not picked and its wall is %#x, want the muted %#x.\n%s", got, muted, s.dump())
	}
	// The loose agents' box under all is muted too, once another channel is picked.
	if got := wallAt(boxRowOf(s, hasEdge("all")) + 1); got != muted {
		t.Errorf("all is not picked and its box's wall is %#x, want the muted %#x.\n%s", got, muted, s.dump())
	}
}

func TestAScrolledBoxRunsOffTheWindowAndTheLastWindowEndsOnAClosingRow(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, boxCols, 30)
	fleetOfNine(t, s) // nine agents in the loose box; the cursor ends on the last
	s.resize(boxCols, 12)
	s.awaitFrame("the roster's count after the resize", sayHidden)

	// Wheel to the end: nothing more below, and the window ends on the box's closing row.
	for range 12 {
		s.send(sidebarWheel(true, boxCols, 4))
	}
	rows := s.awaitFrame("the roster scrolled to its end", func(rows []string) bool {
		return !strings.Contains(strings.Join(rows, "\n"), "below") && sayHidden(rows)
	})
	side := sidebarRows(rows)
	count := slices.IndexFunc(side, func(r string) bool { return rosterCount.MatchString(r) })
	if count < 1 || !isClosing(side[count-1]) {
		t.Errorf("the last window is not a closing row then the count:\n%s", s.dump())
	}
	// And at the top the window's first rows are the box's own, under the pinned edge.
	for range 12 {
		s.send(sidebarWheel(false, boxCols, 4))
	}
	top := sidebarRows(s.awaitFrame("the roster scrolled to its top", func(rows []string) bool {
		return !strings.Contains(strings.Join(rows, "\n"), "above")
	}))
	if len(top) < 2 || !strings.HasPrefix(top[0], "╭") || !strings.HasPrefix(top[1], "│ ") {
		t.Errorf("the roster's top is %q then %q, want all's top edge over a member:\n%s", top[0], top[1], s.dump())
	}
}

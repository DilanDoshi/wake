//go:build unix

// The grid, on a real screen: the two keys that place a conversation, the one
// that takes it away, and the mouse over all of it.
//
// internal/ui/frame_test.go asserts the same shapes against App.View in
// process. These drive the real binary through a pty, so the whole path is
// under test - the byte, bubbletea's decoding of it, the grid, the layout, and
// what lands in the emulator's cells.
//
// What they cannot say is whether that byte is ever sent. This file passed
// against the CSI for ⌃⇧→ for as long as that was a grid key, and it reaches no
// macOS at all: the window server takes all four ctrl+shift+arrows first. A pty
// proves the program and never the keyboard, and docs/live-testing.md owns the
// half only a human at a terminal can answer.

package main

import (
	"strings"
	"testing"
)

// gridScreen is a room with two agents and nothing open, which is where every
// case below starts.
func gridScreen(t *testing.T, cols, rows int) (*screen, []string) {
	t.Helper()
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, cols, rows)
	s.await("ready")
	s.send("\x17") // ⌃W back to the room
	s.await("group chat")
	s.send("/new robin\r")
	s.await("@robin")
	s.send("\x17")
	s.await("group chat")

	s.settle()

	names := agentsOnRoster(s)
	if len(names) != 2 {
		t.Fatalf("want two agents on the roster beside the manager, got %v.\n%s", s.rosterNames(), s.dump())
	}
	// The cursor is on robin, which is where /new left it - opening a
	// conversation selects it. Walk it onto the first agent so a caller's first
	// grid key is about the row it named. There used to be a bare ↑ here, which
	// worked only while the roster had one row: it wraps, and the row it wraps
	// onto is the manager now.
	s.pickRoster(names[1], names[0])
	return s, names
}

// panesOnScreen is how many composer boxes are drawn, which is how many panes
// there are: every pane has one and nothing else draws that corner.
func panesOnScreen(s *screen) int {
	var n int
	for _, line := range s.lines() {
		n += strings.Count(line, "╭")
	}
	return n
}

// paneTitles is the conversation each drawn pane names, read off the composer
// borders where a pane's own name is drawn.
//
// Not `strings.Contains(screen, "@name")`, which was the first draft and is a
// different question: an agent's handle is also on the notice row, in the
// room's transcript and on the roster, so a closed pane's name is still all
// over the frame. Reading the borders asks "is there a *pane* for it".
func paneTitles(s *screen) []string {
	var out []string
	for _, line := range s.lines() {
		if !strings.Contains(line, "╭") {
			continue
		}
		if strings.Contains(line, "group chat") {
			out = append(out, "group chat")
		}
		for _, f := range strings.Fields(line) {
			if strings.HasPrefix(f, "@") {
				out = append(out, f)
			}
		}
	}
	return out
}

// titled reports whether a pane on screen is named for this conversation.
func titled(s *screen, name string) bool {
	for _, t := range paneTitles(s) {
		if t == name {
			return true
		}
	}
	return false
}

// ⌃Y opens the picked agent in a column of its own, beside the room rather
// than instead of it.
func TestCtrlYOpensAColumnOnScreen(t *testing.T) {
	s, names := gridScreen(t, 200, 40)

	s.send("\x19") // ⌃Y
	s.await("@" + names[0])
	s.settle()

	if got := panesOnScreen(s); got != 2 {
		t.Fatalf("want the room and one conversation on screen, got %d panes.\n%s", got, s.dump())
	}
	if !strings.Contains(s.text(), "group chat") {
		t.Fatalf("⌃Y lost the room, which is the pane that never closes.\n%s", s.dump())
	}

	// A second one lands beside the first rather than replacing it, which is the
	// whole difference from ⌃D.
	s.pickRoster(names[0], names[1])
	s.send("\x19")
	s.await("@" + names[1])
	s.settle()

	if got := panesOnScreen(s); got != 3 {
		t.Fatalf("want three panes after a second ⌃Y, got %d.\n%s", got, s.dump())
	}
	if !titled(s, "@"+names[0]) {
		t.Fatalf("the second ⌃Y replaced the first conversation instead of opening beside it: panes are %v.\n%s", paneTitles(s), s.dump())
	}
}

// ⌃B stacks one under the focused pane, and the rule between them is drawn.
func TestCtrlBStacksAPaneOnScreen(t *testing.T) {
	s, names := gridScreen(t, 200, 40)

	s.send("\x19") // ⌃Y opens the first beside the room
	s.await("@" + names[0])
	s.pickRoster(names[0], names[1])
	s.send("\x02") // ⌃B stacks it under
	s.await("@" + names[1])
	s.settle()

	if got := panesOnScreen(s); got != 3 {
		t.Fatalf("want the room and a stacked column, got %d panes.\n%s", got, s.dump())
	}

	// The two conversations are in one column, so one is drawn above the other.
	first, second := s.rowOf("@"+names[0]), s.rowOf("@"+names[1])
	if first < 0 || second < 0 {
		t.Fatalf("both conversations should be on screen, rows %d and %d.\n%s", first, second, s.dump())
	}
	if first == second {
		t.Fatalf("both names are on row %d, so they are side by side rather than stacked.\n%s", first, s.dump())
	}
}

// ⌃W closes the focused pane and leaves the rest, and the room refuses to go.
func TestCtrlWClosesOnePaneOnScreen(t *testing.T) {
	s, names := gridScreen(t, 200, 40)

	s.send("\x19")
	s.await("@" + names[0])
	s.pickRoster(names[0], names[1])
	s.send("\x19")
	s.await("@" + names[1])
	s.settle()

	s.send("\x17") // ⌃W closes the focused one
	s.settle()
	if got := panesOnScreen(s); got != 2 {
		t.Fatalf("⌃W should leave the room and one conversation, got %d panes.\n%s", got, s.dump())
	}
	if titled(s, "@"+names[1]) {
		t.Fatalf("⌃W did not close the focused conversation: panes are %v.\n%s", paneTitles(s), s.dump())
	}
	if !titled(s, "@"+names[0]) {
		t.Fatalf("⌃W closed a pane nobody was in: panes are %v.\n%s", paneTitles(s), s.dump())
	}

	// And down to the room, which then refuses.
	s.send("\x17")
	s.settle()
	s.send("\x17")
	s.await("the room stays open")
	if !titled(s, "group chat") {
		t.Fatalf("⌃W closed the room, which is the one pane always drawn.\n%s", s.dump())
	}
}

// A click moves the keys to the pane it lands in, through the real SGR bytes a
// terminal sends.
func TestAClickFocusesAColumnOnScreen(t *testing.T) {
	s, names := gridScreen(t, 200, 40)

	s.send("\x19")
	s.await("@" + names[0])
	s.settle()

	// The room's own composer row, on the left half of the frame. Clicking it
	// must move the keys off the conversation that just opened.
	row := s.rowOf("group chat")
	if row < 0 {
		t.Fatalf("the room is not on screen.\n%s", s.dump())
	}
	s.click(10, row+1)
	s.settle()

	// The draft lands in whichever composer has the keys, so typing is what
	// says where they are.
	s.send("in the room")
	s.await("in the room")
	if got := s.rowOf("in the room"); got < 0 || got-row > 3 {
		t.Fatalf("the draft did not land in the room's composer at row %d (found at %d).\n%s", row, got, s.dump())
	}
}

// ⇧← and ⇧→ move the keys between the columns, from the CSI a terminal really
// sends.
//
// The bytes are the point. ⌘+arrow is what was asked for and bubbletea names
// nothing for it - its arrow table stops at modifier param 8 and cmd is param
// 9 - while ⌃+arrow is named, sent, and eaten by macOS before a terminal sees
// it, which is the trap ⌃⇧→ fell into when it was a grid key. Shift is what is
// left, and this is the test that drives the whole path: the escape sequence,
// bubbletea's decoding, App.key, Grid.Toward and the composer that ends up with
// the cursor.
//
// Where the draft lands is what says which pane has the keys: focus is not
// something a screen states, it is something typing reveals.
//
// **The column and not the row.** Two side-by-side panes occupy the same rows,
// so s.rowOf finds a draft at the same row whichever composer took it - the
// first draft of this test asserted rows and passed with both bindings deleted.
// colOf is the axis that separates them.
func TestShiftArrowsMoveTheKeysBetweenPanesOnScreen(t *testing.T) {
	s, names := gridScreen(t, 200, 40)

	s.send("\x19") // ⌃Y opens the first agent beside the room
	s.await("@" + names[0])
	s.settle()

	// Where the second pane's box begins. Not where its title is drawn - a pane
	// names itself on the right of its top border, so the conversation's name
	// sits at the far edge of its own column and every draft is left of it.
	edges := paneEdges(s)
	if len(edges) != 2 {
		t.Fatalf("want the room and one conversation side by side, found pane edges %v.\n%s", edges, s.dump())
	}
	split := edges[1]

	s.send("\x1b[1;2D") // ⇧← back to the room
	s.settle()
	s.send("typed in the room")
	s.await("typed in the room")
	if got := colOf(s, "typed in the room"); got < 0 || got >= split {
		t.Fatalf("⇧← did not move the keys to the room: the draft landed at column %d and the conversation's column starts at %d.\n%s", got, split, s.dump())
	}

	s.send("\x1b[1;2C") // ⇧→ back to the conversation
	s.settle()
	s.send("typed in the dm")
	s.await("typed in the dm")
	if got := colOf(s, "typed in the dm"); got < split {
		t.Fatalf("⇧→ did not move the keys to the conversation: the draft landed at column %d and the conversation's column starts at %d.\n%s", got, split, s.dump())
	}

	// And it moved the keys rather than placing anything: still the room and one
	// conversation, which is what separates this from ⌃Y and ⌃D.
	if got := panesOnScreen(s); got != 2 {
		t.Fatalf("the shifted arrows changed the number of panes to %d, and they open nothing.\n%s", got, s.dump())
	}
}

// paneEdges is the screen column each composer's box starts at, left to right.
//
// The composer boxes are the bottom-most boxes on screen - a transcript can draw
// one higher up - but they no longer share a row: the room carries a target line
// the DM does not, so its box sits one row above the conversation's while their
// info bars and legends still align. So the edges are gathered from the two
// bottom-most rows that carry a ╭, which between them hold every pane's left
// edge, scanned by column so the result stays left to right.
func paneEdges(s *screen) []int {
	lines := s.lines()
	var rows []string
	for i := len(lines) - 1; i >= 0 && len(rows) < 2; i-- {
		if strings.ContainsRune(lines[i], '╭') {
			rows = append(rows, lines[i])
		}
	}
	width := 0
	for _, r := range rows {
		if n := len([]rune(r)); n > width {
			width = n
		}
	}
	var out []int
	for col := 0; col < width; col++ {
		for _, r := range rows {
			rr := []rune(r)
			if col < len(rr) && rr[col] == '╭' {
				out = append(out, col)
				break
			}
		}
	}
	return out
}

// colOf is the screen column some text starts at, or -1. The horizontal half of
// rowOf, and the half that can tell two columns apart.
//
// The rune count and not the byte offset strings.Index returns: a line from
// s.lines() is one rune per cell, and the frame is drawn in box characters that
// are three bytes each. The first draft reported column 451 on a 200-column
// screen.
func colOf(s *screen, text string) int {
	for _, line := range s.lines() {
		if at := strings.Index(line, text); at >= 0 {
			return len([]rune(line[:at]))
		}
	}
	return -1
}

// Dragging the divider moves it on screen, through a real press, motion and
// release. This is the one assertion no in-process test can make: it is the
// terminal's own mouse encoding and the emulator's own cells.
func TestDraggingTheDividerMovesItOnScreen(t *testing.T) {
	s, names := gridScreen(t, 200, 40)

	s.send("\x19")
	s.await("@" + names[0])
	// The right sidebar draws a full-height rule of its own, and the rightmost
	// rule is the one dragged below. Closing it leaves the panes' divider as the
	// last one, which is the divider this is about.
	closeRoster(s)

	before := dividerColumns(s)
	if len(before) == 0 {
		t.Fatalf("no divider is drawn between the two panes.\n%s", s.dump())
	}
	at := before[len(before)-1]

	// Press on it, move 20 cells left, let go. SGR: 0 is the left button, 32 is
	// motion with it held, and the lower-case suffix is the release.
	s.send("\x1b[<0;" + itoa(at+1) + ";5M")
	s.send("\x1b[<32;" + itoa(at-20+1) + ";5M")
	s.send("\x1b[<0;" + itoa(at-20+1) + ";5m")
	s.settle()

	after := dividerColumns(s)
	if len(after) == 0 {
		t.Fatalf("the divider vanished after the drag.\n%s", s.dump())
	}
	moved := after[len(after)-1]
	if moved >= at {
		t.Fatalf("the divider was dragged from column %d to %d and is now at %d.\n%s", at, at-20, moved, s.dump())
	}
}

// dividerColumns is every screen column holding a rule on every row of the
// panes, which is what a full-height divider looks like from outside.
func dividerColumns(s *screen) []int {
	lines := s.lines()
	if len(lines) < 3 {
		return nil
	}
	// The composer boxes draw their own verticals, so a column only counts if
	// it carries a rule on the top row of the frame *and* well down it.
	var out []int
	top := []rune(lines[1])
	for x, r := range top {
		if r != '│' {
			continue
		}
		mid := []rune(lines[len(lines)/2])
		if x < len(mid) && mid[x] == '│' {
			out = append(out, x)
		}
	}
	return out
}

// itoa keeps the SGR sequences above readable.
func itoa(n int) string {
	if n < 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if i == len(b) {
		return "0"
	}
	return string(b[i:])
}

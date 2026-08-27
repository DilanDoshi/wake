//go:build unix

package main

// Subagents in the right sidebar, on a real screen.
//
// The unit tests decide what the rows say and where the cursor goes. This is
// the only thing that puts a dispatch through the real binary, a real pty and a
// terminal emulator with the sidebar drawn beside three other regions - which
// is what the rows have to survive: lipgloss joins columns on their widest
// line, so a sidebar row one column too wide moves the room and every pane
// sideways, and no unit test measuring the roster alone can see that happen.

import (
	"strings"
	"testing"
)

// liveDispatchScreen starts a fleet whose agent dispatches a subagent and
// leaves it running, and takes one turn. The conversation is deliberately *not*
// opened: the sidebar has to know what an agent is dispatching whether or not
// anybody has looked at it, which is the whole reason the fold moved to Fleet.
func liveDispatchScreen(t *testing.T) *screen {
	t.Helper()
	withScriptedAgent(t, scriptDispatchesLive)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 110, 34)
	s.await("ready")
	s.send("go\r")
	s.await(heardPrefix + "dispatched")
	return s
}

// The row is on screen, under its agent, without the conversation being open.
func TestARunningSubagentGetsARowInTheSidebar(t *testing.T) {
	s := liveDispatchScreen(t)

	s.await(dispatchType)

	sub := s.rowOf(dispatchType)
	if sub < 0 {
		t.Fatalf("no row names the subagent type:\n%s", s.dump())
	}
	// In the sidebar rather than in the room: the roster is the rightmost
	// region, so the row has to start well past the middle of the frame.
	line := s.lines()[sub]
	if at := strings.Index(line, dispatchType); at < sidebarBand {
		t.Errorf("the subagent row is at column %d, which is not the right sidebar:\n%s", at, s.dump())
	}
}

// The subagent row is nested under its agent within the right sidebar: its
// marker sits exactly subIndent columns past the agent's, and both edges are
// read *inside the sidebar* - past the pane divider - so the room pane cannot
// confound them.
//
// A row wider than its column does not overflow itself - lipgloss joins the
// regions on their widest line, so the whole sidebar gets wider and every pane
// to its left is pushed. The terminal emulator cannot show that directly:
// lines() reads exactly cols cells per row, so every line is bounded by
// construction and a width assertion here can never fail for the right reason -
// the pre-clip width is a unit concern (internal/ui.subagentRow's clip). What
// is observable is the nesting - the subagent indented exactly one step under
// its agent - which a row drawn at the wrong indent or shifted out of the
// sidebar loses. The offset is asserted exactly rather than as "> agent" so a
// one-column-left slip does not pass.
//
// **The edge is read past the divider, because a whole-line edge is cwd-length
// dependent.** leftEdge scans from column zero, so on a deep checkout the room
// pane's cwd path - drawn at column zero when it is too wide for the banner
// sprite - was read as the agent row's edge, while the subagent row, blank in
// the room pane, read the divider instead and the two disagreed. A short
// checkout drew the sprite at column zero on both rows, so they matched by
// accident. sidebar() drops the room pane, so neither reaches the assertion.
func TestASidebarSubagentRowDoesNotWidenTheSidebar(t *testing.T) {
	s := liveDispatchScreen(t)
	s.await(dispatchType)
	s.settle()

	sub := s.rowOf(dispatchType)
	if sub < 1 {
		t.Fatalf("no subagent row on screen:\n%s", s.dump())
	}
	lines := s.lines()
	agentEdge := leftEdge(sidebar(lines[sub-1]))
	subEdge := leftEdge(sidebar(lines[sub]))
	if agentEdge < 0 || subEdge < 0 {
		t.Fatalf("the agent row or its subagent has no sidebar content to compare:\n%s", s.dump())
	}
	if subEdge-agentEdge != subIndent {
		t.Errorf("the subagent marker sits %d columns past its agent's, want %d - it is mis-indented in the sidebar:\n%s",
			subEdge-agentEdge, subIndent, s.dump())
	}
}

// Below sidebarsHideColumns the roster gets no column, so its subagent rows go
// with it rather than being drawn where nothing was reserved - the failure
// groups.go's header describes, in which a sidebar drawn outside its region
// moves every click to its right.
//
// Asserted on *position* rather than presence, because the type is not a
// sidebar-only string: the conversation pane draws its own dispatch list, and
// below dmTakeoverColumns that pane is the whole frame. The first version of
// this test looked for the word anywhere and failed on the pane's copy.
func TestTheSidebarsSubagentRowsGoWithTheSidebar(t *testing.T) {
	s := liveDispatchScreen(t)
	s.await(dispatchType)
	if at := strings.Index(s.lines()[s.rowOf(dispatchType)], dispatchType); at < sidebarBand {
		t.Fatalf("the row is not in the sidebar to begin with, at column %d:\n%s", at, s.dump())
	}

	s.resize(80, 24)
	s.settle()
	for i, line := range s.lines() {
		if at := strings.Index(line, dispatchType); at >= sidebarBand {
			t.Errorf("row %d draws a subagent at column %d with no sidebar reserved:\n%s", i, at, s.dump())
		}
	}
}

// sidebarBand is a column no pane's own content reaches at these widths, so a
// hit past it is the right sidebar rather than a conversation's dispatch list.
const sidebarBand = 60

// paneDivider is the column lipgloss draws between two panes - ui.dividerGlyph,
// spelled here because internal/ui does not export it. The right sidebar is the
// last region and its rows carry no divider of their own, so the rightmost one
// on a row is the sidebar's left border.
const paneDivider = '│'

// subIndent is how many columns a subagent row is indented under its agent
// within the sidebar - ui.toolIndent, re-spelled because internal/ui does not
// export it. subagentRow prepends this many spaces before its ⎿ while an agent's
// own row starts at the sidebar's left edge, so it is the exact offset between
// the two markers.
const subIndent = 2

// sidebar is the portion of a row inside the right sidebar: everything past the
// last pane divider, or "" if the row has none. Reading the edge here rather
// than across the whole row is what keeps it robust to cwd length - the room
// pane, where a long path draws at column zero, is dropped.
func sidebar(line string) string {
	runes := []rune(line)
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] == paneDivider {
			return string(runes[i+1:])
		}
	}
	return ""
}

// leftEdge is the column a row's first non-blank character sits in, or -1.
func leftEdge(line string) int {
	for i, r := range []rune(line) {
		if r != ' ' {
			return i
		}
	}
	return -1
}

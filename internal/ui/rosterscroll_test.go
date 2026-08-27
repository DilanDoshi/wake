package ui

// The sidebars stop lying about the size of the fleet.
//
// Both columns used to draw until they ran out of rows and then stop, with
// nothing saying they had. At thirty agents on a forty-row terminal that is a
// third of the fleet invisible in the one surface whose job is telling an
// operator who needs them - and ↑↓ could put the cursor on a row nobody could
// see, so ⌃D opened a conversation with an agent the operator was never shown.

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// fleetOf is n idle agents, named a0..a(n-1).
func fleetOf(n int) []Agent {
	out := make([]Agent, n)
	for i := range out {
		out[i] = Agent{ID: fmt.Sprintf("s%d", i), Name: fmt.Sprintf("a%d", i), State: rpc.StateIdle}
	}
	return out
}

// subsEverySecond gives every other agent a couple of running dispatches.
//
// The sweeps below were written when a row was one line or two - a tool call or
// none - and they still pass nil for the lookup at every size and cursor they
// try. Subagent rows are a third shape and an unbounded one, so the properties
// these tests exist to hold (the window fills the column, the cursor is inside
// it, At answers for exactly the rows View drew) are re-run against a sidebar
// that has them. ownerPerRow already reads an indented row as belonging to the
// agent above it, which is how a subagent row is drawn.
func subsEverySecond(agents []Agent) subsOf {
	m := map[string][]Task{}
	for i, a := range agents {
		if i%2 == 0 {
			m[a.ID] = []Task{
				running("d"+a.ID+"a", "code-reviewer", 1100),
				running("d"+a.ID+"b", "general-purpose", 0),
			}
		}
	}
	return subsFrom(m)
}

// busyFleetOf is n agents inside a tool call, so every row is two lines.
func busyFleetOf(n int) []Agent {
	out := fleetOf(n)
	for i := range out {
		out[i].State, out[i].Tool, out[i].ToolArg = rpc.StateWorking, "Read", fmt.Sprintf("pkg/f%d.go", i)
	}
	return out
}

// Every agent in the fleet can be brought on screen with the cursor.
//
// This is the defect stated as a property: at any fleet size, for any agent,
// selecting it makes it visible. Without the window it held only for the agents
// that happened to fit, and nothing said which those were.
func TestEveryAgentIsReachableWithTheCursor(t *testing.T) {
	agents := fleetOf(30)
	for _, a := range agents {
		out := stripANSI(Roster{Selected: a.ID}.View(agents, nil, rosterWidth, 8))
		if !strings.Contains(out, a.Name+" ") {
			t.Errorf("the cursor is on %s and the column does not draw it:\n%s", a.Name, out)
		}
	}
}

// The count says exactly how many agents are not on screen.
//
// Derived from what was drawn rather than restated, because a count that is
// computed twice is a count that can disagree with the column beside it - and
// the whole point of the line is that it is the thing an operator cannot work
// out by looking.
func TestTheCountIsExactlyWhatWasLeftOut(t *testing.T) {
	for _, n := range []int{9, 12, 30} {
		for _, height := range []int{4, 8, 20} {
			agents := fleetOf(n)
			// With a cursor, and at the tail. Without one the window always
			// starts at zero, so `len(agents) - w.to` passes as readily as
			// `len(agents) - (w.to - w.from)` - and the mutant emits no
			// overflow line at all, which is the silence this file removes.
			for _, cursor := range []string{"", agents[n-1].ID, agents[n/2].ID} {
				out := stripANSI(Roster{Selected: cursor}.View(agents, nil, rosterWidth, height))

				drawn := 0
				for _, a := range agents {
					if strings.Contains(out, a.Name+" ") {
						drawn++
					}
				}
				want := n - drawn
				if want == 0 {
					if strings.Contains(out, "more") {
						t.Errorf("%d agents in %d rows: everything fits and the column still says something is hidden:\n%s", n, height, out)
					}
					continue
				}
				if !strings.Contains(out, moreLine(want)) {
					t.Errorf("%d agents in %d rows, cursor %q: drew %d and does not say %q:\n%s", n, height, cursor, drawn, moreLine(want), out)
				}
			}
		}
	}
}

// The window is maximal: nothing more would fit on either side of it.
//
// **Nothing asserted this, and the review found it.** Deleting the backward
// growth entirely left the whole suite green - including both pty screen tests -
// while a column with the cursor on the last agent drew one row and six blank
// ones. Every count assertion used the zero cursor, where backward growth cannot
// move; the click test picked a cursor the forward loop alone could fill around.
// So this asks the property directly rather than a symptom of it.
func TestTheWindowFillsTheColumn(t *testing.T) {
	for _, agents := range [][]Agent{fleetOf(30), busyFleetOf(12), fleetOf(9)} {
		for _, subs := range []subsOf{nil, subsEverySecond(agents)} {
			for _, height := range []int{3, 5, 8, 12} {
				for _, cursor := range []string{agents[0].ID, agents[len(agents)/2].ID, agents[len(agents)-1].ID} {
					r := Roster{Selected: cursor}
					w := r.window(agents, subs, rosterWidth, height)
					if w.from == 0 && w.to == len(agents) {
						continue // everything fits
					}
					used := 0
					for _, a := range agents[w.from:w.to] {
						used += rowsFor(a, subsFor(subs, a.ID))
					}
					room := height - 1
					if w.to < len(agents) && used+rowsFor(agents[w.to], subsFor(subs, agents[w.to].ID)) <= room {
						t.Errorf("%d agents, height %d, cursor %q: window [%d,%d) uses %d of %d rows and the agent after it would fit",
							len(agents), height, cursor, w.from, w.to, used, room)
					}
					if w.from > 0 && used+rowsFor(agents[w.from-1], subsFor(subs, agents[w.from-1].ID)) <= room {
						t.Errorf("%d agents, height %d, cursor %q: window [%d,%d) uses %d of %d rows and the agent before it would fit",
							len(agents), height, cursor, w.from, w.to, used, room)
					}
				}
			}
		}
	}
}

// The cursor is always inside the window.
//
// Stated as its own property because the first draft's doc comment claimed it
// was true by construction and it was not: with the cursor on an agent taller
// than the room, the forward loop could not place it and the backward loop then
// measured a range that did not contain it, so the column drew the agent above
// the cursor. ⌃D then opened a conversation with an agent that was not on
// screen - the defect this file exists to remove, arriving through its own fix.
func TestTheCursorIsAlwaysInsideTheWindow(t *testing.T) {
	mixed := fleetOf(9)
	for i := range mixed {
		if i%2 == 1 {
			mixed[i].Tool, mixed[i].ToolArg = "Read", "pkg/f.go"
		}
	}
	for _, agents := range [][]Agent{fleetOf(9), busyFleetOf(9), mixed} {
		for _, subs := range []subsOf{nil, subsEverySecond(agents)} {
			for height := 1; height <= 12; height++ {
				for i, a := range agents {
					r := Roster{Selected: a.ID}
					w := r.window(agents, subs, rosterWidth, height)
					if w.to == w.from {
						// An empty window is only honest when no whole agent fits.
						// Skipping it unconditionally is what made the first draft
						// of this test vacuous: a mutant that returned span{} for
						// everything passed it.
						if room := height - 1; rowsFor(a, subsFor(subs, a.ID)) <= room {
							t.Errorf("height %d, cursor on agent %d (%d rows): window is empty and the cursor's agent fits in %d rows",
								height, i, rowsFor(a, subsFor(subs, a.ID)), room)
						}
						continue
					}
					if i < w.from || i >= w.to {
						t.Errorf("height %d, cursor on agent %d (%d rows): window is [%d,%d), which does not contain it",
							height, i, rowsFor(a, subsFor(subs, a.ID)), w.from, w.to)
					}
				}
			}
		}
	}
}

// rows draws exactly the lines rowsFor promises.
//
// window sizes itself with rowsFor and never renders, so a divergence between
// the two is a column laid out for rows somebody else is drawing.
func TestRowsDrawsExactlyTheLinesRowsForPromises(t *testing.T) {
	for _, a := range []Agent{
		{Name: "a", State: rpc.StateIdle},
		{Name: "a", State: rpc.StateWorking, Tool: "Read", ToolArg: "pkg/f.go"},
		{Name: "a", State: rpc.StateBlocked, Tool: "Write"},
		{Name: "a"},
	} {
		if got, want := len(Roster{}.rows(a, nil, rosterWidth)), rowsFor(a, nil); got != want {
			t.Errorf("rows drew %d lines for %+v and rowsFor promises %d", got, a, want)
		}
	}
}

// A click lands on the agent under the pointer, on a scrolled column.
//
// At and View walk the same window, so this is a property of them sharing one
// function rather than of two functions agreeing. It is the failure that would
// otherwise be silent: a click on a scrolled roster opening a conversation with
// whichever agent would have been on that row unscrolled.
func TestAClickOnAScrolledColumnLandsOnTheAgentUnderIt(t *testing.T) {
	agents := fleetOf(30)
	const height = 8
	r := Roster{Selected: agents[20].ID}

	lines := strings.Split(stripANSI(r.View(agents, nil, rosterWidth, height)), "\n")
	for y, line := range lines {
		name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "○"))
		got, _, ok := r.At(agents, nil, rosterWidth, height, y)
		if name == "" || strings.Contains(line, "more") {
			continue
		}
		if !ok || got.Name != name {
			t.Errorf("row %d draws %q and a click there returns %q (ok=%v)", y, name, got.Name, ok)
		}
	}
}

// An agent is drawn whole or not at all, however tall its row is.
//
// The window grows by whole agents, so a tool call can never appear under a
// name that was cut off above it - which is the shape a column that filled to
// the row would produce every time the last agent to fit was a working one.
func TestAnAgentIsDrawnWholeOrNotAtAll(t *testing.T) {
	agents := busyFleetOf(10)
	for height := 3; height <= 12; height++ {
		out := stripANSI(Roster{}.View(agents, nil, rosterWidth, height))
		for i, a := range agents {
			name := strings.Contains(out, a.Name+" ")
			tool := strings.Contains(out, fmt.Sprintf("f%d.go", i))
			if tool && !name {
				t.Errorf("height %d: agent %s's tool call is drawn without its name:\n%s", height, a.Name, out)
			}
		}
	}
}

// The column measures exactly what it was given, at every size, with and
// without the count.
//
// The overflow line is a row like any other and is budgeted before the walk;
// a window sized as though it were free is one row too tall, and lipgloss wraps
// the row it cannot fit, which pushes the whole column down and shoves the
// panes beside it sideways.
func TestTheColumnStillMeasuresExactlyWhatItWasGiven(t *testing.T) {
	for _, agents := range [][]Agent{nil, fleetOf(1), fleetOf(9), fleetOf(30), busyFleetOf(20)} {
		for _, height := range []int{1, 2, 5, 8, 20} {
			out := Roster{}.View(agents, nil, rosterWidth, height)
			measureColumn(t, out, rosterWidth, height)
		}
	}
}

// A column with no room for a whole agent says how many there are.
//
// The count gets the last row and agents fill what is left, so at a height
// where nothing whole fits the count is all there is. That is the honest
// answer: a cut agent implies the fleet is that agent, which is the one thing
// this column must never say - and the earlier draft, which drew the agent
// anyway, pushed the count off the bottom with it.
func TestAColumnWithNoRoomForAnAgentSaysHowManyThereAre(t *testing.T) {
	for _, tc := range []struct {
		agents []Agent
		height int
	}{
		{busyFleetOf(4), 1},
		{busyFleetOf(4), 2},
		{fleetOf(5), 1},
	} {
		out := stripANSI(Roster{}.View(tc.agents, nil, rosterWidth, tc.height))
		if !strings.Contains(out, moreLine(len(tc.agents))) {
			t.Errorf("%d agents in %d rows does not say all %d are hidden:\n%q", len(tc.agents), tc.height, len(tc.agents), out)
		}
	}
}

// At answers for a row the column drew and for no other row, at every size.
//
// **This is a property rather than a bound, and the difference is the entry.**
// An adversarial review found that At could answer for a row View never emitted
// - through window's old fallback, which drew one agent taller than the whole
// column - and a click on the notice row below the sidebar then opened a
// conversation nobody asked for. The fix was to delete the fallback, so the
// window can no longer be taller than the column; a `y >= height` guard on top
// of that is unreachable, and this project treats an unreachable guard as a
// defect. What replaces it is the invariant the guard was standing in for,
// checked at every height and both row heights, so the day somebody reinstates
// a fallback this fails rather than a click going somewhere quietly wrong.
func TestAtAnswersForTheRowsTheColumnDrewAndNoOthers(t *testing.T) {
	for _, agents := range [][]Agent{fleetOf(1), fleetOf(9), busyFleetOf(7)} {
		for _, subs := range []subsOf{nil, subsEverySecond(agents)} {
			for _, height := range []int{1, 2, 3, 5, 9, 20} {
				for _, cursor := range []string{"", agents[0].ID, agents[len(agents)-1].ID} {
					r := Roster{Selected: cursor}
					drawn := strings.Split(stripANSI(r.View(agents, subs, rosterWidth, height)), "\n")

					owner := ownerPerRow(drawn)
					for y := -1; y <= height+2; y++ {
						got, _, ok := r.At(agents, subs, rosterWidth, height, y)
						want := ""
						if y >= 0 && y < len(owner) {
							want = owner[y]
						}
						switch {
						case ok && want == "":
							t.Errorf("%d agents, height %d, cursor %q: a click at y=%d returned %s and that row is %q",
								len(agents), height, cursor, y, got.Name, rowAt(drawn, y))
						case ok && got.Name != want:
							t.Errorf("%d agents, height %d, cursor %q: a click at y=%d returned %s and the row belongs to %s",
								len(agents), height, cursor, y, got.Name, want)
						case !ok && want != "":
							t.Errorf("%d agents, height %d, cursor %q: a click at y=%d returned nobody and the row belongs to %s",
								len(agents), height, cursor, y, want)
						}
					}
				}
			}
		}
	}
}

// ownerPerRow is which agent each drawn row belongs to, by name, and "" for a
// row that belongs to none - the count, or the padding below the fleet.
//
// A row indented past the glyph is a continuation of the agent above it, which
// is what makes a click on a tool call open its agent's conversation.
func ownerPerRow(rows []string) []string {
	out := make([]string, len(rows))
	current := ""
	for i, row := range rows {
		switch {
		case strings.TrimSpace(row) == "" || strings.Contains(row, "more"):
			current = ""
		case strings.HasPrefix(row, "  "):
			// A continuation keeps the agent above it.
		default:
			current = strings.Fields(row)[1]
		}
		out[i] = current
	}
	return out
}

// rowAt is a row for an error message, or a note that there is none.
func rowAt(rows []string, y int) string {
	if y < 0 || y >= len(rows) {
		return "off the column"
	}
	return rows[y]
}

// Every row of both sidebars measures the column, at every height including
// the ones where the overflow line is all there is.
//
// A row narrower than the column makes the whole block narrower, lipgloss then
// joins the pane beside it several columns early, and Layout.Hit goes on using
// the width the layout reserved - so every click lands on the wrong region for
// as long as that row is on screen.
func TestEveryRowOfBothSidebarsMeasuresTheColumn(t *testing.T) {
	agents := fleetOf(9)
	for i := range agents {
		agents[i].Cwd = "/repos/r" + strconv.Itoa(i)
	}
	for _, height := range []int{1, 2, 3, 5, 9, 20} {
		measureColumn(t, Roster{}.View(agents, nil, rosterWidth, height), rosterWidth, height)
		measureColumn(t, Groups{}.View(agents, groupsWidth, height), groupsWidth, height)
	}
}

// The workspaces sidebar says what it left out too.
//
// It does not scroll, and that is the difference rather than an omission: a
// workspace list has no cursor, so there is nothing to follow and no key that
// could reach the rows below. The count is what an operator can act on.
func TestTheWorkspacesSidebarSaysWhatItLeftOut(t *testing.T) {
	agents := fleetOf(12)
	for i := range agents {
		agents[i].Cwd = "/repos/r" + strconv.Itoa(i)
	}

	const height = 5
	out := stripANSI(Groups{}.View(agents, groupsWidth, height))
	if !strings.Contains(out, moreLine(12-(height-1))) {
		t.Errorf("12 workspaces in %d rows does not say how many are hidden:\n%s", height, out)
	}
	if got := len(strings.Split(out, "\n")); got != height {
		t.Errorf("the workspaces column drew %d rows, want %d", got, height)
	}

	// And says nothing when there is nothing to say.
	if out := stripANSI(Groups{}.View(agents[:2], groupsWidth, height)); strings.Contains(out, "more") {
		t.Errorf("2 workspaces in %d rows claims something is hidden:\n%s", height, out)
	}
}

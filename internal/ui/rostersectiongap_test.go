package ui

// A blank row between sections: a team's header is set off from whatever is above
// it, so each team reads as its own chunk the way the board's bands do.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// gapFleet is an untagged top block over two teams, the shape of a real fleet.
func gapFleet(t *testing.T, sessions ...rpc.SessionStatus) []Agent {
	t.Helper()
	f := (Fleet{}).WithStatus(&rpc.Status{Teams: []string{"backend", "frontend"}, Sessions: sessions})
	return f.sectioned(f.OnRoster())
}

func gapSessions() []rpc.SessionStatus {
	return []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
		{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
		{ID: "s3", Name: "john", Team: "backend", State: rpc.StateIdle},
		{ID: "s4", Name: "mara", Team: "frontend", State: rpc.StateIdle},
	}
}

func rosterLines(agents []Agent, selected string, subs subsOf, width, height int) []string {
	return strings.Split(stripANSI(Roster{Selected: selected}.View(agents, subs, width, height)), "\n")
}

func rowContaining(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

func noSubsOf(string) []Task { return nil }

// The top block's last row and the first team's header are a blank apart, and so
// are one team's last row and the next team's header.
func TestARosterTeamHeaderHasABlankRowAboveIt(t *testing.T) {
	lines := rosterLines(gapSessionsAgents(t), "", noSubsOf, 30, 20)

	for _, tc := range []struct{ header, above string }{{"backend", "alex"}, {"frontend", "john"}} {
		h := rowContaining(lines, tc.header)
		if h < 2 {
			t.Fatalf("the %s header is on row %d:\n%s", tc.header, h, strings.Join(lines, "\n"))
		}
		if strings.TrimSpace(lines[h-1]) != "" {
			t.Errorf("row %d above the %s header is %q, want a blank row", h-1, tc.header, lines[h-1])
		}
		if !strings.Contains(lines[h-2], tc.above) {
			t.Errorf("row %d is %q, want %s's row just above the gap", h-2, lines[h-2], tc.above)
		}
	}
}

func gapSessionsAgents(t *testing.T) []Agent { return gapFleet(t, gapSessions()...) }

// A header with nothing above it has nothing to be set off from: a roster that
// opens on a team opens on its header.
func TestARosterThatOpensOnATeamDrawsNoBlankAboveIt(t *testing.T) {
	agents := gapFleet(t,
		rpc.SessionStatus{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s4", Name: "mara", Team: "frontend", State: rpc.StateIdle},
	)

	lines := rosterLines(agents, "", noSubsOf, 30, 20)

	if rowContaining(lines, "backend") != 0 {
		t.Errorf("the roster does not open on the backend header:\n%s", strings.Join(lines, "\n"))
	}
}

// No team, no header and no gap: the flat roster it has always drawn - scrolled,
// too, where the window is engaged and a stray blank would cost a row of agents.
func TestAFleetWithNoTeamsDrawsNoBlankRows(t *testing.T) {
	var sessions []rpc.SessionStatus
	for _, name := range []string{"alex", "thea", "john", "mara", "omar", "lena"} {
		sessions = append(sessions, rpc.SessionStatus{ID: "s-" + name, Name: name, State: rpc.StateIdle})
	}
	agents := gapFleet(t, sessions...)

	lines := rosterLines(agents, "s-john", noSubsOf, 30, 4)

	if len(lines) != 4 {
		t.Fatalf("a 4-row column drew %d rows:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			t.Errorf("row %d of a team-less roster is blank:\n%s", i, strings.Join(lines, "\n"))
		}
	}
	if !strings.Contains(lines[3], "+3 more") {
		t.Errorf("three agents fit and the other three are counted, but the last row is %q:\n%s", lines[3], strings.Join(lines, "\n"))
	}
}

// What is drawn is what is clickable, at every column height and cursor: the blank
// and the header select nothing, and every row carrying an agent's name resolves to
// that agent. The three paths - window, view and hit - count rows one way.
func TestEveryRosterRowResolvesToWhatIsDrawnThereAtEveryHeight(t *testing.T) {
	agents := gapSessionsAgents(t)
	subs := func(id string) []Task {
		if id == "s3" { // john runs a subagent, so his block is taller than one row
			return []Task{{ID: "t1", Name: "check the harness", Status: core.TaskRunning}}
		}
		return nil
	}
	const width = 36
	for height := 3; height <= 14; height++ {
		for _, sel := range []string{"s1", "s2", "s3", "s4"} {
			r := Roster{Selected: sel}
			lines := rosterLines(agents, sel, subs, width, height)
			if len(lines) > height {
				t.Fatalf("height %d, cursor %s: %d rows drawn", height, sel, len(lines))
			}
			for y, line := range lines {
				a, _, ok := r.At(agents, subs, width, height, y)
				for _, name := range []string{"alex", "thea", "john", "mara"} {
					if strings.Contains(line, name) && (!ok || a.Name != name) {
						t.Errorf("height %d, cursor %s, row %d draws %q but a click resolves to (%q, %v):\n%s",
							height, sel, y, line, a.Name, ok, strings.Join(lines, "\n"))
					}
				}
				if strings.TrimSpace(line) == "" && ok {
					t.Errorf("height %d, cursor %s: the blank row %d selects %q", height, sel, y, a.Name)
				}
				if strings.Contains(line, "─") && ok {
					t.Errorf("height %d, cursor %s: the header on row %d selects %q", height, sel, y, a.Name)
				}
				// A gap or a header is never left over the count: whatever the window
				// keeps of a section, it keeps with a member under what leads it.
				if strings.Contains(line, " more") && y > 0 &&
					(strings.TrimSpace(lines[y-1]) == "" || strings.Contains(lines[y-1], "─")) {
					t.Errorf("height %d, cursor %s: the %q count has a gap or header just above it:\n%s",
						height, sel, strings.TrimSpace(line), strings.Join(lines, "\n"))
				}
			}
		}
	}
}

// The one number the window, the click and the draw share is exactly what is
// drawn: a count a row too tall would leave room unused, a row too short would
// draw past the column.
func TestRowsForRowIsExactlyTheLinesRowLinesDraws(t *testing.T) {
	agents := gapSessionsAgents(t)
	subs := func(id string) []Task {
		if id == "s2" {
			return []Task{{ID: "t1", Name: "check the harness", Status: core.TaskRunning}}
		}
		return nil
	}
	for _, item := range sectionRows(agents) {
		if got, want := rowsForRow(item, subs), len((Roster{}).rowLines(item, subs, 36)); got != want {
			t.Errorf("%s (header %q, gap %v): rowsForRow = %d but %d lines are drawn",
				item.agent.Name, item.header, item.gap, got, want)
		}
	}
}

// teamedSubs puts a running subagent under thea, a team's first agent, so her block
// is a gap, a divider, her own row and the subagent's.
func teamedSubs(id string) []Task {
	if id == "s2" {
		return []Task{{ID: "t1", Dispatch: "d1", Kind: core.TaskAgent, Name: "check the harness", Status: core.TaskRunning}}
	}
	return nil
}

// The window is read off the lines each item draws, not off the number the window
// itself counts by, so a running total that forgets the gap or the divider is
// seen: the rows drawn fit under the count, the count is there and says how many
// agents are hidden, and nothing more would have fitted.
func TestTheWindowFitsTheColumnAndKeepsItsCountAtEveryHeight(t *testing.T) {
	// Enough of a fleet that a block can be taken in backwards and another still
	// refused behind it: two untagged agents, then two teams of two.
	agents := gapFleet(t,
		rpc.SessionStatus{ID: "s0", Name: "bea", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s3", Name: "john", Team: "backend", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s4", Name: "mara", Team: "frontend", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s5", Name: "omar", Team: "frontend", State: rpc.StateIdle},
	)
	rows := sectionRows(agents)
	const width = 36
	for height := 3; height <= 18; height++ {
		for _, sel := range []string{"s0", "s1", "s2", "s3", "s4", "s5"} {
			r := Roster{Selected: sel}
			w := r.window(agents, teamedSubs, width, height)
			if w.to == w.from {
				continue // no whole block fits beside the count: the count alone is drawn
			}
			drawn := func(from, to int) (n int) {
				for _, item := range rows[from:to] {
					n += len(r.rowLines(item, teamedSubs, width))
				}
				return n
			}
			total, shown := drawn(0, len(rows)), drawn(w.from, w.to)
			if total <= height {
				if w.from != 0 || w.to != len(rows) {
					t.Errorf("height %d, cursor %s: everything fits in %d rows but the window is [%d,%d)", height, sel, total, w.from, w.to)
				}
				continue
			}
			room := height - 1
			if shown > room {
				t.Errorf("height %d, cursor %s: the window draws %d rows with room for %d", height, sel, shown, room)
			}
			if cursor := rowIndexOfSelected(rows, sel); cursor < w.from || cursor >= w.to {
				t.Errorf("height %d, cursor %s: the cursor's block is outside the window [%d,%d)", height, sel, w.from, w.to)
			}
			if w.to < len(rows) && shown+drawn(w.to, w.to+1) <= room {
				t.Errorf("height %d, cursor %s: the next block would have fitted below the window", height, sel)
			}
			if w.from > 0 && shown+drawn(w.from-1, w.from) <= room {
				t.Errorf("height %d, cursor %s: the previous block would have fitted above the window", height, sel)
			}
			view := rosterLines(agents, sel, teamedSubs, width, height)
			hidden := len(agents) - (w.to - w.from)
			if want := "+" + strconv.Itoa(hidden) + " more"; rowContaining(view, want) != shown {
				t.Errorf("height %d, cursor %s: the %q count is not on the row (%d) just under the window:\n%s",
					height, sel, want, shown, strings.Join(view, "\n"))
			}
		}
	}
}

// A click on a subagent row opens that subagent, under an agent that leads a
// section as under one that does not: the rows under the blank and the divider are
// still the last of the item's lines.
func TestAClickOnASubagentUnderATeamsFirstAgentOpensTheSubagent(t *testing.T) {
	agents := gapSessionsAgents(t)
	const width, height = 36, 20
	lines := rosterLines(agents, "", teamedSubs, width, height)
	sub := rowContaining(lines, "check the harness")
	if sub < 0 {
		t.Fatalf("the subagent row is not drawn:\n%s", strings.Join(lines, "\n"))
	}

	a, dispatch, ok := (Roster{}).At(agents, teamedSubs, width, height, sub)

	if !ok || a.Name != "thea" || dispatch != "d1" {
		t.Errorf("a click on the subagent row resolves to (%q, %q, %v), want thea's dispatch d1:\n%s", a.Name, dispatch, ok, strings.Join(lines, "\n"))
	}
}

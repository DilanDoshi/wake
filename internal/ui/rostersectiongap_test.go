package ui

// A blank row between sections: a team's header is set off from whatever is above
// it, so each team reads as its own chunk the way the board's bands do.

import (
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

// No team, no header and no gap: the flat roster it has always drawn.
func TestAFleetWithNoTeamsDrawsNoBlankRows(t *testing.T) {
	agents := gapFleet(t,
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "thea", State: rpc.StateIdle},
	)

	lines := rosterLines(agents, "", noSubsOf, 30, 2)

	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			t.Errorf("row %d of a team-less roster is blank:\n%s", i, strings.Join(lines, "\n"))
		}
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

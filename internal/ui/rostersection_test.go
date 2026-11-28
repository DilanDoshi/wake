package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// With no teams the only header is the no-team box's, on the first agent: no item
// carries a team's, and nothing but all comes before the agents.
func TestSectionRowsAddsOnlyTheNoTeamHeaderWithoutTeams(t *testing.T) {
	rows := sectionRows([]Agent{{ID: "a"}, {ID: "b"}})
	if len(rows) != 3 || !rows[0].all {
		t.Fatalf("sectionRows returned %d items for 2 agents, want all and one per agent", len(rows))
	}
	for i, item := range rows {
		if item.teamHeader() {
			t.Errorf("item %d draws a team's header for a fleet with no teams", i)
		}
		if want := i == 1; item.hasHeader() != want {
			t.Errorf("item %d has a header: %v, want %v (only the first agent heads the no-team box)", i, item.hasHeader(), want)
		}
	}
}

// A team draws a header before its first member, and a click on that header row
// is the header's - its channel, never an agent - while the row below it selects
// the member: the row-counting hazard fable named, checked by clicking the row
// under a header.
func TestRosterDrawsTeamHeadersAndAHeaderClickSelectsNoAgent(t *testing.T) {
	noSubs := func(string) []Task { return nil }
	f := (Fleet{}).WithStatus(&rpc.Status{
		Teams: []string{"backend"},
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", State: rpc.StateIdle},
			{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
			{ID: "s3", Name: "john", Team: "backend", State: rpc.StateIdle},
		},
	})
	agents := f.sectioned(f.OnRoster())
	r := Roster{}
	const width, height = 30, 20

	if out := r.View(agents, noSubs, width, height); !strings.Contains(out, "backend") {
		t.Errorf("the roster drew no `backend` team header:\n%s", out)
	}

	// Layout: row 0 all, row 1 the no-team header, row 2 alex (top block), row 3 its
	// box's closing row, row 4 a blank, row 5 the `backend` header, rows 6-7 thea,
	// john, row 8 the closing row.
	for _, y := range []int{1, 3, 4, 8} {
		if spot, ok := r.At(agents, noSubs, width, height, y); ok {
			t.Errorf("a click on row %d, a header that is no channel, a blank or a closing row, resolved to %+v; it belongs to no one", y, spot)
		}
	}
	if spot, ok := r.At(agents, noSubs, width, height, 5); !ok || spot.header != "backend" || spot.agent.ID != "" {
		t.Errorf("a click on row 5, the team header, resolved to (%+v, %v); want the backend header and no agent", spot, ok)
	}
	if spot, ok := r.At(agents, noSubs, width, height, 2); !ok || spot.agent.ID != "s1" {
		t.Errorf("row 2 = (%q, %v), want alex (s1) in the top block", spot.agent.ID, ok)
	}
	if spot, ok := r.At(agents, noSubs, width, height, 6); !ok || spot.agent.ID != "s2" {
		t.Errorf("row 6 = (%q, %v), want thea (s2) under the backend header", spot.agent.ID, ok)
	}
}

// A header is atomic with its section's first member at a scroll boundary: the
// window never keeps a header while dropping the member under it (dangling), nor
// keeps the member while dropping the header above it (dropped). This is the
// HIGH the reviewers found - my first tests used a column taller than the fleet,
// so the window never engaged. Here height 4 against 7 rows forces it.
func TestRosterHeaderIsAtomicWithItsMemberAtAScrollBoundary(t *testing.T) {
	noSubs := func(string) []Task { return nil }
	f := (Fleet{}).WithStatus(&rpc.Status{
		Teams: []string{"backend"},
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", State: rpc.StateIdle},
			{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
			{ID: "s3", Name: "john", Team: "backend", State: rpc.StateIdle},
		},
	})
	agents := f.sectioned(f.OnRoster())

	// Cursor on alex, height too small for thea's header+row: the header must not
	// be drawn with no member under it. (The old separate-header window drew
	// `alex` / `──── backend ────` / `+2 more` here.)
	out := Roster{Selected: "s1"}.View(agents, noSubs, 30, 4)
	if strings.Contains(out, "backend") && !strings.Contains(out, "thea") {
		t.Errorf("the backend header is drawn with no member under it (dangling):\n%s", out)
	}
	// Cursor on thea, same height: its header must come with it.
	out = Roster{Selected: "s2"}.View(agents, noSubs, 30, 4)
	if strings.Contains(out, "thea") && !strings.Contains(out, "backend") {
		t.Errorf("thea is drawn without its backend header (dropped):\n%s", out)
	}
}

// A team tag that reached the header despite the daemon's fence (a hand-edited
// park book) must not break the header's one-row geometry: the edge flattens it, so
// rowsForRow's count of one stays true and clicks below it do not shift.
func TestTeamHeaderLineIsAlwaysOneRow(t *testing.T) {
	if got := teamHeaderLine("back\nend\rmore", 40, HintStyle, HintStyle); strings.ContainsAny(got, "\n\r") {
		t.Errorf("teamHeaderLine kept a line break, so the header spans rows the count misses: %q", got)
	}
}

package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// With no teams no item carries a header, so the roster draws exactly the flat
// list it always has - the property that keeps every existing roster test green.
func TestSectionRowsAddsNoHeaderWithoutTeams(t *testing.T) {
	rows := sectionRows([]Agent{{ID: "a"}, {ID: "b"}})
	if len(rows) != 2 {
		t.Fatalf("sectionRows returned %d items for 2 agents, want one per agent", len(rows))
	}
	for _, item := range rows {
		if item.hasHeader() {
			t.Error("a header was drawn for a fleet with no teams")
		}
	}
}

// A team draws a header before its first member, and a click on that header row
// selects nothing while the row below it selects the member - the row-counting
// hazard fable named, checked by clicking the row under a header.
func TestRosterDrawsTeamHeadersAndAHeaderClickSelectsNothing(t *testing.T) {
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

	// Layout: row 0 alex (top block), row 1 the `backend` header, rows 2-3 thea, john.
	if _, _, ok := r.At(agents, noSubs, width, height, 1); ok {
		t.Error("a click on the team header row selected an agent; a header belongs to no agent")
	}
	if a, _, ok := r.At(agents, noSubs, width, height, 0); !ok || a.ID != "s1" {
		t.Errorf("row 0 = (%q, %v), want alex (s1) in the top block", a.ID, ok)
	}
	if a, _, ok := r.At(agents, noSubs, width, height, 2); !ok || a.ID != "s2" {
		t.Errorf("row 2 = (%q, %v), want thea (s2) under the backend header", a.ID, ok)
	}
}

// A header is atomic with its section's first member at a scroll boundary: the
// window never keeps a header while dropping the member under it (dangling), nor
// keeps the member while dropping the header above it (dropped). This is the
// HIGH the reviewers found - my first tests used a column taller than the fleet,
// so the window never engaged. Here height 3 against 4 rows forces it.
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
	out := Roster{Selected: "s1"}.View(agents, noSubs, 30, 3)
	if strings.Contains(out, "backend") && !strings.Contains(out, "thea") {
		t.Errorf("the backend header is drawn with no member under it (dangling):\n%s", out)
	}
	// Cursor on thea, same height: its header must come with it.
	out = Roster{Selected: "s2"}.View(agents, noSubs, 30, 3)
	if strings.Contains(out, "thea") && !strings.Contains(out, "backend") {
		t.Errorf("thea is drawn without its backend header (dropped):\n%s", out)
	}
}

// A team tag that reached the header despite the daemon's fence (a hand-edited
// park book) must not break the header's one-row geometry: teamHeaderLine flattens
// it, so rowsForRow's count of one stays true and clicks below it do not shift.
func TestTeamHeaderLineIsAlwaysOneRow(t *testing.T) {
	if got := teamHeaderLine("back\nend\rmore", 40); strings.ContainsAny(got, "\n\r") {
		t.Errorf("teamHeaderLine kept a line break, so the header spans rows the count misses: %q", got)
	}
}

package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// With no teams the roster adds no header row, so it draws exactly the flat list
// it always has - the property that keeps every existing roster test green.
func TestSectionRowsAddsNoHeaderWithoutTeams(t *testing.T) {
	rows := sectionRows([]Agent{{ID: "a"}, {ID: "b"}})
	if len(rows) != 2 {
		t.Fatalf("sectionRows added rows for a teamless fleet: got %d, want 2", len(rows))
	}
	for _, row := range rows {
		if row.isHeader() {
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

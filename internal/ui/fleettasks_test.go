package ui

import (
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// The sidebar draws every agent, and until now the dispatch fold lived on DM -
// which App.observe only reaches for a conversation somebody has opened. So an
// agent nobody had opened had no dispatches at all, which is precisely the row
// the roster has to draw.
func TestAFleetFoldsDispatchesForASessionNobodyHasOpened(t *testing.T) {
	f := NewFleet()
	for _, ev := range []core.Event{
		started("a1", "toolu_1", "Auditing the diff", "code-reviewer", core.TaskAgent),
		progressed("a1", "toolu_1", "Reading roster.go", "Read", 1100, 18*time.Second),
	} {
		f, _ = f.Observe(ev, "sess-1")
	}

	rows := f.Tasks("sess-1").Rows()
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 - no DM was ever opened for this session: %+v", len(rows), rows)
	}
	if rows[0].Type != "code-reviewer" {
		t.Errorf("Type = %q, want the subagent_type task_started carried", rows[0].Type)
	}
	if rows[0].Tokens != 1100 {
		t.Errorf("Tokens = %d, want 1100 - the figure the sidebar row spends its last columns on", rows[0].Tokens)
	}
}

// Each session keeps its own list. A fleet-wide fold would put sydney's
// subagents under john, which is the one thing a per-agent sidebar must not do.
func TestOneSessionsDispatchesDoNotReachAnother(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(started("a1", "toolu_1", "Auditing", "code-reviewer", core.TaskAgent), "sess-1")
	f, _ = f.Observe(started("b1", "toolu_2", "Searching", "general-purpose", core.TaskAgent), "sess-2")

	if got := len(f.Tasks("sess-1").Rows()); got != 1 {
		t.Errorf("sess-1 has %d rows, want its own one", got)
	}
	if got := len(f.Tasks("sess-2").Rows()); got != 1 {
		t.Errorf("sess-2 has %d rows, want its own one", got)
	}
	if f.Tasks("sess-1").Rows()[0].Type != "code-reviewer" {
		t.Error("sess-1 got the wrong session's dispatch")
	}
}

// Observe skips its own write when the folded Agent is unchanged - `if known &&
// now == was`. A task frame moves no field on Agent, so the fold has to happen
// on the side of that comparison where it cannot be skipped. Without this the
// first dispatch lands (the agent is new) and every frame after it is dropped.
func TestATaskFrameLandsEvenWhenItChangesNothingAboutTheAgent(t *testing.T) {
	f := NewFleet()
	// The agent exists and is settled before the dispatch arrives, so the
	// comparison below is the one that can skip.
	f, _ = f.Observe(started("a1", "toolu_1", "Auditing", "code-reviewer", core.TaskAgent), "sess-1")

	before, _ := f.Agent("sess-1")
	f, _ = f.Observe(progressed("a1", "toolu_1", "Reading roster.go", "Read", 4200, 20*time.Second), "sess-1")
	after, _ := f.Agent("sess-1")

	if before != after {
		t.Fatalf("the agent moved, so this test no longer exercises the early return: %+v -> %+v", before, after)
	}
	rows := f.Tasks("sess-1").Rows()
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Tokens != 4200 {
		t.Errorf("Tokens = %d, want 4200 - the progress frame was dropped by the unchanged-agent early return", rows[0].Tokens)
	}
}

// A Fleet is a value: a caller holding an older one keeps the fleet it had,
// which is the contract agents already keeps and the reason Observe copies.
func TestAnOlderFleetKeepsTheDispatchesItHad(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(started("a1", "toolu_1", "Auditing", "code-reviewer", core.TaskAgent), "sess-1")

	older := f
	f, _ = f.Observe(started("a2", "toolu_2", "Linting", "general-purpose", core.TaskAgent), "sess-1")

	if got := len(older.Tasks("sess-1").Rows()); got != 1 {
		t.Errorf("the older fleet has %d rows, want the 1 it was holding", got)
	}
	if got := len(f.Tasks("sess-1").Rows()); got != 2 {
		t.Errorf("the newer fleet has %d rows, want 2", got)
	}
}

// A session that has dispatched nothing answers an empty list rather than
// anything a caller has to guard against.
func TestASessionWithNoDispatchesAnswersAnEmptyList(t *testing.T) {
	if got := NewFleet().Tasks("nobody").Rows(); len(got) != 0 {
		t.Errorf("got %d rows for a session that has dispatched nothing, want none", len(got))
	}
}

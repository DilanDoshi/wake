package ui

import (
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// started, progressed and ended build the three frames a dispatch produces, so
// a table test reads as the sequence it is rather than as JSON.
func started(id, dispatch, label, agentType string, kind core.TaskKind) core.Event {
	return core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
		ID: id, Dispatch: dispatch, Kind: kind, Phase: core.TaskStarted,
		Status: core.TaskRunning, Label: label, Type: agentType,
	}}
}

func progressed(id, dispatch, label, tool string, tokens int, elapsed time.Duration) core.Event {
	return core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
		ID: id, Dispatch: dispatch, Kind: core.TaskKindUnknown, Phase: core.TaskProgress,
		Status: core.TaskRunning, Label: label, Tool: tool, Tokens: tokens, Elapsed: elapsed,
	}}
}

func ended(id string, status core.TaskStatus) core.Event {
	return core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
		ID: id, Kind: core.TaskKindUnknown, Phase: core.TaskEnded, Status: status,
	}}
}

func folded(evs ...core.Event) Tasks {
	var t Tasks
	for _, ev := range evs {
		t = t.Observe(ev)
	}
	return t
}

// The whole of an ordinary dispatch: one row, carrying what the newest frame
// said about it.
func TestOneDispatchIsOneRow(t *testing.T) {
	tasks := folded(
		started("a1", "toolu_1", "Count lines in alpha.txt", "general-purpose", core.TaskAgent),
		progressed("a1", "toolu_1", "Reading alpha.txt", "Read", 26984, 4075*time.Millisecond),
	)

	rows := tasks.Rows()
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.Label != "Reading alpha.txt" {
		t.Errorf("Label = %q, want the newest description - a row that froze at the prompt is not a status line", row.Label)
	}
	if row.Type != "general-purpose" {
		t.Errorf("Type = %q, want the type task_started carried - no later frame repeats it", row.Type)
	}
	if row.Tool != "Read" || row.Tokens != 26984 || row.Elapsed != 4075*time.Millisecond {
		t.Errorf("row = %+v, want the progress frame's tool, tokens and elapsed", row)
	}
	if row.Status != core.TaskRunning {
		t.Errorf("Status = %q, want %q", row.Status, core.TaskRunning)
	}
}

// The fold is pure: observing must not touch the value it was called on.
// Everything in this package that holds one keeps its own copy.
func TestObservingDoesNotMutateWhatItWasCalledOn(t *testing.T) {
	before := folded(started("a1", "toolu_1", "one", "general-purpose", core.TaskAgent))
	after := before.Observe(ended("a1", core.TaskDone))

	if got := before.Rows()[0].Status; got != core.TaskRunning {
		t.Errorf("the original row moved to %q: Observe mutated its receiver", got)
	}
	if got := after.Rows()[0].Status; got != core.TaskDone {
		t.Errorf("the returned row is %q, want %q", got, core.TaskDone)
	}
	if before.Running() != 1 || after.Running() != 0 {
		t.Errorf("Running() = %d before and %d after, want 1 and 0", before.Running(), after.Running())
	}
}

// Rows keep the order they started in. Two subagents interleave line by line,
// so an order derived from whichever spoke last would make the list jump under
// somebody reading it - and the cursor sits on a row by index.
func TestRowsKeepTheOrderTheyStartedIn(t *testing.T) {
	tasks := folded(
		started("a1", "toolu_1", "first", "general-purpose", core.TaskAgent),
		started("a2", "toolu_2", "second", "general-purpose", core.TaskAgent),
		progressed("a1", "toolu_1", "first still working", "Read", 10, time.Second),
		progressed("a2", "toolu_2", "second still working", "Bash", 20, time.Second),
		progressed("a1", "toolu_1", "first again", "Grep", 30, time.Second),
	)

	rows := tasks.Rows()
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].ID != "a1" || rows[1].ID != "a2" {
		t.Errorf("order = %q, %q; want a1 then a2 - the list must not reorder under a reader", rows[0].ID, rows[1].ID)
	}
}

// task_updated carries exactly task_id and patch: no dispatch, no label, no
// usage. Applied naively it blanks all three, and the row loses the key its
// transcript is filed under at the exact moment somebody wants to read it.
func TestAnEndingDoesNotBlankWhatItDoesNotCarry(t *testing.T) {
	tasks := folded(
		started("a1", "toolu_1", "Count lines", "general-purpose", core.TaskAgent),
		progressed("a1", "toolu_1", "Reading alpha.txt", "Read", 26984, 4075*time.Millisecond),
		ended("a1", core.TaskDone),
	)

	row := tasks.Rows()[0]
	if row.Dispatch != "toolu_1" {
		t.Errorf("Dispatch = %q, want it kept - it is the key the subagent's transcript is filed under", row.Dispatch)
	}
	if row.Label != "Reading alpha.txt" {
		t.Errorf("Label = %q, want the last thing it said", row.Label)
	}
	if row.Tokens != 26984 || row.Elapsed != 4075*time.Millisecond {
		t.Errorf("row = %+v, want the last usage reported - a finished row reading '0 tokens' unreports work that happened", row)
	}
	if row.Kind != core.TaskAgent {
		t.Errorf("Kind = %q, want %q - an ending names no task_type and must not downgrade one", row.Kind, core.TaskAgent)
	}
	if row.Status != core.TaskDone {
		t.Errorf("Status = %q, want %q", row.Status, core.TaskDone)
	}
}

// The same rule one frame earlier, and the one the corpus actually contains: 1
// of the 10 recorded task_notification frames carries no usage at all.
func TestAnEndingWithoutUsageKeepsTheLastOneReported(t *testing.T) {
	tasks := folded(
		started("a1", "toolu_1", "Count lines", "general-purpose", core.TaskAgent),
		progressed("a1", "toolu_1", "Reading", "Read", 29523, 5*time.Second),
		core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
			ID: "a1", Dispatch: "toolu_1", Phase: core.TaskEnded, Status: core.TaskDone,
		}},
	)

	if row := tasks.Rows()[0]; row.Tokens != 29523 || row.Elapsed != 5*time.Second {
		t.Errorf("row = %+v, want the usage the progress frame reported", row)
	}
}

// A progress frame names no task_type, so its Kind is TaskKindUnknown meaning
// "this frame did not say". Applied, it downgrades a known agent to a row
// nothing can open.
func TestProgressDoesNotDowngradeAKnownKind(t *testing.T) {
	tasks := folded(
		started("a1", "toolu_1", "Count lines", "general-purpose", core.TaskAgent),
		progressed("a1", "toolu_1", "Reading", "Read", 10, time.Second),
	)

	if row := tasks.Rows()[0]; row.Kind != core.TaskAgent {
		t.Errorf("Kind = %q, want %q", row.Kind, core.TaskAgent)
	}
}

// A shell is listed and is not openable; an unrecorded kind is listed and is
// not openable either. Only an agent with a dispatch has a transcript, and
// Openable is the one place that decides it.
func TestOnlyAnAgentWithADispatchCanBeOpened(t *testing.T) {
	cases := []struct {
		name string
		row  Task
		want bool
	}{
		{"agent", Task{Kind: core.TaskAgent, Dispatch: "toolu_1"}, true},
		{"shell", Task{Kind: core.TaskShell, Dispatch: "toolu_1"}, false},
		{"unknown kind", Task{Kind: core.TaskKindUnknown, Dispatch: "toolu_1"}, false},
		{"agent with no dispatch", Task{Kind: core.TaskAgent}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.row.Openable(); got != tc.want {
				t.Errorf("Openable() = %v, want %v - a row with no transcript must not offer one", got, tc.want)
			}
		})
	}
}

// A finished row stays listed. It is the one somebody is most likely to want
// to read, and its transcript is already in memory - dropping it at the moment
// it becomes readable is the complaint this whole change exists to fix.
func TestAFinishedRowStaysListed(t *testing.T) {
	tasks := folded(
		started("a1", "toolu_1", "Count lines", "general-purpose", core.TaskAgent),
		ended("a1", core.TaskDone),
	)

	if len(tasks.Rows()) != 1 {
		t.Fatalf("got %d rows, want the finished one kept", len(tasks.Rows()))
	}
	if tasks.Running() != 0 {
		t.Errorf("Running() = %d, want 0", tasks.Running())
	}
}

// Everything that is not a task frame leaves the set alone, including the
// forwarded frames the rows describe.
func TestNothingButATaskFrameChangesTheSet(t *testing.T) {
	tasks := folded(
		started("a1", "toolu_1", "Count lines", "general-purpose", core.TaskAgent),
		core.Event{Kind: core.KindAssistantText, Text: "hello"},
		core.Event{Kind: core.KindSystem, Text: "hook_started"},
		core.Event{Kind: core.KindToolUse, Subagent: &core.Subagent{Dispatch: "toolu_1"}},
		core.Event{Kind: core.KindTurnEnd},
	)

	rows := tasks.Rows()
	if len(rows) != 1 || rows[0].Status != core.TaskRunning {
		t.Errorf("rows = %+v, want the one running row untouched", rows)
	}
}

// A turn ending does not end a dispatch. An async subagent streams past its
// own result and past stdin closing, so a set cleared on KindTurnEnd - the way
// Agent.Tool is - retires a row for work that is still running.
func TestATurnEndingDoesNotRetireARow(t *testing.T) {
	tasks := folded(
		started("a1", "toolu_1", "Count lines", "general-purpose", core.TaskAgent),
		core.Event{Kind: core.KindTurnEnd},
	)

	if tasks.Running() != 1 {
		t.Errorf("Running() = %d, want 1 - the turn ended, the subagent did not", tasks.Running())
	}
}

// The lookup every lifecycle frame uses, by the one id all four of them carry
// and the rows are keyed on. A match on the dispatch id would be a scan
// returning whichever row carried it first.
func TestARowIsFoundByItsTaskID(t *testing.T) {
	tasks := folded(
		started("a1", "toolu_1", "first", "general-purpose", core.TaskAgent),
		started("a2", "toolu_2", "second", "general-purpose", core.TaskAgent),
	)

	row, ok := tasks.forID("a2")
	if !ok {
		t.Fatal("forID found nothing for a task that started")
	}
	if row.Dispatch != "toolu_2" {
		t.Errorf("Dispatch = %q, want toolu_2", row.Dispatch)
	}
	if _, ok := tasks.forID("absent"); ok {
		t.Error("forID invented a row for a task nothing announced")
	}
}

// The dispatch's name is set once and the live status moves, which is the whole
// of what separates the ending line from the row above it.
func TestTheDispatchNameIsSetOnceAndTheStatusMoves(t *testing.T) {
	tasks := folded(
		started("a1", "toolu_1", "Count lines in alpha.txt", "general-purpose", core.TaskAgent),
		progressed("a1", "toolu_1", "Reading beta.txt", "Read", 10, time.Second),
	)

	row := tasks.Rows()[0]
	if row.Name != "Count lines in alpha.txt" {
		t.Errorf("Name = %q, want the description the dispatch started with", row.Name)
	}
	if row.Label != "Reading beta.txt" {
		t.Errorf("Label = %q, want the newest status", row.Label)
	}
}

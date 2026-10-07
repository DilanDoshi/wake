package ui

// An idle agent with a background shell still running is not done: the ✔, the
// DM's `✻ … done` line and the strip's `N done` all read turnDone, and a shell
// reaches turnDone only through Fleet.RunningTasks.
//
// The shape is testdata/stream/interrupt-cancel-queued-empty.jsonl: the shell's
// task_started arrives mid-turn, the turn's result ends it, and the shell's
// ending frames come long after - so the agent sits idle with doneAt set.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const shellLabel = "make ci"

func shellStarted() core.Event {
	return started("b1", "toolu_9", shellLabel, "", core.TaskShell)
}

// Mutation check: put the shell back out of RunningTasks and every assertion
// about the running half goes red.
func TestAnAgentWithARunningShellIsNotDoneAndSaysSo(t *testing.T) {
	f, _ := finishedFleet().Observe(shellStarted(), "s1")

	if f.done("s1") {
		t.Error("Fleet.done: an idle agent with a shell still running reads done")
	}
	row := rosterOf(f)
	if strings.Contains(row, turnDoneGlyph) {
		t.Errorf("the roster marks an agent done while its shell runs:\n%s", row)
	}
	if !strings.Contains(row, "shell") || !strings.Contains(row, shellLabel) {
		t.Errorf("the roster does not say a shell is running:\n%s", row)
	}

	ag, _ := f.Agent("s1")
	if strip := stripANSI(awarenessStrip([]Agent{ag}, f.RunningTasks, "", 200)); strings.Contains(strip, "done") {
		t.Errorf("the strip counts an agent done while its shell runs: %q", strip)
	}
	d := NewDM("s1", "john")
	d.Agent = ag
	if d.WithRunningSub(len(f.RunningTasks("s1")) > 0).showsDone() {
		t.Error("the DM draws its done line while the shell runs")
	}
}

// The shell ending hands the agent back to done with its turn intact: nothing
// here touched doneAt, so the ✔ and the line return as they were.
func TestTheDoneMarkReturnsWhenTheShellEnds(t *testing.T) {
	f, _ := finishedFleet().Observe(shellStarted(), "s1")
	f, _ = f.Observe(ended("b1", core.TaskDone), "s1")

	if !f.done("s1") {
		t.Error("Fleet.done: the agent did not return to done after its shell ended")
	}
	if row := rosterOf(f); !strings.Contains(row, turnDoneGlyph+" john") || strings.Contains(row, shellLabel) {
		t.Errorf("the roster still draws the ended shell, or lost the ✔:\n%s", row)
	}
	if ag, _ := f.Agent("s1"); ag.doneAt.IsZero() {
		t.Error("doneAt was lost across the shell - the returned done line would be blank")
	}
}

// shellFleet is alex with a subagent and a shell running, beside an agent with
// neither: the cursor has a real stop to land on before and after the shell row.
func shellFleet() Fleet {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
		{ID: "s2", Name: "bea", State: rpc.StateIdle},
	}})
	f, _ = f.Observe(started("a1", "toolu_1", "Audit the diff", "code-reviewer", core.TaskAgent), "s1")
	f, _ = f.Observe(shellStarted(), "s1")
	return f
}

// A shell has nothing behind it to open: the cursor walks past its row, and a
// click on it is a click on its agent - never a dispatch id for viewingPicked to
// open an empty pane with.
func TestTheCursorAndAClickPassOverAShellRow(t *testing.T) {
	f := shellFleet()
	agents := f.OnRoster()

	sawSubagent := false
	for _, stop := range walkable(agents, f.RunningTasks) {
		if stop.SelectedTask == shellStarted().Task.Dispatch {
			t.Errorf("the cursor can land on the shell row: %+v", stop)
		}
		sawSubagent = sawSubagent || stop.SelectedTask == "toolu_1"
	}
	if !sawSubagent {
		t.Error("the subagent row lost its cursor stop")
	}

	lines := strings.Split(stripANSI(Roster{}.View(agents, f.RunningTasks, rosterWidth, 10)), "\n")
	for y, line := range lines {
		if !strings.Contains(line, shellLabel) {
			continue
		}
		a, dispatch, ok := Roster{}.At(agents, f.RunningTasks, rosterWidth, 10, y)
		if !ok || a.ID != "s1" || dispatch != "" {
			t.Errorf("a click on the shell row = (%q, %q, %v), want its agent and no dispatch", a.ID, dispatch, ok)
		}
		return
	}
	t.Errorf("the roster drew no shell row:\n%s", strings.Join(lines, "\n"))
}

// The board's row view reads the same list for its height, its draw and its
// click, so the shell row lands in all three - and clicks like the sidebar's.
func TestABoardClickOnAShellRowIsAClickOnItsAgent(t *testing.T) {
	a := newRoomApp(t).withSize(120, 30)
	a = a.applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
		{ID: "s2", Name: "bea", State: rpc.StateIdle},
	}})
	a = a.applyFrame(taskFrame("s1", shellStarted()))
	agents := a.boardAgents()

	for y, line := range strings.Split(stripANSI(a.boardView(agents, 120)), "\n") {
		if !strings.Contains(line, shellLabel) {
			continue
		}
		i, dispatch, ok := a.boardHit(0, y, agents)
		if !ok || agents[i].ID != "s1" || dispatch != "" {
			t.Errorf("a board click on the shell row = (%d, %q, %v), want alex and no dispatch", i, dispatch, ok)
		}
		return
	}
	t.Error("the board drew no shell row")
}

// A tile counts what it runs by kind: a shell is not a subagent.
func TestATileCountsAShellAsAShell(t *testing.T) {
	shell := Task{Kind: core.TaskShell, Status: core.TaskRunning}
	agent := Task{Kind: core.TaskAgent, Status: core.TaskRunning, Dispatch: "toolu_1"}
	for _, tc := range []struct {
		name    string
		running []Task
		want    string
	}{
		{"a shell alone", []Task{shell}, "⤷ 1 shell"},
		{"a shell and a subagent", []Task{agent, shell}, "⤷ 1 subagent · 1 shell"},
		{"two shells", []Task{shell, shell}, "⤷ 2 shells"},
	} {
		if got := stripANSI(tileSubagents(tc.running, 60)); got != tc.want {
			t.Errorf("%s: tile says %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A park or an end kills the process group, so no task_ended ever arrives for
// its shell: the row must go with the report, or a woken agent - the same session
// id - carries a phantom shell and never reads done again.
func TestAParkOrAnEndForgetsTheShellsThatDiedWithTheProcess(t *testing.T) {
	for _, state := range []string{rpc.StateParked, rpc.StateEnded} {
		f, _ := finishedFleet().Observe(shellStarted(), "s1")
		f = f.WithStatus(report("s1", "john", state))
		if got := f.RunningTasks("s1"); len(got) != 0 {
			t.Errorf("after a %s report the fleet still lists %+v", state, got)
		}
	}
}

// The turn this client watches on an idle agent with a shell running, end to end
// through the reports the daemon pushes: the check goes while the agent works,
// stays gone while the shell runs, and returns with the done time moved on to
// the new turn once the shell ends.
func TestARoomTurnOnAnAgentWithAShellMovesTheDoneTimeOnceTheShellEnds(t *testing.T) {
	start := time.Date(2026, 10, 6, 20, 34, 0, 0, time.Local)
	clock = func() time.Time { return start }
	defer func() { clock = time.Now }()

	f, _ := finishedFleet().Observe(shellStarted(), "s1")
	first, _ := f.Agent("s1")

	clock = func() time.Time { return start.Add(7 * time.Minute) }
	f = f.WithStatus(report("s1", "john", rpc.StateWorking))
	if row := rosterOf(f); strings.Contains(row, turnDoneGlyph) {
		t.Errorf("the check stayed up while the agent worked:\n%s", row)
	}

	clock = func() time.Time { return start.Add(7*time.Minute + 9*time.Second) }
	f = f.WithStatus(report("s1", "john", rpc.StateIdle))
	if f.done("s1") {
		t.Error("the agent read done at the end of a turn while its shell still ran")
	}

	f, _ = f.Observe(ended("b1", core.TaskDone), "s1")
	second, _ := f.Agent("s1")
	if !f.done("s1") || !second.doneAt.After(first.doneAt) {
		t.Errorf("done = %v, doneAt %v -> %v: want done again with the time moved to the new turn", f.done("s1"), first.doneAt, second.doneAt)
	}
}

// decodedFixture is a recorded stream through the real decoder, in arrival order.
// It fails on a line it cannot decode: a reader that skipped them would turn the
// assertions below into ones over nothing.
func decodedFixture(t *testing.T, name string) []core.Event {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "stream", name+".jsonl"))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	var out []core.Event
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		evs, err := core.DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("%s: decoding %q: %v", name, line, err)
		}
		out = append(out, evs...)
	}
	return out
}

// A shell leaves RunningTasks only when a frame says it ended. ⌃] (send-now)
// moves a foreground Bash to the background with a task_updated that patches
// is_backgrounded and names no status - not an ending - and the turn then ends
// with the shell still running; claude kills it only when its stdin closes, in
// the frames after the result. Replayed whole through the decoder and the fold,
// the way the daemon reports it: working for the turn, idle at its result.
func TestAShellMovedToTheBackgroundStaysRunningThroughTheRecordedTurn(t *testing.T) {
	for _, name := range []string{"midturn-now-human", "midturn-recall-now"} {
		evs := decodedFixture(t, name)
		last := -1
		for i, ev := range evs {
			if ev.Kind == core.KindTurnEnd {
				last = i
			}
		}
		if last < 0 {
			t.Fatalf("%s: no turn end in the recording", name)
		}

		f := NewFleet().WithStatus(report("s1", "john", rpc.StateIdle)).WithStatus(report("s1", "john", rpc.StateWorking))
		for _, ev := range evs[:last+1] {
			f, _ = f.Observe(ev, "s1")
		}
		f = f.WithStatus(report("s1", "john", rpc.StateIdle))

		rows := f.RunningTasks("s1")
		if len(rows) != 1 || rows[0].Kind != core.TaskShell {
			t.Errorf("%s: after the turn's result the running rows are %+v, want the one backgrounded shell", name, rows)
		}
		if f.done("s1") {
			t.Errorf("%s: the agent reads done with its backgrounded shell still running", name)
		}
	}
}

// The board marks the cursor by comparing dispatch ids, and a shell row carrying
// none would match the agent's own "no dispatch selected" - two rows marked.
func TestABoardShellRowWithNoDispatchIsNeverTheCursor(t *testing.T) {
	a := newRoomApp(t).withSize(120, 30)
	a = a.applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateIdle}}})
	a = a.applyFrame(taskFrame("s1", started("b1", "", shellLabel, "", core.TaskShell)))
	a.board.Selected = "s1"

	out := stripANSI(a.boardView(a.boardAgents(), 120))
	if !strings.Contains(out, shellLabel) {
		t.Fatalf("the board drew no shell row:\n%s", out)
	}
	if n := strings.Count(out, cardCursor); n != 1 {
		t.Errorf("the board marks %d rows as the cursor, want only the agent's:\n%s", n, out)
	}
}

// The wiring the done line reads: dmFor marks the pane from Fleet.RunningTasks, so
// a shell suppresses the line in the drawn conversation and not only in a DM built
// by hand.
func TestDmForMarksAnAgentWithARunningShell(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withSize(120, 40)
	if a.dmFor("s1").subRunning {
		t.Fatal("baseline: an agent with nothing running was marked as running something")
	}
	a = a.applyFrame(taskFrame("s1", shellStarted()))
	if !a.dmFor("s1").subRunning {
		t.Error("dmFor did not mark an agent whose background shell is running")
	}
}

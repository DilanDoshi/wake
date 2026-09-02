package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestDonePoolIsWellFormed(t *testing.T) {
	// minDoneWords is the repetition floor. Far lower than the working pool's:
	// only the DM's own agent draws a done word, one at a time, so a repeat is
	// neither side-by-side nor frequent.
	const minDoneWords = 40

	if len(doneWords) < minDoneWords {
		t.Errorf("pool has %d words, want at least %d", len(doneWords), minDoneWords)
	}

	seen := make(map[string]bool, len(doneWords))
	for i, w := range doneWords {
		switch {
		case w == "":
			t.Errorf("word %d is empty", i)
		case seen[w]:
			t.Errorf("word %d %q is a duplicate; it costs a slot and doubles its own odds", i, w)
		case !strings.HasSuffix(w, "ed"):
			t.Errorf("word %d %q is not a past tense that reads after \"for\"", i, w)
		case strings.TrimSpace(w) != w:
			t.Errorf("word %d %q has surrounding space", i, w)
		case strings.ToUpper(w[:1]) != w[:1]:
			t.Errorf("word %d %q is not capitalised", i, w)
		}
		seen[w] = true
	}
}

func TestDoneLineReadsLikeTheScreenshot(t *testing.T) {
	started := time.Date(2026, 8, 28, 18, 46, 1, 0, time.Local)
	done := started.Add(1*time.Minute + 59*time.Second)
	line := doneLine("s1", started, done, done.Sub(started), 80)
	plain := ansi.Strip(line)
	if !strings.Contains(plain, "for 1m 59s") {
		t.Errorf("done line %q is missing the elapsed clause", plain)
	}
	if !strings.Contains(plain, "done 6:48 PM") {
		t.Errorf("done line %q is missing the wall-clock done time", plain)
	}
	if !strings.HasPrefix(plain, doneGlyph+" ") {
		t.Errorf("done line %q does not open with the %q glyph", plain, doneGlyph)
	}
}

func TestDoneLineIsEmptyWithNoCompletedTurn(t *testing.T) {
	if got := doneLine("s1", time.Time{}, time.Time{}, 0, 80); got != "" {
		t.Errorf("doneLine = %q, want empty: nothing has finished", got)
	}
}

// A width too narrow for the whole line drops the done-time clause rather than
// cutting mid-word, the way the working line drops its meta.
func TestDoneLineDropsTheDoneTimeBeforeCuttingTheWord(t *testing.T) {
	started := time.Date(2026, 8, 28, 18, 46, 1, 0, time.Local)
	done := started.Add(1*time.Minute + 59*time.Second)
	dur := done.Sub(started)
	full := ansi.Strip(doneLine("s1", started, done, dur, 200))
	if !strings.Contains(full, "done") {
		t.Fatalf("the full-width line %q has no done clause to drop", full)
	}
	// Just short of the whole line: the fixed done clause cannot fit, so it drops
	// whole rather than cutting the word - derived from the full width so the
	// seeded word's own length does not decide the test.
	narrow := ansi.Strip(doneLine("s1", started, done, dur, ansi.StringWidth(full)-5))
	if strings.Contains(narrow, "done") {
		t.Errorf("narrow done line %q kept the done-time clause", narrow)
	}
	if !strings.Contains(narrow, "for 1m 59s") {
		t.Errorf("narrow done line %q lost the elapsed clause it should keep first", narrow)
	}
}

// report is one status push carrying a single session in one state.
func report(id, name, state string) *rpc.Status {
	return &rpc.Status{Sessions: []rpc.SessionStatus{{ID: id, Name: name, State: state}}}
}

func TestATurnEndRecordsTheDoneTimeAndDuration(t *testing.T) {
	start := time.Date(2026, 8, 28, 18, 46, 1, 0, time.Local)
	clock = func() time.Time { return start }
	defer func() { clock = time.Now }()

	// Seen idle, then a start this client watches, then the end 1m59s later.
	f := NewFleet().WithStatus(report("s1", "alex", rpc.StateIdle))
	f = f.WithStatus(report("s1", "alex", rpc.StateWorking))
	clock = func() time.Time { return start.Add(1*time.Minute + 59*time.Second) }
	f = f.WithStatus(report("s1", "alex", rpc.StateIdle))

	a, ok := f.Agent("s1")
	if !ok {
		t.Fatal("no agent after the turn")
	}
	if a.doneAt.IsZero() {
		t.Fatal("the working→idle edge recorded no done time")
	}
	if a.turnDur != 1*time.Minute+59*time.Second {
		t.Errorf("turnDur = %v, want 1m59s", a.turnDur)
	}
}

// A turn that ran through a permission (blocked) before finishing still records
// a done time: turnInFlight covers blocked, and the start is the one this client
// watched, so the duration spans the whole turn.
func TestABlockedTurnStillRecordsADoneTime(t *testing.T) {
	start := time.Date(2026, 8, 28, 18, 46, 1, 0, time.Local)
	clock = func() time.Time { return start }
	defer func() { clock = time.Now }()

	f := NewFleet().WithStatus(report("s1", "alex", rpc.StateIdle))
	f = f.WithStatus(report("s1", "alex", rpc.StateWorking))
	f = f.WithStatus(report("s1", "alex", rpc.StateBlocked))
	clock = func() time.Time { return start.Add(30 * time.Second) }
	f = f.WithStatus(report("s1", "alex", rpc.StateIdle))

	if a, _ := f.Agent("s1"); a.doneAt.IsZero() || a.turnDur != 30*time.Second {
		t.Errorf("blocked→idle recorded doneAt=%v turnDur=%v, want a 30s turn", !a.doneAt.IsZero(), a.turnDur)
	}
}

// An agent already working when this client attached - its first report is
// working - has an unwatched start, so it gets no done line rather than one
// whose duration is really just the time since attachment.
func TestAnAgentWorkingAtAttachGetsNoDoneLine(t *testing.T) {
	f := NewFleet().WithStatus(report("s1", "alex", rpc.StateWorking)) // first ever report
	f = f.WithStatus(report("s1", "alex", rpc.StateIdle))
	if a, _ := f.Agent("s1"); !a.doneAt.IsZero() {
		t.Error("a turn already in flight at attach produced a done line with a fabricated duration")
	}
}

func TestAnAgentIdleWithNoWatchedTurnRecordsNoDoneTime(t *testing.T) {
	f := NewFleet().WithStatus(report("s1", "alex", rpc.StateIdle))
	if a, _ := f.Agent("s1"); !a.doneAt.IsZero() {
		t.Error("an agent seen only idle recorded a done time")
	}
}

// A park forgets the finished turn: a resumed session reports idle directly
// (parked→idle, no working in between), so without clearing on the park the
// stale summary would reappear the moment the pane reopened.
func TestAParkForgetsTheDoneLine(t *testing.T) {
	start := time.Date(2026, 8, 28, 18, 46, 1, 0, time.Local)
	clock = func() time.Time { return start }
	defer func() { clock = time.Now }()

	f := NewFleet().WithStatus(report("s1", "alex", rpc.StateIdle))
	f = f.WithStatus(report("s1", "alex", rpc.StateWorking))
	clock = func() time.Time { return start.Add(time.Minute) }
	f = f.WithStatus(report("s1", "alex", rpc.StateIdle))
	if a, _ := f.Agent("s1"); a.doneAt.IsZero() {
		t.Fatal("no done time to forget")
	}

	f = f.WithStatus(report("s1", "alex", rpc.StateParked)) // ⌃C
	f = f.WithStatus(report("s1", "alex", rpc.StateIdle))   // /resume → idle directly
	if a, _ := f.Agent("s1"); !a.doneAt.IsZero() {
		t.Error("a woken session shows the pre-park turn's done line")
	}
}

// The daemon reports idle for a turn Wake did not initiate: --brief lets an
// agent self-start, and a long job goes idle between turns, so stateLocked
// returns idle (`!owed`) while the agent works and the working report that
// normally replaces the done line never arrives (agent.go's header,
// deferred.md). Here the event stream is that turn's only observable, so the
// agent's own new-turn content must forget the stale done summary - otherwise a
// pane shows `✻ … done 10:41 PM` while tools stream in below it.
func TestNewTurnProseForgetsTheDoneLineWhileStillReportedIdle(t *testing.T) {
	f := fleetWithFinishedTurn(t)
	f, _ = f.Observe(core.Event{Kind: core.KindAssistantText, Text: "Now in the worktree.", SessionID: "s1"}, "s1")
	if a, _ := f.Agent("s1"); !a.doneAt.IsZero() {
		t.Error("the agent's own new-turn prose left the stale done line standing")
	}
}

func TestNewTurnToolUseForgetsTheDoneLineWhileStillReportedIdle(t *testing.T) {
	f := fleetWithFinishedTurn(t)
	f, _ = f.Observe(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{Name: "Read", Display: "roster.go"}, SessionID: "s1"}, "s1")
	if a, _ := f.Agent("s1"); !a.doneAt.IsZero() {
		t.Error("the agent's own new-turn tool call left the stale done line standing")
	}
}

// Reasoning lands before the prose and its deltas are dropped (no preview
// covers it), so the agent's own thinking block is the earliest signal a
// self-started turn is underway - it must forget the stale done line too.
func TestNewTurnThinkingForgetsTheDoneLineWhileStillReportedIdle(t *testing.T) {
	f := fleetWithFinishedTurn(t)
	f, _ = f.Observe(core.Event{Kind: core.KindThinking, Text: "Let me read the test patterns first.", SessionID: "s1"}, "s1")
	if a, _ := f.Agent("s1"); !a.doneAt.IsZero() {
		t.Error("the agent's own new-turn reasoning left the stale done line standing")
	}
}

// The user's report: it also strands a done line after accepting a permission or
// answering a question. On an unowed turn (daemon reports idle, `!owed`) a
// permission is StateBlocked; answering clears the ask and flips it blocked→idle,
// which WithStatus captures a done line on though the turn is continuing. The
// granted tool's own result forgets it - otherwise the summary stands for the
// whole length of that tool (a long bash), the "done" over live work all over.
func TestAGrantedToolResultForgetsADoneLineCapturedOnBlockedToIdle(t *testing.T) {
	start := time.Date(2026, 8, 28, 18, 46, 1, 0, time.Local)
	clock = func() time.Time { return start }
	defer func() { clock = time.Now }()

	f := NewFleet().WithStatus(report("s1", "alex", rpc.StateIdle))
	f = f.WithStatus(report("s1", "alex", rpc.StateWorking))
	f = f.WithStatus(report("s1", "alex", rpc.StateBlocked))
	clock = func() time.Time { return start.Add(30 * time.Second) }
	f = f.WithStatus(report("s1", "alex", rpc.StateIdle)) // answered → blocked→idle captures a done line
	if a, _ := f.Agent("s1"); a.doneAt.IsZero() {
		t.Fatal("setup: blocked→idle should have captured a done time")
	}

	f, _ = f.Observe(core.Event{Kind: core.KindToolResult, Text: "ok", SessionID: "s1"}, "s1")
	if a, _ := f.Agent("s1"); !a.doneAt.IsZero() {
		t.Error("the granted tool's result left a done line standing over a continuing turn")
	}
}

// A subagent's frame is not the agent's own turn - it streams past the parent's
// result (the ev.Subagent==nil gate fold keeps for tool activity everywhere) -
// so it must not forget the parent's done line.
func TestASubagentsActivityKeepsTheDoneLine(t *testing.T) {
	f := fleetWithFinishedTurn(t)
	f, _ = f.Observe(core.Event{
		Kind:      core.KindToolUse,
		Tool:      &core.ToolCall{Name: "Read", Display: "roster.go"},
		Subagent:  &core.Subagent{},
		SessionID: "s1",
	}, "s1")
	if a, _ := f.Agent("s1"); a.doneAt.IsZero() {
		t.Error("a subagent's tool call forgot the parent's done line")
	}
}

// fleetWithFinishedTurn drives one watched turn to its idle end so s1 carries a
// done summary, then leaves the clock fixed for the caller.
func fleetWithFinishedTurn(t *testing.T) Fleet {
	t.Helper()
	start := time.Date(2026, 8, 28, 18, 46, 1, 0, time.Local)
	clock = func() time.Time { return start }
	t.Cleanup(func() { clock = time.Now })

	f := NewFleet().WithStatus(report("s1", "alex", rpc.StateIdle))
	f = f.WithStatus(report("s1", "alex", rpc.StateWorking))
	clock = func() time.Time { return start.Add(time.Minute) }
	f = f.WithStatus(report("s1", "alex", rpc.StateIdle))
	if a, _ := f.Agent("s1"); a.doneAt.IsZero() {
		t.Fatal("setup: no done time to forget")
	}
	return f
}

// A self-started turn (daemon still reporting idle) can begin with a streaming
// preview before its first completed block lands to fire notDone. While that
// preview is up the pane must not also draw the prior turn's done line - a
// sentence being written above `✻ … done` is the same contradiction one step
// earlier. showsDone is gated to a quiet pane for it.
func TestAStreamingPreviewHidesTheDoneLine(t *testing.T) {
	start := time.Date(2026, 8, 28, 18, 46, 1, 0, time.Local)
	d := NewDM("s1", "alex").SetSize(80, 30)
	d.Agent = Agent{State: rpc.StateIdle, startedAt: start, doneAt: start.Add(time.Minute), turnDur: time.Minute}
	d.partial = d.partial.add("Now in the worktree, reading the test patterns")
	if got := ansi.Strip(d.heartbeat()); got != "" {
		t.Errorf("done line %q drew over a live streaming preview", got)
	}
	// Once the preview clears (block landed, or turn ended), the done line returns.
	d.partial = d.partial.cleared()
	if got := ansi.Strip(d.heartbeat()); !strings.Contains(got, "done") {
		t.Errorf("done line did not return after the preview cleared: %q", got)
	}
}

func TestDMHeartbeatIsTheDoneLineWhenIdleAfterATurn(t *testing.T) {
	start := time.Date(2026, 8, 28, 18, 46, 1, 0, time.Local)
	d := NewDM("s1", "alex").SetSize(80, 30)

	d.Agent = Agent{State: rpc.StateIdle, startedAt: start, doneAt: start.Add(2 * time.Minute), turnDur: 2 * time.Minute}
	got := ansi.Strip(d.heartbeat())
	if !strings.Contains(got, "for 2m 0s") || !strings.Contains(got, "done") {
		t.Errorf("idle DM heartbeat = %q, want a done line", got)
	}

	d.Agent.doneAt = time.Time{}
	if d.heartbeat() != "" {
		t.Errorf("an idle agent with no finished turn drew %q", d.heartbeat())
	}

	d.Agent = Agent{State: rpc.StateWorking, startedAt: start}
	if beat := ansi.Strip(d.heartbeat()); !strings.Contains(beat, heartbeatEllipsis) {
		t.Errorf("a working agent drew %q, want the working line", beat)
	}
}

// The done line takes the same chrome as the working line: the line itself plus
// the blank row above it, two rows over idle. If they were not budgeted, the
// pane would be sized short and the alt screen would scroll.
func TestTheDoneLineCostsAChromeRow(t *testing.T) {
	start := time.Now()
	d := NewDM("s1", "alex").SetSize(80, 30)

	d.Agent = Agent{State: rpc.StateIdle}
	without := d.chromeHeight()

	d.Agent = Agent{State: rpc.StateIdle, startedAt: start, doneAt: start.Add(time.Minute), turnDur: time.Minute}
	with := d.chromeHeight()

	if with != without+2 {
		t.Errorf("chromeHeight without the done line = %d, with = %d: it should cost its line and the blank above it", without, with)
	}
}

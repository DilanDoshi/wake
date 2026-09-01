package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func compactStart(id string) core.Event {
	return core.Event{Kind: core.KindSystem, Notice: core.NoticeCompacting, SessionID: id}
}

func compactEnd(id string) core.Event {
	return core.Event{Kind: core.KindSystem, Notice: core.NoticeCompacted, SessionID: id}
}

// The compacting line names the work and nothing else - the wire carries no
// progress figure, so there is no percentage to draw, only Claude Code's own
// `Compacting conversation…` and the shimmer that says it is alive.
func TestCompactingLineNamesTheWork(t *testing.T) {
	forceTrueColour(t)
	got := stripANSI(compactingLine(clock().Add(-2*time.Second), 40))
	if !strings.Contains(got, "Compacting conversation") {
		t.Errorf("compacting line = %q, want it to name the work", got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("compacting line = %q, want Claude's ellipsis", got)
	}
}

// Nothing to draw when nothing is compacting.
func TestCompactingLineIsEmptyWithoutAStart(t *testing.T) {
	if got := compactingLine(time.Time{}, 40); got != "" {
		t.Errorf("a zero start drew %q, want nothing", got)
	}
}

// The bracketing notices drive the working line and nothing else: they leave no
// block in the transcript. The pinned compacting line already says the session
// is compacting, so a bare "compacting" row in the scrollback would be a
// duplicate - which is exactly what a noticeLabel entry for either would add.
func TestCompactionNoticesLeaveNoTranscriptBlock(t *testing.T) {
	forceTrueColour(t)
	d := NewDM("s1", "alex").SetSize(80, 24)
	for _, ev := range []core.Event{compactStart("s1"), compactEnd("s1")} {
		if b := d.renderEvent(ev); strings.TrimSpace(b.text) != "" {
			t.Errorf("notice %q drew a transcript block %q, want none", ev.Notice, b.text)
		}
	}
}

// idleDoneAgent is idle with a finished turn - the state that draws the done
// line, which the compacting line must take precedence over.
func idleDoneAgent() Agent {
	return Agent{ID: "s1", State: rpc.StateIdle, startedAt: clock().Add(-time.Minute), doneAt: clock(), turnDur: time.Minute}
}

// While a compaction runs the DM draws the compacting line above its composer,
// in place of the done line an idle turn would otherwise show. The agent is idle
// (each of a compaction's several result frames clears the turn), so without
// this the pane shows `✻ Cooked …` while it is plainly still compacting.
func TestTheDMDrawsTheCompactingLineOverTheDoneLine(t *testing.T) {
	forceTrueColour(t)
	d := NewDM("s1", "alex").SetSize(80, 20)
	d.Agent = idleDoneAgent()
	d = d.WithCompacting(clock().Add(-3 * time.Second))

	if beat := stripANSI(d.heartbeat()); !strings.Contains(beat, "Compacting conversation") {
		t.Errorf("heartbeat = %q, want the compacting line", beat)
	}
	if !d.hasBeat() {
		t.Error("hasBeat is false while compacting; the row is unbudgeted and the alt screen scrolls")
	}
}

// The compacting row grows the chrome *after* the pane was sized - it is set for
// the draw by dmPane, not at a resize, exactly like the heartbeat row - so View
// has to re-size for it or draw a row past what it was given and scroll the alt
// screen away. Sized while not compacting, then told it is, then drawn.
func TestTheCompactingRowStaysInBounds(t *testing.T) {
	forceTrueColour(t)
	const w, h = 60, 20
	d := NewDM("s1", "alex").SetSize(w, h)
	d.Agent = Agent{ID: "s1", State: rpc.StateIdle}
	d = d.WithCompacting(clock().Add(-2 * time.Second))
	if got := lipgloss.Height(d.View(w, h)); got > h {
		t.Errorf("a compacting conversation drew %d rows into a pane of %d", got, h)
	}
}

// A client that attached mid-compaction missed the start flag, so the outcome
// clears nothing and must not invent a compaction by touching the map.
func TestAnOutcomeWithoutAStartIsANoOp(t *testing.T) {
	var a App
	a = a.observeCompaction("s1", compactEnd("s1"))
	if a.anyCompacting() {
		t.Error("an outcome with no start marked the session compacting")
	}
}

// A DM not marked compacting is unaffected: the done line still shows.
func TestTheDMKeepsTheDoneLineWhenNotCompacting(t *testing.T) {
	forceTrueColour(t)
	d := NewDM("s1", "alex")
	d.Agent = idleDoneAgent()
	if beat := stripANSI(d.heartbeat()); strings.Contains(beat, "Compacting") {
		t.Errorf("heartbeat = %q, want the done line, not a compacting line", beat)
	}
}

// The start flag records when a session's compaction began; the outcome frame
// clears it. Both are the notices the airlock resolves the two status frames to.
func TestObserveCompactionBracketsTheRun(t *testing.T) {
	start := clock()
	restore := clock
	clock = func() time.Time { return start }
	defer func() { clock = restore }()

	var a App
	a = a.observeCompaction("s1", compactStart("s1"))
	if !a.anyCompacting() {
		t.Fatal("no agent compacting after the start flag")
	}
	if got := a.compactingSince("s1"); got != start {
		t.Errorf("compactingSince = %v, want the start %v", got, start)
	}

	a = a.observeCompaction("s1", compactEnd("s1"))
	if a.anyCompacting() {
		t.Error("still compacting after the outcome frame")
	}
	if !a.compactingSince("s1").IsZero() {
		t.Error("compactingSince not cleared after the end")
	}
}

// An ordinary event moves nothing - including a result frame, which a compaction
// emits several of while it runs. Clearing on one would drop the line the moment
// compaction started.
func TestObserveCompactionIgnoresOrdinaryEvents(t *testing.T) {
	var a App
	a = a.observeCompaction("s1", compactStart("s1"))
	a = a.observeCompaction("s1", core.Event{Kind: core.KindTurnEnd, SessionID: "s1"})
	if !a.anyCompacting() {
		t.Error("a turn end cleared the compacting state; a compaction's own result frames would drop the line")
	}
}

// End to end: a compacting frame arriving for an open, idle conversation makes
// that pane draw the compacting line - observe folds the map, dmPane reads it
// through WithCompacting. Both wirings have to be in place, and the agent is
// idle throughout (a compaction runs between turns).
func TestACompactingFrameDrawsTheLineInItsPane(t *testing.T) {
	forceTrueColour(t)
	a := newRoomApp(t).withAgents("sydney").withSize(200, 40).openDMWith("s1", "sydney")

	a = a.observe("s1", compactStart("s1"))
	if got := stripANSI(a.dmPane("s1", 100, 30)); !strings.Contains(got, "Compacting conversation") {
		t.Errorf("the pane did not draw the compacting line while compacting:\n%s", got)
	}

	a = a.observe("s1", compactEnd("s1"))
	if got := stripANSI(a.dmPane("s1", 100, 30)); strings.Contains(got, "Compacting conversation") {
		t.Errorf("the pane still drew the compacting line after the outcome:\n%s", got)
	}
}

// The shimmer keeps moving while a compaction runs, even though no turn is in
// flight - the agent is idle. So the ticker treats a compacting session like a
// working one for the single question it asks: is anything still animating.
func TestTheTickerBeatsWhileCompacting(t *testing.T) {
	ticks := countTicks(t)
	var a App
	a.fleet = NewFleet()
	a = a.observeCompaction("s1", compactStart("s1"))
	if _, cmd := a.beat(); cmd == nil {
		t.Fatal("beat scheduled nothing while a session was compacting")
	}
	if *ticks != 1 {
		t.Errorf("a compacting session scheduled %d ticks, want 1", *ticks)
	}
}

// And it stops when the compaction ends, the same as when a turn does - nothing
// else is working, so the loop must not run for the life of the process.
func TestTheTickerStopsWhenCompactionEnds(t *testing.T) {
	countTicks(t)
	var a App
	a.fleet = NewFleet()
	a = a.observeCompaction("s1", compactStart("s1"))
	a, _ = a.beat()
	a = a.observeCompaction("s1", compactEnd("s1"))
	a.beating = false
	if _, cmd := a.beat(); cmd != nil {
		t.Error("the ticker rescheduled after compaction ended and nothing else was working")
	}
}

// A compaction cut short - the session ends before its outcome frame - is
// dropped on the next report, so the line does not stand and the ticker does not
// run forever. The outcome notice clears a normal compaction; this is the
// backstop for the one that never sends it.
func TestAnEndedSessionStopsCompacting(t *testing.T) {
	a := newRoomApp(t).withAgents("sydney")
	a = a.observeCompaction("s1", compactStart("s1"))
	if !a.anyCompacting() {
		t.Fatal("not compacting after the start flag")
	}

	ended := &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "s1", Name: "sydney", State: rpc.StateEnded}}}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: ended})
	if a.anyCompacting() {
		t.Error("still compacting after the session ended; the line and ticker would run forever")
	}
}

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// The waiting line a DM draws between a self-paced loop's iterations: static and
// dim like the done line it replaces, with the iterations done and the next fire
// as a wall-clock time - never a live-ticking countdown, which would need the
// ticker running through an idle wait.
func TestLoopWaitLineReadsForSelfPaced(t *testing.T) {
	fire := time.Date(2026, 8, 28, 18, 48, 0, 0, time.Local)
	line := ansi.Strip(loopWaitLine(LoopState{Active: true, SelfPaced: true, Iter: 4, NextFire: fire}, 80))
	if !strings.HasPrefix(line, doneGlyph+" "+loopWaitWord) {
		t.Errorf("waiting line %q does not open with the looping head", line)
	}
	if !strings.Contains(line, "iter 4 done") {
		t.Errorf("waiting line %q is missing the iteration count", line)
	}
	if !strings.Contains(line, "next 6:48 PM") {
		t.Errorf("waiting line %q is missing the next-fire wall-clock time", line)
	}
}

// A fixed loop's waiting line names its cadence rather than an iteration count -
// a cron-fire carries no wire marker to count.
func TestLoopWaitLineForFixedShowsCadence(t *testing.T) {
	line := ansi.Strip(loopWaitLine(LoopState{Active: true, Cron: "*/5 * * * *"}, 80))
	if !strings.Contains(line, loopWaitWord) || !strings.Contains(line, "every 5m") {
		t.Errorf("fixed waiting line %q, want the cadence", line)
	}
	if strings.Contains(line, "next") {
		t.Errorf("a fixed loop drew a next-fire time it cannot know: %q", line)
	}
}

func TestLoopWaitLineEmptyWithNoLoop(t *testing.T) {
	if got := loopWaitLine(LoopState{}, 80); got != "" {
		t.Errorf("loopWaitLine of no loop = %q, want empty", got)
	}
}

// A width too tight for the whole line drops the next-fire clause rather than
// cutting mid-word, the done line's own rule.
func TestLoopWaitLineDropsNextBeforeCuttingWord(t *testing.T) {
	fire := time.Date(2026, 8, 28, 18, 48, 0, 0, time.Local)
	l := LoopState{Active: true, SelfPaced: true, Iter: 4, NextFire: fire}
	full := ansi.Strip(loopWaitLine(l, 200))
	narrow := ansi.Strip(loopWaitLine(l, ansi.StringWidth(full)-4))
	if strings.Contains(narrow, "next") {
		t.Errorf("narrow waiting line %q kept the next-fire clause", narrow)
	}
	if !strings.Contains(narrow, "iter 4 done") {
		t.Errorf("narrow waiting line %q lost the iteration clause it should keep first", narrow)
	}
}

// The DM draws the waiting line for an idle looping agent, and it wins over the
// done line: a looping agent between iterations is not "done", it is waiting.
func TestTheWaitingLineWinsOverTheDoneLine(t *testing.T) {
	start := time.Date(2026, 8, 28, 18, 46, 1, 0, time.Local)
	fire := start.Add(20 * time.Minute)
	d := NewDM("s1", "alex").SetSize(80, 30)
	d.Agent = Agent{
		State: rpc.StateIdle, startedAt: start, doneAt: start.Add(time.Minute), turnDur: time.Minute,
		loop: LoopState{Active: true, SelfPaced: true, Iter: 3, NextFire: fire},
	}
	got := ansi.Strip(d.heartbeat())
	if !strings.Contains(got, loopWaitWord) {
		t.Errorf("an idle looping agent drew %q, want the waiting line over the done line", got)
	}
	if !d.hasBeat() {
		t.Error("hasBeat is false while the waiting line draws - the row is not budgeted")
	}
	// Drop the loop and the plain done line returns.
	d.Agent.loop = LoopState{}
	if got := ansi.Strip(d.heartbeat()); !strings.Contains(got, "done") || strings.Contains(got, loopWaitWord) {
		t.Errorf("without a loop the DM drew %q, want the plain done line", got)
	}
}

// A working turn that is one iteration of a self-paced loop marks its working
// line, so the pane says the turn is part of a loop.
func TestTheWorkingLineMarksASelfPacedIteration(t *testing.T) {
	forceTrueColour(t)
	d := NewDM("s1", "alex").SetSize(80, 30)
	d.Agent = Agent{State: rpc.StateWorking, startedAt: clock(), loop: LoopState{Active: true, SelfPaced: true, Iter: 2}}
	if got := stripANSI(d.heartbeat()); !strings.Contains(got, loopGlyph) {
		t.Errorf("a working loop iteration drew %q, want the ↻ clause", got)
	}
}

// Compacting still wins over a loop, and a working turn wins over the waiting
// line: the precedence is compacting > working > loop-wait > done.
func TestCompactingAndWorkingBeatTheWaitingLine(t *testing.T) {
	start := time.Date(2026, 8, 28, 18, 46, 1, 0, time.Local)
	looping := LoopState{Active: true, SelfPaced: true, Iter: 3, NextFire: start.Add(time.Hour)}

	compacting := NewDM("s1", "alex").SetSize(80, 30)
	compacting.Agent = Agent{State: rpc.StateIdle, loop: looping}
	compacting.compactingSince = start
	if got := ansi.Strip(compacting.heartbeat()); !strings.Contains(got, "Compacting") {
		t.Errorf("compaction did not win over the loop-wait line: %q", got)
	}

	working := NewDM("s1", "alex").SetSize(80, 30)
	working.Agent = Agent{State: rpc.StateWorking, startedAt: clock(), loop: looping}
	if got := ansi.Strip(working.heartbeat()); strings.Contains(got, loopWaitWord) {
		t.Errorf("the waiting line drew over a working turn: %q", got)
	}
}

// The waiting line is suppressed for the done line's own reasons: a running
// subagent, and a live streaming preview.
func TestTheWaitingLineIsSuppressedWhileBusyBeneath(t *testing.T) {
	start := time.Date(2026, 8, 28, 18, 46, 1, 0, time.Local)
	d := NewDM("s1", "alex").SetSize(80, 30)
	d.Agent = Agent{State: rpc.StateIdle, loop: LoopState{Active: true, SelfPaced: true, Iter: 2, NextFire: start.Add(time.Hour)}}
	if !d.showsLoopWait() {
		t.Fatal("baseline: an idle looping agent should show the waiting line")
	}
	if d.WithRunningSub(true).showsLoopWait() {
		t.Error("the waiting line showed while a subagent ran beneath the loop")
	}
	preview := d
	preview.partial = preview.partial.add("mid-sentence")
	if preview.showsLoopWait() {
		t.Error("the waiting line showed over a live streaming preview")
	}
}

// The waiting line costs its chrome row, so the pane is sized for it.
func TestTheWaitingLineCostsAChromeRow(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30)
	d.Agent = Agent{State: rpc.StateIdle}
	without := d.chromeHeight()

	d.Agent = Agent{State: rpc.StateIdle, loop: LoopState{Active: true, SelfPaced: true, Iter: 1, NextFire: time.Now().Add(time.Hour)}}
	if with := d.chromeHeight(); with != without+2 {
		t.Errorf("chromeHeight without the waiting line = %d, with = %d: it should cost its line and the blank above it", without, with)
	}
}

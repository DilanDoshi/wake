package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

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

func TestATurnEndRecordsTheDoneTimeAndDuration(t *testing.T) {
	start := time.Date(2026, 8, 28, 18, 46, 1, 0, time.Local)
	clock = func() time.Time { return start }
	defer func() { clock = time.Now }()

	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateWorking},
	}})
	clock = func() time.Time { return start.Add(1*time.Minute + 59*time.Second) }
	f = f.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
	}})

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

// An agent already working when this client attached has a zero startedAt, so
// there is no duration to report and it gets no done line rather than one dated
// to the zero time.
func TestAnAgentIdleWithNoWatchedTurnRecordsNoDoneTime(t *testing.T) {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
	}})
	if a, _ := f.Agent("s1"); !a.doneAt.IsZero() {
		t.Error("an agent seen only idle recorded a done time")
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

// The done line takes a chrome row exactly as the working line does; if it did
// not, the pane would be sized a row short and the alt screen would scroll.
func TestTheDoneLineCostsAChromeRow(t *testing.T) {
	start := time.Now()
	d := NewDM("s1", "alex").SetSize(80, 30)

	d.Agent = Agent{State: rpc.StateIdle}
	without := d.chromeHeight()

	d.Agent = Agent{State: rpc.StateIdle, startedAt: start, doneAt: start.Add(time.Minute), turnDur: time.Minute}
	with := d.chromeHeight()

	if with != without+1 {
		t.Errorf("chromeHeight without the done line = %d, with = %d: it should cost one row", without, with)
	}
}

package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The heartbeat is the first animation in a build whose first non-negotiable is
// being cheap to leave open, so these are cost tests rather than appearance
// ones. Two properties carry the whole argument: one ticker however many agents
// are working, and none at all once they stop.

// countTicks replaces the timer seam with a counter, and puts it back.
func countTicks(t *testing.T) *int {
	t.Helper()
	n := 0
	prev := heartbeatTimer
	heartbeatTimer = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		n++
		return func() tea.Msg { return fn(time.Now()) }
	}
	t.Cleanup(func() { heartbeatTimer = prev })
	return &n
}

// working is a status report putting n agents into a turn.
func workingStatus(n int) *rpc.Status {
	st := &rpc.Status{}
	for i := range n {
		st.Sessions = append(st.Sessions, rpc.SessionStatus{
			ID:    string(rune('a' + i)),
			Name:  string(rune('a' + i)),
			State: rpc.StateWorking,
		})
	}
	return st
}

// A quiet fleet schedules nothing. This is the property that makes the whole
// feature affordable: most Wakes, most of the time, have no turn in flight, and
// those must cost exactly what they cost before the heartbeat existed.
func TestAQuietFleetSchedulesNoTick(t *testing.T) {
	ticks := countTicks(t)

	var a App
	a.fleet = NewFleet()
	if _, cmd := a.beat(); cmd != nil {
		t.Error("beat scheduled a tick with no agent working")
	}
	if *ticks != 0 {
		t.Errorf("a quiet fleet scheduled %d ticks, want 0", *ticks)
	}
}

// Thirty working agents cost one ticker, not thirty. A per-agent timer is the
// shape this file exists to forbid.
func TestManyWorkingAgentsShareOneTicker(t *testing.T) {
	ticks := countTicks(t)

	var a App
	a.fleet = NewFleet().WithStatus(workingStatus(30))
	model, cmd := a.beat()
	if cmd == nil {
		t.Fatal("beat scheduled nothing with thirty agents working")
	}
	if *ticks != 1 {
		t.Errorf("thirty working agents scheduled %d tickers, want 1", *ticks)
	}

	// A second call while one is in flight must not start another - every
	// status frame calls beat, and a busy fleet sends many.
	if _, again := model.beat(); again != nil {
		t.Error("beat started a second ticker while one was already in flight")
	}
	if *ticks != 1 {
		t.Errorf("after a redundant beat there are %d tickers, want 1", *ticks)
	}
}

// The loop ends when the work does, rather than running for the life of the
// process.
func TestTheTickerStopsWhenTheFleetGoesQuiet(t *testing.T) {
	ticks := countTicks(t)

	var a App
	a.fleet = NewFleet().WithStatus(workingStatus(1))
	model, cmd := a.beat()
	if cmd == nil {
		t.Fatal("beat scheduled nothing with an agent working")
	}

	// The tick lands while the agent is still working: the loop continues.
	model, cmd = model.beatArrived()
	if cmd == nil {
		t.Error("the loop stopped while an agent was still working")
	}
	if *ticks != 2 {
		t.Errorf("scheduled %d ticks across two beats, want 2", *ticks)
	}

	// The agent finishes, and the next tick to land is the last.
	quiet := model
	quiet.fleet = quiet.fleet.WithStatus(&rpc.Status{
		Sessions: []rpc.SessionStatus{{ID: "a", Name: "a", State: rpc.StateIdle}},
	})
	if _, cmd := quiet.beatArrived(); cmd != nil {
		t.Error("the ticker rescheduled itself after the fleet went quiet")
	}
	if *ticks != 2 {
		t.Errorf("a quiet fleet scheduled a %drd tick", *ticks)
	}
}

// The age is measured from the transition into working, not from every report
// that repeats it - an agent stays working across many.
func TestTurnAgeRunsFromTheTransitionIntoWorking(t *testing.T) {
	start := time.Now()
	clock = func() time.Time { return start }
	defer func() { clock = time.Now }()

	f := NewFleet().WithStatus(workingStatus(1))

	// Ten seconds and another report later, the turn is ten seconds old.
	clock = func() time.Time { return start.Add(10 * time.Second) }
	f = f.WithStatus(workingStatus(1))

	got := f.agents["a"]
	if age := turnAge(got.State, got.startedAt); age != 10*time.Second {
		t.Errorf("turn age = %v, want 10s - a repeated report restarted the clock", age)
	}
}

// An agent that is not working has no line, whatever else is true of it.
func TestOnlyAWorkingAgentDrawsAHeartbeat(t *testing.T) {
	for _, state := range []string{rpc.StateIdle, rpc.StateBlocked, rpc.StateSilent, ""} {
		if got := workingLine("a", state, "", time.Now(), 0, 40); got != "" {
			t.Errorf("state %q drew a heartbeat: %q", state, got)
		}
	}
	if workingLine("a", rpc.StateWorking, "", time.Now(), 0, 40) == "" {
		t.Error("a working agent drew no heartbeat")
	}
}

// The pane draws the line between the transcript and the composer while a turn
// is in flight, and nothing at all between turns - the composer must not jump
// a row every time an agent starts and stops.
func TestTheConversationPaneDrawsTheHeartbeatWhileWorking(t *testing.T) {
	forceTrueColour(t)
	d := NewDM("s1", "alex")
	d.Agent = Agent{ID: "s1", State: rpc.StateWorking, startedAt: clock()}

	if beat := stripANSI(d.heartbeat()); !strings.Contains(beat, "…") {
		t.Errorf("a working conversation drew no heartbeat, got %q", beat)
	}
	d.Agent.State = rpc.StateIdle
	if beat := d.heartbeat(); beat != "" {
		t.Errorf("an idle conversation drew a heartbeat: %q", beat)
	}
}

// The order App.dmPane actually uses: the pane is sized, and only then told
// which agent it is showing. Both extra rows appear that way - an agent starts
// a turn, or the first fact about a session lands - and neither is a resize, so
// a View that re-sized only on width and height drew a row more than it was
// given and scrolled the whole conversation off the alt screen.
//
// This is the sequence the pty harness caught and no unit test did, which is
// the reason it exists. Keep it in this order.
func TestThePaneStaysInBoundsWhenItsAgentArrivesAfterSizing(t *testing.T) {
	const w, h = 100, 29
	dir := t.TempDir()
	writeHead(t, dir, "ref: refs/heads/main")

	for _, tc := range []struct {
		name  string
		agent Agent
	}{
		{"nothing known", Agent{}},
		{"a status bar appears", Agent{ID: "s1", Cwd: dir, Model: "claude-opus-5"}},
		{"a turn starts", Agent{ID: "s1", State: rpc.StateWorking, startedAt: clock()}},
		{"both at once", Agent{
			ID: "s1", Cwd: dir, Model: "claude-opus-5", State: rpc.StateWorking,
			startedAt: clock(), ContextTokens: 250_000, ContextWindow: 1_000_000,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDM("s1", "alex").SetSize(w, h)
			d.Agent = tc.agent // exactly what dmPane does, after the sizing

			if got := lipgloss.Height(d.View(w, h)); got != h {
				t.Errorf("pane drew %d rows into %d", got, h)
			}
		})
	}
}

// The extra rows are budgeted, or the pane draws more lines than it was given
// and the alt screen scrolls on every frame. The working line brings two rows
// over idle - the line itself and the blank above it - since both states keep
// the one blank row above the composer.
func TestTheHeartbeatsRowIsBudgeted(t *testing.T) {
	const w, h = 60, 20
	idle := NewDM("s1", "alex")
	idle.Agent = Agent{ID: "s1", State: rpc.StateIdle}
	idle = idle.SetSize(w, h)

	busy := NewDM("s1", "alex")
	busy.Agent = Agent{ID: "s1", State: rpc.StateWorking, startedAt: clock()}
	busy = busy.SetSize(w, h)

	if busy.chromeHeight() != idle.chromeHeight()+2 {
		t.Errorf("working chrome is %d rows and idle is %d; the heartbeat's line and its gap are not budgeted",
			busy.chromeHeight(), idle.chromeHeight())
	}
	if got := lipgloss.Height(busy.View(w, h)); got > h {
		t.Errorf("a working conversation drew %d rows into a pane of %d", got, h)
	}
}

// The sidebar's half of the heartbeat. A working agent's row used to carry a
// static ◐, which cannot tell a session that is thinking from one that is
// wedged - the question the roster exists to answer at 15-30 sessions.
//
// It animates through the same heartbeatGlyph the conversation pane uses, off
// the same wall-clock elapsed, so the glyph beside a name and the glyph on its
// working line are the same character at the same moment rather than two
// animations that drift.
func TestAWorkingRowAnimatesInStepWithItsConversation(t *testing.T) {
	start := clock()
	a := Agent{ID: "a", Name: "sydney", State: rpc.StateWorking, startedAt: start}

	restore := clock
	defer func() { clock = restore }()

	var seen []string
	for _, at := range []time.Duration{0, glyphStep, 2 * glyphStep, 3 * glyphStep} {
		clock = func() time.Time { return start.Add(at) }
		row := stripANSI(headLine(a, 20))
		glyph, _, _ := strings.Cut(row, " ")
		seen = append(seen, glyph)

		if want := heartbeatGlyph(at); glyph != want {
			t.Errorf("at %v the row drew %q and the working line draws %q; they must be one glyph", at, glyph, want)
		}
	}
	if seen[0] == seen[1] && seen[1] == seen[2] {
		t.Errorf("the row never changed across three steps: %v", seen)
	}
}

// Only a working row animates. Everything else keeps the glyph its state owns,
// or the sidebar stops being scannable.
func TestOnlyAWorkingRowAnimates(t *testing.T) {
	for _, state := range []string{rpc.StateIdle, rpc.StateBlocked, rpc.StateParked, rpc.StateEnded} {
		a := Agent{ID: "a", Name: "sydney", State: state, startedAt: clock()}
		row := stripANSI(headLine(a, 20))
		if glyph, _, _ := strings.Cut(row, " "); glyph != stateGlyph[state] {
			t.Errorf("state %q drew %q, want its own %q", state, glyph, stateGlyph[state])
		}
	}
}

// An agent already working when this client attached has no start time, so
// there is no elapsed to animate from. It keeps the static glyph rather than
// animating from the zero time, which would put every such row at the same
// frame forever.
func TestAWorkingRowWithNoStartKeepsTheStaticGlyph(t *testing.T) {
	a := Agent{ID: "a", Name: "sydney", State: rpc.StateWorking}
	row := stripANSI(headLine(a, 20))
	if glyph, _, _ := strings.Cut(row, " "); glyph != stateGlyph[rpc.StateWorking] {
		t.Errorf("a turn with no start drew %q, want the static %q", glyph, stateGlyph[rpc.StateWorking])
	}
}

// Tokens are a running total of what each turn produced, because that is what
// the wire reports: every result carries its own turn's output, so this is a
// sum of increments rather than a delta of a cumulative field - which is the
// shape CLAUDE.md warns about for spend.
func TestTokensAccumulateAcrossTurns(t *testing.T) {
	f := NewFleet()
	for _, out := range []int{400, 1_200, 10_000} {
		f, _ = f.Observe(core.Event{
			Kind:      core.KindTurnEnd,
			SessionID: "a",
			Session:   &core.SessionFacts{OutputTokens: out, ContextTokens: 50_000, ContextWindow: 200_000},
		}, "a")
	}
	if got, want := f.agents["a"].Tokens, 11_600; got != want {
		t.Errorf("tokens = %d, want %d summed over three turns", got, want)
	}
	// The context is a level, so it is the newest value rather than the sum.
	if got, want := f.agents["a"].ContextTokens, 50_000; got != want {
		t.Errorf("context tokens = %d, want the newest %d - a level must not accumulate", got, want)
	}
}

// /clear empties the conversation the totals describe, so the totals go too. A
// count that survived would describe a session the model no longer has, and a
// context level would be a percentage of a window just emptied.
func TestAClearForgetsTheAccounting(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(core.Event{
		Kind:      core.KindTurnEnd,
		SessionID: "a",
		Session:   &core.SessionFacts{OutputTokens: 5_000, ContextTokens: 120_000, ContextWindow: 200_000},
	}, "a")
	f, _ = f.Observe(core.Event{Kind: core.KindSessionReset, SessionID: "a"}, "a")

	a := f.agents["a"]
	if a.Tokens != 0 {
		t.Errorf("tokens survived a /clear as %d", a.Tokens)
	}
	if a.ContextTokens != 0 {
		t.Errorf("context survived a /clear as %d", a.ContextTokens)
	}
	// The window is a property of the model, not of the conversation, so it
	// stays - otherwise the next turn has a numerator and no denominator.
	if a.ContextWindow != 200_000 {
		t.Errorf("the context window was forgotten too (%d); it belongs to the model", a.ContextWindow)
	}
}

// The ticker starts from the path production frames actually take: a batch off
// the drain goroutine. frameMsg is the single-frame form only tests deliver, so
// a beat wired only there is a ticker that never starts live - the shimmer and
// the turn's age then move only when some unrelated message happens to redraw.
func TestAStreamedStatusStartsTheTicker(t *testing.T) {
	ticks := countTicks(t)

	a := newRoomApp(t)
	frame := rpc.Frame{Kind: rpc.FrameStatusPush, Status: workingStatus(1)}
	model, _ := a.Update(streamMsg{gen: a.gen, batch: batch{frames: []rpc.Frame{frame}}})
	if !model.(App).beating {
		t.Error("a streamed status put an agent into a turn and no heartbeat is in flight")
	}
	if *ticks != 1 {
		t.Errorf("a streamed working status scheduled %d ticks, want 1", *ticks)
	}
}

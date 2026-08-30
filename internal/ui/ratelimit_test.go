package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The rate-limit warning is a pop-up above the composer that clears itself, not
// a line in the scrollback. These tests hold the four properties that carry the
// feature: a warning pops the timed notice and nothing in the transcript, the
// notice clears after the tick, a benign heartbeat pops nothing, and two
// overlapping warnings clear once rather than early.

// countRateLimitTicks swaps the timer seam for a counter that runs the tick
// immediately, and puts the seam back. Its shape is beat_test.go's countTicks.
func countRateLimitTicks(t *testing.T) *int {
	t.Helper()
	n := 0
	prev := rateLimitTimer
	rateLimitTimer = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		n++
		return func() tea.Msg { return fn(time.Now()) }
	}
	t.Cleanup(func() { rateLimitTimer = prev })
	return &n
}

// rateLimitFrame is one rate_limit_event, as the airlock would hand it up.
func rateLimitFrame(sessionID, status string, notice core.Notice) rpc.Frame {
	return rpc.Frame{
		Kind:      rpc.FrameEvent,
		SessionID: sessionID,
		Event:     &core.Event{Kind: core.KindRateLimit, SessionID: sessionID, Text: status, Notice: notice},
	}
}

// A warning pops the timed notice above the composer and arms exactly one
// clear tick - and never a block in the conversation below.
func TestARateLimitWarningPopsATimedNotice(t *testing.T) {
	ticks := countRateLimitTicks(t)
	a := sizedApp(t, nil, nil, "s1")

	m, _ := a.Update(frameMsg{Frame: rateLimitFrame("s1", "allowed_warning", core.NoticeRateLimited)})

	n, ok := notice.Latest()
	if !ok || !strings.Contains(n.Text, "usage limit") {
		t.Fatalf("no usage-limit notice: Latest = %q, %v", n.Text, ok)
	}
	if *ticks != 1 {
		t.Errorf("a warning armed %d clear ticks, want 1", *ticks)
	}
	if dm := m.(App).dms["s1"]; dm != nil && strings.Contains(visible(*dm, 80, 20), "rate limit") {
		t.Error("the warning landed in the conversation transcript")
	}
}

// The notice stands for the linger and then clears itself.
func TestTheRateLimitNoticeClearsAfterTheTick(t *testing.T) {
	countRateLimitTicks(t)
	a := sizedApp(t, nil, nil, "s1")

	m, _ := a.Update(frameMsg{Frame: rateLimitFrame("s1", "allowed_warning", core.NoticeRateLimited)})
	if _, ok := notice.Latest(); !ok {
		t.Fatal("no notice to clear")
	}

	_, _ = m.Update(rateLimitClearMsg{gen: m.(App).rl.gen})
	if n, ok := notice.Latest(); ok {
		t.Errorf("the notice survived its tick: %q", n.Text)
	}
}

// A benign `allowed` heartbeat is chrome: it pops nothing and arms no tick.
func TestAnAllowedRateLimitPopsNothing(t *testing.T) {
	ticks := countRateLimitTicks(t)
	a := sizedApp(t, nil, nil, "s1")

	a.Update(frameMsg{Frame: rateLimitFrame("s1", "allowed", "")})

	if n, ok := notice.Latest(); ok {
		t.Errorf("an allowed heartbeat popped a notice: %q", n.Text)
	}
	if *ticks != 0 {
		t.Errorf("an allowed heartbeat armed %d ticks, want 0", *ticks)
	}
}

// Two warnings close together clear once, at the second one's deadline - the
// first's expiry no-ops rather than clearing the second early.
func TestOverlappingWarningsClearOnceAtTheLast(t *testing.T) {
	countRateLimitTicks(t)
	a := sizedApp(t, nil, nil, "s1")

	m, _ := a.Update(frameMsg{Frame: rateLimitFrame("s1", "allowed_warning", core.NoticeRateLimited)})
	first := m.(App).rl.gen
	m, _ = m.Update(frameMsg{Frame: rateLimitFrame("s1", "allowed_warning", core.NoticeRateLimited)})
	second := m.(App).rl.gen
	if first == second {
		t.Fatalf("the second warning did not bump the generation: %d", second)
	}

	// The first warning's expiry finds a newer one and leaves it alone.
	m, _ = m.Update(rateLimitClearMsg{gen: first})
	if _, ok := notice.Latest(); !ok {
		t.Error("the stale tick cleared a warning that had been superseded")
	}

	// The newest one's expiry clears it.
	_, _ = m.Update(rateLimitClearMsg{gen: second})
	if n, ok := notice.Latest(); ok {
		t.Errorf("the newest warning outlived its own tick: %q", n.Text)
	}
}

// A warning arriving as the last frame before the daemon exits arms a clear
// tick the done path never consumes (App.stream only arms in the `!m.done`
// branch, and inbox.take can hand back a batch that is both non-empty and
// done). Without a reset that stale arm rides across the reattach and the next
// ordinary batch schedules a spurious tick keyed to a spent warning's gen - the
// early-clear race the gen guard exists to prevent. A hang-up forgets it.
func TestARateLimitWarningArmDoesNotSurviveAHangUp(t *testing.T) {
	ticks := countRateLimitTicks(t)
	a := sizedApp(t, nil, nil, "s1")

	warn := core.Event{Kind: core.KindRateLimit, SessionID: "s1", Text: "allowed_warning", Notice: core.NoticeRateLimited}
	done := streamMsg{batch: batch{frames: []rpc.Frame{{Kind: rpc.FrameEvent, SessionID: "s1", Event: &warn}}, done: true}, gen: a.gen}
	m, _ := a.stream(done)

	if *ticks != 0 {
		t.Fatalf("the hang-up batch scheduled %d clear ticks itself, want 0", *ticks)
	}
	if m.(App).rl.arm {
		t.Error("the warning's arm survived the hang-up; the next batch would schedule a spurious tick")
	}
}

// A rate-limit event of either status never draws in the conversation
// transcript - the surface is the timed notice above the composer.
func TestARateLimitNeverDrawsInTheTranscript(t *testing.T) {
	allowed := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindRateLimit, Text: "allowed"})
	assertHides(t, allowed, 60, 20, "allowed")

	warned := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindRateLimit, Text: "allowed_warning", Notice: core.NoticeRateLimited})
	assertHides(t, warned, 60, 20, "rate limit")
	assertHides(t, warned, 60, 20, "allowed_warning")
}

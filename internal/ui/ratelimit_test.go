package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The rate-limit warning is a pop-up above the composer that clears itself, not
// a line in the scrollback. It is timed like every notice (noticelinger.go);
// what is its own is that a warning pops one and a benign heartbeat pops
// nothing.

// rateLimitFrame is one rate_limit_event, as the airlock would hand it up.
func rateLimitFrame(sessionID, status string, notice core.Notice) rpc.Frame {
	return rpc.Frame{
		Kind:      rpc.FrameEvent,
		SessionID: sessionID,
		Event:     &core.Event{Kind: core.KindRateLimit, SessionID: sessionID, Text: status, Notice: notice},
	}
}

// A warning pops a timed notice above the composer and never a block in the
// conversation below, and its linger clears it.
func TestARateLimitWarningPopsATimedNotice(t *testing.T) {
	ticks := recordNoticeTicks(t)
	a := sizedApp(t, nil, nil, "s1")

	m, _ := a.Update(frameMsg{Frame: rateLimitFrame("s1", "allowed_warning", core.NoticeRateLimited)})

	n, ok := notice.Latest()
	if !ok || !strings.Contains(n.Text, "usage limit") {
		t.Fatalf("no usage-limit notice: Latest = %q, %v", n.Text, ok)
	}
	if len(*ticks) != 1 {
		t.Fatalf("a warning armed %d lingers, want 1", len(*ticks))
	}
	if dm := m.(App).dms["s1"]; dm != nil && strings.Contains(visible(*dm, 80, 20), "rate limit") {
		t.Error("the warning landed in the conversation transcript")
	}

	_, _ = m.Update((*ticks)[0].msg)
	if n, ok := notice.Latest(); ok {
		t.Errorf("the notice survived its linger: %q", n.Text)
	}
}

// A benign `allowed` heartbeat is chrome: it pops nothing and arms no tick.
func TestAnAllowedRateLimitPopsNothing(t *testing.T) {
	ticks := recordNoticeTicks(t)
	a := sizedApp(t, nil, nil, "s1")

	a.Update(frameMsg{Frame: rateLimitFrame("s1", "allowed", "")})

	if n, ok := notice.Latest(); ok {
		t.Errorf("an allowed heartbeat popped a notice: %q", n.Text)
	}
	if len(*ticks) != 0 {
		t.Errorf("an allowed heartbeat armed %d ticks, want 0", len(*ticks))
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

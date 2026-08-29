package daemon

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// effortAgent is a live agent with a session that has not started a process -
// enough for snapshot() and the stdin queue, the same shape colorAgent uses.
func effortAgent(t *testing.T) *agent {
	t.Helper()
	return newAgent(idAlpha, "sydney", "dev-1", "/repo/api", "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})
}

// The status snapshot shows the level a /model probe confirmed once it has one,
// and the level Wake asked for until then. A default-effort session with no
// probe yet shows nothing rather than a guess.
func TestSnapshotPrefersConfirmedEffort(t *testing.T) {
	a := effortAgent(t)
	a.effort = core.EffortHigh
	if got := a.snapshot().Effort; got != core.EffortHigh {
		t.Fatalf("asked-for should show when nothing is confirmed: %q", got)
	}
	a.confirmedEffort = core.EffortXHigh
	if got := a.snapshot().Effort; got != core.EffortXHigh {
		t.Fatalf("confirmed should win over asked-for: %q", got)
	}

	blank := effortAgent(t)
	if got := blank.snapshot().Effort; got != "" {
		t.Fatalf("a session with neither asked-for nor confirmed shows nothing, got %q", got)
	}
}

// The probe is a bare /model queued on the agent's own stdin path, marked so
// apply can send it without counting it as an operator turn.
func TestProbeEnqueuesBareModel(t *testing.T) {
	a := effortAgent(t)
	a.probeEffort()
	select {
	case p := <-a.in:
		if !p.probe {
			t.Error("the probe pending is not marked as a probe")
		}
		if p.frame.Kind != rpc.FrameSend || p.frame.Text != "/model" {
			t.Errorf("probe queued %+v, want a /model FrameSend", p.frame)
		}
	default:
		t.Fatal("probeEffort queued nothing")
	}
}

// A probe is refused for an agent that is blocked on an ask (its stdin is a
// closed decision) or already gone - a probe there is noise at best and an
// answer nobody made at worst.
func TestProbeSkipsABlockedOrGoneAgent(t *testing.T) {
	blocked := effortAgent(t)
	blocked.pending = []ask{{id: "r1"}}
	blocked.probeEffort()
	if len(blocked.in) != 0 {
		t.Error("a blocked agent was probed")
	}

	gone := effortAgent(t)
	close(gone.gone)
	gone.probeEffort()
	if len(gone.in) != 0 {
		t.Error("a gone agent was probed")
	}
}

// absorbProbe swallows the probe's reply and its turn end, records the level,
// and asks for one status push - while letting every other frame through, so a
// real turn draining on the same goroutine is untouched.
func TestAbsorbProbeSuppressesReplyAndRecordsEffort(t *testing.T) {
	a := effortAgent(t)
	a.incProbe()

	// A real assistant frame that is not a /model reply passes through, even
	// mid-window: suppression is keyed on the reply's shape, not the flag alone.
	if suppress, _ := a.absorbProbe(core.Event{Kind: core.KindAssistantText, Text: "echo: hi"}); suppress {
		t.Fatal("a non-reply frame was suppressed during the probe window")
	}

	// The reply: suppressed, level recorded, one publish.
	suppress, publish := a.absorbProbe(core.Event{Kind: core.KindAssistantText, Text: "Current model: Opus 5 (effort: max)"})
	if !suppress || !publish {
		t.Fatalf("the reply must be suppressed and publish the effort: suppress=%v publish=%v", suppress, publish)
	}
	if a.confirmedEffort != core.EffortMax {
		t.Fatalf("effort not recorded: %q", a.confirmedEffort)
	}

	// The probe turn's end: suppressed, no second publish, window closed.
	if suppress, publish := a.absorbProbe(core.Event{Kind: core.KindTurnEnd, Text: "done"}); !suppress || publish {
		t.Fatalf("the probe turn end: suppress=%v publish=%v, want true/false", suppress, publish)
	}

	// After the window, an ordinary turn end passes through.
	if suppress, _ := a.absorbProbe(core.Event{Kind: core.KindTurnEnd, Text: "done"}); suppress {
		t.Fatal("frames after the probe window are still suppressed")
	}
}

// Two probes in flight suppress two replies, not one. A bool cleared by the
// first reply let the second - the response to a later /effort - leak into the
// transcript; the counter closes that.
func TestAbsorbProbeSuppressesBothOfTwoOverlappingProbes(t *testing.T) {
	a := effortAgent(t)
	a.incProbe()
	a.incProbe() // two /model sends went out before either answered

	reply := func(level string) {
		if suppress, _ := a.absorbProbe(core.Event{Kind: core.KindAssistantText, Text: "Current model: Opus 5 (effort: " + level + ")"}); !suppress {
			t.Fatalf("a probe reply (effort %s) leaked to clients", level)
		}
		if suppress, _ := a.absorbProbe(core.Event{Kind: core.KindTurnEnd, Text: "done"}); !suppress {
			t.Fatalf("a probe turn end (effort %s) leaked to clients", level)
		}
	}
	reply("high") // first probe's turn
	reply("max")  // second probe's turn - must still be suppressed

	if a.confirmedEffort != core.EffortMax {
		t.Fatalf("the later probe's level did not win: confirmedEffort = %q", a.confirmedEffort)
	}
	// The window is closed: an ordinary turn now passes through.
	if suppress, _ := a.absorbProbe(core.Event{Kind: core.KindTurnEnd, Text: "done"}); suppress {
		t.Fatal("the probe window did not close after both replies")
	}
}

// The startup probe fires on the session's init and only once, so a per-turn
// init does not re-probe on every turn.
func TestFirstInitFiresOnce(t *testing.T) {
	a := effortAgent(t)
	initEv := core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{Model: "claude-opus-5"}}
	if !a.firstInit(initEv) {
		t.Fatal("the first init did not trigger the probe")
	}
	if a.firstInit(initEv) {
		t.Fatal("a second init re-triggered the probe")
	}
	b := effortAgent(t)
	if b.firstInit(core.Event{Kind: core.KindSystem}) {
		t.Fatal("a system frame carrying no facts (a hook) triggered the probe")
	}
}

// The whole round trip over a real process: the startup probe confirms the
// level a default-effort session is actually at, the reply never reaches a
// client, and a runtime /effort re-probes and re-confirms.
func TestTheProbeConfirmsEffortInvisibly(t *testing.T) {
	fakeClaudeOnPath(t, "probe")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")

	// The startup probe reads back the level the fake reports, though the spawn
	// asked for none.
	c.await("effort confirmed as max", func(f rpc.Frame) bool {
		return f.Status != nil && sessionRow(*f.Status, idAlpha).Effort == core.EffortMax
	})

	// And the reply was suppressed: nothing a client saw carries it.
	for _, f := range c.seen {
		if f.Kind == rpc.FrameEvent && f.Event != nil && core.IsModelReply(f.Event.Text) {
			t.Fatalf("the probe reply leaked to a client: %q", f.Event.Text)
		}
	}

	// A runtime /effort re-probes, so the confirmed level follows the change.
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "/effort low"})
	c.await("effort re-confirmed as low", func(f rpc.Frame) bool {
		return f.Status != nil && sessionRow(*f.Status, idAlpha).Effort == core.EffortLow
	})
}

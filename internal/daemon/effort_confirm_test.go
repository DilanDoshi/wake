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
	a.setConfirmedEffort(core.EffortXHigh)
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

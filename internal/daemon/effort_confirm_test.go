package daemon

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
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

package daemon

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// The context figure reaches the fleet only on a result frame (resultFacts), so
// a client that never witnessed one shows no ctx. The daemon carries it on the
// report so a late attach learns it, the route Model and Effort already take.
// See rpc.SessionStatus.ContextTokens.
func TestSnapshotCarriesContextAfterAResultFrame(t *testing.T) {
	a := movedAgent(t)
	a.observe(core.Event{Kind: core.KindTurnEnd, Session: &core.SessionFacts{
		ContextTokens: 250_000, ContextWindow: 1_000_000,
	}})
	st := a.snapshot()
	if st.ContextTokens != 250_000 {
		t.Errorf("snapshot().ContextTokens = %d, want 250000", st.ContextTokens)
	}
	if st.ContextWindow != 1_000_000 {
		t.Errorf("snapshot().ContextWindow = %d, want 1000000", st.ContextWindow)
	}
}

// A frame that accounts for no context - an init, a tool-use, an interrupted
// result - must not blank a figure a prior result set, withFacts' own > 0 guard
// one wire over.
func TestAContextlessFrameDoesNotBlankTheReportedContext(t *testing.T) {
	a := movedAgent(t)
	a.observe(core.Event{Kind: core.KindTurnEnd, Session: &core.SessionFacts{
		ContextTokens: 250_000, ContextWindow: 1_000_000,
	}})
	a.observe(core.Event{Kind: core.KindToolUse, Session: &core.SessionFacts{}})
	if got := a.snapshot().ContextTokens; got != 250_000 {
		t.Errorf("snapshot().ContextTokens = %d after a contextless frame, want it kept (250000)", got)
	}
}

// /clear empties the conversation the figure describes, so the used half goes
// with it - the UI's own fleet.go reset. The window stays: the model, and so its
// window, is unchanged, and a woken/cleared session reports fresh usage next turn.
func TestSessionResetForgetsTheUsedContextButKeepsTheWindow(t *testing.T) {
	a := movedAgent(t)
	a.observe(core.Event{Kind: core.KindTurnEnd, Session: &core.SessionFacts{
		ContextTokens: 250_000, ContextWindow: 1_000_000,
	}})
	a.observe(core.Event{Kind: core.KindSessionReset})
	st := a.snapshot()
	if st.ContextTokens != 0 {
		t.Errorf("snapshot().ContextTokens = %d after /clear, want 0", st.ContextTokens)
	}
	if st.ContextWindow != 1_000_000 {
		t.Errorf("snapshot().ContextWindow = %d after /clear, want the window kept (1000000)", st.ContextWindow)
	}
}

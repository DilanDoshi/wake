package daemon

import (
	"slices"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// An agent can be blocked on more than one ask at once - parallel tool calls in
// one turn, or two subagents each asking - and the daemon has to hold all of
// them. It held one: a second ask overwrote the first, so answering the ask the
// report named marked the whole agent free while the CLI stayed stopped on the
// one the report had already forgotten. The report then said idle, and a live
// client's reconcile took the surviving card down - a session stuck with the
// HIL invisible, which is the bug this pins.
func TestAnAgentStaysBlockedWhileAnyConcurrentAskIsOutstanding(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-1", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	a.observe(core.Event{Kind: core.KindPermissionRequest, RequestID: "r1"})
	a.observe(core.Event{Kind: core.KindPermissionRequest, RequestID: "r2"})

	a.noteAnswered("r2")

	st := a.snapshot()
	if st.State != rpc.StateBlocked {
		t.Fatalf("state = %q, want blocked: r1 still stops the process, but answering r2 marked the whole agent free", st.State)
	}
	if got := st.RequestIDs; len(got) != 1 || got[0] != "r1" {
		t.Errorf("RequestIDs = %v, want [r1]: the surviving ask is the one the operator still has to answer", got)
	}
}

// The whole-stack half: two asks blocking one agent, driven through a real
// daemon and socket. The test above proves the daemon holds both; this proves
// the report carries both across the wire - so both cards can render - and that
// answering one leaves the agent blocked on the other, with only the second
// answer freeing it. A wire that named one ask would strand the other with
// nothing to correlate an answer against, which is the reported bug end to end.
func TestConcurrentAsksBothCrossTheWireAndAreAnsweredOneAtATime(t *testing.T) {
	fakeClaudeOnPath(t, "twoasks")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")

	// Both asks reach this client as events, attributed to the one session -
	// which is what seeds a card each.
	for _, rid := range []string{askRequestID, askRequestID2} {
		ask := c.await("the ask "+rid, func(f rpc.Frame) bool {
			return f.Kind == rpc.FrameEvent && f.SessionID == idAlpha &&
				f.Event != nil && f.Event.Kind == core.KindPermissionRequest &&
				f.Event.RequestID == rid
		})
		if ask.Event.SessionID != idAlpha {
			t.Fatalf("ask %s attributed to %q, not the session it arrived on", rid, ask.Event.SessionID)
		}
	}

	// The report names both. A reconcile keyed on one of these would take the
	// other card down and strand the agent behind it.
	pollAsks(t, c, idAlpha, askRequestID, askRequestID2)

	// Answer the first. The agent is still blocked - on the second.
	c.send(rpc.Frame{Kind: rpc.FrameAllow, SessionID: idAlpha, RequestID: askRequestID})
	if only := pollAsks(t, c, idAlpha, askRequestID2); only.State != rpc.StateBlocked {
		t.Fatalf("after answering one of two asks the agent is %q, want still blocked on the other", only.State)
	}

	// Answer the second. Now nothing is outstanding and the turn can end.
	c.send(rpc.Frame{Kind: rpc.FrameAllow, SessionID: idAlpha, RequestID: askRequestID2})
	c.pollState(idAlpha, rpc.StateIdle)
}

// pollAsks waits until a session reports exactly these outstanding asks, in any
// order, and returns that row. Order on the wire is arrival order and so
// deterministic here, but the property under test is which asks are present, not
// their sequence.
func pollAsks(t *testing.T, c *testClient, sessionID string, want ...string) rpc.SessionStatus {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	var last rpc.SessionStatus
	for time.Now().Before(deadline) {
		last = sessionRow(c.status(), sessionID)
		got := slices.Clone(last.RequestIDs)
		slices.Sort(got)
		wantSorted := slices.Clone(want)
		slices.Sort(wantSorted)
		if slices.Equal(got, wantSorted) {
			return last
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session %s never reported asks %v; last saw %v", sessionID, want, last.RequestIDs)
	return rpc.SessionStatus{}
}

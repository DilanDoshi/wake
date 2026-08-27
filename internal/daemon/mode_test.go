// Changing a running session's permission mode, over a real socket.
//
// The encoder's bytes are pinned in internal/core. What is pinned here is that
// a FrameMode reaches the agent's stdin through the same queue a send takes,
// that the receipt comes back attributed, and that the mode it names is the
// CLI's answer rather than the client's question.

package daemon

import (
	"strconv"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// TestSetModeReachesTheAgentAndLeavesTheSessionRunning is
// TestInterruptStopsTheTurnAndLeavesTheSessionRunning's pair, and it pulls the
// same two ways: the frame has to travel the path stop takes, and the session
// has to still be there afterwards.
func TestSetModeReachesTheAgentAndLeavesTheSessionRunning(t *testing.T) {
	fakeClaudeOnPath(t, "mode")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameMode, SessionID: idAlpha, Mode: core.PermissionModePlan})

	receipt := c.await("the mode receipt", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.Event != nil && f.Event.Kind == core.KindControlReceipt
	})
	if receipt.Event.RequestID == "" {
		t.Error("the receipt carries no request id: the client sent none and core minted none")
	}
	if receipt.SessionID != idAlpha {
		t.Errorf("receipt arrived as %+v, want it attributed to %s", receipt, idAlpha)
	}
	if receipt.Event.PermissionMode != core.PermissionModePlan {
		t.Errorf("receipt names mode %q, want %q", receipt.Event.PermissionMode, core.PermissionModePlan)
	}

	if st := c.status(); len(live(st)) != 1 {
		t.Fatalf("status = %+v, want the session still running - a mode change ends nothing", st.Sessions)
	}
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "still there?"})
	c.awaitEvent(idAlpha, "echo:")
}

// TestTheReceiptAnswersWithTheModeTheCLIChoseNotTheOneAsked is the trap at
// findings §6, driven end to end. `manual` is accepted and silently becomes
// `default`, so a client that moved its label on the string it *sent* would be
// wrong here - and wrong on a real position rather than in principle, which is
// deferred I7's own defect arriving through the fix for it.
//
// The cycle ⇧⇥ walks does not send `manual` for exactly this reason. The test
// sends it anyway: it is the only way to make the asked mode and the received
// mode disagree, and a wiring that reads the receipt has to survive them
// disagreeing.
func TestTheReceiptAnswersWithTheModeTheCLIChoseNotTheOneAsked(t *testing.T) {
	fakeClaudeOnPath(t, "mode")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameMode, SessionID: idAlpha, Mode: "manual"})

	receipt := c.await("the mode receipt", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.Event != nil && f.Event.Kind == core.KindControlReceipt
	})
	if receipt.Event.PermissionMode != core.PermissionModeDefault {
		t.Errorf("receipt names mode %q, want %q - manual normalizes, and the receipt is the authority",
			receipt.Event.PermissionMode, core.PermissionModeDefault)
	}
}

// TestAModeFrameIsRefusedWhileAnAskIsOutstanding closes the race the client's
// own refusal cannot.
//
// internal/ui refuses ⇧⇥ on a blocked agent because what happens to an
// outstanding ask when the mode changes under it is unrecorded (findings §9
// item 3). That check reads a fleet report, so it is a check against what this
// client last *heard*: the ask can be raised in the daemon after the report went
// out and before the keystroke arrives, and then the refusal never fires.
//
// The daemon knows without asking anybody - it is holding the ask. So the same
// verdict is made here, where it is not a race, and the client's copy stays for
// the sentence it can give the operator.
func TestAModeFrameIsRefusedWhileAnAskIsOutstanding(t *testing.T) {
	fakeClaudeOnPath(t, "ask")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.await("the permission ask", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.Event != nil && f.Event.Kind == core.KindPermissionRequest
	})

	c.send(rpc.Frame{Kind: rpc.FrameMode, SessionID: idAlpha, Mode: core.PermissionModePlan})

	f := c.await("a refusal", func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
	if !strings.Contains(f.Text, "permission request") {
		t.Errorf("error = %q, want it to name the outstanding ask", f.Text)
	}
	st := c.status()
	if len(live(st)) != 1 {
		t.Fatalf("status = %+v, want the session untouched", st.Sessions)
	}
	// Untouched means still blocked, not merely still listed. The refusal above
	// is the daemon's own sentence - nothing was written to the process - so
	// reading it as a failed write would mark a healthy blocked agent
	// unreachable: it reports silent while an operator is looking at its ask,
	// and schedules OS probes for a process the daemon itself refused to write.
	if got := stateOf(st, idAlpha); got != rpc.StateBlocked {
		t.Errorf("after the refusal the session reports %q, want %q: a refusal the daemon wrote itself is "+
			"not evidence the process is gone", got, rpc.StateBlocked)
	}
}

// TestTheModeGuardReadsTheAskUnderTheAgentsLock pins the read itself: apply
// runs on the agent's input goroutine and observe writes a.pending on the
// fan-out one, so an unlocked read in the FrameMode arm is a data race the
// -race pass sees whenever the two overlap. What no lock can close - an ask
// raised after the guard answers and before the frame reaches stdin - is the
// process's own asynchrony, and is not what this asserts.
func TestTheModeGuardReadsTheAskUnderTheAgentsLock(t *testing.T) {
	a := liveAgent(idAlpha, "sydney", t.TempDir())
	c := newClient(nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 200 {
			a.observe(core.Event{Kind: core.KindPermissionRequest, RequestID: strconv.Itoa(i)})
			a.observe(core.Event{Kind: core.KindTurnEnd})
		}
	}()
	for range 200 {
		a.apply(pending{from: c, frame: rpc.Frame{Kind: rpc.FrameMode, Mode: core.PermissionModePlan}})
	}
	<-done
}

// TestAModeFrameCarryingNoModeIsRefused is FrameMode's whole reason for being a
// kind rather than a field: an empty mode would have to mean "leave it" or
// "reset it", both readings are wrong, and the frame is refused rather than
// guessed at. The session is untouched.
func TestAModeFrameCarryingNoModeIsRefused(t *testing.T) {
	fakeClaudeOnPath(t, "mode")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameMode, SessionID: idAlpha})

	f := c.await("an error", func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
	if !strings.Contains(f.Text, "mode") {
		t.Errorf("error = %q, want it to name the empty mode", f.Text)
	}
	if st := c.status(); len(live(st)) != 1 {
		t.Fatalf("status = %+v, want the session untouched by a frame that named no mode", st.Sessions)
	}
}

package daemon

// What a stop aimed at a parked session actually does.
//
// This is the premise `/manager-stop` refuses on: internal/ui declines to write
// a FrameStop at a parked manager and says so in its own words, rather than
// writing one and letting the daemon's error land in the notice row. That
// refusal is only right for as long as the daemon really does refuse, so the
// claim is pinned here rather than read out of submit and believed.
//
// It is refused in **two different shapes**, which is why both are asserted: a
// session parked by ⌃C still has a row in s.agents with its `gone` channel
// closed, so withAgent finds it and agent.submit refuses it; a session parked
// across a restart has no row at all, because a daemon restores nothing, so
// withAgent is what refuses. A UI that wrote the frame would put one of two
// unrelated sentences in front of the operator depending on when they quit.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestAStopAtAParkedSessionIsRefusedInBothOfItsShapes(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	if after := c.awaitSettled(idAlpha); after.State != rpc.StateParked {
		t.Fatalf("parking settled the session as %q, want %q - this test is not about a parked session",
			after.State, rpc.StateParked)
	}

	// The ⌃C shape: a row this daemon still holds, with no process behind it.
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	parked := c.await("an error for the parked session", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && f.SessionID == idAlpha
	})
	if !strings.Contains(parked.Text, "has ended") {
		t.Errorf("a stop at a parked row answered %q, want the ended refusal. If this became a success, "+
			"/manager-stop's parked arm is refusing something that would now work", parked.Text)
	}

	// The park-book shape: an id no row exists for, which is every parked
	// session after a restart.
	const booked = "booked-across-a-restart"
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: booked})
	unknown := c.await("an error for the booked session", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && f.SessionID == booked
	})
	if !strings.Contains(unknown.Text, "unknown session") {
		t.Errorf("a stop at an id with no row answered %q, want the unknown-session refusal", unknown.Text)
	}
}

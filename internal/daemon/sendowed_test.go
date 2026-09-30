package daemon

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A turn is owed from before its write, not from after it: the reply can reach
// fanOut before Send returns, and a mark landing after the turn's end outlives
// it (deferred.md 2026-09-28). A deaf agent never reads its stdin, so a message
// past the pipe's buffer holds the write open while the report is read.
func TestATurnIsOwedWhileItsWriteIsInFlight(t *testing.T) {
	fakeClaudeOnPath(t, "deaf")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: strings.Repeat("x", 1<<20)})
	c.pollState(idAlpha, rpc.StateWorking)
}

// A send refused before a byte was written started no turn, so it leaves the
// agent owing nothing.
func TestARefusedSendLeavesNothingOwed(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha})
	c.await("the empty send's refusal", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && f.SessionID == idAlpha
	})
	if st := stateOf(c.status(), idAlpha); st != rpc.StateIdle {
		t.Fatalf("after a send nothing was written for, the agent reads %q, want idle", st)
	}
}

// A refused send behind a turn still running takes back only its own mark:
// the earlier turn is still owed, so the agent still reads working.
func TestARefusedSendBehindARunningTurnLeavesThatTurnOwed(t *testing.T) {
	fakeClaudeOnPath(t, "mute")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "first"})
	c.pollState(idAlpha, rpc.StateWorking)
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha})
	c.await("the empty send's refusal", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && f.SessionID == idAlpha
	})
	if st := stateOf(c.status(), idAlpha); st != rpc.StateWorking {
		t.Fatalf("a refused send behind a running turn left the agent %q, want working", st)
	}
}

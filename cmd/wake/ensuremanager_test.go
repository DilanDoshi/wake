package main

// Seating the manager on the way into the room: what reaches the socket, and
// what does not.
//
// The decision itself is internal/ui's and is table-tested there against every
// state a daemon can report. What is asserted here is the half only this
// package has: that the frame is written on the connection the room is about to
// be built over, and that a fleet already running one is left alone - which is
// what stops every `wake attach` into a busy fleet putting a refusal in the
// notice row for something nobody typed.

import (
	"net"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// dialFake opens a connection to a fake daemon. The hello it writes is left in
// the socket rather than waited on: nothing here is testing the handshake.
func dialFake(t *testing.T, d *fakeDaemon) net.Conn {
	t.Helper()
	conn, err := daemon.Dial(d.socket)
	if err != nil {
		t.Fatalf("dial the fake daemon: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// awaitFrameOfKind waits for one kind of frame to reach the fake daemon,
// because the write and the daemon's read are on different goroutines.
func awaitFrameOfKind(t *testing.T, d *fakeDaemon, kind string) rpc.Frame {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if f := d.lastOfKind(kind); f.Kind != "" {
			return f
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %s reached the daemon; it was sent %+v", kind, d.frames())
	return rpc.Frame{}
}

// A fleet with no manager gets one asked for, on this connection.
func TestOpeningTheRoomSeatsAManager(t *testing.T) {
	d := startFakeDaemon(t, 0, rpc.Status{Running: true})

	ensureManager(dialFake(t, d), &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", State: rpc.StateIdle},
	}})

	f := awaitFrameOfKind(t, d, rpc.FrameSpawn)
	if f.Role != rpc.RoleManager {
		t.Errorf("the spawn carries Role %q, want %q: without it the daemon draws an ordinary name from "+
			"the pool and the session comes up with no tools, no scope, and nothing answering @%s",
			f.Role, rpc.RoleManager, core.ManagerName)
	}
	if f.Dir == "" {
		t.Error("the spawn names no directory, so the daemon answers it with its own - whichever " +
			"repository happened to fork it, which is also where claude would persist the transcript")
	}
}

// A fleet that already has one is left alone.
//
// The refusal a second spawn earns is truthful, and it would arrive in the
// notice row of somebody who typed nothing: every verb that opens a room goes
// through here.
//
// The absence is proven by **ordering rather than by a timer**. A status frame
// is written afterwards on the same connection and waited for; frames on one
// connection arrive in the order they were written, so a spawn that was going to
// be sent would already be in the record by the time that one is.
func TestOpeningTheRoomAsksForNothingWhenAManagerIsRunning(t *testing.T) {
	d := startFakeDaemon(t, 0, rpc.Status{Running: true})
	conn := dialFake(t, d)

	ensureManager(conn, &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", State: rpc.StateIdle},
		{ID: "s2", Name: core.ManagerName, State: rpc.StateIdle},
	}})

	if err := rpc.WriteFrameTo(conn, rpc.Frame{Kind: rpc.FrameStatus}); err != nil {
		t.Fatalf("write the frame this waits on: %v", err)
	}
	awaitFrameOfKind(t, d, rpc.FrameStatus)

	if f := d.lastOfKind(rpc.FrameSpawn); f.Kind != "" {
		t.Errorf("a fleet already running a manager was sent %+v. A second spawn is refused by name, and "+
			"the refusal lands on an operator who typed nothing", f)
	}
}

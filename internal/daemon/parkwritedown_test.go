// A park the daemon cannot write down is surfaced to the clients still
// attached, not left in the daemon log alone.
//
// stateLocked reports a session parked the instant markParked runs, and the
// durable record is written afterward. When that write fails the session is
// parked in *this* daemon's fleet - ⌃C or ⌃Q did keep it - but the next daemon
// reads nothing about it, so /resume after a restart cannot bring it back. Until
// the broadcast these tests pin, the only account of that was a logf nobody
// attached is reading, so ⌃C/⌃Q promised "parked, /resume brings it back" for a
// session that is gone the moment the daemon exits.
//
// Two arms, one fault. completePark writes from a fan-out goroutine on a live
// ⌃C, and bookParked writes from Serve's own goroutine on ⌃Q. Each is tested
// where its delivery is deterministic: the ⌃C arm over the real socket, and the
// ⌃Q arm at the enqueue, because over a live ⌃Q the frame races closeClients -
// the Tier-1 caveat this fix leaves to the ⌃Q task.

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// The completePark arm: a ⌃C park whose write-down fails tells the attached
// client, over the real socket.
//
// The parked push arrives on both the broken and the fixed daemon - the session
// really is parked - so it is the barrier: completePark broadcasts the error
// before that push and on the same goroutine, so a client that has seen the push
// has already seen the error if one was sent. The assertion is then a
// synchronous scan of what arrived rather than a wait, because on origin/main
// nothing is broadcast and a wait would be a testTimeout for a frame that never
// comes instead of a failure that names what did.
func TestAParkThatCannotBeWrittenDownTellsTheAttachedClient(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitState(idAlpha, rpc.StateIdle)

	// Fail the write-down: the book is replaced through a temp file in its own
	// directory, so a directory nothing may create in is a parked.add that
	// returns an error. Restored before the daemon's own cleanup, which clears a
	// roster in the same directory and then removes the tree.
	unwritable(t, filepath.Dir(parkBookPath(d.socket)))

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	if got := c.awaitSettled(idAlpha); got.State != rpc.StateParked {
		t.Fatalf("the session settled as %q rather than parked, so the write-down never ran and this asserts nothing", got.State)
	}

	found := false
	for _, f := range c.seen {
		if f.Kind == rpc.FrameError && f.SessionID == idAlpha && strings.Contains(f.Text, "written down") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a park whose write-down failed sent the attached client no error naming %s: ⌃C promised a "+
			"recovery the next daemon cannot make, with only a logf to show for it\nsaw: %s", idAlpha, c.transcript())
	}
}

// The bookParked arm: a booking that fails at shutdown queues the error for the
// clients still attached.
//
// Driven directly, for TestTheParkBookRecordsExactlyTheSessionsThatParked's
// reason and one more of its own. That test drives bookParked directly because
// completePark writes the same record from a fan-out goroutine and steals the
// booking on an idle machine; this one does because the broadcast's delivery
// over a live ⌃Q races closeClients, and the property under test is that the
// daemon *tells* an attached client, which is settled at the enqueue. The client
// needs no connection: broadcast is a non-blocking enqueue onto c.out, so a
// booking that failed leaves exactly one frame waiting there.
func TestAFailedBookingAtShutdownTellsTheAttachedClients(t *testing.T) {
	socket := tempSocket(t)
	s := newServer(socket)

	c := newClient(nil)
	s.addClient(c)

	// A session that parked cleanly and is not yet in the book, so bookParked
	// reaches the write rather than skipping it as already held or unbookable.
	a := parkedAgentRow(parkedRecord{ID: idAlpha, Name: "alex", Dir: "/tmp/repo"}, "alex")

	unwritable(t, filepath.Dir(parkBookPath(socket)))

	s.bookParked([]*agent{a})

	select {
	case f := <-c.out:
		if f.Kind != rpc.FrameError || f.SessionID != idAlpha || !strings.Contains(f.Text, "written down") {
			t.Fatalf("bookParked queued %+v for the attached client, want an error naming %s that says it "+
				"could not be written down", f, idAlpha)
		}
	default:
		t.Fatalf("a booking that failed at shutdown queued the attached client nothing: ⌃Q promised a "+
			"recovery the next daemon cannot make, with only a logf to show for it")
	}
}

// unwritable makes a directory one nothing may create a file in, and restores it
// when the test ends. The restore is registered after whatever created the
// directory, so it runs first (cleanups are LIFO) and the tree can be removed.
//
// It is how both arms fail the atomic write the park book goes through:
// writeFileAtomically calls os.CreateTemp in the destination's own directory,
// which needs write permission on it.
func unwritable(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod %s unwritable: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Errorf("restore %s: %v", dir, err)
		}
	})
}

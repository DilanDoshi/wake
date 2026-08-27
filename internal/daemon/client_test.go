// Backpressure, which is the failure this whole package is arranged around.
//
// Deferred item I5, in full: "Backpressure terminates at claude's stdout, not
// at the daemon... one stalled client freezes all 30 agents mid-turn,
// inverting the daemon's entire reason to exist." The chain is core's event
// buffer blocking its pump, the pump then not draining claude's stdout, and
// claude blocking on a full pipe mid-turn. Anything that can stall a fan-out
// is a link in it - a lock, a bounded queue, a slow socket - so the tests
// here are about a fan-out that cannot stall.

package daemon

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The one that matters: a client stops reading and an agent keeps working.
//
// The marker is the discrimination. It is emitted only after the agent has
// written every one of its burst frames, which is far more than the pipe, the
// event buffer and the client queue can hold between them - so an agent that
// was frozen by the stalled client never gets there, and no amount of waiting
// produces it.
func TestALaggingClientLosesFramesRatherThanFreezingTheAgent(t *testing.T) {
	const burst = 4000
	fakeClaudeOnPath(t, "flood")
	t.Setenv(fakeCountEnv, fmt.Sprint(burst))
	shortWriteTimeout(t, 300*time.Millisecond)

	d := startDaemon(t)
	lagging := dialSilent(t, d.socket)
	healthy := attach(t, d.socket)

	healthy.spawn(idAlpha, "sydney")

	// The agent got through the whole burst with a client wedged on the
	// other side of the fan-out.
	healthy.awaitEvent(idAlpha, "flood done")

	// And the wedged client is gone rather than pending: a write that times
	// out means this client is not coming back, and holding it open holds
	// rpc's process-wide write lock with it.
	if err := waitForHangup(lagging, hangupBound); err != nil {
		t.Errorf("the daemon never hung up on a client that stopped reading: %v", err)
	}
}

// The other half of drop-and-mark: the client is told. A transcript with a
// silent hole in it is worse than one that says where the hole is - the
// client is rendering a conversation, and a missing tool result reads as an
// agent that did nothing.
func TestADroppedFrameIsConfessedBeforeTheNextOne(t *testing.T) {
	const overflow = 10

	server, peer := net.Pipe()
	c := newClient(server)
	t.Cleanup(func() {
		c.close()
		_ = peer.Close()
	})

	// Nothing is draining yet, so the queue fills and then drops.
	for i := range clientQueue + overflow {
		c.enqueue(rpc.Frame{Kind: rpc.FrameEvent, SessionID: fmt.Sprintf("s%d", i)})
	}
	if got := c.dropped.Load(); got != overflow {
		t.Fatalf("dropped = %d, want %d - the queue did not overflow the way this test needs", got, overflow)
	}

	go c.write()

	frames, errs := rpc.ReadFrames(peer)
	defer func() {
		_ = peer.Close()
		for range frames {
		}
		<-errs
	}()

	select {
	case f := <-frames:
		if f.Kind != rpc.FrameError {
			t.Fatalf("first frame = %+v, want the gap reported before anything else", f)
		}
		if !strings.Contains(f.Text, fmt.Sprint(overflow)) {
			t.Errorf("gap notice = %q, want it to say how many frames were lost", f.Text)
		}
		// And it is recognisable *as* a gap notice. Two places identify one by
		// its text and neither can see the frame being built: the soak counts
		// them, and watchStates fails on one - a test that concludes "the
		// daemon never entered that state" from the frames it received is only
		// entitled to if none went missing. This is the one test that drives
		// the real drop path, so it is where the ends of that coupling are
		// pinned together.
		if !strings.Contains(f.Text, gapNotice) {
			t.Errorf("gap notice = %q, want it to contain %q - the soak and watchStates both look for that", f.Text, gapNotice)
		}
	case err := <-errs:
		t.Fatalf("read: %v", err)
	case <-time.After(testTimeout):
		t.Fatal("nothing was written to a client with a full queue")
	}
}

// The same shape one layer down from internal/ui/inbox.go, and the same rule.
// Every output token has been an ordinary frame since
// --include-partial-messages, so a client that falls behind fills this queue
// with previews and the next permission request finds no room. Nothing on that
// wire times out.
//
// Mutation check: delete the ceiling from enqueue and this fails with the room
// the record needs already spent.
func TestPreviewsDoNotFillTheQueueTheRecordNeeds(t *testing.T) {
	server, peer := net.Pipe()
	c := newClient(server)
	t.Cleanup(func() {
		c.close()
		_ = peer.Close()
	})

	// Nothing is draining, so this is a client four queues behind on tokens
	// alone.
	for range clientQueue * 4 {
		c.enqueue(previewFrame("s1", "tok "))
	}
	if got := c.dropped.Load(); got != 0 {
		t.Errorf("dropped = %d, want 0: a lost preview is not a hole in this client's view", got)
	}

	// And the room the record needs is still there.
	for i := range clientQueue - partialCeiling {
		c.enqueue(rpc.Frame{Kind: rpc.FrameEvent, SessionID: fmt.Sprintf("s%d", i)})
	}
	if got := c.dropped.Load(); got != 0 {
		t.Errorf("dropped = %d after %d frames of record behind a flood of tokens, want 0", got, clientQueue-partialCeiling)
	}
}

// previewFrame is one output token on its way to a client: the frame kind that
// is a preview of a block being written rather than a record of one.
func previewFrame(sessionID, text string) rpc.Frame {
	return rpc.Frame{
		Kind:      rpc.FrameEvent,
		SessionID: sessionID,
		Event:     &core.Event{Kind: core.KindPartialText, SessionID: sessionID, Text: text},
	}
}

// enqueue is called from the goroutine draining an agent's stdout. If it can
// block for any reason, that agent stops being drained and freezes mid-turn -
// which is the whole chain in I5. This asserts the property directly rather
// than through a fleet, because it is the property, and a bounded queue
// passes every other test in this file while failing this one.
func TestHandingAFrameToAClientNeverBlocks(t *testing.T) {
	server, peer := net.Pipe()
	c := newClient(server)
	t.Cleanup(func() {
		c.close()
		_ = peer.Close()
	})

	// No writer goroutine at all, so nothing is ever taken off the queue.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range clientQueue * 4 {
			c.enqueue(rpc.Frame{Kind: rpc.FrameEvent})
		}
	}()

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("handing frames to a client with a full queue blocked: the agent behind this fan-out is frozen mid-turn")
	}
}

// hangupBound is how long the daemon gets to give up on a client that stopped
// reading, with the write deadline compressed to 300ms above. Generous
// against a loaded machine and far short of the 15s a stalled test would take.
const hangupBound = 5 * time.Second

// dialSilent attaches a client that never reads a byte. Its socket buffer
// fills, and then every write to it blocks - which is the state that holds
// rpc's process-wide write lock and stalls every other client.
func dialSilent(t *testing.T, socket string) net.Conn {
	t.Helper()

	conn, err := Dial(socket)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// waitForHangup reports whether the daemon closed this connection.
//
// The timeout case is separated from every other error on purpose, and it is
// the whole test. The first draft returned nil for *any* read error, so a read
// deadline expiring - which is what happens when the daemon never hangs up at
// all - read as success. Deleting the write deadline from the daemon left that
// version passing, which is how it was found.
func waitForHangup(conn net.Conn, within time.Duration) error {
	if err := conn.SetReadDeadline(time.Now().Add(within)); err != nil {
		return err
	}
	buf := make([]byte, 4096)
	for {
		// Reading now, after the daemon gave up: whatever it managed to
		// write is drained until the close arrives.
		_, err := conn.Read(buf)
		if err == nil {
			continue
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return fmt.Errorf("still connected after %v", within)
		}
		return nil
	}
}

// shortWriteTimeout compresses the write deadline for one test. Five seconds
// is the right production value - a local socket that has not accepted a byte
// in five seconds has a client that is not coming back - and far too long to
// wait for in a test.
func shortWriteTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := clientWriteTimeout
	clientWriteTimeout = d
	t.Cleanup(func() { clientWriteTimeout = prev })
}

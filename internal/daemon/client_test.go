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
		// And the human-readable half opens with the word that names it - this is
		// the log line a person reads, distinct from the typed count machines
		// route on (TestADroppedFrameCarriesTheTypedGapCount). This is the one
		// test that drives the real drop path, so it pins the text half here.
		if !strings.Contains(f.Text, gapNotice) {
			t.Errorf("gap notice = %q, want it to open with %q", f.Text, gapNotice)
		}
	case err := <-errs:
		t.Fatalf("read: %v", err)
	case <-time.After(testTimeout):
		t.Fatal("nothing was written to a client with a full queue")
	}
}

// The gap is a *typed* signal and not only a sentence. The window routes its own
// ring's gap through one invalidation - forgotModes and Fleet.ForgetTurns - and
// the daemon's queue overflow has to reach the same one; recognising it by its
// text is the string-matching gapNotice's own comment complains two other callers
// do. So the gap frame carries the count in rpc.Frame.Dropped, which is what the
// UI reads.
//
// Mutation check: drop `gap.Dropped = n` from flush and this fails at 0.
func TestADroppedFrameCarriesTheTypedGapCount(t *testing.T) {
	const overflow = 7

	server, peer := net.Pipe()
	c := newClient(server)
	t.Cleanup(func() {
		c.close()
		_ = peer.Close()
	})

	for i := range clientQueue + overflow {
		c.enqueue(rpc.Frame{Kind: rpc.FrameEvent, SessionID: fmt.Sprintf("s%d", i)})
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
		if f.Dropped != overflow {
			t.Errorf("gap Dropped = %d, want %d - the typed count the UI routes its invalidation off", f.Dropped, overflow)
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

// drained is what a client's queue holds, in order, without writing any of it.
func drained(c *client) []rpc.Frame {
	var out []rpc.Frame
	for {
		select {
		case f := <-c.out:
			out = append(out, f)
		default:
			return out
		}
	}
}

// A dropped token leaves the window with a block it has only the end of, and the
// window has no way to see that: a fence it never saw open reads as prose. So the
// next preview frame the daemon does queue for that session says tokens were lost
// before it - and only that session's, and only once.
func TestThePreviewAfterADroppedOneIsMarked(t *testing.T) {
	server, peer := net.Pipe()
	c := newClient(server)
	t.Cleanup(func() {
		c.close()
		_ = peer.Close()
	})

	for range partialCeiling {
		c.enqueue(previewFrame("s1", "tok "))
	}
	c.enqueue(previewFrame("s1", "dropped "))
	c.enqueue(previewFrame("s2", "dropped "))
	if got := len(c.out); got != partialCeiling {
		t.Fatalf("%d frames queued, want the %d the ceiling allows: the fixture never dropped one", got, partialCeiling)
	}

	<-c.out
	<-c.out
	<-c.out
	c.enqueue(previewFrame("s1", "after "))
	c.enqueue(previewFrame("s1", "again "))
	c.enqueue(previewFrame("s3", "clean "))
	frames := drained(c)
	marks := map[string][]bool{}
	for _, f := range frames[len(frames)-3:] {
		marks[f.SessionID] = append(marks[f.SessionID], f.Lost)
	}
	if got := marks["s1"]; len(got) != 2 || !got[0] || got[1] {
		t.Errorf("s1's previews after the drop are marked %v, want [true false]: the first says tokens were lost, the next does not repeat it", got)
	}
	if got := marks["s3"]; len(got) != 1 || got[0] {
		t.Errorf("s3 lost nothing and its preview is marked %v", got)
	}
	for _, f := range frames[:len(frames)-3] {
		if f.Lost {
			t.Fatalf("a preview queued before any drop is marked lost: %+v", f)
		}
	}
}

// A mark that could not be queued is not spent: the frame that carried it was
// lost too, so the next one says so.
func TestAMarkOnADroppedPreviewIsNotSpentUntilAFrameCarriesIt(t *testing.T) {
	server, peer := net.Pipe()
	c := newClient(server)
	t.Cleanup(func() {
		c.close()
		_ = peer.Close()
	})

	for range partialCeiling {
		c.enqueue(previewFrame("s1", "tok "))
	}
	c.enqueue(previewFrame("s1", "dropped "))
	<-c.out
	for range clientQueue { // the record fills what the ceiling kept free
		c.enqueue(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s9"})
	}
	c.enqueue(previewFrame("s1", "refused by the full queue "))
	drained(c)
	c.enqueue(previewFrame("s1", "queued at last "))
	if f := drained(c); len(f) != 1 || !f[0].Lost {
		t.Errorf("the preview queued after a refused marked one carries %+v, want Lost", f)
	}
}

// A message start is where a window begins reading afresh, so tokens lost in the
// block before it are not a reason to distrust the one after.
func TestAMessageStartRetiresTheMark(t *testing.T) {
	server, peer := net.Pipe()
	c := newClient(server)
	t.Cleanup(func() {
		c.close()
		_ = peer.Close()
	})

	for range partialCeiling {
		c.enqueue(previewFrame("s1", "tok "))
	}
	c.enqueue(previewFrame("s1", "dropped "))
	drained(c)
	c.enqueue(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{Kind: core.KindMessageStart, SessionID: "s1"}})
	c.enqueue(previewFrame("s1", "the next block "))
	for _, f := range drained(c) {
		if f.Lost {
			t.Errorf("a preview after a message start is marked lost: %+v", f)
		}
	}
}

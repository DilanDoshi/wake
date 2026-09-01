// One attached client, and the two bounds that keep it from taking the fleet
// with it when it stops reading.
//
// This is the file the whole daemon turns on. Backpressure does not terminate
// here: core's event buffer blocks its pump when full, the pump then stops
// draining claude's stdout, and the agent freezes mid-turn. So a chain that
// ends in "the client is slow" ends in "all 30 agents stop working", which
// inverts the daemon's entire reason to exist.
//
// Two different failures, two different bounds:
//
//   - **A client that reads slowly.** Its queue fills. Frames are dropped and
//     the gap is reported to that client. A *bounded queue* would not fix
//     this - it bounds memory, not liveness, and a fan-out blocked on a full
//     queue freezes the agent exactly as a fan-out blocked on a lock does.
//     Dropping is the only answer that keeps the agent running, and core's
//     own Session.send sets the precedent deliberately.
//   - **A client that has stopped reading entirely.** Its socket buffer
//     fills and conn.Write parks forever. Two things answer that: the write
//     gets a deadline, and it happens outside rpc's process-wide write lock -
//     see send. Inside that lock, one wedged peer stalls every write to every
//     other client, which was observed rather than theorised.
//
// What is deliberately *not* dropped: nothing is prioritised, and a dropped
// frame is not retried. A client that lost frames is told how many, so it
// knows its transcript has a hole rather than quietly rendering a wrong one.

package daemon

import (
	"bytes"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// clientQueue is how far a client may run behind before it starts losing
// frames. Sized for a burst, not for a backlog: 20 agents mid-turn produce
// events in bursts of a few dozen, and a client that cannot clear this in the
// time it takes to fill it is not going to catch up by being given more room.
const clientQueue = 512

// partialCeiling is how much of that queue a preview may occupy.
//
// --include-partial-messages makes every output token an ordinary frame -
// ~1,300/s across a busy fleet at the corpus median and ~2,800/s at its
// recorded maximum, against ~100/s of everything else. Without a ceiling a
// client that falls behind fills this queue with previews and the next
// permission request finds no room; nothing on that wire times out, so that
// ask is an agent blocked forever with nothing on any screen to answer.
//
// Half, because the half it reserves is what the burst above is sized against:
// 256 frames of headroom against ~100/s of record covers a stall far longer
// than the 250ms internal/ui/inbox.go's ring is sized for. It bites only while
// a client is hundreds of frames behind, which is exactly when a preview is
// worth nothing anyway - the completed block is behind it in the same queue.
const partialCeiling = clientQueue / 2

// clientWriteTimeout bounds one write to one client.
//
// A socket whose peer stopped reading fills and blocks. Without a bound the
// writer goroutine parks there for good, and this daemon would keep a dead
// client on the books forever. Five seconds is far longer than a local unix
// socket ever needs and short enough that a wedged client is gone rather than
// pending.
const defaultClientWriteTimeout = 5 * time.Second

// clientWriteTimeout is a var only so tests can compress it; nothing outside a
// test assigns it.
var clientWriteTimeout = defaultClientWriteTimeout

// client is one attached TUI: a queue, a writer goroutine and a connection.
type client struct {
	conn net.Conn

	// out is never closed. Closing it would race every goroutine that can
	// still hold this client - the agent input worker reporting a failed
	// write, a fan-out mid-broadcast - and a send on a closed channel
	// panics even inside a select with a default, which on this goroutine
	// would take the daemon and every session with it. The writer retires
	// on closing instead.
	out     chan rpc.Frame
	dropped atomic.Int64

	// pending is the writer goroutine's private encoding buffer. Only that
	// goroutine touches it, so it needs no lock and is reused rather than
	// allocated per frame. See send for why it exists at all.
	pending bytes.Buffer

	closing chan struct{}
	once    sync.Once
}

func newClient(conn net.Conn) *client {
	return &client{
		conn:    conn,
		out:     make(chan rpc.Frame, clientQueue),
		closing: make(chan struct{}),
	}
}

// enqueue hands one frame to this client, or drops it.
//
// It never blocks, and that is its whole contract: it is called from a
// session's fan-out goroutine, which is the goroutine draining that agent's
// stdout. Anything that parks here parks the agent.
//
// A preview is bounded to partialCeiling of the queue and is not counted when
// it is lost: the completed block replaces it a moment later, so a dropped
// token is not the gap flush confesses.
func (c *client) enqueue(f rpc.Frame) {
	preview := f.Event != nil && f.Event.Kind == core.KindPartialText
	if preview && len(c.out) >= partialCeiling {
		return
	}
	select {
	case c.out <- f:
	default:
		if !preview {
			c.dropped.Add(1)
		}
	}
}

// write is the client's writer goroutine: the only thing in the daemon that
// writes to this connection, so frames leave in the order they were queued.
func (c *client) write() {
	for {
		select {
		case <-c.closing:
			return
		case f := <-c.out:
			if err := c.flush(f); err != nil {
				// Not worth saying when this daemon is the one that hung
				// up: shutdown closes every connection, and every writer
				// then reports the close it was told to expect.
				if !c.closed() {
					logf("wake: hanging up on a client that stopped taking frames: %v", err)
				}
				c.close()
				return
			}
		}
	}
}

// closed reports whether this client has already been hung up on.
func (c *client) closed() bool {
	select {
	case <-c.closing:
		return true
	default:
		return false
	}
}

// gapNotice is the word that opens the human-readable half of a dropped-frames
// report. The count also rides the frame as the typed rpc.Frame.Dropped, and
// that is what every consumer routes on now: the UI's invalidation, the soak's
// count, watchStates' failure - none of them string-match this sentence any
// more. The text stays because a frame a person reads in a log should still say
// what happened, and TestADroppedFrameIsConfessedBeforeTheNextOne pins that half.
const gapNotice = "dropped"

// flush reports the gap, if any, and then writes the frame.
//
// The gap frame goes first so a client learns it lost something *before* the
// frame that follows the hole, rather than being told afterwards about a
// transcript it has already rendered.
func (c *client) flush(f rpc.Frame) error {
	if n := c.dropped.Swap(0); n > 0 {
		gap := errorFrame("", fmt.Sprintf("%s %d frames: this client was not reading fast enough, so its view has a gap", gapNotice, n))
		gap.Dropped = int(n)
		if err := c.send(gap); err != nil {
			return err
		}
	}
	return c.send(f)
}

// send puts one frame on the wire: encoded through rpc, written to the socket
// here.
//
// The two steps are separate on purpose, and this is the second half of the
// backpressure story. rpc.WriteFrame serializes on a process-wide mutex,
// which is a correctness backstop for callers who share an io.Writer with no
// identity to key on - and it means whoever is inside a slow conn.Write holds
// that lock and stalls every write to every other client. That is not a
// hypothetical: with the deadline compressed, a wedged client got a healthy
// one disconnected, because the healthy one's deadline expired while it was
// still queueing for the lock.
//
// Encoding into a private buffer keeps the lock over a memcpy and nothing
// else. The socket write happens outside it, under a deadline, so one client's
// stalled peer costs that client and no other.
//
// This is not a second write path in rpc's sense. The hazard that lock exists
// for is two goroutines interleaving bytes on one connection; here exactly one
// goroutine ever writes to this conn - the writer this method runs on - and it
// writes each frame in a single Write of complete bytes.
//
// A deadline that expires mid-write can leave a partial frame on the socket.
// The client is being hung up on at that point, so it sees a truncated last
// line and then EOF; rpc's reader reports that as a decode error, which is
// what it is.
func (c *client) send(f rpc.Frame) error {
	c.pending.Reset()
	if err := rpc.WriteFrame(&c.pending, f); err != nil {
		return err
	}
	if err := c.conn.SetWriteDeadline(time.Now().Add(clientWriteTimeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	if _, err := c.conn.Write(c.pending.Bytes()); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	return nil
}

// close retires the writer and hangs up. Idempotent: the reader goroutine,
// the writer and shutdown all reach it, and only the first does anything.
func (c *client) close() {
	c.once.Do(func() {
		close(c.closing)
		_ = c.conn.Close()
	})
}

package rpc

// Writing and reading frames on a connection. Split from wire.go, which
// declares what a frame is, at that subject seam.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// writeMu serializes every write in this package. Sessions fan out to one
// connection from many goroutines, and two concurrent Writes interleave
// bytes and corrupt the stream beyond recovery.
//
// It is process-wide rather than per-connection because io.Writer has no
// identity to key on and no close hook to clean up by. That makes it a
// correctness backstop, not a throughput strategy, and it puts a liveness
// requirement on every caller: a writer that blocks - a client whose socket
// buffer filled because it stopped reading, a laptop whose lid shut - holds
// this lock and stalls every write in the process, to every other client.
//
// A per-client writer goroutine with a bounded queue does NOT fix that. It
// bounds memory; the goroutine still blocks inside conn.Write holding this
// lock, and every other client's writer then blocks acquiring it. Add a
// mutex the daemon holds across the write and one wedged client wedges the
// daemon permanently.
//
// What actually restores liveness is a bound on the write itself:
// conn.SetWriteDeadline before each WriteFrame, treating the timeout as
// "this client is gone" and closing the connection. A per-connection lock
// instead of this one would also do it, at the cost of an ownership story
// io.Writer cannot express. Either way the fix belongs to whoever owns the
// connections, because that is the only layer that knows when to hang up.
var writeMu sync.Mutex

// WriteFrame writes one newline-terminated JSON frame. It serializes writes
// so concurrent senders cannot interleave bytes on one connection, and the
// JSON encoding escapes any newline in the payload so a multi-line message
// stays a single frame.
//
// It encodes into a local buffer rather than straight to w for three
// reasons: the encode stays outside the lock, an encode failure writes
// nothing at all rather than half a frame, and the critical section is
// exactly one Write of the complete frame. Encode supplies the trailing
// newline, so there is no separate terminator to forget.
//
// This is the only write path in the package; adding a second one that
// bypasses writeMu would reintroduce interleaving.
//
// Writing a frame *into a buffer* and then writing that buffer to a socket is
// not a second path, and internal/daemon does exactly that on purpose. The
// hazard this lock exists for is two goroutines interleaving bytes on one
// connection; the daemon gives each connection a single writer goroutine, so
// the only thing it needs from this function is the encoding, and taking the
// socket write out from under a process-wide lock is what stops one stalled
// peer from stalling every other client. Do not "simplify" that back to
// WriteFrame(conn, f).
func WriteFrame(w io.Writer, f Frame) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(f); err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}

	writeMu.Lock()
	defer writeMu.Unlock()
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	return nil
}

// defaultWriteTimeout bounds one client write to the daemon.
//
// It is the same five seconds the daemon gives a write to a client, and
// deliberately so: the two ends of a unix socket on one machine have the same
// answer to "how long can a peer that is behaving take", and a bound either
// side can outlast is a bound the other side has to guess at.
//
// writeTimeout is a var only so tests can compress it; nothing outside a test
// assigns it. Unsynchronised, and safe on the same terms as the compressible
// timeouts elsewhere in this tree: no test that assigns it runs in parallel.
const defaultWriteTimeout = 5 * time.Second

var writeTimeout = defaultWriteTimeout

// WriteFrameTo writes one frame to a socket under a deadline. Every client in
// this tree writes through this and not through WriteFrame.
//
// # Why the deadline is not optional
//
// writeMu is process-wide, and WriteFrame holds it across w.Write. A daemon
// that stops draining a client's frames fills that client's socket buffer, and
// the write then parks *inside the lock* - so the caller that parks takes every
// other write in the process with it, including the ones a user is waiting on.
// The header above states the hazard and prescribes exactly this fix; the
// daemon applied it to its own writes and the client side never did, which is
// how one wedged write became three call sites with no bound at all.
//
// This is not a second write path in the sense the header forbids. It adds a
// deadline and calls WriteFrame; the encoding, the lock and the single Write of
// a complete frame are unchanged, so nothing here can interleave bytes.
//
// # Why the deadline is set and never cleared
//
// Clearing it is what an earlier version of this function did, and it restored
// the exact failure it exists to prevent - at this function's worst call site.
// The deadline belongs to a *connection*, not to a call, and the clearing
// necessarily happens after WriteFrame has released writeMu. Two goroutines
// writing to one connection then interleave like this:
//
//	second: SetWriteDeadline(+5s)
//	first:  Write completes, unlocks writeMu
//	first:  SetWriteDeadline(zero)      <- removes the second's bound
//	second: takes writeMu, Write parks   <- forever, inside the lock
//
// and every write in the process queues behind it. bubbletea runs every tea.Cmd
// on its own goroutine, so two Enter presses in quick succession are exactly
// two concurrent calls here on the one TUI connection. There is no ordering
// that makes a set/write/clear triple safe unless the clear is inside the lock,
// and putting it there means a second write path through rpc's encoder.
//
// Nothing needs it cleared. A leftover write deadline does not bound a read -
// `wake stop` writes its quit and then reads for two minutes under its own
// SetReadDeadline, unaffected - and every write on these connections comes
// through this function, which sets its own bound first. A caller that writes
// to a wake connection by any other path inherits whatever is on it, which is
// one more reason for there to be no other path.
//
// Two writers that both set a deadline before either writes leave the second
// with a bound that started earlier and so expires sooner. Tighter, never
// absent, on a connection already demonstrating that it is sick.
//
// # What a timeout means, and what the caller owes
//
// The deadline can expire mid-write, which leaves a partial frame on the wire.
// The peer's reader then reports a decode error and ends the connection, which
// is the correct outcome and not a recoverable one: **a connection this
// returned an error for must not be written to again.** The caller owns the
// connection and so owns the hanging up - this cannot do it, because a
// transport that closed its caller's socket would be deciding a policy only the
// caller knows (the TUI reattaches; `wake stop` reports and exits).
func WriteFrameTo(c net.Conn, f Frame) error {
	if err := c.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return fmt.Errorf("bounding the write: %w", err)
	}
	return WriteFrame(c, f)
}

// ReadFrames decodes frames until the reader is exhausted. Both channels
// are closed when the goroutine finishes, so a caller can range over frames
// and then check errs.
//
// The reader ends on the first malformed frame: newline framing means a
// decode failure is a peer that is not speaking this protocol or a stream
// already desynced, and continuing would launder corruption into plausible
// frames. Ending is not crashing - the error is surfaced, both channels
// close, and the goroutine returns, which for a daemon means "close this
// connection and let the client reconnect".
//
// A vanishing client is the ordinary case, not an error: a closed
// connection reads as EOF and closes both channels with nothing on errs.
//
// The caller must drain frames until it is closed. Abandoning it while the
// reader still has data parks the goroutine on a send forever; closing the
// underlying reader does not unblock a goroutine already stalled there.
func ReadFrames(r io.Reader) (<-chan Frame, <-chan error) {
	frames := make(chan Frame, framesBuffer)
	errs := make(chan error, 1)

	go func() {
		defer close(frames)
		defer close(errs)

		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, readBufBytes), maxFrameBytes)
		for sc.Scan() {
			// A bare newline is not a frame. Skipping it keeps a stray
			// blank line from surfacing as a zero-valued Frame.
			if len(sc.Bytes()) == 0 {
				continue
			}
			var f Frame
			// Unmarshal allocates every string it keeps, so the decoded
			// frame does not alias the buffer the scanner reuses.
			if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
				errs <- fmt.Errorf("decode frame: %w", err)
				return
			}
			frames <- f
		}
		if err := sc.Err(); err != nil {
			errs <- fmt.Errorf("read frames: %w", err)
		}
	}()

	return frames, errs
}

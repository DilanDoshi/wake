package ui

// The buffer between the socket and the draw loop, and the goroutine that
// fills it.
//
// # What this exists for
//
// Bubble Tea has one Update goroutine and it draws. Before this, that same
// goroutine was also the only thing taking frames off the daemon's connection:
// App.listen delivered exactly one frame per Update and Update re-armed it, so
// anything slow inside Update stopped the socket being read. A width change is
// slow - measured at 248ms on a 3,000-event transcript, and a window drag is
// tens of those - and the daemon hangs up on a client whose write blocks for
// five seconds. An ordinary drag of a terminal window therefore disconnected a
// live conversation, permanently.
//
// So the drain is a goroutine of its own that does nothing but read. It cannot
// be blocked by rendering, by glamour's process-global lock, or by a user
// holding a key down, because it does not touch any of them.
//
// # Why it is bounded, and why it drops the oldest
//
// Unbounded, a client that renders slower than the fleet talks would grow
// until the machine died - which is a worse failure than the one being fixed,
// and the same reason internal/daemon bounds its side. Bounded and silent is
// worse still: a transcript with a hole in it that nobody was told about is a
// wrong transcript. So it is bounded, it drops, and it reports the drop in the
// daemon's own vocabulary - the client learns its view has a gap.
//
// It drops the *oldest* where the daemon drops the newest, and the difference
// is not an inconsistency. The daemon is sending into a channel and has no
// cheap way to reach the far end of it; this holds the whole buffer and can
// choose. What a reader needs after an outage is where the agent is *now*, and
// the newest frames are the ones that carry it - the newest status push is the
// authoritative one, and the newest events are the ones about to be read.
// Dropping the newest would keep a stale window and then jump.
//
// # A preview may never cost the record a slot
//
// --include-partial-messages makes every output token an ordinary frame:
// ~1,300/s across a fleet at the corpus median and ~2,800/s at its recorded
// maximum, against ~100/s of everything else. Taking a slot each, tokens spend
// the ring on previews and what a 250ms stall then evicts is completed blocks,
// permission requests, receipts and turn endings. Nothing on the permission
// wire times out, so an evicted ask is an agent blocked forever with nothing on
// screen to answer. App.wants cannot help: it runs after frames leave here.
//
// So a partial is not an ordinary frame in this buffer, and two rules say what
// it is instead. It **folds** into the session's unconsumed one rather than
// taking a slot - deltas are additive, so the tokens that arrive during a stall
// cost one slot per talking session however fast it talks, and the ring's
// headroom stops depending on the token rate. A frame of any other kind for
// that session closes the fold, because the block that supersedes a preview
// arrives between two of them and the next block's tokens are not a
// continuation of the last one's. And it **never evicts**: a partial that
// arrives with the ring full and no fold to join is dropped where it stands.
//
// It is not counted as dropped either, at either end of that. A preview is
// replaced by the completed block a moment later, so losing one is not the hole
// in the transcript App.stream's notice describes - and that notice discards
// this window's permission-mode beliefs, which a lost token is no reason to do.
// dropped therefore counts frames that were the record and nothing else.

import (
	"sync"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// inboxFrames is how far behind the draw loop may fall before the view
	// starts losing frames.
	//
	// Sized from the measurements that bound it on both sides. One re-wrap of
	// a 3,000-event transcript is ~250ms and the resize debounce means a whole
	// drag costs one of those, so the stall to cover is a quarter of a second;
	// the review's working figure for a busy fleet is 30 sessions at ~100
	// frames/s, so that stall is ~750 frames. This holds nearly three times
	// that.
	//
	// It is not larger because the room is not free: an rpc.Frame is ~150
	// bytes of header before anything it points at, so the ring itself is
	// ~300KB per connection, allocated for the life of the window. Wake sits
	// open all day beside 15-30 agents and has no excuse to cost much. Past
	// this bound the client is not behind, it is losing ground, and more room
	// does not change that - the same argument client.go makes for its queue.
	inboxFrames = 2048

	// takeLimit is how many frames one Update folds in.
	//
	// It exists because a batch is the whole point: Bubble Tea calls View
	// after every message, View is a fixed ~400µs at any transcript length,
	// and one frame per message meant one full frame drawn per event. Folding
	// a batch draws once for all of them.
	//
	// Bounded because the other direction is just as real: folding one event
	// costs ~117µs, so an unbounded batch is an unbounded Update, and a
	// keystroke waits behind all of it. 256 puts one Update in the tens of
	// milliseconds at the very worst - and far less in practice, because most
	// of a fleet's frames belong to other sessions and are discarded in
	// nanoseconds.
	takeLimit = 256

	// foldWidth is the pane the fold's bound is taken at. Wider than any
	// terminal, so the tail foldChars keeps is always more than a preview can
	// draw and the cut is never visible.
	foldWidth = 800
)

// foldChars bounds the text one folded partial holds, keeping the newest the
// way partial.add does.
//
// It is a bound on *work* rather than on what is shown. Deltas are additive, so
// a draw loop stalled for a minute would otherwise grow one string per session
// for as long as it stalls, and appending to a string costs its length.
var foldChars = previewChars(foldWidth)

// inbox is the frames that have arrived and not yet been drawn.
//
// One pump goroutine writes it and the Bubble Tea loop reads it, so unlike
// everything else in this package it is a pointer with a lock rather than a
// value that is copied. An App holds the pointer; a copy of that App shares the
// buffer, which is correct - there is one connection.
type inbox struct {
	// ready is the doorbell: a capacity-1 channel that a non-blocking send
	// leaves set. It is a signal and never carries data, so a pump that
	// arrives while one is already pending costs nothing and loses nothing -
	// take drains everything that is there whenever it is rung.
	ready chan struct{}

	mu   sync.Mutex
	ring []rpc.Frame
	head int
	n    int

	// folds is where each session's unconsumed partial sits, so the next token
	// can be appended to it rather than take a slot. An entry is removed when
	// its slot is handed over, when it is evicted, and when a frame of another
	// kind for that session closes it.
	folds map[string]int

	dropped int
	err     error
	ended   bool
}

func newInbox() *inbox {
	return &inbox{
		ready: make(chan struct{}, 1),
		ring:  make([]rpc.Frame, inboxFrames),
		folds: make(map[string]int),
	}
}

// pump drains the connection into the buffer until it ends. It is the only
// thing in this package that reads the stream, and the only reason C1 is
// closed rather than narrowed: it runs whatever Update is doing.
func (b *inbox) pump(s Stream) {
	for f := range s.Frames {
		b.add(f)
	}
	b.finish(streamEnd(s.Errs))
}

// add puts one frame in, dropping the oldest if there is no room - except for a
// partial, which folds and never evicts. See the header.
func (b *inbox) add(f rpc.Frame) {
	b.mu.Lock()
	if partialFrame(f) {
		b.addPartial(f)
	} else {
		// This session's preview is closed by anything else it says: the block
		// that supersedes one arrives between two turns' tokens.
		delete(b.folds, f.SessionID)
		if b.n == len(b.ring) {
			b.evictOldest()
		}
		b.put(f)
	}
	b.mu.Unlock()
	b.signal()
}

// partialFrame reports whether a frame is tokens of a block being written
// rather than a record of one.
func partialFrame(f rpc.Frame) bool {
	return f.Kind == rpc.FrameEvent && f.Event != nil && f.Event.Kind == core.KindPartialText
}

// addPartial folds this session's tokens into the slot they already have, or
// takes a free one. It never evicts: a preview is replaced by the completed
// block a moment later, and the frames it would push out are not.
func (b *inbox) addPartial(f rpc.Frame) {
	if at, ok := b.folds[f.SessionID]; ok {
		// A new Event rather than a write through the pointer: the frame the
		// socket handed over is not this buffer's to mutate.
		ev := *b.ring[at].Event
		ev.Text += f.Event.Text
		if len(ev.Text) > foldChars {
			// Bytes rather than runes for partial.add's reason: this bounds
			// work, and a rune halved at the front is dropped by the wrap.
			ev.Text = ev.Text[len(ev.Text)-foldChars:]
		}
		b.ring[at].Event = &ev
		return
	}
	if b.n == len(b.ring) {
		return
	}
	b.folds[f.SessionID] = (b.head + b.n) % len(b.ring)
	b.put(f)
}

// put writes one frame into the free slot behind the newest.
func (b *inbox) put(f rpc.Frame) {
	b.ring[(b.head+b.n)%len(b.ring)] = f
	b.n++
}

// evictOldest makes room by dropping the frame at the head. A preview there is
// not counted - what dropped means is that the record has a hole in it.
func (b *inbox) evictOldest() {
	if partialFrame(b.ring[b.head]) {
		b.unfold(b.head, b.ring[b.head].SessionID)
	} else {
		b.dropped++
	}
	b.ring[b.head] = rpc.Frame{}
	b.head = b.next(b.head)
	b.n--
}

// unfold retires a fold whose slot is no longer in the buffer. Keyed on the
// slot as well as the session, because a session whose block landed can have an
// older closed preview still in the ring and a newer open one behind it.
func (b *inbox) unfold(at int, sessionID string) {
	if i, ok := b.folds[sessionID]; ok && i == at {
		delete(b.folds, sessionID)
	}
}

// finish records why the stream ended and wakes the reader one last time.
func (b *inbox) finish(err error) {
	b.mu.Lock()
	b.err, b.ended = err, true
	b.mu.Unlock()
	b.signal()
}

// signal rings the doorbell without ever blocking on it.
func (b *inbox) signal() {
	select {
	case b.ready <- struct{}{}:
	default:
	}
}

func (b *inbox) next(i int) int { return (i + 1) % len(b.ring) }

// batch is what one Update folds: the frames that were waiting, how many were
// lost before them, and - once and only once - the end of the stream.
type batch struct {
	frames  []rpc.Frame
	dropped int

	// done marks the stream ended, with err saying why. It is reported only
	// after every frame that arrived before it has been handed over, so a
	// hang-up never overtakes the last thing the agent said.
	done bool
	err  error
}

// take removes up to limit frames.
//
// It re-rings the doorbell when it leaves frames behind, so a reader that
// waits on ready and calls this is guaranteed to come back for the rest
// without a timer and without polling.
func (b *inbox) take(limit int) batch {
	b.mu.Lock()
	defer b.mu.Unlock()

	k := min(b.n, limit)
	out := make([]rpc.Frame, k)
	for i := range k {
		at := (b.head + i) % len(b.ring)
		out[i] = b.ring[at]
		if partialFrame(out[i]) {
			// Handed over, so nothing may fold into it any more.
			b.unfold(at, out[i].SessionID)
		}
		// Cleared as it leaves. A frame carries an *core.Event and the text
		// of a whole assistant turn, and a ring that never releases its slots
		// holds a full buffer's worth alive for the life of the process - a
		// few megabytes of an app whose whole claim is that it is cheap to
		// leave open all day.
		b.ring[at] = rpc.Frame{}
	}
	b.head = (b.head + k) % len(b.ring)
	b.n -= k

	got := batch{frames: out, dropped: b.dropped}
	b.dropped = 0
	switch {
	case b.n > 0:
		b.signal()
	case b.ended:
		got.done, got.err = true, b.err
	}
	return got
}

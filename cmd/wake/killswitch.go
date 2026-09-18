package main

// The way out of a Wake that has stopped answering.
//
// # What this exists for
//
// Every exit Wake had went through Bubble Tea's Update loop: ⌃Q⌃Q parks the
// fleet and quits (the first ⌃Q arms), ⌃O then ↵ detaches, ⌃C parks one agent.
// All three are keys the loop reads, so all are gone the moment the loop is the
// thing that is stuck - and it can be. Update calls View, View is written through one
// os.File, and a terminal that stops draining that file parks the write inside
// the renderer's own mutex, which is the goroutine Bubble Tea reads messages
// on. Nothing after that point can be reached by pressing a key, because the
// keystroke's route to the model runs through the same loop.
//
// Measured rather than reasoned about: with bubbletea v1.3.10 in an alt screen
// with mouse tracking, an Update that never returns survives **SIGINT and
// SIGTERM both** - handleSignals does `p.msgs <- InterruptMsg{}` on an
// unbuffered channel that only the wedged loop reads, so the handler parks on
// the send. What is left is SIGHUP, SIGQUIT and SIGKILL, none of which run
// bubbletea's terminal restore: the operator gets their shell back inside an
// alt screen, with mouse reporting on and the tty still in raw mode. See
// TestAWedgedProgramSurvivesTheSignalsBubbleTeaHandles.
//
// # Why it is a byte and not a key
//
// A tea.KeyMsg is produced by the loop that is stuck, so a binding in
// internal/ui cannot be the answer here however it is spelled. This reads the
// tty itself, on a goroutine of its own that does nothing else, and decides
// before Bubble Tea has seen the byte - which is inbox.go's rule about the
// socket, one layer further out: nothing that renders may sit between the
// terminal and the thing that has to notice.
//
// It adds **no legend glyph and no tea.Key case**, which is ⎋⎋'s and the armed
// ⌃O's own reason: the legend is a bijection with App.key, and this is not in
// App.key at all.
//
// # Why two of the same key, when ⌃O's confirm is deliberately a different one
//
// detach.go rules that a same-key confirm is wrong, because terminal
// auto-repeat and the human reply to a key that appeared to do nothing produce
// the same bytes as intent. That ruling is about a key whose *first* press is
// invisible. ⌃C here has a visible first press - it parks the focused agent and
// says so - so a second press is never the reflex that follows silence. It is
// the reflex that follows *the first press not having worked*, which is exactly
// the state this exists for.
//
// The disarm is what keeps ⌃C meaning park: anything at all between the two
// presses takes it back, and parking two agents needs the roster cursor moved
// between them. That is App.disarmed's rule with the same shape and the same
// job.
//
// # Why ⌃C alone, and why ⌃Q was removed from the escape hatch
//
// This watched ⌃Q⌃Q as well, for redundancy: ⌃Q is XON and ⌃C is INTR, and if a
// layer that is not the tty driver - tmux, screen, ssh, cmux - ever ate one, the
// other still arrived. That redundancy is given up here, because keeping it cost
// the far more common failure. ⌃Q is the TUI's park-and-quit, and it is now
// armed (internal/ui/park.go): the first press arms, the second confirms, and
// the confirmed park waits up to three seconds for the daemon's acknowledgement
// before the window closes. While ⌃Q was also an emergency chord, a held key
// auto-repeating - or an impatient second tap during that visible delay -
// arrived as ⌃Q⌃Q in one read and fired *this* exit, which asks the daemon for
// nothing and leaves the fleet untouched. So a perfectly healthy park was
// pre-empted into a bare exit and every agent was left running - the
// fleet-still-running-after-⌃Q failure this whole change exists to close. ⌃C is
// not flow control, gets through where ⌃Q might not, and cannot collide with a
// park because it parks one agent rather than quitting - so it is the whole of
// the escape hatch now.

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/charmbracelet/x/term"
)

const (
	// keyCtrlC is the byte this watches for: the key Claude Code exits on, and
	// the one this build spends the emergency escape on. ⌃Q is no longer a second
	// one - see the header for why watching it pre-empted a healthy park.
	keyCtrlC = 0x03

	// keyEsc opens every escape sequence, including the SGR mouse reports a fast
	// scroll floods the tty with (`\x1b[<b;x;yM`). alignedCut holds a partial one
	// back across the queue handoff so a drop never splits one.
	keyEsc = 0x1b

	// killWindow is how long the first press stays armed. Long enough to be a
	// double press by a hand that has just watched nothing happen, short enough
	// that two deliberate parks a beat apart are two parks.
	killWindow = time.Second

	// restoreTimeout bounds the escape sequences the emergency exit writes. The
	// likeliest reason to be here at all is a terminal that stopped draining,
	// so this write is the one thing that must not be waited on.
	restoreTimeout = 250 * time.Millisecond

	// forwardQueue is how many reads may be waiting to reach Bubble Tea before
	// this stops keeping them.
	//
	// Bounded and dropping for inbox.go's reason, and the drop is the whole
	// point rather than a concession: the consumer being stuck is the case this
	// file exists for, and a pump that blocks handing over the byte *before*
	// the emergency quit never sees the emergency quit.
	forwardQueue = 64

	// readChunk is one read off the tty. A paste arrives in far larger pieces
	// than a keystroke, and the trigger reads whatever a chunk holds.
	readChunk = 4096

	// maxCarry bounds the unfinished sequence chunker holds between reads. Every
	// escape sequence that reaches here is a handful of bytes - the longest, an SGR
	// mouse report for a huge terminal, is well under this - so a carry past it is
	// not a pending sequence but garbage from a misbehaving source, and is flushed
	// rather than held unbounded. This file is the way out of a wedged Wake; it may
	// not grow memory on a stream nobody is draining.
	maxCarry = 64

	// exitEmergency is what the process exits with. 130 is the shell's own
	// "terminated by ⌃C", which is what this is.
	exitEmergency = 130
)

// killSignalGrace is how long a real signal waits for Bubble Tea to end the
// program itself before this ends it instead. A healthy program is gone well
// inside it; a wedged one never was going to be.
//
// A var only so tests can compress it, which is parkAckTimeout's own terms;
// nothing outside a test assigns it.
var killSignalGrace = 2 * time.Second

// emergencyLine is printed to the terminal on the way out. It promises nothing
// about the fleet, because this path asked the daemon for nothing: the agents
// are still running, and still parked or not, exactly as they were.
const emergencyLine = "Wake stopped responding and was closed without parking anything. " +
	"The fleet is untouched - `wake` reopens the room, `wake status` says what is there."

// killTrigger is the decision, and it is a value so the one thing here that can
// close somebody's window is testable without a terminal.
type killTrigger struct {
	count int
	at    time.Time
	spent bool
}

// saw folds one read of the tty and reports whether it completed the sequence.
//
// Every byte is looked at rather than only the first, because a fast double
// press arrives as one read - which is escprobe_test.go's finding about ⎋⎋ in
// the layer above, and is true here for the same reason.
func (k killTrigger) saw(b []byte, now time.Time) (killTrigger, bool) {
	if k.spent {
		return k, false
	}
	for _, c := range b {
		switch {
		case c != keyCtrlC:
			// Anything else disarms - ⌃Q included, which is what keeps it from
			// pre-empting the park. See the header: this is what keeps ⌃C
			// meaning park.
			k.count = 0
		case k.count > 0 && now.Sub(k.at) <= killWindow:
			k.spent = true
			return k, true
		default:
			// A first ⌃C, or one too late to be a second.
			k.count, k.at = 1, now
		}
	}
	return k, false
}

// killSwitch is the tty, read by Wake before Bubble Tea sees it.
//
// It owns raw mode, which Bubble Tea otherwise would: initInput only calls
// MakeRaw when the reader it was given is itself a terminal, and the reader it
// is given here is a pipe. Taking that over is the cost of being in front of
// the bytes, and it is paid back in one place - restore, which every exit from
// this file and from converseModel goes through.
type killSwitch struct {
	tty   *os.File // read from, and the descriptor raw mode is set on
	out   *os.File // written to, because a tty opened for reading may not take one
	state *term.State
	pipe  *os.File // the read end Bubble Tea is handed
	feed  *os.File // the write end the forwarder writes
	queue chan []byte

	// exit is the seam a test replaces. Nothing but a test assigns it.
	exit func()

	once sync.Once
}

// armKillSwitch puts Wake in front of the terminal, or reports that there is no
// terminal to be in front of.
//
// A nil switch is the whole of the not-a-tty case - `wake` with its input piped
// is not a session anybody is pressing keys at - and every method below is safe
// on one, so no caller tests for it.
func armKillSwitch() (*killSwitch, error) {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return nil, nil
	}
	state, err := term.MakeRaw(os.Stdin.Fd())
	if err != nil {
		return nil, fmt.Errorf("entering raw mode: %w", err)
	}
	pipe, feed, err := os.Pipe()
	if err != nil {
		_ = term.Restore(os.Stdin.Fd(), state)
		return nil, fmt.Errorf("opening the input pipe: %w", err)
	}
	k := newKillSwitch(os.Stdin, os.Stdout, state, pipe, feed)
	go k.pump()
	go k.forward()
	return k, nil
}

// newKillSwitch assembles one without touching a terminal, which is what lets a
// test drive the pump over a pipe.
func newKillSwitch(tty, out *os.File, state *term.State, pipe, feed *os.File) *killSwitch {
	k := &killSwitch{
		tty: tty, out: out, state: state,
		pipe: pipe, feed: feed,
		queue: make(chan []byte, forwardQueue),
	}
	k.exit = k.emergencyExit
	return k
}

// Input is what Bubble Tea reads, and it is a pipe rather than the terminal.
//
// A pipe and not a plain io.Reader: cancelreader picks its implementation off
// the reader's type, and only a file gets a cancellable one. A bare reader gets
// the fallback, whose Cancel cannot interrupt a read already in flight, and
// every quit then spends bubbletea's 500ms waitForReadLoop timeout.
func (k *killSwitch) Input() io.Reader {
	if k == nil {
		return os.Stdin
	}
	return k.pipe
}

// alignedCut is how much of buf ends on an escape-sequence boundary: buf[:cut]
// is safe to forward or to drop whole, and buf[cut:] is a trailing unfinished
// sequence to hold back for the next read.
//
// # Why the pump needs it
//
// A pipe is lossless and ordered, so the one way the pump corrupts Bubble Tea's
// input is the drop below: when the forward queue is full it lets a whole read
// go. A read ends at an arbitrary byte - during a scroll flood, almost always
// mid-report - so a dropped chunk splits the report either side of it, and Bubble
// Tea decodes the orphaned `<`, digits, `;` and `M` as typed runes that land in
// the composer. Holding the trailing unfinished sequence back means every chunk
// the pump enqueues (and so every chunk it drops) begins and ends on a boundary,
// and a gap between two boundaries cannot split a sequence.
//
// It reports a boundary only for the escape sequences that actually arrive here -
// CSI (SGR mouse `\x1b[<…M`, arrows, function keys), X10 mouse (`\x1b[M`+3 raw
// bytes) and SS3 (`\x1bO`+1) - and treats a completed sequence, plain runes and
// any other ESC+byte as already whole, since they need no continuation. Plain
// runes are self-delimiting, so everything up to the last unterminated ESC is
// aligned; chunker.step is what decides a genuinely lone trailing ESC is a keypress
// rather than an opening.
func alignedCut(buf []byte) int {
	e := bytes.LastIndexByte(buf, keyEsc)
	if e < 0 {
		return len(buf) // no escape sequence pending; runes are self-delimiting
	}
	if e+1 >= len(buf) {
		return e // a lone trailing ESC; step decides keypress vs opening
	}
	switch buf[e+1] {
	case '[': // CSI: SGR mouse, arrows, function keys - and X10 mouse
		if e+2 >= len(buf) {
			return e // `\x1b[` only so far; the CSI is still opening
		}
		// X10 mouse is the one sequence whose final byte is not the end: `\x1b[M`
		// is followed by three raw coordinate bytes. Wake asks for SGR
		// (`\x1b[?1006h`), so a report is almost always `\x1b[<…M`, but a terminal
		// without 1006 falls back to X10 - hold until all six bytes are here.
		if buf[e+2] == 'M' {
			if len(buf) >= e+6 {
				return len(buf)
			}
			return e
		}
		// Otherwise complete once a final byte (0x40-0x7e, ECMA-48) arrives after
		// `\x1b[`; the SGR params `<`, digits and `;` are all below 0x40.
		for i := e + 2; i < len(buf); i++ {
			if buf[i] >= 0x40 && buf[i] <= 0x7e {
				return len(buf)
			}
		}
		return e
	case 'O': // SS3: `\x1bO`+1 byte (application-mode arrows, F1-F4)
		if len(buf) >= e+3 {
			return len(buf)
		}
		return e
	default: // Alt+key and any other ESC+byte is already whole
		return len(buf)
	}
}

// chunker aligns a stream of reads to escape-sequence boundaries. It is the
// carry alignedCut's doc describes, kept as its own value so the invariant it
// holds - that every chunk it yields, and so every chunk a drop discards, begins
// and ends on a boundary - is testable without the pump's goroutines or a
// terminal, the way killTrigger is.
type chunker struct {
	// carry is the trailing partial sequence held back from the last read; it
	// begins with the ESC of an unfinished report, so prepending it to the next
	// read reassembles that report.
	carry []byte
}

// step folds one read and returns the chunk to forward, updating carry. It aligns
// on every read, not just a full one, because a mouse report split across two
// short reads (a byte stream over SSH or tmux delivers one however it likes) plus a
// coincident drop is the same leak a flood's full reads are - so a trailing partial
// is held whatever the read size.
//
// full says the read filled its buffer, so more is likely pending. It decides only
// the one genuinely ambiguous carry: a lone trailing ESC. On a full read it is the
// opening of a sequence whose rest is coming, so hold it; on a short read it is a
// real Escape keypress that must not wait for the next input, so forward it. A
// partial *sequence* (a split mouse report) is never a keypress, so it is held
// either way. This is bubbletea's own full-buffer heuristic, narrowed to the one
// byte it is actually ambiguous for.
//
// The returned chunk owns its bytes: step copies the read in, so the caller may
// enqueue it without the copy the raw pump needed, and dropping it is safe because
// it is boundary-aligned. carry updates whether or not the caller drops.
func (c *chunker) step(read []byte, full bool) []byte {
	data := make([]byte, 0, len(c.carry)+len(read))
	data = append(data, c.carry...)
	data = append(data, read...)
	cut := alignedCut(data)
	switch {
	case !full && cut == len(data)-1 && data[cut] == keyEsc:
		cut = len(data) // a lone trailing ESC on a short read is a keypress, not an opening
	case len(data)-cut > maxCarry:
		cut = len(data) // not a real pending sequence; do not hold it unbounded
	}
	c.carry = data[cut:]
	return data[:cut]
}

// pump is the read that never waits on anything downstream.
//
// It is the whole mechanism: the trigger is decided here, on this goroutine,
// before the bytes are handed anywhere. A hand-off that could block would put
// the wedged consumer back in front of the key that exists to escape it.
func (k *killSwitch) pump() {
	defer close(k.queue)
	var trigger killTrigger
	var chunks chunker
	buf := make([]byte, readChunk)
	for {
		n, err := k.tty.Read(buf)
		if n > 0 {
			var fired bool
			if trigger, fired = trigger.saw(buf[:n], time.Now()); fired {
				k.exit()
				return
			}
			// Boundary-aligned so a drop below cannot split a mouse report - see
			// alignedCut. step copies buf in, so the chunk is safe to enqueue and
			// buf is free to be overwritten by the next read.
			if chunk := chunks.step(buf[:n], n == len(buf)); len(chunk) > 0 {
				select {
				case k.queue <- chunk:
				default:
					// Dropped. See forwardQueue: a full queue means Bubble Tea has
					// stopped reading, and a report it will never read is worth less
					// than the next one being noticed. Safe because chunk ends on a
					// boundary, so the gap it leaves cannot split a report.
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// forward hands the bytes on, and is allowed to block doing it.
func (k *killSwitch) forward() {
	for chunk := range k.queue {
		if _, err := k.feed.Write(chunk); err != nil {
			return
		}
	}
	_ = k.feed.Close()
}

// restore puts the terminal back the way converseModel found it. Safe to call
// twice: the ordinary exit and the emergency one both reach it.
func (k *killSwitch) restore() {
	if k == nil {
		return
	}
	k.once.Do(func() {
		if k.state != nil {
			_ = term.Restore(k.tty.Fd(), k.state)
		}
	})
}

// restoreTerminal puts the terminal back as far as it can be, and is bounded.
//
// **The termios goes first and the escape sequences are bounded**, and that
// order is the failure this is for. A terminal that has stopped draining is the
// likeliest reason the loop is wedged, so the write below may never complete -
// but tcsetattr is an ioctl and lands anyway. Getting a cooked tty back is what
// makes the shell usable again; an alt screen is what `reset` fixes in one word.
//
// It writes straight to the file rather than through guardedOutput, because the
// mutex that serialises Wake's writes against the renderer's is one of the
// things a wedged renderer is holding.
//
// Split from emergencyExit so it is reachable without ending the process, which
// is the only way anything can assert that the terminal really does come back.
func (k *killSwitch) restoreTerminal() {
	k.restore()
	written := make(chan struct{})
	go func() {
		defer close(written)
		// Mouse reporting off, bracketed paste off, cursor back, alt screen
		// left - bubbletea's own restoreTerminalState, in the order it does it.
		_, _ = io.WriteString(k.out, "\x1b[?1006l\x1b[?1002l\x1b[?1003l\x1b[?2004l\x1b[?25h\x1b[?1049l")
		_, _ = fmt.Fprintf(k.out, "%s\r\n", emergencyLine)
	}()
	select {
	case <-written:
	case <-time.After(restoreTimeout):
	}
}

// emergencyExit is the ending this file exists to provide: the terminal back,
// and then out, without asking the daemon anything.
func (k *killSwitch) emergencyExit() {
	k.restoreTerminal()
	os.Exit(exitEmergency)
}

// watchSignals makes `kill` work on a window that has stopped answering.
//
// Bubble Tea installs its own handler for SIGINT and SIGTERM and it is the
// right one while the loop is alive - so this waits out a grace rather than
// racing it, and fires only if the program is still here afterwards. Go
// delivers a signal to every registered channel, so this costs bubbletea's
// handler nothing.
//
// A second signal is the operator saying the first did not work, and skips the
// grace. killSignals is per platform for one reason: SIGHUP and SIGQUIT are the
// two bubbletea handles neither of, and neither exists off unix.
//
// The exit fires exactly once: either the grace timer runs it, or a second
// signal that beat the grace runs it - never both. grace.Stop reports false once
// the timer has already fired, so guarding the second-signal exit on it is what
// keeps a second signal after the grace from ending the window twice. Harmless
// as os.Exit in production; a close-of-closed-channel panic under a test's exit.
func (k *killSwitch) watchSignals() {
	if k == nil {
		return
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, killSignals...)
	go func() {
		<-sig
		grace := time.AfterFunc(killSignalGrace, k.exit)
		<-sig
		if grace.Stop() {
			k.exit()
		}
	}()
}

package main

// The way out of a Wake that has stopped answering.
//
// # What this exists for
//
// Every exit Wake had went through Bubble Tea's Update loop: ⌃Q parks the fleet
// and quits, ⌃O then ↵ detaches, ⌃C parks one agent. All three are keys the
// loop reads, so all three are gone the moment the loop is the thing that is
// stuck - and it can be. Update calls View, View is written through one
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

// pump is the read that never waits on anything downstream.
//
// It is the whole mechanism: the trigger is decided here, on this goroutine,
// before the bytes are handed anywhere. A hand-off that could block would put
// the wedged consumer back in front of the key that exists to escape it.
func (k *killSwitch) pump() {
	defer close(k.queue)
	var trigger killTrigger
	buf := make([]byte, readChunk)
	for {
		n, err := k.tty.Read(buf)
		if n > 0 {
			var fired bool
			if trigger, fired = trigger.saw(buf[:n], time.Now()); fired {
				k.exit()
				return
			}
			// Copied because the next read overwrites buf and the forwarder may
			// not have written this one yet.
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			select {
			case k.queue <- chunk:
			default:
				// Dropped. See forwardQueue: a full queue means Bubble Tea has
				// stopped reading, and a keystroke it will never read is worth
				// less than the next one being noticed.
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

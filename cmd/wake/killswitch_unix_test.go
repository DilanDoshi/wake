//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
)

// The property the whole file exists for: the pump decides *before* it hands
// anything downstream, so a consumer that has stopped reading cannot take the
// key that escapes it.
//
// The queue is filled first and the pipe behind it is never drained, which is
// what a wedged Bubble Tea looks like from here - forwardQueue full, and the
// forwarder parked inside a write nobody is reading. A build whose pump handed
// bytes over before deciding hangs here rather than failing, which is why the
// wait has a deadline of its own.
func TestTheKillSwitchFiresWithNothingReadingWhatItForwards(t *testing.T) {
	keysRead, keysWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("keys pipe: %v", err)
	}
	pipe, feed, err := os.Pipe()
	if err != nil {
		t.Fatalf("input pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = keysWrite.Close()
		_ = pipe.Close()
	})

	fired := make(chan struct{})
	k := newKillSwitch(keysRead, os.Stderr, nil, pipe, feed)
	k.exit = func() { close(fired) }
	go k.pump()
	go k.forward()

	// Filled on a goroutine, because under a build that hands bytes over before
	// deciding this is where it stops: the pump stops reading, the keys pipe
	// fills, and the press below never lands. That is the failure - the deadline
	// on the wait is what reports it, rather than the test hanging in a write.
	go func() {
		// Well past the 64KB the feed pipe holds, so the forwarder is genuinely
		// parked in a write nobody is reading and the queue behind it is full.
		fill := make([]byte, readChunk)
		for range forwardQueue * 4 {
			if _, err := keysWrite.Write(fill); err != nil {
				return
			}
		}
		_, _ = keysWrite.Write([]byte{keyCtrlC, keyCtrlC})
	}()

	select {
	case <-fired:
	case <-time.After(testTimeout):
		t.Fatal("the kill switch never fired: the pump is waiting on the consumer it exists to escape")
	}
}

// wedgeProbeEnv makes this test binary a Bubble Tea program whose Update never
// returns, which is the state the kill switch exists for. Dispatched by TestMain.
const wedgeProbeEnv = "WAKE_WEDGE_PROBE"

type wedgedModel struct{}

func (wedgedModel) Init() tea.Cmd { return nil }

func (w wedgedModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, sized := msg.(tea.WindowSizeMsg); sized {
		select {} // the freeze, on the goroutine that reads every message
	}
	return w, nil
}

func (wedgedModel) View() string { return "wedged" }

// runWedgedProbe is the child process TestMain hands off to.
func runWedgedProbe() int {
	p := tea.NewProgram(wedgedModel{}, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return 1
	}
	return 0
}

// This is the measurement killswitch.go's header cites, kept as a test because
// the header makes an empirical claim about a dependency and a claim nothing
// asserts is wrong by default.
//
// bubbletea's handleSignals does `p.msgs <- InterruptMsg{}` on an **unbuffered**
// channel that only the event loop reads, so a wedged loop swallows SIGINT and
// SIGTERM both and the operator is left with SIGKILL and a broken terminal. If
// a future bubbletea buffers that channel or exits another way, this fails and
// the header is what needs correcting - not this test.
func TestAWedgedProgramSurvivesTheSignalsBubbleTeaHandles(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), wedgeProbeEnv+"=1", "TERM=xterm-256color")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("start the probe in a pty: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = ptmx.Close()
	})
	// Drained *and answered*. Bubble Tea queries the terminal on the way up -
	// OSC 11 for the background colour, CSI 6n for the cursor - and blocks on
	// the reply, which is **before handleSignals is installed**: a probe that
	// only drains never leaves startup, and dies on the default disposition of
	// the first signal sent. That measures nothing about a wedged loop. Found by
	// this test failing green-side, which is the whole reason for a red-check.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				return
			}
			out := string(buf[:n])
			if strings.Contains(out, "\x1b]11;?") {
				_, _ = ptmx.WriteString("\x1b]11;rgb:0000/0000/0000\x1b\\")
			}
			if strings.Contains(out, "\x1b[6n") {
				_, _ = ptmx.WriteString("\x1b[1;1R")
			}
		}
	}()

	// **Waited on rather than asked about.** os.Process.Signal(0) succeeds
	// against a zombie, so a probe that exited but has not been reaped reads as
	// alive - which passes this test whatever bubbletea does. Wait is the only
	// thing that can tell those apart, and finding that out is what a red-check
	// on this test is for.
	ended := make(chan struct{})
	go func() { _ = cmd.Wait(); close(ended) }()

	stillHere := func(when string) {
		t.Helper()
		select {
		case <-ended:
			t.Fatalf("the wedged program died %s - bubbletea's handler no longer blocks on an "+
				"unbuffered channel, so killswitch.go's header is out of date", when)
		default:
		}
	}

	time.Sleep(750 * time.Millisecond) // wedged on the first WindowSizeMsg
	stillHere("before any signal was sent")

	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		if err := cmd.Process.Signal(sig); err != nil {
			t.Fatalf("sending %v: %v", sig, err)
		}
		time.Sleep(750 * time.Millisecond)
		stillHere("on " + sig.String())
	}
}

// The whole thing end to end, on the binary that ships: two ⌃C in a running
// room close it, and the exit code says which ending it was.
//
// It also proves the pipe is genuinely in front of Bubble Tea. Every other
// screen test would still pass if the kill switch read some other descriptor
// than the one the keys arrive on; this is the only test that can see that.
func TestTwoCtrlCClosesTheWindow(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 100, 30)
	s.await("ready")

	s.send("\x03\x03")

	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != exitEmergency {
			t.Fatalf("⌃C⌃C ended the window as %v, want exit %d", err, exitEmergency)
		}
	case <-time.After(testTimeout):
		t.Fatalf("⌃C⌃C did not close the window.\n%s", s.dump())
	}
}

// The terminal really does come back, against a real pty rather than against
// the sequences this happens to write.
//
// Both halves are asserted because they fail independently and only one of them
// is recoverable by hand: the **termios** is what makes the shell echo again,
// and the **escape sequences** are what leave the alt screen. A restore that
// wrote the sequences and skipped tcsetattr leaves somebody typing blind.
func TestTheEmergencyRestorePutsTheTerminalBack(t *testing.T) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("open a pty: %v", err)
	}
	t.Cleanup(func() { _ = ptmx.Close(); _ = tty.Close() })

	cooked, err := term.GetState(tty.Fd())
	if err != nil {
		t.Fatalf("read the pty state: %v", err)
	}
	if _, err := term.MakeRaw(tty.Fd()); err != nil {
		t.Fatalf("raw mode: %v", err)
	}

	seen := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		n, err := ptmx.Read(buf)
		if err != nil {
			seen <- ""
			return
		}
		seen <- string(buf[:n])
	}()

	k := newKillSwitch(tty, tty, cooked, nil, nil)
	k.restoreTerminal()

	select {
	case out := <-seen:
		for _, want := range []string{"\x1b[?1049l", "\x1b[?1002l", "\x1b[?25h"} {
			if !strings.Contains(out, want) {
				t.Errorf("the restore did not write %q; it wrote %q", want, out)
			}
		}
	case <-time.After(testTimeout):
		t.Fatal("the restore wrote nothing to the terminal")
	}

	// Raw mode clears ECHO, so its being back is the whole assertion: this is
	// the difference between a usable shell and typing blind.
	back, err := term.GetState(tty.Fd())
	if err != nil {
		t.Fatalf("re-read the pty state: %v", err)
	}
	if fmt.Sprintf("%+v", *back) != fmt.Sprintf("%+v", *cooked) {
		t.Errorf("the tty was left in raw mode: %+v, want %+v", *back, *cooked)
	}
}

// A signal ends a window Bubble Tea did not end itself, after the grace it is
// given to try. The grace is compressed so this costs a test milliseconds.
func TestASignalEndsAWindowBubbleTeaDidNotEnd(t *testing.T) {
	was := killSignalGrace
	killSignalGrace = 10 * time.Millisecond
	t.Cleanup(func() { killSignalGrace = was })

	fired := make(chan struct{})
	k := newKillSwitch(os.Stdin, os.Stderr, nil, nil, nil)
	k.exit = func() { close(fired) }
	k.watchSignals()

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("signalling this process: %v", err)
	}
	select {
	case <-fired:
	case <-time.After(testTimeout):
		t.Fatal("the signal never ended the window")
	}
}

// A second signal that lands after the grace already ended the window must not
// end it a second time. grace.Stop() returns false once the timer has fired, so
// the unconditional k.exit() ran again - harmless as os.Exit in production, a
// close-of-closed-channel panic under a test's k.exit, and the way a leaked
// watchSignals goroutine turned a later test's signal into a crash under -count.
func TestASecondSignalAfterTheGraceDoesNotEndTheWindowTwice(t *testing.T) {
	was := killSignalGrace
	killSignalGrace = 10 * time.Millisecond
	t.Cleanup(func() { killSignalGrace = was })

	var calls atomic.Int32
	k := newKillSwitch(os.Stdin, os.Stderr, nil, nil, nil)
	k.exit = func() { calls.Add(1) }
	k.watchSignals()

	// First signal: after the grace, the timer ends the window exactly once.
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("first signal: %v", err)
	}
	deadline := time.Now().Add(testTimeout)
	for calls.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("the grace never ended the window after the first signal")
		}
		time.Sleep(time.Millisecond)
	}

	// A second signal now that the grace has fired wakes the goroutine at its
	// second <-sig; it must not end the window again. The bug does so within
	// microseconds of delivery, so a bounded observation window catches it.
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("second signal: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("the window was ended %d times, want exactly once - a second signal after the grace fired ran k.exit again", got)
	}
}

// The other outcome of the exactly-once guard: a second signal that beats the
// grace ends the window at once - the operator saying the first did not work -
// rather than waiting the grace out. grace.Stop() returns true here (the timer
// had not fired), so this is the branch that does call k.exit. The grace is set
// long so that an exit within a couple of seconds can only be the skip, never
// the timer.
func TestASecondSignalBeforeTheGraceEndsTheWindowAtOnce(t *testing.T) {
	was := killSignalGrace
	killSignalGrace = 10 * time.Second
	t.Cleanup(func() { killSignalGrace = was })

	var calls atomic.Int32
	fired := make(chan struct{})
	k := newKillSwitch(os.Stdin, os.Stderr, nil, nil, nil)
	k.exit = func() {
		if calls.Add(1) == 1 {
			close(fired)
		}
	}
	k.watchSignals()

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("first signal: %v", err)
	}
	// Let the goroutine consume the first signal and arm the grace before the
	// second, so the buffered channel does not coalesce them.
	time.Sleep(100 * time.Millisecond)
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("second signal: %v", err)
	}
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("a second signal within the grace did not end the window at once - the grace-skip branch is broken")
	}
	// And the stopped grace timer must not fire on top of it.
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("the window was ended %d times, want once", got)
	}
}

// The whole thing is off when there is nothing to be in front of, and every
// method is safe on the nil that says so - which is what lets converseModel
// call them without a test for it.
func TestThereIsNoKillSwitchWithoutATerminal(t *testing.T) {
	// `go test` hands this process a /dev/null stdin, which is not a terminal.
	if term.IsTerminal(os.Stdin.Fd()) {
		t.Skip("this test binary was given a terminal on stdin")
	}
	k, err := armKillSwitch()
	if err != nil {
		t.Fatalf("arming without a terminal: %v", err)
	}
	if k != nil {
		t.Fatalf("armKillSwitch took a non-terminal stdin: %+v", k)
	}
	if k.Input() != os.Stdin {
		t.Error("a disarmed switch did not hand Bubble Tea the terminal it always had")
	}
	k.restore()
	k.watchSignals()
}

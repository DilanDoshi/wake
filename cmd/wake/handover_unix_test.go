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

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
)

// handOverRig is a kill switch reading a real pty in raw mode, the way
// armKillSwitch leaves one, with what it forwards to Bubble Tea collected.
type handOverRig struct {
	ptmx, tty *os.File
	cooked    *term.State
	k         *killSwitch
	forwarded chan string
	exits     *atomic.Int32
}

func newHandOverRig(t *testing.T) handOverRig {
	t.Helper()
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("open a pty: %v", err)
	}
	cooked, err := term.MakeRaw(tty.Fd())
	if err != nil {
		t.Fatalf("raw mode: %v", err)
	}
	pipe, feed, err := os.Pipe()
	if err != nil {
		t.Fatalf("input pipe: %v", err)
	}
	r := handOverRig{ptmx: ptmx, tty: tty, cooked: cooked, forwarded: make(chan string, 64), exits: &atomic.Int32{}}
	r.k = newKillSwitch(tty, tty, cooked, pipe, feed)
	r.k.exit = func() { r.exits.Add(1) }
	r.k.startReading() // what armKillSwitch does before the pump starts
	go r.k.pump()
	go r.k.forward()
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := pipe.Read(buf)
			if n > 0 {
				r.forwarded <- string(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		stopPump(r.k)
		_ = ptmx.Close()
		_ = tty.Close()
		_ = pipe.Close()
	})
	return r
}

// stopPump ends a rig's pump before its terminal is closed under it: cancel the
// read, wait for the pump to park, and close resumed, which it takes as shut down.
func stopPump(k *killSwitch) {
	k.mu.Lock()
	rd := k.reader
	k.mu.Unlock()
	if rd.Cancel() {
		select {
		case <-k.held:
			close(k.resumed)
		case <-k.done:
		}
	}
	<-k.done
}

func (r handOverRig) type_(t *testing.T, s string) {
	t.Helper()
	if _, err := r.ptmx.Write([]byte(s)); err != nil {
		t.Fatalf("typing %q: %v", s, err)
	}
}

func (r handOverRig) awaitForwarded(t *testing.T, want string) {
	t.Helper()
	deadline := time.After(testTimeout)
	got := ""
	for !strings.Contains(got, want) {
		select {
		case s := <-r.forwarded:
			got += s
		case <-deadline:
			t.Fatalf("Bubble Tea never received %q (got %q)", want, got)
		}
	}
}

func sameState(a, b *term.State) bool { return fmt.Sprintf("%+v", *a) == fmt.Sprintf("%+v", *b) }

// While a child holds the terminal the kill switch reads none of it: what is
// typed reaches the child, not Bubble Tea, the terminal is cooked again, and
// resuming puts raw mode and the forwarding back.
func TestASuspendedKillSwitchLeavesTheTerminalToTheChild(t *testing.T) {
	r := newHandOverRig(t)
	r.type_(t, "a")
	r.awaitForwarded(t, "a")

	if err := r.k.suspend(); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	now, err := term.GetState(r.tty.Fd())
	if err != nil {
		t.Fatalf("read the state: %v", err)
	}
	if !sameState(now, r.cooked) {
		t.Error("the terminal was left raw for the child")
	}

	r.type_(t, "child\n")
	buf := make([]byte, 64)
	n, err := r.tty.Read(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "child") {
		t.Fatalf("the child read %q (%v), want what was typed", buf[:n], err)
	}
	select {
	case s := <-r.forwarded:
		t.Fatalf("Bubble Tea received %q while a child held the terminal", s)
	case <-time.After(100 * time.Millisecond):
	}

	if err := r.k.resume(); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if now, _ := term.GetState(r.tty.Fd()); sameState(now, r.cooked) {
		t.Error("the terminal was not put back into raw mode")
	}
	r.type_(t, "b")
	r.awaitForwarded(t, "b")
}

// ⌃C⌃C typed at the child's prompt is the child's: it must not close Wake,
// which is what the same two bytes mean once Wake has the terminal back.
func TestTwoCtrlCsForAChildDoNotCloseTheWindow(t *testing.T) {
	r := newHandOverRig(t)
	if err := r.k.suspend(); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	r.type_(t, "\x03\x03")
	time.Sleep(100 * time.Millisecond)
	if err := r.k.resume(); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if n := r.exits.Load(); n != 0 {
		t.Fatalf("the emergency exit fired %d times for keys meant for the child", n)
	}
	r.type_(t, "\x03\x03")
	deadline := time.Now().Add(testTimeout)
	for r.exits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if r.exits.Load() != 1 {
		t.Fatal("the kill switch did not re-arm after the child finished")
	}
}

// A ⌃C at the child's prompt is also a SIGINT to the whole foreground group,
// Wake included; the signal watcher must leave it to the child.
func TestASignalWhileAChildHoldsTheTerminalDoesNotEndTheWindow(t *testing.T) {
	was := killSignalGrace
	killSignalGrace = 10 * time.Millisecond
	t.Cleanup(func() { killSignalGrace = was })

	r := newHandOverRig(t)
	r.k.watchSignals()
	if err := r.k.suspend(); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("signal: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if n := r.exits.Load(); n != 0 {
		t.Fatalf("a signal while a child held the terminal ended the window (%d)", n)
	}
	if err := r.k.resume(); err != nil {
		t.Fatalf("resume: %v", err)
	}

	// Signals are Wake's again once it has the terminal back - and two of them
	// end the watcher, so it cannot outlive this test and fire in the next.
	for range 2 {
		if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
			t.Fatalf("signal: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	deadline := time.Now().Add(testTimeout)
	for r.exits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := r.exits.Load(); n != 1 {
		t.Fatalf("after the child, signals ended the window %d times, want 1", n)
	}
}

// The hand-over runs a child on the kill switch's own terminal - reading what is
// typed, writing where Wake writes - and takes the switch back afterwards.
func TestTheHandOverRunsAChildOnTheRealTerminal(t *testing.T) {
	r := newHandOverRig(t)
	cmd := exec.Command("sh", "-c", `read x; echo "got:$x"`)
	h := r.k.handOver(cmd, "Signing in to linear for Wake")

	done := make(chan error, 1)
	go func() { done <- h.Run() }()

	out := make(chan string, 1)
	go func() {
		seen := ""
		buf := make([]byte, 256)
		for !strings.Contains(seen, "got:hello") {
			n, err := r.ptmx.Read(buf)
			seen += string(buf[:n])
			if err != nil {
				break
			}
			if strings.Contains(seen, "Signing in") && !strings.Contains(seen, "hello") {
				_, _ = r.ptmx.Write([]byte("hello\n"))
			}
		}
		out <- seen
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the child failed: %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("the child never finished")
	}
	if seen := <-out; !strings.Contains(seen, "Signing in to linear for Wake") || !strings.Contains(seen, "got:hello") {
		t.Errorf("the terminal showed %q", seen)
	}
	r.type_(t, "z")
	r.awaitForwarded(t, "z")
}

// Without a terminal there is nothing to hand over, and saying so beats a
// child that reads a pipe and refuses.
func TestThereIsNoHandOverWithoutAKillSwitch(t *testing.T) {
	var k *killSwitch
	if err := k.handOver(exec.Command("true"), "x").Run(); err == nil {
		t.Fatal("a hand-over with no terminal claimed to run")
	}
}

// A pause that parks the pump and then cannot restore the terminal must give
// the pump back: failing halfway would leave Wake with no keyboard and no way
// out, the state the kill switch exists to rescue.
func TestASuspendThatCannotRestoreTheTerminalGivesItBack(t *testing.T) {
	was := restoreTTY
	restoreTTY = func(uintptr, *term.State) error { return errors.New("tcsetattr refused") }
	t.Cleanup(func() { restoreTTY = was })

	r := newHandOverRig(t)
	if err := r.k.suspend(); err == nil {
		t.Fatal("a suspend that could not restore the terminal reported success")
	}
	if r.k.quiet.Load() {
		t.Error("the signal watcher was left muted")
	}
	r.type_(t, "x")
	r.awaitForwarded(t, "x")
}

// A pump that has already exited cannot let go of anything; asking it must
// fail at once rather than wait on an answer that cannot come.
func TestASuspendAfterThePumpExitedFailsAtOnce(t *testing.T) {
	keysRead, keysWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("keys pipe: %v", err)
	}
	pipe, feed, err := os.Pipe()
	if err != nil {
		t.Fatalf("input pipe: %v", err)
	}
	t.Cleanup(func() { _ = pipe.Close() })
	k := newKillSwitch(keysRead, os.Stderr, nil, pipe, feed)
	k.startReading()
	go k.pump()
	go k.forward()
	_ = keysWrite.Close()
	<-k.done

	errc := make(chan error, 1)
	go func() { errc <- k.suspend() }()
	select {
	case err := <-errc:
		if err == nil {
			t.Error("suspending a pump that had exited reported success")
		}
	case <-time.After(testTimeout):
		t.Fatal("suspend waited on a pump that had already exited")
	}
	if k.quiet.Load() {
		t.Error("the signal watcher was left muted")
	}
}

// A signal's grace armed just before a hand-over must not end the window under
// the child's prompt; it ends it once the terminal is Wake's again.
func TestAGraceArmedBeforeAHandOverWaitsForIt(t *testing.T) {
	was := killSignalGrace
	killSignalGrace = 100 * time.Millisecond
	t.Cleanup(func() { killSignalGrace = was })

	r := newHandOverRig(t)
	r.k.watchSignals()
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("signal: %v", err)
	}
	time.Sleep(20 * time.Millisecond) // the grace is armed
	if err := r.k.suspend(); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	time.Sleep(250 * time.Millisecond) // past the grace, with the child still holding the terminal
	if n := r.exits.Load(); n != 0 {
		t.Fatalf("the grace ended the window under the child (%d)", n)
	}
	if err := r.k.resume(); err != nil {
		t.Fatalf("resume: %v", err)
	}
	deadline := time.Now().Add(testTimeout)
	for r.exits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if r.exits.Load() != 1 {
		t.Fatal("the deferred exit never fired once the terminal came back")
	}
	// The second signal ends the watcher so it cannot outlive this test.
	_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
	time.Sleep(50 * time.Millisecond)
}

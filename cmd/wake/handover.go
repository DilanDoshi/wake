package main

// Handing the real terminal to a child for as long as it runs - today one
// command, `claude mcp login <server>`, which refuses anything that is not a
// terminal on its stdin (probed 2026-09-23: "stdin isn't a terminal, so
// authentication can't be completed here").
//
// This is not a PTY and not an emulator: Wake stops drawing and stops reading,
// and the child talks to the operator's own terminal directly, the way `git
// commit` hands it to an editor. Bubble Tea's ExecProcess does its half (leaves
// the alt screen, stops its reader, blocks the event loop); this is the other
// half, because the kill switch - not Bubble Tea - owns raw mode and the tty
// read. Left alone it would take the child's keystrokes, forward them into Wake
// after the child exits, fire the emergency exit on a ⌃C⌃C typed at the child's
// prompt, and let the SIGINT that ⌃C raises in the whole foreground group close
// the window.

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/muesli/cancelreader"
)

// suspendWait bounds how long a hand-over waits for the pump to let go of the
// terminal. A cancelled read returns at once; this is for a pump that has
// already exited, which will never answer.
const suspendWait = time.Second

var errNoTerminal = errors.New("there is no terminal to hand over")

// startReading is the pump's first reader of tty, published for suspend.
// armKillSwitch calls it before the pump starts, so a hand-over can never race
// the pump for it; a second call returns the same reader.
func (k *killSwitch) startReading() cancelreader.CancelReader {
	k.mu.Lock()
	if r := k.reader; r != nil {
		k.mu.Unlock()
		return r
	}
	k.mu.Unlock()
	r, err := cancelreader.NewReader(k.tty)
	if err != nil {
		// Not cancellable, so never suspendable: a hand-over says so rather than
		// racing this read for the child's keystrokes.
		r, _ = cancelreader.NewReader(io.Reader(struct{ io.Reader }{k.tty}))
	}
	k.mu.Lock()
	k.reader = r
	k.mu.Unlock()
	return r
}

// suspend stops the pump reading, mutes the signal watcher and puts the
// terminal back the way the operator's shell had it.
func (k *killSwitch) suspend() error {
	if k == nil {
		return errNoTerminal
	}
	k.mu.Lock()
	r := k.reader
	k.mu.Unlock()
	if r == nil {
		return errNoTerminal
	}
	k.quiet.Store(true)
	if !r.Cancel() {
		k.quiet.Store(false)
		return errors.New("this terminal's read cannot be paused, so it cannot be handed over")
	}
	select {
	case <-k.held:
	case <-time.After(suspendWait):
		k.quiet.Store(false)
		return errors.New("Wake's terminal reader did not let go")
	}
	if k.state != nil {
		if err := term.Restore(k.tty.Fd(), k.state); err != nil {
			return fmt.Errorf("restoring the terminal for the child: %w", err)
		}
	}
	return nil
}

// resume is suspend undone, in reverse: raw mode, a fresh reader, then the
// signals are Wake's again.
func (k *killSwitch) resume() error {
	var rawErr error
	if k.state != nil {
		_, rawErr = term.MakeRaw(k.tty.Fd())
	}
	r, err := cancelreader.NewReader(k.tty)
	if err != nil {
		r, _ = cancelreader.NewReader(io.Reader(struct{ io.Reader }{k.tty}))
	}
	k.mu.Lock()
	k.reader = r
	k.mu.Unlock()
	k.resumed <- r
	k.quiet.Store(false)
	if rawErr != nil {
		return fmt.Errorf("putting the terminal back in raw mode: %w", rawErr)
	}
	return nil
}

// handOver wraps cmd as the command Bubble Tea's Exec runs: the child gets the
// kill switch's own terminal for its stdin and output, never Bubble Tea's
// pipe, and header is printed first so the screen says whose prompt it is.
func (k *killSwitch) handOver(cmd *exec.Cmd, header string) tea.ExecCommand {
	return handOver{k: k, cmd: cmd, header: header}
}

type handOver struct {
	k      *killSwitch
	cmd    *exec.Cmd
	header string
}

// The setters are Bubble Tea offering its own streams, which are exactly the
// ones the child must not have: its input is the kill switch's pipe.
func (handOver) SetStdin(io.Reader)  {}
func (handOver) SetStdout(io.Writer) {}
func (handOver) SetStderr(io.Writer) {}

func (h handOver) Run() error {
	if err := h.k.suspend(); err != nil {
		return err
	}
	h.cmd.Stdin, h.cmd.Stdout, h.cmd.Stderr = h.k.tty, h.k.out, h.k.out
	_, _ = fmt.Fprintf(h.k.out, "\r\n%s\r\n\r\n", h.header)
	runErr := h.cmd.Run()
	if err := h.k.resume(); err != nil && runErr == nil {
		return err
	}
	return runErr
}

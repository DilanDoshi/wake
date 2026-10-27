package main

// The one writer Bubble Tea draws through, so Wake can put an escape sequence
// on the terminal between frames.

import (
	"errors"
	"os"
	"sync"
)

// errHandedBack is a write of Wake's own after the program let the terminal go.
var errHandedBack = errors.New("the terminal was handed back")

// guardedOutput serialises Wake's own writes against the renderer's.
//
// It **embeds *os.File** rather than wrapping an io.Writer, and that is
// load-bearing rather than tidy: Bubble Tea and termenv decide whether they are
// talking to a terminal by asserting the output to a file and asking about its
// descriptor. A plain io.Writer wrapper fails that assertion silently - no
// error anywhere - and the whole app degrades to unstyled text. Every colour in
// theme.go goes with it, and no test that does not look at a rendered screen
// can see it happen.
type guardedOutput struct {
	*os.File
	mu     sync.Mutex
	sealed bool
}

// Write is the renderer's frames and Wake's own sequences, one at a time, so a
// clipboard write cannot land in the middle of a frame.
func (g *guardedOutput) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sealed {
		return 0, errHandedBack
	}
	return g.File.Write(p)
}

// WriteString is Write for io.WriteString, which Bubble Tea's escapes and Wake's
// own sequences arrive through: without it the embedded *os.File's would take
// them past the lock.
func (g *guardedOutput) WriteString(s string) (int, error) { return g.Write([]byte(s)) }

// seal ends Wake's writes once Bubble Tea has handed the terminal back: one in
// flight finishes first, and one a command goroutine has yet to make - the
// friends' pictures, a copy - is refused rather than landing on the shell's
// screen, or being cut off mid-escape by the exit.
func (g *guardedOutput) seal() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sealed = true
}

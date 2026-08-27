package main

// The one writer Bubble Tea draws through, so Wake can put an escape sequence
// on the terminal between frames.

import (
	"os"
	"sync"
)

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
	mu sync.Mutex
}

// Write is the renderer's frames and Wake's own sequences, one at a time, so a
// clipboard write cannot land in the middle of a frame.
func (g *guardedOutput) Write(p []byte) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.File.Write(p)
}

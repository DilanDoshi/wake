package main

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

// pipeOutput is a guardedOutput over a pipe, and what was written to it.
func pipeOutput(t *testing.T) (*guardedOutput, func() string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	g := &guardedOutput{File: w}
	return g, func() string {
		_ = w.Close()
		b, _ := io.ReadAll(r)
		return string(b)
	}
}

// done reports whether f finishes within a moment.
func done(f func()) bool {
	ch := make(chan struct{})
	go func() { f(); close(ch) }()
	select {
	case <-ch:
		return true
	case <-time.After(100 * time.Millisecond):
		return false
	}
}

// Bubble Tea writes a frame with Write and an escape with io.WriteString, and
// Wake's own sequences (a clipboard copy, the friends' pictures) go through
// io.WriteString too: both must wait for the lock, or a sequence lands inside a
// frame. The embedded *os.File's own WriteString would skip it.
func TestEveryWriteToTheTerminalTakesTheLock(t *testing.T) {
	g, _ := pipeOutput(t)
	g.mu.Lock()
	if done(func() { _, _ = io.WriteString(g, "\x1b[?1049h") }) {
		t.Error("io.WriteString wrote while a frame held the terminal")
	}
	if done(func() { _, _ = g.Write([]byte("frame")) }) {
		t.Error("Write wrote while a frame held the terminal")
	}
	g.mu.Unlock()
}

// Once Bubble Tea has handed the terminal back, a sequence a command goroutine
// had yet to write is dropped - on the shell's screen it would be noise, and an
// exit mid-write would leave the terminal inside an unfinished escape - while
// one already being written finishes first.
func TestASealedTerminalTakesNoMoreOfWakesWrites(t *testing.T) {
	g, written := pipeOutput(t)
	g.mu.Lock()
	sealed := make(chan struct{})
	go func() { g.seal(); close(sealed) }()
	select {
	case <-sealed:
		t.Fatal("seal returned while a write was in flight")
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := g.File.WriteString("in flight"); err != nil {
		t.Fatal(err)
	}
	g.mu.Unlock()
	<-sealed
	if _, err := io.WriteString(g, "late"); !errors.Is(err, errHandedBack) {
		t.Errorf("a write after the seal returned %v, want %v", err, errHandedBack)
	}
	if _, err := g.Write([]byte("later")); !errors.Is(err, errHandedBack) {
		t.Errorf("a Write after the seal returned %v", err)
	}
	if got := written(); got != "in flight" {
		t.Errorf("the terminal got %q, want only the write that was in flight", got)
	}
}

//go:build unix

// The terminal's tab title on a real screen: a window on a named fleet titles
// the terminal with the fleet's name, a /wake-name retitles it, and leaving puts
// the title the terminal had before back - a stack push before the program and
// a pop after it, which is what every exit through Bubble Tea writes.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// title is what vt10x has read off an OSC 0, 1 or 2, under the lock the
// parser writes it under.
func (s *screen) title() string {
	s.term.Lock()
	defer s.term.Unlock()
	return s.term.Title()
}

// awaitTitle waits for the title to become want.
func (s *screen) awaitTitle(want string) {
	s.t.Helper()
	deadline := time.Now().Add(testTimeout)
	for s.title() != want {
		if time.Now().After(deadline) {
			s.t.Fatalf("the title never became %q; it is %q.\n%s", want, s.title(), s.dump())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The tail keeps the newest bytes and stays bounded: three times its keep,
// written as a pty delivers it, ends on the newest byte with the buffer between
// one and two keeps long. That it trims once per keep rather than copying
// itself per read is its construction, which no assertion here can see.
func TestTheRawTailKeepsTheNewestBytesBounded(t *testing.T) {
	var tail rawTail
	chunk := make([]byte, 4096)
	var n int
	for written := 0; written < 3*rawTailKeep; written += len(chunk) {
		for i := range chunk {
			chunk[i] = byte(n)
			n++
		}
		if _, err := tail.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	got := tail.String()
	if len(got) < rawTailKeep || len(got) > 2*rawTailKeep {
		t.Errorf("the tail holds %d bytes, want between %d and %d", len(got), rawTailKeep, 2*rawTailKeep)
	}
	if got[len(got)-1] != byte(n-1) {
		t.Errorf("the tail ends on %d, want the newest byte %d", got[len(got)-1], byte(n-1))
	}
}

func TestTheTabTitleIsTheFleetsNameAndComesBackOnExit(t *testing.T) {
	withScriptedAgent(t, "")
	home, err := os.MkdirTemp("", "wakeh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	// A fleet called atlas, read off its socket's place under the scratch HOME.
	t.Setenv("WAKE_SOCKET", filepath.Join(home, ".wake", "fleets", "atlas", "daemon.sock"))

	s := startWake(t, 200, 40)
	s.await("group chat")
	s.awaitTitle("atlas")
	s.send("/wake-name dev\r")
	s.await("fleet atlas is now dev")
	s.awaitTitle("dev")

	s.send("\x0f\r") // ⌃O arms the detach and ↵ confirms it
	s.await("Detached")
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the detach ended the window with %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatalf("the window did not close after the detach.\n%s", s.dump())
	}

	raw := s.raw.String()
	const push, pop, leaveAlt = "\x1b[22;0t", "\x1b[23;0t", "\x1b[?1049l"
	first := strings.Index(raw, "\x1b]2;")
	if i := strings.Index(raw, push); i < 0 || first < 0 || i > first {
		t.Errorf("the title stack push (%q at %d) does not precede the first title (at %d)", push, i, first)
	}
	if i, left := strings.LastIndex(raw, pop), strings.LastIndex(raw, leaveAlt); i < 0 || i < left {
		t.Errorf("the title stack pop (%q at %d) does not follow the alt screen's end (at %d)", pop, i, left)
	}
	if i, last := strings.LastIndex(raw, pop), strings.LastIndex(raw, "\x1b]2;"); last > i {
		t.Errorf("a title (at %d) was written after the pop (at %d)", last, i)
	}
	if n := strings.Count(raw, push); n != 1 {
		t.Errorf("the title stack was pushed %d times, want once", n)
	}
}

//go:build unix

// Shift-clicks on a real screen: Wake asks a terminal that holds Shift back for
// its own selection to deliver it (XTSHIFTESCAPE) in the write that pushes the
// title stack, before the alt screen, and gives the terminal's behaviour back
// after the alt screen is left.

package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestShiftClicksAreAskedForWithTheTitleStackAndGivenBackAfterTheAltScreen(t *testing.T) {
	withScriptedAgent(t, "")
	home, err := os.MkdirTemp("", "wakeh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	t.Setenv("WAKE_SOCKET", "") // this session's own is a real fleet; the scratch HOME draws one

	s := startWake(t, 200, 40)
	s.await("group chat")
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
	const enterAlt, leaveAlt = "\x1b[?1049h", "\x1b[?1049l"
	on, off := strings.Index(raw, shiftEscapeOn), strings.LastIndex(raw, shiftEscapeOff)
	if n := strings.Count(raw, shiftEscapeOn); n != 1 {
		t.Errorf("Shift was asked for %d times, want once", n)
	}
	if enter := strings.Index(raw, enterAlt); on < 0 || on > enter {
		t.Errorf("Shift is asked for at %d, want it before the alt screen is entered at %d (it rides the title stack's push)", on, enter)
	}
	if left := strings.LastIndex(raw, leaveAlt); off < 0 || off < left {
		t.Errorf("Shift is given back at %d, want it after the alt screen is left at %d", off, left)
	}
	if n := strings.Count(raw, shiftEscapeOff); n != 1 {
		t.Errorf("Shift was given back %d times, want once", n)
	}
}

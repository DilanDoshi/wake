//go:build unix

// The composer box after a window resize, drawn by the real binary.
//
// A short pane caps the box below the draft's line count, so typing scrolls it
// to the tail. Growing the window then leaves the box tall enough for the whole
// draft - and bubbles' text area, which only re-scrolls to follow the cursor,
// kept the deeper scroll and drew the last lines above blank prompt rows: the
// "extra space in the query bar" a resize left behind. See Composer.reposition.

package main

import (
	"strings"
	"testing"
)

// trailingBlankBoxRows counts the blank prompt rows at the bottom of the first
// composer box on screen - the rows a stale scroll leaves below the draft.
func trailingBlankBoxRows(s *screen) int {
	lines := s.lines()
	top, bottom := -1, -1
	for i, l := range lines {
		if top < 0 && strings.Contains(l, "╭") {
			top = i
			continue
		}
		if top >= 0 && strings.Contains(l, "╰") {
			bottom = i
			break
		}
	}
	if top < 0 || bottom < 0 {
		return -1
	}
	n := 0
	for i := bottom - 1; i > top; i-- {
		if strings.TrimSpace(strings.Trim(lines[i], "│>")) == "" {
			n++
		} else {
			break
		}
	}
	return n
}

// A draft that scrolled to its tail in a short window shows in full, with no
// blank rows below it, once the window grows enough to hold it.
func TestTheComposerHasNoBlankRowsAfterAResizeGrowsIt(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	// A short window (and below dmTakeoverColumns, so a single pane): the box caps
	// well under the draft's line count, so typing scrolls it to the tail.
	s := startWakeInAConversation(t, 90, 11)
	s.await("ready")
	closeRoster(s)
	s.settle()

	// Eight lines, ⌃J between them - more than the short box can show at once.
	s.send("one\x0atwo\x0athree\x0afour\x0afive\x0asix\x0aseven\x0aeight")
	s.await("eight")
	s.settle()

	// The window grows tall enough to hold the whole draft.
	s.resize(90, 40)
	s.settle()

	if !strings.Contains(s.text(), "one") {
		t.Fatalf("the first draft line is off screen after the window grew - the box kept a stale scroll:\n%s", s.dump())
	}
	if b := trailingBlankBoxRows(s); b != 0 {
		t.Errorf("the composer box has %d blank rows below the draft after the resize - extra space in the query bar:\n%s", b, s.dump())
	}
}

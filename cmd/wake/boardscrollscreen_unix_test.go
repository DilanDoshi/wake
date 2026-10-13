//go:build unix

package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// minBoardTileRows is the owner's 2026-10-12 floor for a tile (internal/ui's
// minTileHeight), restated here as the number a reader of the screen can count.
const minBoardTileRows = 20

var boardTileTitle = regexp.MustCompile(`╭─ [●◐○▪·] ([a-z][a-z0-9-]*)`)

// titlesOn is the tile names drawn on the board right now, off the tiles' top
// edges.
func titlesOn(s *screen) []string {
	var out []string
	for _, ln := range s.lines() {
		for _, m := range boardTileTitle.FindAllStringSubmatch(ln, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// A fleet bigger than a frame holds draws three tiles across at a readable height
// and scrolls to the rest, on a real screen: the wheel moves the window and
// PgDn reaches every agent instead of closing the board.
func TestTheTiledBoardIsThreeAcrossAndScrollsOnAScreen(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 140, 50)
	s.await("ready")
	s.send("\x17") // ⌃W to the room
	s.await("group chat")
	var names []string
	for i := range 8 {
		name := fmt.Sprintf("tile%d", i)
		names = append(names, name)
		s.send("/new " + name + "\r")
		s.await("@" + name)
		// /new drafts a mention; esc clears it, settled before the next byte or
		// bubbletea reads the two as one alt-modified key (escprobe_test.go).
		s.send("\x1b")
		s.settle()
		s.send("\x17")
		s.await("group chat")
		s.settle()
	}

	s.send("/board\r")
	s.await("BOARD")
	s.send("\t") // ⇥ to tiles
	s.await("╭")
	s.settle()

	seen := map[string]bool{}
	note := func() {
		for _, n := range titlesOn(s) {
			seen[n] = true
		}
	}
	note()

	// Three across at most, three across when there are enough to fill a row, and
	// a tile at least the floor tall.
	widest, top := 0, -1
	for y, ln := range s.lines() {
		n := strings.Count(ln, "╭")
		widest = max(widest, n)
		if n > 3 {
			t.Fatalf("row %d draws %d tiles across, want at most 3.\n%s", y, n, s.dump())
		}
		if n > 0 && top < 0 {
			top = y
		}
	}
	if widest != 3 {
		t.Fatalf("no row draws three tiles across (widest %d).\n%s", widest, s.dump())
	}
	bottom := top
	for y, ln := range s.lines() {
		if y > top && strings.Contains(ln, "╰") {
			bottom = y
			break
		}
	}
	if rows := bottom - top + 1; rows < minBoardTileRows {
		t.Fatalf("a tile spans %d rows, want at least %d.\n%s", rows, minBoardTileRows, s.dump())
	}

	// One wheel notch scrolls the window: the tiles' top edge leaves the frame.
	first := s.lines()[top]
	s.send("\x1b[<65;20;10M") // wheel down, SGR
	s.settle()
	if got := s.lines()[top]; got == first {
		t.Fatalf("a wheel notch drew the same top row.\n%s", s.dump())
	}

	// PgDn scrolls rather than closing the board, and walking it reaches every
	// agent that /new started.
	for range 12 {
		s.send("\x1b[6~") // PgDn
		s.settle()
		if !strings.Contains(s.text(), "BOARD") {
			t.Fatalf("PgDn closed the board.\n%s", s.dump())
		}
		note()
	}
	for _, name := range names {
		if !seen[name] {
			t.Errorf("scrolling the board never drew %s; saw %v", name, seen)
		}
	}
}

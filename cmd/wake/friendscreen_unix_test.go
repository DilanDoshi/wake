//go:build unix

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/ui"
)

// cornerCols and cornerRows are the cells a friend takes at the sidebar's right
// edge (internal/ui's friendCols and friendRows); the screen's last two rows are
// the strip and the notice row, under the pane.
const cornerCols, cornerRows = 9, 5

// cornerOf is the cells a friend takes in one frame's rows, as text: a row's
// runes are its cells (lines builds them so), right-trimmed.
func (s *screen) cornerOf(rows []string) string {
	var b strings.Builder
	right := s.cols - ui.SideMargin
	for _, row := range rows[s.rows-2-cornerRows : s.rows-2] {
		cells := []rune(row + strings.Repeat(" ", s.cols))
		b.WriteString(string(cells[right-cornerCols : right]))
		b.WriteByte('\n')
	}
	return b.String()
}

// awaitCorner waits for a whole frame's corner to draw a friend (drawn) or
// nothing.
func (s *screen) awaitCorner(drawn bool) string {
	s.t.Helper()
	return s.cornerOf(s.awaitFrame(fmt.Sprintf("the corner drawing a friend = %v", drawn), func(rows []string) bool {
		return strings.ContainsAny(s.cornerOf(rows), "▀▄") == drawn
	}))
}

// Reads between two pieces of one frame wait for the rest (BUG-68). The frame is
// Bubble Tea's alt-screen shape (home, rows top to bottom, the cursor parked at
// the last row's start), cut inside the corner's last row.
func TestTheCornerIsNeverReadHalfPainted(t *testing.T) {
	const cols, rows = 30, 12
	sprite := []string{"  ▄▀▀▀▄  ", " ▀▄▀▄▀▄▀ ", "▄▀▀▀▀▀▀▀▄", " ▀▄▄▄▄▄▀ ", "  ▀▀ ▀▀  "}
	lines := make([]string, rows-2-cornerRows)
	for _, r := range sprite {
		lines = append(lines, strings.Repeat(" ", cols-ui.SideMargin-cornerCols)+r)
	}
	painted, last := altFrame(lines, cols, rows), sprite[cornerRows-1]
	s := halfPainted(t, painted, strings.Index(painted, last)+len(string([]rune(last)[:cornerCols/2])), cols, rows)

	if got, want := s.awaitCorner(true), strings.Join(sprite, "\n")+"\n"; got != want {
		t.Fatalf("the corner was read part-way through its frame:\n%s\nwant the whole frame's:\n%s", got, want)
	}
}

// On a real screen, through a terminal without Kitty graphics (the harness
// strips every detection variable): Skip sits in the corner of a fresh window,
// /friends none clears it, /friends bo draws Bo, and the next window to open
// opens with Bo, because the choice is the machine's.
func TestAFriendSitsInTheCornerAndTheChoiceOutlastsTheWindow(t *testing.T) {
	withScriptedAgent(t, "")
	kept := filepath.Join(t.TempDir(), friendsFile)
	t.Setenv(friendsFileEnv, kept)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 160, 40)
	s.await("ready")
	skip := s.awaitCorner(true)

	s.send("/friends none\r")
	s.awaitCorner(false)
	s.send("/friends bo\r")
	bo := s.awaitCorner(true)
	if bo == skip {
		t.Fatalf("Bo drew the same corner as Skip:\n%s", bo)
	}
	deadline := time.Now().Add(screenTimeout)
	for loadFriend(kept) != "bo" {
		if time.Now().After(deadline) {
			b, err := os.ReadFile(kept)
			t.Fatalf("the choice was never kept: %q %v", b, err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	next := startWakeInAConversation(t, 160, 40)
	next.await("ready")
	if got := next.awaitCorner(true); got != bo {
		t.Errorf("the next window opened with\n%s\nwant Bo's\n%s", got, bo)
	}
}

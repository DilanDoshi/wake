//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cornerCols and cornerRows are the cells a friend takes at the sidebar's right
// edge (internal/ui's friendCols and friendRows); the screen's last two rows are
// the strip and the notice row, under the pane.
const cornerCols, cornerRows = 9, 5

// corner is the screen's bottom-right cells where a friend sits, as text.
func (s *screen) corner() string {
	s.term.Lock()
	defer s.term.Unlock()
	var b strings.Builder
	for y := s.rows - 2 - cornerRows; y < s.rows-2; y++ {
		for x := s.cols - cornerCols; x < s.cols; x++ {
			b.WriteRune(s.term.Cell(x, y).Char)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// awaitCorner waits for the corner to draw a friend (drawn) or nothing.
func (s *screen) awaitCorner(drawn bool) string {
	s.t.Helper()
	deadline := time.Now().Add(screenTimeout)
	for time.Now().Before(deadline) {
		c := s.corner()
		if strings.ContainsAny(c, "▀▄") == drawn {
			return c
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.t.Fatalf("the corner never drew a friend = %v.\n%s", drawn, s.dump())
	return ""
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

//go:build unix

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hinshun/vt10x"

	"github.com/DilanDoshi/wake/internal/ui"
)

// cornerCols and cornerRows are the cells a friend takes at the sidebar's right
// edge (internal/ui's friendCols and friendRows); the screen's last two rows are
// the strip and the notice row, under the pane.
const cornerCols, cornerRows = 9, 5

// corner is the cells a friend takes, as text, and whether they are one frame's:
// Bubble Tea paints top to bottom and leaves the cursor on the last row (BUG-68).
func (s *screen) corner() (text string, whole bool) {
	s.term.Lock()
	defer s.term.Unlock()
	var b strings.Builder
	right := s.cols - ui.SideMargin
	for y := s.rows - 2 - cornerRows; y < s.rows-2; y++ {
		for x := right - cornerCols; x < right; x++ {
			b.WriteRune(s.term.Cell(x, y).Char)
		}
		b.WriteByte('\n')
	}
	return b.String(), s.term.Cursor().Y == s.rows-1
}

// awaitCorner waits for a whole frame's corner to draw a friend (drawn) or
// nothing.
func (s *screen) awaitCorner(drawn bool) string {
	s.t.Helper()
	deadline := time.Now().Add(screenTimeout)
	for time.Now().Before(deadline) {
		if c, whole := s.corner(); whole && strings.ContainsAny(c, "▀▄") == drawn {
			return c
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, whole := s.corner()
	s.t.Fatalf("the corner never drew a friend = %v; the newest read was a whole frame's (cursor on the last row): %v.\n%s", drawn, whole, s.dump())
	return ""
}

// splitPaint hands vt10x a frame in two pieces, the second only after reads of
// the first: a frame the pty or vt10x's parser splits, read between the pieces.
type splitPaint struct {
	vt10x.Terminal
	rest  []byte
	reads int
}

func (p *splitPaint) Unlock() {
	p.Terminal.Unlock()
	if p.reads--; p.reads == 0 {
		_, _ = p.Write(p.rest)
	}
}

// Reads between two pieces of one frame wait for the rest (BUG-68). The frame is
// Bubble Tea's alt-screen shape (home, rows top to bottom, the cursor parked at
// the last row's start), cut inside the corner's last row.
func TestTheCornerIsNeverReadHalfPainted(t *testing.T) {
	const cols, rows = 30, 12
	sprite := []string{"  ▄▀▀▀▄  ", " ▀▄▀▄▀▄▀ ", "▄▀▀▀▀▀▀▀▄", " ▀▄▄▄▄▄▀ ", "  ▀▀ ▀▀  "}
	top := rows - 2 - cornerRows
	var frame strings.Builder
	frame.WriteString("\x1b[H")
	for y := range rows {
		line := strings.Repeat(" ", cols)
		if r := y - top; r >= 0 && r < cornerRows {
			line = strings.Repeat(" ", cols-ui.SideMargin-cornerCols) + sprite[r] + strings.Repeat(" ", ui.SideMargin)
		}
		frame.WriteString(line)
		if y < rows-1 {
			frame.WriteString("\r\n")
		}
	}
	fmt.Fprintf(&frame, "\x1b[%d;H", rows)
	painted, last := frame.String(), sprite[cornerRows-1]
	cut := strings.Index(painted, last) + len(string([]rune(last)[:cornerCols/2]))

	term := vt10x.New(vt10x.WithSize(cols, rows))
	if _, err := term.Write([]byte(painted[:cut])); err != nil {
		t.Fatal(err)
	}
	// Three reads of the half, so neither one read nor two alike pass for whole.
	s := &screen{t: t, term: &splitPaint{Terminal: term, rest: []byte(painted[cut:]), reads: 3}, cols: cols, rows: rows}

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

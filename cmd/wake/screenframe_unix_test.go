//go:build unix

package main

// The harness reads only whole frames (BUG-68): vt10x parses a frame across
// several lock holds, so a read can land between two pieces of one paint. These
// hand it a frame in two pieces and read between them.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hinshun/vt10x"
)

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

// altFrame is lines as Bubble Tea's alt-screen renderer paints them: home, every
// row top to bottom at the screen's width, the cursor parked at the last row.
func altFrame(lines []string, cols, rows int) string {
	var b strings.Builder
	b.WriteString("\x1b[H")
	for y := range rows {
		line := ""
		if y < len(lines) {
			line = lines[y]
		}
		b.WriteString(line + strings.Repeat(" ", cols-len([]rune(line))))
		if y < rows-1 {
			b.WriteString("\r\n")
		}
	}
	fmt.Fprintf(&b, "\x1b[%d;H", rows)
	return b.String()
}

// halfPainted is a screen in the alt screen, as Bubble Tea's is, holding frame up
// to the byte cut; it gets the rest only after three reads, so neither one read
// nor two alike pass for whole.
func halfPainted(t *testing.T, frame string, cut, cols, rows int) *screen {
	t.Helper()
	term := vt10x.New(vt10x.WithSize(cols, rows))
	if _, err := term.Write([]byte("\x1b[?1049h" + frame[:cut])); err != nil {
		t.Fatal(err)
	}
	return &screen{t: t, term: &splitPaint{Terminal: term, rest: []byte(frame[cut:]), reads: 3}, cols: cols, rows: rows}
}

// Two equal reads of half a frame are a stall, not quiet.
func TestSettleNeverSettlesOnHalfAFrame(t *testing.T) {
	const cols, rows = 30, 8
	lines := []string{"top", "", "middle row", "", "", "bottom row", "", ""}
	frame := altFrame(lines, cols, rows)
	s := halfPainted(t, frame, strings.Index(frame, "middle")+len("mid"), cols, rows)

	if got, want := strings.Join(s.settle(), "\n"), strings.Join(lines, "\n"); got != want {
		t.Fatalf("settle settled part-way through a frame:\n%s\nwant the whole frame:\n%s", got, want)
	}
}

// The name on a composer border is read from the frame that drew it, not from a
// later read that may hold only the border's first cells, or one border's first
// cells over the rest of the frame before's.
func TestAConversationsNameIsReadFromAWholeFrame(t *testing.T) {
	const cols, rows = 40, 8
	pane := func(border string) string {
		return altFrame([]string{"", "ready", "", border, "│ >                │", "╰──────────────────╯"}, cols, rows)
	}
	border := "╭──────── @alex ─╮"
	before, frame := pane("╭──────── @bob ─╮"), pane(border)
	s := halfPainted(t, before+frame, len(before)+strings.Index(frame, border)+len("╭────"), cols, rows)

	if got := s.agentName(); got != "alex" {
		t.Fatalf("agentName read %q off a half-painted border, want alex", got)
	}
}

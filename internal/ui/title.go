package ui

// The terminal's tab title is the fleet's name (the owner's 2026-11-01 ask):
// `sing-dev`, not the `wake --fleet sing-dev` a shell's integration shows for
// the command. Written once per change through the terminal writer, never in a
// frame; cmd/wake keeps the terminal's own title on its stack around the program.

import (
	"io"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/notice"
)

// wakeTitle titles a window on no named fleet: a socket set by hand outside
// ~/.wake/fleets names none.
const wakeTitle = "wake"

// The xterm title stack: cmd/wake pushes before Bubble Tea starts and pops once
// the terminal is handed back, so a terminal keeping a stack shows the shell's
// title again on exit.
const (
	TitleStackPush = "\x1b[22;0t"
	TitleStackPop  = "\x1b[23;0t"
)

// tabTitle is the title for a fleet reported as fleet, computed where the name
// is folded (withFleetName), not per message. A title is an escape sequence's
// payload, so a character that would end it or open another - a control, a
// byte that is not UTF-8 - is dropped whatever the daemon sent.
func tabTitle(fleet string) string {
	var b strings.Builder
	for i := 0; i < len(fleet); {
		r, n := utf8.DecodeRuneInString(fleet[i:])
		i += n
		if unicode.IsControl(r) || (r == utf8.RuneError && n == 1) {
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return wakeTitle
	}
	return b.String()
}

// dueTitle writes the title when the fleet's has moved since the last write:
// once per change, never per frame, at the cost of one string compare.
func (a App) dueTitle() (App, tea.Cmd) {
	if a.daemon.title == a.titled || a.out == nil {
		return a, nil
	}
	a.titled = a.daemon.title
	return a, writeTitle(a.out, a.titled)
}

// retitle writes the title again whatever it was: after a hand-over, where the
// child owned the terminal and may have titled it.
func (a App) retitle() tea.Cmd {
	if a.out == nil {
		return nil
	}
	return writeTitle(a.out, a.daemon.title)
}

// titleAsked numbers the titles in the order asked for, on the Update
// goroutine, and titleOrder keeps the writes in it: two commands can run at
// once (a rename's behind a hand-over's), and the newer is the one to keep.
var (
	titleAsked atomic.Uint64
	titleOrder inOrder
)

// writeTitle puts the title on the terminal between frames, from a command for
// writeSequence's reason; an older write that would land after a newer one is
// skipped.
func writeTitle(out io.Writer, title string) tea.Cmd {
	turn := titleAsked.Add(1)
	return func() tea.Msg {
		titleOrder.write(turn, func() {
			if _, err := io.WriteString(out, ansi.SetWindowTitle(title)); err != nil {
				notice.Report("title: %v", err)
			}
		})
		return nil
	}
}

package ui

// Putting a selection on the clipboard, in layers.
//
// The layers are Claude Code 2.1.232's, and so are its reasons: a native tool
// is the only one that works when the terminal is not listening, tmux has its
// own buffer worth filling, and OSC 52 is the only one that crosses an ssh
// connection. Matched against Claude Code rather than assumed - see the
// design doc's §2.

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/notice"
)

// screenChunk is how much base64 goes in one DCS string under GNU screen,
// which truncates a long one rather than refusing it.
const screenChunk = 76

// copyTimeout bounds a clipboard subprocess. A pbcopy that never returns must
// not hold a goroutine for the life of the program.
const copyTimeout = 2 * time.Second

// copiedFormat confirms a copy in the notice slot (count is runes, not bytes);
// copiedNativeFailFormat is the same when the machine's clipboard tool errored
// - OSC 52 still carried the copy, so the tool failure rides along.
const (
	copiedFormat           = "copied %d chars to clipboard"
	copiedNativeFailFormat = "copied %d chars (system clipboard unavailable: %v)"
)

// copiedMsg is what a clipboard write produced: a sequence for the terminal,
// how many characters it carried, and whatever went wrong reaching the
// machine's own clipboard.
type copiedMsg struct {
	seq   string
	chars int
	err   error
}

// multiplexer names what sits between wake and the terminal, "" for nothing.
//
// It takes the lookup rather than reading the environment so a test can say
// what it wants without setting a process-wide variable.
func multiplexer(env func(string) string) string {
	switch {
	case env("TMUX") != "":
		return "tmux"
	case strings.HasPrefix(env("TERM"), "screen"):
		return "screen"
	default:
		return ""
	}
}

// clipboardSequence is the OSC 52 write, wrapped for whatever is in the way.
func clipboardSequence(text, mux string) string {
	switch mux {
	case "tmux":
		// tmux swallows an OSC it does not understand. A DCS passthrough with
		// the inner ESC doubled reaches the terminal underneath it.
		return "\x1bPtmux;\x1b" + ansi.SetSystemClipboard(text) + "\x1b\\"
	case "screen":
		return screenSequence(text)
	default:
		return ansi.SetSystemClipboard(text)
	}
}

// screenSequence is one OSC 52 split across as many DCS strings as it takes.
//
// The split continues a single sequence rather than emitting a complete one per
// chunk: several complete ones would set the clipboard once per chunk, each
// time to a fragment of the base64, and the last fragment would win.
func screenSequence(text string) string {
	b64 := base64.StdEncoding.EncodeToString([]byte(text))
	var b strings.Builder
	b.WriteString("\x1bP\x1b]52;c;")
	for i := 0; i < len(b64); i += screenChunk {
		if i > 0 {
			b.WriteString("\x1b\\\x1bP")
		}
		b.WriteString(b64[i:min(i+screenChunk, len(b64))])
	}
	b.WriteString("\x07\x1b\\")
	return b.String()
}

// copyToClipboard writes text everywhere it can reach, and is nil when there is
// nothing to write.
//
// The machine's own clipboard is a subprocess and happens here. The OSC 52
// sequence has to reach the terminal, which Bubble Tea owns, so it rides back
// on the message for App.update to hand to the writer.
func copyToClipboard(text string) tea.Cmd {
	if text == "" {
		return nil
	}
	return func() tea.Msg {
		return copiedMsg{
			seq:   clipboardSequence(text, multiplexer(os.Getenv)),
			chars: len([]rune(text)),
			err:   nativeCopy(text),
		}
	}
}

// copied confirms a clipboard write, in one notice: notice keeps a single slot,
// so a separate error report would be overwritten unseen by the confirmation in
// the same Update. OSC 52 went out regardless, so the copy happened even when
// the subprocess failed - the confirmation stands and names the failure inline.
func (a App) copied(m copiedMsg) (tea.Model, tea.Cmd) {
	if m.err != nil {
		notice.Report(copiedNativeFailFormat, m.chars, m.err)
	} else {
		notice.Report(copiedFormat, m.chars)
	}
	return a, a.writeSequence(m.seq)
}

// cleared drops a selection, which every keystroke does before doing its own
// job.
//
// Not "esc dismisses a selection": esc is how an operator stops a runaway
// agent, and a highlight left over from a copy two minutes ago swallowing that
// press is an agent that does not stop. The copy has already happened by then,
// so the highlight has nothing left to defend - the same reading as the card
// arm, where every input that is not the confirm takes it back.
func (a App) cleared() App {
	a.sel, a.selecting = selection{}, false
	return a
}

// WithOutput is the terminal Wake may write a clipboard sequence to. It must
// be the same writer Bubble Tea draws through - see cmd/wake/output.go.
func (a App) WithOutput(w io.Writer) App {
	a.out = w
	return a
}

// writeSequence puts an escape sequence on the terminal between frames.
//
// A Cmd rather than a write from Update because Update is the goroutine that
// renders, and this is the one thing in the package that touches the terminal
// without being a frame.
func (a App) writeSequence(seq string) tea.Cmd {
	if a.out == nil || seq == "" {
		return nil
	}
	out := a.out
	return func() tea.Msg {
		if _, err := io.WriteString(out, seq); err != nil {
			notice.Report("clipboard: %v", err)
		}
		return nil
	}
}

// nativeCopy fills the machine's own clipboard, and tmux's buffer beside it
// when there is one. Neither is fatal: OSC 52 is still on its way.
func nativeCopy(text string) error {
	var errs []error
	if os.Getenv("TMUX") != "" {
		errs = append(errs, run(text, "tmux", "load-buffer", "-w", "-"))
	}
	if runtime.GOOS == "darwin" {
		errs = append(errs, run(text, "pbcopy"))
	}
	return errors.Join(errs...)
}

// run feeds text to a command's stdin, bounded so a clipboard tool that hangs
// cannot hold a goroutine for the life of the program.
func run(text, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	c.Stdin = strings.NewReader(text)
	return c.Run()
}

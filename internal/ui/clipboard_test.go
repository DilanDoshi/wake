package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/notice"
)

func TestTheClipboardSequenceIsOSC52(t *testing.T) {
	if got, want := clipboardSequence("hi", ""), "\x1b]52;c;aGk=\x07"; got != want {
		t.Errorf("clipboardSequence = %q, want %q", got, want)
	}
}

func TestUnderTmuxTheSequenceIsWrappedForPassthrough(t *testing.T) {
	got := clipboardSequence("hi", "tmux")
	if !strings.HasPrefix(got, "\x1bPtmux;") {
		t.Errorf("clipboardSequence = %q: tmux swallows an OSC it does not understand unless it is wrapped in a DCS passthrough", got)
	}
	if !strings.Contains(got, "\x1b\x1b]52;c;aGk=") {
		t.Errorf("clipboardSequence = %q: the inner ESC must be doubled inside a tmux passthrough", got)
	}
	if !strings.HasSuffix(got, "\x1b\\") {
		t.Errorf("clipboardSequence = %q: a DCS string ends with ST", got)
	}
}

func TestUnderScreenTheSequenceIsOneContinuedString(t *testing.T) {
	// screen truncates a long DCS string, so the payload is split - but into
	// continuations of one OSC 52, not into several complete ones. Several
	// complete ones would set the clipboard once per chunk, each to a fragment
	// of the base64.
	long := strings.Repeat("a", 400)
	got := clipboardSequence(long, "screen")
	if n := strings.Count(got, "]52;c;"); n != 1 {
		t.Errorf("clipboardSequence has %d OSC 52 introducers, want 1: the chunks continue one string", n)
	}
	if n := strings.Count(got, "\x1bP"); n < 2 {
		t.Errorf("clipboardSequence over 400 bytes produced %d chunks, want at least 2", n)
	}
	if !strings.HasPrefix(got, "\x1bP\x1b]52;c;") {
		t.Errorf("clipboardSequence = %.20q: screen needs the OSC opened inside a DCS", got)
	}
}

func TestAShortPayloadUnderScreenIsNotSplit(t *testing.T) {
	got := clipboardSequence("hi", "screen")
	if want := "\x1bP\x1b]52;c;aGk=\x07\x1b\\"; got != want {
		t.Errorf("clipboardSequence = %q, want %q", got, want)
	}
}

func TestTheMultiplexerIsReadFromTheEnvironment(t *testing.T) {
	for _, c := range []struct {
		tmux, term, want string
	}{
		{"/tmp/tmux-501/default,123,0", "screen-256color", "tmux"},
		{"", "screen.xterm-256color", "screen"},
		{"", "screen", "screen"},
		{"", "xterm-ghostty", ""},
		{"", "", ""},
	} {
		env := func(k string) string {
			switch k {
			case "TMUX":
				return c.tmux
			case "TERM":
				return c.term
			}
			return ""
		}
		if got := multiplexer(env); got != c.want {
			t.Errorf("multiplexer(TMUX=%q TERM=%q) = %q, want %q", c.tmux, c.term, got, c.want)
		}
	}
}

func TestNothingIsCopiedForAnEmptySelection(t *testing.T) {
	if cmd := copyToClipboard(""); cmd != nil {
		t.Error("an empty selection must produce no command: there is nothing to put anywhere")
	}
}

// A successful copy leaves a confirmation where the UI already looks for one.
func TestASuccessfulCopyReportsTheCharacterCount(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)

	app := dmApp(nil, Stream{}, "s1", "alex")
	m, cmd := app.Update(copiedMsg{seq: "\x1b]52;c;aGVsbG8gd29ybGQh\x07", chars: 13})
	if _, ok := m.(App); !ok {
		t.Fatalf("Update(copiedMsg) returned %T, want App", m)
	}
	if cmd != nil {
		cmd()
	}

	n, said := notice.Latest()
	if !said {
		t.Fatal("a successful copy reported no notice")
	}
	if want := "copied 13 chars to clipboard"; n.Text != want {
		t.Errorf("notice = %q, want %q", n.Text, want)
	}
}

// The count is what a screen reads as characters, not the encoded byte count -
// the fix note calls out len([]rune(text)) by name because a multi-byte rune
// must count once.
func TestTheCopiedCountIsRunesNotBytes(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)

	cmd := copyToClipboard("héllo") // 5 runes, 6 bytes
	if cmd == nil {
		t.Fatal("non-empty text produced no clipboard command")
	}
	msg, ok := cmd().(copiedMsg)
	if !ok {
		t.Fatalf("copyToClipboard's command produced %T, want copiedMsg", cmd())
	}
	if msg.chars != 5 {
		t.Errorf("copiedMsg.chars = %d, want 5", msg.chars)
	}
}

// OSC 52 went out regardless of whether the machine's own clipboard subprocess
// could run, so a native failure still confirms the copy - in one notice, which
// also names the failure rather than dropping it into a second slot nothing
// would show (notice keeps only the most recent).
func TestTheNativeFailureNoticeConfirmsTheCopyAndNamesTheFailure(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)

	app := dmApp(nil, Stream{}, "s1", "alex")
	m, cmd := app.Update(copiedMsg{seq: "x", chars: 3, err: errors.New("pbcopy: not found")})
	if _, ok := m.(App); !ok {
		t.Fatalf("Update(copiedMsg) returned %T, want App", m)
	}
	if cmd != nil {
		cmd()
	}

	n, said := notice.Latest()
	if !said {
		t.Fatal("a copy that hit a native-clipboard error reported no notice")
	}
	if !strings.Contains(n.Text, "copied 3 chars") {
		t.Errorf("notice = %q, want it to confirm the copy still happened over OSC 52", n.Text)
	}
	if !strings.Contains(n.Text, "pbcopy: not found") {
		t.Errorf("notice = %q, want it to name the native-clipboard failure, not swallow it", n.Text)
	}
}

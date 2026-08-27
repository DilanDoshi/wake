package ui

import (
	"strings"
	"testing"
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

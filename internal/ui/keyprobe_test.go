package ui

// What bubbletea actually names, checked against the library rather than
// against a terminal.
//
// This is the probe `docs/notes/decisions.md` describes, kept rather than
// thrown away - and promoted from a probe to a guard, which is strictly better:
// a note telling the next person to re-run something is a note they will trust
// instead. It runs in CI, so a bubbletea upgrade that changes key decoding
// fails here with the table in front of them.
//
// It answers two questions the legend depends on:
//
//   - the keys the panes bind are named on every terminal, from bytes any
//     terminal sends, with no keyboard protocol involved - `⇧⇥` included, which
//     is what made it available when `⇥` was taken over for pane focus;
//   - `⌃⇧A`, `⌃↵`, `⇧↵` and `⌃⇧↵` are not named *at all* - not merely unnamed,
//     but silently swallowed in both the encodings a protocol-speaking terminal
//     would send, so enabling Kitty mode makes them worse rather than better.
//     That is why spec §6's next-agent jump is a tab key, why broadcast stays
//     `@all`, and why the grid keys are not the `⇧↵` and `⌃⇧↵` they were asked
//     for.
//
// It needs no terminal: tea.WithInput takes any reader, and the program is
// driven by the bytes rather than by a TTY.
//
// **And that is its limit.** The grid keys shipped as `⌃⇧→`/`⌃⇧↓` on the
// strength of a green run here - bubbletea names them, every terminal sends
// them - and they reached Wake on no macOS at all: WindowServer takes the whole
// ctrl+shift+arrow family first. A byte that is never sent is not a byte this
// file can miss. TestNoKeyIsACtrlArrow is what holds that, and
// docs/notes/decisions.md has the measurement.

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// probeSentinel ends every case. Without it a program whose input produced no
// message at all waits forever for one, which is the deadlock the throwaway
// version of this hit - and "nothing arrived" is exactly the answer half these
// cases are checking for.
const probeSentinel = "\x03" // ⌃C

// probe collects the key messages one byte sequence produces, and quits on the
// sentinel.
type probe struct{ got *[]string }

func (p probe) Init() tea.Cmd { return nil }

func (p probe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	if m.Type == tea.KeyCtrlC {
		return p, tea.Quit
	}
	*p.got = append(*p.got, m.String())
	return p, nil
}

func (p probe) View() string { return "" }

// keysFor is what bubbletea reports for one byte sequence.
func keysFor(t *testing.T, seq string) []string {
	t.Helper()
	var got []string
	done := make(chan error, 1)
	go func() {
		_, err := tea.NewProgram(probe{got: &got},
			tea.WithInput(strings.NewReader(seq+probeSentinel)),
			tea.WithOutput(io.Discard),
			tea.WithoutRenderer(),
			tea.WithoutSignalHandler(),
		).Run()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("running the probe over %q: %v", seq, err)
		}
	case <-time.After(cmdTimeout):
		t.Fatalf("the probe over %q never finished: the sentinel did not arrive", seq)
	}
	return got
}

func TestTheKeysTheLegendNamesAreTheKeysBubbleteaReports(t *testing.T) {
	for _, tc := range []struct {
		what string
		seq  string
		want string
	}{
		{"⇥, from the byte every terminal sends", "\t", "tab"},
		{"⇧⇥, which carries the next-blocked jump now that ⇥ carries pane focus", "\x1b[Z", "shift+tab"},
		{"⌃D", "\x04", "ctrl+d"},
		{"⌃W", "\x17", "ctrl+w"},
		{"⌃G", "\x07", "ctrl+g"},
		{"⌃R", "\x12", "ctrl+r"},
		{"⌃F, which branches the conversation and shadows the text area's CharacterForward", "\x06", "ctrl+f"},
		{"⌃O, which detaches now that ⌃C parks", "\x0f", "ctrl+o"},
		{"⌃Q, which parks the fleet and closes Wake", "\x11", "ctrl+q"},
		{"⌃T, which flips the mention mode and shadows the text area's TransposeCharacterBackward", "\x14", "ctrl+t"},
		{"↵", "\r", "enter"},
		{"⌃Y, which opens a conversation in a new column", "\x19", "ctrl+y"},
		{"⌃B, which stacks one under the focused pane and shadows the text area's CharacterBackward", "\x02", "ctrl+b"},
		{"⌥↑, which walks the prompt history, in the CSI encoding", "\x1b[1;3A", "alt+up"},
		{"⌥↓, the same", "\x1b[1;3B", "alt+down"},
		{"⌥↑, in the Esc+ encoding a terminal set up for a meta key sends instead", "\x1b\x1b[A", "alt+up"},
		{"⌥↓, the same", "\x1b\x1b[B", "alt+down"},
		{"⇧→, which moves the keys to the pane on the right", "\x1b[1;2C", "shift+right"},
		{"⇧←", "\x1b[1;2D", "shift+left"},
		{"⇧↑, which moves them to the upper slot of a split column", "\x1b[1;2A", "shift+up"},
		{"⇧↓", "\x1b[1;2B", "shift+down"},
	} {
		got := keysFor(t, tc.seq)
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s: bubbletea reported %v, want [%s]. A legend entry whose key the library does not name is a key nobody can press", tc.what, got, tc.want)
		}
	}
}

// The finding that decided the next-agent key, held as a guard so an upgrade
// that changes it is a failure rather than a surprise.
//
// If one of these ever starts producing a message, that is not a bug here - it
// is the day `⌃⇧A` becomes bindable, and this test failing is how somebody
// finds out. The table in docs/notes/decisions.md is the same data.
func TestTheChordsSpecSixAsksForProduceNoKeyMessageAtAll(t *testing.T) {
	for _, tc := range []struct{ what, seq string }{
		{"⌃⇧A, Kitty CSI-u", "\x1b[97;6u"},
		{"⌃⇧A, xterm modifyOtherKeys", "\x1b[27;6;65~"},
		{"⌃↵, Kitty CSI-u", "\x1b[13;5u"},
		{"⌃↵, xterm modifyOtherKeys", "\x1b[27;5;13~"},
		{"⇧↵, Kitty CSI-u", "\x1b[13;2u"},
		{"⇧↵, xterm modifyOtherKeys", "\x1b[27;2;13~"},
		{"⌃⇧↵, Kitty CSI-u", "\x1b[13;6u"},
		{"⌃⇧↵, xterm modifyOtherKeys", "\x1b[27;6;13~"},
		// ⌘+arrow, which is what pane movement was asked for. The arrow table
		// in v1.3.10 knows modifier params 2-8 and cmd is bit 8, so every one
		// of these is param 9 or higher and matches no entry. The second wall
		// is not visible here and is the taller one: no macOS terminal hands ⌘
		// to a tty at all - it belongs to the terminal's own menus.
		{"⌘→", "\x1b[1;9C"},
		{"⌘←", "\x1b[1;9D"},
		{"⌘↑", "\x1b[1;9A"},
		{"⌘↓", "\x1b[1;9B"},
		{"⌃⌘→, in case the extra modifier finds a different row", "\x1b[1;13C"},
	} {
		if got := keysFor(t, tc.seq); len(got) != 0 {
			t.Errorf("%s now reports %v. bubbletea named nothing for it when ⇥ was chosen over it - if that has changed, spec §6's chord is bindable and the legend can say so", tc.what, got)
		}
	}
	// And the no-protocol case, which is the reason a chord that *is* delivered
	// would still be wrong: the bytes are a plain ⌃A, which is a different
	// intent entirely.
	if got := keysFor(t, "\x01"); len(got) != 1 || got[0] != "ctrl+a" {
		t.Errorf("⌃⇧A without a keyboard protocol reports %v, want [ctrl+a] - the collision that makes the chord unusable", got)
	}
	// The same collision for ⇧↵, and the worse one: a terminal with no keyboard
	// protocol sends it as the byte it sends for ↵, which is *send*. So even a
	// library that named the chord could not carry the grid keys on it without
	// making every ⇧↵ on such a terminal send the draft.
	if got := keysFor(t, "\r"); len(got) != 1 || got[0] != "enter" {
		t.Errorf("⇧↵ without a keyboard protocol reports %v, want [enter] - the collision that makes the chord unusable, and it collides with send", got)
	}
}

// The one encoding of ⌥↑ that arrives as nothing, which is why the prompt
// history has a line in docs/live-testing.md.
//
// Modifier 9 is how a terminal spells *meta* rather than alt for the same
// physical key, and bubbletea's arrow table stops at 8. The two encodings that
// do work are in the legend table above; whether ⌥+letter is headroom at all is
// PR #39's altprobe_test.go, which is the measurement this key was chosen from
// and is not repeated here.
func TestTheMetaEncodingOfAnAltArrowIsNamedByNothing(t *testing.T) {
	if got := keysFor(t, "\x1b[1;9A"); len(got) != 0 {
		t.Errorf("⌥↑ as modifier 9 now reports %v. If bubbletea has learned the meta encoding, the "+
			"prompt history reaches a terminal that spells the modifier that way and "+
			"docs/live-testing.md's caveat about it can go", got)
	}
}

// No key in App.key holds ctrl and an arrow, because on macOS none of them
// arrives.
//
// All eight are system shortcuts - ⌃←/→ and ⌃⇧←/→ move a space, ⌃↑/↓ and
// ⌃⇧↑/↓ are Mission Control and Application Windows - and WindowServer
// consumes them before the terminal is reached. The probe above cannot see
// this: it is a fact about the window server, not about bubbletea or about
// bytes, and every check this package can run passes on a key nobody can press.
// That is the exact failure the legend rule exists to stop, arriving through the
// one door the rule's own guards do not watch, so it is held as a class rather
// than left to be rediscovered by pressing it.
//
// It was ⌃⇧ alone until pane movement needed an arrow family and ⌃+arrow read
// as the obvious escape from the ⌃⇧ ban. It is the same trap one modifier
// shallower, and KeyCtrlShift… carries the KeyCtrl… prefix, so one check holds
// both.
func TestNoKeyIsACtrlArrow(t *testing.T) {
	bound := keyCasesIn(t, "keys.go", "key")
	if len(bound) == 0 {
		t.Fatal("App.key binds nothing: the scan is broken and this test is asserting nothing")
	}
	arrows := []string{"Up", "Down", "Left", "Right"}
	for name := range bound {
		if !strings.HasPrefix(name, "KeyCtrl") {
			continue
		}
		for _, a := range arrows {
			if strings.HasSuffix(name, a) {
				t.Errorf("App.key binds tea.%s, and macOS takes every ctrl+arrow and ctrl+shift+arrow "+
					"before any terminal sees it. bubbletea names the chord and every terminal sends "+
					"it, so this passes every other guard in this package while advertising a key that "+
					"does nothing - see docs/notes/decisions.md", name)
			}
		}
	}
}

// What a *fast* double ⌃O is by the time bubbletea has read it, which is the
// question escprobe_test.go asks about ⎋⎋ and gets the opposite answer to.
//
// ⎋⎋ sharing one read collapses into a single `alt+esc`, because bubbletea
// reads a lone ESC followed by another byte as that byte with Alt set. Nothing
// does that for \x0f: **two ⌃O in one read are two plain messages**, identical
// to two deliberate presses seconds apart. So terminal auto repeat, and the
// human reply to a key that appeared to do nothing, both reach App.key as a
// second ⌃O and nothing anywhere can tell them from intent.
//
// That is the measurement behind the confirm being ↵ rather than a second ⌃O;
// see detach.go. If bubbletea ever starts collapsing these, this failing is how
// somebody finds out that a repeat has become distinguishable.
func TestTwoCtrlOsInOneReadArriveAsTwoPresses(t *testing.T) {
	if got := keysFor(t, "\x0f\x0f"); len(got) != 2 || got[0] != "ctrl+o" || got[1] != "ctrl+o" {
		t.Errorf("⌃O⌃O sharing one read reports %v, want [ctrl+o ctrl+o]. The detach confirm is a "+
			"different key because a repeat is indistinguishable from a press; if that has changed, "+
			"the argument in detach.go has changed with it", got)
	}
}

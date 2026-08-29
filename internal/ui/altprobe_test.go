package ui

// What ⌥ is worth as headroom, measured rather than assumed.
//
// Wake's ⌃ space is full: the text area claims ⌃a ⌃b ⌃d ⌃e ⌃f ⌃h ⌃k ⌃n ⌃p ⌃t
// ⌃u ⌃v ⌃w, ⌃S and ⌃Q are the terminal's flow control, ⌃Z is suspend, and the
// legend already binds thirteen of what is left. So the next key this product
// needs comes from ⌥ or from nowhere - and this project has twice shipped a key
// that the library named and no terminal delivered (⌃⇧A, then the ⌃⇧-arrow grid
// keys). That is the mistake this file exists to stop repeating.
//
// **The two halves of ⌥ are not equally safe, and the difference decides which
// one a feature may be built on.**
//
//   - ⌥+arrow is named in the xterm modifier encoding, which terminals send
//     with no configuration and no keyboard protocol. It is real headroom.
//   - ⌥+letter is named only from the ESC prefix, which a terminal emits only
//     when it has been told to treat Option as Meta - off by default in
//     Terminal.app, and a setting in iTerm2. Under a Kitty-protocol terminal it
//     produces **no message at all**, which is ⌃⇧A's failure exactly: enabling a
//     protocol makes the chord vanish rather than work.
//
// So a binding an operator must be able to reach goes on ⌥+arrow. A ⌥+letter
// binding is a convenience for a configured terminal and may never be the only
// way to do something.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestAltArrowsAreNamedInTheEncodingTerminalsSend is the headroom claim. If it
// ever fails, the one modifier space Wake has left is gone and whatever was
// built on it needs a different key.
func TestAltArrowsAreNamedInTheEncodingTerminalsSend(t *testing.T) {
	for _, tc := range []struct{ what, seq, want string }{
		{"⌥↑, xterm modifier - what a terminal sends unconfigured", "\x1b[1;3A", "alt+up"},
		{"⌥↓, xterm modifier", "\x1b[1;3B", "alt+down"},
		{"⌥↑, ESC prefix - what a Meta-configured terminal sends", "\x1b\x1b[A", "alt+up"},
	} {
		got := keysFor(t, tc.seq)
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s: bubbletea reported %v, want [%s]. ⌥+arrow is the only modifier space this product has left", tc.what, got, tc.want)
		}
	}
}

// TestAltLettersNeedAConfiguredTerminalAndVanishUnderKitty is the warning, held
// as a guard so it is a failure rather than a rediscovery.
//
// Both halves are asserted. The ESC-prefix half says ⌥+letter works at all; the
// Kitty half says it stops working on the terminals most likely to be running a
// keyboard protocol. A binding that is a *sole* route to something must not be
// built here - see this file's header.
func TestAltLettersNeedAConfiguredTerminalAndVanishUnderKitty(t *testing.T) {
	for _, tc := range []struct{ what, seq, want string }{
		{"⌥O", "\x1bo", "alt+o"},
		{"⌥T", "\x1bt", "alt+t"},
		{"⌥R", "\x1br", "alt+r"},
		{"⌥G", "\x1bg", "alt+g"},
	} {
		got := keysFor(t, tc.seq)
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s from the ESC prefix: bubbletea reported %v, want [%s]", tc.what, got, tc.want)
		}
	}
	// The trap. 111 and 116 are 'o' and 't'; 3 is the modifier code for alt.
	for _, tc := range []struct{ what, seq string }{
		{"⌥O, Kitty CSI-u", "\x1b[111;3u"},
		{"⌥T, Kitty CSI-u", "\x1b[116;3u"},
	} {
		if got := keysFor(t, tc.seq); len(got) != 0 {
			t.Errorf("%s now reports %v. bubbletea named nothing for it when this was measured - if that has changed, ⌥+letter is safe under a keyboard protocol and this file's ruling can be relaxed", tc.what, got)
		}
	}
}

// And the arrows are unmodified by comparison, which is what keeps bare ↑↓ for
// the roster or the query cursor and ⌥↑↓ available for the prompt history.
func TestBareArrowsAreStillTheirOwnKeys(t *testing.T) {
	for _, tc := range []struct {
		seq  string
		want tea.KeyType
	}{
		{"\x1b[A", tea.KeyUp},
		{"\x1b[B", tea.KeyDown},
	} {
		if got := keysFor(t, tc.seq); len(got) != 1 || got[0] != tc.want.String() {
			t.Errorf("bubbletea reported %v for %q, want [%s]", got, tc.seq, tc.want)
		}
	}
}

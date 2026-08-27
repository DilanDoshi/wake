package ui

// What two escapes look like by the time bubbletea has read them.
//
// ⎋⎋ is a *double press*, and a double press is only two messages if the
// library says so. A terminal hands over whatever bytes are pending in one
// read, so a fast press arrives as `\x1b\x1b` in a single chunk - and
// bubbletea reads a lone ESC followed by another byte as that byte with Alt
// set. **Measured: it is one message, `alt+esc`, not two.**
//
// That is why App.key passes `m.Alt` to escape() as a collapsed double press.
// Without it the feature works for somebody pressing slowly and silently fails
// for everybody else - the failure mode nobody reports and everybody feels.
//
// This is keyprobe_test.go's question one key over, and it is checkable in
// process for the same reason: tea.WithInput takes a reader.

import "testing"

// The one claim this harness can make honestly. It cannot measure a *lone* ⎋:
// the probe's sentinel is ⌃C and everything is one read, so `\x1b\x03` merges
// into alt+ctrl+c and the probe quits having recorded nothing. A single press
// is two reads on a real terminal, and the pty harness is where that is
// checkable - see cmd/wake/keysscreen_unix_test.go.
func TestTwoEscapesInOneReadCollapseIntoAltEsc(t *testing.T) {
	got := keysFor(t, "\x1b\x1b")
	if len(got) != 1 || got[0] != "alt+esc" {
		t.Errorf("⎋⎋ sharing one read reports %v, want [alt+esc]. App.key reads m.Alt as a collapsed double press; if bubbletea has started reporting two messages that branch is dead code and ⎋⎋ should be two plain presses again", got)
	}
}

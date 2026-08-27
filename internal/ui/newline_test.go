package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A draft can have a second line, and ↵ is not how you get one.
//
// The text area is multi-line and shipped bound to `enter`/`ctrl+m` for
// InsertNewline - both of which App.key takes for *send* before the composer
// sees them. So the box was multi-line and nothing could put a second line in
// it: every attempt sent the draft instead, which is the one failure mode that
// cannot be undone by pressing something else.
func TestADraftTakesANewlineWithoutBeingSent(t *testing.T) {
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyEnter, Alt: true}, // ⌥↵ - what a terminal set up for ⇧↵ sends
		{Type: tea.KeyCtrlJ},            // ⌃J - the fallback that needs no setup
	} {
		t.Run(k.String(), func(t *testing.T) {
			c := NewComposer()
			c = typeInto(t, c, "first")
			c, _ = c.Update(k)
			c = typeInto(t, c, "second")

			if got := c.Value(); !strings.Contains(got, "\n") {
				t.Fatalf("%s left the draft %q, with no newline in it: the composer is multi-line and "+
					"this is the only way to reach a second line", k.String(), got)
			}
			if got := c.Value(); !strings.HasPrefix(got, "first") || !strings.HasSuffix(got, "second") {
				t.Errorf("%s produced %q, want the two halves either side of the break", k.String(), got)
			}
		})
	}
}

// ↵ is still send, which is the half that must not change.
//
// A newline key is only safe because the key beside it kept its meaning: if
// rebinding InsertNewline had also made ↵ insert one, every message in the
// fleet would need a second keystroke to leave.
func TestEnterIsStillSendRatherThanANewline(t *testing.T) {
	c := typeInto(t, NewComposer(), "hello")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if strings.Contains(c.Value(), "\n") {
		t.Errorf("↵ put a newline in the draft (%q); it is send, and the composer never sees it", c.Value())
	}
}

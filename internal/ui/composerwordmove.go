package ui

// ⌥← and ⌥B with no word to their left (BUG-78).
//
// bubbles v1.0.0's wordLeft steps left until it lands on a non-space. At row 0,
// column 0 that step is a no-op, so with no non-space left of the caret on any
// row above or before it the walk never ends: it spins forever on the room's
// Update goroutine, with ⌃C⌃C the only way out. (The caret at the very start of
// a draft, before a word, returns at once and moves nothing.) The text area is
// left as it is - a `replace` breaks `go install` - and the key is answered here
// before it reaches it, as a no-op, the move having nowhere to go. Probed over
// every caret of a spread of drafts, no other word key loops or panics.

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// wordLeftStuck reports whether msg is the text area's word-backward key and
// the draft holds no non-space left of the caret - which also takes in the
// harmless start-of-draft case, where wordLeft would move nothing as well. The
// move cannot be run on a copy to find out, as CanCursorUp does: that is the loop.
func (c Composer) wordLeftStuck(msg tea.Msg) bool {
	k, ok := msg.(tea.KeyMsg)
	if !ok || !key.Matches(k, c.ta.KeyMap.WordBackward) {
		return false
	}
	lines := strings.Split(c.ta.Value(), "\n")
	line, info := c.ta.Line(), c.ta.LineInfo()
	here := string([]rune(lines[line])[:info.StartColumn+info.ColumnOffset])
	return strings.TrimSpace(strings.Join(lines[:line], "")+here) == ""
}

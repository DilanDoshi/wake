package ui

import "strings"

// oneLine flattens text to a single printable line.
//
// Every control character goes, not only the newline: a carriage return
// redraws the row from column zero, an escape can move the cursor or clear the
// screen outright, and all of them arrive from the same untrusted place - an
// agent's stderr, by way of the daemon's error frames. A space keeps words
// apart where a line break used to be.
//
// The second class of caller is a **dispatch's name** (taskrows.go's Task.name,
// rostersubs.go's subagentName), which is a description a model wrote. There the
// newline is the one that bites rather than the escape: lipgloss measures and
// clips per line, so a name carrying one draws two physical rows out of a value
// the height oracle counted as one - a pane taller than its terminal, which
// scrolls the alt screen away on every draw, and a sidebar whose clicks map to
// the wrong agent below that row.
//
// C1 (U+0080-U+009F) goes with C0 and DEL. A terminal in 8-bit mode reads
// U+009B as CSI, which is an escape sequence introducer with no ESC in front of
// it - so stripping only the C0 escape would leave the same capability behind
// under a different byte.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' || (r >= '\x7f' && r <= '\u009f') {
			return ' '
		}
		return r
	}, s)
}

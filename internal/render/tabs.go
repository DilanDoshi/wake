package render

// Tabs. ansi counts one as no cell; a terminal draws up to eight and erases none of
// them, so a row that holds one shows the frame before, overruns its pane and, past
// the terminal's width, wraps every row below. Anything this package measures
// expands them first, column-aware: markdown, diffs, tool results, and the streamed
// preview. Surfaces drawn by lipgloss's Render alone (an own turn, cards, local
// replies, peer messages, workflow sections) get its fixed four spaces before it
// wraps. Not at the airlock: an answer is keyed on the ask's raw text.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// tabWidth is the stop ExpandTabs lands on, in cells - the one CommonMark reads a
// tab in a block's indentation as, so a tab-indented list or code block keeps its
// structure.
const tabWidth = 4

// ExpandTabs replaces every tab with the spaces to the next tabWidth stop, so
// what is measured is what the terminal draws. A string with no tab comes back as
// it came.
func ExpandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	out, _ := ExpandTabsAt(0, s)
	return out
}

// ExpandTabsAt is ExpandTabs for text that begins at cell column col, and reports
// the column it ends at - so text that arrives in pieces, or whose line's start was
// cut away, expands as it would whole. The column is counted in cells (a wide rune
// is two, a combining mark none) and restarts after each newline.
func ExpandTabsAt(col int, s string) (string, int) {
	if !strings.Contains(s, "\t") { // the common token: only the column moves
		if i := strings.LastIndexByte(s, '\n'); i >= 0 {
			return s, ansi.StringWidth(s[i+1:])
		}
		return s, col + ansi.StringWidth(s)
	}
	var b strings.Builder
	for len(s) > 0 {
		i := strings.IndexAny(s, "\t\n")
		if i < 0 {
			b.WriteString(s)
			col += ansi.StringWidth(s)
			break
		}
		b.WriteString(s[:i])
		col += ansi.StringWidth(s[:i])
		if s[i] == '\n' {
			b.WriteByte('\n')
			col = 0
		} else {
			pad := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", pad))
			col += pad
		}
		s = s[i+1:]
	}
	return b.String(), col
}

package render

// Tabs. ansi.StringWidth counts a tab as zero cells, and a terminal moves to
// the next eight-column stop without erasing the cells it skips - so a row
// holding one is measured short and drawn long, shows what was there before
// through the gap, overruns its pane and, past the terminal's width, wraps and
// shifts every row below it. The cure is to leave none in anything this package
// returns, which means expanding them where they are still measurable.
//
// Two expansion rules coexist, and each surface is self-consistent, so what it
// draws is what a copy of it reads back:
//
//   - ExpandTabs, column-aware to a four-column stop: markdown (at its entry),
//     diffs and tool results here, and the streamed preview (internal/ui's
//     partial.go) - the surfaces whose rows this package or the preview measures.
//   - lipgloss's own fixed four spaces per tab, for the surfaces drawn by
//     Style.Render alone - the operator's own turn and the cards - which
//     internal/ui's copytext.go reads back as ownTabWidth.
//
// Not at the airlock: core.Contained leaves a tab alone because an answer is
// keyed on the ask's raw text (EncodeAnswer), and a string stored expanded
// would no longer match the one the agent sent.

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

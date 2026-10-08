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

// expandSource is ExpandTabs for markdown, placing each tab where CommonMark would:
// structure from the line's start, and a fenced block's code from the column its
// fence opened at, so a tab-indented line of code in a list item or a quote indents
// from the code's own edge rather than from the container's prefix.
func expandSource(src string) string {
	if !strings.Contains(src, "\t") {
		return src
	}
	lines := strings.Split(src, "\n")
	run, inset := "", 0 // the open fence's run, and the column its code starts at
	for i, line := range lines {
		switch {
		case run == "":
			run, inset = fenceOpener(line)
			lines[i] = ExpandTabs(line)
		case closes(line, run):
			run = ""
			lines[i] = ExpandTabs(line)
		default:
			lines[i] = expandFrom(line, inset)
		}
	}
	return strings.Join(lines, "\n")
}

// fenceOpener is the run of a fence that line opens - ``` or ~~~ and their kin -
// and the column it opens at, past any quote marks, list marker and indent; "" for
// a line that opens none.
func fenceOpener(line string) (string, int) {
	text := ExpandTabs(line)
	at := len(text) - len(strings.TrimLeft(text, " >"))
	rest := text[at:]
	if n := listMarkerWidth(rest); n > 0 {
		at += n + len(rest[n:]) - len(strings.TrimLeft(rest[n:], " "))
		rest = text[at:]
	}
	if rest == "" || (rest[0] != '`' && rest[0] != '~') {
		return "", 0
	}
	n := len(rest) - len(strings.TrimLeft(rest, rest[:1]))
	if n < 3 || (rest[0] == '`' && strings.Contains(rest[n:], "`")) {
		return "", 0
	}
	return rest[:n], at
}

// closes reports whether line closes the fence run opened: the same mark, at
// least as long, and nothing after it but space.
func closes(line, run string) bool {
	rest := strings.TrimLeft(line, " \t>")
	return strings.HasPrefix(rest, run) && strings.TrimSpace(strings.TrimLeft(rest, run[:1])) == ""
}

// listMarkerWidth is the width of the list marker text opens with (`-`, `*`, `+`,
// `1.`, `1)`), followed by a space, or 0.
func listMarkerWidth(text string) int {
	if len(text) >= 2 && strings.ContainsRune("-*+", rune(text[0])) && text[1] == ' ' {
		return 1
	}
	digits := len(text) - len(strings.TrimLeft(text, "0123456789"))
	if digits > 0 && digits < len(text)-1 && (text[digits] == '.' || text[digits] == ')') && text[digits+1] == ' ' {
		return digits + 1
	}
	return 0
}

// expandFrom expands a line of a fence's code whose first inset columns are its
// container's prefix: the prefix from the line's start, the code from its own edge.
// A tab that straddles the edge leaves its far part to the code, as CommonMark does.
func expandFrom(line string, inset int) string {
	var b strings.Builder
	col, i := 0, 0
	for ; i < len(line) && col < inset && strings.IndexByte(" >\t", line[i]) >= 0; i++ {
		if line[i] != '\t' {
			b.WriteByte(line[i])
			col++
			continue
		}
		stop := (col/tabWidth + 1) * tabWidth
		b.WriteString(strings.Repeat(" ", stop-col))
		col = stop
	}
	code, _ := ExpandTabsAt(max(col-inset, 0), line[i:])
	return b.String() + code
}

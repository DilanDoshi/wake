package render

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Each diff line carries a two-space gutter and a marker before the content.
const (
	diffDelMarker = "  - "
	diffAddMarker = "  + "
)

// Wake's diff colours. A changed line is a full-width *band* - the ground is the
// signal, not the letterform - and within a paired change the words that
// actually differ carry a brighter ground still. That is why there are four
// colours here rather than two.
//
// The light bands are Claude Code's own, kept by hand alongside the rest of the
// palette. The dark bands are muted below Claude's on the owner's 2026-08-29
// override: deeper, more translucent grounds the white text reads cleanly over,
// each word ground still a step brighter than its band. markdownstyle_test.go
// pins all four.
var (
	addBand = lipgloss.NewStyle().
		Background(lipgloss.AdaptiveColor{Light: "#69db7c", Dark: "#17351e"}).
		Foreground(lipgloss.AdaptiveColor{Light: "#000000", Dark: "#ffffff"})
	delBand = lipgloss.NewStyle().
		Background(lipgloss.AdaptiveColor{Light: "#ffa8b4", Dark: "#3f1d24"}).
		Foreground(lipgloss.AdaptiveColor{Light: "#000000", Dark: "#ffffff"})
	addWord = addBand.Background(lipgloss.AdaptiveColor{Light: "#2f9d44", Dark: "#256b3f"})
	delWord = delBand.Background(lipgloss.AdaptiveColor{Light: "#d1454b", Dark: "#7a3a46"})
)

// span is a run of a diff line that shares one ground: either the line's band
// or, where the counterpart line differs, the emphasised word ground.
type span struct {
	text string
	emph bool
}

// MaxDiffLines bounds how many changed lines are *rendered*, counting both
// sides together.
//
// The cap is here rather than over the finished string, and that is the whole
// point of it. The trim used to happen after: Diff word-diffed, styled and
// padded every line and a caller then threw nearly all of it away, so two edits
// at opposite ends of a 50,000-line file built a hundred thousand styled rows
// inside the draw loop before anything bounded them. The cap now decides what
// to render, not what to keep.
//
// It is set high on purpose. §9 of the spec promises the DM "full +/−" diffs
// where the group chat shows none, so this must never trim an honest edit - a
// 40-line-in, 40-line-out rewrite is an ordinary refactor and has to render
// whole. Only the prefix/suffix blow-up reaches hundreds of lines for a change
// of two.
const MaxDiffLines = 200

// hiddenLinesFormat reports what the cap removed. The two-space indent lines it
// up with the gutter above it.
const hiddenLinesFormat = "  " + ellipsis + " %d lines not shown"

// tabWidth is what one tab is expanded to before anything measures a line.
//
// Expanded rather than measured, because ansi.StringWidth counts a tab as zero
// and a terminal does not: `"\tfoo"` measures 3 and occupies 8 columns, so a
// band padded to fill 20 drew 24 and wrapped into the next row. Source is
// tab-indented far more often than not, so this is the common case rather than
// an edge one.
const tabWidth = 4

// Diff renders a line-level diff. This is a deliberately simple
// common-prefix/suffix trim rather than a full Myers diff: Edit results
// are usually one contiguous hunk, and a wrong-but-fast diff is worse
// than a simple correct one.
//
// Removed lines are emitted before added ones, and the i-th of each is treated
// as a pair for word-level emphasis. That pairing is positional for the same
// reason the line diff is prefix/suffix: it is right for the common case - a
// line edited in place - and degrades to a plain band when it is not, never to
// a wrong claim about which words moved.
func Diff(oldStr, newStr string, width int) string {
	if oldStr == newStr {
		return ""
	}
	del, add := changedSides(oldStr, newStr)
	del, add, hidden := capSides(del, add)

	var b strings.Builder
	for i, l := range del {
		b.WriteString(band(diffDelMarker, l, counterpart(add, i), width, delBand, delWord))
		b.WriteString("\n")
	}
	if hidden > 0 {
		// Between the two halves, where the cut actually is: both polarities
		// survive it, which is what keeps an over-long diff from rendering as
		// a solid wall of one colour.
		b.WriteString(ansi.Truncate(fmt.Sprintf(hiddenLinesFormat, hidden), width, ellipsis))
		b.WriteString("\n")
	}
	for i, l := range add {
		b.WriteString(band(diffAddMarker, l, counterpart(del, i), width, addBand, addWord))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// changedSides is the removed and added lines of an edit, with the common
// prefix and suffix trimmed off. One function so DiffSummary counts exactly the
// lines Diff draws - two walks of the same rule would drift, and the drift
// would be a header claiming a different edit from the bands under it.
func changedSides(oldStr, newStr string) (del, add []string) {
	oldLines, newLines := splitLines(oldStr), splitLines(newStr)
	start := commonPrefix(oldLines, newLines)
	endOld, endNew := trimCommonSuffix(oldLines, newLines, start)
	return oldLines[start:endOld], newLines[start:endNew]
}

// DiffSummary is how much an edit changed, in Claude Code's own words:
// `Added 9 lines, removed 1 line`.
//
// It sits with the tool header rather than inside Diff, which is where Claude
// draws it and also the only place it can go: every line Diff emits is a
// full-width band bounded to the pane, and a summary is neither. Empty when
// nothing changed, so a caller can concatenate without asking.
func DiffSummary(oldStr, newStr string) string {
	if oldStr == newStr {
		return ""
	}
	del, add := changedSides(oldStr, newStr)
	return diffSummary(len(del), len(add))
}

// diffSummary is how much this edit changed, in Claude Code's own words.
//
// Singular and plural are both spelled out because "removed 1 lines" is the
// tell that a count was formatted rather than written, and this line sits at
// the top of every edit in the fleet.
//
// A pure removal or a pure addition names only the half that happened: "added 0
// lines" beside a real removal reads as a second fact, and it is not one.
func diffSummary(removed, added int) string {
	switch {
	case removed == 0:
		return fmt.Sprintf("Added %s", lineCount(added))
	case added == 0:
		return fmt.Sprintf("Removed %s", lineCount(removed))
	default:
		return fmt.Sprintf("Added %s, removed %s", lineCount(added), lineCount(removed))
	}
}

// lineCount is n with the right noun on it.
func lineCount(n int) string {
	if n == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", n)
}

// capSides bounds what will be rendered, keeping each side's head and tail and
// reporting how many lines were dropped.
//
// Both ends of each side rather than the head: the removed lines are all
// emitted before the added ones, so keeping only the head of an over-long diff
// renders it as a wall of red with no green at all - the worst available cut in
// the one view whose promise is "full +/−".
func capSides(del, add []string) ([]string, []string, int) {
	total := len(del) + len(add)
	if total <= MaxDiffLines {
		return del, add, 0
	}
	// Each side gets its share of the budget, and a short side lends the rest
	// of its share to the long one.
	half := MaxDiffLines / 2
	delRoom, addRoom := min(len(del), half), min(len(add), half)
	if spare := MaxDiffLines - delRoom - addRoom; spare > 0 {
		if len(del) > delRoom {
			delRoom = min(len(del), delRoom+spare)
		} else if len(add) > addRoom {
			addRoom = min(len(add), addRoom+spare)
		}
	}
	return endsOf(del, delRoom), endsOf(add, addRoom), total - delRoom - addRoom
}

// endsOf keeps the first and last lines of a side, up to room of them.
func endsOf(lines []string, room int) []string {
	if len(lines) <= room {
		return lines
	}
	head, tail := room/2, room-room/2
	kept := make([]string, 0, room)
	kept = append(kept, lines[:head]...)
	return append(kept, lines[len(lines)-tail:]...)
}

// counterpart is the line on the other side of the change that lines up with
// index i, or "" when the two sides are different lengths there.
func counterpart(other []string, i int) string {
	if i < len(other) {
		return other[i]
	}
	return ""
}

// band renders one diff line as a full-width coloured band.
//
// The ground has to reach the right-hand edge or the block reads as ragged
// text rather than a band, and it has to survive every span boundary - so each
// span is rendered with its own complete style rather than nested inside an
// outer one, whose reset the first inner span would end. The padding that
// fills the line carries the band style for the same reason.
// A line too wide for the pane wraps, with the marker repeated down the left
// edge so every row still says which side of the change it is - which is what
// Claude Code's own diff does. It used to end in an ellipsis with the rest
// unreachable, and an edit whose point is past column 80 was an edit the
// operator could not read.
func band(marker, line, against string, width int, bandStyle, wordStyle lipgloss.Style) string {
	room := width - ansi.StringWidth(marker)
	if width < 1 || room < 1 {
		return ""
	}
	rows := wrapSpans(emphasise(expandTabs(line), expandTabs(against)), room)
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		var b strings.Builder
		b.WriteString(bandStyle.Render(marker))
		used := ansi.StringWidth(marker)
		for _, s := range row {
			style := bandStyle
			if s.emph {
				style = wordStyle
			}
			b.WriteString(style.Render(s.text))
			used += ansi.StringWidth(s.text)
		}
		if pad := width - used; pad > 0 {
			b.WriteString(bandStyle.Render(strings.Repeat(" ", pad)))
		}
		out = append(out, b.String())
	}
	return strings.Join(out, "\n")
}

// wrapSpans breaks a span list into rows of at most width cells, cutting
// inside a span where it has to and carrying the emphasis across the cut.
//
// By cell rather than by word: a diff band is source, where the indentation is
// half of what makes a line readable, and word-wrapping moves it.
func wrapSpans(spans []span, width int) [][]span {
	if width < 1 {
		return nil
	}
	var rows [][]span
	var cur []span
	used := 0
	for _, s := range spans {
		text := s.text
		for ansi.StringWidth(text) > 0 {
			if used >= width {
				rows, cur, used = append(rows, cur), nil, 0
			}
			w, room := ansi.StringWidth(text), width-used
			if w <= room {
				cur, used = append(cur, span{text: text, emph: s.emph}), used+w
				break
			}
			cut := ansi.Truncate(text, room, "")
			if cutW := ansi.StringWidth(cut); cutW > 0 {
				cur = append(cur, span{text: cut, emph: s.emph})
				text = ansi.TruncateLeft(text, cutW, "")
			} else if used == 0 {
				// A rune wider than a whole row, which only a one-column band
				// can produce. Dropped rather than looped on forever.
				text = ansi.TruncateLeft(text, 1, "")
			}
			rows, cur, used = append(rows, cur), nil, 0
		}
	}
	return append(rows, cur)
}

// expandTabs replaces tabs with spaces to the next stop, so what is measured is
// what the terminal will draw. See tabWidth.
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			pad := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", pad))
			col += pad
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
}

// emphasise splits line into spans, marking the run that differs from against.
// A line with no counterpart is one unemphasised span: with nothing to compare
// it to, every word is equally new and highlighting all of them says nothing.
func emphasise(line, against string) []span {
	if against == "" || line == "" {
		return []span{{text: line}}
	}
	a, b := tokenize(line), tokenize(against)
	start := commonPrefix(a, b)
	endA, _ := trimCommonSuffix(a, b, start)

	if start == 0 && endA == len(a) {
		// Nothing in common at either end: the whole line changed, so a band
		// with one all-over highlight would just be a louder band.
		return []span{{text: line}}
	}
	// Empty spans are dropped rather than rendered: styling "" still emits the
	// escapes to enter and leave the style, for no cells.
	var out []span
	for _, s := range []span{
		{text: strings.Join(a[:start], "")},
		{text: strings.Join(a[start:endA], ""), emph: true},
		{text: strings.Join(a[endA:], "")},
	} {
		if s.text != "" {
			out = append(out, s)
		}
	}
	return out
}

// tokenize splits a line into alternating runs of space and non-space, so a
// word-level comparison keeps the whitespace that separated the words and can
// be rejoined without it.
func tokenize(s string) []string {
	var out []string
	var cur []rune
	var inSpace bool
	for i, r := range s {
		space := unicode.IsSpace(r)
		if i > 0 && space != inSpace {
			out = append(out, string(cur))
			cur = cur[:0]
		}
		inSpace = space
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

// splitLines splits content into lines, ignoring a trailing newline. Empty
// content is zero lines rather than one blank line, so creating a file from
// nothing does not render a phantom removed line.
func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// commonPrefix counts the leading items the two sides share.
func commonPrefix(oldItems, newItems []string) int {
	i := 0
	for i < len(oldItems) && i < len(newItems) && oldItems[i] == newItems[i] {
		i++
	}
	return i
}

// trimCommonSuffix returns the end of the changed region on each side, having
// walked back over the trailing items the two sides share.
func trimCommonSuffix(oldItems, newItems []string, start int) (int, int) {
	endOld, endNew := len(oldItems), len(newItems)
	for endOld > start && endNew > start && oldItems[endOld-1] == newItems[endNew-1] {
		endOld--
		endNew--
	}
	return endOld, endNew
}

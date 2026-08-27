package ui

// The sample beside an option: what choosing it produces, as the ask wrote it.
//
// # Why this is drawn at all, having been ruled out once
//
// core.AskDetail deliberately dropped the preview for as long as a card was a
// room-only surface, and the argument was sound there: a preview is a document,
// the room is a hub, and deep reading is DM work. It stopped being sound when a
// question became answerable in a conversation, which is a 1:1 pane with room to
// read in. The old ruling was about the room and is kept for the room - a narrow
// pane still drops this entirely.
//
// # Three tiers, and dropping is one of them
//
// Beside the options where both halves clear their floors, under them where the
// panel alone does, and nothing at all below that. The last is not a failure
// case: the room is one column of a three-region layout, the card is pinned
// until somebody answers it, and rows are scarcest exactly where the panel is
// least readable. The option's own detail line survives every tier, and that is
// the sentence a choice can actually be made on.
//
// # Not markdown, deliberately
//
// The recorded samples are a markdown table and the CSV beside it - monospace
// content that reads correctly raw and would be *re*-flowed by a renderer into
// something that no longer lines up. And a pinned card goes back through its
// renderer on every frame: docs/notes/deferred.md records a plan card costing
// about a quarter of a frame through glamour, and a panel that did the same
// would pay it for as long as the ask is open. Split, bound, truncate.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	// previewLines is how much of a sample survives beside the options. Enough
	// for a header row and a few rows under it, which is what makes a table
	// legible as a table.
	previewLines = 6

	// previewStackedLines is how much survives under them. Fewer, because a
	// stacked panel spends the pane's scarcest axis: a card is pinned until it
	// is answered, and every row it takes is a row of conversation.
	previewStackedLines = 3

	// previewMinPanel is the narrowest panel worth drawing. Below it a sample
	// is truncated past the point of saying anything, and a panel that shows
	// nothing legible is worse than the detail line it displaced.
	previewMinPanel = 24

	// previewMinOptions is what the options keep when a panel sits beside them.
	// They are the half that can be answered, so they are the half that wins.
	previewMinOptions = 32

	// previewRule separates the two columns. A rule rather than a gap, because
	// the sample is text an agent wrote and the reader has to be able to see
	// where Wake's row ends and it begins.
	previewRuleGlyph = "│"
	previewRule      = " " + previewRuleGlyph + " "

	// previewMoreFmt is what the panel cut. A panel that simply stopped would
	// read as the whole of what the option produces.
	previewMoreFmt = "+%d more lines"
)

// previewSplit divides a width between the options and a panel beside them.
//
// ok is false when either half would fall below its floor, which is the signal
// to stack or to drop rather than to squeeze both into illegibility.
func previewSplit(width int) (options, panel int, ok bool) {
	rule := ansi.StringWidth(previewRule)
	panel = (width - rule) / 2
	options = width - rule - panel
	if panel < previewMinPanel || options < previewMinOptions {
		return width, 0, false
	}
	return options, panel, true
}

// previewBlock is a sample bounded to a box: its first lines, each truncated to
// the width, and a muted row saying what was cut.
//
// Rendered line by line rather than as one block, which is what keeps a sample
// inside its panel: it is text a model wrote, and the newlines in it are the
// one thing that could otherwise put a row of Wake's own frame under an agent's
// control. Every line that arrives becomes at most one row here.
func previewBlock(sample string, width, keep int) string {
	lines := strings.Split(strings.TrimRight(sample, "\n"), "\n")
	kept := lines[:min(len(lines), max(keep, 1))]

	rows := make([]string, 0, len(kept)+1)
	for _, line := range kept {
		rows = append(rows, HintStyle.MaxWidth(width).Render(ansi.Truncate(line, width, ellipsis)))
	}
	if cut := len(lines) - len(kept); cut > 0 {
		rows = append(rows, mutedLine(fmt.Sprintf(previewMoreFmt, cut), width))
	}
	return strings.Join(rows, "\n")
}

// beside joins the options and a panel into two columns with the rule between.
//
// Both are padded to their own width first, because lipgloss sizes a joined row
// on its widest line and a short options block would otherwise let the panel
// slide left under it.
func beside(options, panel string, optionsWidth, panelWidth int) string {
	rows := max(lipgloss.Height(options), lipgloss.Height(panel))
	box := func(block string, width int) string {
		return lipgloss.NewStyle().Width(width).MaxWidth(width).Height(rows).Render(block)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		box(options, optionsWidth), previewRuleColumn(rows), box(panel, panelWidth))
}

// previewRuleColumn is the rule drawn its whole height, the way dividerColumn
// draws the one between two panes.
//
// One glyph on the top row is what lipgloss gives a single-line block joined
// against taller ones, and it leaves the panel unbounded on every row but the
// first - which is precisely the rows a sample an agent wrote occupies.
func previewRuleColumn(rows int) string {
	out := make([]string, max(rows, 1))
	for i := range out {
		out[i] = previewRule
	}
	return HintStyle.Render(strings.Join(out, "\n"))
}

package render

// The open block of a streaming answer is drawn as plain text, and drawn the way
// Markdown lays prose out - inside the document margin, wrapped greedily at the
// width less both margins - so the block that finishes does not jump a column or
// re-break its rows as it turns formatted. It is not a second wrap: it is the same
// x/ansi.Wrap reflowProse re-wraps glamour's prose with, at the same budget.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// proseBudget is the width a block's text wraps at: what is left of width past the
// block's lead and the far margin, as glamour counts it.
func proseBudget(width, lead int) int { return width - lead - int(defaultMargin) }

// Prose lays plain text out the way Markdown lays out prose: every source line on
// its own rows, each inside the document margin and wrapped at width less the
// margin on both sides. A blank source line is a blank row. Text is never cut or
// reflowed across lines - what the agent wrote is what is drawn - and below the
// width Markdown can lay out it wraps at the floor, as Markdown does.
func Prose(text string, width int) string {
	margin := strings.Repeat(" ", int(defaultMargin))
	budget := proseBudget(boundedWidth(width), int(defaultMargin))
	lines := strings.Split(text, "\n")
	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			rows = append(rows, "")
			continue
		}
		for _, row := range strings.Split(ansi.Wrap(strings.TrimRight(line, " "), budget, ""), "\n") {
			rows = append(rows, margin+row)
		}
	}
	return strings.Join(rows, "\n")
}

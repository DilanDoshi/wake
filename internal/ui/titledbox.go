package ui

// A bordered box with a label set into an edge, which two surfaces draw: the
// composer names the pane in its top edge, and an answerable card names the
// agent in its top and its keys in its bottom.
//
// One implementation rather than two, because a second box-drawer beside this
// is what the non-negotiables refuse - and because the two would drift on the
// detail that is easy to get wrong: a label wider than the edge it sits in
// eats its own frame, which draws a box with no corner.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// titledEdge draws one horizontal border edge with a label set into it.
//
// lead is how many columns of border sit before the label, which is the whole
// of what makes the composer's title right-aligned and a card's left-aligned.
// The label is dropped when the edge cannot hold it with a segment of border
// either side - a title that eats its own frame is worse than no title.
func titledEdge(left, fill, right, label string, edge, lead int, rule, text lipgloss.Style) string {
	if label != "" && edge > titleMinBorder {
		label = " " + ansi.Truncate(label, edge-titleMinBorder, ellipsis) + " "
	} else {
		label = ""
	}
	lead = clamp(lead, 0, max(edge-lipgloss.Width(label), 0))
	tail := max(edge-lead-lipgloss.Width(label), 0)
	return rule.Render(left+strings.Repeat(fill, lead)) +
		text.Render(label) +
		rule.Render(strings.Repeat(fill, tail)+right)
}

// titledBox wraps a body in a rounded border carrying a label in each edge.
//
// The two labels are why this exists rather than lipgloss's own border: a
// style renders the sides and the corners and cannot carry text in either
// edge, so both are drawn here as three pieces and the label keeps its own
// colour. Costing the box two rows and giving back the two the labels used to
// occupy inside it, a framed card is exactly as tall as an unframed one was.
func titledBox(body string, width int, style lipgloss.Style, top, bottom string, topStyle, bottomStyle lipgloss.Style) string {
	edge := max(width-boxFrameWidth, 0)
	// MaxWidth as well as Width, because a body is arbitrary text an agent
	// wrote and one row wider than the frame draws a ragged box: glamour pads
	// to its own minimum at narrow widths and returns a row wider than the
	// width it was given. Width lays the rows out; MaxWidth is what makes the
	// frame's own claim true.
	inner := style.BorderTop(false).BorderBottom(false).Width(edge).MaxWidth(edge).Render(body)
	b := lipgloss.RoundedBorder()
	rule := lipgloss.NewStyle().Foreground(style.GetBorderTopForeground())
	return lipgloss.JoinVertical(lipgloss.Left,
		titledEdge(b.TopLeft, b.Top, b.TopRight, top, edge, titleInset, rule, topStyle),
		inner,
		titledEdge(b.BottomLeft, b.Bottom, b.BottomRight, bottom, edge, titleInset, rule, bottomStyle),
	)
}

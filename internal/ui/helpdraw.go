package ui

// The /wake-help view drawn: the pane's title, a filter line, the rows that fit
// from the scroll offset, and the key line, in exactly the pane's height and
// width so the column keeps its place in the frame and never scrolls the alt
// screen. A row is two columns - what to press, what it does - and wraps rather
// than cuts, so a narrow pane loses no word; narrower than two columns can hold,
// it stacks them.

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	helpTitleSuffix = " › " + wakeHelpCommand

	// helpIndent is the room a row leaves at its left, helpMargin at its right so a
	// full line does not touch the divider, and helpGap what sits between its
	// columns. helpMinText is the least a text column may be before the two stack,
	// and helpMinLead the least the lead column is offered at any width.
	helpIndent  = 2
	helpMargin  = 1
	helpGap     = 2
	helpMinText = 16
	helpMinLead = 8

	// helpNoMatch is said for a query nothing holds.
	helpNoMatch = "nothing matches "
)

var helpHeadStyle = lipgloss.NewStyle().Foreground(Accent).Bold(true)

// helpBody is the laid-out rows for a pane w cells wide and h rows tall, how
// many of them the body draws at once, and the furthest it scrolls.
func (a App) helpBody(w, h int) (rows []string, room, limit int) {
	rows = a.helpRows(w)
	room = max(h-helpBodyTop-helpFootRows, 1)
	return rows, room, max(len(rows)-room, 0)
}

// helpRows is the rows at a width, from the view's own layout when it was made
// for this width and query, and laid out fresh when it was not.
func (a App) helpRows(w int) []string {
	if v := a.help.view; v.laid.ok && v.laid.width == w && v.laid.query == v.Query {
		return v.laid.rows
	}
	return helpLines(helpFilter(helpSections(a.help.shell), a.help.view.Query), w)
}

// helpPane is the view in the pane it belongs to.
func (a App) helpPane(width, height int) string {
	v := a.help.view
	head := a.viewTitle(v.Pane, helpTitleSuffix, width)
	if height <= workflowTitleRows {
		return firstRows(head, height)
	}
	rows, room, limit := a.helpBody(width, height)
	body := windowRows(rows, min(v.Scroll, limit), room)
	if len(rows) == 0 {
		body = []string{HintStyle.Render(helpNoMatch + strconv.Quote(v.Query))}
	}
	filter := HintStyle.Render("filter: ") + TextStyle.Render(v.Query) + saveCursor
	block := stacked([]string{filter}, body, []string{HintStyle.Render(helpKeys)}, height-workflowTitleRows)
	return head + "\n" + fitBlock(block, width)
}

// helpLines lays sections out at a width: a heading, its rows, a blank row
// between sections.
func helpLines(sections []helpSection, width int) []string {
	var out []string
	for i, s := range sections {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, helpHeadStyle.Render(s.title))
		lead := helpLeadWidth(s.rows, width)
		for _, r := range s.rows {
			out = append(out, helpRowLines(r, lead, width)...)
		}
	}
	return out
}

// helpLeadWidth is the lead column a section's rows share: its widest, held to a
// third of the pane so a long verb wraps instead of starving its sentence.
func helpLeadWidth(rows []helpRow, width int) int {
	widest := 0
	for _, r := range rows {
		if !r.sub {
			widest = max(widest, ansi.StringWidth(r.lead))
		}
	}
	return min(widest, max(width/3, helpMinLead))
}

// helpRowLines is one row at a width: two columns where the text keeps helpMinText,
// the lead over the text where it does not.
func helpRowLines(r helpRow, lead, width int) []string {
	indent := strings.Repeat(" ", helpIndent)
	if r.sub {
		var out []string
		for _, l := range helpWrapped(r.text, width-helpIndent-helpMargin) {
			out = append(out, indent+HintStyle.Render(l))
		}
		return out
	}
	textW := width - helpIndent - lead - helpGap - helpMargin
	if textW < helpMinText {
		var out []string
		for _, l := range helpWrapped(r.lead, width-helpIndent-helpMargin) {
			out = append(out, indent+AccentStyle.Render(l))
		}
		for _, l := range helpWrapped(r.text, width-helpIndent-helpIndent-helpMargin) {
			out = append(out, indent+indent+TextStyle.Render(l))
		}
		return out
	}
	leads, texts := helpWrapped(r.lead, lead), helpWrapped(r.text, textW)
	var out []string
	for i := range max(len(leads), len(texts)) {
		l, t := helpCell(leads, i), helpCell(texts, i)
		out = append(out, indent+AccentStyle.Render(l+strings.Repeat(" ", max(lead-ansi.StringWidth(l), 0)))+
			strings.Repeat(" ", helpGap)+TextStyle.Render(t))
	}
	return out
}

// helpWrapped is s broken into lines of at most w cells, a word at a time and a
// long word by force; nothing at all is no line.
func helpWrapped(s string, w int) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(ansi.Wrap(s, max(w, 1), ""), "\n")
}

func helpCell(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

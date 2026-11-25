package ui

// Sections in the roster: each team is a box (owner's 2026-11-19 pick), its header
// the top edge, its members between walls and a closing row under the last; and the
// row-counting that keeps window, At and View agreeing about where they fall -
// fable's "one counting function", because a header is a row that belongs to no
// agent and a count that is off by one lands every click a header lower than the
// pointer.
//
// **A header is attached to its agent, not a row of its own.** The first draft
// made a header a separate item, and the window's accumulation could then keep a
// header while dropping its section's first member (dangling at the bottom) or
// keep the members while dropping the header above them (at the top). Gluing the
// header to the agent below it makes header-and-first-member one atomic block, so
// the window includes or excludes them together - the bug two reviewers found at
// the scroll boundary a fleet of 15-30 is normally sitting at.
//
// The header is derived from the ordered agents rather than carried: the app
// hands the roster its agents already in section order (Fleet.sectioned), which
// also blanks a top-block agent's team, so a header goes before an agent whose
// team differs from the one above it and never inside the header-less top block.
// The agents with no team are a box too, headed unteamedLabel (the owner's
// 2026-11-25 pick), a header that picks nothing: it is no channel.
//
// **`all` is the first item and scrolls like any other** (owner's 2026-11-25 ruling:
// it was pinned over the list before). It belongs to no agent, so it is the one item
// without one; it is a row, never a box.
//
// A blank row sets a header off from whatever is above it (rosterItem.gap), and it
// rides on the same item for the same reason the header does: the window keeps or
// drops header, gap and member together, and one count (rowsForRow) serves them.
// The closing row rides on the section's last item the same way, so a window cuts
// between items and draws a cut box as walls with no edge: it runs on past the cut.

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// sectionRule is the divider fill a team header is drawn on.
const sectionRule = "─"

// boxInset is the columns a member's row gives up on each side: a wall and a space.
const boxInset = 2

// unteamedLabel heads the box of the agents that are in no team. A space keeps it
// from ever being a team's name, which is one token.
const unteamedLabel = "no team"

// boxBorder is the corners and walls of a section's box, the composer's.
var boxBorder = lipgloss.RoundedBorder()

// rosterItem is one agent's row block, with the team header drawn above it when
// it is the first of its section, and a blank row above that when anything is
// above it. One item per agent, so the window counts whole blocks and neither can
// be split from its member - and the all row, item 0, which has no agent.
type rosterItem struct {
	agent  Agent
	header string // header drawn above this agent: its team's, or unteamedLabel; "" for none
	gap    bool   // a blank row above the header, setting the section off from the rows above it
	footer bool   // the box's closing row under this agent, the last of its section
	all    bool   // the all channel's own row, item 0: it belongs to no agent
}

func (item rosterItem) hasHeader() bool { return item.header != "" }

// teamHeader is whether the header is a team's, a channel; unteamedLabel's is not.
func (item rosterItem) teamHeader() bool { return item.hasHeader() && item.agent.Team != "" }

// lead is the rows an item draws above its agent's own: the gap, then the header.
// They belong to no agent: a click on the header picks its channel, and one on
// the gap nothing.
func (item rosterItem) lead() int {
	n := 0
	if item.gap {
		n++
	}
	if item.hasHeader() {
		n++
	}
	return n
}

// trail is the rows an item draws under its agent's own: the closing row of a
// section's last member. Like the lead, it belongs to no agent.
func (item rosterItem) trail() int {
	if item.footer {
		return 1
	}
	return 0
}

// sectionRows is the all item, then one item per agent: a header on the first agent
// of each team, the boundary being a team change in the ordered slice - which
// Fleet.sectioned guarantees is grouped and top-block-blanked - and on the first of
// the agents with no team, and a closing row on the last of each. A header with
// nothing above it but all, or any row above it - a roster that opens on a team, or
// the no-team box right under all - needs no gap to set it off.
func sectionRows(agents []Agent) []rosterItem {
	rows := make([]rosterItem, 1, len(agents)+1)
	rows[0] = rosterItem{all: true}
	for i, a := range agents {
		hdr := ""
		switch {
		case a.Team == "" && i == 0:
			hdr = unteamedLabel
		case a.Team != "" && (i == 0 || agents[i-1].Team != a.Team):
			hdr = a.Team
		}
		last := i == len(agents)-1 || agents[i+1].Team != a.Team
		rows = append(rows, rosterItem{agent: a, header: hdr, gap: hdr != "" && i > 0, footer: last})
	}
	return rows
}

// rowsForRow is how many lines an item draws, the one number window and At both
// count by: the agent's own rows (rowsFor), plus the lines above them (lead) and
// the one under (trail).
func rowsForRow(item rosterItem, subs subsOf) int {
	if item.all {
		return 1
	}
	return item.lead() + rowsFor(item.agent, subsFor(subs, item.agent.ID)) + item.trail()
}

// rowLines is the strings an item draws: the gap and the header's top edge (when it
// has them), the agent's own rows between the box's walls, and its closing row.
func (r Roster) rowLines(item rosterItem, subs subsOf, width int) []string {
	if item.all {
		return []string{r.allLine(width)}
	}
	var lines []string
	if item.gap {
		lines = append(lines, "")
	}
	if item.hasHeader() {
		lines = append(lines, r.headerLine(item.header, width))
	}
	channel := item.agent.Team // the box the agent stands in, and its state: a team's, or the unteamed box's, never lit
	if channel == "" {
		channel = unteamedLabel
	}
	rule, _ := r.channelStyles(channel)
	left, right := rule.Render(boxBorder.Left+" "), rule.Render(" "+boxBorder.Right)
	inner := max(width-2*boxInset, 0)
	for _, line := range r.rows(item.agent, subsFor(subs, item.agent.ID), inner) {
		pad := strings.Repeat(" ", max(inner-ansi.StringWidth(line), 0))
		lines = append(lines, boxFit(left+line+pad+right, width))
	}
	if item.footer {
		corners := ansi.StringWidth(boxBorder.BottomLeft + boxBorder.BottomRight)
		edge := boxBorder.BottomLeft + strings.Repeat(boxBorder.Bottom, max(width-corners, 0)) + boxBorder.BottomRight
		lines = append(lines, boxFit(rule.Render(edge), width))
	}
	return lines
}

// boxFit cuts a line to a column too narrow for its walls. A column that holds them
// needs no cut: rows are cut and padded to the room inside, so the line is exactly
// the column, and a lipgloss pass over every line is per-draw cost for nothing.
func boxFit(line string, width int) string {
	if width < 2*boxInset {
		return clip(line, width)
	}
	return line
}

// channelStops is the sidebar's walk: walkable with the channel headers it draws,
// all first and each team's header before its first member. The no-team header is no
// channel, so it is no stop.
func channelStops(agents []Agent, subs subsOf) []Roster {
	var out []Roster
	for _, item := range sectionRows(agents) {
		switch {
		case item.all:
			out = append(out, Roster{Header: core.BroadcastName})
			continue
		case item.teamHeader():
			out = append(out, Roster{Header: item.header})
		}
		out = append(out, walkable([]Agent{item.agent}, subs)...)
	}
	return out
}

// cursorRow is the item the cursor sits on - all's, an agent's, or the one a team
// header is drawn above - or -1. window anchors its scroll on it.
func (r Roster) cursorRow(rows []rosterItem) int {
	for i, item := range rows {
		switch {
		case r.Header == core.BroadcastName && item.all:
			return i
		case r.Header != "" && item.teamHeader() && item.header == r.Header:
			return i
		case r.Selected != "" && item.agent.ID == r.Selected:
			return i
		}
	}
	return -1
}

// centredEdge is a rule `width` wide with the label in its middle, between left
// and right (a box's corners, or nothing), the rule drawn in the rule style and the
// name in the text one. Drawn through titledEdge (the composer's and a card's own
// rule drawer, so no second one), which truncates a name wider than the edge with
// an ellipsis and drops it when the edge cannot hold it with border either side.
// oneLine first, defensively: a team tag restored from an edited park book could
// carry a newline or control sequence, and a header must stay one row or
// rowsForRow miscounts and every click below it shifts.
func centredEdge(left, right, label string, width int, rule, text lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	label = oneLine(label)
	edge := max(width-ansi.StringWidth(left+right), 0)
	lead := max(0, (edge-(ansi.StringWidth(label)+2))/2)
	return clip(titledEdge(left, sectionRule, right, label, edge, lead, rule, text), width)
}

// teamHeaderLine is the board's section divider: `──── backend ────`.
func teamHeaderLine(team string, width int, rule, text lipgloss.Style) string {
	return centredEdge("", "", team, width, rule, text)
}

// channelStyles is how a channel's box is drawn: lit when it is the picked channel,
// its name bright and bold under the cursor, muted otherwise.
func (r Roster) channelStyles(channel string) (rule, text lipgloss.Style) {
	picked := channel == r.channel || channel == core.BroadcastName && r.channel == ""
	rule, text = HintStyle, HintStyle
	if picked {
		rule, text = AccentStyle, AccentStyle
	}
	if channel == r.Header {
		if !picked {
			text = TextStyle
		}
		text = text.Bold(true)
	}
	return rule, text
}

// headerLine is a channel header as the roster draws it: the top edge of its box.
func (r Roster) headerLine(header string, width int) string {
	rule, text := r.channelStyles(header)
	return centredEdge(boxBorder.TopLeft, boxBorder.TopRight, header, width, rule, text)
}

// rosterSpot is what a roster row is: an agent's (and a dispatch under it), or a
// channel header's.
type rosterSpot struct {
	agent    Agent
	dispatch string
	header   string
}

// allLine is the all row: a plain rule naming the channel, and how many rows the
// roster holds when it says (`all · 22`; Roster.total).
func (r Roster) allLine(width int) string {
	label := core.BroadcastName
	if r.total > 0 {
		label += " · " + strconv.Itoa(r.total)
	}
	rule, text := r.channelStyles(core.BroadcastName)
	return centredEdge("", "", label, width, rule, text)
}

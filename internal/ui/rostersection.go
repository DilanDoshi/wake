package ui

// Section headers in the roster: the team dividers, and the row-counting that
// keeps window, At and View agreeing about where they fall - fable's "one
// counting function", because a header is a row that belongs to no agent and a
// count that is off by one lands every click a header lower than the pointer.
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
// With every Team empty no header is drawn and the roster is the flat list it
// always was - the property that keeps every existing test green.
//
// A blank row sets a header off from whatever is above it (rosterItem.gap), and it
// rides on the same item for the same reason the header does: the window keeps or
// drops header, gap and member together, and one count (rowsForRow) serves them.

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// sectionRule is the divider fill a team header is drawn on.
const sectionRule = "─"

// rosterItem is one agent's row block, with the team header drawn above it when
// it is the first of its section, and a blank row above that when anything is
// above it. One item per agent, so the window counts whole blocks and neither can
// be split from its member.
type rosterItem struct {
	agent  Agent
	header string // team header drawn above this agent, "" for none
	gap    bool   // a blank row above the header, setting the section off from the rows above it
}

func (item rosterItem) hasHeader() bool { return item.header != "" }

// lead is the rows an item draws above its agent's own: the gap, then the header.
// They belong to no agent, so a click on them opens nothing.
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

// sectionRows attaches a header to the first agent of each team, the boundary
// being a team change in the ordered slice - which Fleet.sectioned guarantees is
// grouped and top-block-blanked. One item per agent. A header with nothing above
// it - a roster that opens on a team - needs no gap to set it off.
func sectionRows(agents []Agent) []rosterItem {
	rows := make([]rosterItem, len(agents))
	for i, a := range agents {
		hdr := ""
		if a.Team != "" && (i == 0 || agents[i-1].Team != a.Team) {
			hdr = a.Team
		}
		rows[i] = rosterItem{agent: a, header: hdr, gap: hdr != "" && i > 0}
	}
	return rows
}

// rowsForRow is how many lines an item draws, the one number window and At both
// count by: the agent's own rows (rowsFor), plus the lines above them (lead).
func rowsForRow(item rosterItem, subs subsOf) int {
	return item.lead() + rowsFor(item.agent, subsFor(subs, item.agent.ID))
}

// rowLines is the strings an item draws: the gap and the header rule (when it has
// them), then the agent's own rows.
func (r Roster) rowLines(item rosterItem, subs subsOf, width int) []string {
	var lines []string
	if item.gap {
		lines = append(lines, "")
	}
	if item.hasHeader() {
		lines = append(lines, r.headerLine(item.header, width))
	}
	return append(lines, r.rows(item.agent, subsFor(subs, item.agent.ID), width)...)
}

// channelStops is the sidebar's walk: walkable with the channel headers it draws,
// the pinned all first and each team's header before its first member.
func channelStops(agents []Agent, subs subsOf) []Roster {
	out := []Roster{{Header: core.BroadcastName}}
	for _, item := range sectionRows(agents) {
		if item.hasHeader() {
			out = append(out, Roster{Header: item.header})
		}
		out = append(out, walkable([]Agent{item.agent}, subs)...)
	}
	return out
}

// cursorRow is the item the cursor sits on - an agent's, or the one a team header
// is drawn above - or -1. window anchors its scroll on it.
func (r Roster) cursorRow(rows []rosterItem) int {
	for i, item := range rows {
		if r.Header != "" && item.header == r.Header || r.Selected != "" && item.agent.ID == r.Selected {
			return i
		}
	}
	return -1
}

// teamHeaderLine is a section divider: `──── backend ────`, the team name
// centered in a rule drawn in the rule style and the name in the text one. Drawn
// through titledEdge (the composer's and a card's own rule drawer, so no second
// one), which truncates a name wider than the column with an ellipsis and drops
// it when the column cannot hold it with border either side. oneLine first,
// defensively: a team tag restored from an edited park book could carry a
// newline or control sequence, and a header must stay one row or rowsForRow
// miscounts and every click below it shifts.
func teamHeaderLine(team string, width int, rule, text lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	team = oneLine(team)
	lead := max(0, (width-(ansi.StringWidth(team)+2))/2)
	return clip(titledEdge("", sectionRule, "", team, width, lead, rule, text), width)
}

// headerLine is a channel header as the roster draws it: lit when it is the
// picked channel, its name bright and bold under the cursor, muted otherwise.
func (r Roster) headerLine(header string, width int) string {
	picked := header == r.channel || header == core.BroadcastName && r.channel == ""
	rule, text := HintStyle, HintStyle
	if picked {
		rule, text = AccentStyle, AccentStyle
	}
	if header == r.Header {
		if !picked {
			text = TextStyle
		}
		text = text.Bold(true)
	}
	return teamHeaderLine(header, width, rule, text)
}

// allRows is the pinned `── all ──` row every column opens on.
const allRows = 1

// rosterSpot is what a roster row is: an agent's (and a dispatch under it), or a
// channel header's.
type rosterSpot struct {
	agent    Agent
	dispatch string
	header   string
}

// Column is the sidebar: the pinned all over the agents View lays out below it,
// with the picked channel ("" for all) marked. Hit counts its rows the same way.
func (r Roster) Column(agents []Agent, subs subsOf, width, height int, channel string) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	r.channel = channel
	all := r.headerLine(core.BroadcastName, width)
	if height <= allRows {
		return all
	}
	return all + "\n" + r.View(agents, subs, width, height-allRows)
}

// Hit is the row under line y of Column: the all header, or what At finds in the
// agents below it.
func (r Roster) Hit(agents []Agent, subs subsOf, width, height, y int) (rosterSpot, bool) {
	if width <= 0 || y < 0 || y >= height {
		return rosterSpot{}, false
	}
	if y < allRows {
		return rosterSpot{header: core.BroadcastName}, true
	}
	return r.At(agents, subs, width, height-allRows, y-allRows)
}

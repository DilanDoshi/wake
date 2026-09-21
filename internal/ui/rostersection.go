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

import "github.com/charmbracelet/x/ansi"

// sectionRule is the divider fill a team header is drawn on.
const sectionRule = "─"

// rosterItem is one agent's row block, with the team header drawn above it when
// it is the first of its section. One item per agent, so the window counts whole
// blocks and a header can never be split from its member.
type rosterItem struct {
	agent  Agent
	header string // team header drawn above this agent, "" for none
}

func (item rosterItem) hasHeader() bool { return item.header != "" }

// sectionRows attaches a header to the first agent of each team, the boundary
// being a team change in the ordered slice - which Fleet.sectioned guarantees is
// grouped and top-block-blanked. One item per agent.
func sectionRows(agents []Agent) []rosterItem {
	rows := make([]rosterItem, len(agents))
	for i, a := range agents {
		hdr := ""
		if a.Team != "" && (i == 0 || agents[i-1].Team != a.Team) {
			hdr = a.Team
		}
		rows[i] = rosterItem{agent: a, header: hdr}
	}
	return rows
}

// rowsForRow is how many lines an item draws, the one number window and At both
// count by: the agent's own rows (rowsFor), plus the header line above it.
func rowsForRow(item rosterItem, subs subsOf) int {
	n := rowsFor(item.agent, subsFor(subs, item.agent.ID))
	if item.hasHeader() {
		n++
	}
	return n
}

// rowLines is the strings an item draws: the header rule (if any) then the
// agent's own rows.
func (r Roster) rowLines(item rosterItem, subs subsOf, width int) []string {
	var lines []string
	if item.hasHeader() {
		lines = append(lines, teamHeaderLine(item.header, width))
	}
	return append(lines, r.rows(item.agent, subsFor(subs, item.agent.ID), width)...)
}

// rowIndexOfSelected is the item the cursor sits on - one per agent - or -1.
// window anchors its scroll on it.
func rowIndexOfSelected(rows []rosterItem, id string) int {
	if id == "" {
		return -1
	}
	for i, item := range rows {
		if item.agent.ID == id {
			return i
		}
	}
	return -1
}

// teamHeaderLine is a section divider: `──── backend ────`, the team name
// centered in a muted rule. Drawn through titledEdge (the composer's and a card's
// own rule drawer, so no second one), which truncates a name wider than the
// column with an ellipsis and drops it when the column cannot hold it with border
// either side. oneLine first, defensively: a team tag restored from an edited
// park book could carry a newline or control sequence, and a header must stay one
// row or rowsForRow miscounts and every click below it shifts.
func teamHeaderLine(team string, width int) string {
	if width <= 0 {
		return ""
	}
	team = oneLine(team)
	lead := max(0, (width-(ansi.StringWidth(team)+2))/2)
	return clip(titledEdge("", sectionRule, "", team, width, lead, HintStyle, HintStyle), width)
}

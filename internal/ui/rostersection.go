package ui

// Section headers in the roster: the team dividers, and the row-counting that
// keeps window, At and View agreeing about where they fall - fable's "one
// counting function", because a header is a row that belongs to no agent and a
// count that is off by one lands every click a header lower than the pointer.
//
// The rows are derived from the ordered agents rather than carried, so a fleet
// with no team draws exactly as before - no header row - and the sections appear
// only once a /team groups the fleet. The app hands the roster its agents already
// in section order (Fleet.sectioned), so a header goes before an agent whose team
// differs from the one above it.

import "github.com/charmbracelet/x/ansi"

// sectionRule is the divider fill a team header is drawn on.
const sectionRule = "─"

// rosterItem is one drawn row: an agent, or a team's section header.
type rosterItem struct {
	agent  Agent
	header string // "" for an agent row; the team name for a header row
}

func (row rosterItem) isHeader() bool { return row.header != "" }

// sectionRows interleaves a header before the first agent of each team. The
// boundary is a team change in the ordered slice, so it relies on the agents
// being grouped, which Fleet.sectioned guarantees. With every Team empty no
// header is emitted and the result is the flat list the roster always drew.
func sectionRows(agents []Agent) []rosterItem {
	rows := make([]rosterItem, 0, len(agents))
	for i, a := range agents {
		if a.Team != "" && (i == 0 || agents[i-1].Team != a.Team) {
			rows = append(rows, rosterItem{header: a.Team})
		}
		rows = append(rows, rosterItem{agent: a})
	}
	return rows
}

// rowsForRow is how many lines a row draws, the one number window and At both
// count by: a header is one line, an agent is rowsFor's own answer.
func rowsForRow(row rosterItem, subs subsOf) int {
	if row.isHeader() {
		return 1
	}
	return rowsFor(row.agent, subsFor(subs, row.agent.ID))
}

// rowLines is the strings a row draws: the header rule, or the agent's own rows.
func (r Roster) rowLines(row rosterItem, subs subsOf, width int) []string {
	if row.isHeader() {
		return []string{teamHeaderLine(row.header, width)}
	}
	return r.rows(row.agent, subsFor(subs, row.agent.ID), width)
}

// rowIndexOfSelected is the row the cursor sits on - an agent row, never a header
// - or -1. window anchors its scroll on it.
func rowIndexOfSelected(rows []rosterItem, id string) int {
	if id == "" {
		return -1
	}
	for i, row := range rows {
		if !row.isHeader() && row.agent.ID == id {
			return i
		}
	}
	return -1
}

// teamHeaderLine is a section divider: `──── backend ────`, the team name
// centered in a muted rule. Drawn through titledEdge (the composer's and a card's
// own rule drawer, so no second one), which truncates a name wider than the
// column with an ellipsis and drops it when the column cannot hold it with border
// either side. No member count in v1 - see the teams spec.
func teamHeaderLine(team string, width int) string {
	if width <= 0 {
		return ""
	}
	lead := max(0, (width-(ansi.StringWidth(team)+2))/2)
	return clip(titledEdge("", sectionRule, "", team, width, lead, HintStyle, HintStyle), width)
}

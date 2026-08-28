package ui

// The subagent rows under an agent in the right sidebar.
//
// The founding message asked for "the list of all your sub agents ...
// categorized by group chat or just list all so you can toggle into them to
// manage them directly there". This is that list, drawn where the operator is
// already looking to see who needs them rather than as a surface of its own.
//
// # Running only, and why that is what bounds the column
//
// Fleet.RunningTasks is the filter and its header carries the argument. The
// consequence for this file is the one that matters: a sidebar drawing every
// dispatch a session ever made grows without limit next to thirty agents, and
// the roster is the surface whose whole job is to be scanned. Running
// dispatches are few and clear themselves.
//
// # The budget, in the order it is spent
//
// The type first, then the count if what is left holds it whole. That is the
// opposite order to headLine's unread badge, and deliberate: the badge is why a
// row is worth looking at, where a subagent's type is the row's whole identity
// and its token count is something to know beside it. A type cut to make room
// for a figure buys the least urgent thing on the row with the only thing on it
// that names anything.
//
// Dropped rather than abbreviated for the reason `⌃Q` reads `quit & park all`
// and never `quit & park`: a partial `↓ 1` is a *different number*, and a wrong
// figure on screen is worse than no figure.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// subsOf answers an agent's running dispatches, and nil for an agent with none.
//
// A lookup passed in rather than a field on Roster, because Move and Next both
// return fresh Roster literals - a field would be silently dropped by every one
// of them, which is the "declared, read, never assigned" defect this project
// has already paid for twice.
type subsOf func(sessionID string) []Task

// subGlyph marks a row as belonging to the one above it. render.ToolCall's own
// continuation character, so the sidebar has one vocabulary for "this is under
// that" rather than two.
const subGlyph = "⎿"

// subagentRow is one running dispatch: what kind of agent it is, and what it
// has spent if the column can hold the figure whole.
func subagentRow(t Task, width int) string {
	head := strings.Repeat(" ", toolIndent) + subGlyph + " " + subagentName(t)
	count := tokenArrow + " " + humanTokens(t.Tokens)
	if t.Tokens <= 0 || lipgloss.Width(head)+1+lipgloss.Width(count) > width {
		return clip(head, width)
	}
	return clip(head+" "+count, width)
}

// subagentName is what the row calls a dispatch: its subagent_type, which is
// what somebody chose when they dispatched it.
//
// The fallbacks are not decoration. Type is empty on a background shell, on
// task_updated and on task_notification, and is unverified for any type other
// than general-purpose - core.TaskUpdate.Type says so and says a consumer
// tolerates "". So the description stands in, and the pane's own word for a
// dispatch with neither stands in after that - the same three cases in the same
// order the transcript's ending line resolves them (taskline.go).
// Flattened through oneLine for the reason its own header gives: these are
// strings a model wrote, and a newline in one draws a second physical row out
// of a value rowsFor counted as one.
func subagentName(t Task) string {
	switch {
	case t.Type != "":
		return oneLine(t.Type)
	case t.Label != "":
		return oneLine(t.Label)
	case t.Name != "":
		return oneLine(t.Name)
	default:
		return subagentUnnamed
	}
}

// viewingPicked puts a conversation that has just been opened inside the
// subagent the sidebar cursor is on, and leaves it on the conversation itself
// when the cursor is on an agent's own row.
//
// It is the whole of what "toggle into them" costs, because the surface already
// exists: DM.Viewing swaps the pane's transcript onto one dispatch's forwarded
// frames, so the sidebar reaches that transcript rather than a second one.
//
// Guarded on the id, so a cursor left on one agent's subagent cannot put a
// different agent's conversation inside a dispatch it does not have - which is
// reachable, because ⌃D falls back to the first agent in attention order when
// the roster has no selection at all.
// **An empty SelectedTask is an instruction, not a reason to do nothing.** It
// means the cursor is on the agent's own row, and `Viewing("")` is how the
// pane is told to show the conversation itself. Returning early there left a
// pane that had been sent into a subagent stuck in it: walking the cursor back
// up and pressing ⌃D again is what this promises as the way back, and it did
// nothing.
func (a App) viewingPicked(id string) App {
	if a.roster.Selected != id {
		return a
	}
	d, ok := a.dms[id]
	if !ok {
		return a
	}
	return a.withDM(id, d.Viewing(a.roster.SelectedTask))
}

// subsFor is the running dispatches a lookup answers for one agent, and none
// when there is no lookup at all.
//
// The nil case is the fleet that is not dispatching, which is most of it most
// of the time - and every caller that does not care about subagents, which
// includes every test written before this surface existed.
func subsFor(subs subsOf, id string) []Task {
	if subs == nil {
		return nil
	}
	return subs(id)
}

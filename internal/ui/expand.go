package ui

// Expanding a tool result: ⌃E, and what it acts on.
//
// render.ToolResult has taken a `collapsed` argument since it was written and
// nothing ever passed false, so ten lines and an ellipsis was where a long
// result ended for the operator. That is a loss rather than a fold - the pane
// showed less than the agent saw, with no way to ask for the rest - which is
// why this is a key and not a preference.
//
// **Claude Code's key is ⌃O and Wake cannot have it.** ⌃O detaches, and detach
// is the property the background daemon exists to provide. ⌃E shadows the text
// area's "line end" instead, which the End key still reaches - the same trade
// ⌃D, ⌃W, ⌃F and ⌃B already make, and the cheapest one left: every other
// unclaimed byte is either the terminal's flow control or its suspend.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
)

// noExpandTarget is ⌃E with no conversation in front of it. The room draws no
// tool results at all - it carries what is addressed to you, what is blocked,
// and closing words - so there is nothing there to expand. Named rather than
// silent, for noPaneAdvice's reason.
const noExpandTarget = "⌃E expands the tool results in one conversation, and the room shows none. ⌃D opens one"

// toggleExpanded flips whether the focused conversation shows its tool results
// whole, and refuses by name when there is no conversation on the keys.
//
// The focused pane and not the roster's pick, which is what ⌃D, ⌃Y and ⌃B read:
// expanding is about what is being *read*, and the cursor names an agent that
// may have no pane on screen.
func (a App) toggleExpanded() (tea.Model, tea.Cmd, bool) {
	dm, ok := a.dms[a.focus]
	if !ok {
		notice.Report("%s", noExpandTarget)
		return a, nil, true
	}
	return a.withDM(a.focus, dm.toggleExpanded()), nil, true
}

// toggleExpanded flips the flag and re-renders, which is the whole of it: the
// transcript is memoized *lines*, so a flag nothing re-derived would change
// what the next event renders as and leave every line already on screen alone.
//
// Collapsing also clears the runs and results a click opened one at a time, so
// ⌃E means show-everything then hide-everything. A click on a rollup expands one
// run and leaves no rollup line behind to re-fold it - see rollup.go's openRun -
// so ⌃E is the way back, and it would not be if a click-opened run outlived it.
//
// It returns the reader to the newest line, exactly as a width change does and
// for the same reason - the lines a scroll offset points at have renumbered, so
// restoring a stale offset is a worse lie than following the conversation.
func (d DM) toggleExpanded() DM {
	d.expanded = !d.expanded
	if !d.expanded {
		d.runOpen, d.opened = nil, nil
	}
	d.tr = d.tr.replace(renderTranscript(d)).toBottom()
	return d
}

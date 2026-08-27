package ui

// The line a dispatch leaves in the conversation when it ends.
//
//	● Subagent "Counting lines in alpha.txt" finished · 24s
//
// Claude Code draws the same line, and it is the half of a dispatch that
// belongs in the scrollback rather than in the rows beside it: the rows say
// what is happening *now* and are gone when the turn is, while this is a thing
// that happened, at a point, in an order - which is what a transcript is for.
//
// The parent's own dispatch tool call is the start marker, so there is no
// started line: two lines one row apart stating the same fact is the noise
// this whole surface exists to remove.

import (
	"strconv"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	// taskLineGlyph opens the line. Claude Code's own, and filled rather than
	// the transcript's ⏺ so an ending is not mistaken at a glance for another
	// tool call starting.
	taskLineGlyph = "●"

	// What each kind is called. A background shell is not a subagent, which is
	// the whole reason core.TaskKind exists - see its doc comment for what
	// each wrong guess costs.
	//
	// **"Subagent" rather than Claude's own "Agent"**, for two reasons that
	// agree. `Agent` is the wire name of the dispatch tool and policed outside
	// the airlock, so using it here would cost an exemption. And in Wake an
	// *agent* is already a fleet member - the thing `@name` addresses, the
	// thing a roster row is - so a transcript line calling a dispatch an Agent
	// names the one word this product has already spent.
	taskLineAgent = "Subagent"
	taskLineShell = "Shell"

	// The three endings, in Wake's words. "ended" is the honest reading of a
	// status this build does not model: the binary names `failed` and `paused`
	// that no recording carries, so an unmodelled ending is expected traffic,
	// and reporting one as either "finished" or "halted" is a claim no frame
	// made.
	taskLineFinished = "finished"
	taskLineHalted   = "halted"
	taskLineEnded    = "ended"
)

// taskLineStyle is the colour of an ending, and it is Claude's own mapping:
// completed reads as success, anything stopped as a warning, and an ending
// nobody has recorded recedes rather than claiming either.
func taskLineStyle(s core.TaskStatus) lipgloss.Style {
	switch s {
	case core.TaskDone:
		return lipgloss.NewStyle().Foreground(Success)
	case core.TaskStopped:
		return warnStyle
	default:
		return HintStyle
	}
}

// taskLineWord is what the line says happened.
func taskLineWord(s core.TaskStatus) string {
	switch s {
	case core.TaskDone:
		return taskLineFinished
	case core.TaskStopped:
		return taskLineHalted
	default:
		return taskLineEnded
	}
}

// taskLineKind is what the thing that ended is called, and "" for a kind this
// build cannot name - in which case the line is drawn without one rather than
// guessing, because the description alone is still true.
func taskLineKind(k core.TaskKind) string {
	switch k {
	case core.TaskAgent:
		return taskLineAgent
	case core.TaskShell:
		return taskLineShell
	default:
		return ""
	}
}

// taskLine is the block a lifecycle frame produces, and "" for every frame
// that is not an ending naming its dispatch.
//
// **Only the ending that names a dispatch draws.** Both task_updated and
// task_notification report the same ending - the airlock collapses them to one
// phase deliberately - so a line keyed on the phase alone draws two of them one
// row apart. The dispatch id is the discriminator rather than an arbitrary
// choice between the two: this line names a dispatch, and that is the frame
// that can be tied to one. It is also the frame carrying the final usage, so
// the same test picks the only ending that knows how long the work took.
//
// A frame is all it reads. DM.renderAll re-derives every block from its own
// event on a re-wrap, so a line built from a transition the fold remembered
// would either vanish or double at the next width change - the failure class
// lastread.go is an essay about.
func taskLine(u *core.TaskUpdate, width int) string {
	if u == nil || u.Phase != core.TaskEnded || u.Dispatch == "" {
		return ""
	}
	line := taskLineGlyph + " " + taskLineSubject(u) + " " + taskLineWord(u.Status)
	if u.Elapsed > 0 {
		line += metaSep + elapsedText(u.Elapsed)
	}
	return taskLineStyle(u.Status).MaxWidth(width).Render(line)
}

// taskLineSubject is what ended: what it is called and what it was doing.
// Either half can be missing - a shell carries no type, and no frame is
// guaranteed to describe a subagent type nothing has recorded - so the line
// degrades to whichever half it has rather than drawing an empty pair of
// quotes.
func taskLineSubject(u *core.TaskUpdate) string {
	kind := taskLineKind(u.Kind)
	switch {
	case kind != "" && u.Label != "":
		return kind + " " + strconv.Quote(u.Label)
	case u.Label != "":
		return strconv.Quote(u.Label)
	case kind != "":
		return kind
	default:
		return subagentUnnamed
	}
}

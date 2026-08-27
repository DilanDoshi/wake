package ui

// The list under a conversation: what it has dispatched, and the row that
// leads back to the conversation itself.
//
// Claude Code draws the same thing in its footer -
// `general-purpose  Updating pr.md  56m 26s · ↓ 488.7k tokens` - and this is
// the surface that makes a subagent's work reachable now that it no longer
// lands in the transcript. Without it the dispatches are held and invisible,
// which is worse than the noise it replaces.
//
// It is chrome, so every row it draws comes out of the transcript's height:
// see DM.chromeHeight, which asks taskRowCount rather than measuring this.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// taskIndent sets a dispatch's row under the conversation's, so the list
	// reads as one thing with things beneath it rather than as four peers.
	taskIndent = "  "

	// taskGap separates a row's columns. Two spaces, the status bar's
	// separator, for the same reason: this line is read at a glance and a
	// glyph between the columns is one more thing on it.
	taskGap = "  "

	// The two marks in the first column, and they are two facts rather than
	// one: openMark says which transcript is on screen, cursorMark says where
	// the keys are. They are usually the same row - ↵ opens what the cursor is
	// on - and they differ exactly while somebody is choosing what to open next
	// from inside what they opened last.
	//
	// Marks rather than colour alone, both of them. A cursor drawn only in the
	// accent is invisible to a terminal that drops styling, and invisible to
	// every test that reads the screen as characters.
	openMark   = "▸"
	cursorMark = "›"

	// bothMark is the row that is open *and* cursored, which is where the
	// cursor sits for as long as nobody walks away from what they opened - so
	// it is the common case rather than a corner, and it needs a glyph of its
	// own. Drawn as openMark it made walking onto the open row invisible.
	bothMark = "»"

	// noMark is what every other row carries, so the names stay in one column.
	noMark = " "

	// minMetaWidth is the narrowest a row can be and still keep its meta. Below
	// it the description is what survives: it says what is happening, where the
	// meta says only how long it has been happening.
	minMetaWidth = 44
)

// taskStatusGlyph is one glyph per status and one status per glyph, the rule
// stateGlyph keeps for the roster and for the same reason: a glyph shared
// between two statuses is a row reporting a halted subagent as a finished one.
// TestEveryTaskStatusHasItsOwnGlyph holds both directions.
//
// The glyphs are the roster's where the meaning is the roster's - a working
// task and a working agent are the same idea - and the transcript's ⊘ for an
// ending nobody asked for, which is what it already marks there.
var taskStatusGlyph = map[core.TaskStatus]string{
	core.TaskRunning: stateGlyph[rpc.StateWorking],
	core.TaskDone:    stateGlyph[rpc.StateEnded],
	core.TaskStopped: "⊘",
}

// taskGlyph is the glyph for a status, and unknownGlyph for one this build
// does not model - drawn rather than blanked, exactly as an unknown agent
// state is.
func taskGlyph(s core.TaskStatus) string {
	if g, ok := taskStatusGlyph[s]; ok {
		return g
	}
	return unknownGlyph
}

// taskRowCount is how many rows taskView draws, without drawing them.
//
// SetSize asks this on every re-lay through chromeHeight, and drawing a row
// costs a truncation per column - so the count is derived from the list rather
// than from the rendered block. The two are held together by
// TestTheRowCountMatchesWhatIsDrawn, because a count that drifts from the draw
// is a pane one row too tall, which scrolls the alt screen away on every frame.
func (d DM) taskRowCount() int {
	if len(d.tasks.Rows()) == 0 {
		return 0
	}
	return len(d.tasks.Rows()) + 1
}

// taskView draws the list at a width, and "" for a conversation that has
// dispatched nothing - which is most of them, and a row saying so is chrome
// nobody asked for.
func (d DM) taskView(width int) string {
	rows := d.tasks.Rows()
	if len(rows) == 0 {
		return ""
	}
	lines := make([]string, 0, len(rows)+1)
	lines = append(lines, d.conversationRow(width))
	for i, row := range rows {
		lines = append(lines, d.dispatchRow(row, i+2, width))
	}
	return strings.Join(lines, "\n")
}

// conversationRow is the row that leads back, and the row that says the reader
// is here. It wears the agent's own state glyph rather than a second
// vocabulary for the same fact - see stateGlyph.
func (d DM) conversationRow(width int) string {
	line := mark(d.viewing == "", d.cursorOn(1)) + glyphOf(d.Agent.State) + " " + d.Name
	return rowStyle(d.cursorOn(1)).Render(ansi.Truncate(line, width, ellipsis))
}

// dispatchRow is one dispatch: what it is, what it is doing now, and what it
// has spent.
//
// The meta is dropped whole rather than cut, and before the description is
// touched. A row too narrow for both keeps what is happening; how long it has
// been happening is the part a reader can do without.
func (d DM) dispatchRow(t Task, row, width int) string {
	open := d.viewing != "" && d.viewing == t.Dispatch
	style := rowStyle(d.cursorOn(row))
	head := mark(open, d.cursorOn(row)) + taskIndent + taskGlyph(t.Status) + " " + t.name()

	meta := taskMeta(t)
	room := width - lipgloss.Width(head) - lipgloss.Width(taskGap) - lipgloss.Width(meta)
	if meta == "" || width < minMetaWidth || room < 1 {
		return style.Render(ansi.Truncate(head, width, ellipsis))
	}
	line := head + taskGap + strings.Repeat(" ", room) + meta
	return style.Render(ansi.Truncate(line, width, ellipsis))
}

// name is what a row calls a dispatch: its type and what it is doing.
//
// Both, because they answer different questions - which agent this is, and
// what it is up to - and Claude Code's own row carries the pair. Either may be
// absent: a shell has no type, and no frame is guaranteed to carry a
// description for a subagent type nothing has recorded.
func (t Task) name() string {
	switch {
	case t.Type != "" && t.Label != "":
		return oneLine(t.Type + taskGap + t.Label)
	case t.Label != "":
		return oneLine(t.Label)
	case t.Type != "":
		return oneLine(t.Type)
	default:
		return subagentUnnamed
	}
}

// taskMeta is the elapsed time and the token count in the heartbeat's own
// spelling - `4s · ↓ 27.0k tokens`, which is Claude's, already measured and
// already formatted by elapsedText and tokenText. One formatter for this in
// the tree, not two.
//
// tokenText carries its own leading separator, because in the heartbeat it
// always follows a word. Here it can be the only clause, so the separator is
// stripped in that one case rather than the formatter being duplicated.
//
// Empty when nothing has been reported, so a dispatch that has only just
// started does not claim to have spent zero of anything.
func taskMeta(t Task) string {
	switch {
	case t.Elapsed > 0 && t.Tokens > 0:
		return elapsedText(t.Elapsed) + tokenText(t.Tokens)
	case t.Elapsed > 0:
		return elapsedText(t.Elapsed)
	case t.Tokens > 0:
		return strings.TrimPrefix(tokenText(t.Tokens), metaSep)
	default:
		return ""
	}
}

// mark is the first column: which row is open, where the cursor is, or both.
func mark(open, cursored bool) string {
	switch {
	case open && cursored:
		return bothMark
	case open:
		return openMark
	case cursored:
		return cursorMark
	default:
		return noMark
	}
}

// rowStyle draws the cursored row in the accent and the rest muted, which is
// the roster's own rule for a cursor (headStyle) rather than a second one: the
// loud line is the one the keys are about.
func rowStyle(cursored bool) lipgloss.Style {
	if cursored {
		return AccentStyle
	}
	return HintStyle
}

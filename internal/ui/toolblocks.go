package ui

// A tool call as the DM draws it: the ⏺ header, the fold under it, what a
// click opens, and how a call settles from running to finished.
//
// dm_blocks.go owns every other kind of block. This is its own file because
// a tool call is the one block with state behind it - which call a line
// belongs to, which the operator has opened, and how each one ended.

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
)

// expandKey is the key named under anything this pane folded. Claude Code
// composes the same hint from its own registry and says ctrl+o; ⌃O detaches
// here, so ⌃E is Wake's - see expand.go for why that trade is the cheapest
// one left.
const expandKey = "⌃E"

// toolUseBlock renders the ⏺ header, the `$ …` line for a call that carries a
// command, and the todo list or diff the call's own input carries.
//
// The bullet says what became of the call and the rest never changes, which is
// what lets a result recolour one line instead of re-rendering the block.
func toolUseBlock(tool *core.ToolCall, bullet lipgloss.Style, width int) string {
	if tool == nil {
		return ""
	}
	return joinBlock(
		toolHeadline(tool, bullet, width),
		render.ToolCommand(tool.Command, toolStyle(bullet), width),
		todoBlock(tool.Todos, width),
		diffBlock(tool.Diff, width),
	)
}

// toolHeadline is the ⏺ row alone - the one line a settling result rewrites.
func toolHeadline(tool *core.ToolCall, bullet lipgloss.Style, width int) string {
	return render.ToolCall(render.Call{
		Name:    tool.Name,
		Title:   tool.Title,
		Display: tool.Display,
	}, toolStyle(bullet), width)
}

// toolResultBlock renders the body under a call. call is the invocation this
// result answers, or nil for one whose call this pane never saw - which
// degrades to a plain fold rather than guessing at a receipt.
func toolResultBlock(ev core.Event, call *core.ToolCall, expanded bool, width int) string {
	r := render.Result{
		Body:      ev.Text,
		Collapsed: !expanded,
		Expand:    expandKey,
		Failed:    ev.Tool != nil && ev.Tool.IsError,
	}
	if call != nil {
		r.Receipt = call.Receipt
		// The call already opened the gutter with its `$ …` line, so the body
		// continues it rather than drawing a second ⎿.
		r.Continued = call.Command != ""
	}
	return render.ToolResult(r, toolStyle(ToolRunningStyle), width)
}

// toolStyle is the palette a tool block is drawn with. The bullet is the
// caller's because it carries the call's outcome; everything else is fixed.
func toolStyle(bullet lipgloss.Style) render.ToolStyle {
	return render.ToolStyle{
		Bullet: bullet,
		Name:   ToolNameStyle,
		Arg:    ToolArgStyle,
		Body:   HintStyle,
		Fail:   ErrorStyle,
	}
}

// bulletFor is the ⏺ style for a call: dim while its result is outstanding,
// green when it landed, red when it failed. See theme.go for the source.
func (d DM) bulletFor(id string) lipgloss.Style {
	failed, settled := d.outcomes[id]
	switch {
	case !settled:
		return ToolRunningStyle
	case failed:
		return ToolFailStyle
	default:
		return ToolOkStyle
	}
}

// expandedTool reports whether this call's result is drawn whole: either the
// whole conversation is expanded, or this one was opened by a click.
func (d DM) expandedTool(id string) bool { return d.expanded || d.opened[id] }

// observedTool files a tool call and its outcome as events arrive.
//
// Both maps are keyed on a tool_use id, written once per call, and never
// iterated - so they are shared with an older copy of this DM rather than
// copied on every event, which is the cost dm.go's Append rule forbids. Only
// `opened` is toggled, and it is copied on write because a click may rewrite
// an entry that is already there.
func (d DM) observedTool(ev core.Event) DM {
	if ev.Tool == nil {
		return d
	}
	switch ev.Kind {
	case core.KindToolUse:
		if d.calls == nil {
			d.calls = map[string]*core.ToolCall{}
		}
		d.calls[ev.Tool.ID] = ev.Tool
	case core.KindToolResult:
		if d.outcomes == nil {
			d.outcomes = map[string]bool{}
		}
		d.outcomes[ev.Tool.ID] = ev.Tool.IsError
	}
	return d
}

// settled recolours the ⏺ of the call a result just answered.
//
// One line is rewritten rather than the block re-rendered, because a result is
// an ordinary event and Append may not cost what the conversation so far cost.
// A call whose header is not in this transcript - drawn before a re-wrap that
// dropped it, or belonging to a subagent's view - is left alone.
func (d DM) settled(ev core.Event) DM {
	if ev.Kind != core.KindToolResult || ev.Tool == nil {
		return d
	}
	call, ok := d.calls[ev.Tool.ID]
	if !ok {
		return d
	}
	at, drawn := d.tr.headLine(ev.Tool.ID)
	if !drawn {
		return d
	}
	w := d.blockWidth()
	d.tr = d.tr.restyle(at, w, toolHeadline(call, d.bulletFor(ev.Tool.ID), w))
	return d
}

// openTool toggles the one call drawn on a transcript line, and reports
// whether a line held one at all.
func (d DM) openTool(line int) (DM, bool) {
	id := d.tr.toolAt(line)
	if id == "" {
		return d, false
	}
	opened := make(map[string]bool, len(d.opened)+1)
	for k, v := range d.opened {
		opened[k] = v
	}
	opened[id] = !opened[id]
	d.opened = opened

	// The reader is left where they are rather than returned to the newest
	// line, which is where ⌃E puts them: expanding one block renumbers only
	// the lines *below* it, so a scroll position above the click still points
	// at what was being read. Following the conversation is preserved the way
	// Append preserves it - sampled before the content changes.
	following := d.tr.atBottom()
	d.tr = d.tr.replace(renderTranscript(d))
	if following {
		d.tr = d.tr.toBottom()
	}
	return d, true
}

// diffBlock renders the before and after an edit carries in its own input.
// Nil for a call that carries neither - Bash, Read, Write - which degrades to
// the header rather than guessing.
func diffBlock(diff *core.ToolDiff, width int) string {
	if diff == nil {
		return ""
	}
	// The count goes above the bands, which is where Claude Code draws it -
	// `Added 9 lines, removed 1 line` under an Update's header. It says what
	// the bands cannot: how big the edit is before you read it.
	body := render.Diff(diff.Old, diff.New, width)
	summary := render.DiffSummary(diff.Old, diff.New)
	if summary == "" || body == "" {
		return body
	}
	return HintStyle.Render(summary) + "\n" + body
}

// todoBlock renders the task list a call carries under it, and "" for every
// other call. It draws in one place now: a *subagent's* checklist, in its own
// forwarded transcript. A subagent has no board pinned over the composer, so its
// list is drawn inline there the way every dispatch's frames are - the parent's
// own checklist is the board (checklistpin.go) and never reaches here, dropped
// by eventBlock's isChecklistOp guard. A legacy TodoWrite, which carries its
// whole list on one call, is drawn here too. The conversion is a copy rather than
// a shared type: internal/render takes plain values and never imports core.
func todoBlock(todos []core.Todo, width int) string {
	if len(todos) == 0 {
		return ""
	}
	items := make([]render.Todo, len(todos))
	for i, t := range todos {
		items[i] = render.Todo{Text: t.Text, Status: todoState(t.Status)}
	}
	return render.TodoList(items, width)
}

package ui

// The /workflows view: a dynamic Workflow() run's phases and agents, drawn in
// place of one pane's body. A headless session cannot draw Claude Code's own
// interactive /workflows menu, so Wake draws its equivalent from what it already
// folds (Fleet.tasks) and the run records the daemon reads off disk.
//
// Pane-scoped like the rewind picker - drawn only in Pane, reading keys only
// while that pane holds them - and key-capturing like the resume picker: read
// above App.key's switch, it owns every key while it is up, so it adds no
// legendEntries entry and advertises its keys on itself. ⌃C is the one key it
// hands back, after closing, for cmd/wake/killswitch.go's invariant.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// workflowsTakeNoArgument refuses an argument rather than ignoring it.
const workflowsTakeNoArgument = workflowsCommand + " opens this pane's workflow runs and takes no argument"

type workflowLevel int

const (
	levelList  workflowLevel = iota // the runs in scope
	levelRun                        // one run's phases beside the cursored phase's agents
	levelAgent                      // one agent's prompt, activity and outcome
)

// workflowFilter narrows the agents column; f cycles it.
type workflowFilter int

const (
	filterAll workflowFilter = iota
	filterRunning
	filterDone
	filterFailed
)

// workflowFilterWords names each filter, in f's order.
var workflowFilterWords = [...]string{"all", "running", "done", "failed"}

func (f workflowFilter) next() workflowFilter {
	return (f + 1) % workflowFilter(len(workflowFilterWords))
}

// admits reports whether an agent passes. An unrecorded state passes only
// "all": it is none of the three words the filter names.
func (f workflowFilter) admits(a core.WorkflowAgent) bool {
	switch f {
	case filterRunning:
		return a.State == core.WorkflowAgentRunning
	case filterDone:
		return a.State == core.WorkflowAgentDone
	case filterFailed:
		return a.State == core.WorkflowAgentFailed
	}
	return true
}

// WorkflowView is the open view; the zero value is none.
type WorkflowView struct {
	Up      bool
	Pane    string // the pane it is drawn in, "" the room
	Session string // whose runs, "" every agent's
	Task    string // the run open at levelRun
	Level   workflowLevel
	Cursor  int // the list row, or the phase row at levelRun
	Column  int // 0 phases, 1 agents
	Agent   int // the agent row within the cursored phase, after the filter
	Filter  workflowFilter

	// Detail is the agent open at levelAgent, by its snapshot Index: a row
	// number would move to another agent when the filter stops admitting it.
	Detail   int
	Expanded bool // Activity draws each call's input and the start of its result
	Scroll   int  // the agent level's first body row

	// Settling is a /workflows view nothing has been pressed in yet. Until then
	// its level follows the run count, because the runs on disk arrive after it
	// opened and exactly one run in scope skips the list.
	Settling bool
}

func (v WorkflowView) Open() bool { return v.Up }

// workflowState is everything the view needs, held as one App field.
type workflowState struct {
	view WorkflowView
	disk map[string][]core.WorkflowRun // each session's runs off disk, replaced per reply
	asks []string                      // sessions owed a FrameWorkflows; Update's drain writes them

	transcripts map[[2]string][]core.Event // each agent's own transcript by transcriptKey, replaced per reply
	asked       core.WorkflowAgent         // the open agent as it stood when it was last asked for
	agentAsk    rpc.Frame                  // the FrameWorkflowAgent owed, if Kind is set; Update's drain writes it
	replies     uint64                     // agent replies folded, so a kept layout knows its transcript is stale
	agent       agentCache                 // the open agent laid out; see relaidAgent
}

// workflowIn reports whether the view is drawn in this pane.
func (a App) workflowIn(id string) bool {
	return a.workflow.view.Open() && a.workflow.view.Pane == id
}

// openWorkflows is /workflows: this pane's runs, or every agent's in the room.
func (a App) openWorkflows(arg string) (App, tea.Cmd) {
	a = a.clearDraft()
	if arg != "" {
		notice.Report("%s", workflowsTakeNoArgument)
		return a, nil
	}
	a.workflow.view = WorkflowView{Up: true, Pane: a.focus, Session: a.focus, Settling: true}
	var ids []string
	for _, ag := range a.workflowScope(a.focus) {
		ids = append(ids, ag.ID)
	}
	return a.settleWorkflow().askWorkflows(ids...).takeWorkflowAsks()
}

// openWorkflow opens one run's level in its agent's pane - the sidebar row's
// way in, once an open key or a click has given that pane the keys. A placement
// that refused (⌃B from a lower pane) left them elsewhere, and opening the pane
// here would be ⌃D's replace under ⌃B's name. It returns no command because its
// callers cannot pass one on; its disk ask waits for Update's drain.
func (a App) openWorkflow(session, task string) App {
	if a.focus != session {
		return a
	}
	a.workflow.view = WorkflowView{Up: true, Pane: session, Session: session, Task: task, Level: levelRun}
	return a.askWorkflows(session)
}

func (a App) closeWorkflow() App {
	a.workflow.view = WorkflowView{}
	return a
}

// settleWorkflow re-derives an untouched view's level from the run count.
func (a App) settleWorkflow() App {
	v := a.workflow.view
	if !v.Open() || !v.Settling {
		return a
	}
	runs := a.workflowRuns(v.Session)
	if len(runs) == 1 {
		v = v.intoRun(runs[0].Task)
	} else {
		v.Level, v.Task, v.Cursor = levelList, "", 0
	}
	a.workflow.view = v
	return a
}

// pickedWorkflow is the running workflow the roster cursor names under id.
func (a App) pickedWorkflow(id string) (Task, bool) {
	if a.roster.Selected != id || a.roster.SelectedTask == "" {
		return Task{}, false
	}
	for _, t := range a.fleet.RunningTasks(id) {
		if t.Kind == core.TaskWorkflow && t.Dispatch == a.roster.SelectedTask {
			return t, true
		}
	}
	return Task{}, false
}

// viewingWorkflow opens the view when the roster cursor is on one of id's
// workflows, and leaves everything alone otherwise. A workflow forwards no
// frames of its own, so DM.Viewing on its dispatch would draw a blank pane.
func (a App) viewingWorkflow(id string) App {
	if t, ok := a.pickedWorkflow(id); ok {
		return a.openWorkflow(id, t.ID)
	}
	return a
}

// workflowKey owns every key while the view's pane holds them.
func (a App) workflowKey(m tea.KeyMsg) (App, tea.Cmd, bool) {
	v := a.workflow.view
	if !v.Open() || a.focus != v.Pane {
		return a, nil, false
	}
	if m.Type == tea.KeyCtrlC {
		return a.closeWorkflow(), nil, false
	}
	return a.workflowKeyed(m), nil, true
}

// workflowWheel is the wheel over the view: it walks the rows as ↑↓ do, since
// the transcript under the view is not drawn and scrolling it would leave it
// silently scrolled back once the view closes. Like any wheel, it moves no keys.
func (a App) workflowWheel(up bool) App {
	k := tea.KeyMsg{Type: tea.KeyDown}
	if up {
		k.Type = tea.KeyUp
	}
	return a.workflowKeyed(k)
}

// workflowKeyed is one key against the view. The agent level scrolls against
// what it draws, and arriving there asks for that agent's own transcript.
func (a App) workflowKeyed(m tea.KeyMsg) App {
	v := a.workflow.view
	if v.Level == levelAgent {
		a.workflow.view = v.agentKey(m, a.agentScrollLimit())
		return a.relaidAgent()
	}
	a.workflow.view = v.keyed(m, a.workflowRuns(v.Session))
	if a.workflow.view.Level == levelAgent {
		a.workflow.asked = core.WorkflowAgent{}
		a = a.reaskWorkflowAgent()
	}
	return a.relaidAgent()
}

// keyed is one key against the view at its level; any key ends settling.
func (v WorkflowView) keyed(m tea.KeyMsg, runs []workflowRunView) WorkflowView {
	v.Settling = false
	if v.Level == levelRun {
		return v.runKey(m, runs)
	}
	return v.listKey(m, runs)
}

func (v WorkflowView) listKey(m tea.KeyMsg, runs []workflowRunView) WorkflowView {
	last := max(len(runs)-1, 0)
	switch m.Type {
	case tea.KeyUp:
		v.Cursor = clamp(v.Cursor-1, 0, last)
	case tea.KeyDown:
		v.Cursor = clamp(v.Cursor+1, 0, last)
	case tea.KeyEnter, tea.KeyRight:
		if len(runs) > 0 {
			return v.intoRun(runs[clamp(v.Cursor, 0, last)].Task)
		}
	case tea.KeyEsc, tea.KeyLeft:
		return WorkflowView{}
	}
	return v
}

func (v WorkflowView) runKey(m tea.KeyMsg, runs []workflowRunView) WorkflowView {
	if m.Type == tea.KeyEsc || m.Type == tea.KeyLeft {
		return v.back(runs)
	}
	run, ok := runFor(runs, v.Task)
	if !ok {
		return v
	}
	switch {
	case m.Type == tea.KeyRunes && string(m.Runes) == "f":
		v.Filter, v.Agent = v.Filter.next(), 0
	case m.Type == tea.KeyUp:
		return v.step(run.Snap, -1)
	case m.Type == tea.KeyDown:
		return v.step(run.Snap, 1)
	case m.Type == tea.KeyEnter || m.Type == tea.KeyRight:
		return v.drill(run.Snap)
	}
	return v
}

// drill is ↵/→ at the run level: from the phases to their agents, and from an
// agent into its own detail.
func (v WorkflowView) drill(s core.WorkflowSnapshot) WorkflowView {
	agents := v.agentsOf(s)
	if v.Column == 0 || len(agents) == 0 {
		v.Column = 1
		return v
	}
	v.Level, v.Detail = levelAgent, agents[clamp(v.Agent, 0, len(agents)-1)].Index
	v.Expanded, v.Scroll = false, 0
	return v
}

// agentKey is one key at the agent level: ↵ toggles and → opens the Activity's
// detail, j/k and ↑↓ scroll within limit, and esc/← go back to the run level.
func (v WorkflowView) agentKey(m tea.KeyMsg, limit int) WorkflowView {
	by := 0
	switch {
	case m.Type == tea.KeyEsc || m.Type == tea.KeyLeft:
		v.Level, v.Detail, v.Expanded, v.Scroll = levelRun, 0, false, 0
		return v
	case m.Type == tea.KeyEnter:
		v.Expanded = !v.Expanded
	case m.Type == tea.KeyRight:
		v.Expanded = true
	case m.Type == tea.KeyDown || m.Type == tea.KeyRunes && string(m.Runes) == "j":
		by = 1
	case m.Type == tea.KeyUp || m.Type == tea.KeyRunes && string(m.Runes) == "k":
		by = -1
	}
	// Taken from where the draw clamped it, so a scroll left past a shorter
	// body - after a collapse or a resize - moves from what is on screen.
	v.Scroll = clamp(min(v.Scroll, limit)+by, 0, limit)
	return v
}

// intoRun is the run level on one run, cursors at the top.
func (v WorkflowView) intoRun(task string) WorkflowView {
	v.Level, v.Task, v.Cursor, v.Column, v.Agent = levelRun, task, 0, 0, 0
	return v
}

// back is esc at the run level: agents to phases, phases to the list when
// there is one to choose from, and closed otherwise.
func (v WorkflowView) back(runs []workflowRunView) WorkflowView {
	switch {
	case v.Column == 1:
		v.Column = 0
		return v
	case len(runs) > 1:
		return WorkflowView{Up: true, Pane: v.Pane, Session: v.Session, Level: levelList,
			Cursor: max(runIndex(runs, v.Task), 0), Filter: v.Filter}
	}
	return WorkflowView{}
}

// step moves the cursor of the column with the keys, without wrapping.
func (v WorkflowView) step(s core.WorkflowSnapshot, by int) WorkflowView {
	if v.Column == 0 {
		v.Cursor = clamp(v.Cursor+by, 0, max(len(workflowPhases(s))-1, 0))
		v.Agent = 0
		return v
	}
	v.Agent = clamp(v.Agent+by, 0, max(len(v.agentsOf(s))-1, 0))
	return v
}

// workflowPhases is what the phases column lists: the phases the script
// declared, then any an agent names that none declared, so no agent is on no row.
func workflowPhases(s core.WorkflowSnapshot) []core.WorkflowPhase {
	out := append([]core.WorkflowPhase(nil), s.Phases...)
	for _, ag := range s.Agents {
		if phaseAt(out, ag.Phase) < 0 {
			out = append(out, core.WorkflowPhase{Index: ag.Phase})
		}
	}
	return out
}

func phaseAt(phases []core.WorkflowPhase, index int) int {
	for i, p := range phases {
		if p.Index == index {
			return i
		}
	}
	return -1
}

// cursorPhase is the phase the cursor is on, clamped to the ones there are.
func (v WorkflowView) cursorPhase(phases []core.WorkflowPhase) (core.WorkflowPhase, bool) {
	if len(phases) == 0 {
		return core.WorkflowPhase{}, false
	}
	return phases[clamp(v.Cursor, 0, len(phases)-1)], true
}

// agentsOf is the cursored phase's agents the filter admits.
func (v WorkflowView) agentsOf(s core.WorkflowSnapshot) []core.WorkflowAgent {
	p, ok := v.cursorPhase(workflowPhases(s))
	if !ok {
		return nil
	}
	var out []core.WorkflowAgent
	for _, ag := range s.PhaseAgents(p.Index) {
		if v.Filter.admits(ag) {
			out = append(out, ag)
		}
	}
	return out
}

// workflowPress is a press on the view's own pane: it takes the keys there and
// makes the row under the pointer the cursor. It never starts a selection - the
// transcript one would measure is not what is drawn. The pane is measured with
// the regions the press landed in, before the keys move (startSelection's rule).
func (a App) workflowPress(id string, col, top, height, x, y int, r Regions) App {
	v := a.workflow.view
	runs := a.workflowRuns(v.Session)
	a.workflow.view = v.hit(runs, r.Cols[col], height-workflowTitleRows,
		x-a.layout.PaneLeft(r, col), y-top-workflowTitleRows)
	a.fleet = a.fleet.Focus(id)
	return a.refocus(id)
}

// hit makes the row at (x, y) - view-local, in a w by h view - the cursor of
// its column. Anything that is not a row leaves the view as it was, and so
// does the agent level, which has none: the run level's cursors are what esc
// goes back to.
func (v WorkflowView) hit(runs []workflowRunView, w, h, x, y int) WorkflowView {
	if x < 0 || y < 0 || v.Level == levelAgent {
		return v
	}
	v.Settling = false
	if v.Level == levelList {
		if len(runs) == 0 || y >= h-1 {
			return v
		}
		_, owner, start := v.listLayout(runs, w, h-1)
		if i := start + y; i < len(owner) && owner[i] >= 0 {
			v.Cursor = owner[i]
		}
		return v
	}
	run, ok := runFor(runs, v.Task)
	if !ok {
		return v
	}
	return v.hitRun(run, v.runGeometry(run, w, h), x, y)
}

func (v WorkflowView) hitRun(run workflowRunView, g runGeom, x, y int) WorkflowView {
	row := y - len(g.head)
	col := v.Column
	if g.boxed {
		row-- // the top edge
		col = 0
		if x > g.pw+2 { // past the phases cell, the wall between them included
			col = 1
		}
	}
	if row < 0 || row >= g.body {
		return v
	}
	if col == 0 {
		if i := g.phaseStart + row; i < len(workflowPhases(run.Snap)) {
			v.Column, v.Cursor, v.Agent = 0, i, 0
		}
		return v
	}
	if i := g.agentStart + row; i < len(v.agentsOf(run.Snap)) {
		v.Column, v.Agent = 1, i
	}
	return v
}

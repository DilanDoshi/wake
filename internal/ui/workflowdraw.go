package ui

// The /workflows view drawn: a list of runs, or one run's header over a box of
// two columns - its phases beside the cursored phase's agents - with the keys
// under it (Claude Code's own layout, matched by hand). Every row is cut and
// padded to the pane's width and the block is exactly the pane's height, so
// the column keeps its place in the frame and never scrolls the alt screen.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	workflowCursor      = "❯"
	workflowTitleRows   = 1 // the pane's own title, over the view
	workflowTitleSuffix = " › workflows"

	noWorkflowsSession = "No workflows in this session."
	noWorkflowsFleet   = "No workflows in this fleet."
	workflowGone       = "This run is no longer listed."
	noAgentsStarted    = "no agents started"

	glyphDone    = "✔"
	glyphRunning = "⏺"
	glyphFailed  = "✗"
	glyphOther   = "·"

	// workflowBoxChrome is what the two-column box costs a row: three walls
	// and a space either side of each column.
	workflowBoxChrome = 7
	minPhaseColumn    = 8
	minAgentColumn    = 12

	// spreadGap is the least space between a row's text and the figure set
	// against its far edge.
	spreadGap = 2
)

// modelFamilies is what an agent row calls its model: the column is narrow,
// and a dated id says nothing to an operator the family word does not.
var modelFamilies = []string{"opus", "sonnet", "haiku", "fable"}

func keyLine(keys ...string) string { return HintStyle.Render(strings.Join(keys, " · ")) }

// runKeyLine offers x only while there is a run to stop.
func runKeyLine(run workflowRunView) string {
	keys := []string{"↑↓ select", "↵ open", "f filter"}
	if run.Status == core.TaskRunning {
		keys = append(keys, "x stop")
	}
	return keyLine(append(keys, "s save", "esc back")...)
}

// workflowPane is the view in the pane it belongs to: a row naming the pane,
// as its composer's edge would, over the view itself.
func (a App) workflowPane(width, height int) string {
	v := a.workflow.view
	title, style := roomTitle, AccentStyle
	if v.Pane != "" {
		ag, _ := a.fleet.Agent(v.Pane)
		title, style = agentPrefix+a.agentName(v.Pane), speakerStyle(ag)
	}
	head := fitRow(style.Render(ansi.Truncate(oneLine(title)+workflowTitleSuffix, width, ellipsis)), width)
	if height <= workflowTitleRows {
		return firstRows(head, height)
	}
	return head + "\n" + v.render(a.workflowRuns(v.Session), width, height-workflowTitleRows)
}

// render is the view in exactly w by h cells.
func (v WorkflowView) render(runs []workflowRunView, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	var rows []string
	if v.Level == levelRun {
		rows = v.runRows(runs, w, h)
	} else {
		rows = v.listRows(runs, w, h)
	}
	for i := range rows {
		rows[i] = fitRow(rows[i], w)
	}
	return strings.Join(rows, "\n")
}

// --- the list -------------------------------------------------------------

func (v WorkflowView) listRows(runs []workflowRunView, w, h int) []string {
	if len(runs) == 0 {
		empty := noWorkflowsSession
		if v.Session == "" {
			empty = noWorkflowsFleet
		}
		return stacked([]string{TextStyle.Render(empty)}, nil, []string{keyLine("esc close")}, h)
	}
	n := h - 1
	lines, _, start := v.listLayout(runs, w, n)
	return stacked(nil, windowRows(lines, start, n), []string{keyLine("↑↓ select", "↵ open", "esc close")}, h)
}

// listLayout is the list's lines, the run each one is (-1 for the room's
// agent headings), and the first line an n-row window draws - one measure for
// the draw and the mouse.
func (v WorkflowView) listLayout(runs []workflowRunView, w, n int) (lines []string, owner []int, start int) {
	cur := clamp(v.Cursor, 0, len(runs)-1)
	at := 0
	for i, run := range runs {
		if v.Session == "" && (i == 0 || runs[i-1].Session != run.Session) {
			lines = append(lines, AccentStyle.Render(agentPrefix+oneLine(run.Agent)))
			owner = append(owner, -1)
		}
		if i == cur {
			at = len(lines)
		}
		lines = append(lines, listRow(run, i == cur, w))
		owner = append(owner, i)
	}
	start, _ = windowAround(at, len(lines), n)
	return lines, owner, start
}

func listRow(run workflowRunView, cursored bool, w int) string {
	lead, style := "  ", TextStyle
	if cursored {
		lead, style = workflowCursor+" ", AccentStyle
	}
	left := style.Render(lead) + runGlyph(run.Status) + " " + style.Render(oneLine(run.Name))
	if run.Summary != "" {
		left += HintStyle.Render(" · " + oneLine(run.Summary))
	}
	return spread(left, HintStyle.Render(runFigures(run)), w)
}

// runFigures is what a run has done: N/M agents, how long, and its state.
func runFigures(run workflowRunView) string {
	var parts []string
	if n := len(run.Snap.Agents); n > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d agents", run.Snap.Done(), n))
	}
	if run.Elapsed > 0 {
		parts = append(parts, elapsedText(run.Elapsed))
	}
	return strings.Join(append(parts, string(run.Status)), " · ")
}

func runGlyph(s core.TaskStatus) string {
	switch s {
	case core.TaskDone:
		return ToolOkStyle.Render(glyphDone)
	case core.TaskRunning:
		return ToolRunningStyle.Render(glyphRunning)
	case core.TaskFailed:
		return ToolFailStyle.Render(glyphFailed)
	}
	return HintStyle.Render(glyphOther)
}

// --- the run --------------------------------------------------------------

// runGeom is where the run level puts things, measured once so the draw and
// the mouse agree about which row is which.
type runGeom struct {
	head       []string // the header, and a failed run's error under it
	boxed      bool     // the two-column box fits; else the keyed column alone
	pw, aw     int      // the two columns' widths inside the box
	body       int      // rows of phases and agents: the content, bounded by the pane
	phaseStart int      // the first phase and agent the windows draw
	agentStart int
}

func (v WorkflowView) runGeometry(run workflowRunView, w, h int) runGeom {
	g := runGeom{head: []string{v.header(run, w)}}
	if run.Status == core.TaskFailed && run.Error != "" {
		g.head = append(g.head, ErrorStyle.Render(firstErrorLine(run.Error)))
	}
	phases := workflowPhases(run.Snap)
	g.pw = min(phaseWidth(phaseCells(run.Snap, phases)), max((w-workflowBoxChrome)/3, minPhaseColumn))
	g.aw = w - workflowBoxChrome - g.pw
	g.body = h - len(g.head) - 1 // the key line
	if g.boxed = g.body >= 3 && g.aw >= minAgentColumn; g.boxed {
		// The box's two edges, and no taller than its longest column, so it
		// holds still while the cursor moves between phases.
		g.body = min(g.body-2, max(len(phases), widestPhase(run.Snap, phases), 1))
	}
	g.phaseStart, _ = windowAround(clamp(v.Cursor, 0, max(len(phases)-1, 0)), len(phases), g.body)
	agents := v.agentsOf(run.Snap)
	g.agentStart, _ = windowAround(clamp(v.Agent, 0, max(len(agents)-1, 0)), len(agents), g.body)
	return g
}

// widestPhase is how many agents the most crowded phase has.
func widestPhase(s core.WorkflowSnapshot, phases []core.WorkflowPhase) int {
	most := 0
	for _, p := range phases {
		most = max(most, len(s.PhaseAgents(p.Index)))
	}
	return most
}

func (v WorkflowView) runRows(runs []workflowRunView, w, h int) []string {
	run, ok := runFor(runs, v.Task)
	if !ok {
		return stacked([]string{HintStyle.Render(workflowGone)}, nil, []string{keyLine("esc back")}, h)
	}
	g := v.runGeometry(run, w, h)
	return stacked(g.head, v.runBody(run, g, w), []string{runKeyLine(run)}, h)
}

func (v WorkflowView) runBody(run workflowRunView, g runGeom, w int) []string {
	phases := workflowPhases(run.Snap)
	if !g.boxed {
		if v.Column == 1 {
			return windowRows(v.agentRows(run.Snap, w), g.agentStart, g.body)
		}
		return windowRows(v.phaseRows(run.Snap, phases, w), g.phaseStart, g.body)
	}
	left := padRows(windowRows(v.phaseRows(run.Snap, phases, g.pw), g.phaseStart, g.body), g.body)
	right := padRows(windowRows(v.agentRows(run.Snap, g.aw), g.agentStart, g.body), g.body)
	wall := HintStyle.Render("│")
	out := []string{titledEdge("╭", "─", "", "Phases", g.pw+2, 0, HintStyle, v.columnStyle(0)) +
		titledEdge("┬", "─", "╮", v.agentsTitle(run.Snap, phases), g.aw+2, 0, HintStyle, v.columnStyle(1))}
	for i := range g.body {
		out = append(out, wall+" "+fitRow(left[i], g.pw)+" "+wall+" "+fitRow(right[i], g.aw)+" "+wall)
	}
	return append(out, HintStyle.Render("╰"+strings.Repeat("─", g.pw+2)+"┴"+strings.Repeat("─", g.aw+2)+"╯"))
}

// header is the run's name and what it is for, against how far it has got.
func (v WorkflowView) header(run workflowRunView, w int) string {
	left := TextStyle.Bold(true).Render(oneLine(run.Name))
	if v.Session == "" {
		left = AccentStyle.Render(agentPrefix+oneLine(run.Agent)+" › ") + left
	}
	if run.Summary != "" {
		left += HintStyle.Render(" · " + oneLine(run.Summary))
	}
	return spread(left, HintStyle.Render(runFigures(run)), w)
}

// columnStyle lights the title and cursor of the column that has the keys.
func (v WorkflowView) columnStyle(col int) lipgloss.Style {
	if v.Column == col {
		return AccentStyle
	}
	return TextStyle
}

// agentsTitle heads the agents column: the phase, how many agents it has, and
// the filter when one is narrowing them.
func (v WorkflowView) agentsTitle(s core.WorkflowSnapshot, phases []core.WorkflowPhase) string {
	p, ok := v.cursorPhase(phases)
	if !ok {
		return "Agents"
	}
	title := phaseTitle(p) + " · " + agentCount(len(s.PhaseAgents(p.Index)))
	if v.Filter != filterAll {
		title += " · " + workflowFilterWords[v.Filter]
	}
	return title
}

func agentCount(n int) string {
	if n == 1 {
		return "1 agent"
	}
	return strconv.Itoa(n) + " agents"
}

func phaseTitle(p core.WorkflowPhase) string {
	if p.Title == "" {
		return "phase " + strconv.Itoa(p.Index)
	}
	return oneLine(p.Title)
}

// phaseCell is one phase row's parts, so the rows and the column's width are
// measured off the same strings.
type phaseCell struct{ glyph, title, count string }

// phaseCells: a phase is ✔ when every agent it has is done, else its number.
func phaseCells(s core.WorkflowSnapshot, phases []core.WorkflowPhase) []phaseCell {
	out := make([]phaseCell, len(phases))
	for i, p := range phases {
		all := s.PhaseAgents(p.Index)
		done := 0
		for _, ag := range all {
			if ag.State == core.WorkflowAgentDone {
				done++
			}
		}
		glyph := strconv.Itoa(p.Index)
		if len(all) > 0 && done == len(all) {
			glyph = glyphDone
		}
		out[i] = phaseCell{glyph: glyph, title: phaseTitle(p), count: fmt.Sprintf("%d/%d", done, len(all))}
	}
	return out
}

// cellWidths is the widest glyph, title and count - the columns a row aligns on.
func cellWidths(cells []phaseCell) (gw, tw, cw int) {
	for _, c := range cells {
		gw, tw, cw = max(gw, ansi.StringWidth(c.glyph)), max(tw, ansi.StringWidth(c.title)), max(cw, len(c.count))
	}
	return gw, tw, cw
}

// phaseWidth is the phases column at its natural width: cursor, glyph,
// title and count, a space between each.
func phaseWidth(cells []phaseCell) int {
	gw, tw, cw := cellWidths(cells)
	return ansi.StringWidth(workflowCursor+" ") + gw + 1 + tw + 1 + cw
}

func (v WorkflowView) phaseRows(s core.WorkflowSnapshot, phases []core.WorkflowPhase, width int) []string {
	cells := phaseCells(s, phases)
	gw, tw, cw := cellWidths(cells)
	tw = max(min(tw, width-(phaseWidth(cells)-tw)), 1)
	cur := clamp(v.Cursor, 0, max(len(cells)-1, 0))
	out := make([]string, len(cells))
	for i, c := range cells {
		lead, style := "  ", TextStyle
		if i == cur {
			lead, style = workflowCursor+" ", v.columnStyle(0)
		}
		glyph := fitRow(c.glyph, gw)
		if c.glyph == glyphDone {
			glyph = ToolOkStyle.Render(glyph)
		}
		title := fitRow(ansi.Truncate(c.title, tw, ellipsis), tw)
		out[i] = style.Render(lead) + glyph + " " + style.Render(title) + " " +
			HintStyle.Render(strings.Repeat(" ", cw-len(c.count))+c.count)
	}
	return out
}

func (v WorkflowView) agentRows(s core.WorkflowSnapshot, width int) []string {
	agents := v.agentsOf(s)
	if len(agents) == 0 {
		return []string{HintStyle.Render(v.noAgents(s))}
	}
	lw := 0
	for _, ag := range agents {
		lw = max(lw, ansi.StringWidth(oneLine(ag.Label)))
	}
	lw = max(min(lw, width/2), 1)
	cur := clamp(v.Agent, 0, len(agents)-1)
	out := make([]string, len(agents))
	for i, ag := range agents {
		lead, style := " ", TextStyle
		if i == cur {
			lead, style = workflowCursor, v.columnStyle(1)
		}
		label := fitRow(ansi.Truncate(oneLine(ag.Label), lw, ellipsis), lw)
		left := style.Render(lead) + agentGlyph(ag.State) + " " + style.Render(label) + "  " + HintStyle.Render(agentMeta(ag))
		dur := ""
		if ag.Duration > 0 {
			dur = elapsedText(ag.Duration)
		}
		out[i] = spread(left, HintStyle.Render(dur), width)
	}
	return out
}

// noAgents says why the agents column is empty: nothing started yet, or
// nothing the filter admits.
func (v WorkflowView) noAgents(s core.WorkflowSnapshot) string {
	if p, ok := v.cursorPhase(workflowPhases(s)); ok && len(s.PhaseAgents(p.Index)) > 0 {
		return "no " + workflowFilterWords[v.Filter] + " agents"
	}
	return noAgentsStarted
}

func agentGlyph(s core.WorkflowAgentState) string {
	switch s {
	case core.WorkflowAgentDone:
		return ToolOkStyle.Render(glyphDone)
	case core.WorkflowAgentRunning:
		return ToolRunningStyle.Render(glyphRunning)
	case core.WorkflowAgentFailed:
		return ToolFailStyle.Render(glyphFailed)
	}
	return HintStyle.Render(glyphOther)
}

// agentMeta is an agent's model and spend, led by its state word when that
// word was not one the view has a glyph for.
func agentMeta(ag core.WorkflowAgent) string {
	var parts []string
	if ag.State == core.WorkflowAgentUnknown {
		parts = append(parts, string(ag.State))
	}
	if ag.Model != "" {
		parts = append(parts, workflowModel(ag.Model))
	}
	if ag.Tokens > 0 {
		parts = append(parts, humanTokens(ag.Tokens))
	}
	return strings.Join(parts, " · ")
}

func workflowModel(id string) string {
	for _, family := range modelFamilies {
		if strings.Contains(id, family) {
			return family
		}
	}
	return oneLine(id)
}

// --- rows -----------------------------------------------------------------

// stacked lays head, body and foot one under the next in exactly h rows, blank
// below: the body is cut to what the pane leaves it, and a pane too short for
// any of it keeps the head, then the foot.
func stacked(head, body, foot []string, h int) []string {
	n := max(h-len(head)-len(foot), 0)
	out := append(slices.Clone(head), body[:min(len(body), n)]...)
	return padRows(append(out, foot...), h)
}

// windowRows is up to n rows of a list from start.
func windowRows(rows []string, start, n int) []string {
	end := min(start+max(n, 0), len(rows))
	return slices.Clone(rows[min(start, end):end])
}

// fitRow cuts a row to w cells and pads it to exactly w.
func fitRow(s string, w int) string {
	s = ansi.Truncate(s, w, "")
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

// spread sets right against a w-wide row's far edge, cutting left to make
// room; right is dropped whole when it cannot fit, because a figure cut short
// reads as a different figure.
func spread(left, right string, w int) string {
	rw := ansi.StringWidth(right)
	if rw == 0 || rw+spreadGap >= w {
		return ansi.Truncate(left, w, ellipsis)
	}
	left = ansi.Truncate(left, w-rw-spreadGap, ellipsis)
	return left + strings.Repeat(" ", w-ansi.StringWidth(left)-rw) + right
}

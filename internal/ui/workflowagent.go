package ui

// The /workflows view's agent level drawn: one agent's prompt, activity and
// outcome under a fixed head, with its keys at the foot - laid out once per
// change and windowed per frame (App.relaidAgent).

import (
	"cmp"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
)

// agentRows is the agent level laid out at one width: a fixed head, the body
// that scrolls, the keys. It costs a render per tool call, so it is laid out
// once per change and kept (App.relaidAgent); a frame only windows it.
type agentRows struct{ head, body, foot []string }

func layAgent(ag core.WorkflowAgent, events []core.Event, expanded bool, w int) agentRows {
	return agentRows{
		head: []string{TextStyle.Bold(true).Render(oneLine(ag.Label)), agentStatus(ag), HintStyle.Render(agentFigures(ag))},
		body: agentBody(ag, events, expanded, w),
		foot: []string{agentKeyLine(expanded)},
	}
}

// shown is how many body rows an h-row block leaves, and limit how far the body
// then scrolls - the one measure the draw and the scroll keys share.
func (r agentRows) shown(h int) int { return max(h-len(r.head)-len(r.foot), 0) }
func (r agentRows) limit(h int) int { return max(len(r.body)-r.shown(h), 0) }

// render is the rows in exactly w by h cells, the body scrolled by scroll -
// clamped here, so a scroll past either end draws that end.
func (r agentRows) render(w, h, scroll int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	return fitBlock(stacked(r.head, windowRows(r.body, clamp(scroll, 0, r.limit(h)), r.shown(h)), r.foot, h), w)
}

// stateWord is an unresolved state as the wire spelled it.
func stateWord(ag core.WorkflowAgent) string { return oneLine(cmp.Or(ag.StateWord, string(ag.State))) }

// agentStatus is the agent's state and model: "✔ Completed · haiku".
func agentStatus(ag core.WorkflowAgent) string {
	word := stateWord(ag)
	switch ag.State {
	case core.WorkflowAgentDone:
		word = "Completed"
	case core.WorkflowAgentRunning:
		word = "Running"
	case core.WorkflowAgentFailed:
		word = "Failed"
	}
	line := agentGlyph(ag.State) + " " + TextStyle.Render(word)
	if ag.Model != "" {
		line += HintStyle.Render(" · " + workflowModel(ag.Model))
	}
	return line
}

// agentFigures is what the agent has spent, and how long it took once done.
func agentFigures(ag core.WorkflowAgent) string {
	var parts []string
	if ag.Tokens > 0 {
		parts = append(parts, humanTokens(ag.Tokens)+" tok")
	}
	parts = append(parts, plural(ag.ToolCalls, "tool call"))
	if ag.Duration > 0 {
		parts = append(parts, elapsedText(ag.Duration))
	}
	return strings.Join(parts, " · ")
}

func agentKeyLine(expanded bool) string {
	toggle := "↵ expand"
	if expanded {
		toggle = "↵ collapse"
	}
	return keyLine("j/k scroll", toggle, "esc back")
}

// agentBody is what scrolls. The Prompt and the Outcome are the snapshot's own
// previews - the task text alone, where the transcript's first turn wraps it
// in the harness's framing - so they stand when the transcript does not. A
// failed agent with no result has its error for an outcome.
func agentBody(ag core.WorkflowAgent, events []core.Event, expanded bool, w int) []string {
	body := agentSection("Prompt", ag.Prompt, TextStyle, w)
	body = append(body, agentActivity(events, expanded, w)...)
	if strings.TrimSpace(ag.Result) == "" && ag.Error != "" {
		return append(body, agentSection("Outcome", firstErrorLine(ag.Error), ErrorStyle, w)...)
	}
	return append(body, agentSection("Outcome", ag.Result, TextStyle, w)...)
}

// agentSection is a titled block of the agent's own words after a blank row,
// and nothing while there are none - a running agent has no outcome yet.
func agentSection(title, text string, style lipgloss.Style, w int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	words := style.Width(max(w, bodyIndent+1)).PaddingLeft(bodyIndent).Render(text)
	return append([]string{"", sectionTitle(title)}, strings.Split(words, "\n")...)
}

func sectionTitle(s string) string { return TextStyle.Bold(true).Render(s) }

// agentActivity is one headline per tool call, drawn by the conversation's own
// tool blocks; expanded adds each call's input and the start of its result.
// No events is a transcript missing, unreadable or not read yet.
func agentActivity(events []core.Event, expanded bool, w int) []string {
	if len(events) == 0 {
		return []string{"", HintStyle.Render(activityUnavailable)}
	}
	results := map[string]core.Event{}
	for _, ev := range events {
		if ev.Kind == core.KindToolResult && ev.Tool != nil {
			results[ev.Tool.ID] = ev
		}
	}
	iw := max(w-bodyIndent, 1)
	out := []string{"", sectionTitle("Activity")}
	for _, ev := range events {
		if ev.Kind != core.KindToolUse || ev.Tool == nil {
			continue
		}
		res, settled := results[ev.Tool.ID]
		bullet := outcomeBullet(settled && res.Tool.IsError, settled)
		block := toolHeadline(ev.Tool, bullet, iw)
		if expanded {
			// No fold key: every key here is the view's, and ⌃E is the conversation's.
			block = joinBlock(toolUseBlock(ev.Tool, bullet, iw), toolResultBlock(res, ev.Tool, false, "", iw))
		}
		out = append(out, indented(block)...)
	}
	if len(out) == 2 {
		out = append(out, indented(HintStyle.Render(noToolCalls))...)
	}
	return out
}

// indented is a block's rows under its section title.
func indented(block string) []string {
	if block == "" {
		return nil
	}
	rows := strings.Split(block, "\n")
	for i := range rows {
		rows[i] = strings.Repeat(" ", bodyIndent) + rows[i]
	}
	return rows
}

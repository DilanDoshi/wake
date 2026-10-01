package ui

// The room's record that an agent's question was resolved by the operator.
//
// The ask left a yellow "has a question" line in the group chat (App.observe →
// roomBlock's KindPermissionRequest case). A settle resolves that line in place:
// it becomes the answered record - purple, the answers under a ⎿ the way a tool
// result reads in a DM - or a muted cancelled one, rather than going stale above
// a second line (the owner's 2026-08-28 request). Live-only, by that entry's
// ruling: nothing re-derives it off disk.
//
// Questions only, on the owner's 2026-08-28 request scope: a permission or a
// plan is a verb (allow/deny), not a chosen answer, and neither posts a distinct
// room line the way a question does. The settle points a question has are the
// review's Submit (cardreview.go), the refusal (cardanswer.go) and esc (send.go).

import (
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
)

// resolvedArrow joins a question to the answer it was given in the record.
const resolvedArrow = " → "

// recordQuestionResolved resolves agentID's ask requestID in the room: answered
// (with one "question → answer" row each) or cancelled. It is authored above the
// airlock, the way Event.FromRoom is - no frame carries it - and goes only to the
// room, never to the agent's DM, which already has the card and the turn that
// follows it.
func (a App) recordQuestionResolved(agentID, requestID string, answered bool, answers []string) App {
	agent, _ := a.fleet.Agent(agentID)
	notice := core.NoticeQuestionCancelled
	if answered {
		notice = core.NoticeQuestionAnswered
	}
	return a.withRoom(a.room.resolveAsk(core.Event{
		Kind:      core.KindSystem,
		SessionID: agentID,
		RequestID: requestID,
		Notice:    notice,
		Text:      strings.Join(answers, "\n"),
	}, agent))
}

// resolvedAnswers is the record's rows: each question - its chip, else its
// words - and the review's own label for what was chosen, so the room and the
// card that sent it say the same thing.
func (c Card) resolvedAnswers() []string {
	out := make([]string, 0, len(c.Detail.Questions))
	for i, q := range c.Detail.Questions {
		out = append(out, questionChip(q)+resolvedArrow+c.answerLabel(i))
	}
	return out
}

// questionChip is what names a question in one row.
func questionChip(q core.Question) string {
	if q.Header != "" {
		return collapseWhitespaceOneLine(q.Header)
	}
	return collapseWhitespaceOneLine(q.Text)
}

// resolveAsk puts the resolution where the ask's line is, or appends it when the
// room no longer holds that line (evicted past retention), so an answer is never
// silent. The line keeps its id and attribution; only what it says changes.
func (r Room) resolveAsk(resolved core.Event, by Agent) Room {
	held := r.said.slice(r.said.first(), r.said.len())
	for i, l := range held {
		if l.ev.Kind != core.KindPermissionRequest || l.ev.RequestID != resolved.RequestID || l.by.ID != by.ID {
			continue
		}
		lines := append([]roomLine(nil), held...)
		lines[i].ev = resolved
		return r.relaid(r, lines, l.id)
	}
	return r.Append(resolved, by)
}

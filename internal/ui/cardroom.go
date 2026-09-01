package ui

// The room's record that an agent's question was resolved by the operator.
//
// The ask left a yellow "has a question" line in the group chat (App.observe →
// roomBlock's KindPermissionRequest case). Without this its close is invisible
// there: the card is drawn in the agent's own pane, and the awareness strip's
// "N need you" clears on its own when the agent leaves StateBlocked - but the
// warn line in the room just goes stale. So a settle leaves one line behind,
// the way a dispatch leaves "● Subagent finished".
//
// Questions only, on the owner's 2026-08-28 request scope: a permission or a
// plan is a verb (allow/deny), not a chosen answer, and neither posts a distinct
// room line the way a question does. The two settle points a question has are
// the review's Submit (cardreview.go) and the refusal (cardanswer.go).

import "github.com/DilanDoshi/wake/internal/core"

// recordQuestionResolved appends one line to the room noting that agentID's
// question was answered (green) or cancelled (muted). It is authored above the
// airlock, the way Event.FromRoom is - no frame carries it - and goes only to
// the room, never to the agent's DM, which already has the card and the turn
// that follows it.
func (a App) recordQuestionResolved(agentID string, answered bool) App {
	agent, _ := a.fleet.Agent(agentID)
	notice := core.NoticeQuestionCancelled
	if answered {
		notice = core.NoticeQuestionAnswered
	}
	return a.withRoom(a.room.Append(core.Event{
		Kind:      core.KindSystem,
		SessionID: agentID,
		Notice:    notice,
	}, agent))
}

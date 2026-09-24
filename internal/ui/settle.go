package ui

import tea "github.com/charmbracelet/bubbletea"

// settle is everything a folded frame can leave owing, derived after the fold
// because observe returns only an App: a held message the frame freed (see
// queue.go), the heartbeat a turn that started needs, the rate-limit notice's
// linger, a stalled login's park, and the MCP asks a reply owes the daemon.
// One helper for the single-frame path and the batch path, so the two cannot
// drift.
func (a App) settle() (App, tea.Cmd) {
	a, flush := a.flushQueued()
	a, tick := a.beat()
	a, rl := a.armRateLimitClear()
	a, park := a.autoParkStalled()
	a, mcp := a.mcpFollowUp()
	return a, tea.Batch(flush, tick, rl, park, mcp)
}

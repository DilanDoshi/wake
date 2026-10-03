package ui

import tea "github.com/charmbracelet/bubbletea"

// settle is everything a folded frame can leave owing, derived after the fold
// because observe returns only an App: a held message the frame freed (see
// queue.go), a send-now claude has answered (recall.go), the heartbeat a turn that started needs, a stalled login's park or
// its wake once the login works, and the MCP asks a reply owes the daemon.
// One helper for the single-frame path and the batch path, so the two cannot
// drift.
func (a App) settle() (App, tea.Cmd) {
	a, flush := a.flushQueued()
	a, hurried := a.sendRecalled()
	a, tick := a.beat()
	a, park := a.autoParkStalled()
	a, wake := a.autoWakeRecovered()
	a, mcp := a.mcpFollowUp()
	return a, tea.Batch(flush, hurried, tick, park, wake, mcp)
}

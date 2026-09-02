package ui

// The line a DM draws above its composer: the working spinner while a turn is in
// flight, and the done summary once it has finished. Split from dm.go, which was
// a handful of lines under the hard maximum when the done line arrived - the same
// subject-split panedraw.go took. The line itself is beat.go's workingLine and
// doneLine; this is only which of them a DM shows, and the one row that costs.

import "github.com/DilanDoshi/wake/internal/rpc"

const (
	// composerGap is the blank row kept above the composer, so the input box sits
	// clear of the last line of output rather than crammed against it - Claude
	// Code's own spacing. Both panes keep it while nothing (a card, picker or
	// completion menu) is pinned there. Budgeted in baseChrome, or the pane draws
	// a row past what it was given and the alt screen scrolls.
	composerGap = 1

	// beatGap is the blank row above the working/done line, so it too sits clear
	// of the output. Its breathing room below is composerGap - one blank each side.
	beatGap = 1
)

// heartbeat is the line above the composer: the working line while a turn is in
// flight, the done line once it has finished, or "" for an agent that has done
// nothing this client saw. Drawn at the transcript's width so it lines up with
// the prose above it.
func (d DM) heartbeat() string {
	// Compaction runs between turns - each of its several result frames clears
	// the turn, so the agent is idle - which is exactly when the done line would
	// show. So it wins over both: a session plainly still working is not "done".
	if !d.compactingSince.IsZero() {
		return compactingLine(d.compactingSince, d.blockWidth())
	}
	if d.Agent.State == rpc.StateWorking {
		return workingLine(d.SessionID, d.Agent.State, d.Agent.Doing, d.Agent.startedAt, d.Agent.TurnTokens, d.blockWidth())
	}
	if d.showsDone() {
		return doneLine(d.SessionID, d.Agent.startedAt, d.Agent.doneAt, d.Agent.turnDur, d.blockWidth())
	}
	return ""
}

// showsDone is whether the idle agent has a finished turn to summarise. Gated to
// idle: a parked or ended agent's standing is in the title, and a blocked one is
// still owed a turn. And gated to a quiet pane: a live streaming preview is a new
// turn in flight (one the daemon reports idle, so State never returns to working
// - see notDone), and the summary must not draw over the sentence being written.
func (d DM) showsDone() bool {
	return d.Agent.State == rpc.StateIdle && !d.Agent.doneAt.IsZero() && d.partial.view == ""
}

// hasBeat is whether the pane draws the line above the composer at all - the one
// row baseChrome and SetSize must both account for, or the pane is sized a row
// out and the alt screen scrolls on every draw.
func (d DM) hasBeat() bool {
	return !d.compactingSince.IsZero() || d.Agent.State == rpc.StateWorking || d.showsDone()
}

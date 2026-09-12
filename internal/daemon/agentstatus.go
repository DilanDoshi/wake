package daemon

import (
	"cmp"
	"slices"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// snapshot is this agent's line in a status report.
func (a *agent) snapshot() rpc.SessionStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := rpc.SessionStatus{
		ID:             a.id,
		Name:           a.name,
		Label:          a.label,
		Color:          a.color,
		Dir:            a.dir,
		Cwd:            a.runningIn(),
		ParentID:       a.parent,
		Tool:           a.tool,
		ToolArg:        a.toolArg,
		Effort:         cmp.Or(a.confirmedEffort, a.effort),
		ConfirmedModel: a.confirmedModel,
		Model:          a.observedModel,
		Budget:         a.budget,
		Commands:       a.commands,
		PRs:            slices.Clone(a.prs),
		Goal:           goalStatus(a.goalCondition),
		Loop:           loopStatus(a.loop),
		State:          a.stateLocked(time.Now()),
		RequestIDs:     a.pendingIDsLocked(),
		PID:            a.sess.Pgid(),
		QuietMS:        time.Since(a.lastEvent).Milliseconds(),
	}
	switch {
	case a.err != nil:
		st.Error = a.err.Error()
	case a.unreachable != nil:
		st.Error = a.unreachable.Error()
	}
	return st
}

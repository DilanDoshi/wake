package ui

// What the /workflows view draws from: the workflow rows this client has
// folded live (Fleet.tasks) and each session's run records the daemon reads
// off disk (FrameWorkflows / FrameWorkflowsReply), one run per task id.

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// workflowRunsFailed names the write that could not happen, sendFailed's pattern.
const workflowRunsFailed = "asking for workflow runs"

// workflowRunView is one run as the view draws it: a live row or a record.
type workflowRunView struct {
	Session, Agent, Task, Name, Summary string
	Status                              core.TaskStatus
	Error                               string
	Started                             time.Time
	Elapsed                             time.Duration
	Snap                                core.WorkflowSnapshot
	Live                                bool
}

// workflowScope is whose runs a view shows: one agent, or every agent on the
// roster ordered by name, so the room's groups hold still while the ranking moves.
func (a App) workflowScope(session string) []Agent {
	if session != "" {
		ag, ok := a.fleet.Agent(session)
		if !ok {
			ag = Agent{ID: session, Name: session}
		}
		return []Agent{ag}
	}
	agents := a.fleet.OnRoster()
	slices.SortStableFunc(agents, func(x, y Agent) int { return strings.Compare(x.Name, y.Name) })
	return agents
}

// workflowRuns is every run in scope, grouped by agent, newest first within one.
func (a App) workflowRuns(session string) []workflowRunView {
	var out []workflowRunView
	for _, ag := range a.workflowScope(session) {
		out = append(out, a.agentRuns(ag)...)
	}
	return out
}

// agentRuns is one agent's runs: its live rows newest first, then the records
// of runs it has none for, in the daemon's newest-first order - a run this
// client never folded ended before it was watching. Live wins a task id both
// know; the record fills what a live row never carries.
func (a App) agentRuns(ag Agent) []workflowRunView {
	disk := a.workflow.disk[ag.ID]
	rows := a.fleet.tasks[ag.ID].Rows()
	seen := map[string]bool{}
	var out []workflowRunView
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Kind != core.TaskWorkflow {
			continue
		}
		run := liveRun(rows[i], ag)
		if at := slices.IndexFunc(disk, func(r core.WorkflowRun) bool { return r.TaskID == run.Task }); at >= 0 {
			run.Started, run.Summary = disk[at].Started, cmp.Or(run.Summary, disk[at].Summary)
		}
		seen[run.Task] = true
		out = append(out, run)
	}
	for _, rec := range disk {
		if !seen[rec.TaskID] {
			out = append(out, diskRun(rec, ag))
		}
	}
	return out
}

func liveRun(t Task, ag Agent) workflowRunView {
	return workflowRunView{
		Session: ag.ID, Agent: ag.Name, Task: t.ID, Name: t.Name, Summary: t.Summary,
		Status: t.Status, Error: t.Error, Elapsed: t.Elapsed, Snap: t.Workflow, Live: true,
	}
}

func diskRun(r core.WorkflowRun, ag Agent) workflowRunView {
	run := workflowRunView{
		Session: ag.ID, Agent: ag.Name, Task: r.TaskID, Name: r.Name, Summary: r.Summary,
		Status: r.Status, Error: r.Error, Started: r.Started, Elapsed: r.Duration,
	}
	if r.Progress != nil {
		run.Snap = *r.Progress
	}
	return run
}

func runIndex(runs []workflowRunView, task string) int {
	return slices.IndexFunc(runs, func(r workflowRunView) bool { return r.Task == task })
}

func runFor(runs []workflowRunView, task string) (workflowRunView, bool) {
	if at := runIndex(runs, task); at >= 0 {
		return runs[at], true
	}
	return workflowRunView{}, false
}

// workflowReplied folds a workflow reply. Every workflow reply kind comes
// through here, so app.go's apply spends one line on all of them.
func (a App) workflowReplied(f rpc.Frame) App {
	var runs []core.WorkflowRun
	if f.Workflow != nil {
		runs = f.Workflow.Runs
	}
	disk := make(map[string][]core.WorkflowRun, len(a.workflow.disk)+1)
	maps.Copy(disk, a.workflow.disk)
	disk[f.SessionID] = runs
	a.workflow.disk = disk
	return a.settleWorkflow()
}

// askWorkflows queues a FrameWorkflows per session; takeWorkflowAsks writes them.
func (a App) askWorkflows(ids ...string) App {
	a.workflow.asks = append(slices.Clone(a.workflow.asks), ids...)
	return a
}

func (a App) takeWorkflowAsks() (App, tea.Cmd) {
	if len(a.workflow.asks) == 0 {
		return a, nil
	}
	frames := make([]rpc.Frame, 0, len(a.workflow.asks))
	for _, id := range a.workflow.asks {
		frames = append(frames, rpc.Frame{Kind: rpc.FrameWorkflows, SessionID: id})
	}
	a.workflow.asks = nil
	return a, a.write(workflowRunsFailed, frames...)
}

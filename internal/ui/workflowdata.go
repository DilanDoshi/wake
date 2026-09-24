package ui

// What the /workflows view draws from: the workflow rows this client has
// folded live (Fleet.tasks) and each session's run records the daemon reads
// off disk (FrameWorkflows / FrameWorkflowsReply), one run per task id - and,
// at the agent level, that agent's own transcript (FrameWorkflowAgent / Reply).

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

// workflowRunsFailed and workflowAgentFailed name the write that could not
// happen, sendFailed's pattern.
const (
	workflowRunsFailed  = "asking for workflow runs"
	workflowAgentFailed = "asking for a workflow agent's transcript"
)

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
	if f.Kind == rpc.FrameWorkflowAgentReply {
		return a.workflowAgentReplied(f)
	}
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
	var agent tea.Cmd
	if a.workflow.agentAsk.Kind != "" {
		agent = a.write(workflowAgentFailed, a.workflow.agentAsk)
		a.workflow.agentAsk = rpc.Frame{}
	}
	if len(a.workflow.asks) == 0 {
		return a, agent
	}
	frames := make([]rpc.Frame, 0, len(a.workflow.asks))
	for _, id := range a.workflow.asks {
		frames = append(frames, rpc.Frame{Kind: rpc.FrameWorkflows, SessionID: id})
	}
	a.workflow.asks = nil
	return a, tea.Batch(a.write(workflowRunsFailed, frames...), agent)
}

// --- the agent level --------------------------------------------------------

// transcriptKey is one agent's transcript: its session, then its own id.
func transcriptKey(session, agentID string) [2]string { return [2]string{session, agentID} }

// openAgent is the agent the view's agent level is on, and the run it is in.
func (a App) openAgent() (workflowRunView, core.WorkflowAgent, bool) {
	v := a.workflow.view
	if v.Level != levelAgent {
		return workflowRunView{}, core.WorkflowAgent{}, false
	}
	run, ok := runFor(a.workflowRuns(v.Session), v.Task)
	if !ok {
		return workflowRunView{}, core.WorkflowAgent{}, false
	}
	at := slices.IndexFunc(run.Snap.Agents, func(ag core.WorkflowAgent) bool { return ag.Index == v.Detail })
	if at < 0 {
		return run, core.WorkflowAgent{}, false
	}
	return run, run.Snap.Agents[at], true
}

// reaskWorkflowAgent owes one FrameWorkflowAgent while the open agent has moved
// since its last - so the re-read is an event, a new snapshot, never a timer.
// An agent with no id yet cannot be asked for; its first snapshot with one is
// a move, so it is asked for then.
func (a App) reaskWorkflowAgent() App {
	run, ag, ok := a.openAgent()
	if !ok || ag.AgentID == "" || sameProgress(ag, a.workflow.asked) {
		return a
	}
	a.workflow.asked = ag
	a.workflow.agentAsk = rpc.Frame{Kind: rpc.FrameWorkflowAgent, SessionID: run.Session,
		Workflow: &rpc.WorkflowFrame{Agent: ag.AgentID}}
	return a
}

// reaskOnProgress is observe's half of the re-ask: only a task frame carries a
// snapshot, so every other event - a streamed token above all - costs nothing.
func (a App) reaskOnProgress(ev core.Event) App {
	if ev.Task == nil {
		return a
	}
	return a.reaskWorkflowAgent()
}

// sameProgress is the part of an agent's entry that says its transcript grew.
func sameProgress(x, y core.WorkflowAgent) bool {
	return x.AgentID == y.AgentID && x.ToolCalls == y.ToolCalls && x.State == y.State && x.Tokens == y.Tokens
}

// workflowAgentReplied keeps one agent's transcript in place of the last. An
// empty one is kept too - a transcript missing or not yet written - and the
// agent level says its activity is unavailable rather than reporting anything.
func (a App) workflowAgentReplied(f rpc.Frame) App {
	if f.Workflow == nil {
		return a
	}
	kept := make(map[[2]string][]core.Event, len(a.workflow.transcripts)+1)
	maps.Copy(kept, a.workflow.transcripts)
	kept[transcriptKey(f.SessionID, f.Workflow.Agent)] = f.Events
	a.workflow.transcripts = kept
	return a
}

// agentScrollLimit is how far the open agent scrolls in the pane the view is
// drawn in - the one measure the draw clamps by.
func (a App) agentScrollLimit() int {
	run, ag, ok := a.openAgent()
	w, h, drawn := a.paneSize(a.workflow.view.Pane)
	if !ok || !drawn {
		return 0
	}
	events := a.workflow.transcripts[transcriptKey(run.Session, ag.AgentID)]
	return agentLayout(ag, events, a.workflow.view.Expanded, w, h-workflowTitleRows).limit
}

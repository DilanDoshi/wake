package ui

// A subagent's transcript, read back off claude's own file when a pane opens on
// it. A client that attached after the subagent began was handed its
// task_started and nothing it said (daemon/taskreplay.go), so the row is
// openable and the pane would have nothing to draw; fleetsubs.go's backlog is
// only what this client saw.
//
// Asked on every open, and answered under history.go's rules: dropped if a
// frame arrived since the ask or a /clear came, and replacing rather than
// merging. It is also dropped unless longer than what the pane holds, the
// daemon's read being a bounded tail a client that watched from the start can
// outgrow. The window history.go has remains: a line on disk whose live copy
// has not yet arrived can be drawn twice, as no live event carries an identity
// to tell them apart by.

import (
	"cmp"
	"maps"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// subagentFailed names the write that could not happen, historyFailed's way.
const subagentFailed = "asking for a subagent's transcript"

// subKey names one subagent's transcript: its session, then its own id - the
// task id, which is also what claude names the file by.
type subKey struct{ session, agent string }

// subAsk is what an open recorded when it asked: the dispatch the answer is
// filed under, how many events the pane held - which makes "nothing arrived
// since" checkable - and the ask's number, so only the newest ask's answer lands.
type subAsk struct {
	dispatch string
	held     int
	number   uint64
}

// subTranscripts is the asks outstanding, the ones Update has yet to write, and
// the last number handed out, all copied on write for App.dms' reason.
type subTranscripts struct {
	asked   map[subKey]subAsk
	pending []rpc.Frame
	last    uint64
}

// askSubagent queues a FrameSubagent for the dispatch a pane has just been
// opened on. Only a running subagent has a file of its own to read: the
// conversation itself is History's, and a shell or a workflow has none.
func (a App) askSubagent(session, dispatch string) App {
	d, ok := a.dms[session]
	if !ok || dispatch == "" {
		return a
	}
	row, ok := a.runningAgent(session, dispatch)
	if !ok || rpc.ValidAgentID(row.ID) != nil {
		return a
	}
	number := a.subTranscripts.last + 1
	asked := make(map[subKey]subAsk, len(a.subTranscripts.asked)+1)
	maps.Copy(asked, a.subTranscripts.asked)
	asked[subKey{session, row.ID}] = subAsk{dispatch: dispatch, held: d.subs[dispatch].len(), number: number}
	a.subTranscripts = subTranscripts{
		asked: asked,
		pending: append(slices.Clone(a.subTranscripts.pending), rpc.Frame{
			Kind: rpc.FrameSubagent, SessionID: session, Workflow: &rpc.WorkflowFrame{Agent: row.ID, Ask: number},
		}),
		last: number,
	}
	return a
}

// runningAgent is the subagent row a session's dispatch names.
func (a App) runningAgent(session, dispatch string) (Task, bool) {
	for _, row := range a.fleet.RunningTasks(session) {
		if row.Kind == core.TaskAgent && row.Dispatch == dispatch {
			return row, true
		}
	}
	return Task{}, false
}

// takeSubagentAsks drains what an open queued into one command, takeHistoryAsks'
// reason: an open is reached from places that cannot return one.
func (a App) takeSubagentAsks() (App, tea.Cmd) {
	if len(a.subTranscripts.pending) == 0 {
		return a, nil
	}
	cmd := a.write(subagentFailed, a.subTranscripts.pending...)
	a.subTranscripts.pending = nil
	return a, cmd
}

// subagentArrived folds the daemon's answer into the pane it was asked for.
func (a App) subagentArrived(f rpc.Frame) App {
	if f.Workflow == nil {
		return a
	}
	key := subKey{f.SessionID, f.Workflow.Agent}
	ask, asked := a.subTranscripts.asked[key]
	if !asked || ask.number != f.Workflow.Ask {
		return a // nobody's, or an older open's: the newer ask is still owed its own
	}
	remaining := maps.Clone(a.subTranscripts.asked)
	delete(remaining, key)
	a.subTranscripts.asked = remaining
	d, ok := a.dms[f.SessionID]
	if !ok || d.subs[ask.dispatch].len() != ask.held || len(f.Events) <= ask.held {
		return a
	}
	row, _ := a.fleet.tasks[f.SessionID].forID(key.agent)
	return a.withDM(f.SessionID, d.withSubTranscript(ask.dispatch, asSubagent(f.Events, ask.dispatch, row)))
}

// forgetSubagentAsks drops every ask for a session - a /clear's, for
// forgetHistoryAsk's reason: the pane it blanks is back at the count the ask
// recorded, and would read as "nothing arrived since".
func (a App) forgetSubagentAsks(session string) App {
	asked := make(map[subKey]subAsk, len(a.subTranscripts.asked))
	for k, v := range a.subTranscripts.asked {
		if k.session != session {
			asked[k] = v
		}
	}
	a.subTranscripts.asked = asked
	return a
}

// asSubagent puts on a sidechain line what a forwarded frame carries and the
// line does not - the dispatch it is filed under, and the type and task the row
// knows - so it draws as a live one does.
func asSubagent(events []core.Event, dispatch string, row Task) []core.Event {
	out := make([]core.Event, len(events))
	for i, ev := range events {
		ev.Subagent = &core.Subagent{Dispatch: dispatch, Type: row.Type, Task: cmp.Or(row.Name, row.Label)}
		out[i] = ev
	}
	return out
}

// withSubTranscript replaces one dispatch's frames with the daemon's read and
// draws them if that is the transcript on screen. A reader scrolled back stays
// where they are, appendForwarded's rule.
func (d DM) withSubTranscript(dispatch string, events []core.Event) DM {
	d = d.withSubBacklog(dispatch, events)
	if d.viewing != dispatch {
		return d
	}
	following := d.tr.atBottom()
	d.tr = d.rewrapped()
	if following {
		d.tr = d.tr.toBottom()
	}
	return d
}

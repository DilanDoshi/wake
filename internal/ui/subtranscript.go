package ui

// A subagent's transcript, read back off claude's own file when a pane opens
// on it.
//
// A subagent's frames reach only the clients attached while it spoke, and a
// client that attaches later (a reattach, a second window) is handed the
// dispatch's task_started and nothing it said. So the row is there and
// openable, and the pane has nothing to draw - fleetsubs.go's backlog is only
// what this client saw. claude writes the subagent's lines to its own file, so
// opening the pane asks the daemon for it, the way history.go asks for the
// conversation's, and draws what comes back.
//
// Asked on every open, never once: the fleet's copy this pane is seeded from is
// whatever this client saw, so an earlier answer is no better than it.
//
// The answer follows history.go's rules. It is dropped if a frame arrived since
// the ask - the same line is then in the live copy and on disk, and the two
// cannot be told apart. And it replaces rather than merges, because the file
// has every line the subagent streamed. It is also dropped unless it is longer
// than what the pane already holds: the daemon's read is a bounded tail, so a
// client that watched a long subagent from the start holds more than it, and
// shrinking that to the tail would be the regression.

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
// filed under, and how many events the pane held, which is what makes "nothing
// has arrived since" checkable when the answer comes back.
type subAsk struct {
	dispatch string
	held     int
}

// subTranscripts is the asks outstanding and the ones Update has yet to write,
// both copied on write for App.dms' reason.
type subTranscripts struct {
	asked   map[subKey]subAsk
	pending []rpc.Frame
}

// askSubagent queues a FrameSubagent for the dispatch a pane has just been
// opened on. Only a subagent has a file of its own to read: an empty dispatch
// is the conversation itself, which history.go reads, and anything else a row
// carries - a shell, a workflow - has none under an id the daemon would take.
func (a App) askSubagent(session, dispatch string) App {
	d, ok := a.dms[session]
	if !ok || dispatch == "" {
		return a
	}
	row, ok := a.fleet.agentRow(session, dispatch)
	if !ok || rpc.ValidAgentID(row.ID) != nil {
		return a
	}
	asked := make(map[subKey]subAsk, len(a.subTranscripts.asked)+1)
	maps.Copy(asked, a.subTranscripts.asked)
	asked[subKey{session, row.ID}] = subAsk{dispatch: dispatch, held: d.subs[dispatch].len()}
	a.subTranscripts = subTranscripts{
		asked: asked,
		pending: append(slices.Clone(a.subTranscripts.pending), rpc.Frame{
			Kind: rpc.FrameSubagent, SessionID: session, Workflow: &rpc.WorkflowFrame{Agent: row.ID},
		}),
	}
	return a
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
	if !asked {
		return a
	}
	a = a.forgetSubagentAsk(key)
	d, ok := a.dms[f.SessionID]
	if !ok || d.subs[ask.dispatch].len() != ask.held || len(f.Events) <= ask.held {
		return a
	}
	row, _ := a.fleet.tasks[f.SessionID].forID(key.agent)
	return a.withDM(f.SessionID, d.withSubTranscript(ask.dispatch, asSubagent(f.Events, ask.dispatch, row)))
}

// forgetSubagentAsk drops one ask, so a second answer to it is ignored.
func (a App) forgetSubagentAsk(key subKey) App {
	asked := maps.Clone(a.subTranscripts.asked)
	delete(asked, key)
	a.subTranscripts.asked = asked
	return a
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

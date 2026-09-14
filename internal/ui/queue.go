package ui

// Type-ahead: a message typed while its agent is working waits here rather than
// going to the wire, and is delivered when the agent's turn ends.
//
// # Why the message is held and not sent
//
// Claude Code's stdin queues a message written mid-turn, but only reliably for
// one the *sender* stamped with a uuid - and EncodeUserMessage stamps none, so
// what happens to an un-stamped line written mid-turn is unrecorded (the CLI
// coalesces bursts into one result, docs/notes runtime findings §3). The safe
// answer is to never write mid-turn: a message reaches the agent only when it is
// idle, the same path a first message takes, which is the one that works. That
// is the whole of the "the agent doesn't even see the message" fix.
//
// # Where it lives, and why that is not "Wake owns state"
//
// Per window, keyed by session id, copy-on-write like App.quitting - it is
// transient UI state the way a draft or a selection is, never persisted, lost on
// close exactly as Claude Code loses its own queue. The daemon holds nothing of
// it; a crash costs an unsent message, which was never committed.
//
// # The flush is edge-triggered, which is what serialises it
//
// flushQueued fires one message on an agent's working→idle edge (see the
// frameMsg case). One per edge means each queued message reaches the agent as
// its own turn, one after another, so a burst never coalesces. esc rides this
// for free: an interrupt aborts the turn, the agent reports idle, and the next
// queued message flushes - "esc interrupts and lets the next queued message
// through", with no branch of its own.

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// queuedGlyph marks a waiting message in the pin above the composer.
const queuedGlyph = "⧗"

// maxQueuedPinRows bounds the pin: a deep queue would otherwise take rows from
// the transcript without limit and make the pane taller than the terminal, the
// alt-screen overflow the chrome accounting exists to prevent. Past it the last
// row counts the rest (`+N more`), the room working line's own pattern.
const maxQueuedPinRows = 3

// queuedMsg is one message waiting for an agent to finish its turn. wire is what
// reaches the agent (chip- and mention-stripped, as sendDM/sendRoom produce);
// echo is what the transcript draws (as typed). fromRoom marks a broadcast, so
// its held-DM echo heads `› you · from the room` and its provenance is public.
type queuedMsg struct {
	wire     string
	echo     string
	images   []core.ImageBlock
	fromRoom bool
}

// shouldQueue is whether a message to this agent must wait rather than go now:
// its turn is in flight, or it already has messages queued (a later one must
// land behind them, never jump the order).
func (a App) shouldQueue(id string) bool {
	if len(a.queued[id]) > 0 {
		return true
	}
	agent, ok := a.fleet.Agent(id)
	return ok && turnInFlight(agent.State)
}

// enqueue appends one message to an agent's queue, copy-on-write for the reason
// App.dms is: a discarded App must keep the queue it had.
func (a App) enqueue(id string, msg queuedMsg) App {
	next := make(map[string][]queuedMsg, len(a.queued)+1)
	for k, v := range a.queued {
		next[k] = v
	}
	next[id] = append(slices.Clone(next[id]), msg)
	a.queued = next
	return a
}

// dequeue pops the oldest message for an agent. The bool is false when there was
// none, which flushQueued reads rather than indexing an empty slice.
func (a App) dequeue(id string) (App, queuedMsg, bool) {
	q := a.queued[id]
	if len(q) == 0 {
		return a, queuedMsg{}, false
	}
	msg := q[0]
	next := cloneQueue(a.queued)
	if len(q) == 1 {
		delete(next, id)
	} else {
		next[id] = slices.Clone(q[1:])
	}
	a.queued = next
	return a, msg, true
}

// dropQueue forgets everything queued for an agent that can no longer receive
// it - one that ended or parked while its queue waited.
func (a App) dropQueue(id string) App {
	if _, held := a.queued[id]; !held {
		return a
	}
	next := cloneQueue(a.queued)
	delete(next, id)
	a.queued = next
	return a
}

// cloneQueue copies the queue map. A local rather than maps.Clone so the slices
// stay shared until enqueue clones the one it grows - a flush that only deletes
// a key copies no message.
func cloneQueue(m map[string][]queuedMsg) map[string][]queuedMsg {
	next := make(map[string][]queuedMsg, len(m))
	for k, v := range m {
		next[k] = v
	}
	return next
}

// flushQueued delivers one waiting message to each agent that just finished a
// turn, and drops the queue of any that ended or parked while it waited.
//
// prev is the fleet before this report folded, so a genuine working→idle edge is
// told from an agent that was already idle - the latter has nothing to flush
// (its queue only fills while it is in flight). One message per edge is what
// keeps a burst from coalescing: the delivered message starts a turn, and the
// next waits for that turn's own idle.
func (a App) flushQueued(prev Fleet) (App, tea.Cmd) {
	if len(a.queued) == 0 {
		return a, nil
	}
	ids := make([]string, 0, len(a.queued))
	for id := range a.queued {
		ids = append(ids, id)
	}
	var cmds []tea.Cmd
	for _, id := range ids {
		agent, ok := a.fleet.Agent(id)
		switch {
		case !ok || agent.State == rpc.StateEnded || agent.State == rpc.StateParked:
			a = a.dropQueue(id)
		case turnInFlight(prev.agents[id].State) && agent.State == rpc.StateIdle:
			var (
				msg queuedMsg
				had bool
			)
			if a, msg, had = a.dequeue(id); had {
				var cmd tea.Cmd
				a, cmd = a.deliver(id, msg)
				cmds = append(cmds, cmd)
			}
		}
	}
	if len(cmds) == 0 {
		return a, nil
	}
	return a, tea.Batch(cmds...)
}

// deliver sends one held message and echoes it, the way an immediate send would
// have. sending records the provenance so the room draws a broadcast and leaves
// a DM turn in its own pane; the echo lands only in a held DM, echoToRouted's
// own rule, since the room's line was drawn when the message was typed.
func (a App) deliver(id string, msg queuedMsg) (App, tea.Cmd) {
	a.fleet = a.fleet.sending(id, !msg.fromRoom)
	if d, held := a.dms[id]; held {
		nd := d.Append(core.Event{Kind: core.KindUserText, SessionID: id, Text: msg.echo, FromRoom: msg.fromRoom})
		a = a.withDM(id, nd)
	}
	return a, a.write(sendFailed, rpc.Frame{Kind: rpc.FrameSend, SessionID: id, Text: msg.wire, Images: msg.images})
}

// queuedTexts is the echo text of each message waiting for an agent, oldest
// first, for the pin above its composer. nil for an agent with none.
func (a App) queuedTexts(id string) []string {
	q := a.queued[id]
	if len(q) == 0 {
		return nil
	}
	out := make([]string, len(q))
	for i, m := range q {
		out[i] = m.echo
	}
	return out
}

// queuedRows is how many rows the pin draws, bounded by maxQueuedPinRows and
// counted rather than drawn for baseChrome, which runs on every re-lay. Exact
// because each waiting message is one truncated line and the overflow is one
// more.
func (d DM) queuedRows() int { return min(len(d.queued), maxQueuedPinRows) }

// queuedPin is the waiting messages above the composer, each a dim ⧗ line
// truncated to the width, and "" for a conversation with none. Past the cap the
// last row counts the rest. Where Claude Code shows a queued message: below the
// input, moving into the transcript when its turn comes (deliver appends the
// real echo then).
func (d DM) queuedPin(width int) string {
	if len(d.queued) == 0 {
		return ""
	}
	w := max(width, 1)
	line := func(s string) string {
		return HintStyle.Render(ansi.Truncate(queuedGlyph+" "+oneLine(s), w, ellipsis))
	}
	if len(d.queued) <= maxQueuedPinRows {
		rows := make([]string, len(d.queued))
		for i, text := range d.queued {
			rows[i] = line(text)
		}
		return strings.Join(rows, "\n")
	}
	rows := make([]string, maxQueuedPinRows)
	for i := 0; i < maxQueuedPinRows-1; i++ {
		rows[i] = line(d.queued[i])
	}
	rows[maxQueuedPinRows-1] = HintStyle.Render(ansi.Truncate(
		fmt.Sprintf("%s +%d more queued", queuedGlyph, len(d.queued)-(maxQueuedPinRows-1)), w, ellipsis))
	return strings.Join(rows, "\n")
}

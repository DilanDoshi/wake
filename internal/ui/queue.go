package ui

// Type-ahead: a message typed while its agent is working waits here rather than
// going to the wire, and is delivered once the agent is free again.
//
// # Why the message is held and not sent
//
// A message written to a busy stdin is one the CLI coalesces or drops - the
// unstamped-mid-turn behaviour is unrecorded (docs/notes runtime findings §3) -
// so the agent may never see it. The safe answer is to never write mid-turn: a
// message reaches the agent only when it is free, the same path a first message
// takes. That is the whole of the "the agent doesn't even see the message" fix.
//
// # Knowing when the agent is free, deterministically
//
// Agent.State lags: the daemon sets a turn owed the instant it writes, but only
// reports "working" when the agent emits its first event (its init, seconds
// later). So a fast follow-up typed before that report would see "idle" and race
// onto a busy stdin. Wake instead stamps every message it sends with a uuid
// (rpc.Frame.MessageID → core.EncodeUserMessage), which makes the CLI emit a
// command_lifecycle for it (core.KindMessageState): App.inflight is set the
// instant a message is dispatched and cleared by that message's
// completed/cancelled lifecycle. shouldQueue reads it, so a follow-up queues at
// once rather than lagging behind a status report.
//
// # The flush, and its backstop
//
// An agent is free when it holds no in-flight Wake message and the daemon reports
// it idle. flushQueued delivers one message to each free agent with a queue - one
// per turn, so a burst never coalesces. The primary "message over" signal is the
// lifecycle (observeMessageState); the State working→idle edge is a gap backstop,
// clearing a stranded inflight if that lifecycle frame was lost. esc rides this
// for free: an interrupt cancels the running message, its lifecycle reads
// cancelled, and the next queued message flushes.
//
// # Where it lives
//
// Per window, keyed by session id, copy-on-write like App.quitting - transient UI
// state the way a draft is, never persisted, lost on close exactly as Claude Code
// loses its own queue.

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

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

// queuedMsg is one message waiting for an agent to be free. id is the uuid it is
// stamped with, so its command_lifecycle can be matched back; wire is what
// reaches the agent (chip- and mention-stripped, as sendDM/sendRoom produce);
// echo is what the transcript draws (as typed). fromRoom marks a broadcast, so
// its held-DM echo heads `from the room` and its provenance is public.
type queuedMsg struct {
	id       string
	wire     string
	echo     string
	images   []core.ImageBlock
	fromRoom bool
}

// newQueued builds a message with a freshly minted uuid, so an immediate send and
// a queued one are stamped the same way.
func newQueued(wire, echo string, images []core.ImageBlock, fromRoom bool) queuedMsg {
	return queuedMsg{id: uuid.NewString(), wire: wire, echo: echo, images: images, fromRoom: fromRoom}
}

// shouldQueue is whether a message to this agent must wait rather than go now: it
// has a Wake message still in flight, its turn is in flight per the daemon, or it
// already has messages queued (a later one lands behind them, never ahead).
func (a App) shouldQueue(id string) bool {
	if a.inflight[id] != "" || len(a.queued[id]) > 0 {
		return true
	}
	agent, ok := a.fleet.Agent(id)
	return ok && turnInFlight(agent.State)
}

// agentFree is whether a queued message may go now: no Wake message in flight and
// the daemon reports the agent idle. Idle explicitly rather than !turnInFlight, so
// an agent whose state this client has not yet seen waits for a real idle report.
func (a App) agentFree(id string) bool {
	agent, ok := a.fleet.Agent(id)
	return a.inflight[id] == "" && ok && agent.State == rpc.StateIdle
}

// enqueue appends one message to an agent's queue, copy-on-write for the reason
// App.dms is: a discarded App must keep the queue it had.
func (a App) enqueue(id string, msg queuedMsg) App {
	next := cloneQueue(a.queued)
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

// dropQueue forgets everything queued for an agent that can no longer receive it,
// and any in-flight uuid it held - one that ended or parked while its queue waited.
func (a App) dropQueue(id string) App {
	if _, held := a.queued[id]; held {
		next := cloneQueue(a.queued)
		delete(next, id)
		a.queued = next
	}
	return a.clearInflight(id)
}

// cloneQueue copies the queue map. A local rather than maps.Clone so the slices
// stay shared until enqueue clones the one it grows - a flush that only deletes a
// key copies no message.
func cloneQueue(m map[string][]queuedMsg) map[string][]queuedMsg {
	next := make(map[string][]queuedMsg, len(m))
	for k, v := range m {
		next[k] = v
	}
	return next
}

// withInflight records the uuid of the message just dispatched to an agent, and
// clearInflight forgets it once that message's turn is over. Copy-on-write.
func (a App) withInflight(id, msgID string) App {
	next := make(map[string]string, len(a.inflight)+1)
	for k, v := range a.inflight {
		next[k] = v
	}
	next[id] = msgID
	a.inflight = next
	return a
}

func (a App) clearInflight(id string) App {
	if _, held := a.inflight[id]; !held {
		return a
	}
	next := make(map[string]string, len(a.inflight))
	for k, v := range a.inflight {
		if k != id {
			next[k] = v
		}
	}
	a.inflight = next
	return a
}

// observeMessageState clears an agent's in-flight mark when the message it named
// completes or is cancelled - the deterministic "the turn is over" signal, ahead
// of the State-edge backstop. Ignores a lifecycle for a uuid this window did not
// send (another client's, or a stale one).
func (a App) observeMessageState(id string, ev core.Event) App {
	if ev.MessageEnded() && ev.MessageID != "" && ev.MessageID == a.inflight[id] {
		return a.clearInflight(id)
	}
	return a
}

// markSent records a dispatched message - the in-flight uuid, the room/DM
// provenance, and the transcript echo - without writing it. The write is the
// caller's, so a broadcast's frames go out as one command (send.go's rule).
func (a App) markSent(id string, msg queuedMsg) App {
	a = a.withInflight(id, msg.id)
	a.fleet = a.fleet.sending(id, !msg.fromRoom)
	if d, held := a.dms[id]; held {
		nd := d.Append(core.Event{Kind: core.KindUserText, SessionID: id, Text: msg.echo, FromRoom: msg.fromRoom})
		a = a.withDM(id, nd)
	}
	return a
}

// sendFrame is the wire frame for one message, carrying its stamped uuid so the
// CLI's command_lifecycle names it.
func sendFrame(id string, msg queuedMsg) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameSend, SessionID: id, Text: msg.wire, Images: msg.images, MessageID: msg.id}
}

// flushQueued delivers one waiting message to each agent that is now free, drops
// the queue of any that ended or parked, and applies the State-edge backstop for a
// lifecycle frame that never arrived.
//
// prev is the fleet before this batch folded, so the backstop fires on a genuine
// working→idle edge rather than an agent that was already idle. One message per
// free agent per call keeps a burst from coalescing: the delivered message sets
// inflight, so the agent is no longer free until its turn ends.
func (a App) flushQueued(prev Fleet) (App, tea.Cmd) {
	// Both empty is the common idle case, and it must stay free. When either has
	// an entry the backstop below runs even for an agent with no queue, because an
	// in-flight mark left standing after a turn ends would wrongly queue the next
	// message forever - so inflight is reconciled whether or not anything waits.
	if len(a.queued) == 0 && len(a.inflight) == 0 {
		return a, nil
	}
	seen := make(map[string]struct{}, len(a.queued)+len(a.inflight))
	ids := make([]string, 0, len(a.queued)+len(a.inflight))
	collect := func(id string) {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	for id := range a.queued {
		collect(id)
	}
	for id := range a.inflight {
		collect(id)
	}
	var frames []rpc.Frame
	for _, id := range ids {
		agent, ok := a.fleet.Agent(id)
		if !ok || agent.State == rpc.StateEnded || agent.State == rpc.StateParked {
			a = a.dropQueue(id) // also clears any inflight mark
			continue
		}
		// Backstop: our in-flight message's turn ended per the daemon's edge, in
		// case its completed lifecycle was lost to a frame gap. Runs regardless of
		// whether a message waits, so the mark never strands the next one.
		if a.inflight[id] != "" && turnInFlight(prev.agents[id].State) && agent.State == rpc.StateIdle {
			a = a.clearInflight(id)
		}
		if len(a.queued[id]) == 0 || !a.agentFree(id) {
			continue
		}
		var (
			msg queuedMsg
			had bool
		)
		if a, msg, had = a.dequeue(id); had {
			a = a.markSent(id, msg)
			frames = append(frames, sendFrame(id, msg))
		}
	}
	if len(frames) == 0 {
		return a, nil
	}
	return a, a.write(sendFailed, frames...)
}

// queuedTexts is the echo text of each message waiting for an agent, oldest first,
// for the pin above its composer. nil for an agent with none.
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
// because each waiting message is one truncated line and the overflow is one more.
func (d DM) queuedRows() int { return min(len(d.queued), maxQueuedPinRows) }

// queuedPin is the waiting messages above the composer, each a dim ⧗ line
// truncated to the width, and "" for a conversation with none. Past the cap the
// last row counts the rest. Where Claude Code shows a queued message: below the
// input, moving into the transcript when its turn comes (markSent appends the real
// echo then).
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

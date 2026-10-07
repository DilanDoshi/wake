package ui

// Type-ahead: a message typed while its agent is working goes to claude at once,
// and claude queues it - read at the next tool boundary in the running turn, or
// as the next turn if this one ends first; a command waits for the turn to end.
//
// # Why it is written rather than held
//
// Wake used to hold every such message until the agent was idle, on the
// assumption that a line written to a busy stdin is coalesced or dropped. That
// was never recorded; recording it (claude 2.1.288,
// docs/superpowers/notes/2026-10-02-mid-turn-delivery-findings.md) showed the
// opposite: claude queues the line, says so (command_lifecycle "queued"), and
// takes it up between tool calls (midturn-absent.jsonl). Holding it only kept
// the agent from reading it - the steer arrived after the work it was meant to
// steer.
//
// # Pinned until claude takes it up
//
// What is written but not yet taken up is pinned above the composer, as Claude
// Code lists its queue. Its "started" lifecycle moves it into the conversation
// at that point - where the model read it. A lost "started" is backstopped by
// the message's "completed" and by the turn end naming it (Event.Answered). Until
// it starts it can be taken back or hurried (recall.go).
//
// # Busy, deterministically
//
// An idle agent's message is drawn at once. Agent.State lags a send by seconds,
// so busy also reads inflight - every Wake message claude has started and not
// finished, set the instant one is sent to an idle agent - and a queue that is
// not empty. The State working→idle edge clears inflight if a lifecycle was lost.
//
// # The one message still held
//
// A /rename to a busy agent is held here, not written: its mirror renames Wake
// when the passthrough goes, and the daemon holds that want until claude's reply,
// one at a time (renamesync.go). It goes out one per turn once the agent is free,
// the mechanism every message used before.
//
// Per window, keyed by session id, copy-on-write like App.quitting - transient UI
// state, never persisted, lost on close as Claude Code loses its own queue.

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// queuedGlyph marks a waiting message in the pin above the composer.
const queuedGlyph = "⧗"

// maxQueuedPinRows bounds the pin: a deep queue would otherwise take rows from
// the transcript without limit and make the pane taller than the terminal, the
// alt-screen overflow the chrome accounting exists to prevent. Past it the last
// row counts the rest (`+N more`), the room working line's own pattern.
const maxQueuedPinRows = 3

// queuedMsg is one message waiting for claude to take it up. id is the uuid it
// is stamped with, so its command_lifecycle can be matched back; wire is what
// reaches the agent (chip- and mention-stripped, as sendDM/sendRoom produce);
// echo is what the transcript draws (as typed). fromRoom marks a broadcast, so
// its held-DM echo heads `from the room` and its provenance is public. rename is
// a `/rename`'s mirror name, "" for none. held is a /rename not yet written (see
// the header); recallID is the request id of a take-back on its way to claude,
// "" for none (recall.go).
type queuedMsg struct {
	id       string
	wire     string
	echo     string
	images   []core.ImageBlock
	fromRoom bool
	rename   string
	held     bool
	recallID string
}

// newQueued builds a message under the uuid it will be stamped with, minted at
// the send, so an immediate send and a queued one are stamped the same way: a
// random one for a DM, a room send's (roomprovenance.go) for the room.
func newQueued(id, wire, echo string, images []core.ImageBlock, fromRoom bool) queuedMsg {
	return queuedMsg{id: id, wire: wire, echo: echo, images: images, fromRoom: fromRoom}
}

// shouldQueue is whether a message to this agent waits in claude's queue rather
// than being read now: a Wake message is in flight, the daemon reports a turn,
// or something is already queued ahead of it.
func (a App) shouldQueue(id string) bool {
	if len(a.inflight[id]) > 0 || len(a.queued[id]) > 0 {
		return true
	}
	agent, ok := a.fleet.Agent(id)
	return ok && turnInFlight(agent.State)
}

// agentFree is whether a held /rename may go now: nothing in flight and the
// daemon reports the agent idle - idle explicitly, so an agent this client has
// not yet seen waits for a real report.
func (a App) agentFree(id string) bool {
	agent, ok := a.fleet.Agent(id)
	return len(a.inflight[id]) == 0 && ok && agent.State == rpc.StateIdle
}

// queue sends one message to a busy agent: written now and pinned, or, for a
// /rename, held with its mirror. The frame is nil when nothing is written.
func (a App) queue(id string, msg queuedMsg) (App, []rpc.Frame) {
	if msg.rename != "" {
		msg.held = true
		return a.enqueue(id, msg), nil
	}
	return a.enqueue(id, msg), []rpc.Frame{sendFrame(id, msg)}
}

// enqueue appends one message to an agent's queue, copy-on-write for the reason
// App.dms is: a discarded App must keep the queue it had.
func (a App) enqueue(id string, msg queuedMsg) App {
	return a.withQueue(id, append(slices.Clone(a.queued[id]), msg))
}

// withQueue replaces one agent's queue, dropping the key when it empties.
func (a App) withQueue(id string, q []queuedMsg) App {
	next := make(map[string][]queuedMsg, len(a.queued)+1)
	for k, v := range a.queued {
		next[k] = v
	}
	if len(q) == 0 {
		delete(next, id)
	} else {
		next[id] = q
	}
	a.queued = next
	return a
}

// unqueue takes one message out of an agent's queue by its uuid.
func (a App) unqueue(id, msgID string) (App, queuedMsg, bool) {
	q := a.queued[id]
	i := slices.IndexFunc(q, func(m queuedMsg) bool { return m.id == msgID })
	if i < 0 {
		return a, queuedMsg{}, false
	}
	msg := q[i]
	return a.withQueue(id, slices.Delete(slices.Clone(q), i, i+1)), msg, true
}

// dropQueue forgets everything queued for an agent that can no longer receive
// it - one that ended or parked - with its in-flight marks and any take-back.
func (a App) dropQueue(id string) App {
	if _, held := a.queued[id]; held {
		a = a.withQueue(id, nil)
	}
	return a.clearInflight(id).dropRecall(id)
}

// withInflight records a message claude has started for an agent, and
// clearInflight forgets them all. Copy-on-write; the inner sets are never
// shared between Apps either.
func (a App) withInflight(id, msgID string) App {
	return a.withInflightSet(id, func(s map[string]bool) { s[msgID] = true })
}

func (a App) clearInflight(id string) App {
	if len(a.inflight[id]) == 0 {
		return a
	}
	return a.withInflightSet(id, func(s map[string]bool) { clear(s) })
}

func (a App) withInflightSet(id string, edit func(map[string]bool)) App {
	next := make(map[string]map[string]bool, len(a.inflight)+1)
	for k, v := range a.inflight {
		next[k] = v
	}
	set := make(map[string]bool, len(next[id])+1)
	for k := range next[id] {
		set[k] = true
	}
	edit(set)
	if len(set) == 0 {
		delete(next, id)
	} else {
		next[id] = set
	}
	a.inflight = next
	return a
}

// forgetInflight drops what a reattach cannot confirm: every in-flight mark, and
// every written message claude may have taken up while this client was gone,
// with any take-back of them. A held /rename was never written, so it stays to
// go out on the next idle.
func (a App) forgetInflight() App {
	a.inflight = map[string]map[string]bool{}
	for id := range a.recalls {
		a = a.dropRecall(id)
	}
	for id, q := range a.queued {
		a = a.withQueue(id, slices.DeleteFunc(slices.Clone(q), func(m queuedMsg) bool { return !m.held }))
	}
	return a
}

// observeMessageState folds a message's lifecycle, a turn end's list of what it
// answered, and a take-back's receipt into the queue: taken up moves a pinned
// message into the conversation, cancelled is a take-back that was in time, and
// an ending takes it out of flight. What this window did not send is ignored.
func (a App) observeMessageState(id string, ev core.Event) App {
	var ended []string
	switch {
	case ev.MessageStarted():
		a = a.takenUp(id, ev.MessageID, true)
	case ev.MessageCancelled():
		a, ended = a.withdrawn(id, ev.MessageID), []string{ev.MessageID}
	case ev.MessageEnded():
		a, ended = a.takenUp(id, ev.MessageID, false), []string{ev.MessageID}
	case ev.Kind == core.KindTurnEnd:
		for _, msgID := range ev.Answered {
			a = a.takenUp(id, msgID, false)
		}
		ended = ev.Answered
	case ev.Kind == core.KindControlReceipt && ev.Control != nil && ev.Control.Recalled != nil:
		a = a.recallAnswered(id, ev.RequestID, *ev.Control.Recalled)
	}
	for _, msgID := range ended {
		if a.inflight[id][msgID] {
			a = a.withInflightSet(id, func(s map[string]bool) { delete(s, msgID) })
		}
	}
	return a.settleRecall(id)
}

// takenUp draws a pinned message claude has read, in flight when it is running
// now rather than already done.
func (a App) takenUp(id, msgID string, running bool) App {
	if q := a.queued[id]; !slices.ContainsFunc(q, func(m queuedMsg) bool { return m.id == msgID && !m.held }) {
		return a
	}
	a, msg, _ := a.unqueue(id, msgID)
	if running {
		a = a.withInflight(id, msgID)
	}
	return a.echoSent(id, msg)
}

// markSent records a message an idle agent takes now - in flight, and drawn -
// without writing it. The write is the caller's, so a broadcast's frames go out
// as one command (send.go's rule).
func (a App) markSent(id string, msg queuedMsg) App {
	return a.withInflight(id, msg.id).echoSent(id, msg)
}

// echoSent draws a message as said to an agent: in its held DM, and as the turn
// the room/DM provenance follows.
func (a App) echoSent(id string, msg queuedMsg) App {
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

// reconcileInflight clears an agent's in-flight marks when the daemon reports
// its turn over - a working→idle edge - or the agent gone: the backstop for a
// completed lifecycle lost to a frame gap, run per report (from applyStatus) so
// an edge that opens and closes inside one inbox drain is not collapsed. prev is
// the fleet before this one report folded.
func (a App) reconcileInflight(prev Fleet) App {
	for id := range a.inflight {
		agent, ok := a.fleet.Agent(id)
		switch {
		case !ok || agent.State == rpc.StateEnded || agent.State == rpc.StateParked:
			a = a.clearInflight(id)
		case turnInFlight(prev.agents[id].State) && agent.State == rpc.StateIdle:
			a = a.clearInflight(id)
		}
	}
	return a
}

// flushQueued writes each held /rename whose agent is now free, its mirror just
// ahead of it, one per agent per call, and drops the queue of any agent that
// ended or parked.
func (a App) flushQueued() (App, tea.Cmd) {
	var frames []rpc.Frame
	for id, q := range a.queued {
		agent, ok := a.fleet.Agent(id)
		if !ok || agent.State == rpc.StateEnded || agent.State == rpc.StateParked {
			a = a.dropQueue(id)
			continue
		}
		// Not while a take-back or send-now is out: its message goes from another
		// command, which could land between the mirror and the passthrough.
		i := slices.IndexFunc(q, func(m queuedMsg) bool { return m.held })
		if _, out := a.recalls[id]; i < 0 || out || !a.agentFree(id) {
			continue
		}
		var msg queuedMsg
		a, msg, _ = a.unqueue(id, q[i].id)
		a = a.markSent(id, msg)
		notice.Report(renameAsked, agentPrefix, agent.Name)
		frames = append(frames, renameFrame(id, msg.rename, true), sendFrame(id, msg))
	}
	if len(frames) == 0 {
		return a, nil
	}
	return a, a.write(sendFailed, frames...)
}

// pinText is the message as the queued pin draws it for its recipient. A room
// broadcast keeps its addressing @name in the echo - the transcript needs it
// under a "from the room" head (dm_blocks.go) - but in the recipient's own DM the
// pin has no such head, so a bare @<own name> reads as a stray self-mention.
// Strip exactly that: this agent's own leading @name, whole word, with a body
// after it. Everything else is kept as typed - a different agent's mention (an
// open-mode broadcast's @john, or @all) is context the terse pin still shows, a
// leading @word that only prefixes this agent's name or is a path is not its
// routing address, and a bare @name with no body keeps the name rather than
// stripping to nothing. Matching the exact name rather than re-parsing the echo
// with a display regex is what keeps it from clipping `@alexander` to `alex` or
// `@a/b` to `/b` - the failure the router's own whole-word grammar avoids.
func (m queuedMsg) pinText(recipient string) string {
	if !m.fromRoom || recipient == "" {
		return m.echo
	}
	rest, ok := strings.CutPrefix(m.echo, "@"+recipient)
	if !ok || rest == "" {
		return m.echo
	}
	if r, _ := utf8.DecodeRuneInString(rest); !unicode.IsSpace(r) {
		return m.echo
	}
	return strings.TrimLeftFunc(rest, unicode.IsSpace)
}

// queuedTexts is the pin text of each message waiting for an agent, oldest first,
// for the pin above its composer. nil for an agent with none. The agent's own
// name resolves the self-mention pinText strips from a room broadcast.
func (a App) queuedTexts(id string) []string {
	q := a.queued[id]
	if len(q) == 0 {
		return nil
	}
	name := ""
	if ag, ok := a.fleet.Agent(id); ok {
		name = ag.Name
	}
	out := make([]string, len(q))
	for i, m := range q {
		out[i] = m.pinText(name)
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
// input, moving into the transcript when claude takes it up (takenUp appends the
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

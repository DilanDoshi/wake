package ui

// Taking queued messages back, and sending them now - Claude Code's ↑ and its
// send-now over its queue.
//
// Both start by asking claude to give back what it has queued but not taken up
// (rpc.FrameRecall, cancel_async_message). Each message's own lifecycle says
// whether that was in time - "cancelled" gives it back, "started" means claude
// read it first, and it stays sent - and the receipt for its request id says so
// too, for when the lifecycle is lost (midturn-cancel.jsonl,
// midturn-cancel-late.jsonl). When none is still on its way back:
//
//   - ↑ puts what came back in the composer, one per line, oldest first, ahead
//     of the draft - Claude Code's "take back what you queued". A room broadcast
//     taken back leaves the room a record that it never reached that agent.
//   - send-now writes what came back and the draft as one message with priority
//     now: claude moves running work to the background and reads it in the same
//     turn (midturn-recall-now.jsonl). Taking the queue back first is not
//     optional - a now written behind a queued message ends the turn at the next
//     tool boundary instead (midturn-next-then-now.jsonl).
//
// A held /rename is never taken back (its mirror waits with it), and send-now
// leaves a queued command where it is: claude runs commands after the turn, and
// one folded into a message would be text.

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// noticeTakenBack is the room's record of a broadcast taken back from one
// agent, authored here as the question records are (cardroom.go): no frame
// carries it.
const noticeTakenBack core.Notice = "taken_back"

// takenBackClosed is said when what came back has no conversation to return to.
const takenBackClosed = "%s's conversation closed before claude gave this back: %s"

// recall is a take-back or send-now waiting on claude. back is what it has
// given back so far, in queue order; draft is a send-now's own message.
type recall struct {
	now   bool
	draft *queuedMsg
	back  []queuedMsg
}

// recallable is what a gesture may ask back: written, not already on its way,
// never a /rename, and for send-now never a command, which claude runs after the
// turn (midturn-slash.jsonl) rather than reading as a message.
func recallable(m queuedMsg, now bool) bool {
	return !m.held && m.recallID == "" && m.rename == "" && (!now || !leadingCommand(m.wire))
}

// startRecall marks what id's gesture asks back and returns the frames that
// ask. ok is false when there is nothing to ask for, or a recall is already out.
func (a App) startRecall(id string, r recall) (App, []rpc.Frame, bool) {
	if _, out := a.recalls[id]; out {
		return a, nil, false
	}
	q := slices.Clone(a.queued[id])
	var frames []rpc.Frame
	for i, m := range q {
		if recallable(m, r.now) {
			q[i].recallID = uuid.NewString()
			frames = append(frames, rpc.Frame{Kind: rpc.FrameRecall, SessionID: id, RequestID: q[i].recallID, MessageID: m.id})
		}
	}
	if len(frames) == 0 {
		return a, nil, false
	}
	next := make(map[string]recall, len(a.recalls)+1)
	for k, v := range a.recalls {
		next[k] = v
	}
	next[id] = r
	a.recalls = next
	return a.withQueue(id, q), frames, true
}

func (a App) withoutRecall(id string) App {
	if _, out := a.recalls[id]; !out {
		return a
	}
	next := make(map[string]recall, len(a.recalls))
	for k, v := range a.recalls {
		if k != id {
			next[k] = v
		}
	}
	a.recalls = next
	return a
}

// takeBack is ↑ in a conversation with messages queued: ask them all back. ok is
// false with nothing to take, so ↑ goes on to the prompt history; while one is
// already out ↑ waits for it rather than walking the history under it.
func (a App) takeBack(id string) (App, tea.Cmd, bool) {
	if _, out := a.recalls[id]; out {
		return a, nil, true
	}
	a, frames, ok := a.startRecall(id, recall{})
	if !ok {
		return a, nil, false
	}
	return a, a.write(sendFailed, frames...), true
}

// hurry is send-now's half of a send to a busy agent: ask back what is queued
// and send it with msg once claude answers, or send msg now if nothing is.
func (a App) hurry(id string, msg queuedMsg) (App, []rpc.Frame) {
	if next, frames, ok := a.startRecall(id, recall{now: true, draft: &msg}); ok {
		return next, frames
	}
	return a.enqueue(id, msg), []rpc.Frame{nowFrame(id, msg)}
}

func nowFrame(id string, msg queuedMsg) rpc.Frame {
	f := sendFrame(id, msg)
	f.Now = true
	return f
}

// withdrawn folds a message claude will never run. One a take-back asked for is
// kept for the gesture; any other is simply gone.
func (a App) withdrawn(id, msgID string) App {
	a, msg, ok := a.unqueue(id, msgID)
	r, out := a.recalls[id]
	if !ok || !out || msg.recallID == "" {
		return a
	}
	r.back = append(slices.Clone(r.back), msg)
	next := make(map[string]recall, len(a.recalls))
	for k, v := range a.recalls {
		next[k] = v
	}
	next[id] = r
	a.recalls = next
	return a
}

// unrecalled releases each message whose recall frame never reached the daemon:
// nothing will answer it, so it stays pinned as an ordinary queued message and
// its recall finishes with what did come back.
func (a App) unrecalled(unsent []rpc.Frame) App {
	for _, f := range unsent {
		q := a.queued[f.SessionID]
		i := slices.IndexFunc(q, func(m queuedMsg) bool { return m.recallID != "" && m.recallID == f.RequestID })
		if f.Kind != rpc.FrameRecall || i < 0 {
			continue
		}
		q = slices.Clone(q)
		q[i].recallID = ""
		a = a.withQueue(f.SessionID, q).settleRecall(f.SessionID)
	}
	return a
}

// claudeAnswered is whether claude has answered every message a recall asked for.
func (a App) claudeAnswered(id string) bool {
	return !slices.ContainsFunc(a.queued[id], func(m queuedMsg) bool { return m.recallID != "" })
}

// recallAnswered folds a take-back's receipt: the second record of claude's
// answer, for when the message's own lifecycle was lost to a gap.
func (a App) recallAnswered(id, requestID string, recalled bool) App {
	i := slices.IndexFunc(a.queued[id], func(m queuedMsg) bool { return m.recallID == requestID })
	if i < 0 {
		return a
	}
	msgID := a.queued[id][i].id
	if recalled {
		return a.withdrawn(id, msgID)
	}
	return a.takenUp(id, msgID, false)
}

// settleRecall finishes a take-back once claude has answered it: what came back
// goes into that conversation's composer. A send-now finishes in sendRecalled,
// which can write.
func (a App) settleRecall(id string) App {
	r, out := a.recalls[id]
	if !out || r.now || !a.claudeAnswered(id) {
		return a
	}
	return a.withoutRecall(id).putBack(id, r.back, nil)
}

// dropRecall ends a recall claude can no longer finish - its agent ended or
// parked, or this client reattached - returning what came back and a send-now's
// draft to the operator rather than letting them go nowhere.
func (a App) dropRecall(id string) App {
	r, out := a.recalls[id]
	if !out {
		return a
	}
	return a.withoutRecall(id).putBack(id, r.back, r.draft)
}

// putBack returns messages, and a draft that never went, to their
// conversation's composer, one per line ahead of what is typed there. A room
// broadcast among them leaves the room a record that it never reached the agent.
// With the conversation closed, a notice says what came back.
func (a App) putBack(id string, back []queuedMsg, draft *queuedMsg) App {
	agent, _ := a.fleet.Agent(id)
	for _, m := range back {
		if m.fromRoom {
			a = a.withRoom(a.room.Append(core.Event{Kind: core.KindSystem, SessionID: id, Notice: noticeTakenBack}, agent))
		}
	}
	if draft != nil {
		back = append(slices.Clone(back), *draft)
	}
	if len(back) == 0 {
		return a
	}
	if a.dms[id] == nil {
		notice.Report(takenBackClosed, agent.Name, joined(back, nil).wire)
		return a
	}
	c := a.dms[id].Composer()
	typed := c.Value()
	c = c.WithDraft("")
	for _, m := range back {
		c = c.InsertText(m.wire)
		for _, img := range m.images {
			c = c.Attach(img)
		}
		c = c.InsertText("\n")
	}
	if typed == "" {
		c = c.WithDraft(strings.TrimSuffix(c.Value(), "\n"))
	} else {
		c = c.InsertText(typed)
	}
	return a.withComposerFor(id, c)
}

// sendRecalled writes each send-now claude has answered: what came back and the
// draft, as one message sent now, pinned until claude takes it up.
func (a App) sendRecalled() (App, tea.Cmd) {
	var frames []rpc.Frame
	for id, r := range a.recalls {
		if !r.now || !a.claudeAnswered(id) {
			continue
		}
		a = a.withoutRecall(id)
		parts := r.back
		if r.draft != nil {
			parts = append(slices.Clone(parts), *r.draft)
		}
		if len(parts) == 0 {
			continue
		}
		msg := joined(parts, r.draft)
		a = a.enqueue(id, msg)
		frames = append(frames, nowFrame(id, msg))
	}
	if len(frames) == 0 {
		return a, nil
	}
	return a, a.write(sendFailed, frames...)
}

// joined is several queued messages as one, oldest first, under the draft's
// stamp and provenance when there is a draft.
func joined(parts []queuedMsg, draft *queuedMsg) queuedMsg {
	var wire, echo []string
	var images []core.ImageBlock
	for _, m := range parts {
		wire, echo = append(wire, m.wire), append(echo, m.echo)
		images = append(images, m.images...)
	}
	msg := newQueued(uuid.NewString(), strings.Join(wire, "\n"), strings.Join(echo, "\n"), images, parts[0].fromRoom)
	if draft != nil {
		msg.id, msg.fromRoom = draft.id, draft.fromRoom
	}
	return msg
}

// sendNow is ⌃]: the draft as a send-now, or with no draft, what is queued for
// the conversation's agent hurried on its own.
func (a App) sendNow() (tea.Model, tea.Cmd, bool) {
	if strings.TrimSpace(a.composer().Value()) != "" {
		m, cmd := a.submit(true)
		return m, cmd, true
	}
	if a.focus == "" {
		return a, nil, true
	}
	next, frames, ok := a.startRecall(a.focus, recall{now: true})
	if !ok {
		return a, nil, true
	}
	return next, next.write(sendFailed, frames...), true
}

package ui

// What ⌃Q leaves unsent. The daemon interrupts every agent on the way out and
// cancels what claude has queued behind its turn (shutdownstop.go); a cancelled
// message is written to no user record on disk, so neither ↑ nor a reopened pane
// brings it back. So the window names each message claude had not read at the
// press, and cmd/wake prints their words after the alt screen is gone, where
// scrollback keeps them.

import "slices"

// QueuedMessage is one message claude had not read when ⌃Q was pressed: its
// words as typed, and the agents it was waiting at.
type QueuedMessage struct {
	To   []string
	Text string
}

// QueueAtQuit is what ⌃Q found unread, and whether the daemon runs this build,
// which interrupts every turn and cancels what is queued. One on another build
// may let a turn run and then run what is queued.
type QueueAtQuit struct {
	Messages  []QueuedMessage
	Cancelled bool
}

// queuedNow is every unread message, once per send and wording however many
// agents it was waiting at: a room send stamps each target's uuid with the same
// send bytes (roomprovenance.go), and a send-now can join different words to it.
func (a App) queuedNow() []QueuedMessage {
	var out []QueuedMessage
	at := map[string]int{}
	for _, agent := range a.fleet.Agents() {
		name := a.agentName(agent.ID)
		for _, msg := range a.unread(agent.ID) {
			key := msg.id
			if send, _, ok := roomSendOf(msg.id); ok {
				key = string(send[:])
			}
			key += "\x00" + msg.echo
			if i, seen := at[key]; seen {
				out[i].To = append(out[i].To, name)
				continue
			}
			at[key] = len(out)
			out = append(out, QueuedMessage{To: []string{name}, Text: msg.echo})
		}
	}
	return out
}

// unread is what this window holds for id that claude has not read, oldest
// first: what a take-back has given back so far, what is still pinned ⧗, and a
// send-now's own message (recall.go).
func (a App) unread(id string) []queuedMsg {
	r := a.recalls[id]
	out := append(slices.Clone(r.back), a.queued[id]...)
	if r.draft != nil {
		out = append(out, *r.draft)
	}
	return out
}

// QueueAtQuit is what ⌃Q found unread, for cmd/wake's exit line.
func (a App) QueueAtQuit() QueueAtQuit { return a.quit.queue }

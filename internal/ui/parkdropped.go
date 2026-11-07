package ui

// What ⌃Q drops. The daemon interrupts every agent on the way out and cancels
// what claude has queued behind its turn (shutdownstop.go); a cancelled message
// is written to no user record on disk, so neither ↑ nor a reopened pane brings
// it back. So the window names each message pinned ⧗ at the press, and cmd/wake
// prints their words after the alt screen is gone, where scrollback keeps them.

// DroppedMessage is one message pinned ⧗ when ⌃Q was pressed: its words as
// typed, and the agents it was waiting at.
type DroppedMessage struct {
	To   []string
	Text string
}

// queuedNow is every pinned message, once per send however many agents it was
// queued at.
func (a App) queuedNow() []DroppedMessage {
	var out []DroppedMessage
	at := map[string]int{}
	for _, agent := range a.fleet.Agents() {
		for _, msg := range a.queued[agent.ID] {
			name, key := a.agentName(agent.ID), sendOf(msg.id)
			if i, seen := at[key]; seen {
				out[i].To = append(out[i].To, name)
				continue
			}
			at[key] = len(out)
			out = append(out, DroppedMessage{To: []string{name}, Text: msg.echo})
		}
	}
	return out
}

// sendOf is what one send's queued copies share: a room send stamps each
// target's uuid with the same send bytes (roomprovenance.go), and any other
// message is its own.
func sendOf(id string) string {
	if send, _, ok := roomSendOf(id); ok {
		return string(send[:])
	}
	return id
}

// DroppedOnPark is what ⌃Q dropped from claude's queues, for cmd/wake's exit
// line.
func (a App) DroppedOnPark() []DroppedMessage { return a.quit.dropped }

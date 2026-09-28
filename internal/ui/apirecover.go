package ui

// Waking a session parked for a failing API once the API answers again.
//
// autoParkStalled and /reauth park a session whose login died, because a running
// claude never picks up a fresh token and a new process does (apierror.go). So
// the wake needs only proof the login works now: a real model turn from any agent
// - they share one login - or a /login check that reports signed in. Wake cannot
// watch the login itself without a process on a timer, so those two are the
// triggers. Proof must come after the park, or a session that keeps failing
// beside a working agent would park and wake in a loop.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// recoveryState is every session parked for a failing API, against how many
// proofs the fleet had seen when it went down.
type recoveryState struct {
	proofs uint64
	parked map[string]uint64
}

// parkedForAPI remembers a session this client is parking for a failing API.
func (a App) parkedForAPI(id string) App {
	next := make(map[string]uint64, len(a.recovery.parked)+1)
	for held, at := range a.recovery.parked {
		next[held] = at
	}
	next[id] = a.recovery.proofs
	a.recovery.parked = next
	return a
}

// apiAnswered counts one proof the login works.
func (a App) apiAnswered() App {
	a.recovery.proofs++
	return a
}

// autoWakeRecovered wakes every remembered session that is parked and has seen
// proof since. One still parking keeps waiting; one live again was brought back
// some other way and is forgotten, as is one the fleet no longer knows.
func (a App) autoWakeRecovered() (App, tea.Cmd) {
	if len(a.recovery.parked) == 0 {
		return a, nil
	}
	parked := make(map[string]Agent, len(a.recovery.parked))
	for _, agent := range a.parkedAgents() {
		parked[agent.ID] = agent
	}
	next := make(map[string]uint64, len(a.recovery.parked))
	var wake []Agent
	for id, at := range a.recovery.parked {
		agent, isParked := parked[id]
		_, parking := a.parking[id]
		_, waking := a.waking[id]
		switch {
		case waking: // already asked for; nothing more is owed
		case parking || (isParked && a.recovery.proofs == at):
			next[id] = at
		case isParked:
			wake = append(wake, agent)
		}
	}
	a.recovery.parked = next
	if len(wake) == 0 {
		return a, nil
	}
	return a.wake(wake)
}

// isAPIParked is whether a parked session will wake itself, for the pin's words.
func (a App) isAPIParked(agent Agent) bool {
	_, held := a.recovery.parked[agent.ID]
	return held && agent.State == rpc.StateParked
}

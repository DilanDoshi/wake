package ui

// Waking a session parked for a failing API once the API answers again.
//
// autoParkStalled and /reauth park a session whose login died, because a running
// claude never picks up a fresh token and a new process does (apierror.go). So
// the wake needs only proof the login works now: a model turn the API accepted
// from any agent - they share one login - or a /login check that reports signed
// in. Wake cannot watch the login itself without a process on a timer, so those
// two are the triggers. The proof must come after the park is confirmed, and a
// session that fails again after a wake on a turn's word waits for /login: a
// live process's token working does not prove a new one can read the login.

import (
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// apiAwaitTail and apiLoginTail end the pin and the park notice of a session
	// that wakes itself: on any proof, or - once a turn's word has failed it - on
	// a signed-in /login only.
	apiAwaitTail = "it wakes when the API answers (or /login)"
	apiLoginTail = "/login wakes it once you are signed in"

	apiParkedFormat = "%s%s is parked; %s"
)

// recoveryState is every session parked for a failing API, and the proofs the
// fleet has seen: model turns the API accepted, and signed-in /login checks.
type recoveryState struct {
	turns, logins uint64
	held          map[string]apiPark
}

// apiPark is one session's way back, and the proofs already seen when its park
// was confirmed.
type apiPark struct {
	stage         parkStage
	turns, logins uint64
	loginOnly     bool
	retried       bool // a refused wake has had its one retry
}

type parkStage int

const (
	parkAsked    parkStage = iota // FramePark written, not yet reported parked
	parkHeld                      // reported parked, waiting for proof
	wakeSent                      // FrameWake written, not yet reported live
	wakeRetry                     // the wake was refused; retried on the next parked report
	wakeUnproven                  // live again, with no accepted turn of its own yet
)

// withHeld is the copy-on-write step every change to the set goes through.
func (a App) withHeld(id string, p apiPark, keep bool) App {
	next := make(map[string]apiPark, len(a.recovery.held)+1)
	for held, q := range a.recovery.held {
		next[held] = q
	}
	if keep {
		next[id] = p
	} else {
		delete(next, id)
	}
	a.recovery.held = next
	return a
}

// parkedForAPI remembers a session this client is parking for a failing API. One
// woken and failing again before a turn of its own went through is loginOnly.
func (a App) parkedForAPI(id string) App {
	prior, had := a.recovery.held[id]
	return a.withHeld(id, apiPark{stage: parkAsked, loginOnly: had && (prior.loginOnly || prior.stage == wakeUnproven)}, true)
}

// forgetAPIPark drops a session's auto-wake: it proved itself, or a hand park
// replaced the API's.
func (a App) forgetAPIPark(id string) App {
	if _, held := a.recovery.held[id]; !held {
		return a
	}
	return a.withHeld(id, apiPark{}, false)
}

// apiAnswered records a turn the API accepted from id: the session is proven,
// and every session parked for a failing login has its proof.
func (a App) apiAnswered(id string) App {
	a.recovery.turns++
	return a.forgetAPIPark(id)
}

// loginAnswered records a /login check that reports signed in.
func (a App) loginAnswered() App {
	a.recovery.logins++
	return a
}

// reconciledRecovery reads each held session's stage off a fleet report: a park
// confirmed is stamped with the proofs seen so far, a session live again is
// awake but unproven, and one the fleet no longer holds is forgotten.
func (a App) reconciledRecovery() App {
	if len(a.recovery.held) == 0 {
		return a
	}
	inBook := make(map[string]bool, len(a.fleet.Parked()))
	for _, s := range a.fleet.Parked() {
		inBook[s.ID] = true
	}
	next := make(map[string]apiPark, len(a.recovery.held))
	for id, p := range a.recovery.held {
		agent, ok := a.fleet.Agent(id)
		parked := inBook[id] || (ok && agent.State == rpc.StateParked)
		live := ok && !parked && agent.State != rpc.StateEnded
		switch {
		case parked && p.stage == parkAsked:
			p.stage, p.turns, p.logins = parkHeld, a.recovery.turns, a.recovery.logins
		case parked && p.stage == wakeRetry: // the proof already seen still stands
			p.stage = parkHeld
		case live && p.stage != parkAsked:
			p.stage = wakeUnproven
		case !parked && !live:
			continue
		}
		next[id] = p
	}
	a.recovery.held = next
	return a
}

// autoWakeRecovered wakes every held session whose park has seen proof since. A
// wake this window already asked for is left to run.
func (a App) autoWakeRecovered() (App, tea.Cmd) {
	var due []string
	for id, p := range a.recovery.held {
		if p.stage == parkHeld && (a.recovery.logins > p.logins || (!p.loginOnly && a.recovery.turns > p.turns)) {
			due = append(due, id)
		}
	}
	if len(due) == 0 {
		return a, nil
	}
	slices.Sort(due)
	var wake []Agent
	for _, id := range due {
		p := a.recovery.held[id]
		p.stage = wakeSent
		a = a.withHeld(id, p, true)
		if _, waking := a.waking[id]; !waking {
			wake = append(wake, a.heldAgent(id))
		}
	}
	if len(wake) == 0 {
		return a, nil
	}
	return a.wake(wake)
}

// wakeRefused handles an auto-wake the daemon refused. A report can show a park
// a moment before the daemon will take its wake, so the first refusal retries on
// the next report that shows the park, on the proof already seen; a second gives
// up and leaves the session to /resume, so a wake the daemon never takes cannot
// retry on every report. Keyed on the id, as startSettled is.
func (a App) wakeRefused(id string) App {
	p, held := a.recovery.held[id]
	if !held || p.stage != wakeSent {
		return a
	}
	next := make(map[string]struct{}, len(a.waking))
	for w := range a.waking {
		if w != id {
			next[w] = struct{}{}
		}
	}
	a.waking = next
	if p.retried {
		return a.forgetAPIPark(id)
	}
	p.stage, p.retried = wakeRetry, true
	return a.withHeld(id, p, true)
}

// heldAgent is a held session's row, from the fleet or the park book.
func (a App) heldAgent(id string) Agent {
	if agent, ok := a.fleet.Agent(id); ok {
		return agent
	}
	for _, agent := range a.fleet.Parked() {
		if agent.ID == id {
			return agent
		}
	}
	return Agent{ID: id}
}

// apiParkTail is how a parked session's own wake is said, if it has one.
func (a App) apiParkTail(id string) (string, bool) {
	p, held := a.recovery.held[id]
	switch {
	case !held || p.stage == wakeUnproven:
		return "", false
	case p.loginOnly:
		return apiLoginTail, true
	}
	return apiAwaitTail, true
}

// reportAPIPark is parkArrived's notice for a session that wakes itself.
func (a App) reportAPIPark(id, name string) bool {
	tail, ok := a.apiParkTail(id)
	if ok {
		notice.Report(apiParkedFormat, agentPrefix, name, tail)
	}
	return ok
}

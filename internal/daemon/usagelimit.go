package daemon

// Resuming the agents a claude.ai usage limit stopped (owner's 2026-10-17
// ruling, reversing deferred.md's "no message is sent for you at the reset").
//
// A limit ends the turn and the agent sits idle. Claude Code itself continues
// at the reset; a headless session does not, and only Wake can notice a login
// to another account in some other terminal. So the daemon - one decision, not
// one per window - writes "continue" to each agent whose last turn the limit
// ended, once, when Claude's config file names another account or the reset the
// limit named has passed. One goroutine does it, running only while an agent is
// stopped, waiting on the file and a timer, never polling. An unreachable API's
// stop rides the same goroutine, on its own proof (unreachable.go).

import (
	"errors"
	"os"
	"sync"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// resumeText is what the operator would type, and what an agent cut off
// mid-turn picks up from.
const resumeText = "continue"

// resetSlack waits past a reset the message names only to the minute.
const resetSlack = time.Minute

// limitClock is the time a reset is judged against; a var only so tests can
// move it past one, the way silenceLimit is compressed.
var limitClock = time.Now

// usageStop is an agent a usage limit stopped: when, the reset its message
// named (zero for none, or one already past), who was signed in then, whether
// its resume is queued, and whether the last try found the input full - then
// only a nudge retries it, never the timer, or a reset already past would spin.
// An unreachable API's stop has no reset or account; netOnly waits for the
// network alone (unreachable.go).
type usageStop struct {
	at      time.Time
	reset   time.Time
	account core.Account
	queued  bool
	blocked bool

	unreachable bool
	netOnly     bool
}

// heldStop is one stopped agent as the resume goroutine sees it: its stop, and
// whether it could take a resume now - idle, and none queued. A busy one is
// left alone until its turn ends, which nudges the goroutine again.
type heldStop struct {
	usageStop
	ready bool
}

func (u usageStop) stopped() bool { return !u.at.IsZero() }

// usageStopped reports whether a usage limit or an unreachable API has this
// agent stopped.
func (a *agent) usageStopped() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.limit.stopped()
}

// resumeAsk is one queued resume: the stop it answers, and how to announce it.
type resumeAsk struct {
	at       time.Time
	announce func(messageID string)
}

// limitWatch is the goroutine resuming stopped agents, the nudge that tells it
// a new one arrived, and the newest turn the API accepted from any agent -
// whether an answer nudges it too (unreachable.go).
type limitWatch struct {
	mu         sync.Mutex
	running    bool
	nudge      chan struct{}
	answered   time.Time
	unanswered bool
}

// noteLimit records the stop a usage limit or an unreachable API leaves,
// reporting a new one, and clears it when the agent's own turn goes through or a
// /clear abandons the conversation. A send does not: its turn is in flight, which
// already holds a resume off, and a local command runs no turn. A subagent's
// events are not the agent's. A reset not after the hit is stale - it would fire
// at once and stop again - so it arms no timer.
func (a *agent) noteLimit(ev core.Event, now time.Time) bool {
	if ev.Subagent != nil {
		return false
	}
	if ev.ModelOutput() || ev.Kind == core.KindSessionReset {
		a.mu.Lock()
		a.limit, a.unproven = usageStop{}, false
		a.mu.Unlock()
		return false
	}
	if ev.Kind == core.KindAPIError && ev.Notice == core.NoticeAPIUnreachable {
		a.mu.Lock() // right after Wake's own resume, it waits for the network
		a.limit = usageStop{at: now, unreachable: true, netOnly: a.unproven}
		a.mu.Unlock()
		return true
	}
	if ev.Kind != core.KindAPIError || ev.Notice != core.NoticeUsageLimit {
		return false
	}
	reset, ok := core.UsageLimitReset(ev.Text, now)
	if !ok || !reset.After(now) {
		reset = time.Time{}
	}
	stop := usageStop{at: now, reset: reset, account: signedIn()}
	a.mu.Lock()
	a.limit = stop
	a.mu.Unlock()
	return true
}

// signedIn is the account Claude's config file names now, zero if unreadable.
func signedIn() core.Account {
	path, err := core.ConfigFile()
	if err != nil {
		return core.Account{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return core.Account{}
	}
	account, _ := core.DecodeAccount(raw)
	return account
}

// limitHit makes sure the resume goroutine is running, and wakes it.
func (s *server) limitHit() {
	s.limits.mu.Lock()
	defer s.limits.mu.Unlock()
	if s.limits.running {
		s.nudgeLimits()
		return
	}
	s.limits.running = true
	s.start(s.watchLimits)
}

// nudgeLimits wakes the resume goroutine; one pending wake says enough. Called
// under limits.mu.
func (s *server) nudgeLimits() {
	select {
	case s.limits.nudge <- struct{}{}:
	default:
	}
}

// watchLimits resumes stopped agents on a login to another account or at
// their reset, or - an unreachable API's - on an answer or a network change
// after the stop, and returns once none is stopped or the daemon stops.
func (s *server) watchLimits() {
	var changes <-chan struct{}
	stop := func() {}
	if path, err := core.ConfigFile(); err != nil {
		logf("wake: a login elsewhere will not resume a limited agent: %v", err)
	} else {
		changes, stop = watchConfig(path)
	}
	defer stop()
	network, stopNetwork := watchNetwork()
	defer stopNetwork()
	var changed time.Time // the newest network change, as the kernel reported it
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	s.resumeOnLogin()            // a login between a stop's read and the watch
	s.resumeUnreachable(changed) // an answer between a stop and this goroutine
	for {
		due, held := s.nextReset()
		if !held && s.limitsIdle() {
			return
		}
		rearm(timer, due)
		select {
		case <-s.quit:
			return
		case <-s.done:
			return
		case <-changes:
			s.resumeOnLogin()
		case at := <-network:
			if at.After(changed) {
				changed = at
			}
		case <-timer.C:
			s.resumeDue(limitClock())
		case <-s.limits.nudge:
			s.resumeOnLogin()
			s.resumeDue(limitClock())
		}
		s.resumeUnreachable(changed)
	}
}

// rearm points the timer at a reset, or stops it for none.
func rearm(timer *time.Timer, due time.Time) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	if !due.IsZero() {
		timer.Reset(max(due.Add(resetSlack).Sub(limitClock()), 0))
	}
}

// limitsIdle marks the goroutine done if still nothing is stopped - checked
// under the lock limitHit takes after recording a stop, so none is missed.
func (s *server) limitsIdle() bool {
	s.limits.mu.Lock()
	defer s.limits.mu.Unlock()
	if len(s.limited()) > 0 {
		return false
	}
	s.limits.running = false
	return true
}

// limited is every live agent a usage limit or an unreachable API stopped, with
// its stop.
func (s *server) limited() map[*agent]heldStop {
	s.mu.Lock()
	agents := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	s.mu.Unlock()
	out := map[*agent]heldStop{}
	for _, a := range agents {
		a.mu.Lock()
		if a.limit.stopped() && !a.ended && !a.stopped && !a.parking {
			idle := !a.owed && len(a.pending) == 0 && a.tool == ""
			out[a] = heldStop{usageStop: a.limit, ready: idle && !a.limit.queued}
		}
		a.mu.Unlock()
	}
	return out
}

// nextReset is the earliest reset a ready agent waits for, and whether any
// agent is stopped at all.
func (s *server) nextReset() (time.Time, bool) {
	stops := s.limited()
	var due time.Time
	for _, stop := range stops {
		if stop.ready && !stop.blocked && !stop.reset.IsZero() && (due.IsZero() || stop.reset.Before(due)) {
			due = stop.reset
		}
	}
	return due, len(stops) > 0
}

// resumeOnLogin resumes every agent stopped under an account other than the
// one signed in now. One stopped before the account could be read takes it.
func (s *server) resumeOnLogin() {
	stops := s.limited()
	limits := false
	for _, stop := range stops {
		limits = limits || !stop.unreachable
	}
	if !limits {
		return // a login lifts a usage limit alone: the file need not be read
	}
	now := signedIn()
	if now == (core.Account{}) {
		return
	}
	for a, stop := range stops {
		switch {
		case stop.unreachable: // a login says nothing about the connection
		case stop.account == (core.Account{}):
			a.mu.Lock()
			a.limit.account = now
			a.mu.Unlock()
		case stop.ready && stop.account != now:
			s.resume(a, stop.at, "its usage limit, on a login to another account")
		}
	}
}

// resumeDue resumes every agent whose reset has passed. Wall-clock time, so a
// laptop that slept through it resumes on the next wake.
func (s *server) resumeDue(now time.Time) {
	for a, stop := range s.limited() {
		if stop.ready && !stop.reset.IsZero() && !now.Before(stop.reset.Add(resetSlack)) {
			s.resume(a, stop.at, "its usage limit, on its reset")
		}
	}
}

// nudgeLimitsDue wakes the resume goroutine on the liveness tick when a reset
// has passed - a timer counts no time while the machine sleeps - or a resume
// found the agent's input full and is waiting to try again.
func (s *server) nudgeLimitsDue(now time.Time) {
	due, _ := s.nextReset()
	if !due.IsZero() && !now.Before(due.Add(resetSlack)) || s.anyBlocked() {
		s.limitHit()
	}
}

// anyBlocked reports a resume waiting for an agent's input to have room.
func (s *server) anyBlocked() bool {
	for _, stop := range s.limited() {
		if stop.blocked {
			return true
		}
	}
	return false
}

// resume queues the "continue" behind whatever the agent's input already
// holds, marking the stop it answers queued; sendResume decides at the write
// whether it is still owed. A full input blocks it until the next nudge.
func (s *server) resume(a *agent, at time.Time, why string) {
	if !a.queueResume(at, true) {
		return
	}
	ask := &resumeAsk{at: at, announce: func(id string) {
		logf("wake: session %s resumed after %s", a.id, why)
		ev := core.Event{Kind: core.KindUserText, Notice: core.NoticeAutoResumed, Text: resumeText, MessageID: id}
		s.broadcast(rpc.Frame{Kind: rpc.FrameEvent, SessionID: a.id, Event: &ev})
	}}
	select {
	case a.in <- pending{frame: rpc.Frame{Kind: rpc.FrameSend, SessionID: a.id, Text: resumeText}, resume: ask}:
	default:
		a.blockResume(at)
		logf("wake: session %s: input full, its resume waits for the next try", a.id)
	}
}

// blockResume marks the stop that hit at as waiting for room in the input.
func (a *agent) blockResume(at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.limit.at.Equal(at) {
		a.limit.queued, a.limit.blocked = false, true
	}
}

// dropLimit ends the stop: the operator interrupted, so whatever the limit cut
// off is not to be continued for them.
func (a *agent) dropLimit() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.limit, a.unproven = usageStop{}, false
}

// queueResume marks the stop that hit at at queued or not, reporting whether
// that stop is still this agent's: a newer limit or a turn may have replaced it.
func (a *agent) queueResume(at time.Time, queued bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.limit.at.Equal(at) {
		return false
	}
	a.limit.queued = queued
	if queued {
		a.limit.blocked = false
	}
	return true
}

// sendResume writes the resume only for the stop it was queued for, and only
// while the agent is idle: a turn in flight holds it off and leaves the stop for
// a later trigger, a newer stop is not this resume's, and a second trigger finds
// the stop cleared by the first. Announced before the write, so the line lands
// ahead of the reply; a failed write is reported as apply reports one.
func (a *agent) sendResume(r resumeAsk) {
	a.mu.Lock()
	mine := a.limit.at.Equal(r.at)
	ready := mine && !a.owed && len(a.pending) == 0 && a.tool == "" && !a.stopped && !a.ended && !a.parking
	flags := rpc.MessageResumed
	switch {
	case ready:
		if a.limit.unreachable {
			flags |= rpc.MessageReconnected
		}
		a.limit, a.unproven, a.owed = usageStop{}, a.limit.unreachable, true
	case mine:
		a.limit.queued = false // a later trigger may try again
	}
	a.mu.Unlock()
	if !ready {
		return
	}
	id := rpc.NewMessageSend(flags).ID()
	r.announce(id)
	if err := a.sess.Send(resumeText, nil, id, false); err != nil {
		a.unsent(false)
		if !errors.Is(err, core.ErrNotWritten) {
			a.noteUnreachable(err)
		}
		logf("wake: session %s: resume not sent: %v", a.id, err)
	}
}

// resumedLine marks the daemon's own "continue", which comes back off disk under
// the uuid it stamped, so a reopen draws it as Wake's and not the operator's.
func resumedLine(ev core.Event) core.Event {
	if _, flags, ok := rpc.MessageOf(ev.MessageID); ok && ev.Kind == core.KindUserText && flags&rpc.MessageResumed != 0 {
		ev.Notice = core.NoticeAutoResumed
	}
	return ev
}

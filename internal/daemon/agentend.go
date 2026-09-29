package daemon

// How one agent's process is ended - reclaimed, retired, stopped or killed -
// and what each ending means for a park in flight. The judgement that a
// session is gone is agent.go's; this is what the daemon does about it.

// beginReclaim records OS proof when no earlier report exists and makes ending
// the process a once-only action. A stopped session is allowed: reclaim then
// completes the park it could not finish on its own.
func (a *agent) beginReclaim(err error) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ended || a.reclaiming {
		return false
	}
	if a.unreachable == nil {
		a.unreachable = err
	}
	a.reclaiming = true
	return true
}

// lostProcess is what the daemon does about a session it has proved is gone:
// record why, then reclaim what it was holding. docs/notes/bugs.md BUG-17.
//
// **The detection was built and the reclamation was not.** noteUnreachable set
// a field and nothing else, so a session whose process had exited kept five
// goroutines, two descriptors, a zombie and one of the thirty live-cap slots -
// liveCount switches on state, and silent is neither parked nor ended. Before
// BUG-16 that lasted until an explicit shutdown; after BUG-16 the row itself
// keeps the live count nonzero and prevents empty exit. The verb that would
// have finished the job, rpc.FrameKill, has no producer anywhere in the build:
// no key, no slash command, no CLI verb, no MCP tool writes one. So the only
// surface that could reclaim one was a test.
//
// It kills the **group**, which is the point rather than a side effect: the
// session is wedged precisely because something it spawned is holding its
// stdout open, and core's pump is parked in Scan until that descriptor closes.
// That is what a.kill already does for FrameKill, so this is the established
// answer arriving on the path that can prove it is needed.
//
// **One caller, and that is the safety argument.** Only the watchdog reclaims,
// because only the watchdog asks the OS: goneNow returns (nil, err) when it
// cannot ask, so a failed probe marks nothing, and goneIn skips a pid it does
// not recognise rather than reading absence as death. A failed write to stdin
// reports and does not reclaim - EPIPE proves stdin has no reader, which is not
// the same as the process being gone, and the gap between those two is a live
// agent's process group. That distinction cost nothing to keep: an agent that
// really is gone is quiet, and quiet is what probeQuietAgents selects on.
//
// Once per session: beginReclaim distinguishes a prior report from a prior OS
// proof, so report-then-probe reclaims while two probes still end it once.
func (a *agent) lostProcess(err error) {
	if !a.beginReclaim(err) {
		return
	}
	a.endProcess()
}

// finish records how the session ended and retires its input goroutine.
//
// err being non-nil is not necessarily a crash: core's WaitDelay turns a clean
// exit 0 into an error whenever anything the agent spawned held stderr past
// the bound, and an interrupted session exits 1 with an empty stderr. It is
// reported as what it is - how this session ended - and never as "it failed".
func (a *agent) finish(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ended {
		return
	}
	a.ended = true
	a.err = err
	close(a.gone)
}

// reclaimingNow reports whether the watchdog has OS proof the process is gone.
// A failed write sets unreachable but is not enough to skip shutdown's grace.
func (a *agent) reclaimingNow() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reclaiming
}

func (a *agent) finished() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ended
}

// stop is the spec's stop verb: close stdin and let the in-flight turn
// finish. It does not signal and it does not touch the agent's process group.
func (a *agent) stop() error {
	a.mu.Lock()
	a.stopped = true
	a.mu.Unlock()
	return a.sess.Stop()
}

// endForShutdown makes the hard-ending meaning decision under the agent lock,
// then ends the process outside it. preserveReclaimPark is ParkAll's one
// exception: OS proof that raced shutdown keeps the park label rather than
// turning a recoverable transcript into an ordinary kill.
//
// Cancelling reaches every way core's pump can park - a scan on a stdout a
// grandchild holds, a send to a consumer that stopped reading, a Wait stuck on
// stderr - and SIGKILLs the agent's whole process group. What it does *not*
// reach is a caller parked inside Send: those writes take no context, and an
// agent that stopped draining stdin parks the writer until the pipe's read end
// dies. Stop closes stdin, which fails that write at once, and Stop is never
// blocked by the write it closes out from under. Neither call substitutes for
// the other.
//
// A killed session is never a parked one, and clearing the label here is the
// whole of how that is enforced. What a --resume of a transcript a SIGKILL cut
// mid-turn loads is unrecorded, and this project's rule is that unrecorded
// behaviour is refused rather than designed around - so a park request that has
// not completed by the time somebody reaches for kill is withdrawn rather than
// honoured. markParked is never called after this, so the ending retires
// normally.
//
// **It withdraws a park only from a session that has not already ended**, and
// that condition is the whole of what makes the withdrawal correct rather than
// merely early. `ended` is set in retire, after core's Wait has returned, so it
// is Wake's own proof that the process is gone: a signal sent after it cuts
// nothing, and the transcript --resume reads is the one the agent finished
// writing. Clearing the label there would withdraw a park whose process ended
// on its own, and it is reachable - shutdown's grace samples `finished()` on a
// 20ms tick and kills whatever had not ended by the last look, so a session
// that ends in that gap gets a kill it did not need. Both fields are under this
// lock, so the test is atomic with the clear rather than a check-then-act.
func (a *agent) endForShutdown(preserveReclaimPark bool) bool {
	a.mu.Lock()
	preservePark := preserveReclaimPark && a.reclaiming
	if !a.ended && !preservePark {
		a.parking = false
	}
	a.mu.Unlock()

	a.endProcess()
	return preservePark
}

// kill is the ordinary hard ending: never preserve a park still in flight.
func (a *agent) kill() {
	a.endForShutdown(false)
}

// endProcess stops the session and cancels its context, without deciding what
// the ending *means*.
//
// Split from kill because reclaiming is not the same verb as killing, and the
// difference is one line: kill abandons a park in flight, because FrameKill
// means end this rather than put it down. lostProcess must not - an operator
// who pressed ⌃C on a wedged agent asked for something recoverable, and the
// process being gone does not make it unrecoverable: the transcript is on
// claude's disk and the park record is what /resume needs to reach it. So the
// reclaim ends the *process* and lets completePark decide what that was.
func (a *agent) endProcess() {
	_ = a.sess.Stop()
	a.cancel()
}

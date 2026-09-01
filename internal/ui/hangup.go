package ui

// The connection ending, and the way back onto one.
//
// Split from app.go, which owns the messages: this is the one path where the
// model replaces the thing it is reading from, and it reads better beside the
// text it prints than buried in the switch.

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// hungUp is the stream ending: the daemon exited, or closed this client.
//
// This is C1's last link. The conversation is still alive on the other side -
// the agent keeps working, `wake status` still lists it - so the only thing
// that made a hang-up permanent was having no way back. There is one now, and
// it is taken without being asked for, because a user who has just watched
// their window go dead should not have to know a verb to get it back.
//
// A session that has *ended* is not reattached to. There is nothing there.
//
// **A hang-up once ⌃Q has been pressed never reattaches**, and there are two of
// those rather than one - which is the whole reason this is two conditions.
//
// *Still waiting*: this hang-up is the only answer that ask will get. A daemon
// that took the park-all closes its clients on the way out, so it is also the
// ordinary end of the story - but a daemon that crashed closes them too, and
// nothing on the wire separates the two, which is `wake stop`'s own
// discrimination problem arriving here. So it settles as unconfirmed rather than
// as done, and the exit line says which it is.
//
// *Already answered*: **the reply and the hang-up arrive in the same batch**,
// and that is the ordinary case rather than an edge - the daemon enqueues the
// status reply and then, a grace later, closes every client, and one Update
// folds whatever the ring holds. App.stream folds the frames and then comes
// straight here, so without this the answer that settled the ask never became
// the quit: the window sat open reattaching to a daemon that had just parked the
// fleet and exited. Found by the end-to-end lifecycle test and by nothing
// smaller, which is why there is now a unit test for exactly that batch.
//
// Reattaching in either case would start a *fresh* daemon holding no sessions,
// on the way out of a window whose last act was asking the old one to park them.
func (a App) hungUp(err error) (tea.Model, tea.Cmd) {
	// A hang-up forgets a rate-limit warning's pending clear, the way reattached
	// below forgets a permission mode and a turn's tokens. A warning in the final
	// batch arms a clear tick, but that arm is consumed only on the live
	// (`!m.done`) path in App.stream - so without this a batch that is both
	// non-empty and done leaves the arm standing, and the next batch after a
	// reattach schedules a tick keyed to a spent warning. Only the arm is
	// dropped: gen stays monotonic so an in-flight tick from before the outage
	// cannot match a warning raised after it. See ratelimit.go.
	a.rl.arm = false
	if a.quit.waiting() {
		return a.parkAllSettled(fmt.Errorf("%w: %w", errParkAllHungUp, err))
	}
	if end := a.closing(); end != nil {
		return a, end
	}
	if a.dial == nil || a.ended || a.reattaching {
		notice.Report("%v", err)
		return a, nil
	}
	a.reattaching = true
	a.in = nil
	notice.Report("the daemon hung up; reattaching%s…", a.reattachTarget())

	dial := a.dial
	id := a.sessionID
	return a, func() tea.Msg {
		conn, stream, sess, fleet, err := dial()
		if err != nil {
			return errMsg{Err: fmt.Errorf("%w. %s", err, reattachAdvice(id))}
		}
		return reattachedMsg{conn: conn, stream: stream, session: sess, fleet: fleet}
	}
}

// attachedName is what this client's own session is called, for a line a person
// reads. It comes from the DM rather than from the fleet because the DM was
// given the name the daemon assigned at spawn, which is the name that exists
// before any report has arrived.
func (a App) attachedName() string {
	if d := a.dms[a.sessionID]; d != nil {
		return d.Name
	}
	return ""
}

// reattachTarget is the *to whom* half of a reattach sentence, and it is empty
// for a model that is not attached to anybody.
//
// **Bare `wake` runs exactly that model.** It opens the room over the whole
// fleet with no DM beside it, so sessionID is "" and dms is empty, and
// attachedName is "" - while the dialer is wired up, so every sentence below is
// live on that path. Interpolated naively they read "reattaching to @…" and
// "reattached to @; …", which is the bare handle this project has ruled against
// twice: cmd/wake.reattach's own comment records an unnamed session announced as
// a bare `@` on the notice row and drawn as one in the DM header, *"which reads
// as two agents"*.
//
// So the name is not defaulted, substituted or elided - the clause is dropped.
// A room reattaches to a room, and there is nobody to name.
func (a App) reattachTarget() string {
	name := a.attachedName()
	if name == "" {
		return ""
	}
	return " to " + agentPrefix + name
}

// reattached swaps in the new connection and says what was missed.
func (a App) reattached(m reattachedMsg) (tea.Model, tea.Cmd) {
	a.conn = m.conn
	a.in = newInbox()
	a.gen++
	a.reattaching = false
	// Fold the fleet report the dialer fetched on the way back in. Nothing else
	// reconciles what changed during the outage - serveClient enqueues only a
	// hello, and the one FrameStatus ui.App writes is ⌃Q's - so an ask raised
	// while this client was gone is on no surface until this runs: Cards.Reconcile
	// mints its card, and the roster and the awareness strip read the state off
	// the fleet. It is cmd/wake's own daemon.Status read on the probe connection,
	// so it adds no second FrameStatus writer here. Nil (a failed room reattach, a
	// unit test) is a no-op. Before the two corrections below, so they win:
	// WithStatus re-stamps a working turn's clock and tokens, which ForgetTurns
	// then clears.
	a = a.applyStatus(m.fleet)
	// Everything that happened while this client was gone happened without it,
	// and the fleet report it comes back with carries no permission mode. A
	// belief that survived the disconnection is one nothing can confirm - see
	// forgotModes for which way that fails.
	a = a.forgotModes()
	// And what any turn in flight had produced, for the same reason one step
	// further: the boundary that would have cleared it may have been in the gap.
	// See Fleet.ForgetTurns.
	a.fleet = a.fleet.ForgetTurns()
	go a.in.pump(m.stream)

	notice.Report("%s", reattachedText(a.reattachTarget(), m.session))
	return a, a.listen()
}

// reattachedText is what a returning client is told it missed.
//
// It cannot say *what* it missed and does not pretend to: the daemon keeps no
// replay buffer, deliberately - Wake owns almost no state, and the transcript
// that does exist is the one claude persists on disk. So the honest report is
// the shape of the hole and where the agent is now, which is the question
// somebody staring at a reconnected window actually has.
//
// The state comes from the fleet report the dialer took on the way in, and
// StateBlocked is the one worth naming: a permission request that arrived
// during the outage is still outstanding, the agent is stopped dead until it is
// answered, and rpc.SessionStatus carries the RequestID for exactly this case.
// The next report puts that ask back up as a card - see Cards.Reconcile - and
// the card carries the key, which is why no key is named here.
//
// It takes the rendered target rather than a name, which is what lets the room's
// dialer hand back a zero SessionStatus and get an honest sentence out of the
// default arm: *"reattached; what it said meanwhile is not in the conversation
// above"*. There is no one agent in a room whose state is the thing to report,
// and there is nobody to name. See App.reattachTarget.
func reattachedText(target string, s rpc.SessionStatus) string {
	missed := "what it said meanwhile is not in the conversation above"
	switch s.State {
	case rpc.StateBlocked:
		return fmt.Sprintf("reattached%s, blocked on a permission request; %s", target, missed)
	case rpc.StateWorking, rpc.StateSilent, rpc.StateIdle:
		return fmt.Sprintf("reattached%s, %s; %s", target, s.State, missed)
	default:
		return fmt.Sprintf("reattached%s; %s", target, missed)
	}
}

// reattachAdvice is the way back when reattaching automatically did not work.
// It names a verb that exists, which is the whole of what C1 was missing.
//
// ⌃O rather than ⌃C: the key that leaves is ⌃O now, and ⌃C parks. A ⌃C here
// would ask the daemon to park a session over a connection that has already
// hung up - nothing would happen, on the one surface whose job is to say what
// will.
//
// **And "a verb that exists" is a claim about the invocation, not only about the
// word.** `wake attach` with nothing after it is refused by cmd/wake.checkArity
// - *"`wake attach` takes one session id or name"* - because attaching to
// whichever session happens to be running is a guess that is wrong the moment
// there are two. So a room, which has no session id to put there, is told to run
// bare `wake` instead: the verb that reopens a room is the one this client is.
// Naming a wake verb that does not work is the legend rule broken at the shell,
// and a failure path is the worst place to break it.
func reattachAdvice(sessionID string) string {
	if sessionID == "" {
		return "⌃O, then `wake`"
	}
	return fmt.Sprintf("⌃O, then `wake attach %s`", sessionID)
}

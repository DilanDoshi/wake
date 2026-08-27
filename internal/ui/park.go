package ui

// Parking, from inside the room.
//
// # Why ⌃C means this now
//
// ⌃C detached because stopping was irreversible - a stray keystroke that ended
// a session destroyed an hour of context with no way back, so detach was the
// safe thing to put under the key everyone reaches for. Park removes that
// reason: the process stops, the transcript stays, and `/resume` brings it
// back. So ⌃C can mean what an operator expects it to mean, and detach moves
// to a key of its own rather than disappearing - "close the terminal, let
// twenty agents keep working" is what the background daemon is *for*.
//
// The original ruling that put detach under ⌃C is quoted forward rather than
// deleted, because it is still the argument for these being *two* keys: "a key
// that sometimes stops a turn and sometimes closes the whole workspace costs
// someone an hour once and is never trusted again". ⌃C parks and never leaves;
// ⌃O leaves and never parks. Neither guesses.
//
// # Why ⌃Q writes one frame and not N
//
// Because the daemon is the only process that can see the whole fleet, and
// because parking the fleet is also the daemon's own shutdown: FrameParkAll
// parks and then exits, so the park book is complete before anything can read
// it. A client that wrote N FramePark frames and then quit would be racing its
// own writes against the program ending, and would have no way to park an
// agent whose report it had not yet seen.
//
// # Why ⌃Q writes a second frame it does not need the answer to
//
// Nothing on this socket acknowledges a park-all. ⌃C can let the *report* make
// the promise, because ⌃C leaves the window open for a report to arrive in;
// ⌃Q's window is closing, so a refused, dropped or lost write has nowhere at
// all to land and `Parking 3 agents.` is printed over it either way. Park is
// the feature that makes stopping an agent recoverable, and a park somebody was
// told happened and did not is the failure it exists to prevent.
//
// So the ask is confirmed the way `wake mcp`'s acting tools confirm theirs, and
// it needs **no new frame kind and no daemon change**: serveClient dispatches
// one connection's frames synchronously and in order, so a FrameStatus written
// *behind* FrameParkAll cannot be answered until the ParkAll has been
// dispatched. Reaching the reply means this daemon is provably ending as a park.
//
// **What did not transfer from there is the wait itself.** `wake mcp` dials a
// connection per call and blocks reading it, and there is nothing else on it. A
// TUI has one long-lived connection carrying the whole fleet's events, drained
// by inbox.go's pump - and nothing that renders, and nothing that blocks, may
// sit between that socket and the ring. So the answer arrives the way every
// other frame does, through the ring and folded by Update, and the wait is a
// state on the model rather than a read. What ends the program is
// parkAllSettled, from whichever of four places got there first.

import (
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// parkFailed and parkAllFailed name the write that could not happen.
	parkFailed    = "parking that conversation"
	parkAllFailed = "parking the fleet"

	// noParkTarget is ⌃C with no agent in front of it. ⌃C parks one
	// conversation; the room is not one conversation, and parking the whole
	// fleet is ⌃Q and says so.
	noParkTarget = "⌃C parks one conversation. " + noPaneAdvice + ", or ⌃Q parks the fleet and closes Wake"

	// parkedFormat is said only from the room, for interruptedFormat's reason:
	// a DM names its agent in its own header, but in the room the target is
	// whoever the roster cursor is on, and a key that stops work somewhere the
	// eye is not resting has to say where.
	parkedFormat = "parking %s%s\u2026"

	// parkConfirmed is said when a fleet report first names the session as
	// parked, and it is the only place the promise is made.
	//
	// The split is ⌃F's rule one key over, and CLAUDE.md states it: **the
	// report is the confirmation and the keypress is not.** The daemon refuses
	// a park for real reasons - a session already ended, one it does not hold -
	// and the refusal arrives after the keypress, so a sentence promising
	// resume on the press is one the next frame contradicts. Naming the ask
	// costs nothing if the park succeeds and is honest if it does not.
	parkConfirmed = "%s%s is parked - " + resumeVerb + " %s brings it back"

	// parkingFleet is said on the keypress. The window is about to close, so
	// this is the last thing the notice row can carry, and it names the verb
	// that undoes it.
	//
	// "offers back whatever finished in time" rather than "offers them back",
	// and the hedge is load-bearing: the daemon gives each agent the grace and
	// **kills whatever has not ended by the end of it**, and a killed session is
	// never a parked one, so it is dropped from the book. A turn running a build
	// routinely outlasts the grace. This client cannot know which - it is
	// leaving - so it may not promise the outcome, only the ask.
	parkingFleet = "parking %d agents and closing Wake. `wake` reopens the room and offers back whatever finished in time."

	// parkWouldDeny is ⌃C on an agent that is waiting for the operator, and it
	// is the one state park refuses.
	//
	// Park closes stdin. A permission ask that dies because Wake closed stdin
	// is recorded as `non_execution_kind: "permission-rule"` - a **denial** -
	// and CLAUDE.md's own sentence is that it is *indistinguishable* from one
	// the operator made. Nothing downstream can tell them apart, and it
	// **survives the wake**: the session comes back believing it was told no
	// about a question nobody saw. That is worse than losing work, because
	// losing work is visible.
	//
	// So this refuses rather than parking silently, and it names esc because esc
	// handles exactly this state cleanly - the ask is withdrawn on a
	// control_cancel_request, the DM draws it, and the session stays usable.
	// ⌃C and esc share interruptTarget and sit one legend row apart with opposite
	// safety on this one state; only one of them said so.
	//
	// It says *when* they can park rather than only that they cannot, which is
	// forkRefusal's rule: a refusal that is only "no" leaves an operator with a
	// key that does nothing and no idea when it would.
	parkWouldDeny = "%s%s is blocked on a permission request, and parking closes stdin under it - which the agent is told is a denial. Answer it, or esc withdraws it, then ⌃C parks."
)

// parkAckTimer is how ⌃Q's deadline on the daemon's answer is scheduled, and it
// is a seam for settleTimer's reason: the *delay* is the guarantee, and a test
// that hands the model an expiry which has already arrived says nothing about
// whether the model asked for it three seconds later or immediately. Reach it
// through this, never by calling tea.Tick directly.
var parkAckTimer = tea.Tick

// parkAckTimeout bounds that wait, and the bound is not a formality: without one
// a daemon that took the frame and then stopped speaking - the shape a graceful
// shutdown already has, where a dial lands in a listen backlog nothing accepts
// from - leaves ⌃Q as a window that never closes and a fleet nobody can see.
// **A park that fails may not hang the quit and may not be dropped**, so this
// expiring is a settlement with a reason rather than a silence.
//
// The same three seconds daemon.Status gives its own exchange and `wake mcp`
// gives its acting calls, for their reason: two ends of a unix socket on one
// machine have the same answer to "how long can a peer that is behaving take".
// It is only ever reached when something is already wrong - the ordinary cost of
// this wait is one round trip on a socket the client is already connected to.
//
// A var only so tests can compress it; nothing outside a test assigns it, which
// is actTimeout's own terms.
var parkAckTimeout = defaultParkAckTimeout

const defaultParkAckTimeout = 3 * time.Second

// The three ways ⌃Q ends without an answer. Each is a sentence cmd/wake prints
// on the way out, so each says what is *not* known rather than reporting a
// failure as an outcome: a write that failed may have failed after a partial
// write, and a daemon that went quiet may have taken the verb and died parking.
//
// **The hang-up carries its own second clause and it is measured rather than
// hedged.** client.close() closes the connection without draining what is
// queued on it, and shutdown reaches closeClients in microseconds when there is
// nothing to stop - so over a fleet with no live process the reply loses that
// race about 40% of the time (15/25 on this machine, against 25/25 with one
// live agent, at a mean of 48µs). That makes an EOF here the *ordinary* end of
// a successful ⌃Q over an already-parked fleet, as well as what a crash looks
// like, and nothing on the wire separates them - which is `wake stop`'s own
// sentence about its own EOF. A line that read as alarm would be alarm nobody
// reads by the third time. The close, if it is ever worth it, is in deferred.md
// and it is a bounded drain in closeClients.
var (
	errParkAllUnsent  = errors.New("this window had no connection to the daemon, so nothing was asked")
	errParkAllHungUp  = errors.New("the daemon closed the connection without answering, which is what a finished shutdown looks like and also what a crash looks like")
	errParkAllUnknown = errors.New("the daemon did not answer")
)

// parkAll is ⌃Q's whole state.
//
// It is one field on App rather than several, and it lives in this file for
// mention's reason - nothing about ⌃Q belongs in app.go's line count. Read once,
// by cmd/wake, off the final model; see ParkedFleet.
type parkAll struct {
	// asked is how many agents this shutdown hands to the next `wake`. Counted
	// before the write, because after tea.Quit there is no model anybody reads.
	asked int

	// pressed records that ⌃Q is why this program is ending. It is set on the
	// keypress and never cleared: the second press is a no-op, not a restart.
	pressed bool

	// settled is the answer having arrived, by any of the four routes, and err
	// is why it could not be confirmed. nil err on a settled ask is the daemon
	// saying it took it - the only state in which the exit line may promise a
	// parked fleet.
	settled bool
	err     error
}

// waiting is the window between the keypress and the answer: asked for, not yet
// answered, program still running.
func (p parkAll) waiting() bool { return p.pressed && !p.settled }

// parkAllMsg settles that wait from a command. The reply settles it from a
// frame instead, which is why this carries only the failures.
type parkAllMsg struct{ err error }

// park parks the conversation that has the keys.
//
// The target is interruptTarget's - the focused DM, and otherwise the roster's
// selection - because esc and ⌃C are the two keys that act on *one* agent from
// either pane, and one target rule stated in one place is what stops them
// disagreeing about which one that is.
func (a App) park() (tea.Model, tea.Cmd, bool) {
	id, ok := a.interruptTarget()
	if !ok {
		notice.Report("%s", noParkTarget)
		return a, nil, true
	}
	if a.endedAgent(id) || a.parkedAgent(id) {
		// Nothing to park and nothing lost, so this is silent - the same trade
		// interrupt makes for an ended session.
		return a, nil, true
	}
	next, cmd, parked := a.parkTarget(id, a.agentName(id))
	if !parked {
		return a, nil, true
	}
	if a.focus == "" {
		notice.Report(parkedFormat, agentPrefix, a.agentName(id))
	}
	return next, cmd, true
}

// parkTarget is the park itself, once something has decided which session: the
// one state it refuses, the wait it registers, and the frame it writes. It
// reports whether it wrote one.
//
// Split out when `/manager` grew a park of its own. Two callers with different
// ways of choosing a target and one rule about what parking *does* - and the
// rule is the half that must not be copied: parking closes stdin, and an
// outstanding permission ask that dies that way is recorded as a **denial** the
// operator never made, which survives the wake. A second copy of that check is
// two places to correct the day it changes, on the one surface where being
// wrong costs somebody a repository.
//
// The name is passed rather than looked up because both callers have already
// resolved the row this is about, and a second lookup is a second answer to a
// question that has been answered - the refusal would be free to name a
// different agent from the one the caller decided to park.
func (a App) parkTarget(id, name string) (App, tea.Cmd, bool) {
	if a.blockedAgent(id) {
		// See parkWouldDeny - and note this refusal *speaks* even from a DM,
		// where a successful park is silent, because the operator pressed a key
		// that did not do what they expected.
		notice.Report(parkWouldDeny, agentPrefix, name)
		return a, nil, false
	}
	return a.awaitingPark(id), a.write(parkFailed, rpc.Frame{Kind: rpc.FramePark, SessionID: id}), true
}

// parkFleet asks the daemon to park everything, and closes Wake once it has
// answered.
//
// tea.Sequence rather than tea.Batch, and that is the whole of the ordering:
// Batch runs both commands concurrently, so the deadline would start before the
// write goroutine had reached the socket and could expire on a write that was
// still in progress. Sequence runs the write to completion first, so the three
// seconds are three seconds of the daemon's silence rather than of this
// client's own transport.
//
// **What is emphatically not in that sequence is tea.Quit.** It was, and it is
// the bug: the program ended on the keypress, so every refusal the write could
// produce arrived at a window that had already closed and the exit line
// promised a parked fleet regardless.
//
// It counts what it is parking before it writes, because after the program ends
// there is no model anybody reads. cmd/wake uses that count for the line it
// prints instead of asking a daemon that is, by then, shutting down and will
// spend its whole status timeout in the backlog.
func (a App) parkFleet() (tea.Model, tea.Cmd, bool) {
	if a.quit.pressed {
		// The second press is a no-op rather than a second ask. beginQuit's
		// first verb wins, so another FrameParkAll changes nothing at the
		// daemon; another wait would be a second deadline racing the first,
		// and the loser would settle an ask that was already answered.
		return a, nil, true
	}
	a.quit = parkAll{asked: a.carriedForward(), pressed: true}
	notice.Report(parkingFleet, a.quit.asked)
	return a, tea.Sequence(a.askParkAll(), a.parkAllDeadline()), true
}

// askParkAll is the write, with its failure turned into a settlement.
//
// It wraps a.write rather than replacing it: every client write in this package
// goes through that one place, and this adds the one thing a shared helper
// cannot know - that a failure here is not a line for a notice row on a screen
// that is about to be torn down, it is the answer to the question ⌃Q is waiting
// on, and cmd/wake prints it after the alt screen is gone.
//
// The FrameStatus is the acknowledgement and the ordering is what makes it one.
// Written behind the verb, never ahead of it: a status answered before the
// ParkAll was dispatched proves nothing about the ParkAll.
//
// **A model with no connection is answered here rather than earlier**, and that
// is one decision in one place rather than the same test twice. a.write reports
// having nothing to write to by handing back a **nil command** - a success from
// every angle a sequence can see, and the silent total failure this whole change
// is about. It is also the one outcome that knows the fleet is untouched, which
// is why it gets a sentence of its own rather than the general one.
func (a App) askParkAll() tea.Cmd {
	write := a.write(parkAllFailed,
		rpc.Frame{Kind: rpc.FrameParkAll},
		rpc.Frame{Kind: rpc.FrameStatus},
	)
	return func() tea.Msg {
		if write == nil {
			return parkAllMsg{err: errParkAllUnsent}
		}
		if failed, bad := write().(errMsg); bad {
			return parkAllMsg{err: failed.Err}
		}
		// Written, not answered. The reply is what settles it.
		return nil
	}
}

// parkAllDeadline is the bound on the daemon's silence. See parkAckTimeout.
func (a App) parkAllDeadline() tea.Cmd {
	return parkAckTimer(parkAckTimeout, func(time.Time) tea.Msg {
		return parkAllMsg{err: fmt.Errorf("%w within %v", errParkAllUnknown, parkAckTimeout)}
	})
}

// parkAllSettled records how the ask ended and closes the window.
//
// The one place the program is allowed to end for ⌃Q, and it is reached from
// four: the reply that answers the question, a write that failed, the deadline,
// and the daemon hanging up. First one wins - the rest find the ask already
// settled and change nothing, so a deadline expiring a microsecond after the
// reply cannot overwrite a confirmed park with "the daemon did not answer".
//
// It is a no-op when ⌃Q was not pressed, which is what lets the failure routes
// be folded into paths that carry ordinary failures too.
func (a App) parkAllSettled(err error) (App, tea.Cmd) {
	if !a.quit.waiting() {
		return a, nil
	}
	a.quit.settled, a.quit.err = true, err
	return a, tea.Quit
}

// parkAllTaken is the reply arriving.
//
// **Only a reply, never a push.** A push is the daemon announcing somebody
// else's state change and can already have been in the socket when ⌃Q was
// pressed; reading one as this ask's answer would confirm a park nobody had
// taken. That is daemon.Status's own rule, arriving on the one connection this
// package writes a question on.
//
// What it does not prove is in this file's header and in deferred.md: launch
// confirms every spawn, fork and wake with the same FrameStatusReply, so a
// /new, ⌃F or /resume still in flight when ⌃Q is pressed has a reply of its own
// coming, and this cannot tell the two apart. The window is one round trip wide
// on a connection that is provably alive, and the failure needs the daemon to
// die inside it; the alternative - a correlator on the frame - is a daemon
// change, and the whole point of this instrument is that it is not one.
func (a App) parkAllTaken(kind string) App {
	if kind != rpc.FrameStatusReply {
		return a
	}
	settled, _ := a.parkAllSettled(nil)
	return settled
}

// closing is tea.Quit once the ask has settled, for the paths that fold a frame
// and then have to say whether that ended the program.
func (a App) closing() tea.Cmd {
	if a.quit.settled {
		return tea.Quit
	}
	return nil
}

// reading re-arms the drain, unless a frame just folded was ⌃Q's answer - in
// which case the program is ending, and another read on a connection that is
// about to close is a goroutine with nothing to hand anybody back.
func (a App) reading() tea.Cmd {
	if end := a.closing(); end != nil {
		return end
	}
	return a.listen()
}

// carriedForward is how many sessions this shutdown hands to the next `wake`.
//
// **It is not `live()`, and that is the whole of this function.** ⌃Q counted
// with `a.live()`, which is send.go's *broadcast-addressee* list - its subject
// is "who can I send a message to", and it leaves out `rpc.StateParked` for a
// reason that is entirely about addressing: a parked agent has no process to
// read a message, so `@all` reaching it would be one refused frame per parked
// session. None of that is true of counting. A session ⌃C parked is a session
// this shutdown carries forward: it is in the book, the next daemon restores
// it, and the offer names it.
//
// The daemon's own arithmetic is what this has to agree with, and `live()` did
// not: `bookParked` counts an entry it finds already in the book as booked, so
// a fleet of one live and one already-parked is logged as *"parked 2 sessions
// on the way out"* while the operator was told one - and a fleet parked by hand
// first was reported as **zero** on the way out of a shutdown carrying all of
// it.
//
// Ended is the only exclusion, and it is the daemon's: `agent.bookable` is
// `ended && (parking || parked)`, so a session that simply finished is written
// down nowhere and offered back never.
//
// It counts rows rather than reusing either existing filter. Borrowing a filter
// whose subject is a different question is the defect this replaces, and the
// verdict per state is held in `parkStates` - the same table ⌃C's verdicts live
// in, so a new reachable state is a build failure until somebody says what this
// does with it.
func (a App) carriedForward() int {
	n := 0
	for _, agent := range a.fleet.Agents() {
		if agent.State != rpc.StateEnded {
			n++
		}
	}
	return n
}

// ParkedFleet is how many agents ⌃Q asked to park, whether ⌃Q is why this
// program is ending, and why the ask could not be confirmed.
//
// Exported for one caller: cmd/wake's converse, which prints what happened on
// the way out. It reads the final model rather than asking daemon.Status,
// because the daemon this would ask is the one that is shutting down - the dial
// lands in the backlog and burns the whole status timeout for an i/o error, and
// the line the operator gets is "could not count what is still running" after a
// key whose whole point was that it counted.
//
// **The error is the third return rather than a second method**, because the
// two are one fact and a caller that read the count without it would print the
// sentence this whole change exists to stop printing. A nil error on a ⌃Q means
// the daemon said it took the verb; anything else is what it could not confirm,
// in the daemon's own words where there are any.
func (a App) ParkedFleet() (int, bool, error) { return a.quit.asked, a.quit.pressed, a.quit.err }

// parkedAgent reports whether there is a process behind this session right now.
//
// Separate from endedAgent rather than folded into it, because the two need
// different sentences: an ended session is over, and a parked one is one
// command away. Folding them would make ⌃C's silent case and the DM's advice
// line the same, and the advice is the whole affordance.
func (a App) parkedAgent(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	agent, ok := a.fleet.Agent(sessionID)
	return ok && agent.State == rpc.StateParked
}

// blockedAgent reports whether this session is stopped dead on a permission
// ask that the operator has not answered.
//
// Its own predicate rather than a read inside park, for parkedAgent's reason:
// it is one state with one consequence and one sentence, and the sentence is
// the whole affordance. Nothing else reads it - the cards list and the roster
// glyph each derive blockedness their own way from the same reports.
func (a App) blockedAgent(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	agent, ok := a.fleet.Agent(sessionID)
	return ok && agent.State == rpc.StateBlocked
}

// parkArrived says a session is parked the first time a fleet report says so.
//
// It is startArrived's shape and exists for startArrived's reason: the keypress
// is an ask and the report is the answer. It reports once per transition rather
// than once per report - every report names a parked session for as long as it
// stays parked, and a notice repeated on every fleet update is a notice row
// nobody reads.
//
// It says nothing about sessions this client did not ask to park. A fleet is
// shared, and another window's ⌃C is not this operator's business - the roster
// already shows it.
func (a App) parkArrived(st *rpc.Status) App {
	if st == nil || len(a.parking) == 0 {
		return a
	}
	for _, s := range st.Sessions {
		if _, asked := a.parking[s.ID]; s.State != rpc.StateParked || !asked {
			continue
		}
		next := make(map[string]struct{}, len(a.parking))
		for id := range a.parking {
			if id != s.ID {
				next[id] = struct{}{}
			}
		}
		a.parking = next
		notice.Report(parkConfirmed, agentPrefix, s.Name, s.Name)
	}
	return a
}

// awaitingPark remembers a park this client asked for, so the report that
// answers it can be told from every other report naming a parked session.
func (a App) awaitingPark(id string) App {
	next := make(map[string]struct{}, len(a.parking)+1)
	for held := range a.parking {
		next[held] = struct{}{}
	}
	next[id] = struct{}{}
	a.parking = next
	return a
}

// One supervised session, and the judgement the daemon makes about it that
// nothing below the daemon can.
//
// # Telling a silent session from an idle one
//
// core reports a session ending by closing Events() and setting Err(). It
// cannot report a session that has *stopped happening*, and there is a real
// state where nothing is wrong from inside: an agent that exits cleanly while
// something it spawned still holds stdout leaves the pump parked in Scan with
// Err() nil and Events() open forever. There is also the ordinary case of an
// agent simply waiting for its next instruction. From outside those two look
// identical - no events, no error, channel open - and one of them is holding a
// live-cap slot for a process that no longer exists.
//
// The distinction is not observable from the stream. It is observable from
// what the daemon knows and core does not: **whether anything is owed.** An
// idle agent owes nothing; the last thing that happened was a turn ending, or
// nothing has been sent yet. A working agent owes a turn end, because Wake
// wrote a message to its stdin and has not seen the turn close. So:
//
//	owes nothing                              -> idle
//	owes a turn end, events still arriving    -> working
//	owes a permission answer from a human     -> blocked
//	owes a turn end, nothing for silenceLimit -> silent
//
// Two more rules, and both exist because the first four are blind in the same
// place: they can only ever fire on a session Wake is *waiting for*. An agent
// that dies right after finishing its last turn owes nothing, so no timer
// runs, and nobody writes to it, so nothing fails - and it reads idle forever
// while holding a live-cap slot. So:
//
//	a write to its stdin failed         -> silent, at once (noteUnreachable)
//	quiet, and the OS says it is gone   -> silent (probeQuietAgents)
//
// Both are evidence rather than inference. The first is proof the moment
// anyone tries to talk to it; the second is the daemon asking the OS about the
// process group core recorded at spawn, which is the only route to the answer
// when nobody is talking to it at all.
//
// Silent is reported, never acted on. Wake is not entitled to kill an agent on
// a timer - it might be four seconds into a legitimate ten-minute Bash - so
// the daemon makes the distinction visible and leaves the verb to the
// operator. That is the whole policy: the daemon never guesses, and the
// operator can no longer be unable to tell.
//
// It is deliberately conservative in one direction. An agent that starts a
// turn on its own (--brief lets it) is owed nothing by Wake and reads as idle
// while it works. Being wrongly idle is harmless; being wrongly silent would
// invite a kill nobody meant.

package daemon

import (
	"cmp"
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// defaultSilenceLimit is how long a session may owe a turn end while
// producing nothing at all before it is reported silent.
//
// It has to clear the longest legitimate gap inside a turn, which is one slow
// tool call: an agent running a test suite or a build emits the tool_use and
// then nothing until the result. Five minutes is longer than almost all of
// those and far shorter than "this agent died two hours ago and nobody
// noticed". It bounds a *report*, never a kill, so erring long costs
// lateness rather than a half-written file.
const defaultSilenceLimit = 5 * time.Minute

// silenceLimit is a var only so tests can compress it; nothing outside a test
// assigns it.
var silenceLimit = defaultSilenceLimit

// ask is one permission request an agent is blocked on: which one, and what it
// wants from the operator. An agent holds a slice of these - see agent.pending.
//
// The second half is here rather than left to the client because this is the
// only layer that has both facts at once. core sees the ask go past and
// forgets it; a client can be told the ask exists and then answer it with a
// frame that carries no evidence of what it was answering. The daemon watched
// the ask arrive and holds it until it is settled, so it is the only place
// that can notice an answer settling it wrongly - see allow.
type ask struct {
	id string

	// choice marks an ask whose answer has to ride inside the allow -
	// core.AskChoice. It is false for an ordinary permission ask and false for
	// a plan approval, and the second of those is the one worth stating: a
	// plan ask also demands a human, and a bare allow is its complete and
	// correct answer, so keying this on "requires a human" instead would fire
	// on every plan an operator ever approves.
	choice bool
}

// agent is one core.Session plus everything the daemon knows about it.
type agent struct {
	id   string
	name string

	// label is what this session is working on, derived from where it was
	// started. Display only: nothing addresses an agent by it, and nothing
	// here reads it.
	label string

	// color is this session's identity hue, chosen by an operator's /color and
	// carried onto the snapshot and into the park book. Display only and never an
	// argv word: it is Wake's own, not something claude is told. One of
	// rpc.ColorNames, or empty for none. See color.go.
	color string

	// dir is where this session was **started**, and it never moves. park
	// writes it down, unpark launches from it and a fork runs in it, because
	// claude locates a transcript by the directory the process started in even
	// when every frame names a worktree - discover.go's 58-of-428 case.
	dir string

	// cwd is where the session is running now, which stops being dir the moment
	// an agent uses EnterWorktree. Display only: the roster row, the status
	// bar's branch, the workspaces sidebar. Empty until a turn says otherwise.
	cwd string

	// effort is the reasoning level this session was started at, or "" for
	// none. Display and the park book only; nothing here re-reads it.
	effort string

	// confirmedEffort is the level a bare /model probe read back, or "" before
	// one has answered. Display prefers it over effort - it is the level the
	// session is genuinely at, where effort is only the level Wake asked for.
	// Not written to the park book: a woken session re-probes and re-confirms.
	confirmedEffort string

	// model is what this session runs as, or "" for none. Display and the park
	// book only, like effort. Read through currentModel: launch writes it and
	// park reads it from another goroutine.
	model string

	// budget is the ceiling this session was started under and fallback is the
	// chain it fails over to, both "" for none. Written once by launch and never
	// again - there is no runtime command for either, so nothing moves one
	// mid-session the way `/effort` moves a level. Read through currentSpend.
	budget   string
	fallback string

	// commands is the slash commands the last init advertised, kept so they
	// reach a client that attached after that init - the completion menu's, and
	// the report is the only route to them for such a client. Display only, like
	// cwd, and updated on the receipt the same way. See rpc.SessionStatus.Commands.
	commands []string

	// parent is the session this one was forked from, or empty. Immutable
	// after newAgent and display only, exactly like label: nothing addresses an
	// agent by it and nothing here reads it.
	parent string

	sess   *core.Session
	cancel context.CancelFunc

	// in carries stdin writes off the connection's read goroutine. Writing
	// to an agent's stdin can park for good against a child that stopped
	// draining it, and the goroutine that would park is the one carrying
	// that client's *kill* frame - the call an operator makes when an agent
	// is wedged must not be the call a wedged agent blocks.
	//
	// Never closed, for the reason client.out is never closed: submit runs
	// on another goroutine and a send on a closed channel panics even under
	// a select with a default.
	in   chan pending
	gone chan struct{}

	mu        sync.Mutex
	started   time.Time
	lastEvent time.Time
	owed      bool
	// claudeID is the session id claude is currently writing this conversation
	// under, which is the one its transcript is named after. Equal to the
	// agent's own id until a /clear mints a new one. See observe.
	claudeID string

	// pending is every ask this agent is blocked on, oldest first. A slice
	// because concurrent asks are real - parallel tool calls, or two subagents
	// each asking - and collapsing them to one marked the whole agent free the
	// moment any single one was answered, while the CLI stayed stopped on the
	// rest. See ask, and rpc.SessionStatus.RequestIDs.
	pending []ask
	tool    string
	toolArg string
	stopped bool
	ended   bool

	// parking records that this session's process is being stopped in order to
	// park it, which is the only thing that tells retire a park from an
	// ending: both arrive as the events channel closing. It is cleared by
	// kill, because a session SIGKILLed mid-anything is not something any
	// recording says --resume can pick up.
	parking bool

	// parked is set once that has happened, and is what stateLocked reports.
	//
	parked bool

	// wakeable is published only after completePark has finished every
	// old-generation storage mutation. parked stays the display state: the
	// process is already gone while roster removal or the park-book write may
	// still be blocked. A wake must wait for the stronger fact, or the old
	// finalizer can erase the successor's roster row or park generation.
	wakeable bool

	// parkDurable records whether this parked generation reached the park
	// book. A wakeable row whose write failed must keep the daemon alive: its
	// in-memory agent is then the only route to --resume, and empty exit would
	// destroy it. Published with wakeable after the write attempt returns.
	parkDurable bool

	// parkGeneration is the complete comparable parked.json generation this
	// in-memory row represents. Retry acknowledgements compare the full record,
	// because same-ID timestamps may collide and legacy records may both be zero.
	parkGeneration parkedRecord

	err      error
	reported string

	// unreachable is set when a write to this agent's stdin failed on a
	// session nobody stopped. Kept apart from err, which is how the session
	// *ended*: a session can be unreachable and not yet ended, and that gap
	// is the whole state this daemon exists to make visible.
	unreachable error

	// reclaiming is set by the one OS-confirmed lostProcess call that ends the
	// process. A write report leaves it false so the watchdog can still prove
	// the stronger fact; a second proof then does not end the process twice.
	reclaiming bool

	// probeAfter is the earliest time this agent is worth asking the OS about
	// again, and probeEvery is the interval that produced it. Both are cleared
	// by any event, which is what makes the schedule a suspicion rather than a
	// slower timer - see quietAndDue.
	probeAfter time.Time
	probeEvery time.Duration
}

func newAgent(id, name, label, dir, parent string, sess *core.Session, cancel context.CancelFunc) *agent {
	now := time.Now()
	return &agent{
		id:        id,
		name:      name,
		label:     label,
		dir:       dir,
		parent:    parent,
		sess:      sess,
		cancel:    cancel,
		in:        make(chan pending, agentQueue),
		gone:      make(chan struct{}),
		started:   now,
		lastEvent: now,
	}
}

// runningIn is where this session is now: what it last said, or where it was
// started if it has not said. The caller holds a.mu.
func (a *agent) runningIn() string {
	if a.cwd != "" {
		return a.cwd
	}
	return a.dir
}

// observe records what one event says about this session's liveness.
func (a *agent) observe(ev core.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastEvent = time.Now()
	// Proof of life, so the probe schedule starts over. This is the half that
	// makes the backoff a decaying suspicion instead of a slower tick: the case
	// the probe exists for is an agent that dies *right after* finishing its
	// last turn, so the hazard is concentrated near the last event and the rate
	// should decay from it. An agent that speaks again has a fresh silence, and
	// a fresh silence is worth asking about promptly.
	a.probeEvery, a.probeAfter = 0, time.Time{}

	if ev.SessionID != "" && ev.SessionID != a.claudeID {
		// Claude's own id for this conversation, which is what its transcript
		// is named after - the agent's own until a /clear mints a new one.
		// Re-keyed on session_id changing between events, which is the rule
		// CLAUDE.md's trap list states: the id changes on a hook_started four
		// frames before the init that also carries it, so keying on init
		// misattributes everything between.
		a.claudeID = ev.SessionID
	}

	// Where the session is now - never where it started, which is a.dir and does
	// not move. EnterWorktree is on every agent's tool list, so this changes
	// without Wake doing anything, and it moves on the receipt the way the
	// permission mode does.
	//
	// **Absolute only.** This arrives on the child's own stdout rather than on a
	// Frame, so it passes no wire fence; everything else here that decides a
	// directory either proves it (discover.go) or requires it absolute
	// (maySpawn). See core.SessionFacts.Dir.
	if ev.Session != nil && filepath.IsAbs(ev.Session.Dir) {
		a.cwd = ev.Session.Dir
	}

	// The completion menu's, kept for the report so a client that attached after
	// this init still learns them. Only when a frame names some: a result frame
	// carries none, and blanking the set once a turn would empty the menu the
	// same way withFacts guards against. See rpc.SessionStatus.Commands.
	if ev.Session != nil && len(ev.Session.SlashCommands) > 0 {
		a.commands = ev.Session.SlashCommands
	}

	switch ev.Kind {
	case core.KindSessionReset:
		// Cleared rather than set from the frame: conversation_reset names the
		// id that *died* and does not name its successor, which arrives on the
		// next event. The arm below relearns it.
		// /clear mints a new claude session id and leaves the old transcript
		// under the old one, while Wake goes on addressing this agent by the id
		// it spawned under. Without this, reading history back finds the
		// *pre-clear* conversation and shows it as the context of an agent whose
		// memory was cleared. The successor is not on this frame - it arrives on
		// the next one - so this only forgets, and the arm below relearns.
		a.claudeID = ""
	case core.KindToolUse:
		// The sidebar's "what is this agent on". Not cleared by the tool's own
		// result - see rpc.SessionStatus.Tool.
		if ev.Tool != nil {
			a.tool, a.toolArg = ev.Tool.Name, ev.Tool.Display
		}
	case core.KindPermissionRequest:
		// Blocked until a human answers, and RequestID is the only
		// correlator an answer has. Appended so a client that reattached
		// after the event went past can still find every outstanding ask and
		// settle it - a session can be blocked on more than one at once.
		//
		// Ask is kept with it because it stops being knowable the moment this
		// event is past: the answer frame that comes back names a request id
		// and nothing else about what it is answering. A duplicate id is not
		// appended twice, so a reattach's replay cannot double an ask.
		a.addPending(ev.RequestID, ev.Ask == core.AskChoice)
	case core.KindRequestWithdrawn:
		// Claude has retired an ask nobody answered - an interrupt landing
		// on one, in every recording there is. Answering it now does
		// nothing at all: a well-formed allow for a withdrawn request
		// produced no frame, no error and no tool run. So a client still
		// holding it is holding a prompt whose answer goes into the void,
		// and this is the only frame that says so - the interrupt's own
		// receipt names the interrupt and reports nothing about the ask.
		//
		// Only an exact, non-empty match. The frame carries no session_id
		// and need not name anything this agent is waiting on, and
		// protocol.go decodes a withdrawal that names nothing rather than
		// dropping it, so "" reaches here and must not mean "all of them".
		// Clearing on a mismatch is this defect inverted: a live prompt
		// taken off the operator's screen with the agent still stopped dead
		// behind it.
		if ev.RequestID != "" {
			a.pending = withoutAsk(a.pending, ev.RequestID)
		}
	case core.KindTurnEnd:
		// The turn is closed, so nothing is owed. A denied tool still ends
		// its turn normally, which is why this is keyed on the turn end and
		// not on anything about how the turn went.
		//
		// It clears the ask too, and that is a backstop rather than the
		// route: an ask this daemon never saw withdrawn is dead by the time
		// its turn closes. It is also why the case above cannot be tested
		// through a fake that ends the turn in the same breath - see
		// fakeWithdrawTheAsk.
		a.owed = false
		a.pending = nil
		// And nothing is being run any more. Cleared here rather than on the
		// tool's own result for the reason rpc.SessionStatus.Tool gives: a
		// result answers the call, and blanking the row between the two is a
		// flicker at the rate a busy agent works.
		a.tool, a.toolArg = "", ""
	}
}

// noteSent records that Wake asked for a turn, which is what makes a later
// silence mean something.
func (a *agent) noteSent() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.owed = true
}

// addPending records one outstanding ask, oldest first. The caller holds a.mu.
//
// A duplicate id is not appended twice: a reattaching client's replay can
// deliver the same ask as an event a second time, and a doubled ask would keep
// the agent blocked after its one answer took only the first copy down.
func (a *agent) addPending(id string, choice bool) {
	if id == "" {
		return
	}
	for _, p := range a.pending {
		if p.id == id {
			return
		}
	}
	a.pending = append(a.pending, ask{id: id, choice: choice})
}

// withoutAsk is the pending set with one id removed, as a new slice. An empty
// id matches nothing, since pending never holds one - see addPending.
func withoutAsk(asks []ask, id string) []ask {
	out := make([]ask, 0, len(asks))
	for _, p := range asks {
		if p.id != id {
			out = append(out, p)
		}
	}
	return out
}

// noteAnswered takes down the one ask this answer settled, leaving any others
// the agent is still blocked on. An empty id settles nothing: an answer with no
// correlator names no ask.
func (a *agent) noteAnswered(requestID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending = withoutAsk(a.pending, requestID)
}

// awaitsChoice reports whether the ask this id names is one whose answer has
// to ride inside the allow. It is asked *before* the answer goes out, because
// noteAnswered is about to forget which ask this was.
//
// An id this agent is not blocked on answers false. That is the honest reading
// rather than a cautious one: an answer naming an ask the daemon never saw, or
// one already settled, is not evidence that a choice is being dropped, and
// warning about it would put a notice on the screen for every stale answer a
// reattaching client sends.
func (a *agent) awaitsChoice(requestID string) bool {
	if requestID == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.pending {
		if p.id == requestID {
			return p.choice
		}
	}
	return false
}

// blockedOnAsk reports whether any permission ask is outstanding. apply reads
// it on the agent's input goroutine while observe writes pending on the fan-out
// one, so the read takes the lock. What the lock cannot close - an ask already
// in flight on stdout when apply's frame reaches stdin - is the process's own
// asynchrony, and no check on this side of the pipe can.
func (a *agent) blockedOnAsk() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.pending) > 0
}

// noteUnreachable records that a write to this agent's stdin failed.
//
// This is the second half of the liveness policy, and the faster half: it is
// evidence rather than a timer. A write that fails on a session nobody stopped
// is the shape of an agent that exited while something it spawned kept its
// stdout, leaving the pump parked in Scan with Err() nil and Events() open. No
// amount of waiting would have told anyone; one failed write says so at once.
//
// **It is enough to report and deliberately not enough to reclaim.** EPIPE says
// stdin has no reader, which is not the same as the process being gone - and
// the gap between those two is a live agent's process group. lostProcess is the
// caller that may kill, and it is the one that asks the OS first. See its
// header; the two must not drift, because this comment said "proof" while that
// one said "not proof" for exactly one revision.
//
// A session Wake stopped is excluded, because a write failing after stop is
// what stop means.
func (a *agent) noteUnreachable(err error) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ended || a.unreachable != nil || a.stopped {
		return false
	}
	a.unreachable = err
	return true
}

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

// pendingIDsLocked is the outstanding asks' ids, oldest first, or nil for none.
// The caller holds a.mu. nil rather than an empty slice so the field is omitted
// from the wire when nothing is blocked, matching what one empty id used to do.
func (a *agent) pendingIDsLocked() []string {
	if len(a.pending) == 0 {
		return nil
	}
	ids := make([]string, len(a.pending))
	for i, p := range a.pending {
		ids[i] = p.id
	}
	return ids
}

// snapshot is this agent's line in a status report.
func (a *agent) snapshot() rpc.SessionStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := rpc.SessionStatus{
		ID:         a.id,
		Name:       a.name,
		Label:      a.label,
		Color:      a.color,
		Dir:        a.dir,
		Cwd:        a.runningIn(),
		ParentID:   a.parent,
		Tool:       a.tool,
		ToolArg:    a.toolArg,
		Effort:     cmp.Or(a.confirmedEffort, a.effort),
		Budget:     a.budget,
		Commands:   a.commands,
		State:      a.stateLocked(time.Now()),
		RequestIDs: a.pendingIDsLocked(),
		PID:        a.sess.Pgid(),
		QuietMS:    time.Since(a.lastEvent).Milliseconds(),
	}
	switch {
	case a.err != nil:
		st.Error = a.err.Error()
	case a.unreachable != nil:
		st.Error = a.unreachable.Error()
	}
	return st
}

// stateLocked is the liveness policy, stated once. See the file header for
// why these are the cases that exist.
func (a *agent) stateLocked(now time.Time) string {
	switch {
	case a.parked:
		// Above ended, and that is the whole of the ordering argument: parking
		// works by ending the process, so a.ended is true here too. Reporting
		// ended would say the context is gone when it is on disk and one
		// --resume away.
		return rpc.StateParked
	case a.ended:
		return rpc.StateEnded
	case a.unreachable != nil:
		// Evidence, not a timer: a write to its stdin failed and nobody
		// stopped it.
		return rpc.StateSilent
	case len(a.pending) > 0:
		return rpc.StateBlocked
	case !a.owed:
		return rpc.StateIdle
	case now.Sub(a.lastEvent) >= silenceLimit:
		return rpc.StateSilent
	default:
		return rpc.StateWorking
	}
}

// changed reports whether this agent's state has moved since the last time
// the watchdog announced it, and records the new one.
func (a *agent) changed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.stateLocked(time.Now())
	if state == a.reported {
		return false
	}
	a.reported = state
	return true
}

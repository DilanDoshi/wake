// The ending verbs and the fleet report. Split from wire.go, which owns the
// frame and the two directions it travels; this is the vocabulary a client
// uses to end things and to ask what is still running.

package rpc

// Ending a session, and ending the daemon. Spec §5 names four verbs and they
// are deliberately four: park, detach (nothing on the wire - the client just
// disconnects), stop, and quit. Kill is the fifth thing, and it is not a verb
// the spec offers an operator by default: it exists for an agent that is
// already wedged.
//
// One kind each, never one kind with a mode field. A frame that arrived with
// an empty mode would need a default, and here every default destroys
// something: defaulting to kill takes an agent down mid-Edit and leaves a
// half-written file in someone's repo, defaulting to stop makes a wedged
// agent unkillable, defaulting to quit ends the fleet. An unrecognized kind
// does nothing at all, which is the only safe end of that failure - the same
// reasoning that made FrameAllow and FrameDeny two kinds.
//
// FrameInterrupt is here for the opposite reason: it is the one verb in this
// file that does **not** end anything, and it is next to the ones that do
// because that is where somebody would otherwise confuse them.
//
// FrameWake and FrameParkAll are the two the 2026-08-10 lifecycle spec added,
// and they are the recoverable half: one brings a parked session back, and one
// parks the whole fleet on the way out. FrameParkAll is where the "one kind
// each" rule earns its keep twice over - it and FrameQuit both end the daemon
// and they disagree about what happens to the sessions it had parked, so a
// mode field on one kind would put a fleet's recoverability behind a default.
const (
	// FrameInterrupt aborts the turn a session is running. It does not end
	// the session, and that distinction is recorded rather than assumed: an
	// interrupted process keeps its session_id, takes the next message
	// normally, and resumes later with the aborted turn's context intact
	// (docs/superpowers/notes/2026-08-08-interrupt-findings.md §6). "Stop
	// this turn" and "stop this session" are different operations on
	// Claude's wire, so they are different kinds on Wake's.
	//
	// It carries only a SessionID. There is deliberately no field for
	// Claude's cancel_queued - see core.interruptCancelQueued for the
	// argument, and note the shape of it: a transport that cannot express a
	// choice nobody has made is the same protection omitempty gives
	// UpdatedInput.
	//
	// The correlator is minted below this frame, not on it. core.Session
	// mints the request_id as it writes, because that is the only layer
	// that can promise the id it reports is the id that went out - and the
	// receipt comes back on the event stream as a
	// core.KindControlReceipt, not as a reply to this.
	FrameInterrupt = "interrupt"

	// FrameStop ends one session the way the spec says a session ends:
	// close its stdin and let the in-flight turn finish. It does not
	// signal, and it must never be routed to the kill path.
	FrameStop = "stop"

	// FramePark stops one session's process and keeps everything else about
	// it: its id, its transcript on disk, its name, its label and the
	// directory it ran in. It is the recoverable ending, and it is what made
	// ⌃C safe to rebind - a stop is irreversible, a park is `/resume`. That
	// rebinding has happened, and this is the frame ⌃C writes.
	//
	// A separate kind from FrameStop for the reason FrameStop and FrameKill
	// are separate: no default is safe. A stop that arrived meaning park loses
	// an agent an operator expected back; a park that arrived meaning stop
	// leaves a session in the fleet forever with no process behind it. An
	// unrecognized kind does neither.
	//
	// It carries only a SessionID. Where the session resumes is not on this
	// frame and must never be: the daemon knows the directory it recorded,
	// and resuming in a different working directory is completely unrecorded
	// (2026-08-10 findings §12).
	FramePark = "park" // client → daemon: stop a session's process, keep the session

	// FrameWake brings a parked session back: the same process argv it had,
	// with a bare `--resume <its own id>` in place of `--session-id`. It keeps
	// its id, its name, its label and the directory it ran in, because
	// --resume reuses the id it is given and because resuming anywhere else is
	// unrecorded.
	//
	// It carries only a SessionID, for FramePark's reason: where it resumes is
	// the daemon's memory and not a client's to supply.
	FrameWake = "wake" // client → daemon: bring a parked session back

	// FrameKill is the hard ending for an agent that has stopped
	// behaving - SIGKILL to its whole process group, mid-tool, whatever it
	// was writing. Separate from FrameStop precisely so it cannot happen by
	// accident.
	FrameKill = "kill"

	// FrameQuit stops every session, then the daemon exits. It is what
	// `wake stop` sends, and it is the deliberate ending: it also forgets
	// what this daemon had parked, because spec §2 makes stop the one verb
	// there is no way back from and a book that survived it would give it one
	// by accident.
	FrameQuit = "quit"

	// FrameParkAll parks every session and then exits, which is ⌃Q. It is what
	// makes the next start able to offer the fleet back: a fleet that was
	// parked on the way out is one Wake knows how to restore, and a fleet that
	// was merely abandoned is a guess.
	//
	// A separate kind from FrameQuit, and the difference is the whole reason
	// there are two. FrameQuit is `wake stop` - the deliberate ending, which
	// also forgets what is parked. This one is the recoverable one. A single
	// kind with a mode field would need a default for a frame that arrived
	// without one, and both defaults destroy something: defaulting to stop
	// throws away a fleet somebody meant to keep, defaulting to park leaves
	// twenty sessions the next `wake` offers back after somebody deliberately
	// ended them. Same reasoning as stop / kill / quit.
	//
	// It carries nothing at all - not even a SessionID - because it is about
	// the fleet rather than about a session. Where each one resumes is the
	// daemon's own memory, exactly as it is for FramePark.
	FrameParkAll = "park_all"

	// FrameStatus asks what is running and FrameStatusReply answers it. A
	// reply is only ever an answer to a request on that same connection.
	FrameStatus      = "status"
	FrameStatusReply = "status_reply"

	// FrameStatusPush is the same Status sent unasked, when a session changes
	// state or ends - so a client learns that an agent went silent without
	// polling for it.
	//
	// A separate kind rather than a correlator on the reply, and the two are
	// not equivalent. These frames differ in *meaning*, not merely in
	// provenance: one answers a question and the other announces a change, and
	// a client wants different things from them - a TUI renders every push and
	// ignores replies it did not ask for, while `wake status` wants exactly
	// one answer and must not accept an announcement that was already in
	// flight when it asked.
	//
	// Sharing one kind made that impossible to express. daemon.Status returned
	// the first status frame on a fresh connection, which could be a broadcast
	// that landed between the hello and the request - an answer that predates
	// the question by up to a liveness tick. It is also what made two of this
	// package's own guards vacuous: they slept, asked once, and read a frame
	// pushed before the state they were asserting about had changed.
	//
	// A correlator would have closed the same hole, at the cost of an id
	// minted by every client and echoed by the daemon on a field that already
	// means something else here (RequestID is a permission ask). The kind is
	// where "what is this frame" belongs, and it costs no wire surface and no
	// state on either side.
	FrameStatusPush = "status_push"
)

// The states a session can be in, as reported in a SessionStatus.
//
// The distinction that matters, and the one nothing below the daemon can
// make: a *silent* session is not an *idle* one. An idle agent has finished
// its turn and is waiting for a human, which is the ordinary resting state of
// most of a fleet. A silent agent owes Wake a turn end and has produced
// nothing at all for a long time - the shape a session takes when its process
// has exited while something it spawned holds stdout, which leaves core's
// pump parked in Scan with Err() nil and Events() open. From outside those
// two look identical: no events, no error, channel open. They are told apart
// by whether anything is owed, which only the daemon knows, because only the
// daemon knows what it sent.
const (
	// StateIdle is running with no turn in flight. Nothing is owed.
	StateIdle = "idle"

	// StateWorking is a turn in flight and events still arriving.
	StateWorking = "working"

	// StateBlocked is a permission request outstanding. The agent is
	// stopped dead until someone answers, which is not a fault - it is the
	// highest-priority thing in the fleet.
	StateBlocked = "blocked"

	// StateSilent is a turn in flight and nothing arriving. Reported, never
	// acted on: Wake is not entitled to kill an agent on a timer, because
	// the agent might be in the middle of an Edit. The operator gets the
	// distinction and the kill verb; the daemon does not guess.
	StateSilent = "silent"

	// StateEnded is the process gone. The session stays in the report for
	// as long as a client might still ask about it, carrying Error.
	StateEnded = "ended"

	// StateParked is a session whose process was stopped on purpose and whose
	// transcript is kept. It is neither working nor idle nor ended: nothing is
	// running, and `--resume <its own id>` brings it back with the
	// conversation intact (resume-park.jsonl / resume-wake.jsonl, two
	// processes over one session id).
	//
	// It is reported ahead of ended by agent.stateLocked, and that ordering is
	// load-bearing rather than cosmetic: parking works by ending the process,
	// so a parked agent is also an ended one from every angle below the
	// daemon. Reporting it as ended would tell an operator their ⌃C destroyed
	// an hour of context, which is precisely the fear ⌃C used to mean detach
	// because of.
	StateParked = "parked"

	// StateOrphaned is a session recorded on disk with no daemon holding
	// it: the trees a SIGKILLed daemon left behind. It appears only in the
	// status of a machine whose daemon is gone.
	StateOrphaned = "orphaned"
)

// Status is what `wake status` reports. A background process nobody can see
// or stop is a liability rather than a feature, and at 20 agents "is it still
// running" is the first question anyone asks.
type Status struct {
	// Running distinguishes a daemon that answered from one that did not.
	// It is not redundant with a nil Status: a status assembled from the
	// on-disk roster after the daemon died has sessions and no daemon.
	Running bool `json:"running"`

	PID    int    `json:"pid,omitempty"`
	Socket string `json:"socket,omitempty"`

	// Sessions is every session the daemon holds, or - when Running is
	// false - every one the last daemon recorded before it died, each
	// StateOrphaned.
	Sessions []SessionStatus `json:"sessions,omitempty"`

	// Parked is the park book: sessions a previous daemon left behind, which
	// this one is holding nothing for. **Disjoint from Sessions**, and that
	// separation is the whole point - a daemon that restored these into the
	// fleet put a roster row and a whole conversation in front of somebody who
	// had just quit, which is what ⌃Q is for getting rid of.
	//
	// They are addressable and nothing else: /resume resolves a name here and
	// the daemon launches from the record. Until then no name is claimed, no
	// row is drawn and no process exists, so a fleet with a full park book and
	// nothing running reports zero sessions - which is what `wake status`
	// should say about a machine where nothing is running.
	//
	// A session parked by ⌃C *within* a live session stays in Sessions as
	// StateParked instead: its daemon is still holding it, and losing that row
	// would lose the one thing on screen saying where it went.
	Parked []SessionStatus `json:"parked,omitempty"`

	// ProbeIncomplete is a fail-closed signal owned SOLELY by FleetOnDisk: the
	// roster/liveness half of this report is not authoritative, because the
	// sweep hit its deadline before every record was verified. A consumer that
	// reads Sessions to assert a *negative* - "nothing is running", so it is safe
	// to proceed - must refuse to when this is set, because a dropped record is
	// an under-count and `wake stop && rm -rf` is what trusting it costs. The
	// daemon's live path never sets it, so a real daemon's report is unchanged;
	// the park-book half needs no ps and is unaffected. omitempty drops false, so
	// every frame that was byte-identical before still is - this is additive.
	ProbeIncomplete bool `json:"probe_incomplete,omitempty"`
}

// SessionStatus is one agent's line in that report.
type SessionStatus struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`

	// Label is what this session is working on - the branch or the directory
	// it was started in - so a row reads `sydney <> dev-5748` rather than a
	// bare name. Display only, and separate from Name rather than folded into
	// it: `wake attach` resolves a name, and a name with a label glued to it
	// is not one anybody would type.
	//
	// Empty is legitimate. A session started somewhere that names nothing
	// renders as a bare name, which is what it was before this field existed.
	Label string `json:"label,omitempty"`

	// Color is this session's identity hue - the name from rpc.ColorNames its
	// name-tag, status bar and roster row are drawn in. Client-chosen (only the
	// operator knows how they want the fleet grouped by eye), so unlike Name it is
	// a display string a client sets rather than the daemon derives. Empty means
	// no colour, which is every session before /color touches it.
	Color string `json:"color,omitempty"`

	// Dir is the absolute directory this session runs in, and it is the
	// workspace identity the room's left sidebar groups by.
	//
	// Label cannot do that job. Label is the git branch, so two repositories
	// both on `main` are one row - which is the ordinary case for somebody
	// running agents across seven repos, and it is the case the sidebar exists
	// for. The pair reads `api-v2` (directory) over `main` (branch).
	//
	// Empty is legitimate for a session started somewhere the daemon could not
	// name, exactly as Label is.
	Dir string `json:"dir,omitempty"`

	// Cwd is where the session is running **now**, which stops being Dir the
	// moment an agent uses EnterWorktree - a tool every session Wake spawns
	// carries. Empty until the session has said where it is; a consumer wanting
	// "where is it" should read Cwd or Dir, in that order.
	//
	// **Two fields rather than one, and the split is load-bearing.** Dir is the
	// directory the process was *started* in, which is what park writes down,
	// what unpark launches from and what a fork runs in - because claude
	// locates a transcript by that one even when every frame names a worktree
	// (discover.go's 58-of-428 case). Cwd is display: the roster row, the
	// status bar's branch, the workspaces sidebar. Following the cwd into Dir
	// made a park record the worktree and a wake resume against the wrong
	// project slug, which is an empty conversation under a live session id.
	Cwd string `json:"cwd,omitempty"`

	// ParentID is the session this one was forked from, or empty for a session
	// that is nobody's fork.
	//
	// Wake's own memory, and it has to be: **nothing on Claude's wire says a
	// session was forked or from what.** A fork's `init` carries 23 top-level
	// keys and not one names an ancestor, the parent emits nothing at all
	// while a fork is taken from it, and this is true for a live parent
	// exactly as for a dead one (2026-08-10 findings §6).
	//
	// It lives here and on daemon.agent, and deliberately **not** in the
	// on-disk roster. roster.go is the minimum needed to *find a process
	// again*, and ancestry helps nobody do that; worse, a persisted parent id
	// outlives the parent's name, because nothing turns a roster record back
	// into an agent - so a later daemon would report an edge to a session it
	// holds nothing about. The durable copy already exists and is not Wake's:
	// a fork's transcript preserves the parent's per-message uuids across both
	// recorded generations (2026-08-10 findings §8), which is a forensic route
	// to lineage after the fact rather than a substitute for recording it now.
	//
	// An id and not a name, because a name is never an address. Whoever draws
	// it resolves the id through the report it is already holding.
	ParentID string `json:"parent_id,omitempty"`

	// Tool and ToolArg are the tool call this session is currently inside -
	// the tool's name, and the one argument worth showing beside it, already
	// resolved behind the airlock as core.ToolCall.Display.
	//
	// Two fields rather than one joined string, because the two consumers want
	// different things from them: a TUI passes them to render.ToolCall, which
	// measures the argument against the name to bound the line, and an MCP
	// tool wants them as data a model can compare. Joining here would decide
	// for both.
	//
	// Empty between turns. It is set on a tool call and cleared on a turn end,
	// and deliberately *not* cleared on the tool's own result: a result is the
	// answer to the call, not a new activity, and blanking the row for the
	// moment between them makes the sidebar flicker at exactly the rate a busy
	// agent works.
	//
	// This is carried on the report and *also* derived from the event stream
	// by internal/ui, and both are deliberate. The stream is what keeps the
	// sidebar current, because a status push fires on a state *change* and an
	// agent stays `working` across ten tool calls. The report is what lets a
	// freshly started `wake mcp` answer list_agents at all - it has no history,
	// and "peter and john are both editing auth/token.go" is a string
	// comparison the manager can only make if the daemon carries it.
	Tool    string `json:"tool,omitempty"`
	ToolArg string `json:"tool_arg,omitempty"`

	// Effort is the reasoning level this session runs at, or "" for the one
	// Wake chose nothing for.
	//
	// It is on the report because nothing on Claude's wire ever says it back:
	// "effort" appears in the recorded corpus only as an entry in
	// init.slash_commands, never as a value. So this is Wake's own memory of
	// what it passed - the same shape CLAUDE.md requires for permissionMode,
	// which init normalizes rather than echoes - and the report is the only
	// route by which a client that attached late can learn it.
	Effort string `json:"effort,omitempty"`

	// Budget is the spend ceiling this session was started under, or "" for one
	// with none. Here for Effort's reason and by the same route: nothing on
	// Claude's wire reports a cap, so this is Wake's own memory of what it
	// passed and the only way a client that attached late can learn it.
	//
	// **It is the cap and never the spend.** A client may say what Wake asked
	// for; it may not present this as progress toward anything, because the
	// number that would make that a measurement - total_cost_usd - resets to
	// zero on /clear and is accumulated per session-id epoch elsewhere.
	//
	// The failover chain is deliberately not here. It would be a second field
	// no surface draws: a session running its second-choice model is
	// indistinguishable on this wire from one running its first, so a chain on
	// the report could only ever repeat what was asked for beside a model field
	// that already says what is in use.
	Budget string `json:"budget,omitempty"`

	// Commands is every slash command this session advertised on its init - what
	// the completion menu offers. Here for Effort's reason and by the same route:
	// it rides the init *event*, which keeps a connected client current but tells
	// a client that attached late nothing, since no event is replayed. The report
	// is the only route by which such a client learns it, so a reattach used to
	// leave the menu empty for every agent. The daemon's own memory of the last
	// init it decoded; the word is Wake's ("commands"), never claude's wire
	// `slash_commands`, which only the airlock may name.
	Commands []string `json:"commands,omitempty"`

	// State is one of the State constants above.
	State string `json:"state"`

	// QuietMS is how long since this session last produced an event, in
	// milliseconds. Milliseconds rather than a time.Duration because a
	// duration marshals as a bare nanosecond count that a second client -
	// the SwiftUI app in a later phase - has to know to divide; and a
	// wall-clock timestamp because two processes' clocks are the same clock
	// here but will not always be.
	//
	// It is the evidence behind State, not a decoration: silent and idle
	// differ by what is owed, and how long it has been quiet is what says
	// whether the difference has started to matter.
	QuietMS int64 `json:"quiet_ms,omitempty"`

	// RequestIDs are every ask a StateBlocked session is waiting on, oldest
	// first, so a client that reattached after the events went past can still
	// answer them.
	//
	// A slice rather than one id because a session can be blocked on several
	// asks at once - parallel tool calls in one turn, or two subagents each
	// asking - and one id here dropped the rest: the client's Reconcile took
	// down every card the report could not name, stranding a live ask with
	// nothing on screen and the agent stopped dead behind it.
	//
	// Empty is therefore load-bearing, because an ask can die without ever
	// being answered: interrupting a session with one outstanding **withdraws**
	// it, and an answer written afterwards is silently ignored - no frame, no
	// error, no tool run. So a client must read this whole set on every report
	// rather than remembering the ones it saw, or it offers a prompt whose
	// answer goes nowhere.
	//
	// The daemon drops one on core.KindRequestWithdrawn and clears all on a
	// turn end; see agent.observe for why only the id a withdrawal names is
	// retired.
	RequestIDs []string `json:"request_ids,omitempty"`

	// Error is why a StateEnded session ended, already formatted. Empty for
	// a clean exit - and non-empty is not necessarily a crash: core's
	// WaitDelay turns a clean exit 0 into an error whenever something the
	// agent spawned held stderr past the bound.
	Error string `json:"error,omitempty"`

	// PID is the process group the agent leads, recorded so a later daemon
	// can find what a crashed one left behind. Zero where the platform has
	// no process groups.
	PID int `json:"pid,omitempty"`
}

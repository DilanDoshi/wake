package ui

// The client-side fleet: every agent the daemon has told this client about,
// folded from a stream it was already receiving.
//
// # Why this exists at all
//
// daemon.fanOut broadcasts every session's events to every attached client,
// and App.apply throws away the ones whose SessionID is not its own. The room
// is that discard turned into a fold. No daemon change, no new frame kind, no
// second connection - a room is a second consumer of a stream that is already
// arriving.
//
// # One pass, three consumers
//
// Observe is the only place an event is inspected. It updates the per-agent
// record the right sidebar draws, the unread counts the left sidebar draws,
// and it returns the events the room draws. Three views, one pass, and no way
// for them to disagree about what arrived.
//
// # The filter is Decision 1B, relocated
//
// The brief settled on prose plus boundaries and its amendment relocated the
// two: the prose goes in the chat, the boundary goes in the sidebar. So the
// room gets the agent's own words, the operator's own turn, and anything an
// agent needs answered - and nothing else. Subagent prose is excluded: it is 9
// of 54 assistant_text events and it reintroduces the interleaving that
// dm_blocks.go needed a gutter and a per-block header to solve. The DM has
// that machinery; the room would need its own copy.
//
// A turn end returns an event only when the turn produced no prose. Wake does
// not author "finished": a turn ending does not mean the work stopped - 3 of
// 46 recorded results are turns nobody asked for, and an async subagent
// streams past its own result - so a Wake-written completion would be false
// 3 times in 46. The agent's words are the agent's claim; Wake only relays.
// The marker is for the 8 of 52 turns that produce no words at all.
//
// The corpus's own discriminator for those three - a result's origin - is not
// decoded, and nothing here may read it: it is a policed word the airlock has
// never spelled. Which is the point. Wake asserts nothing about completion, so
// it needs no field to be right about it.
//
// # Cost
//
// Its methods take value receivers and return a new Fleet, and the copy that
// makes that safe is gated on something having changed. More than half the
// stream is lifecycle chatter that changes nothing about any agent, and a
// fleet-sized copy per chatter frame would be exactly the "work per frame that
// could be work per change" the non-negotiables forbid.

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// Agent is one agent as the client knows it: what the daemon reported, plus
// what this client has counted.
//
// Every field is comparable on purpose - Observe decides whether an event
// changed anything by comparing two of these, so a slice or a map here is a
// compile error rather than a silent copy on every frame.
type Agent struct {
	ID    string
	Name  string
	Label string

	// Cwd is where this session is running now, which is not where it was
	// started once an agent has used EnterWorktree. Every surface here wants
	// the first; the startup directory is the daemon's business - park, unpark
	// and fork - and reaches no view.
	Cwd string

	// ParentID is the session this agent was forked from, or empty. It comes
	// from the report alone - nothing on Claude's wire says a session was
	// forked or from what, in either direction, so there is no event to derive
	// it from and no staleness to worry about: it never changes.
	ParentID string

	// State is one of the rpc.State constants. Empty until a report arrives -
	// an agent first seen through an event is real but not yet described.
	State string

	QuietMS int64

	// startedAt is when the current turn began, and drives the heartbeat's age.
	// Zero for an agent that was already working when this client attached -
	// see Agent.turnAge, which reads that as a turn just begun rather than
	// backdating it to the zero time.
	startedAt time.Time

	Tool    string
	ToolArg string

	// Effort is the reasoning level this session runs at, or "" for one Wake
	// chose nothing for. It comes from the report alone: nothing on claude's
	// wire says it, so the daemon's record is the only source there is.
	Effort string

	// Budget is the spend ceiling this session was started under, from the
	// report for Effort's reason. It is what Wake **asked for** - see
	// rpc.SessionStatus.Budget, which refuses to let it be drawn as progress.
	Budget string

	// MCPNeedsAuth is how many of this session's MCP servers are waiting to be
	// authenticated - the count Claude Code puts a warning row on.
	//
	// **A count and not the roster**, which is this type's own rule rather than
	// a shortcut: every field here is comparable because Observe decides whether
	// an event changed anything by comparing two Agents, and a slice would make
	// that a compile error - or, worse, a fleet-sized copy on every one of the
	// lifecycle frames that are more than half the stream. The airlock carries
	// the whole roster because it does not decide; reducing it to the number one
	// surface needs is the decision, and it belongs here.
	//
	// Off the event stream like Model and the context figures, and zero for the
	// same reason: a session this client has not watched take a turn has not
	// described itself yet. init arrives at the start of every turn, so the row
	// appears within seconds of a live agent doing anything.
	MCPNeedsAuth int

	// advertised is every slash command this session's claude announced on its
	// last init, or nil for one that has not taken a turn yet. The completion
	// menu offers it; nothing routes on it.
	//
	// **A pointer to an immutable set, which is the one shape that keeps this
	// struct comparable.** Every other field here is a value because Observe
	// decides whether an event moved anything by comparing two Agents, and a
	// slice would make that a compile error - or a fleet-sized copy on every
	// lifecycle frame. withFacts replaces the pointer only when the words
	// differ, so re-advertising the same 133 commands each turn compares equal.
	advertised *commandSet

	// Doing is the present-tense label of whatever task the agent last marked
	// in progress - claude's activeForm, which is the word it puts on its own
	// working line. Empty for an agent that has written no task list, which is
	// most of them; the heartbeat falls back to its word pool there.
	Doing string

	// LastLine is the last line of the agent's own most recent prose, bounded
	// at the fold so a fleet of thirty holds thirty rows and not thirty
	// paragraphs. The board's row draws it; a subagent's prose does not move
	// it, the room's own exclusion.
	LastLine string

	// What the session runs as and how full its window is, folded from the
	// lifecycle frames that carry them. Each is kept until something says
	// otherwise: init names a model and no window, a result names both, and a
	// zero from either would blank a status bar once per turn.
	//
	// These come off the event stream and not off the report, so they are
	// empty for a session this client has not yet watched take a turn. init
	// arrives at the *start* of every turn, so a model appears within seconds
	// of a live agent doing anything; a context figure waits for the first turn
	// to end. Putting them on rpc.SessionStatus is what would close that, and
	// it is daemon work.
	Model         string
	ContextTokens int
	ContextWindow int

	// Tokens is what this session has produced, summed over its turns. A total
	// rather than a level, because that is what the wire gives: each result
	// reports its own turn's output, so this is a sum of increments and not a
	// delta of a running figure - which is what keeps it clear of the trap
	// CLAUDE.md records against total_cost_usd.
	//
	// It restarts at a /clear, because that is what /clear does to the session
	// the number describes. See fold's KindSessionReset.
	Tokens int

	// TurnTokens is what the turn *in flight* has produced, summed from the
	// message_delta frames as they arrive and cleared when the turn ends.
	//
	// A different figure from Tokens above rather than a fresher one, and the
	// two are never added: Tokens is every completed turn and this is the one
	// still running, so a surface wanting "what is this costing me right now"
	// reads this and one wanting "what has this session cost" reads that. It
	// exists only under --include-partial-messages; without those frames it
	// stays zero and the surfaces that draw it draw nothing.
	TurnTokens int

	// turnDone and turnCur are how TurnTokens is arrived at: the messages of
	// this turn that have finished, and the newest cumulative figure for the one
	// in flight. Unexported because the sum is the only thing worth reading -
	// see withFacts for why it is not one addition.
	turnDone int
	turnCur  int

	// Unread is room lines from this agent that arrived while its DM was not
	// the focused pane. An hour inside one DM must not cost you what
	// accumulated everywhere else.
	Unread int

	// spoke records that this agent has produced prose in the turn it is
	// currently inside, which is what decides whether its turn end needs a
	// marker. Unexported: it is bookkeeping, not something a view renders.
	spoke bool

	// inDM records that the turn this agent is currently inside was sent from
	// its own DM, so the room draws none of it. A turn nobody started that way
	// - an agent speaking on its own, or anything addressed from the room - is
	// public, which is the right default: the room is where an agent that needs
	// you says so.
	inDM bool
}

// Fleet is every agent this client knows about.
type Fleet struct {
	// agents is copied on every write - and only on a write. A caller can hold
	// an older Fleet and it stays the fleet it had, which is the same contract
	// DM and transcript keep.
	agents map[string]Agent

	// order preserves first-seen order, so two agents with identical
	// attention rank do not swap places between frames. Rank sorts within it.
	order []string

	// focused is the agent whose DM is open, or "" for none. Its arrivals are
	// read the moment they land.
	focused string

	// tasks is what each session has dispatched, keyed on session id. A second
	// map rather than a field on Agent because Agent must stay comparable -
	// see fleettasks.go, which owns the fold and the argument.
	tasks map[string]Tasks

	// checklists is each session's live task list, accumulated from the run of
	// TaskCreate/TaskUpdate ops that build it - claude's replacement for the
	// retired TodoWrite, which sent the whole list every call. A second map for
	// tasks' reason, and folded by foldChecklist. See checklist.go.
	checklists map[string]checklist

	// parked is the daemon's park book: sessions nothing is holding, which this
	// client can name and resume and cannot do anything else with.
	//
	// **Deliberately not in agents**, and that separation is the feature. These
	// draw no roster row, open no conversation and take no cursor - a daemon
	// that put them in the fleet handed back the whole roster after a ⌃Q. They
	// exist here so `/resume <name>` has something to resolve against, which is
	// the only surface that reads them.
	parked []Agent
}

func NewFleet() Fleet {
	return Fleet{agents: map[string]Agent{}, tasks: map[string]Tasks{}, checklists: map[string]checklist{}}
}

// WithStatus folds a fleet report. Both kinds are folded: a reply and a push
// are the same snapshot, and a snapshot can miss a change but never invent one.
//
// An ended session is kept rather than deleted, so the roster does not lose a
// row the moment an agent dies - daemon.recentEndings exists for the same
// reason, so a client that missed the announcement can still learn a session
// ended. What it ended *of* is on the report and is not carried here; see
// notCarriedOntoAnAgent, which is checked against rpc.SessionStatus's own
// fields.
func (f Fleet) WithStatus(st *rpc.Status) Fleet {
	if st == nil {
		return f
	}
	f = f.copy()
	for _, s := range st.Sessions {
		a := f.agents[s.ID]
		if a.ID == "" {
			a.ID = s.ID
			f.order = append(f.order, s.ID)
		}
		a.Name, a.Label, a.Cwd, a.ParentID = s.Name, s.Label, runningIn(s), s.ParentID
		a.Effort, a.Budget = s.Effort, s.Budget

		// Stamped on the way *into* working, so the heartbeat measures the turn
		// rather than the report: reports fire on a state change, but an agent
		// stays working across ten tool calls and several of them.
		if s.State == rpc.StateWorking && !turnInFlight(a.State) {
			a.startedAt = clock()
			// A new turn owns the count as well as the clock, and clearing it
			// *here* is what covers the ends fold cannot see. Every `result`
			// frame reaches KindTurnEnd, interrupted ones included - but a turn
			// can end with no result at all: ⌃C closes stdin on a working agent,
			// and the next thing that session does is a new turn under this same
			// record. Clearing only at the end carried the parked turn's figure
			// into it and every turn after.
			a.TurnTokens, a.turnDone, a.turnCur = 0, 0, 0
		}
		a.State, a.QuietMS = s.State, s.QuietMS
		// Only when the report has one. The event stream is the fresher
		// source between state changes - see rpc.SessionStatus.Tool - so a
		// report assembled before the current call must not blank the row.
		if s.Tool != "" {
			a.Tool, a.ToolArg = s.Tool, s.ToolArg
		}
		f.agents[s.ID] = a
	}

	// Replaced wholesale rather than merged, unlike the agents above. The book
	// is the daemon's and a resume removes an entry from it, so a fold that kept
	// what a previous report named would keep offering a session that has
	// already come back.
	f.parked = nil
	for _, s := range st.Parked {
		f.parked = append(f.parked, Agent{
			ID: s.ID, Name: s.Name, Label: s.Label, Cwd: runningIn(s),
			Effort: s.Effort, State: s.State,
		})
	}
	return f
}

// Parked is the park book, for the one caller that resumes out of it.
func (f Fleet) Parked() []Agent { return f.parked }

// Observe folds one event and returns what the room should draw for it.
//
// The copy happens last and only if the agent's record actually moved. An
// event that changes nothing - which is most of them - returns the receiver
// itself, so leaving Wake open beside a busy fleet costs a map lookup and a
// struct comparison per frame rather than a fleet-sized copy.
//
// An agent this client has never heard of is added here rather than waiting
// for a report: fan-out starts before the spawn's confirmation is enqueued, so
// an event can precede every report there will ever be. Both halves of that -
// the map entry and the order slice - are written after the copy, because an
// append to order on a Fleet held by value is lost while the write through the
// map lands, which leaves an agent the map knows and the roster never lists.
func (f Fleet) Observe(ev core.Event, sessionID string) (Fleet, []core.Event) {
	was, known := f.agents[sessionID]
	if !known {
		was = Agent{ID: sessionID}
	}

	now, out := fold(was, ev, sessionID)
	if len(out) > 0 && countsAsUnread(ev.Kind) && sessionID != f.focused {
		now.Unread++
	}
	// Folded before the return below and tested by it: a dispatch frame moves
	// nothing on Agent, so `now == was` holds for every one of them and a
	// return taken on that alone would drop the whole fold.
	tasks, dispatched := f.foldTask(ev, sessionID)
	if known && now == was && !dispatched {
		return f, out
	}

	f = f.copy()
	if !known {
		f.order = append(f.order, sessionID)
	}
	f.agents[sessionID] = now
	if dispatched {
		f.tasks[sessionID] = tasks
	}
	return f, out
}

// activeForm is the present-tense label of the item a task list marks in
// progress, and "" for a list with none. The first one wins: claude's own tool
// description says at least one task is in progress at all times, and two would
// be the model contradicting itself rather than a case to render.
func activeForm(todos []core.Todo) string {
	for _, t := range todos {
		if t.Status == core.TodoActive {
			return t.ActiveForm
		}
	}
	return ""
}

// withFacts folds what a frame said about the session itself, keeping what it
// did not mention. Every field is last-writer-wins over the frames that name
// it, and a frame naming none returns the agent untouched.
func (a Agent) withFacts(f *core.SessionFacts) Agent {
	if f == nil {
		return a
	}
	if f.Model != "" {
		a.Model = f.Model
	}
	if f.ContextWindow > 0 {
		a.ContextWindow = f.ContextWindow
	}
	if f.ContextTokens > 0 {
		a.ContextTokens = f.ContextTokens
	}
	// Recounted only when the frame carried a roster: a result frame names no
	// servers, so counting unconditionally would zero the warning once per turn
	// - the same trap Model and the window are guarded against just above.
	if len(f.MCPServers) > 0 {
		a.MCPNeedsAuth = needsAuth(f.MCPServers)
	}
	// Same guard, and one more reason: the pointer is kept when the words have
	// not changed, so an unchanged set compares equal and costs no copy.
	if len(f.SlashCommands) > 0 && !a.advertised.same(f.SlashCommands) {
		a.advertised = &commandSet{names: slices.Clone(f.SlashCommands)}
	}
	// Summed, not replaced: every result reports its own turn's output.
	a.Tokens += f.OutputTokens
	// **Replaced within a message and summed across them**, which is the one
	// piece of arithmetic in this feature that is easy to get wrong: the figure
	// is cumulative for the message it belongs to, and a message emits one or
	// more of them. Adding them up reported 250 for a message that produced 150.
	// turnDone carries the messages already finished; see turnMessage.
	if f.TurnOutputTokens > 0 {
		a.turnCur = max(a.turnCur, f.TurnOutputTokens)
		a.TurnTokens = a.turnDone + a.turnCur
	}
	return a
}

// turnMessage closes whatever message was in flight and opens the next.
//
// The boundary is what makes a turn's figure addable at all - without it there
// is no telling one message's cumulative count from the next one's. See
// core.KindMessageStart.
func (a Agent) turnMessage() Agent {
	a.turnDone += a.turnCur
	a.turnCur = 0
	a.TurnTokens = a.turnDone
	return a
}

// runningIn is where a session is now rather than where it started. Every
// reader of Agent.Cwd wants the first - the workspace strip, the banner's path,
// and bangDir, which is where `!cmd` runs. The startup directory is the
// daemon's business (park, unpark, fork) and reaches no surface here.
func runningIn(s rpc.SessionStatus) string {
	if s.Cwd != "" {
		return s.Cwd
	}
	return s.Dir
}

// fold is the filter itself: what one event does to an agent's record, and
// what the room draws for it. Pure, so the room's whole policy is one function
// a table test can drive.
func fold(a Agent, ev core.Event, sessionID string) (Agent, []core.Event) {
	a = a.withFacts(ev.Session)
	switch ev.Kind {
	case core.KindToolUse:
		// What an agent is doing, which is the sidebar's column and never the
		// room's. A subagent's tool call is not the agent's own activity: 26
		// of the corpus's 44 tool calls are a subagent's.
		if ev.Tool != nil && ev.Subagent == nil {
			a.Tool, a.ToolArg = ev.Tool.Name, ev.Tool.Display
			// Kept rather than replaced when a call carries no list: this is
			// what the agent said it was doing, and most of its tool calls are
			// not the one that said it.
			if doing := activeForm(ev.Tool.Todos); doing != "" {
				a.Doing = doing
			}
		}
		return a, nil

	case core.KindAssistantText:
		if ev.Subagent != nil || blank(ev.Text) {
			return a, nil
		}
		a.spoke = true
		a.LastLine = lastProseLine(ev.Text)
		return a, []core.Event{ev}

	case core.KindUserText:
		// The same question the prompt walk asks of the same frames, so the two
		// cannot come to different answers about whose words they are - see
		// typedByHand in prompts.go.
		if !typedByHand(ev) {
			return a, nil
		}
		return a, []core.Event{ev}

	case core.KindPermissionRequest, core.KindRequestWithdrawn:
		// Both go to the room, which promotes them into cards; the agent record
		// keeps no ask id of its own - Cards owns that, seeded from events here
		// and reconciled against the report's rpc.SessionStatus.RequestIDs.
		return a, []core.Event{ev}

	case core.KindTurnEnd:
		// Empty between turns, the same rule Tool keeps: this is what the agent
		// said it was doing *now*, and carried into a later turn it becomes a
		// claim about work that is not happening.
		//
		// TurnTokens goes with them, and it is the whole of what makes it a
		// *turn's* figure rather than a session total arriving more often. The
		// result that ends this turn has already been added to Tokens above, by
		// withFacts, so nothing is lost by clearing it here.
		a.Tool, a.ToolArg, a.Doing = "", "", ""
		a.TurnTokens, a.turnDone, a.turnCur = 0, 0, 0
		spoke := a.spoke
		a.spoke, a.inDM = false, false
		if spoke {
			return a, nil
		}
		return a, []core.Event{{Kind: core.KindTurnEnd, SessionID: sessionID}}

	case core.KindMessageStart:
		// One message of this turn ended and the next began, which is the only
		// thing that makes the counts on the deltas addable. See turnMessage.
		return a.turnMessage(), nil

	case core.KindSessionReset:
		// /clear. The conversation the totals describe is gone, so the totals
		// go with it - a token count that survived would be a figure for a
		// session the model no longer has, and a context level would be a
		// percentage of a window that has just been emptied. The room still
		// draws the reset itself; this only forgets the accounting.
		//
		// The turn's own figure goes with them and for the same sentence: it
		// describes a turn in a conversation the model no longer has.
		a.Tokens, a.ContextTokens = 0, 0
		a.TurnTokens, a.turnDone, a.turnCur = 0, 0, 0
		return a, nil

	default:
		return a, nil
	}
}

// ForgetTurns drops what this client believed about every turn in flight: the
// tool it was running, the task it named, how much it has produced, and when it
// began.
//
// **Only what the stream told it.** fold's KindTurnEnd clears two more - spoke
// and inDM - and neither belongs here, because the gap that calls this is no
// reason to doubt either. `dropped` is one counter for the whole fleet, so a
// frame lost for *any* agent lands here for *every* agent:
//
//   - **inDM is not stream-derived at all.** It is set by Fleet.sending the
//     moment the operator presses send in a DM, and cleared the same way for a
//     broadcast. Clearing it on a gap discards a fact this client still knows,
//     and the turn it belongs to may still be running - so the rest of that
//     private turn's prose would go into the room.
//   - **spoke cleared mid-turn draws a second ending.** It is what suppresses
//     the room's finished marker for a turn that already produced words; reset
//     before the real KindTurnEnd arrives, the marker is drawn under prose that
//     plainly finished.
//
// A stale inDM after an ending nobody saw is a real residual, and it is narrow:
// a broadcast sets it false before its own turn, so only an *unprompted* turn
// can inherit one. See docs/notes/bugs.md.
//
// **A reset needs a boundary this client saw**, and two things make one
// missable: the inbox drops the oldest frame when a window is too slow to keep
// up, and a reattach comes back onto a daemon that keeps no replay buffer. Miss
// a turn's result *and* the idle report behind it, and the next turn's deltas
// land on top of the last one's - with nothing to correct it until a boundary
// that does arrive.
//
// So a gap forgets instead. It is forgotModes' ruling about the same class of
// thing: a belief that survived the gap is one nothing can confirm. What it
// costs is a working agent reading as having just begun and having produced
// nothing, which is exactly what a client that has just attached shows - and
// turnAge already spells that case out.
func (f Fleet) ForgetTurns() Fleet {
	f = f.copy()
	for id, a := range f.agents {
		a.Tool, a.ToolArg, a.Doing = "", "", ""
		a.TurnTokens, a.turnDone, a.turnCur, a.startedAt = 0, 0, 0, time.Time{}
		f.agents[id] = a
	}
	return f
}

// turnInFlight is whether a state means "this session owes a turn", which is
// what says a move into working is a turn *resuming* rather than starting.
//
// Read off the daemon's own stateLocked rather than guessed at, and the two
// that are easy to get wrong are the two that decide this: **blocked** is a
// permission ask outstanding with the turn still owed, and **silent** is
// owed-and-quiet. Both are the same turn as the working either side of them.
// Treating either as a boundary restarts the turn's clock and throws away the
// tokens it has produced, so a permission answered halfway through leaves the
// row counting the suffix while the result frame states the whole.
//
// The others are boundaries by construction: idle is `!owed`, and parked, ended
// and orphaned have no process owing anything.
//
// Its domain is derived from stateGlyph by
// TestEveryStateTheRosterDrawsIsInFlightOrIsNot, so a seventh state is a
// decision somebody has to make here rather than one that silently reads as a
// boundary.
func turnInFlight(state string) bool {
	switch state {
	case rpc.StateWorking, rpc.StateBlocked, rpc.StateSilent:
		return true
	default:
		return false
	}
}

// countsAsUnread: everything the room draws is something you have not seen,
// except the words you typed yourself.
//
// The quiet marker counts, and that is the case worth stating. For 8 of 52
// recorded turns it is the only thing the room shows, so an agent whose turn
// said nothing would otherwise leave a line in the room with no badge anywhere
// saying it is there.
// A progress frame is not something you have not read either: it draws no line
// anywhere, and thirty working agents would otherwise drive every badge in the
// sidebar on their own. It never reaches this - fold returns nothing for it, and
// Observe only counts an event the room takes - but the pair is stated here
// because that is where the question is answered.
func countsAsUnread(kind core.EventKind) bool {
	return kind != core.KindUserText && kind != core.KindTurnTokens
}

// blank is text with nothing in it.
//
// The decoder does not drop an empty text block and dm_blocks.userBlock
// already renders one as nothing, so an empty run of prose would set spoke,
// suppress the quiet marker, and leave the turn showing nothing at all in the
// room - the failure the marker exists to prevent, arriving through the branch
// meant to prevent it.
func blank(s string) bool { return strings.TrimSpace(s) == "" }

// copy is the immutability contract. The map and the order slice are both
// duplicated, so a caller holding an older Fleet holds the fleet it had.
func (f Fleet) copy() Fleet {
	out := Fleet{
		agents:     make(map[string]Agent, len(f.agents)+1),
		tasks:      make(map[string]Tasks, len(f.tasks)+1),
		checklists: make(map[string]checklist, len(f.checklists)+1),
		focused:    f.focused,
	}
	maps.Copy(out.agents, f.agents)
	// A session's list is replaced wholesale by foldChecklist, never appended in
	// place, so the shallow copy is all this needs - Tasks' own reason.
	maps.Copy(out.checklists, f.checklists)
	// Copied for agents' own reason: a caller holding an older Fleet keeps the
	// dispatches it had. Tasks is a value whose slice is never written in
	// place - Tasks.Observe copies before it appends - so the shallow copy is
	// the whole of what this needs.
	maps.Copy(out.tasks, f.tasks)
	out.order = append(make([]string, 0, len(f.order)+1), f.order...)
	// Carried, not rebuilt: Observe copies for every event that moves an agent,
	// and a book dropped there would take /resume's only index with it between
	// one status report and the next.
	out.parked = append([]Agent(nil), f.parked...)
	return out
}

func (f Fleet) Agent(id string) (Agent, bool) { a, ok := f.agents[id]; return a, ok }

// OnRoster is every agent the sidebar draws and the cursor can land on: the
// fleet minus the ones that have ended.
//
// An ended session cannot be messaged, resumed or parked, and its name has
// already gone back to the pool - so a row for one is a name on screen that
// nothing can be done with, and it can be a name a *live* agent now also has.
// Its ending is announced in the room and kept by `wake status`, which are the
// two surfaces whose subject is what happened rather than what to do next.
func (f Fleet) OnRoster() []Agent {
	out := make([]Agent, 0, len(f.order))
	for _, id := range f.order {
		if a := f.agents[id]; a.State != rpc.StateEnded {
			out = append(out, a)
		}
	}
	return Rank(out)
}

// sending records where the turn about to start was sent from, so the room can
// draw a turn addressed to the room and leave a private one in its own DM.
//
// It is per client and lasts one turn: a reattached window has no memory of a
// turn it did not send, and treats what arrives as public. That is the safe
// direction of the two - a reply in the room is noise, a reply nowhere is lost.
func (f Fleet) sending(id string, inDM bool) Fleet {
	a, known := f.agents[id]
	if !known || a.inDM == inDM {
		return f
	}
	a.inDM = inDM
	f = f.copy()
	f.agents[id] = a
	return f
}

// inDM says whether this session's in-flight turn was sent from its own DM.
func (f Fleet) inDM(id string) bool { return f.agents[id].inDM }

// ByName resolves a display name to an agent. Exact, never a prefix: the
// daemon guarantees no two *live* sessions share a name, which is what makes
// this unambiguous, and prefix resolution belongs to `wake attach` where a
// human is typing at a shell rather than routing a message.
func (f Fleet) ByName(name string) (Agent, bool) {
	for _, id := range f.order {
		if a := f.agents[id]; a.Name == name && a.State != rpc.StateEnded {
			return a, true
		}
	}
	return Agent{}, false
}

// Agents is every agent in attention order.
//
// It sorts on every call rather than caching a ranked slice inside the Fleet,
// and the reason is the batch: App folds every frame that arrived while it was
// busy and draws once, so a rank computed on every change would run many times
// per drawn frame, while a rank computed per call runs once. Lazy is the
// cheaper of the two here, not the lazier.
//
// Measured at 30 agents: 6.8µs against a View that costs ~250µs. Affordable
// once per frame and not more than once - a caller wanting the roster twice in
// one draw should hold the slice.
func (f Fleet) Agents() []Agent {
	out := make([]Agent, 0, len(f.order))
	for _, id := range f.order {
		out = append(out, f.agents[id])
	}
	return Rank(out)
}

// MarkRead clears one agent's unread count.
func (f Fleet) MarkRead(id string) Fleet {
	a, ok := f.agents[id]
	if !ok || a.Unread == 0 {
		return f
	}
	f = f.copy()
	a.Unread = 0
	f.agents[id] = a
	return f
}

// Focus records whose DM is open, and reads what has accumulated for them.
//
// It records no agent of its own. "" is how "no DM is open" is spelled, and an
// id nobody has reported yet is not a row - inventing one here would put an
// empty line in the roster for whatever the caller last had selected.
func (f Fleet) Focus(id string) Fleet {
	f = f.copy()
	f.focused = id
	return f.MarkRead(id)
}

// Focused is the agent whose DM is open, or "" for none.
func (f Fleet) Focused() string { return f.focused }

// needsAuth is how many servers are waiting to be authenticated.
//
// The comparison is against core's word, never a literal: internal/ui does not
// spell Claude's vocabulary, which is the rule the permission modes carry.
func needsAuth(servers []core.MCPServer) int {
	n := 0
	for _, s := range servers {
		if s.State == core.MCPNeedsAuth {
			n++
		}
	}
	return n
}

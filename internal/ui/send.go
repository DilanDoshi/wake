package ui

// Sending: where a draft goes, and how many turns that costs.
//
// # Everything this file writes goes out as one command
//
// bubbletea runs every tea.Cmd on its own goroutine, and rpc's write lock is
// process-wide and held across the socket write. So a broadcast built as N
// commands would be thirty goroutines queueing on one lock for one keystroke,
// with the UI still echoing messages that are going nowhere. write takes the
// frames rather than a frame, and there is one place for that to be true.

import (
	"fmt"
	"maps"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// sendFailed and interruptFailed name the write that could not happen, so
	// the notice row says which key was pressed rather than only what the
	// socket said about it.
	sendFailed      = "sending that message"
	interruptFailed = "interrupting that turn"
	answerFailed    = "answering that request"

	// escDismissReason is what the model is told when esc dismisses a question.
	// esc on a question is a deny rather than an interrupt (see interrupt), so
	// this reaches the model verbatim as the tool result the way any refusal
	// does - a blank one reads as a tool that failed for no reason.
	escDismissReason = "the operator dismissed this question without answering"

	// NoAddressee is what the room says about a draft addressed to nobody. It
	// is a refusal rather than a guess: with thirty agents listening, inventing
	// a recipient is the misroute core.Route.Resolved exists to prevent.
	//
	// It names the verb that removes the refusal, which is the legend rule read
	// into a sentence: an operator who is told only that the room will not guess
	// has no way to learn that there is a thing whose whole job is being the
	// answer.
	//
	// **It named `wake manager` until /manager shipped, and the change is the
	// rung-7 rule getting cheaper rather than being dropped.** A shell verb is a
	// claim about a part of the build this package never reads, which is why
	// cmd/wake grew a scan holding every `wake <verb>` any sentence in here
	// offers to the verbs it actually dispatches. This names a command in *this*
	// package instead, built from managerVerb rather than spelled - so a
	// sentence that outlived its command is a compile error rather than a check.
	// It stays exported because the room is not the only thing that reads it.
	NoAddressee = "address this with @name, or @all - the room does not guess who you meant. " + managerVerb + " starts the manager, and an unaddressed message then goes to it"

	// managerParked is the same refusal when the manager exists and is parked.
	//
	// A second sentence rather than a widening of the one above, for
	// parkedAdvice's reason: NoAddressee's remedy used to be **wrong** here. The
	// name is still claimed, so `wake manager` failed - and it failed saying "a
	// manager is already running; @manager reaches it", which is false on both
	// clauses. Each sentence sent the operator to the other and neither named
	// the verb that works. That is the loop `wake attach`'s parked refusal was
	// rewritten for in Phase 3, arriving in two sentences written after the
	// lesson was recorded.
	//
	// **Both sentences name `/manager` now, and that is what ends the loop
	// rather than merely correcting it.** They still differ, because they are
	// about different states - one manager is missing and one is parked, which
	// is the difference between a spawn and a wake - but the *remedy* no longer
	// does, so a refusal that picked the wrong sentence would no longer send
	// anybody to a command that would be refused. `/resume manager` still works
	// and is no longer advertised: it is the general verb, and this is the one
	// word that is right in every state.
	managerParked = "the manager is parked, so the room has no default addressee. " + managerVerb +
		" brings it back, or address this with @name or @all"

	// noneListening is a broadcast with nobody live to receive it. A different
	// sentence, because core.Resolve produces an empty target list for both and
	// telling somebody who typed @all to address it with @all is advice about a
	// thing they already did.
	noneListening = "nobody is listening: every agent has ended, or none has started"

	// endedAdvice is a message with nowhere to go.
	endedAdvice = "this session has ended, so nothing more can be sent to it. " + detachAdvice

	// parkedAdvice is a message to a conversation that is one command from
	// coming back. It is a second sentence beside endedAdvice rather than a
	// widening of it: an ended session is over and a parked one is waiting, and
	// the difference between those two is a verb the operator can type.
	parkedAdvice = "%s%s is parked. " + resumeVerb + " %s brings it back."

	// noInterruptTarget is ⎋ with no agent in front of it. ⎋ stops one agent's
	// turn; the room is not one agent, and stopping the whole fleet is not what
	// anybody pressing it meant.
	noInterruptTarget = "esc stops one agent's turn. " + noPaneAdvice

	// interruptedFormat says whose turn was stopped, and it is said only from
	// the room. A DM names its agent in its own header, so success there is
	// silent - but in the room the target is whoever the roster cursor is on,
	// which closeDM does not clear, so it can be an agent chosen by a selection
	// the operator made some time ago. A key that stops work somewhere the eye
	// is not resting has to say where.
	interruptedFormat = "stopped %s%s's turn"
)

// submit sends the focused draft.
//
// # The single-source rule
//
// A sent message must appear either as a frame the transcript replayed or as a
// local echo, never both, and Wake owns which. It is the local echo, because
// today it is the only one that exists: core.Session's argv does not carry
// --replay-user-messages, so nothing comes back for the user's own turn, and
// without an echo a person's message is invisible in their own conversation the
// moment they press enter.
//
// Neither pane de-duplicates - both draw exactly what they are handed - so
// whoever adds that flag has to delete these echoes in the same change. What
// the flag emits has never been recorded, which is precisely why the
// authoritative half is the one Wake can see.
func (a App) submit() (tea.Model, tea.Cmd) {
	text := a.composer().Value()
	// Attachments still referenced by a chip in the draft. A blank draft has no
	// chips, so it also has no images - the empty guard below stays honest.
	images := a.composer().Images()
	if strings.TrimSpace(text) == "" {
		return a, nil
	}
	// A bang is a local command, not a message, and it is asked before
	// anything else: `!ls` is addressed to nobody, and a session that has
	// ended is still a session somebody wants to run `!git status` in.
	if a, cmd, ok := a.bang(text); ok {
		return a, cmd
	}
	// A slash command is addressed to Wake, exactly as a bang is - and for the
	// same reason it is asked before the ended check: `/resume` is the one
	// thing you type into a conversation whose agent is not running.
	if a, cmd, ok := a.slash(text); ok {
		return a, cmd
	}
	// A `/rename bob` is claude's own word, so the router above leaves it a
	// message - and Wake mirrors it onto its own handle in the same keystroke,
	// so the roster and claude's title do not drift, which was the reported
	// confusion. mirror is nil for every other draft, so an ordinary send is
	// still one command. See renameMirror.
	mirror := a.renameMirror(text)
	if a.focus != "" {
		model, cmd := a.sendDM(text, images)
		return model, tea.Batch(mirror, cmd)
	}
	model, cmd := a.sendRoom(text, images)
	return model, tea.Batch(mirror, cmd)
}

// sendDM writes one message to the one agent a DM is with. There is nothing to
// route: the pane names its recipient in its own header.
func (a App) sendDM(text string, images []core.ImageBlock) (tea.Model, tea.Cmd) {
	if a.parkedAgent(a.focus) {
		// Above the ended arm, because a parked session is an ended one from
		// every angle below the daemon and would otherwise be answered with
		// "nothing more can be sent to it" - which is wrong by one command.
		//
		// The draft is kept, for endedAdvice's reason - and here it is worth
		// more, because the thing to do next is one command and then send it.
		notice.Report(parkedAdvice, agentPrefix, a.agentName(a.focus), a.agentName(a.focus))
		return a, nil
	}
	if a.endedAgent(a.focus) {
		// The draft is kept rather than cleared: there is nothing to send it
		// to, and clearing it would destroy what someone just typed on top of
		// telling them it went nowhere.
		notice.Report("%s", endedAdvice)
		return a, nil
	}
	// After the refusals and before the send: a configure command needs a
	// session that can receive text, and the two arms above are the two ways
	// there is not one. The pane names its own recipient, so there is nothing
	// to resolve.
	if next, cmd, ok := a.configure([]string{a.focus}, text); ok {
		return next, cmd
	}
	id := a.focus
	wire := a.composer().WireText(text)
	a = a.clearDraft()
	a.fleet = a.fleet.sending(id, true)
	// The echo keeps the chips - what the operator typed, image markers and all
	// - while the wire text has the backed ones stripped and their images ride
	// beside it.
	a = a.withDM(id, a.dms[id].Append(core.Event{Kind: core.KindUserText, SessionID: id, Text: text}))
	return a, a.write(sendFailed, sendFrames([]string{id}, wire, images)...)
}

// sendRoom routes a draft the way §7 says: a leading @name that matches a live
// agent, @all for everybody, and nothing else addressed at all.
//
// The echo goes in once however many agents the message went to. One broadcast
// is one thing you said, and thirty copies of it would be the room reporting
// its own postage.
func (a App) sendRoom(text string, images []core.ImageBlock) (tea.Model, tea.Cmd) {
	// The manager is the default addressee when there is one, and nothing is
	// when there is not - never whichever agent this window happens to have
	// attached, which is the misroute Route.Resolved exists to prevent.
	//
	// Through App.route rather than core.Resolve, because mention mode decides
	// what a leading @name means and the composer has already drawn that
	// answer. Two callers of the router would be two answers to one question,
	// with the promise on screen and the turns on the wire free to disagree.
	//
	// Routed on the chip-stripped text, not the raw draft: a leading `[Image
	// #1]` chip would otherwise sit before an `@name` and defeat leadingMention,
	// silently sending a drop-then-mention to the manager instead of the named
	// agent. WireText also produces the wire body, so what routes and what sends
	// are one string - r.Text below is already chip-free.
	r := a.route(a.room.Composer().WireText(text))
	if len(r.Targets) == 0 {
		switch {
		case r.Broadcast:
			notice.Report("%s", noneListening)
		case a.hasParkedManager():
			// Before the general refusal, because the general one's remedy is
			// wrong here: the name is still claimed, so `wake manager` fails.
			notice.Report("%s", managerParked)
		default:
			notice.Report("%s", NoAddressee)
		}
		return a, nil
	}
	// After the no-targets refusals above, so a picker is only ever opened over
	// something it can be confirmed against. It reads the *direct* route: open
	// mode widens a message to the fleet and does not widen a knob.
	if cr := r.configureRoute(); len(cr.Targets) > 0 {
		if next, cmd, ok := a.configure(cr.Targets, cr.Text); ok {
			return next, cmd
		}
	}
	a = a.clearDraft()
	for _, id := range r.Targets {
		a.fleet = a.fleet.sending(id, false)
	}
	// Echoed as it was typed, mention and all: the room is the record of who you
	// said it to, chips included, while the agents get r.Text - already routed
	// off the chip-stripped draft above, so it carries the images' words and not
	// their markers.
	a = a.withRoom(a.room.Append(core.Event{Kind: core.KindUserText, Text: text}, Agent{}))
	a = a.echoToRouted(r.Targets, text)
	return a, a.write(sendFailed, sendFrames(r.Targets, r.Text, images)...)
}

// echoToRouted puts a routed message into each conversation it was addressed
// to, spelled as it was typed.
//
// **Only conversations this client already holds**, which is the rule
// App.observe uses for the agent's own events - and here it is load-bearing
// rather than symmetric. An unopened conversation is filled from claude's
// transcript when it opens, DecodeTranscriptLine keeps user lines as well as
// assistant ones, and neither pane de-duplicates: writing this turn here too
// would draw it twice, once as typed and once as the agent received it.
//
// At most one map copy for the whole fan-out, and none at all until a target
// turns out to be held. withDM copies App.dms per call, so @all at thirty
// agents would otherwise be thirty copies of it for one keystroke - and a room
// with no conversation open beside it, which is where most messages are typed,
// would pay for a copy that changes nothing.
func (a App) echoToRouted(targets []string, text string) App {
	var next map[string]*DM
	for _, id := range targets {
		if _, held := a.dms[id]; !held {
			continue
		}
		if next == nil {
			next = maps.Clone(a.dms)
		}
		dm := next[id].Append(core.Event{Kind: core.KindUserText, SessionID: id, Text: text, FromRoom: true})
		next[id] = &dm
	}
	if next == nil {
		return a
	}
	a.dms = next
	return a
}

// clearDraft empties the focused composer and re-reads where ↵ would now send.
func (a App) clearDraft() App {
	return a.withComposer(a.composer().Reset()).retarget()
}

// retarget recomputes the room composer's target line.
//
// It runs on the two things that can change the answer and on nothing else: a
// keystroke, which is what turns "hello" into "@all hello", and a fleet report,
// which is what turns "@john" from a word into an agent. Running it per drawn
// frame instead would be work per frame that is already work per change, which
// the non-negotiables forbid - and running it per *event* would be a fleet-wide
// name scan for every line thirty agents produce.
//
// Only the room's. A DM has one recipient and its header names them.
func (a App) retarget() App {
	draft := a.room.Composer().Value()
	if strings.TrimSpace(draft) == "" {
		a.room = a.room.WithComposer(a.room.Composer().WithTarget(roomRoute{}, 0))
		return a
	}
	// Routed on the chip-stripped text so the preview matches sendRoom: a leading
	// chip must not hide the `@name` behind it. The empty check stays on the raw
	// draft, so an image-only draft (chip, no words) still previews its default
	// addressee rather than reading as nothing typed.
	r := a.route(a.room.Composer().WireText(draft))
	a.room = a.room.WithComposer(a.room.Composer().WithTarget(r, len(r.Targets)))
	return a
}

// live is the fleet as an addressee list: every agent that could still read a
// message.
//
// An ended agent is left out rather than addressed, and so is one no report has
// named yet - a mention can only match a name, and an agent with no name has
// none to match. The daemon guarantees no two *live* sessions share a name,
// which is what makes core.Resolve's exact match unambiguous.
//
// A **parked** agent is left out for ended's reason rather than by analogy with
// it: its process has gone, so a message addressed to it is a frame the daemon
// refuses, and at thirty agents `@all` would spend one refusal per parked
// session on a single keystroke. Note the asymmetry with the name pool that is
// deliberate: a parked session keeps its name claimed, so nothing else can
// answer to it - the name is reserved *and* unaddressable, which is what makes
// it still the same agent when it comes back.
//
// **The manager is left out, and it is not an agent that failed a test here.**
// It is the service, and it arrives at core.Resolve separately - see service()
// below - which is what makes "a broadcast is to the fleet" true by
// construction. It still answers to `@manager`, from there.
func (a App) live() []core.Addressee {
	agents := a.fleet.Agents()
	out := make([]core.Addressee, 0, len(agents))
	for _, agent := range agents {
		if agent.Name == "" || agent.Name == core.ManagerName ||
			agent.State == rpc.StateEnded || agent.State == rpc.StateParked {
			continue
		}
		out = append(out, core.Addressee{ID: agent.ID, Name: agent.Name})
	}
	return out
}

// hasParkedManager reports whether the reason there is no default addressee is
// a manager somebody parked.
//
// It is asked only on the refusal path, which is what keeps it off every
// keystroke: retarget draws the composer's target line from service() alone,
// and a parked manager is simply not a target.
func (a App) hasParkedManager() bool {
	for _, agent := range a.fleet.Agents() {
		if agent.Name == core.ManagerName && agent.State == rpc.StateParked {
			return true
		}
	}
	return false
}

// service is the manager, or a zero Addressee when the fleet has none.
//
// Keyed on the name, which is safe for the reason internal/mcp keys on it:
// daemon/names.go refuses core.ManagerName to every ordinary spawn and
// names_test.go holds the daemon's reserved set equal to this package's own
// routing constants, so a session called `manager` is one the daemon
// deliberately named. It is a name doing display's job and not an address -
// what crosses the socket is still the id this returns.
//
// The same state filter as live(), and for the same reasons rather than for
// symmetry: a parked or ended manager has no process, so an unaddressed draft
// would be a message the daemon refuses - and a room whose manager has ended
// should say what is missing, which is exactly what a zero service makes it do.
func (a App) service() core.Addressee {
	for _, agent := range a.fleet.Agents() {
		if agent.Name != core.ManagerName ||
			agent.State == rpc.StateEnded || agent.State == rpc.StateParked {
			continue
		}
		return core.Addressee{ID: agent.ID, Name: agent.Name}
	}
	return core.Addressee{}
}

// sendFrames is one message addressed to each of N agents, carrying any dropped
// images alongside the text. images is nil for every ordinary send, and its
// omitempty tag keeps those frames byte-for-byte what they were.
func sendFrames(targets []string, text string, images []core.ImageBlock) []rpc.Frame {
	out := make([]rpc.Frame, 0, len(targets))
	for _, id := range targets {
		out = append(out, rpc.Frame{Kind: rpc.FrameSend, SessionID: id, Text: text, Images: images})
	}
	return out
}

// interrupt asks the daemon to stop the turn one agent is running.
//
// Esc, because that is the key Claude Code itself uses and this view's whole
// promise is that it feels like Claude Code. It is bound rather than left to
// the composer because the text area has no meaning for it, and because "stop
// what you are doing" is the thing an operator needs most urgently and least
// wants to go hunting for.
//
// It does not touch the model. The draft stays exactly where it was - a person
// types the correction and stops the turn in either order, and losing what they
// were writing would be a second thing gone wrong in the same keystroke - and
// nothing is echoed into either transcript, because Claude reports the abort
// itself, on both shapes, and drawing a second account of it would put two
// lines in the conversation for one event.
//
// A session that has ended has no turn to stop and nothing at the other end of
// the socket, so that case is silent. That is deliberately unlike submit, which
// reports: a message that is not sent is lost, an interrupt that is not sent
// costs nothing. Having no agent at all is *not* that case and does report -
// the operator pressed a key and nothing happened, which is the one outcome
// this project treats as worse than a refusal.
//
// The request_id is minted where the write happens, in core.Session, and not
// here. This layer has no way to know whether its frame ever reached a process,
// so an id invented here would name a request that might never exist.
func (a App) interrupt() (tea.Model, tea.Cmd, bool) {
	// In the room, ⎋ is about the draft. The room is not one agent, so the
	// alternative was stopping whichever one a cursor rested on - which is what
	// it did, and an operator who pressed ⎋ over a half-typed broadcast had a
	// turn stopped somewhere they were not looking. Claude Code's own ⎋ clears
	// the input, which is what the hands expect.
	if a.focus == "" && !a.composerEmpty() {
		return a.clearDraft(), nil, true
	}
	id, ok := a.interruptTarget()
	if !ok {
		notice.Report("%s", noInterruptTarget)
		return a, nil, true
	}
	if a.endedAgent(id) {
		return a, nil, true
	}
	// A question is not withdrawn by an interrupt the way a permission is. The
	// CLI cancels a pending permission on a control_cancel_request the moment
	// esc lands, so its card clears; an AskUserQuestion is preserved instead
	// (question-findings.md §9, still unrecorded), so the ask stays open and
	// the card never clears. So esc on a question settles it with a deny - the
	// same frame [d] writes, adding no unrecorded dependency [d] does not
	// already carry - which unblocks the agent and takes the card down here,
	// the way commitAnswer does.
	//
	// Only the focused pane's card, which is the one surface a card is drawn on
	// at all. And only a card whose shape is *known* to be a question: one
	// rebuilt from a fleet report on reattach carries no Ask and reads as a
	// permission, so esc interrupts it - which is right for the permission it
	// usually is, and leaves [d] as the way to clear the rarer report-only
	// question. cardOf is what the pane actually draws, so the card denied is
	// the one on screen.
	if a.focus != "" {
		if card, ok := a.cardOf(a.focus); ok && card.Shape() == ShapeQuestion {
			a.cards = a.cards.Settle(card.AgentID, card.RequestID)
			return a, a.write(answerFailed, card.Deny(escDismissReason)), true
		}
	}
	if a.focus == "" {
		notice.Report(interruptedFormat, agentPrefix, a.agentName(id))
	}
	return a, a.write(interruptFailed, rpc.Frame{Kind: rpc.FrameInterrupt, SessionID: id}), true
}

// agentName is what to call one agent in a sentence somebody reads. The id is
// the fallback rather than a blank: an agent no report has named yet is still a
// thing that was just stopped.
func (a App) agentName(sessionID string) string {
	if agent, ok := a.fleet.Agent(sessionID); ok && agent.Name != "" {
		return agent.Name
	}
	return sessionID
}

// interruptTarget is the one agent ⎋ stops, and ⌃C parks: the conversation the
// operator is looking at.
//
// Two sources, in the order of how explicitly each was chosen:
//
//  1. **The focused DM.** Opening a conversation and typing into it is the most
//     explicit statement there is about who you mean - and it is where every
//     card is now drawn, so a card's own ⎋ and the pane's are one target rather
//     than two that have to be reconciled.
//  2. **The roster cursor**, which is where ⇧⇥ leaves it and is the only
//     statement of intent the room has: it draws no card, so nothing else on
//     that surface names an agent. See pickedAgent - the selection is left
//     behind by whichever conversation was open last, and reading it rather
//     than the ring is what stopped ⌃C parking an agent nobody chose.
//
// There used to be a third between them - the agent whose card the room drew -
// and it went with the room's card. Its whole argument was that a card on
// screen names its agent more explicitly than a sidebar row does, which is
// still true and is now a statement about the *focused pane*: the card is in
// the conversation, so case 1 already is that case.
func (a App) interruptTarget() (string, bool) {
	if a.focus != "" {
		return a.focus, true
	}
	if agent, ok := a.pickedAgent(); ok {
		return agent.ID, true
	}
	return "", false
}

// write is every client write in this package: one command, however many
// frames.
//
// Bounded, and this is the call site the transport's own header names. rpc's
// write lock is process-wide and is held across the socket write, so a daemon
// that has stopped draining this client parks one goroutine per keypress - each
// holding or queueing for that lock, with the UI still working and still
// echoing messages that are going nowhere. The deadline turns that into a
// reported failure. See rpc.WriteFrameTo.
func (a App) write(what string, frames ...rpc.Frame) tea.Cmd {
	if a.conn == nil || len(frames) == 0 {
		return nil
	}
	conn := a.conn
	return func() tea.Msg {
		for i, f := range frames {
			err := rpc.WriteFrameTo(conn, f)
			switch {
			case err == nil:
			case len(frames) == 1:
				return errMsg{Err: fmt.Errorf("%s: %w", what, err)}
			default:
				// How far it got, because the local echo has already drawn the
				// message as sent and it is the single source of what you said.
				// At thirty targets a deadline expiring on the fifth leaves
				// twenty-five agents unmessaged, and "it failed" does not say
				// which of those two happened. Aborting is right - the rest
				// would queue behind the same stuck lock - but silently is not.
				//
				// Only when there was more than one frame: the count is noise
				// on a single write, and the notice row is one truncated line,
				// so noise in front of the cause is the cause off the end of it.
				return errMsg{Err: fmt.Errorf("%s (%d of %d sent): %w", what, i, len(frames), err)}
			}
		}
		return nil
	}
}

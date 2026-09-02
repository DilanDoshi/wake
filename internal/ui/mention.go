package ui

// Mention mode: what a leading `@name` means, and the one place that decides
// it.
//
// # Why it is a mode at all
//
// Spec §7 makes `@alex hey` configurable on the grounds that the two readings
// differ by 20× in cost: one turn, or one per agent. Nothing else in the room
// has that property - `@all` is expensive and says so, a DM is one agent by
// construction - so this is the only draft whose price is not written in what
// was typed. That is what the composer's target line is for, and why this file
// hands it the mode rather than only the answer.
//
// # What the mode may not touch
//
// Three routes are not a mention and are untouched: `@all`, which is already
// the send-to-everyone capability at the point of use; `@manager`, because the
// manager is the thing that manages the fleet rather than a member of it; and
// a draft with no mention at all, which goes to the manager or is refused.
// **That last one is the important one.** A widening keyed on "the route
// resolved" instead of on "a mention matched a fleet agent" reads identically,
// passes every fan-out test, and turns `who is free` into thirty turns - and
// the refusal of an unaddressed draft is deliberate and stays the default.

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
)

const (
	// mentionFlippedFormat is what ⌃T says it did, and it says what a mention
	// now reaches rather than only naming the mode.
	//
	// A report rather than silence, because the mode is drawn on the target
	// line and the target line is drawn only when there is a draft. ⌃T is
	// pressed *before* typing, on an empty composer, where nothing on screen
	// would move - and a key that silently changes what the next message costs
	// is the failure this project treats as worse than a refusal, arriving on
	// the one surface whose safety argument is "it is never a memory problem".
	//
	// The mode's own name is interpolated rather than spelled twice, so the
	// notice row and the composer cannot end up calling one reading two things.
	mentionFlippedFormat = "mentions are %s: %s"

	// What each reading means, in the words somebody who has just met them
	// needs. `direct` and `open` are this build's vocabulary and not anybody
	// else's.
	mentionDirectMeans = agentPrefix + "name reaches that agent alone"
	mentionOpenMeans   = agentPrefix + "name reaches the whole fleet, one turn each"
)

// mentionFlipped is the sentence for one reading.
func mentionFlipped(m MentionMode) string {
	means := mentionDirectMeans
	if m == MentionOpen {
		means = mentionOpenMeans
	}
	return fmt.Sprintf(mentionFlippedFormat, m, means)
}

// flipMention is ⌃T: the other reading, said out loud, with the room's target
// line re-read.
//
// retarget runs on a keystroke and on a fleet report and on nothing else,
// which is what keeps a fleet-wide name scan off every drawn frame. A flip is
// neither of those things happening to the draft, so it has to ask for the
// re-read itself - otherwise `→ @john · direct` stays on screen over a send
// that now fans out.
//
// It flips wherever it is pressed, including inside a DM. The mode is this
// client's rather than this pane's, and refusing it in a DM would mean finding
// the way back to the room before setting a mode to type into it.
func (a App) flipMention() (tea.Model, tea.Cmd, bool) {
	if a.mention == MentionOpen {
		a.mention = MentionDirect
	} else {
		a.mention = MentionOpen
	}
	notice.Report("%s", mentionFlipped(a.mention))
	return a.retarget(), nil, true
}

// # Where the mode lives
//
// On the App, not on a Composer. §7 says ⌃T flips it "for the session", which
// is this client rather than a pane: a DM is locked to one agent and has
// nothing to widen, so a per-composer copy would be N copies of a value only
// one of them can use, and ⌃T pressed inside a DM would flip the one that is
// not consulted. The room's composer draws it because the room is where it
// decides anything.

// MentionMode is the two readings of `@alex hey`.
//
// MentionDirect is the zero value and that is load-bearing rather than
// alphabetical: it is the reading this build has always had, it is one turn,
// and it is what an App nobody has said anything about routes with. §7 names
// it the default, and the expensive reading has to be one an operator asked
// for.
type MentionMode int

const (
	// MentionDirect: only the agent named receives it. One turn.
	MentionDirect MentionMode = iota

	// MentionOpen: the whole fleet receives it and the agent named is
	// addressed in the words. One turn each.
	MentionOpen
)

// String is the word the composer draws and the notice row reports, so the two
// cannot disagree about what the mode is called.
func (m MentionMode) String() string {
	if m == MentionOpen {
		return "open"
	}
	return "direct"
}

// roomRoute is where ↵ will send a room draft: core's answer to the text, plus
// what mention mode did to it.
//
// The two extra fields exist because core.Route cannot carry them and should
// not. `Resolved` says a name matched and says nothing about *which list* it
// matched in, and the router is a pure function over the fleet it is handed -
// the mode is this client's, set by a key, and nothing on the wire has heard
// of it.
type roomRoute struct {
	core.Route

	// mentioned is set when a leading @name matched a live member of the
	// fleet - the one route mention mode decides. False for @all, for
	// @manager and for a draft with no mention, so each of those is drawn and
	// sent exactly as it was before the mode existed.
	mentioned bool

	// mode is the reading this route was resolved under. Carried rather than
	// re-read at the point of drawing, so the line the composer shows is a
	// property of the route that was computed and not of the App as it stands
	// a moment later. A slash command resolves to MentionDirect even while the
	// App is in open mode, because route below never widens a knob.
	mode MentionMode

	// direct is the reading before open mode widened it, and it is empty unless
	// it did - which is only ever a *message* now, because route never widens a
	// slash command (see below). It is what the completion menu resolves the one
	// named agent through (addressedAgent): a menu is a knob, so it offers that
	// agent's own commands and paths even while a message to them fans out.
	//
	// Carried rather than resolved a second time, because two callers of the
	// router are two answers to one question.
	direct core.Route
}

// configureRoute is where a knob goes: the direct reading when open mode widened
// a message, and the route itself otherwise. A slash command's route is already
// direct, so this returns it unchanged there.
func (r roomRoute) configureRoute() core.Route {
	if r.direct.Targets != nil {
		return r.direct
	}
	return r.Route
}

// route is the one answer to "where does this draft go".
//
// The composer's question and ↵'s question are the same question, asked in one
// place so the two cannot disagree - which is the whole of what makes a mode
// safe. A composer promising one turn over a send that starts thirty is
// exactly the failure §7's "the composer always shows the current target and
// mode" is written against, and it is invisible to any test that drives only
// one of the two. See TestNothingRoutesARoomDraftExceptTheOneFunctionThatDoes.
func (a App) route(text string) roomRoute {
	live := a.live()
	r := core.Resolve(text, live, a.service())
	if !fleetMention(r, live) {
		return roomRoute{Route: r, mode: a.mention}
	}
	// **Open mode widens a message; it does not widen a knob.** `@john hello` in
	// open mode reaches the whole fleet and keeps the name in the text, so the
	// others can see who was addressed - a property of something being *said*. A
	// slash command is not that: `@john /model opus`, `@john /clear`, `@john
	// /effort xhigh` each configure or control one session, so they reach john
	// alone with the mention stripped - exactly as they would in his DM - and
	// widening one would retune or clear thirty off one keystroke. r.Text is the
	// body core stripped the mention off, so leadingCommand reads the command
	// rather than the @name. It resolves MentionDirect so the composer promises
	// the one turn it sends.
	if a.mention == MentionDirect || leadingCommand(r.Text) {
		return roomRoute{Route: r, mentioned: true, mode: MentionDirect}
	}
	return roomRoute{Route: openRoute(r, text, live), direct: r, mentioned: true, mode: a.mention}
}

// fleetMention reports whether a route is the one mention mode decides: a
// leading @name that matched a live member of the fleet.
//
// Asked against live rather than by testing Resolved for emptiness, because
// Resolved also carries the broadcast's own name and the manager's - and the
// manager arrives at core.Resolve as the service precisely so that "the fleet"
// is a list this side can point at. Broadcast is refused by name as well as by
// the lookup: `all` is reserved so no agent can be called it, and a predicate
// that depends on another package's reserved set to stay correct is one that
// goes wrong the day that set changes.
func fleetMention(r core.Route, live []core.Addressee) bool {
	if r.Broadcast || r.Resolved == "" {
		return false
	}
	for _, addressee := range live {
		if addressee.Name == r.Resolved {
			return true
		}
	}
	return false
}

// openRoute is the open reading: every live agent receives the draft, and the
// agent named is addressed in the words.
//
// # The text is what was typed, mention and all
//
// That is the safety property rather than a convenience. Direct mode strips
// the mention because the recipient is the route; here there are nineteen
// other recipients, and handing them `ship it` with nothing in it saying whose
// it was is a fleet where nineteen agents ship it. Sending two different texts
// - stripped to the one addressed, whole to the rest - would be a second
// message shape for one keystroke, and the room echoes one line whichever it
// is; §7 says "everyone receives it", singular.
//
// The raw draft rather than a reconstruction from Resolved and Text, for
// passThrough's reason: what is not routed reaches the CLI exactly as typed,
// and rebuilding `@name ` in front of the rest would normalise whitespace
// somebody may have meant.
//
// # It is not marked Broadcast
//
// Broadcast means @all, which the composer draws with its own line and which
// core owns the meaning of. This route is a mention that reached the fleet -
// the target line says so in those words, and marking it a broadcast would
// make `@john ship it` read as `→ @all`, naming the wrong agent on the one
// route whose whole risk is who hears it.
func openRoute(r core.Route, text string, live []core.Addressee) core.Route {
	ids := make([]string, 0, len(live))
	for _, addressee := range live {
		ids = append(ids, addressee.ID)
	}
	return core.Route{Targets: ids, Text: text, Resolved: r.Resolved}
}

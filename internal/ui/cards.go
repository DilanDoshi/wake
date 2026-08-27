package ui

// Answerable cards: an agent that needs something from you, pinned in that
// agent's own conversation until it is settled. The room draws none - see
// App.cardOf for why the group chat is the wrong surface for one.
//
// # One mechanism, three shapes
//
// Everything here arrives the same way: a blocking control request decoded to
// core.KindPermissionRequest, carrying a RequestID that an answer names. A
// question and a plan are not a second channel - the spike recorded both and
// they use this one, each preceded by an ordinary tool call naming the same
// id.
//
// What differs is what an allow *means*, and that difference is why this type
// exists rather than a switch at the call site:
//
//   - a permission allow means "run it exactly as it asked", and carries no
//     updated input, because absent is the only shape any recording covers;
//   - a plan allow *is* the approval - the CLI answers a bare one with "User
//     has approved your plan";
//   - a question allow is not an answer at all. The answer travels as its own
//     frame kind, and a bare allow on a question runs the tool while telling
//     the model nobody replied, on a turn that still ends successfully with
//     nothing anywhere reporting the loss.
//
// So the card mints its own answer frame, and the shape it mints is data
// rather than a caller's guess. A caller that had to remember which of the
// three it was holding would get it wrong on the approve path, which is the
// one nobody thinks to guard: the failure is invisible on every field Claude's
// wire has.
//
// # Pinned until settled, and never abandoned
//
// The ask blocks the agent indefinitely - 342 seconds of zero bytes in one
// recording, with no timeout, no auto-resolve and no heartbeat. And closing
// stdin on an unanswered ask ends the turn as a permission rule denial on a
// *success* turn, which is byte-for-byte what a deliberate deny produces. So
// Wake cannot tell afterwards whether the operator refused or whether Wake
// dropped it, and the only available defence is to not get into that state:
// the card stays up until something settles it, and it is reconciled against
// every fleet report, in both directions.

import (
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// CardShape is what the operator is being asked for.
type CardShape int

const (
	// ShapePermission is may-I-do-this: a yes or a no about an action.
	ShapePermission CardShape = iota

	// ShapeQuestion is a choice among options the agent supplied, and the one
	// shape whose allow has to carry something.
	ShapeQuestion

	// ShapePlan is a plan waiting for approval - a yes or a no like a
	// permission, but about a document rather than an action.
	ShapePlan
)

// noChoice marks a question nothing has been picked for yet, so an unanswered
// question is left out of the answer rather than sent as option zero.
const noChoice = -1

// Card is one outstanding ask.
type Card struct {
	AgentID   string
	RequestID string

	// Tool and Arg are what is being asked about, already resolved: the tool's
	// name and its one meaningful argument. Both are empty on an interactive
	// ask - neither interactive tool has a mapped argument, and neither name
	// is a word this package is allowed to spell.
	Tool string
	Arg  string

	// Ask is which of the three this is, as the airlock resolved it. It is
	// read rather than re-derived from the payload: the wire's own answer to
	// "what does this ask want" is the one the encoder beneath also acted on,
	// and a second opinion here is how a renderer starts disagreeing with the
	// thing that writes the bytes.
	Ask core.AskKind

	// Detail is what the ask is putting to the operator - the questions, or
	// the plan - and nil for a plain permission and for an interactive ask
	// whose payload this build cannot read.
	Detail *core.AskDetail

	// Choice is the option index chosen for each question, noChoice for one
	// nobody has answered. Its length tracks Detail.Questions.
	//
	// One index per question, never a set. Multi-select is false on every
	// recorded question and the comma-separated encoding for it is a read of
	// the CLI binary with no recording behind it, which §9 of the question
	// findings forbids building against. A multi-select question therefore
	// takes one answer here - a real label the tool accepts - rather than a
	// guess about how two are joined.
	Choice []int

	// Typed is the operator's own words for each question, empty for one
	// answered by picking an option. Its length tracks Detail.Questions.
	//
	// A parallel slice to Choice rather than a sentinel in it, because the two
	// carry different things and only one of them is an index. answers()
	// prefers a non-empty entry here, which is the whole of what free text
	// costs on the wire: the schema is already question text to a *label*, and
	// a typed answer is a label the options did not contain.
	Typed []string

	// Cursor is which step the operator is on - a question, or the review past
	// the last one - and Option which choice within it. Both are presentation
	// state and both live on the card rather than in the room, so a card that
	// scrolls off and back keeps where you were.
	Cursor int
	Option int

	// asked is the ask's own input, carried back unopened.
	//
	// Unexported, and that is the airlock rule in a form this package cannot
	// break by accident: an answer has to travel with the input it answers
	// (core.EncodeAnswer folds the choices into it), and nothing above
	// internal/core may index it. A caller cannot reach it, cannot build a
	// card that carries the wrong one, and cannot read a key out of it.
	asked map[string]any
}

// Shape is which of the three this is.
//
// Keyed on the resolved kind, never on the tool's name. A tool-name allowlist
// is the wrong shape twice over: any tool whose permission check asks for
// interaction reasons can be interactive, so a list would silently draw the
// next one as a bare yes/no - and both recorded names are words the airlock
// never spells, so this package could not write the list even if it wanted to.
//
// An unmodelled kind draws as a permission: a yes/no on a named tool degrades
// to less certainty, never to an empty card.
func (c Card) Shape() CardShape {
	switch c.Ask {
	case core.AskChoice:
		return ShapeQuestion
	case core.AskApproval:
		return ShapePlan
	case core.AskPermission:
		return ShapePermission
	default:
		return ShapePermission
	}
}

// Allow is the frame that settles this card affirmatively, and it is a
// different frame kind for a question than for the other two.
//
// That difference is the whole point of the type. An allow carrying an
// optional answers field would mean an empty one had to mean "allow it bare",
// which is the defect itself; a question is therefore settled by a frame that
// cannot be sent without choices, and one that arrives without them is refused
// with nothing written rather than delivered as a silent non-answer.
func (c Card) Allow() rpc.Frame {
	f := rpc.Frame{Kind: rpc.FrameAllow, SessionID: c.AgentID, RequestID: c.RequestID}
	if c.Shape() != ShapeQuestion {
		return f
	}
	f.Kind = rpc.FrameAnswer
	f.UpdatedInput = c.asked
	f.Answers = c.answers()
	return f
}

// Deny is the frame that refuses it. reason reaches the model verbatim as the
// tool result, so it is the one channel for saying what to do instead of
// retrying the identical call.
func (c Card) Deny(reason string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameDeny, SessionID: c.AgentID, RequestID: c.RequestID, Reason: reason}
}

// Answered reports whether every question this ask put has a choice.
//
// It exists so a caller can decline to offer the key at all, which is the
// cheaper half of the defence: an answer short of one question is refused
// beneath the daemon, and a refusal the operator has to read is worse than a
// key that was not offered. False is never a reason to send a bare allow.
func (c Card) Answered() bool {
	if c.Shape() != ShapeQuestion {
		return true
	}
	if c.Detail == nil || len(c.Detail.Questions) == 0 {
		return false
	}
	// Through firstUnanswered rather than beside it. They are the same
	// question - is every question answered, and which is the first that is
	// not - and holding them apart is how one learned to count a typed answer
	// while the other did not, which sent Submit back to a question the
	// operator had already answered with nothing on screen saying why.
	_, missing := c.firstUnanswered()
	return !missing
}

// Pick chooses an option for a question. Out-of-range indices are ignored
// rather than clamped: a clamp would answer a different question from the one
// the caller named.
func (c Card) Pick(question, option int) Card {
	if !c.inRange(question, option) {
		return c
	}
	choice := append([]int(nil), c.Choice...)
	choice[question] = option
	c.Choice = choice
	return c
}

// Move walks the cursor: questions forward or back, and options within the
// question the cursor is on. Both are clamped, because a cursor is a place to
// look rather than an answer.
func (c Card) Move(questions, options int) Card {
	if c.Detail == nil || len(c.Detail.Questions) == 0 {
		return c
	}
	cursor := clamp(c.Cursor+questions, 0, c.lastStep())
	if cursor != c.Cursor {
		// A different question is a different list of options, so the cursor
		// starts at its top. Carrying the last question's index forward opened
		// a fresh set of labels with the cursor already part-way down it,
		// which reads as the options having moved rather than the question.
		c.Cursor, c.Option = cursor, 0
		return c
	}
	c.Option = clamp(c.Option+options, 0, max(c.optionsAt(cursor)-1, 0))
	return c
}

// question is the one the cursor is on, and whether there is one. The cursor
// is bounded here as well as in Move because a card can be re-rendered after
// its payload changed - a reconciled ask carries none at all.
func (c Card) question() (core.Question, bool) {
	if c.Detail == nil || len(c.Detail.Questions) == 0 {
		return core.Question{}, false
	}
	return c.Detail.Questions[clamp(c.Cursor, 0, len(c.Detail.Questions)-1)], true
}

// answers is question text to chosen option label, which is the schema: not an
// index, not an object, and not the question's position.
//
// A question with nothing chosen is left out rather than sent blank. The
// encoder refuses the whole answer either way - which is the designed outcome,
// since a blank choice and a missing one are the same lost answer - but an
// absent key and an empty string are different claims and only one of them is
// true.
func (c Card) answers() map[string]string {
	if c.Detail == nil {
		return nil
	}
	out := make(map[string]string, len(c.Detail.Questions))
	for i, q := range c.Detail.Questions {
		// The operator's own words first. A typed answer is a label the
		// options did not contain, which is the whole of what free text costs
		// on this wire - the schema is already question text to a label.
		if typed := c.typed(i); typed != "" {
			out[q.Text] = typed
			continue
		}
		o := c.chosen(i)
		if o < 0 || o >= len(q.Options) {
			continue
		}
		out[q.Text] = q.Options[o].Label
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// typed is the operator's own answer to question i, or "".
func (c Card) typed(i int) string {
	if i < 0 || i >= len(c.Typed) {
		return ""
	}
	return strings.TrimSpace(c.Typed[i])
}

// chosen is the option picked for question i, or noChoice.
func (c Card) chosen(i int) int {
	if i < 0 || i >= len(c.Choice) {
		return noChoice
	}
	return c.Choice[i]
}

// picked reports whether option o of question i is the chosen one, for the
// marker in the view. It goes through chosen so the two cannot disagree.
func (c Card) picked(i, o int) bool { return c.chosen(i) == o }

// inRange reports whether this question and option both exist.
func (c Card) inRange(question, option int) bool {
	if c.Detail == nil || question < 0 || question >= len(c.Detail.Questions) || question >= len(c.Choice) {
		return false
	}
	return option >= 0 && option < len(c.Detail.Questions[question].Options)
}

// Cards is every ask this client believes is outstanding, oldest first.
//
// A list rather than one card, and that is not speculative. The corpus shows
// two subagents running concurrently, so a parent ask plus a subagent ask - or
// two parallel tool calls each gated - is plainly reachable. The daemon holds
// the whole set per agent and reports it as rpc.SessionStatus.RequestIDs, so a
// live client and a reattaching one converge on the same list: every ask a
// blocked session owes, not just the last one seen.
//
// Oldest first, because the agent that has been stopped longest is the one
// costing the most - and because a list that reorders under the operator's
// hand is how somebody allows the wrong thing.
//
// armed is the settle that has been pressed once and not yet confirmed. It
// lives here rather than on the Card because it is a fact about the keyboard
// rather than about the ask, and because every method that rebuilds this list
// drops it - which is the safe default, and the one exception is stated where
// it is made.
type Cards struct {
	open  []Card
	armed armedSettle

	// writing is the composer's answer mode: the ask a typed answer or a
	// refusal's reason is being written for. Here rather than on the Card for
	// armed's reason - it is a fact about the keyboard, not about the ask.
	writing writingAnswer

	// plans memoizes each plan card's rendered body, keyed per card identity.
	// A plan is markdown through glamour - internal/render's one process-global
	// mutex - and Bubble Tea redraws on a blink and on every mouse motion during
	// a drag, so without this it re-renders on frames where nothing about it
	// moved, which the first non-negotiable forbids. Keyed per identity, not one
	// App-level slot, because two panes can draw two different plan cards in one
	// frame and a shared slot would thrash and could serve one the other's body.
	// Pruned to the open set wherever that set shrinks - Reconcile and Settle -
	// so a retired card's body does not outlive it regardless of report cadence.
	// Mirrors DM.bar/barFrom, one level up because there are N cards - and
	// reference semantics are what let View, which returns only a string, still
	// persist a render across frames. See cachedPlan. Keyed on [agentID,
	// requestID], the pair the rest of this file matches an ask on.
	plans map[[2]string]planMemo
}

// planKey is everything the plan card's body render depends on. A value type so
// "has anything changed" is one comparison, mirroring barKey. agentID and
// requestID are in it as well as in the map key: a fleet report can swap the
// drawn card with no keystroke, and a key that omitted them could serve the
// stale body of the card that was there before.
type planKey struct {
	agentID   string
	requestID string
	plan      string
	width     int
}

// planMemo is one plan card's rendered body and the key it was rendered from.
type planMemo struct {
	key  planKey
	body string
}

// armedSettle names one ask and one key.
//
// Both halves, and neither is optional. The key, because arming a deny may not
// be confirmed by an allow. The ask, because the top card changes on its own -
// a fleet report that no longer names the ask on top takes it down and brings
// the one behind it forward, with no keystroke anywhere - and a confirm that
// read only the key would then settle whichever agent came forward. That is the
// accident the confirm exists to prevent, arriving through the fix for it.
//
// The zero value is not armed: Cards never holds a card whose RequestID is
// empty, so an empty requestID here can match nothing.
type armedSettle struct {
	agentID   string
	requestID string
	key       rune
}

// arm remembers that this key has been pressed once against this ask.
func (cs Cards) arm(c Card, key rune) Cards {
	cs.armed = armedSettle{agentID: c.AgentID, requestID: c.RequestID, key: key}
	return cs
}

// disarm takes back a settle that was pressed once. Every keystroke that is not
// the confirm goes through here, including the ones the card has no meaning for
// at all: an arm that survived a chord would be confirmed by the next `d` typed
// at an empty composer, minutes later, against a card nobody was looking at.
func (cs Cards) disarm() Cards {
	cs.armed = armedSettle{}
	return cs
}

// armedKey is the key this card is armed for, and whether it is armed at all.
// It is the one place the arm is matched against a card, so the view and the
// keypress cannot disagree about which card is armed.
func (cs Cards) armedKey(c Card) (rune, bool) {
	if cs.armed.requestID == "" {
		return 0, false
	}
	if cs.armed.agentID != c.AgentID || cs.armed.requestID != c.RequestID {
		return 0, false
	}
	return cs.armed.key, true
}

// armedFor reports whether this exact ask is armed for this exact key.
func (cs Cards) armedFor(c Card, key rune) bool {
	k, ok := cs.armedKey(c)
	return ok && k == key
}

// Add folds one event: it promotes an ask and retires a withdrawn one.
//
// Both, rather than only the promotion, because both arrive on this seam and
// the second is not optional. A well-formed allow for a withdrawn request
// produces no frame, no error and no tool run, so a card left up after the
// withdrawal invites an answer into the void - and the operator has no way to
// tell that is what happened.
//
// An ask this list already holds is replaced in place rather than appended: a
// reattach can deliver the same ask twice, once as an event and once through a
// report, and a card that jumped to the end of the queue on a duplicate would
// move under the operator's hand.
func (cs Cards) Add(agentID string, ev core.Event) Cards {
	if ev.RequestID == "" {
		return cs
	}
	switch ev.Kind {
	case core.KindRequestWithdrawn:
		return cs.Settle(agentID, ev.RequestID)
	case core.KindPermissionRequest:
		return cs.with(newCard(agentID, ev))
	default:
		return cs
	}
}

// newCard is one ask, promoted.
func newCard(agentID string, ev core.Event) Card {
	card := Card{AgentID: agentID, RequestID: ev.RequestID, Ask: ev.Ask}
	if ev.Tool == nil {
		return card
	}
	card.Tool, card.Arg, card.Detail, card.asked = ev.Tool.Name, ev.Tool.Display, ev.Tool.Ask, ev.Tool.Input
	if card.Detail == nil {
		return card
	}
	card.Choice = make([]int, len(card.Detail.Questions))
	for i := range card.Choice {
		card.Choice[i] = noChoice
	}
	card.Typed = make([]string, len(card.Detail.Questions))
	return card
}

// Settle takes one ask down: answered, denied, or withdrawn.
//
// Keyed on the pair, for Reconcile's reason: an ask id is unique per agent and
// nothing promises it is unique across them, so matching on the id alone takes
// down a second agent's card as well. That self-heals on the next report, and
// what it costs meanwhile is the card on a blocked agent nobody is looking at.
//
// The arm goes only if it named *this* ask. It survives an unrelated settle for
// the reason Reconcile carries it, and it must not survive this one: Reconcile
// can put an ask back before the daemon has acted on the frame just written,
// and a stale arm on the returning card would settle it twice.
func (cs Cards) Settle(agentID, requestID string) Cards {
	out := make([]Card, 0, len(cs.open))
	for _, c := range cs.open {
		if c.AgentID != agentID || c.RequestID != requestID {
			out = append(out, c)
		}
	}
	settled := Cards{open: out, armed: cs.armed, writing: cs.writing, plans: prunePlans(cs.plans, out)}
	if cs.writing.agentID == agentID && cs.writing.requestID == requestID {
		// The mode goes with the ask, for the arm's own reason one line down:
		// Reconcile can put an ask back before the daemon has acted on the
		// frame just written, and answer mode left standing would let the next
		// ↵ settle the returning card a second time.
		settled = settled.stopWriting()
	}
	if cs.armed.agentID == agentID && cs.armed.requestID == requestID {
		return settled.disarm()
	}
	return settled
}

// Reconcile squares this list with a fleet report.
//
// Both directions matter and neither is optional. An ask the report no longer
// names is dead - answering it produces no frame, no error and no tool run -
// so leaving the card up invites the operator to answer into the void. And an
// ask the report names that this client never saw is one that arrived while it
// was detached: the report carries every outstanding request id for exactly
// that case, and a client that only ever learned asks from events would leave
// that agent stopped forever.
//
// The report names *every* ask a session is blocked on, not one, because a
// session can be blocked on several at once - and a reconcile keyed on a single
// id took down the cards that id was not, stranding a live ask with the agent
// stopped behind it. See rpc.SessionStatus.RequestIDs.
//
// A report is always the whole fleet, so an agent missing from one has ended.
// Matching on the pair rather than on the request id alone: an id is unique
// per agent and nothing promises it is unique across them.
//
// # The one method that carries an armed settle forward
//
// Every other rebuild here drops the arm, because every other rebuild is
// something the operator did. A report is not: it arrives whenever any of
// thirty agents changes state, so an arm dropped by one would be a confirm the
// operator could not reliably reach - press, a report lands, press again, and
// the second press arms instead of confirming. Carrying it costs nothing,
// because armedKey matches on the ask: an arm whose card this report took down
// can never match again, so the arm is inert rather than loose.
func (cs Cards) Reconcile(st *rpc.Status) Cards {
	if st == nil {
		return cs
	}
	// Every (agent, ask) the report still names. A session can be blocked on
	// several at once, so all of its ids are live - keying on one would take
	// down every card the report could not fit, which is the stranded-ask bug
	// this reconcile is answering.
	live := make(map[[2]string]rpc.SessionStatus, len(st.Sessions))
	for _, s := range st.Sessions {
		for _, rid := range s.RequestIDs {
			live[[2]string{s.ID, rid}] = s
		}
	}

	out := make([]Card, 0, len(cs.open)+len(live))
	for _, c := range cs.open {
		key := [2]string{c.AgentID, c.RequestID}
		if _, ok := live[key]; ok {
			out = append(out, c)
			delete(live, key)
		}
	}
	for _, s := range st.Sessions {
		for _, rid := range s.RequestIDs {
			key := [2]string{s.ID, rid}
			if _, ok := live[key]; !ok {
				continue
			}
			delete(live, key)
			// Known only from a report, so the payload is what the report
			// carries: the tool and its argument, with nothing resolved. It
			// draws as a permission, which is a yes/no on a named tool rather
			// than nothing at all - and if the ask was a question, the event
			// that carries its questions is gone, so this is the honest extent
			// of what is known. A second concurrent ask borrows the agent's
			// current tool label, since the report names only one.
			out = append(out, Card{AgentID: s.ID, RequestID: rid, Tool: s.Tool, Arg: s.ToolArg})
		}
	}
	return Cards{open: out, armed: cs.armed, writing: cs.writing, plans: prunePlans(cs.plans, out)}
}

// prunePlans drops memo entries for cards no longer open, so the cache does not
// grow for the life of the process. A nil source stays nil; the map is allocated
// lazily in with, when the first plan card is added.
func prunePlans(plans map[[2]string]planMemo, open []Card) map[[2]string]planMemo {
	if plans == nil {
		return nil
	}
	kept := make(map[[2]string]planMemo, len(plans))
	for _, c := range open {
		id := [2]string{c.AgentID, c.RequestID}
		if m, ok := plans[id]; ok {
			kept[id] = m
		}
	}
	return kept
}

// cachedPlan is a plan card's rendered body, served from the memo when nothing
// it depends on has moved and re-rendered through the planBody seam otherwise -
// one equality on planKey, mirroring DM.withBar.
//
// The write is in place, and that is the whole mechanism: View has a value
// receiver and cannot hand a new Cards back, but cs.plans is a reference shared
// across the App's value copies, so an entry written here persists to the stored
// Cards. It is safe because View runs on the one Update/render goroutine, and
// the entry is keyed by its content - a write from a discarded copy is correct.
// A nil map (a zero-value Cards a test built directly) renders every time rather
// than panicking on the write; every plan card in the live app is added through
// with, which allocates the map.
func (cs Cards) cachedPlan(card Card, width int) string {
	key := planKey{agentID: card.AgentID, requestID: card.RequestID, plan: card.Detail.Plan, width: width}
	id := [2]string{card.AgentID, card.RequestID}
	if m, ok := cs.plans[id]; ok && m.key == key {
		return m.body
	}
	body := planBody(card.Detail.Plan, width)
	if cs.plans != nil {
		cs.plans[id] = planMemo{key: key, body: body}
	}
	return body
}

// Top is the ask to answer first, and whether there is one.
func (cs Cards) Top() (Card, bool) {
	if len(cs.open) == 0 {
		return Card{}, false
	}
	return cs.open[0], true
}

// For is the oldest ask this one agent is blocked on, and it is the only way a
// card reaches a surface.
//
// A conversation draws its own agent's ask and nobody else's. The room draws
// none - see App.cardOf - so an ask is on screen exactly when the conversation
// that raised it is open, which is what makes several agents blocked at once
// answerable side by side instead of one at a time behind a count.
func (cs Cards) For(agentID string) (Card, bool) {
	return cs.first(func(c Card) bool { return c.AgentID == agentID })
}

// first is the oldest card matching a predicate, in queue order.
func (cs Cards) first(want func(Card) bool) (Card, bool) {
	for _, c := range cs.open {
		if want(c) {
			return c, true
		}
	}
	return Card{}, false
}

func (cs Cards) Len() int { return len(cs.open) }

// With writes a card back - a choice made, a cursor moved - keeping its place
// in the queue. A card whose ask this list no longer holds is dropped rather
// than re-added: it was settled or withdrawn while the operator was choosing,
// and re-adding it would offer an answer that goes nowhere.
func (cs Cards) With(card Card) Cards {
	if _, ok := cs.byRequest(card.AgentID, card.RequestID); !ok {
		return cs
	}
	return cs.with(card)
}

func (cs Cards) byRequest(agentID, requestID string) (Card, bool) {
	for _, c := range cs.open {
		if c.AgentID == agentID && c.RequestID == requestID {
			return c, true
		}
	}
	return Card{}, false
}

// with replaces a card in place, or appends it if this list has never seen it.
//
// It carries the arm, for Reconcile's reason and at the same seam: another
// agent blocking is not a keystroke, and at thirty agents it is most of what
// arrives - so an arm dropped by one would be a confirm the operator cannot
// reach. Nothing here can make a stale arm live, because armedKey matches on
// the ask this list is being handed.
func (cs Cards) with(card Card) Cards {
	// Allocated here, the one path every plan card is added through, so cachedPlan
	// always has a live map to write its render into - a nil map cannot be
	// allocated during View, which returns only a string. Carried, not rebuilt,
	// so a card written back (a cursor moved) does not drop the cache.
	plans := cs.plans
	if plans == nil {
		plans = make(map[[2]string]planMemo)
	}
	out := append(make([]Card, 0, len(cs.open)+1), cs.open...)
	for i, c := range out {
		if c.AgentID == card.AgentID && c.RequestID == card.RequestID {
			out[i] = card
			return Cards{open: out, armed: cs.armed, writing: cs.writing, plans: plans}
		}
	}
	return Cards{open: append(out, card), armed: cs.armed, writing: cs.writing, plans: plans}
}

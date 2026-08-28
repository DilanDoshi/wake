package ui

// What an answerable card looks like. cards.go owns which asks are open and
// how they are settled; this file owns the rows.
//
// Only the top card is drawn. A room with three stacked cards is a queue, and
// a queue is what the roster is for - the count says how many are behind it,
// and the oldest is on top because the agent that has been stopped longest is
// the one costing the most.
//
// # The keys, and where they are bound
//
// cardkeys.go binds every affordance drawn here, and this file draws only the
// ones that would currently do something - which is why a question offers
// [d]eny alone until an option has been chosen. They are runes rather than
// chords because they exist only while a card is up: the legend under the
// composer names what this build always does, and an affordance that comes and
// goes with a blocked agent belongs on the thing that came and went. That is
// also why the legend guard cannot see them and does not need to - the card is
// its own legend, rendered from the same Card the keys act on.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
)

const (
	// cardFrameWidth is what the box costs a row: two border columns and the
	// one column of padding either side that BoxStyle carries.
	cardFrameWidth = 4

	// cardLead marks the one thing in the room that is not conversation: an
	// agent that has stopped and is waiting on you. Same glyph the DM heads a
	// permission ask with, so the two surfaces agree.
	cardLead = "⚠ "

	// cardChooseFmt and cardChooseOne are how an option is picked - the digits
	// that reach this question's options. They are what makes the answer key
	// reachable at all, so they are drawn whether or not it is.
	cardChooseFmt = "[1-%d] choose"
	cardChooseOne = "[1] choose"

	// cardKeySep separates two affordances, matching the spacing inside each.
	cardKeySep = "  "

	// One label per key and shape, because the word has to match what the key
	// does. "Allow" is a tool call, "answer" is a choice among options, and
	// "approve" is a plan - and a plan approved by a key labelled "allow" reads
	// as though something is about to be run.
	//
	// The bracketed rune is a literal rather than something built out of the
	// bound key, and TestEveryRuneTheCardBracketsIsOneCardKeysBinds is what
	// makes that safe: it requires a bijection between the runes drawn here and
	// the ones cardkeys.go binds, so a card advertising a key that does nothing
	// is a build failure. That is the composer legend's own rule, on the one
	// surface the legend guard cannot see - and the same shape, a slice of
	// labels held to a switch, rather than a second mechanism.
	cardAllowLabel   = "[a]llow"
	cardAnswerLabel  = "[a]nswer"
	cardApproveLabel = "[a]pprove"
	cardDenyLabel    = "[d]eny"

	// The brackets the labels above put round a key, named because the guard
	// over those labels reads them back out.
	cardKeyOpen  = "["
	cardKeyClose = "]"

	// cardConfirmGlyph is the key that completes a settle, and it is not the
	// rune that armed it. The rune is what the accident produces - a character
	// that vanished is retyped, and `add` is three card keys in a row - so a
	// same-rune confirm is defeated by the exact thing the arm exists to catch.
	//
	// ↵ has nothing else to do here: an arm only exists while the composer is
	// empty, and submit on an empty draft does nothing. The composer's legend
	// still says `↵ send`, which is what this build always does; the card
	// advertises the override, which is what a card is for.
	cardConfirmGlyph = "↵"

	// The armed line: what a confirm would do, that it cannot be taken back,
	// and the way out.
	//
	// All three, and the middle one is the one this shipped without.
	// "press ↵ to deny · any other key cancels" is an are-you-sure word for
	// word, and the fact the whole design rests on - that after the confirm
	// nothing retracts it - was written only where developers read. The
	// operator is who needs it.
	cardArmedFmt    = "%s %s"
	cardArmedFinal  = "cannot be undone"
	cardArmedCancel = "any other key cancels"

	// cardConfirmHint is what the *unarmed* line adds, so nothing has to
	// vanish before the operator learns the key takes a second one.
	//
	// It reads as a forward-looking claim about [a] and [d] and not about ↵
	// itself, which matters on a question: ↵ chooses there while nothing is
	// armed, and confirms once something is. Both are true at once because
	// pressing [a] or [d] is what moves between them, and the armed line
	// replaces this whole string rather than adding to it.
	cardConfirmHint = "then " + cardConfirmGlyph

	// cardMoveKeys walks a question and answers it without a digit - the pair
	// claude's own question screen uses. Drawn only on a question with options
	// to walk, because a yes/no has nothing for a cursor to move over and
	// cardKey hands both keys back on every other shape.
	cardMoveKeys = "↑↓ move" + cardKeySep + cardConfirmGlyph + " choose"

	// cardInterruptHint is the key that destroys this ask, drawn beside the
	// keys that answer it - the re-scoped undo item's own shape: ⎋ ends an
	// outstanding ask and a billed turn, a two-press ⎋ was rejected for
	// urgency, so the card makes the destruction visible instead. Spelled from
	// the legend's constants so the two surfaces cannot drift, and unbracketed
	// so the bijection guard reads no rune out of it.
	cardInterruptHint = escGlyph + " " + escInterruptLabel

	// cardDismissHint is ⎋'s label on a question, where it is not an interrupt.
	// A pending permission is withdrawn by the interrupt itself, but a question
	// is not (see send.go's interrupt), so ⎋ settles it with a deny instead -
	// and the label has to say the thing the key does, which is this project's
	// "the legend never lies" rule reaching a surface the legend guard cannot
	// see. "dismiss" rather than "interrupt": the ask is refused, not the turn.
	cardDismissHint = escGlyph + " dismiss"

	// cardKeysPaused replaces the key line while the composer holds a draft,
	// when cardKey reads no rune at all - a line still advertising [a]llow
	// would be the legend rule broken on the one surface the legend guard
	// cannot see.
	cardKeysPaused = "keys return when the draft is sent or cleared"

	// cardDot separates the clauses of a key line. The composer spells a route
	// and its price the same way.
	cardDot = "  ·  "

	// The headlines. Each is what the agent wants, in the fewest words that
	// still say which of the three it is.
	cardWantsFmt    = "wants %s"
	cardWantsYou    = "needs you"
	cardHasQuestion = "has a question"
	cardHasPlan     = "has a plan for you"


	// The option markers. cardChosen is what the answer will carry;
	// cardCursor is where the operator is looking. They are different things
	// and a card that spelled them the same way would leave somebody pressing
	// a key on the row they had merely scrolled to.
	cardChosen   = "▸ "
	cardUnchosen = "  "
	cardCursor   = "› "

	// cardPlanLines is how much of a plan a card shows. A plan is markdown a
	// model wrote to be read and the card is pinned above a composer, so the
	// rest of it is read in the transcript below. More than a reply's two
	// lines, because the reader is being asked to approve this rather than to
	// decide whether to look at it.
	cardPlanLines = 8
)

// The pair of affordances each shape offers, built from the labels above so
// there is one spelling of each key.
const (
	cardAllowKeys  = cardAllowLabel + cardKeySep + cardDenyLabel
	cardAnswerKeys = cardAnswerLabel + cardKeySep + cardDenyLabel
	cardPlanKeys   = cardApproveLabel + cardKeySep + cardDenyLabel
)

// planBody is the seam a plan card's markdown is rendered through, so the memo
// that keeps it off the draw loop (Cards.cachedPlan) and the test that counts
// it share one entry point. Reach a plan body through this, never render.Markdown
// directly: glamour runs behind internal/render's one process-global mutex, and a
// direct call is invisible to the memo that keeps it off every blink and every
// mouse motion. Mirrors drawStatusBar. cardPlanLines is a const and stays out of
// the memo's key for that reason.
var planBody = func(plan string, width int) string {
	return collapsed(render.Markdown(plan, width), words(plan), width, cardPlanLines)
}

// View draws the top card, or nothing when there is none.
//
// It never returns a line wider than width, for the reason roomBlock does not:
// the room is one column of a three-region layout and lipgloss sizes a joined
// row on its widest line, so one over-wide line shoves both sidebars out of
// place.
//
// The card it draws is chosen by the caller rather than taken from the top of
// the list: a pane draws its own agent's ask, and there is no count under it
// because there is nothing behind it - one agent is blocked on one ask at a
// time on this surface, and the fleet's others are on their own panes.
func (cs Cards) View(card Card, width int, by Agent, typing bool) string {
	w := max(width, minBlockWidth)
	// The body is laid out for the room *inside* the frame, which is narrower
	// than the pane by the border and its padding. Sized here rather than
	// clipped afterwards, for questionBody's own reason: a row built for a
	// column it is not in gets cut back to fit rather than laid out to fit.
	inner := max(w-cardFrameWidth, 1)

	parts := []string{cs.body(card, inner)}
	return titledBox(joinBlock(parts...), w, cardBoxStyle,
		cardLead+speaker(by)+markerSep+card.headline(),
		cs.keyLine(card, inner, typing),
		warnStyle, HintStyle)
}

// body is the card's body, with a plan card's markdown served from the memo so
// it goes back through glamour on change rather than on every frame. Every other
// shape is cheap and drawn straight. The plan check mirrors Card.body's own,
// which stays the seam's direct, uncached path for a caller that is not a Cards.
func (cs Cards) body(card Card, width int) string {
	if card.Shape() == ShapePlan && card.Detail != nil && strings.TrimSpace(card.Detail.Plan) != "" {
		return cs.cachedPlan(card, width)
	}
	return card.body(width)
}

// keyLine is the card's own legend: the affordances for this shape, or - once a
// settle has been pressed once - what a second press would do and what takes it
// back.
//
// The card is where an affordance that comes and goes with a blocked agent
// belongs, so an affordance that *changed* has to say so here too. A card still
// advertising "[d]eny" after a press that armed one would be the legend rule
// broken on the surface the legend guard cannot see.
//
// The width is what the line has to fit in, and it is passed rather than left
// to the truncation above: this line is the card's whole legend, it is the last
// row so it is the first thing cut, and a key line cut mid-word advertises an
// affordance the operator cannot read. What gets dropped is decided by the
// shape that has something optional to drop.
func (cs Cards) keyLine(top Card, width int, typing bool) string {
	// With a draft in the composer no card key is read at all - cardKey's
	// first gate - so the keys are not advertised, and the line says what
	// brings them back.
	// Answer mode: the draft *is* the answer, so the line says what ↵ will do
	// with it and how to get out. Checked before the paused line, which is
	// about a draft the card has no claim on.
	if q, writing := cs.writingFor(top); writing {
		if q == denyReasonStep {
			return denyModeKeys
		}
		return answerModeKeys
	}
	if typing {
		return cardKeysPaused
	}
	key, ok := cs.armedKey(top)
	if !ok {
		// `then ↵` belongs to a shape that still arms. A question has none -
		// ↵ and the digits choose outright, its settle is the review step, and
		// its refusal opens a box that announces itself - so the hint was
		// promising a second press that would do nothing.
		hint := ""
		if top.arms() {
			hint = cardDot + cardConfirmHint
		}
		line := top.keys(max(width-ansi.StringWidth(hint), 0)) + hint
		// ⎋ is appended only after the answer keys have taken their room, so it
		// is the first clause dropped: the legend carries an ⎋ label on every
		// pane, where the digits and the refusal exist nowhere else. Its word is
		// the shape's: a question is dismissed with a deny, everything else has
		// its turn interrupted - see cardDismissHint.
		escHint := cardInterruptHint
		if top.Shape() == ShapeQuestion {
			escHint = cardDismissHint
		}
		if esc := cardDot + escHint; ansi.StringWidth(line+esc) <= width {
			return line + esc
		}
		return line
	}
	return fmt.Sprintf(cardArmedFmt, cardConfirmGlyph, unbracket(top.label(key))) +
		cardDot + cardArmedFinal + cardDot + cardArmedCancel
}

// unbracket is a key label as a plain word: `[d]eny` reads `deny`.
//
// The armed line names the verb without its bracket because the bracket is a
// claim about which key to press, and while a settle is armed that key is ↵.
// Computed from the label rather than spelled beside it, so the two cannot
// drift - and so this package still never writes Claude's wire words as
// literals, which is what keeps the airlock's allowlist where it is.
func unbracket(label string) string {
	return strings.NewReplacer(cardKeyOpen, "", cardKeyClose, "").Replace(label)
}

// headline is what this agent wants, in one line.
func (c Card) headline() string {
	switch c.Shape() {
	case ShapeQuestion:
		return cardHasQuestion
	case ShapePlan:
		return cardHasPlan
	case ShapePermission:
	}
	if c.Tool == "" {
		// Reachable: an ask learned from a fleet report carries whatever the
		// report had, and a report assembled before the tool call names none.
		return cardWantsYou
	}
	return fmt.Sprintf(cardWantsFmt, c.Tool)
}

// keys are the affordances for this shape, and only the ones that would
// currently do something. cardkeys.go binds every one of them.
//
// room is what the line has to fit in. Only a question has anything optional to
// drop; the other two shapes are two keys and always fit.
func (c Card) keys(room int) string {
	switch c.Shape() {
	case ShapeQuestion:
		return c.questionKeys(room)
	case ShapePlan:
		return cardPlanKeys
	case ShapePermission:
	}
	return cardAllowKeys
}

// arms reports whether this shape still settles by a rune and then ↵, which is
// what the key line's `then ↵` is about. Only a question does not: it is
// settled on its review step. See cardsteps.go.
func (c Card) arms() bool { return c.Shape() != ShapeQuestion }

// label is how one key is drawn on this shape.
//
// It exists because an armed settle names one key where the unarmed line names
// the pair, and the two accounts must not disagree - so both are the same four
// labels. The allow key is the one that moves: it is a tool call, a choice or a
// plan, and a plan confirmed by a line reading "[a]llow" would say something is
// about to be run at the moment the operator is deciding whether it should be.
func (c Card) label(key rune) string {
	if key == cardDenyKey {
		return cardDenyLabel
	}
	switch c.Shape() {
	case ShapeQuestion:
		return cardAnswerLabel
	case ShapePlan:
		return cardApproveLabel
	case ShapePermission:
	}
	return cardAllowLabel
}

// questionKeys is a choice among options: which digits reach them, and whether
// there is yet an answer to send.
//
// A question with no readable payload - one learned from a fleet report, where
// the event carrying the options is gone - offers only the refusal, because
// there is nothing here to choose from and a bare allow is not an answer.
//
// The refusal alone is also what a question offers before anything has been
// chosen. The answer key is *not* offered then, and that is the cheaper half of
// the defence: an answer short of one question is refused beneath the daemon,
// and a refusal the operator has to read is worse than a key that was not
// offered. What it must never do instead is fall back to a bare allow.
//
// The move keys are the one droppable half, and they are dropped first when the
// line will not fit. Digits and the refusal are load-bearing - they are the only
// way to answer and the only way out - where ↑↓ and ↵ are a second route to
// something the digits already reach. A narrow pane keeps the affordance it
// cannot do without.
func (c Card) questionKeys(room int) string {
	if c.OnReview() {
		return reviewKeys
	}
	// Never [a]nswer. A question is settled on its review step, which shows
	// every answer before the press - so a settle key here would be a second
	// account of one irreversible thing, reachable from a surface that is not
	// showing what it would send.
	settle := cardDenyLabel
	q, ok := c.question()
	if !ok || len(q.Options) == 0 {
		return settle
	}
	need := chooseKeys(min(otherIndex(q)+1, cardMaxOptions)) + cardKeySep + settle
	full := cardMoveKeys + cardKeySep + need
	if ansi.StringWidth(full) <= room {
		return full
	}
	return need
}

// chooseKeys names the digits this question answers to. One option is spelled
// as one digit rather than as a range of one.
func chooseKeys(options int) string {
	if options == 1 {
		return cardChooseOne
	}
	return fmt.Sprintf(cardChooseFmt, options)
}

// body is what the operator has to read to answer.
//
// Each shape falls back to the permission body when its own payload is
// missing, and that is deliberate rather than defensive: an ask can reach this
// list from a fleet report with nothing resolved at all, and an interactive
// ask this build cannot read is a shape nobody has recorded. Both degrade to a
// yes/no on a named tool - more output, never less.
func (c Card) body(width int) string {
	switch c.Shape() {
	case ShapeQuestion:
		// The step past the last question, which is what submits. Checked
		// before the question, because Card.question clamps a review cursor
		// back onto the last question and would draw it again.
		if c.OnReview() {
			return c.reviewBody(width)
		}
		if _, ok := c.question(); ok {
			return c.questionBody(width)
		}
	case ShapePlan:
		if c.Detail != nil && strings.TrimSpace(c.Detail.Plan) != "" {
			return planBody(c.Detail.Plan, width)
		}
	case ShapePermission:
	}
	return AccentStyle.MaxWidth(width).Render(render.ToolCall(render.Call{Name: c.Tool, Display: c.Arg}, render.ToolStyle{}, width))
}

// questionBody draws the question the cursor is on and its options, with the
// chosen one marked and the cursor's own explanation under it.
//
// One question at a time: an ask carries up to four and the room has one
// card's worth of rows. The count line says there are others.
//
// The chip is drawn above the question and the cursored option's sample beside
// it, in whichever of preview.go's three tiers this width carries. Every row
// here is sized to the options column rather than to the pane: with a panel
// beside them those differ, and a row built to the full width would be laid out
// for a column it is not in and then cut back to fit.
func (c Card) questionBody(width int) string {
	q, ok := c.question()
	if !ok {
		return ""
	}
	sample := c.sample(q)
	options, panel, sideBySide := previewSplit(width)
	if sample == "" {
		options, sideBySide = width, false
	}

	rows := make([]string, 0, len(q.Options)+6)
	rows = append(rows, stepHeader(c, options), "")
	// Wrapped rather than truncated, alone among the rows here. Every other
	// line has somewhere else to be read in full - an option's detail is a
	// shortening of its own label, the count line is three words - and the
	// question is the one string the operator cannot answer without. A panel
	// beside it halves the room it has, which is what made a cut visible.
	rows = append(rows, TextStyle.Width(options).MaxWidth(options).Render(q.Text), "")
	for i, o := range q.Options {
		rows = append(rows, c.optionRow(i, o.Label, options))
	}
	rows = append(rows, c.optionRow(otherIndex(q), c.otherRowLabel(), options))
	// The cursored option's explanation, in a fixed slot *below* the list
	// rather than inline under the row it describes.
	//
	// Inline, it re-flowed every row beneath the cursor on every press: the
	// next label moved up as the old detail went away and a new one pushed it
	// back down. Two options and a detail each is enough to read as the
	// options swapping places, and the operator is reading them at exactly
	// that moment. The slot is drawn whenever *any* option explains itself, so
	// the block's height does not depend on where the cursor is either.
	if anyOptionExplains(q) {
		rows = append(rows, "", detailRow(c.optionDetail(q), options))
	}

	body := strings.Join(rows, "\n")
	switch {
	case sample == "":
		return body
	case sideBySide:
		return beside(body, previewBlock(sample, panel, previewLines), options, panel)
	case width >= previewMinPanel:
		// Stacked: the panel keeps fewer rows here than beside, because a
		// stacked one spends the axis a pinned card is already spending.
		return body + "\n" + previewBlock(sample, width, previewStackedLines)
	}
	// Too narrow for either. The options and the detail line survive, which is
	// what a choice can be made on - see preview.go on why dropping is a tier.
	return body
}

// sample is the cursored option's preview, and "" when it has none or the
// cursor is on no option. Only the cursored one: all of them for all the
// options is the wall of text the room exists not to be, and the reader is
// deciding about one at a time.
func (c Card) sample(q core.Question) string {
	if c.Option < 0 || c.Option >= len(q.Options) {
		return ""
	}
	if strings.TrimSpace(q.Options[c.Option].Preview) == "" {
		return ""
	}
	return q.Options[c.Option].Preview
}

// optionRow is one option: whether it is chosen, and whether the cursor is on
// it. The two markers are separate because they answer separate questions -
// what will be sent, and what a key would change.
func (c Card) optionRow(i int, label string, width int) string {
	chosen := c.picked(c.Cursor, i)
	if q, ok := c.question(); ok && i == otherIndex(q) {
		// The Other row is marked chosen by there being words in it, since it
		// holds no option index for picked to match.
		chosen = c.typed(c.Cursor) != ""
	}
	return optionRow(label, width, i == c.Option, chosen, AccentStyle)
}

// otherRowLabel is the Other row: the invitation, or the answer already
// written there. Showing the words back is what makes a typed answer
// reviewable in place rather than only on the review step.
func (c Card) otherRowLabel() string {
	if typed := c.typed(c.Cursor); typed != "" {
		return typed
	}
	return otherLabel
}

// optionRow is one row of a cursored list: the lead glyph, the label, bounded
// to the width, drawn in cursorStyle when it is the one under the cursor.
//
// A package function rather than a method because a Picker draws the same row
// and is not a Card - see picker.go for why it cannot be one. One
// implementation; a second bordered list beside this would be the parallel
// implementation the non-negotiables refuse. The cursor colour is the caller's
// so the completion menu can wear its own purple where a card and the picker
// wear the orange accent.
func optionRow(label string, width int, cursored, chosen bool, cursorStyle lipgloss.Style) string {
	lead := cardUnchosen
	switch {
	case chosen:
		lead = cardChosen
	case cursored:
		lead = cardCursor
	}
	style := HintStyle
	if cursored {
		style = cursorStyle
	}
	return style.MaxWidth(width).Render(ansi.Truncate(lead+collapseWhitespaceOneLine(label), width, ellipsis))
}

// anyOptionExplains reports whether this question has an explanation to show
// at all, which is what decides the slot exists. Asked about the whole
// question rather than about the cursored option, so walking the list cannot
// change the block's height.
func anyOptionExplains(q core.Question) bool {
	for _, o := range q.Options {
		if strings.TrimSpace(o.Detail) != "" {
			return true
		}
	}
	return false
}

// optionDetail is the cursored option's explanation, and "" when the cursor is
// on no option or that option has none. The empty case still costs its row -
// the slot is the question's, not the option's.
func (c Card) optionDetail(q core.Question) string {
	if c.Option < 0 || c.Option >= len(q.Options) {
		return ""
	}
	return q.Options[c.Option].Detail
}

// detailRow is the cursored option's own explanation: one flattened, bounded
// line, and only for the option under the cursor.
//
// The explanation is often the deciding information - two labels can be
// equally plausible and the consequence is what separates them - but all of it
// for all of them would be the wall of text the room exists not to be. One
// line for the one option being considered is the trade: enough to choose by,
// and the rest is DM work like every other long thing here.
func detailRow(detail string, width int) string {
	room := max(width-ansi.StringWidth(cardUnchosen), 1)
	return mutedLine(cardUnchosen+ansi.Truncate(collapseWhitespaceOneLine(detail), room, ellipsis), width)
}

// askHeadline is what an ask wants, in one line, for a surface that has no
// Card - the room's transcript, which draws the announcement while the card
// itself lives in one pane.
//
// Through Card.headline rather than beside it, so the room and the card cannot
// come to describe one blocked agent two different ways.
func askHeadline(ev core.Event) string {
	return newCard("", ev).headline()
}

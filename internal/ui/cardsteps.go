package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// A question ask is a wizard, and this file is the step model behind it.
//
// # Why there is a step past the last question
//
// cards.go settles by arm-and-confirm because a settle cannot be undone: an
// allow runs the tool, a deny reaches the model verbatim, and nothing on this
// wire retracts either. The arm was the only moment the operator could still
// change their mind - and all it showed them was a verb.
//
// A question ask can do better, because it has something to show. The step
// past the last question is a review: every question with the answer that is
// about to travel, and two options to end it. That is a strictly stronger
// guard than the arm it replaces, which is the whole reason ShapeQuestion is
// allowed to drop the two-press settle that ShapePermission and ShapePlan
// keep. Those two have nothing to review - a yes/no about one named action -
// so they keep the arm.
//
// # The cursor is one axis, not two
//
// Card.Cursor indexes the step and Card.Option the choice within it. The
// review is simply the last step, and its options are Submit and Cancel - so
// ↑↓, ↵ and the digits reach it through the same paths a question does, and
// cardkeys.go binds nothing new for it. The card is its own legend and every
// bracketed rune it draws is held to a bijection with what that file binds; a
// review page with keys of its own would have to earn two more entries.

// hasSteps reports whether this card walks steps at all. Only a question does:
// a permission and a plan are one decision with nothing to walk.
func (c Card) hasSteps() bool {
	return c.Shape() == ShapeQuestion && c.Detail != nil && len(c.Detail.Questions) > 0
}

// lastStep is the highest legal cursor - the review for a question ask, and
// the last question for anything else.
func (c Card) lastStep() int {
	if !c.hasSteps() {
		return 0
	}
	return len(c.Detail.Questions)
}

// OnReview reports whether the cursor is past the last question, which is the
// step that submits.
func (c Card) OnReview() bool {
	return c.hasSteps() && c.Cursor >= len(c.Detail.Questions)
}

// optionsAt is how many choices the given step offers, so Move clamps against
// the step the cursor is actually on rather than against a question.
func (c Card) optionsAt(step int) int {
	if !c.hasSteps() {
		return 0
	}
	if step >= len(c.Detail.Questions) {
		return reviewOptions
	}
	// One past the options the model supplied: the row that takes an answer
	// of the operator's own. It is always offered, so the count does not
	// depend on whether this question happens to suit its own list.
	return len(c.Detail.Questions[step].Options) + 1
}

// firstUnanswered is the earliest question with no choice, and whether there
// is one. It is what Submit consults: a review reached with a gap in it takes
// the operator to the gap rather than writing a short answer the daemon would
// refuse beneath them.
func (c Card) firstUnanswered() (int, bool) {
	if !c.hasSteps() {
		return 0, false
	}
	for i := range c.Detail.Questions {
		// Typed as well as picked, for Answered's reason: an answer in the
		// operator's own words is an answer. Reading only Choice sent Submit
		// back to a question that had been answered, with nothing on screen
		// saying why.
		if c.chosen(i) == noChoice && c.typed(i) == "" {
			return i, true
		}
	}
	return 0, false
}

// toStep puts the cursor on one step, at the top of it.
//
// At the top, because a step is a different list: carrying the previous
// question's option index forward opened a fresh set of labels with the cursor
// already part-way down, which is half of what was reported as the options
// swapping places. Move states the same rule for the same reason.
func (c Card) toStep(step int) Card {
	c.Cursor = clamp(step, 0, c.lastStep())
	c.Option = 0
	return c
}

// atOption puts the cursor on one option of the current step, clamped. A digit
// names an option directly, and the review's own digits go through here so the
// key and the arrows cannot disagree about what is selected.
func (c Card) atOption(option int) Card {
	c.Option = clamp(option, 0, max(c.optionsAt(c.Cursor)-1, 0))
	return c
}

// --- the strip ------------------------------------------------------------

const (
	// The tabs. A question is checked once it has an answer, which is what the
	// strip says that a count cannot: which steps are done, not how many exist.
	stripChecked   = "☒ "
	stripUnchecked = "☐ "

	// The submit tab wears a tick rather than a box, because it is not a thing
	// to answer - it is where answering ends.
	stripSubmitMark  = "✔ "
	stripSubmitLabel = "Submit"

	// The ends, which are the affordances for ←→. Drawn on the strip itself
	// rather than in the key line for cards_blocks.go's reason: a key that
	// exists only while an ask is up belongs on the thing that came and went.
	stripLeft  = "← "
	stripRight = " →"

	// stepTabSep separates two tabs, matching the spacing inside each.
	stepTabSep = "  "

	// stepCountFmt is the fallback: the count the strip replaced, for a width
	// that will not hold the strip. A strip cut mid-tab claims a question that
	// cannot be read, so the tier drops the whole thing rather than a piece.
	stepCountFmt = "question %d of %d"

	// stepReviewLabel is the fallback's wording on the review, where "question
	// 3 of 2" would be a lie about a step that is not a question.
	stepReviewLabel = "review · " + stripSubmitLabel
)

// stepStrip is the tab row, or "" when the width will not hold it.
//
// Tiered rather than truncated, like preview.go's panel: the caller falls back
// to stepCount when this returns nothing.
func (c Card) stepStrip(width int) string {
	if !c.hasSteps() {
		return ""
	}
	tabs := make([]string, 0, len(c.Detail.Questions)+1)
	for i, q := range c.Detail.Questions {
		mark := stripUnchecked
		if c.chosen(i) != noChoice || c.typed(i) != "" {
			mark = stripChecked
		}
		tabs = append(tabs, c.stripTab(i, mark+stripTabName(q.Header, i)))
	}
	tabs = append(tabs, c.stripTab(len(c.Detail.Questions), stripSubmitMark+stripSubmitLabel))

	strip := stripLeft + strings.Join(tabs, stepTabSep) + stripRight
	if ansi.StringWidth(strip) > width {
		return ""
	}
	return strip
}

// stripTab draws one tab, accented when the cursor is on it.
func (c Card) stripTab(step int, text string) string {
	if step == c.Cursor {
		return AccentStyle.Render(text)
	}
	return HintStyle.Render(text)
}

// stripTabName is a question's chip, or its number when the ask supplied no
// header. A tab with no name is a tab nobody can aim ←→ at.
// Flattened, not merely trimmed, and the reason is the one character BUG-9's
// fence deliberately leaves alone. A newline in a header survives containment -
// prose needs it - and it reaches a strip that is structurally one row, while
// the width guard above cannot see it: ansi.StringWidth sums across lines, so
// `A\nB` measures 2 and fits. optionRow and detailRow in this same package
// already collapse for this reason; this row was the one that did not.
func stripTabName(header string, i int) string {
	if h := collapseWhitespaceOneLine(header); h != "" {
		return h
	}
	return fmt.Sprintf("%d", i+1)
}

// stepCount is the strip's fallback: where the cursor is, in words that fit.
func stepCount(c Card) string {
	if !c.hasSteps() {
		return ""
	}
	if c.OnReview() {
		return stepReviewLabel
	}
	return fmt.Sprintf(stepCountFmt, c.Cursor+1, len(c.Detail.Questions))
}

// stepHeader is the one row that says where in the ask the operator is: the
// strip when it fits, and the count it replaced when it does not.
//
// One function so both the question body and the review draw the same row -
// two callers reaching for the tier separately is how the two steps start
// disagreeing about which question is current.
func stepHeader(c Card, width int) string {
	if strip := c.stepStrip(width); strip != "" {
		return strip
	}
	return mutedLine(stepCount(c), width)
}

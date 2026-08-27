package ui

// The question card as a wizard: the options list holds still under the
// cursor, and advancing a step starts at the top of the next question.
//
// Both of these were reported as one symptom - "the choices swap instead of
// staying still" - and they are two separate causes that compound. A list that
// re-flows under ↑↓ and a cursor that lands part-way down the next question
// read, together, as options trading places.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// optionRowsOf is the row index of each option's label in a rendered card.
//
// By label rather than by counting rows, because the point of the assertion is
// that a given option is on a given line - a count would pass on a card whose
// rows all moved together.
func optionRowsOf(t *testing.T, card Card, rendered string) map[string]int {
	t.Helper()
	q, ok := card.question()
	if !ok {
		t.Fatal("the card is putting no question, so there are no option rows to locate")
	}
	at := make(map[string]int, len(q.Options))
	for i, line := range strings.Split(rendered, "\n") {
		plain := ansi.Strip(line)
		for _, o := range q.Options {
			if strings.Contains(plain, firstWords(o.Label)) {
				at[o.Label] = i
			}
		}
	}
	if len(at) != len(q.Options) {
		t.Fatalf("found %d of the question's %d option rows, so this test would assert almost nothing:\n%s", len(at), len(q.Options), rendered)
	}
	return at
}

// The reported defect, in the smallest form that shows it.
//
// The detail row used to be inserted *inline* beneath whichever option the
// cursor was on, so moving the cursor down pulled the next label up a row and
// pushed a fresh detail row below it. Every option below the cursor moved, on
// every press, while the operator was reading them.
func TestTheOptionsHoldStillWhileTheCursorWalksThem(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	if len(card.Detail.Questions[0].Options) < 2 {
		t.Fatal("the recorded question offers fewer than two options, so nothing here could move")
	}

	first := oneCard(card).topView(wideRoom, Agent{Name: "sydney"})
	before := optionRowsOf(t, card, first)

	moved := card.Move(0, 1)
	second := oneCard(moved).topView(wideRoom, Agent{Name: "sydney"})
	after := optionRowsOf(t, moved, second)

	for label, row := range before {
		if after[label] != row {
			t.Errorf("↓ moved option %q from row %d to row %d - the list re-flows under the cursor, so options appear to swap places:\nbefore:\n%s\nafter:\n%s",
				label, row, after[label], first, second)
		}
	}
}

// The cursored option's explanation is often the deciding information, so
// holding the list still may not be paid for by dropping it.
func TestTheCursoredOptionStillExplainsItself(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	q := card.Detail.Questions[0]
	var detail string
	for _, o := range q.Options {
		if o.Detail != "" {
			detail = o.Detail
			break
		}
	}
	if detail == "" {
		t.Skip("no recorded option carries a detail")
	}
	out := ansi.Strip(oneCard(card).topView(wideRoom, Agent{Name: "sydney"}))
	if !strings.Contains(out, firstWords(detail)) {
		t.Errorf("the cursored option's explanation is not drawn, so the list holds still by saying less:\n%s", out)
	}
}

// The second cause. Move clamped the *old* option index into the new
// question's range instead of resetting it, so answering option 3 of question
// one opened question two with the cursor already on option 3 - a fresh set of
// labels with the cursor part-way down it.
func TestAdvancingAQuestionStartsAtTheTopOfTheNextOne(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	if len(card.Detail.Questions) < 2 {
		t.Fatal("the recorded ask puts one question, so there is no advance to check")
	}

	// Walk down to a non-zero option, then answer it.
	moved := card.Move(0, 1)
	if moved.Option == 0 {
		t.Fatal("the cursor would not move off option 0, so this test cannot see the defect")
	}
	advanced := moved.Pick(0, moved.Option).Move(1, 0)

	if advanced.Cursor != 1 {
		t.Fatalf("answering the first question left the cursor on question %d, want 1", advanced.Cursor)
	}
	if advanced.Option != 0 {
		t.Errorf("question 2 opened with the cursor on option %d, want 0 - the previous question's index was carried forward", advanced.Option)
	}
}

// --- the strip ------------------------------------------------------------

// The strip is the one row that says how much of the ask is left. It replaced
// the chip and the "question 1 of 4" count, which between them named the step
// the operator was on and never the ones they were not.
func TestTheStripNamesEveryQuestionAndTheSubmitStep(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	out := ansi.Strip(oneCard(card).topView(wideRoom, Agent{Name: "sydney"}))

	for i, q := range card.Detail.Questions {
		if q.Header == "" {
			t.Fatalf("question %d carries no header, so the strip has nothing to name it by", i+1)
		}
		if !strings.Contains(out, q.Header) {
			t.Errorf("the strip omits question %d (%q), so the operator cannot see what is still to come:\n%s", i+1, q.Header, out)
		}
	}
	if !strings.Contains(out, stripSubmitLabel) {
		t.Errorf("the strip does not name the submit step, so the end of the ask is invisible until it is reached:\n%s", out)
	}
}

// A tab is checked when its question has an answer. That is the whole reason
// the strip beats a count: it says which are done, not just how many there are.
func TestTheStripChecksTheQuestionsThatHaveBeenAnswered(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	before := ansi.Strip(oneCard(card).topView(wideRoom, Agent{Name: "sydney"}))
	if strings.Contains(before, stripChecked) {
		t.Errorf("a card nobody has answered already checks a question in its strip:\n%s", before)
	}

	after := ansi.Strip(oneCard(card.Pick(0, 0)).topView(wideRoom, Agent{Name: "sydney"}))
	if !strings.Contains(after, stripChecked) {
		t.Errorf("answering a question did not check it in the strip:\n%s", after)
	}
}

// The strip is tiered like preview.go's panel: a strip cut mid-tab claims a
// question that cannot be read, so a width that will not hold it falls back to
// the count it replaced rather than to a fragment.
func TestANarrowCardFallsBackToTheCountRatherThanACutStrip(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	out := ansi.Strip(oneCard(card).topView(minBlockWidth, Agent{Name: "sydney"}))

	want := stepCount(card)
	if !strings.Contains(out, want) {
		t.Errorf("a card too narrow for the strip does not fall back to %q, so it says nothing about how many questions there are:\n%s", want, out)
	}
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > minBlockWidth {
			t.Errorf("a card row is %d columns wide in a %d-column block, which shoves the sidebars out of place: %q", w, minBlockWidth, line)
		}
	}
}

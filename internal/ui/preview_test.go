package ui

// The sample beside an option, and the chip above a question.
//
// Every expectation here is read out of the recorded ask rather than written
// down, for askdetail_test.go's reason one level up: a hand-written preview
// would pin one recording's prose and would keep passing against a renderer
// that returned a constant.

import (
	"strings"
	"testing"
)

// recordedQuestion is the first recorded AskChoice as a Card, with the cursor
// on the option the assertions are about.
func recordedQuestion(t *testing.T) Card {
	t.Helper()
	return newCard("s1", *questionAsk(t))
}

// The chip is what tells one question of four from the next when only one is
// on screen at a time.
func TestAQuestionDrawsTheChipTheAskLabelledItWith(t *testing.T) {
	card := recordedQuestion(t)
	q, ok := card.question()
	if !ok {
		t.Fatal("the recorded ask puts no question")
	}
	if q.Header == "" {
		t.Fatal("the recorded question carries no header: this test cannot produce the condition")
	}
	if got := card.body(wideRoom); !strings.Contains(stripANSI(got), q.Header) {
		t.Errorf("the card does not draw the chip %q the ask labelled its question with:\n%s", q.Header, got)
	}
}

// A pane with room for both draws the cursored option's sample beside the
// options, which is the deciding information for two labels that are equally
// plausible until you see what each produces.
func TestAWidePaneDrawsTheCursoredOptionsSampleBesideTheOptions(t *testing.T) {
	card := recordedQuestion(t)
	q, _ := card.question()
	sample := firstLineOf(t, q.Options[0].Preview)

	body := stripANSI(card.body(wideRoom))
	if !strings.Contains(body, sample) {
		t.Errorf("the sample %q is not drawn at %d columns:\n%s", sample, wideRoom, body)
	}
	if !sideBySideIn(body, sample) {
		t.Errorf("the sample is drawn with no rule beside it, so it is under the options rather than next to them:\n%s", body)
	}
	// And the rule runs the whole panel, not just its first row: a sample an
	// agent wrote is unbounded on every row that has no rule on it.
	rows := strings.Split(strings.TrimRight(body, "\n"), "\n")
	for i, line := range rows {
		if !strings.Contains(line, previewRuleGlyph) {
			t.Errorf("row %d of the card has no rule, so the panel is unbounded there:\n%s", i, body)
			break
		}
	}
}

// Only the cursored one. All of them for all the options is the wall of text
// the room exists not to be, and the reader is deciding about one at a time.
func TestOnlyTheCursoredOptionsSampleIsDrawn(t *testing.T) {
	card := recordedQuestion(t)
	q, _ := card.question()
	if len(q.Options) < 2 {
		t.Fatal("the recorded question offers one option: this test cannot produce the condition")
	}
	other := firstLineOf(t, q.Options[1].Preview)

	body := stripANSI(card.body(wideRoom))
	if strings.Contains(body, other) {
		t.Errorf("the sample for an option the cursor is not on is drawn:\n%s", body)
	}
	// And moving the cursor brings it forward, so the panel tracks the cursor
	// rather than being pinned to option zero.
	moved := stripANSI(card.Move(0, 1).body(wideRoom))
	if !strings.Contains(moved, other) {
		t.Errorf("the cursor moved to the second option and its sample did not follow:\n%s", moved)
	}
}

// A pane too narrow for a panel beside the options drops it rather than
// squeezing both, and keeps the options - which are the part that can be
// answered.
func TestATooNarrowPaneDropsTheSampleAndKeepsTheOptions(t *testing.T) {
	card := recordedQuestion(t)
	q, _ := card.question()
	sample := firstLineOf(t, q.Options[0].Preview)

	body := stripANSI(card.body(minBlockWidth))
	if strings.Contains(body, sample) {
		t.Errorf("the sample is drawn at %d columns, where there is no room to read it:\n%s", minBlockWidth, body)
	}
	if !strings.Contains(body, q.Options[0].Label) {
		t.Errorf("the options went with it, which is the half that can be answered:\n%s", body)
	}
}

// Bounded in both directions, at every width. A card is arbitrary text an
// agent wrote, and one line wider than the pane shoves both sidebars out of
// place - the reason roomBlock is bounded at all.
func TestTheSamplePanelIsBoundedAtEveryWidth(t *testing.T) {
	card := recordedQuestion(t)
	q, _ := card.question()
	// The options and the Other row past them, the panel at its tallest, and
	// the fixed rows around both: the strip, the question (which wraps to two
	// at the narrow widths here), the detail slot, and the blank line before
	// each of the question, the options and the detail. Derived so a build
	// that grows any of them has to say so here.
	rows := len(q.Options) + 1 + previewLines + 7
	for _, w := range cardWidths {
		body := card.body(w)
		if got := widest(body); got > w {
			t.Errorf("at width %d the card draws a line %d wide:\n%s", w, got, stripANSI(body))
		}
		if got := len(strings.Split(body, "\n")); got > rows {
			t.Errorf("at width %d the card is %d rows, more than the %d its own bounds allow:\n%s", w, got, rows, stripANSI(body))
		}
	}
}

// The three tiers are ordered and each one is reachable: there is a width that
// draws a panel beside the options, a narrower one that stacks it, and a
// narrower one still that draws none. Found by walking the widths rather than
// written down, so a build that moves a floor re-measures instead of asserting
// a number that has stopped being true.
func TestTheThreeTiersAreEachReachableAndInOrder(t *testing.T) {
	card := recordedQuestion(t)
	q, _ := card.question()
	sample := firstLineOf(t, q.Options[0].Preview)

	var dropped, stacked, side int
	for w := minBlockWidth; w <= 200; w++ {
		body := stripANSI(card.body(w))
		switch {
		case !strings.Contains(body, sample):
			dropped = w
		case sideBySideIn(body, sample):
			if side == 0 {
				side = w
			}
		default:
			stacked = w
		}
	}
	if dropped == 0 || stacked == 0 || side == 0 {
		t.Fatalf("the tiers are not all reachable: widest dropped=%d, widest stacked=%d, narrowest beside=%d", dropped, stacked, side)
	}
	if dropped >= stacked || stacked >= side {
		t.Errorf("the tiers are out of order: dropped up to %d, stacked up to %d, beside from %d - a wider pane must never show less",
			dropped, stacked, side)
	}
}

// sideBySideIn reports whether the sample shares a row with the rule, which is
// what "beside" means on a frame: a stacked panel has the rows to itself.
func sideBySideIn(body, sample string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, sample) && strings.Contains(line, previewRuleGlyph) {
			return true
		}
	}
	return false
}

// A sample longer than the panel keeps is cut, and says so. A panel that
// silently stopped would read as the whole of what the option produces.
func TestALongSampleIsCutAndSaysSo(t *testing.T) {
	long := strings.Repeat("a line of sample output\n", previewLines*3)
	panel := previewBlock(long, 40, previewLines)
	if got := len(strings.Split(panel, "\n")); got != previewLines+1 {
		t.Errorf("a sample of %d lines drew %d rows, want %d kept plus one saying what was cut",
			previewLines*3, got, previewLines+1)
	}
	if !strings.Contains(stripANSI(panel), "more") {
		t.Errorf("the panel cut the sample and does not say so:\n%s", panel)
	}
}

// firstLineOf is a sample's opening line, which is what an assertion can look
// for on a single row of the frame.
func firstLineOf(t *testing.T, sample string) string {
	t.Helper()
	if strings.TrimSpace(sample) == "" {
		t.Fatal("the recorded option carries no preview: this test cannot produce the condition")
	}
	return strings.Split(strings.TrimSpace(sample), "\n")[0]
}

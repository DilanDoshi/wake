package ui

// The adversarial pass: what this branch asserts, and what would make each
// assertion false.
//
// Every test here was written to *break* a claim made in a commit message or a
// comment, not to confirm one. Three of the four claims survived; the fourth
// is why cards_blocks.go bounds the Other row's digits.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// Claim: "a framed card is exactly as tall as the unframed one was - the two
// rows the box costs are the two the labels used to occupy inside it."
//
// Nothing asserted it, and it is load-bearing twice over: menuRows clips the
// block from the bottom and cardFullyDrawn decides whether the keys may be
// read at all, so a card two rows taller than it was is a card whose keys go
// dead at heights that used to be fine.
func TestTheFrameCostsTheCardNoRows(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	cs := oneCard(card)
	by := Agent{ID: "s1", Name: "sydney"}

	for _, w := range []int{40, 52, 92, 160} {
		framed := len(strings.Split(cs.View(card, w, by, false), "\n"))

		// The same rows the card would draw with no frame: the headline, the
		// body laid out for the same room, and the key line.
		inner := max(w-cardFrameWidth, 1)
		bare := len(strings.Split(joinBlock(
			warnLine(cardLead+speaker(by)+markerSep+card.headline(), inner),
			card.body(inner),
			mutedLine(cs.keyLine(card, inner, false), inner),
		), "\n"))

		if framed != bare {
			t.Errorf("at width %d a framed card is %d rows against the unframed %d: the claim that the frame is free is what menuRows and cardFullyDrawn are budgeted on", w, framed, bare)
		}
	}
}

// Claim: "the options hold still while the cursor walks them."
//
// The passing test walks one width. The rows are laid out beside a preview
// panel whose height is its own, and beside() joins on the taller of the two -
// so a width where the panel is the taller column is a width where the body's
// own stability proves nothing about what is drawn.
func TestTheOptionsHoldStillAtEveryWidth(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	moved := card.Move(0, 1)
	for _, w := range []int{minBlockWidth, 40, 52, 92, 160} {
		first := oneCard(card).topView(w, Agent{Name: "sydney"})
		second := oneCard(moved).topView(w, Agent{Name: "sydney"})

		// Row *positions*, not the row count. The reported defect moved every
		// option below the cursor while keeping the block exactly as tall - the
		// detail row left one place and arrived at another - so a height
		// comparison passes straight over it. This is the mutation that found
		// that out.
		before, after := optionRowsOf(t, card, first), optionRowsOf(t, moved, second)
		for label, row := range before {
			if after[label] != row {
				t.Errorf("at width %d ↓ moved option %q from row %d to row %d:\nbefore:\n%s\nafter:\n%s", w, label, row, after[label], first, second)
			}
		}
	}
}

// Claim: extracting titledEdge left the composer's own top edge unchanged.
//
// A refactor's whole promise is that nothing moved, and the suite passing is
// not evidence of that - it is evidence that nothing *asserted* moved. This
// compares the edge against the arithmetic the old code did, spelled out.
func TestTheComposersTopEdgeIsWhatItWas(t *testing.T) {
	for _, title := range []string{"", "@sydney", "a title longer than the box can possibly hold"} {
		for _, w := range []int{minComposerWidth, 30, 80, 200} {
			c := NewComposer().WithTitle(title).SetWidth(w)
			got := strings.Split(c.box(w), "\n")[0]

			b := lipgloss.RoundedBorder()
			edge := w - boxFrameWidth
			label := ""
			if title != "" && edge > titleMinBorder {
				label = " " + ansi.Truncate(title, edge-titleMinBorder, ellipsis) + " "
			}
			rule := lipgloss.NewStyle().Foreground(c.boxStyle().GetBorderTopForeground())
			want := rule.Render(b.TopLeft+strings.Repeat(b.Top, edge-lipgloss.Width(label)-titleInset)) +
				headerStyle.Render(label) +
				rule.Render(strings.Repeat(b.Top, titleInset)+b.TopRight)

			if got != want {
				t.Errorf("title %q at width %d: the extracted edge draws %q, the arithmetic it replaced draws %q", title, w, ansi.Strip(got), ansi.Strip(want))
			}
		}
	}
}

// Claim: the digits reach the Other row.
//
// They do not, at nine options - there are nine single digits and the Other
// row is the tenth thing. The card must not advertise a digit that does not
// exist, and nothing may panic reaching for one.
func TestAQuestionWithEveryDigitSpentStillDrawsAndDoesNotOverPromise(t *testing.T) {
	opts := make([]core.Option, cardMaxOptions)
	for i := range opts {
		opts[i] = core.Option{Label: "option " + string(rune('a'+i))}
	}
	card := Card{
		AgentID: "s1", RequestID: "r1", Ask: core.AskChoice,
		Detail: &core.AskDetail{Questions: []core.Question{{Text: "which?", Header: "pick", Options: opts}}},
		Choice: []int{noChoice}, Typed: []string{""},
	}
	out := ansi.Strip(oneCard(card).topView(wideRoom, Agent{Name: "sydney"}))
	if !strings.Contains(out, otherLabel) {
		t.Fatalf("the Other row is not drawn at %d options:\n%s", cardMaxOptions, out)
	}
	// It advertises the digits that exist and no more.
	if !strings.Contains(out, chooseKeys(cardMaxOptions)) {
		t.Errorf("the card does not advertise the %d digits it has:\n%s", cardMaxOptions, out)
	}
	if strings.Contains(out, chooseKeys(cardMaxOptions+1)) {
		t.Errorf("the card advertises a %dth digit, and there are only %d:\n%s", cardMaxOptions+1, cardMaxOptions, out)
	}
	// And the arrows still reach it, which is what keeps it answerable.
	walked := card
	for range cardMaxOptions + 2 {
		walked = walked.Move(0, 1)
	}
	if walked.Option != otherIndex(card.Detail.Questions[0]) {
		t.Errorf("↓ stops at option %d and the Other row is %d, so at %d options it cannot be reached at all", walked.Option, otherIndex(card.Detail.Questions[0]), cardMaxOptions)
	}
}

// Claim: "the key line is honest in both directions."
//
// `then ↵` says a settle takes a second press, and it was true when [a] and
// [d] were both arms. A question has neither now - ↵ and the digits choose
// outright, and the refusal opens a box that announces itself - so the hint
// was telling the operator to press a key that would do nothing. Found by
// reading a rendered screen rather than by a test, which is what
// cmd/wake's pty harness is for.
func TestOnlyAShapeThatArmsSaysASecondPressIsComing(t *testing.T) {
	question := cardFor(t, recordedAsks(t, choiceFixture)[0])
	for name, c := range map[string]Card{
		"a question": question,
		"the review": question.toStep(len(question.Detail.Questions)),
	} {
		line := (Cards{open: []Card{c}}).keyLine(c, wideRoom, false)
		if strings.Contains(line, cardConfirmHint) {
			t.Errorf("%s draws %q, which promises a second press that neither ↵ nor a digit takes: %q", name, cardConfirmHint, line)
		}
		if !strings.Contains(line, cardDenyLabel) {
			t.Errorf("%s does not offer the refusal, which is its only way out: %q", name, line)
		}
	}

	// And a shape that *does* still arm keeps it, or the two-press settle is
	// unadvertised on the one surface that is its own legend.
	perm, _ := (Cards{}.Add("s1", ask("r1", "Bash", "ls"))).Top()
	line := (Cards{open: []Card{perm}}).keyLine(perm, wideRoom, false)
	if !strings.Contains(line, cardConfirmHint) {
		t.Errorf("a permission card no longer says its allow key takes a second press: %q", line)
	}
}

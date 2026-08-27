package ui

// What the three cards look like.
//
// Every assertion about content is derived from the recorded ask it is drawn
// from, never from a string typed here: a hand-written "What output format
// should the report use?" pins one recording's prose, and a renderer that
// printed a constant would satisfy it. What is written out are the *shapes* -
// the words Wake itself chose, which are the thing a reader has to be able to
// tell apart.

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// cardWidths are the widths the room actually gets. 52 is 120 columns with
// both sidebars and a DM open, 92 is 200 columns with the right sidebar
// closed, and minBlockWidth is the floor every block renderer is held to.
var cardWidths = []int{minBlockWidth, 40, narrowRoom, wideRoom, 120}

// oneCard is a queue holding exactly this card, choices and cursor included.
// Cards.With deliberately refuses a card the list does not already hold - a
// settled ask must not be resurrected by the answer to it - so a view test
// building one from a card it has already modified says so here.
func oneCard(c Card) Cards { return Cards{open: []Card{c}} }

// topView draws the oldest open ask with the rest counted behind it, which is
// what the room does when no conversation is on screen. These tests are about
// the rows a card draws rather than about which pane draws it, so they name the
// card the one way that needs no layout.
func (cs Cards) topView(width int, by Agent) string {
	top, ok := cs.Top()
	if !ok {
		return ""
	}
	return cs.View(top, width, by, false)
}

// The digits a question answers to, written out once.
//
// Every other assertion about them compares the view against chooseKeys itself,
// which is the helper under test: `chooseKeys` returning "" leaves all of those
// green (strings.Contains(x, "") always is) while the card draws no way to pick
// an option at all - and a question with no way to choose is a question whose
// [a]nswer key can never be offered, which is the precise defect the digits
// were added to prevent.
func TestTheDigitsAQuestionAnswersToAreSpelledOut(t *testing.T) {
	for _, tc := range []struct {
		options int
		want    string
	}{
		{1, "[1] choose"},
		{2, "[1-2] choose"},
		{4, "[1-4] choose"},
	} {
		if got := chooseKeys(tc.options); got != tc.want {
			t.Errorf("chooseKeys(%d) = %q, want %q", tc.options, got, tc.want)
		}
	}
}

func TestACardNamesTheAgentAndWhatItIsAskingFor(t *testing.T) {
	cs := Cards{}.Add("s1", ask("r1", "Bash", "rm -rf build/"))
	out := cs.topView(narrowRoom, Agent{ID: "s1", Name: "alex", Label: "build"})
	for _, want := range []string{"alex", "build", "rm -rf build/", "Bash", cardAllowKeys} {
		if !strings.Contains(out, want) {
			t.Errorf("the card does not show %q:\n%s", want, out)
		}
	}
}

// A question card that hides its options leaves the operator choosing between
// things they cannot see - and, since the answer is keyed on the question's own
// text, a card that cannot show the question is one that cannot answer it
// either.
func TestAQuestionCardShowsItsQuestionAndEveryOptionItOffers(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	out := oneCard(card).topView(wideRoom, Agent{Name: "sydney"})

	q := card.Detail.Questions[0]
	if !strings.Contains(out, firstWords(q.Text)) {
		t.Errorf("a question card hides the question:\n%s", out)
	}
	for _, o := range q.Options {
		if !strings.Contains(out, firstWords(o.Label)) {
			t.Errorf("a question card hides the option %q, so the operator is choosing between options they cannot see:\n%s", o.Label, out)
		}
	}
	// Nothing has been chosen yet, so the answer key is not offered - a
	// question short one choice is refused beneath the daemon, and a refusal
	// the operator has to read is worse than a key that was not offered. What
	// is offered is the way to choose.
	if strings.Contains(out, cardAnswerKeys) {
		t.Errorf("a question nobody has answered offers %q:\n%s", cardAnswerKeys, out)
	}
	// One past the options the model supplied: the digits reach the Other row
	// too, which is where an answer the list does not contain goes.
	if !strings.Contains(out, chooseKeys(otherIndex(q)+1)) {
		t.Errorf("a question card offers no way to choose an option, so %q could never become reachable:\n%s", cardAnswerKeys, out)
	}
	if strings.Contains(out, cardAllowKeys) {
		t.Errorf("a question card offers %q, which is the word for a tool call rather than for a choice:\n%s", cardAllowKeys, out)
	}
	// An ask putting more than one question has to say so, or answering the
	// first looks like answering the ask. The strip says it by naming every
	// step rather than by counting them - see cardsteps.go.
	for i, q := range card.Detail.Questions {
		if !strings.Contains(ansi.Strip(out), stripTabName(q.Header, i)) {
			t.Errorf("the card gives no sign question %d of %d exists:\n%s", i+1, len(card.Detail.Questions), out)
		}
	}
}

// The chosen option has to be distinguishable from the one the cursor is
// merely resting on. They are different claims - what will be sent, and what a
// key would change - and a card that spelled them the same way would have
// somebody approve the row they had scrolled to.
func TestAChosenOptionIsMarkedDifferentlyFromTheOneTheCursorIsOn(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	before := oneCard(card).topView(wideRoom, Agent{Name: "sydney"})
	if strings.Contains(before, cardChosen) {
		t.Errorf("a card nobody has answered already marks an option as chosen:\n%s", before)
	}

	after := oneCard(card.Pick(0, 1).Move(0, 1)).topView(wideRoom, Agent{Name: "sydney"})
	if !strings.Contains(after, cardChosen) {
		t.Errorf("a chosen option is not marked, so the operator cannot see what their answer will carry:\n%s", after)
	}
}

// A plan card that shows no plan is the current behaviour, and it is what this
// task exists to fix: approving a document you have not read.
func TestAPlanCardShowsThePlanAndPointsAtTheDMForTheRest(t *testing.T) {
	card := planCard(t)
	out := oneCard(card).topView(wideRoom, Agent{Name: "sydney", Label: "auth-fix"})

	if !strings.Contains(out, cardHasPlan) {
		t.Errorf("a plan card does not say it is a plan:\n%s", out)
	}
	if !strings.Contains(out, cardPlanKeys) {
		t.Errorf("a plan card offers %q: a plan approved by a key labelled \"allow\" reads as though something is about to be run:\n%s", cardAllowKeys, out)
	}
	if !containsAnyWord(out, card.Detail.Plan) {
		t.Errorf("a plan card shows no plan, so the operator approves a document they never saw:\n%s", out)
	}
	// The recorded plans are thousands of characters; the room is a hub, so
	// past a few lines it has to become a pointer rather than the content.
	if !strings.Contains(out, openDMHint) {
		t.Errorf("a plan long enough to be cut does not say where the rest is:\n%s", out)
	}
	if got := len(strings.Split(out, "\n")); got > cardPlanLines+6 {
		t.Errorf("a plan card is %d rows: the room is one column of a three-region layout and a card that fills it has pushed the conversation off screen", got)
	}
}

// An ask this build cannot read must still be answerable. A card with an empty
// body offers a yes and a no about nothing.
func TestAnInteractiveAskWithNoPayloadDegradesToAYesOrNoRatherThanToAnEmptyCard(t *testing.T) {
	for name, kind := range map[string]core.AskKind{"a question": core.AskChoice, "a plan": core.AskApproval} {
		ev := ask("r1", "Write", "note.txt")
		ev.Ask = kind
		out := Cards{}.Add("s1", ev).topView(narrowRoom, Agent{Name: "sydney"})
		if !strings.Contains(out, "note.txt") {
			t.Errorf("%s whose payload this build cannot read drew a card with nothing in it:\n%s", name, out)
		}
	}
}

// A card is one column of a three-region layout, and lipgloss sizes a joined
// row on its widest line - so one over-wide line here shoves both sidebars out
// of place. Checked over every shape at every width the layout produces.
func TestNoCardDrawsALineWiderThanThePaneItIsIn(t *testing.T) {
	choice := cardFor(t, recordedAsks(t, choiceFixture)[0])
	cards := map[string]Cards{
		"a permission": Cards{}.Add("s1", ask("r1", "Bash", strings.Repeat("rm -rf a-very-long-path/", 20))),
		"a question":   oneCard(choice.Pick(0, 0)),
		"a plan":       oneCard(planCard(t)),
		"a queue of two": Cards{}.Add("s1", ask("r1", "Bash", "ls")).
			Add("s2", ask("r2", "Write", "note.txt")),
	}
	long := Agent{ID: "s1", Name: "sydney", Label: "a-very-long-branch-name-indeed"}
	for name, cs := range cards {
		for _, w := range cardWidths {
			out := cs.topView(w, long)
			if out == "" {
				t.Fatalf("%s drew nothing at width %d", name, w)
			}
			assertFitsWidth(t, out, max(w, minBlockWidth))
		}
	}
}

// A card counts nothing under it any more, and it must not: it is about one
// agent, drawn in that agent's own pane, and a number about the fleet under it
// would be a claim its surface cannot back. What counts the fleet's blocked
// agents is the awareness strip's "N need you", which is in every view - see
// TestTheCountsSurviveAWidthTheNameDoesNot.
func TestACardCountsNothingUnderIt(t *testing.T) {
	two := Cards{}.Add("s1", ask("r1", "Bash", "ls")).Add("s2", ask("r2", "Write", "note.txt"))
	if got := two.topView(narrowRoom, Agent{Name: "alex"}); strings.Contains(got, "waiting") {
		t.Errorf("a card drew a count of the fleet's other asks:\n%s", got)
	}
}

func TestNoCardsDrawsNothingAtAll(t *testing.T) {
	if got := (Cards{}).topView(wideRoom, Agent{Name: "alex"}); got != "" {
		t.Errorf("an empty queue drew %q: the room reserves no row for a card nobody is being asked for", got)
	}
}

// firstWords is enough of a recorded string to find it in a rendered card
// without depending on where the renderer wrapped or truncated it.
func firstWords(s string) string {
	fields := strings.Fields(s)
	if len(fields) > 3 {
		fields = fields[:3]
	}
	return strings.Join(fields, " ")
}

// containsAnyWord reports whether the rendering carries a distinctive run of
// the source. Markdown rendering re-wraps and re-styles, so a whole line is
// the wrong unit; a long word from the body is not.
func containsAnyWord(out, src string) bool {
	for _, w := range strings.Fields(src) {
		if len(w) > 8 && strings.Contains(out, w) {
			return true
		}
	}
	return false
}

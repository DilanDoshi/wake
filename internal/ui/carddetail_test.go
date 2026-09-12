package ui

// ⌃E opens the cursored option's whole description on a question card. A
// description too long for one line is otherwise truncated with an ellipsis and
// has no other way to be read - the card does not scroll.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A sentinel at the end of a deliberately over-long description: the collapsed
// row must cut it, the expanded block must keep it.
const detailSentinel = "ENDOFDESCRIPTIONSENTINEL"

func TestExpandingRevealsTheWholeOptionDescription(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	if len(card.Detail.Questions[0].Options) == 0 {
		t.Fatal("the recorded question offers no options, so there is no description to expand")
	}
	// Longer than one line at wideRoom, ending in the sentinel. Preview cleared
	// so no panel splits the width - the options column is the whole card and the
	// truncation is predictable.
	long := strings.Repeat("this option changes the output format in a way worth reading about ", 4) + detailSentinel
	card.Detail.Questions[0].Options[0].Detail = long
	card.Detail.Questions[0].Options[0].Preview = ""
	card.Option = 0

	collapsed := ansi.Strip(oneCard(card).topView(wideRoom, Agent{Name: "sydney"}))
	if strings.Contains(collapsed, detailSentinel) {
		t.Fatalf("the collapsed card already shows the whole description, so this test cannot see the fix:\n%s", collapsed)
	}
	if !strings.Contains(collapsed, ellipsis) {
		t.Errorf("the collapsed description was not truncated with an ellipsis:\n%s", collapsed)
	}

	card.DetailExpanded = true
	expanded := ansi.Strip(oneCard(card).topView(wideRoom, Agent{Name: "sydney"}))
	if !strings.Contains(expanded, detailSentinel) {
		t.Errorf("⌃E did not reveal the rest of the option's description:\n%s", expanded)
	}
}

// ⌃E on the focused pane flips its question card's description open, and a
// second press closes it.
func TestCtrlEExpandsAndCollapsesTheFocusedQuestionCard(t *testing.T) {
	a := wrapped(t, 200, 40, 200).(App).openDMWith("s1", "sydney")
	ask := recordedAsks(t, choiceFixture)[0]
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ask}).applyGeometry()

	card, ok := a.cardOf(a.focus)
	if !ok {
		t.Fatal("the focused pane draws no card, so ⌃E has nothing to act on")
	}
	if !card.canExpandDetail() {
		t.Fatal("the recorded question carries no option description, so ⌃E has nothing to open")
	}
	if card.DetailExpanded {
		t.Fatal("a fresh card starts with its description already expanded")
	}

	a = pressCtrlE(t, a)
	if card, _ := a.cardOf(a.focus); !card.DetailExpanded {
		t.Error("⌃E did not expand the focused question card's description")
	}
	a = pressCtrlE(t, a)
	if card, _ := a.cardOf(a.focus); card.DetailExpanded {
		t.Error("a second ⌃E did not collapse the description")
	}
}

// An option with no description still holds the slot's 2-column indent when
// expanded, matching the collapsed row: the cursored option may lack a
// description while another carries one, so the slot is drawn either way and
// must not jump between the two states.
func TestExpandingAnEmptyDescriptionKeepsTheSlotIndent(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	if len(card.Detail.Questions[0].Options) < 2 {
		t.Skip("need two options so one explains itself while the cursored one does not")
	}
	card.Detail.Questions[0].Options[0].Detail = "" // cursored: no description
	card.Detail.Questions[0].Options[0].Preview = ""
	card.Detail.Questions[0].Options[1].Detail = "but this other option does explain itself"
	card.Option = 0
	q, _ := card.question()

	collapsed := card.detailSlot(q, 60)
	card.DetailExpanded = true
	if expanded := card.detailSlot(q, 60); expanded != collapsed {
		t.Errorf("an empty description draws differently expanded vs collapsed, so the slot jumps:\ncollapsed=%q\nexpanded=%q", collapsed, expanded)
	}
}

// Expansion is a card-level mode, so walking to another option keeps it open -
// the operator reads each option's whole description in turn.
func TestExpansionSurvivesACursorMove(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	if len(card.Detail.Questions[0].Options) < 2 {
		t.Skip("need two options to move between")
	}
	card.DetailExpanded = true
	if moved := card.Move(0, 1); !moved.DetailExpanded {
		t.Error("moving the option cursor collapsed the description")
	}
}

// ⌃E only claims a question card that has a description to open. A permission
// card has none, so ⌃E falls through to the tool-result expansion it always did.
func TestCtrlELeavesANonQuestionCardToTheToolResultExpansion(t *testing.T) {
	perm := cardFor(t, recordedAsks(t, permFixture)[0])
	if perm.canExpandDetail() {
		t.Error("a permission card claims ⌃E, which would take it from the tool-result expansion")
	}
}

func pressCtrlE(t *testing.T, a App) App {
	t.Helper()
	next, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlE})
	return next
}

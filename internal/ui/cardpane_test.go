package ui

// Which pane an ask is answerable in.
//
// The card used to be a room-only surface, which is a claim about the layout
// that the layout does not honour: below dmTakeoverColumns exactly one column
// is drawn, so a focused conversation leaves Regions.Cols[0] at zero. The ask
// was then on no surface at all - nothing drawn, and cardFullyDrawn false, so
// every key was dead as well - and the agent stayed blocked with no way to
// answer it anywhere on screen.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// narrowColumns is a terminal below dmTakeoverColumns, where exactly one grid
// column is drawn. Derived from the breakpoint rather than typed, so a build
// that moves it re-measures instead of testing a width that no longer means
// anything.
const narrowColumns = dmTakeoverColumns - 1

// asking is one agent's conversation open at this width, with a recorded
// AskChoice delivered into it - the real order, which is that the pane is open
// and then the agent asks.
func asking(t *testing.T, width int) (App, string) {
	t.Helper()
	ev := questionAsk(t)
	a := newRoomApp(t).withSize(width, 40).withAgents("john")
	a = pick(a, "s1").openDMWith("s1", "john").applyGeometry()
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: ev})
	return a, ev.Tool.Ask.Questions[0].Text
}

// The question and its options are on screen in the conversation that asked
// them, at a width where the room is not drawn at all.
func TestAQuestionIsDrawnInTheConversationItWasAskedIn(t *testing.T) {
	a, text := asking(t, narrowColumns)

	if a.regions().Room() != 0 {
		t.Fatalf("the room has width %d at %d columns, so this test is not exercising the layout it was written for",
			a.regions().Room(), narrowColumns)
	}
	frame := stripANSI(a.View())
	if !strings.Contains(frame, text) {
		t.Errorf("the question is on no surface at %d columns - the room is not drawn and the conversation drew none:\n%s",
			narrowColumns, frame)
	}
}

// And it is answerable there. Drawing it without binding the keys to it would
// be the worse half of the same bug: a card that looks answerable and is not.
func TestAQuestionIsAnswerableInTheConversationItWasAskedIn(t *testing.T) {
	a, _ := asking(t, narrowColumns)

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{cardFirstOption}})
	card, ok := a.cards.For("s1")
	if !ok {
		t.Fatal("the ask is no longer open, so there is nothing to have answered")
	}
	if got := card.chosen(0); got != 0 {
		t.Errorf("the first digit chose option %d, want 0: the card's keys are dead in the pane drawing it", got)
	}
}

// The keys answer the card the focused pane draws, never the fleet's oldest.
//
// Two agents blocked and the conversation open on the second, which is the only
// arrangement that tells the two apart: Cards.Top is the first agent's ask and
// the pane is putting the second's. A key read against Top would answer an
// agent the operator is not looking at, on a card they never saw - the accident
// the arm-and-confirm exists to prevent, arriving through the wrong card
// instead of the wrong keystroke.
func TestTheKeysAnswerTheCardTheFocusedPaneDraws(t *testing.T) {
	ev := questionAsk(t)
	a := newRoomApp(t).withSize(narrowColumns, 40).withAgents("john", "sydney")
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: ev})
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: ev})
	a = pick(a, "s2").openDMWith("s2", "sydney").applyGeometry()

	if top, _ := a.cards.Top(); top.AgentID != "s1" {
		t.Fatalf("the oldest ask belongs to %q, want s1: this test is not exercising the difference", top.AgentID)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{cardFirstOption}})

	focused, ok := a.cards.For("s2")
	if !ok {
		t.Fatal("the focused conversation's ask is gone")
	}
	if got := focused.chosen(0); got != 0 {
		t.Errorf("the key left the focused conversation's own question at %d, want the chosen 0", got)
	}
	oldest, ok := a.cards.For("s1")
	if !ok {
		t.Fatal("the other agent's ask is gone")
	}
	if got := oldest.chosen(0); got != noChoice {
		t.Errorf("the key answered %q instead - an agent whose card is on no pane the operator is looking at", oldest.AgentID)
	}
}

// A pinned card does not move where a selection starts, because it is drawn
// above the *composer* now (see App.menuBlock), not above the transcript. The
// transcript is the pane's first row whether an agent is blocked or not, so a
// drag lands on the same line either way.
//
// It did not, while the card went up through paneChrome: startSelection offset a
// drag's first line by the card's height, so every drag in a pane jumped the
// moment the agent asked - which is exactly when somebody is reading that pane to
// work out what it is asking. Below the transcript the card is not among the rows
// above it, so the offset is a plain `top`.
func TestAPinnedCardDoesNotMoveWhereASelectionStarts(t *testing.T) {
	a, _ := asking(t, narrowColumns)
	card, ok := a.cards.For("s1")
	if !ok {
		t.Fatal("the conversation is pinning no ask")
	}

	// Row 3 is transcript in both cases now - the card is pinned down by the
	// composer, not up over the banner.
	pinned, _ := drag(a, 2, 10, 3)
	if pinned.sel.empty() {
		t.Fatal("the drag selected nothing, so there is no first line to compare")
	}

	// The same pane with the ask settled, so the comparison is against the card
	// this build actually draws rather than a number written here.
	bare := a
	bare.cards = bare.cards.Settle("s1", card.RequestID)
	bare, _ = drag(bare, 2, 10, 3)

	if pinned.selTop != bare.selTop {
		t.Errorf("a pinned card moved the selection's first line from %d to %d: the card is drawn above the "+
			"composer now, not above the transcript, so it must not offset a drag", bare.selTop, pinned.selTop)
	}
}

// The reported symptom, end to end: an agent that put a question in a
// conversation goes silent, and everything typed at it afterwards queues behind
// an ask it is still waiting on. Nothing on the far side times out - the corpus
// records one blocked 342 seconds with zero bytes out - so the only thing that
// ends it is an answer, and until the card reached this pane there was no
// surface to give one on.
//
// Walked with ↑↓ and ↵ rather than with digits, so what is covered is the route
// a reader of claude's own question screen would take - through the review
// step the last ↵ lands on, which is where a question is settled now. The arm
// that used to end this is ShapePermission's and ShapePlan's; see
// cardsteps.go on why a question earns a stronger guard than it.
func TestAnsweringInTheConversationSendsTheFrameThatUnblocksTheAgent(t *testing.T) {
	a, _ := asking(t, narrowColumns)

	card, ok := a.cards.For("s1")
	if !ok || card.Detail == nil {
		t.Fatal("the conversation is putting no answerable question")
	}
	asked := len(card.Detail.Questions)
	if asked < 2 {
		t.Fatalf("the recorded ask puts %d questions: this test is meant to walk more than one", asked)
	}
	for range asked {
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	}

	if !topCard(t, a).OnReview() {
		t.Fatalf("answering every question left the cursor on step %d rather than the review, so there is nothing to submit", topCard(t, a).Cursor)
	}
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameAnswer {
		t.Errorf("the conversation settled the question with a %q frame, want %q: a bare allow runs the tool and tells the model nobody replied, on a turn that still ends successfully", f.Kind, rpc.FrameAnswer)
	}
	if f.SessionID != "s1" {
		t.Errorf("the answer is addressed to %q, want the agent whose conversation put the question", f.SessionID)
	}
	if len(f.Answers) != asked {
		t.Errorf("the answer carries %d choices for %d questions, which is the shape core.EncodeAnswer refuses - and a refused answer leaves the agent exactly as blocked as it was", len(f.Answers), asked)
	}
	if a.cards.Len() != 0 {
		t.Errorf("%d asks are still open after the answer went out", a.cards.Len())
	}
}

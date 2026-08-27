package ui

// What the card's key line advertises, and when it may not.
//
// Two rules from the undo re-scoping (docs/notes/deferred.md, 2026-08-12):
// the key that destroys an ask is drawn beside the keys that answer it, and a
// line may not advertise keys that are not being read - which is what the
// bracketed labels were doing whenever the composer held a draft.

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// askKinds is every shape an ask can take, read from core so a fourth kind
// reaches these tests without anybody restating the list.
func askKinds(t *testing.T) map[string]string {
	t.Helper()
	kinds := constValuesOfType(t, filepath.Join("..", "core", "event.go"), "AskKind")
	if len(kinds) < 3 {
		t.Fatalf("found %d AskKind constants in core: the scan is broken and this test is asserting nothing", len(kinds))
	}
	return kinds
}

// TestTheCardOffersTheInterruptBesideItsKeys is the re-scoped undo item's own
// shape: ⎋ destroys an outstanding ask and a billed turn, it is gated on
// nothing, and a two-press ⎋ was rejected for urgency - so the card draws the
// key beside the keys that answer it, spelled the way the legend spells it.
func TestTheCardOffersTheInterruptBesideItsKeys(t *testing.T) {
	spelled := escGlyph + " " + escInterruptLabel
	for name, value := range askKinds(t) {
		card := answerableCard(core.AskKind(value))
		line := oneCard(card).keyLine(card, wideRoom, false)
		// The suffix, not merely present: the interrupt is the line's last
		// clause because it is the first dropped, and a reordering that put it
		// mid-line survived every Contains in this file.
		if !strings.HasSuffix(line, spelled) {
			t.Errorf("core.%s draws %q: the key that destroys this ask is not the last clause beside the keys that answer it", name, line)
		}
	}
}

// The interrupt is the most droppable clause on the line: the legend under the
// composer already carries `esc interrupt` on every pane, where the digits and
// the refusal exist nowhere else. So a width that fits the answer keys and not
// the interrupt keeps the answer keys - and on a question, the move keys
// survive before the interrupt does, for the same second-route reason.
func TestTheInterruptIsDroppedBeforeTheKeysThatAnswer(t *testing.T) {
	card := answerableCard(core.AskPermission)
	base := cardAllowKeys + cardDot + cardConfirmHint
	line := oneCard(card).keyLine(card, ansi.StringWidth(base), false)
	if strings.Contains(line, cardInterruptHint) {
		t.Errorf("at a width that exactly fits the answer keys, the line %q still carries the interrupt", line)
	}
	if !strings.Contains(line, cardAllowLabel) || !strings.Contains(line, cardDenyLabel) {
		t.Errorf("the interrupt cost the line its answer keys: %q", line)
	}

	q := answerableCard(core.AskChoice)
	keys := q.questionKeys(wideRoom)
	if !strings.Contains(keys, cardMoveKeys) {
		t.Fatalf("the wide question line %q has no move keys: this test cannot see the drop order", keys)
	}
	tight := ansi.StringWidth(keys + cardDot + cardConfirmHint)
	got := oneCard(q).keyLine(q, tight, false)
	if !strings.Contains(got, cardMoveKeys) {
		t.Errorf("at %d columns the question line %q dropped its move keys to admit the interrupt", tight, got)
	}
	if strings.Contains(got, cardInterruptHint) {
		t.Errorf("at %d columns the question line %q carries the interrupt beyond the width that fits it", tight, got)
	}
}

// While the composer holds a draft, no card key is read at all - cardKey's
// first gate - so a line still advertising [a]llow and [d]eny is the legend
// rule broken on the one surface the legend guard cannot see. The card says
// the keys are waiting instead, and says what brings them back.
func TestTheKeyLineSaysTheKeysWaitWhileADraftIsTyped(t *testing.T) {
	a := blockedPane(t)
	// Before the press, or the assertion is about nothing: the composer's
	// textarea is shared by pointer, so the pre-press App must be read first.
	if !strings.Contains(shown(a), cardAllowLabel) {
		t.Fatalf("with an empty composer the card does not advertise its keys, so this test cannot see them pause:\n%s", shown(a))
	}
	typed, _ := press(a, 'x')
	out := shown(typed)
	if !strings.Contains(out, cardKeysPaused) {
		t.Errorf("with a draft in the composer the card does not say its keys are paused:\n%s", out)
	}
	for _, label := range []string{cardAllowLabel, cardDenyLabel} {
		if strings.Contains(out, label) {
			t.Errorf("with a draft in the composer the card still advertises %q, which no key path reads:\n%s", label, out)
		}
	}
}

// The interrupt survives at exactly the width that fits it and goes at one
// less - the boundary pinned because a mutant flipping <= to < survived the
// whole suite: every earlier width in these tests was either roomy or tight,
// never exact.
func TestTheInterruptFitsAtExactlyItsOwnWidth(t *testing.T) {
	card := answerableCard(core.AskPermission)
	exact := ansi.StringWidth(cardAllowKeys + cardDot + cardConfirmHint + cardDot + cardInterruptHint)
	if got := oneCard(card).keyLine(card, exact, false); !strings.Contains(got, cardInterruptHint) {
		t.Errorf("at exactly %d columns the line %q dropped an interrupt that fits", exact, got)
	}
	if got := oneCard(card).keyLine(card, exact-1, false); strings.Contains(got, cardInterruptHint) {
		t.Errorf("at %d columns the line %q keeps an interrupt one column too wide", exact-1, got)
	}
}

// The pause is the focused pane's own: cardKey reads a.cardOf(a.focus), so a
// draft gates exactly that pane's card and no other.
//
// Two conversations open, because that is now the only arrangement with two
// cards on screen: the room draws none, so a room-and-one-DM fixture has one
// card and cannot tell a per-pane gate from a global one.
func TestADraftPausesOnlyTheFocusedPanesCard(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateBlocked},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateBlocked},
	)
	a = a.openDMWith("s1", "alex").openRight("s2", "sydney").opened(t)
	a = a.applyFrame(askFrame("s1", "r1")).applyFrame(askFrame("s2", "r2"))

	if out := shown(a); strings.Count(out, cardAllowLabel) != 2 {
		t.Fatalf("two blocked agents with both conversations open put %d cards up, want two:\n%s", strings.Count(out, cardAllowLabel), out)
	}
	if out := shown(a); strings.Contains(out, cardKeysPaused) {
		t.Fatalf("with no draft anywhere a card is already paused:\n%s", out)
	}
	typed, _ := press(a, 'x')
	out := shown(typed)
	if n := strings.Count(out, cardKeysPaused); n != 1 {
		t.Errorf("a draft in the focused conversation paused %d cards, want exactly its own:\n%s", n, out)
	}
	// The other conversation still advertises its keys: its card was not gated.
	if !strings.Contains(out, cardAllowLabel) {
		t.Errorf("a card stopped advertising keys over a draft that is not in its own composer:\n%s", out)
	}
}

// askFrame is one permission ask on one agent, for tests that need two.
func askFrame(sessionID, requestID string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: sessionID, Event: &core.Event{
		Kind: core.KindPermissionRequest, RequestID: requestID,
		Tool: &core.ToolCall{Name: "Bash", Display: "make test"},
	}}
}

// The esc the card advertises acts on the card it is printed on, and a card is
// printed in its own agent's conversation. interruptTarget used to read
// Cards.Top here: with two agents blocked and the *younger* one's conversation
// open, those name different agents and the hint destroyed an ask the operator
// was not looking at. Only two blocked agents with one conversation open can
// tell the versions apart.
func TestEscInAConversationInterruptsTheAgentWhoseCardItDraws(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "john", State: rpc.StateBlocked},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateBlocked},
	)
	a = a.applyFrame(askFrame("s1", "r1")).applyFrame(askFrame("s2", "r2"))
	a = a.openDMWith("s2", "sydney").opened(t)

	if top, _ := a.cards.Top(); top.AgentID != "s1" {
		t.Fatalf("the oldest ask belongs to %q, want s1: this test is not exercising the difference", top.AgentID)
	}
	card, ok := a.cardOf(a.focus)
	if !ok || card.AgentID != "s2" {
		t.Fatalf("the focused conversation draws %q's card, want s2: this test is not exercising the difference", card.AgentID)
	}

	next, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	// The write rides inside Update's history-ask batch here - the opened
	// conversation's ask has no daemon to answer it - so every member runs:
	// HANDOFF's own trap, a tea.Batch under a harness that runs one command.
	runBatch(t, cmd)
	frames := recorderOf(t, next).taken(t)
	var interrupts []rpc.Frame
	for _, f := range frames {
		if f.Kind == rpc.FrameInterrupt {
			interrupts = append(interrupts, f)
		}
	}
	if len(interrupts) != 1 {
		t.Fatalf("esc wrote %d interrupts among %v, want one", len(interrupts), frames)
	}
	if interrupts[0].SessionID != "s2" {
		t.Errorf("esc interrupted %q - an agent whose card this pane is not drawing - want s2", interrupts[0].SessionID)
	}
}

// runBatch runs a command and every member of any batch it returns, so a write
// sharing an Update with a history ask still reaches the recorder.
func runBatch(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command: nothing was sent")
	}
	switch msg := cmd().(type) {
	case errMsg:
		t.Fatalf("the write failed: %v", msg.Err)
	case tea.BatchMsg:
		for _, member := range msg {
			if member != nil {
				runBatch(t, member)
			}
		}
	}
}

// The paused line brackets nothing, or TestEveryRuneTheCardBracketsIsOneCardKeysBinds
// would demand a binding for a line whose whole point is that no key is live.
func TestThePausedLineAdvertisesNoKey(t *testing.T) {
	if got := bracketed(cardKeysPaused); len(got) != 0 {
		t.Errorf("the paused key line brackets %q while advertising that no key is read", got)
	}
	if got := bracketed(cardInterruptHint); len(got) != 0 {
		t.Errorf("the interrupt hint brackets %q, which the bijection guard reads as a bound rune", got)
	}
}

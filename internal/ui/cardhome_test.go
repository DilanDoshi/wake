package ui

// Where an ask is drawn: its agent's own conversation, and nowhere else.
//
// The room used to take the oldest ask whose agent had no pane on screen, so
// walking out of a conversation moved a question somebody was mid-answer into
// the group chat. That surface is wrong for it twice over. It holds one card
// and counts the rest, so a fleet with several agents blocked at once - the
// case this build exists for - shows one of them and a number; and it puts a
// question about one agent's work under thirty agents' interleaved lines,
// stripped of the turn that raised it.
//
// So the room draws none, and what says somebody is waiting is what already
// said it: the roster row, the awareness strip's "N need you", and ⌃X, which
// opens the next blocked agent's conversation - where the card is.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// twoBlocked is two agents blocked on a tool each, with no conversation open.
// askFrame and askOn ask about different tools, which is what these assertions
// need: they are about *which* card is on a surface, and one repeated headline
// cannot say.
func twoBlocked(t *testing.T) App {
	t.Helper()
	return newRoomApp(t).withSize(220, 44).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "iris", State: rpc.StateBlocked},
		rpc.SessionStatus{ID: "s2", Name: "john", State: rpc.StateBlocked},
	).applyGeometry().applyFrame(askFrame("s1", "r1")).applyFrame(askOn("s2", "r2"))
}

// headlineFor is the line a card leads with, and it is card-only text: a pane's
// transcript draws the tool call too, so the tool's *argument* on screen says
// nothing about whether a card is up.
//
// **It is no longer a card-vs-room discriminator, and the guards below stopped
// using it as one.** The room *announces* a blocked agent - a line in its own
// transcript, carrying no keys - while the pane *offers* the ask. Two halves of
// one report, and the answer to a group chat that went silent about a stopped
// agent whenever a DM was open. But it puts these same words on screen twice,
// so "the headline appears exactly once" fails on a build where both halves are
// working.
//
// What replaced it is App.cardOf, which is the property these tests were always
// about: the room offers no ask, and each pane offers its own. The word count
// was a proxy for that. The cost of the swap, stated rather than buried: a
// stray second card *body* drawn somewhere unexpected would no longer trip a
// count - cardOf answers who owns an ask, not how many times it got painted.
func headlineFor(tool string) string { return "wants " + tool }

// The report, in the owner's words: leave iris's conversation and her question
// turns up in the group chat. Here john's is the pane on screen, so iris has
// none - which is exactly the state the old rule promoted her card to the room
// in.
func TestAnAskDoesNotMoveToTheRoomWhenItsConversationIsClosed(t *testing.T) {
	a := twoBlocked(t).openDMWith("s2", "john").applyGeometry()

	if _, ok := a.cardOf(""); ok {
		t.Error("the room is putting an ask: a question belongs to the conversation that raised it")
	}
}

// And with nothing open at all the room is still not the place: the roster and
// the strip say who is waiting, and ⌃X is how you get to them.
func TestTheRoomAloneDrawsNoAsk(t *testing.T) {
	a := twoBlocked(t)

	if _, ok := a.cardOf(""); ok {
		t.Error("the room is putting an ask with no conversation open")
	}
}

// The keeper, and the half the report is really about: a conversation that is
// *drawn* keeps its ask whether or not it holds the keys. Clicking into
// another pane moves the keys and must move nothing else.
func TestADrawnConversationKeepsItsAskWhenTheKeysLeave(t *testing.T) {
	a := twoBlocked(t).openDMWith("s1", "iris").applyGeometry().withFocus("")

	if a.focus != "" {
		t.Fatal("the keys are still in iris's pane: this test is not exercising the difference")
	}
	card, ok := a.cardOf("s1")
	if !ok || card.AgentID != "s1" {
		t.Fatal("iris's drawn conversation stopped putting her ask once the keys left it")
	}
	if room, ok := a.cardOf(""); ok {
		t.Errorf("the room is putting %s's ask while it holds the keys: an ask is drawn in its agent's conversation and nowhere else", room.AgentID)
	}
	if out := shown(a); !strings.Contains(out, headlineFor("Bash")) {
		t.Errorf("iris's ask is on no surface while her conversation is drawn:\n%s", out)
	}
}

// Two agents blocked at once is the case the room could never draw: it has one
// card's worth of rows. Both conversations open, both asks up, each in its own
// pane - which is the whole reason for the rule.
func TestTwoBlockedAgentsEachDrawTheirOwnAsk(t *testing.T) {
	a := twoBlocked(t).openDMWith("s1", "iris").openRight("s2", "john").applyGeometry()

	for _, want := range []struct{ id, tool string }{{"s1", "Bash"}, {"s2", "Write"}} {
		card, ok := a.cardOf(want.id)
		if !ok || card.Tool != want.tool {
			t.Errorf("%s's pane is putting %q, want its own %q", want.id, card.Tool, want.tool)
		}
	}
	// And no third copy: the room offers neither of them.
	if _, ok := a.cardOf(""); ok {
		t.Error("the room is putting an ask as well as the two panes")
	}
}

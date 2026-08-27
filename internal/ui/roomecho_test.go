package ui

// A message routed from the room lands in the addressed agent's conversation
// too, still carrying the mention that says where it came from.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
)

// roomWithHeldDMs is the room with three agents, some of their conversations
// held, and the keys back on the room - which is where a routed message is
// typed.
func roomWithHeldDMs(t *testing.T, held ...string) App {
	t.Helper()
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john", "marcus")
	for _, id := range held {
		a = a.openDMWith(id, a.agentName(id))
	}
	return a.refocus("")
}

// dmEvents is one held conversation's transcript.
func dmEvents(a App, id string) []core.Event {
	dm, ok := a.dms[id]
	if !ok {
		return nil
	}
	return dm.events.slice(0, dm.events.len())
}

// userTurns is the operator's own turns in one conversation, which is the half
// this feature is about.
func userTurns(a App, id string) []core.Event {
	var out []core.Event
	for _, ev := range dmEvents(a, id) {
		if ev.Kind == core.KindUserText {
			out = append(out, ev)
		}
	}
	return out
}

// The message you type in the room shows up in the conversation it was
// addressed to, spelled the way you typed it.
//
// The mention is the point. What reaches the agent has it stripped - Claude
// Code expands a leading @name before the model sees it - so the stripped text
// is what the wire carries and cannot be what the pane shows: read back later,
// a bare "ship it" in john's conversation is indistinguishable from one typed
// into john's composer, and those are different acts.
func TestARoomMessageLandsInTheAddressedAgentsHeldDM(t *testing.T) {
	a := roomWithHeldDMs(t, "s2")
	a, _ = pressKey(a.withDraft("@john ship it"), tea.KeyMsg{Type: tea.KeyEnter})

	turns := userTurns(a, "s2")
	if len(turns) != 1 {
		t.Fatalf("john's conversation holds %d of your turns, want the one just addressed to him: %+v", len(turns), dmEvents(a, "s2"))
	}
	if got := turns[0].Text; got != "@john ship it" {
		t.Errorf("john's conversation shows %q, want %q - the mention is what says this came from the room rather than from his own composer", got, "@john ship it")
	}
}

// The turn carries where it came from, so the pane can label it rather than
// leave a reader inferring origin from an @.
//
// The inference does not work: in a conversation what you type is sent
// verbatim, so `@john ship it` typed into john's own composer is a different
// message from the same words typed in the room, and the two would draw
// identically.
func TestARoomMessageIsMarkedAsComingFromTheRoom(t *testing.T) {
	a := roomWithHeldDMs(t, "s2")
	a, _ = pressKey(a.withDraft("@john ship it"), tea.KeyMsg{Type: tea.KeyEnter})

	turns := userTurns(a, "s2")
	if len(turns) != 1 {
		t.Fatalf("john's conversation holds %d of your turns, want 1", len(turns))
	}
	if !turns[0].FromRoom {
		t.Error("the turn is not marked as coming from the room, so the pane cannot tell it from one typed into this composer - and those two send the agent different text")
	}
}

// And a turn typed into the conversation itself is not marked, or every turn
// in it claims an origin it does not have.
func TestATurnTypedIntoAConversationIsNotMarkedAsFromTheRoom(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john", "marcus")
	a = a.openDMWith("s2", "john")
	a, _ = pressKey(a.withDraft("ship it"), tea.KeyMsg{Type: tea.KeyEnter})

	turns := userTurns(a, "s2")
	if len(turns) != 1 {
		t.Fatalf("john's conversation holds %d of your turns, want the one just typed into it", len(turns))
	}
	if turns[0].FromRoom {
		t.Error("a turn typed into the conversation is marked as coming from the room: the label would then be on every turn and say nothing")
	}
}

// A broadcast is in every conversation it reached, and still one thing you
// said in the room.
//
// Both halves matter and they pull opposite ways: a DM that a broadcast is
// missing from shows an agent answering something that pane never mentions,
// and a room that drew one echo per target would be reporting its own postage.
func TestABroadcastReachesEveryHeldConversationAndEchoesOnceInTheRoom(t *testing.T) {
	a := roomWithHeldDMs(t, "s1", "s3")
	a, _ = pressKey(a.withDraft("@all ship it"), tea.KeyMsg{Type: tea.KeyEnter})

	for _, id := range []string{"s1", "s3"} {
		turns := userTurns(a, id)
		if len(turns) != 1 {
			t.Errorf("%s's conversation holds %d of your turns, want the broadcast it was sent", id, len(turns))
			continue
		}
		if got := turns[0].Text; got != "@all ship it" {
			t.Errorf("%s's conversation shows %q, want %q", id, got, "@all ship it")
		}
	}
	// Counted in the room's own transcript rather than on the frame. The frame
	// now holds the message once per pane drawing it, which is the feature -
	// counting there would make this test fail for the reason it is meant to
	// pass.
	if n := roomTurns(a); n != 1 {
		t.Errorf("the room holds your broadcast %d times, want 1 - one broadcast is one thing you said, and thirty copies would be the room reporting its own postage", n)
	}
}

// roomTurns is how many of the operator's own turns the room transcript holds.
func roomTurns(a App) int {
	n := 0
	for _, line := range a.room.said.slice(0, a.room.said.len()) {
		if line.ev.Kind == core.KindUserText {
			n++
		}
	}
	return n
}

// A conversation nobody opened is left alone, and this is the load-bearing
// half of the rule rather than a saving.
//
// Opening one reads claude's transcript, which holds this turn too - as the
// agent received it, without the mention. Writing it here as well would put
// the same turn on screen twice, once in each spelling, which is the
// single-source rule breaking in the one place nothing would catch it.
func TestARoomMessageDoesNotBuildAConversationNobodyOpened(t *testing.T) {
	a := roomWithHeldDMs(t)
	before := len(a.dms)

	a, _ = pressKey(a.withDraft("@all ship it"), tea.KeyMsg{Type: tea.KeyEnter})

	if got := len(a.dms); got != before {
		t.Errorf("a broadcast built %d conversations nobody had opened. Each one duplicates a turn claude's own transcript already holds, the moment it is opened", got-before)
	}
}

// The unaddressed draft is routed too - to the manager - so it lands in the
// manager's conversation for the same reason a mention lands in john's.
func TestAnUnaddressedRoomMessageLandsInTheManagersHeldConversation(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents(core.ManagerName, "john")
	a = a.openDMWith("s1", core.ManagerName).refocus("")
	a, _ = pressKey(a.withDraft("ship it"), tea.KeyMsg{Type: tea.KeyEnter})

	turns := userTurns(a, "s1")
	if len(turns) != 1 {
		t.Fatalf("the manager's conversation holds %d of your turns, want the one the room routed to it: %+v", len(turns), dmEvents(a, "s1"))
	}
	if !turns[0].FromRoom {
		t.Error("the manager's copy is not marked as coming from the room, and it is the case with no mention to infer it from")
	}
}

// A message routed into a conversation you had left lands **below** the
// last-read rule, which is where the thing that explains the reply belongs.
//
// It is a consequence rather than a decision: DM.Append draws the boundary
// before the first event to arrive since somebody left, and until now nothing
// could arrive in a conversation the operator was not in except the agent's own
// output. Pinned because the alternative reads wrong in a way nobody would
// notice for a while - your own message above the rule claims you already read
// it here, and you did not, you typed it in the room.
func TestARoomMessageLandsBelowTheRuleInAConversationYouLeft(t *testing.T) {
	a := roomWithHeldDMs(t, "s2")
	a = said(a, "s2", "standing by") // something to have read before leaving
	a = a.withDM("s2", a.dms["s2"].Leave())
	a, _ = pressKey(a.withDraft("@john ship it"), tea.KeyMsg{Type: tea.KeyEnter})

	lines := dmLines(t, a, "s2")
	rules := boundaries(lines)
	if len(rules) != 1 {
		t.Fatalf("the conversation drew %d last-read rules, want 1:\n%s", len(rules), strings.Join(lines, "\n"))
	}
	if at := lineWith(lines, "@john ship it"); at < rules[0] {
		t.Errorf("your routed message is on line %d and the rule is on %d, so the message sits above it - which says you already read it here, and you typed it in the room:\n%s", at, rules[0], strings.Join(lines, "\n"))
	}
}

// The pane says so, in the one place a reader is already looking for who is
// speaking.
func TestARoomTurnIsHeadedWithItsOrigin(t *testing.T) {
	d := NewDM("s2", "john").SetSize(subagentTestWidth, 40)
	out := stripANSI(d.eventBlock(core.Event{
		Kind:     core.KindUserText,
		Text:     "@john ship it",
		FromRoom: true,
	}))

	if !strings.Contains(out, roomTurnLabel) {
		t.Errorf("a turn routed from the room is headed as an ordinary one, so nothing on screen separates it from a turn typed here:\n%s", out)
	}
	if !strings.Contains(out, "@john ship it") {
		t.Errorf("the label replaced the message instead of heading it:\n%s", out)
	}
}

// And an ordinary turn keeps the ordinary head, or the label is on everything
// and distinguishes nothing.
func TestATurnTypedHereKeepsTheOrdinaryHead(t *testing.T) {
	d := NewDM("s2", "john").SetSize(subagentTestWidth, 40)
	out := stripANSI(d.eventBlock(core.Event{Kind: core.KindUserText, Text: "ship it"}))

	if strings.Contains(out, roomTurnLabel) {
		t.Errorf("a turn typed into this conversation is headed as one from the room:\n%s", out)
	}
	if !strings.Contains(out, userLabel) {
		t.Errorf("a turn typed into this conversation lost its head:\n%s", out)
	}
}

// And the agent still receives it stripped, which is the divergence that makes
// the echo necessary rather than decorative.
func TestTheAgentStillReceivesTheRoomMessageWithoutTheMention(t *testing.T) {
	a := roomWithHeldDMs(t, "s2")
	a, cmd := pressKey(a.withDraft("@john ship it"), tea.KeyMsg{Type: tea.KeyEnter})

	if got := sentFrame(t, a, cmd).Text; got != "ship it" {
		t.Errorf("the daemon was sent %q, want %q: a leading @name is expanded by Claude Code before the model sees it, so it is stripped on the wire and kept on screen", got, "ship it")
	}
}

//go:build unix

// The features CLAUDE.md calls built, driven from a keyboard and checked on the
// screen. One test per claim: a claim nothing renders is a claim nobody has
// seen.
//
// These run at 100x30, where the conversation takes the whole pane and the room
// is one ⌃W away - so both panes are reachable and the handle is on row 0.

package main

import (
	"strings"
	"testing"
)

// ⌃W closes the conversation and the room takes the keys.
func TestClosingTheConversationLeavesTheRoomTypeable(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("\x17") // ⌃W
	s.await("group chat")

	s.send("typed into the room")
	s.await("typed into the room")
}

// /new starts an agent and the activity sidebar lists it.
func TestSlashNewStartsAnAgentTheRosterShows(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("\x17") // ⌃W, so the draft reaches the room
	s.await("group chat")
	s.send("/new robin\r")
	s.await("robin")

	// Read off the sidebar rather than off the screen: "robin" is in the room's
	// own text too, so the whole claim in this test's name is the roster's rows.
	s.settle()
	if names := s.rosterNames(); !strings.Contains(strings.Join(names, " "), "robin") {
		t.Fatalf("the activity sidebar lists %v, not the agent /new started.\n%s", names, s.dump())
	}
}

// /name changes the handle the room routes on.
func TestSlashNameChangesTheHandle(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("/name quill\r")
	s.await("@quill")
}

// /task says what an agent is working on.
func TestSlashTaskRelabelsTheAgent(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.send("/task @" + name + " shipping the room\r")

	// The label is drawn beside the handle on a room line, so it takes a line
	// after the relabel to be visible at all. Nothing shows it for an agent
	// that has not spoken - deferred.md carries that gap, and it is the
	// roster's to answer.
	s.send("@" + name + " hello\r")
	s.await(name + " <> shipping the room")
}

// !cmd runs a shell line and puts its output in the conversation.
func TestABangLineRunsAShellCommand(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("!echo bang-ran-here\r")
	s.await("bang-ran-here")
}

// The composer says where ↵ will send before the key is pressed.
func TestTheComposerNamesWhereEnterWillSend(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.send("@" + name + " hello")
	s.await("→ @" + name)
}

// ⌃C parks the agent and /resume brings it back.
func TestParkingAnAgentAndWakingItAgain(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x03") // ⌃C
	s.await("parked")

	s.send("\x17") // ⌃W, so /resume reaches the room
	s.await("group chat")
	s.send("/resume " + name + "\r")
	// Awaited rather than settled for: the wake is a verb still in flight, and
	// the frame goes quiet with `! bringing @name back…` under a strip still
	// drawn from the report before it. See screen.awaitGone.
	s.awaitGone("parked")
}

// A conversation held in a DM stays in the DM. The room is for what needs the
// operator or names them - that is the whole filter the product rests on.
func TestADMStaysOutOfTheRoom(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("a question asked in private\r")
	s.await(heardPrefix + "a question asked in private")

	s.send("\x17") // ⌃W: the room, with the DM shut
	s.await("group chat")
	s.settle()
	if strings.Contains(s.text(), heardPrefix) {
		t.Fatalf("the agent's DM reply is in the group chat.\n%s", s.dump())
	}
}

// The mirror of the rule above, and it is what stops the fix being "the room
// draws nothing": a turn addressed from the room is answered in the room.
func TestARoomTurnIsDrawnInTheRoom(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.send("@" + name + " a question asked in public\r")
	s.await(heardPrefix + "a question asked in public")
}

// A blocked agent raises a card in its own conversation, and settling it takes
// the key and then ↵.
//
// No ⌃W. It used to close the conversation first, because the card was the
// room's and these run at 100 columns - under dmTakeoverColumns, where a
// focused conversation leaves the room with no width at all. The room draws no
// card now, so the conversation is the whole surface.
func TestABlockedAgentRaisesAnAnswerableCard(t *testing.T) {
	withScriptedAgent(t, scriptAsks)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("write the file\r")

	s.await(askedTool)
	s.await("[a]llow")

	s.send("a")
	s.await("cannot be undone")

	s.send("\r")
	s.await(heardPrefix + "answered")
}

// And closing the conversation does not hand the card to the room, which is the
// bug this rule was written for: an ask somebody was mid-answer moved into the
// group chat the moment they looked at another agent. The roster still says who
// is waiting, and ⌃X is what opens them.
func TestClosingAConversationDoesNotMoveItsCardToTheRoom(t *testing.T) {
	withScriptedAgent(t, scriptAsks)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("write the file\r")
	s.await("[a]llow")

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.settle()

	if strings.Contains(s.text(), "[a]llow") {
		t.Errorf("the room took the card of an agent whose conversation was closed:\n%s", s.dump())
	}
}

// A question is answerable in the conversation that put it.
//
// It exists because the card was once room-only, and these run at 100 columns -
// under dmTakeoverColumns, so a focused conversation leaves the room with no
// width at all. An ask put in a conversation was therefore on no surface
// anywhere, with its keys dead as well, and the agent stayed blocked with
// nothing on screen to answer. The card has since gone the other way and lives
// only in the conversation, which is what the test above now says for a
// permission ask; this one is the question shape, whose keys are a chip, the
// digits and the answer key rather than a bare allow.
//
// Mutation check: making App.cardOf return nothing for a conversation fails
// here at the chip. Reverting cardKey to read Cards.Top does *not* fail this
// test and is not claimed to - one agent with one ask makes the two the same
// card. That property needs two blocked agents and is held by
// TestTheKeysAnswerTheCardTheFocusedPaneDraws in internal/ui.
func TestAQuestionIsAnsweredInTheConversationThatPutIt(t *testing.T) {
	withScriptedAgent(t, scriptQuestions)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("which format?\r")

	// Every part of the card, in the pane that put the question.
	s.await(questionChip)
	s.await(questionText)
	s.await(questionOptionA)
	s.await(questionOptionB)
	s.await(questionSample)

	// ↵ chooses the cursored option and advances a step. With one question
	// asked, that step is the review: every answer laid out before the press
	// that sends it, which is what a question ask has instead of an arm.
	s.send("\r")
	s.await("Review your answers")
	s.await("Submit answers")

	// ↵ on Submit is the press. There is no second key: the review is what the
	// arm used to be, and it shows what it is about to send.
	s.send("\r")
	s.await(heardPrefix + "answered")
}

// A question answered in the operator's own words, end to end on a terminal.
//
// The whole wizard: the framed card, the Other row past the options the model
// supplied, the composer titled for what it is holding, and the review naming
// the typed answer back before the one press that sends it. In process each of
// those is a string comparison; only a real screen can say they are on it
// together and in that order.
func TestAQuestionIsAnsweredInTheOperatorsOwnWords(t *testing.T) {
	withScriptedAgent(t, scriptQuestions)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 110, 30)
	s.await("ready")
	s.send("which format?\r")
	s.await(questionOptionA)

	// The row past the two options the recording supplies, reached by the digit
	// that is one past them.
	s.await("Other")
	s.send("3")
	s.await("answering")

	const mine = "markdown, but split per region"
	s.send(mine)
	s.await(mine)

	// ↵ takes it as the answer rather than sending it as a message, and the
	// review says so back.
	s.send("\r")
	s.await("Review your answers")
	s.await(mine)
	s.await("Submit answers")

	s.send("\r")
	s.await(heardPrefix + "answered")
}

// ⎋ stops a turn that is in flight, and the conversation says so.
func TestEscapeInterruptsATurn(t *testing.T) {
	withScriptedAgent(t, scriptInterruptible)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("take your time\r")
	s.await(workingMarker)

	s.send("\x1b")
	s.await("turn interrupted")
}

// ⌃F branches the conversation, and the fork says what it inherited.
func TestForkOpensTheForkAndSaysWhatItIs(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x06") // ⌃F
	s.await("is a fork of @" + name)
}

// ⌃T flips what a leading @name means, and the composer says which is on.
func TestMentionModeChangesWhatTheComposerPromises(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.send("@" + name + " ship it")
	s.await("→ @" + name)

	s.send("\x14") // ⌃T
	s.await("open")
}

// ⇥ moves the keys to the next conversation. Below 120 columns the room takes
// the pane, so focusing it is also what closes the DM.
func TestTabMovesTheKeysToTheRoom(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("\t")
	s.await("group chat")
	s.send("typed after tab")
	s.await("typed after tab")
}

// ⌃O leaves the window and the fleet keeps working, which is what the line on
// the way out has to say - and it takes ⌃O and then ↵ to get there.
//
// The first press is the one this asserts hardest, because it is the one a
// Claude Code operator presses by reflex: there, ⌃O expands the tool result the
// transcript just truncated. On a real screen, at real bytes, that press has to
// leave the window standing and *say so where it can still be read* - which is
// the legend, not the notice row a fleet overwrites within seconds.
//
// The double press is sent as one write, which is what terminal auto repeat
// produces. It is the case the same-key confirm shipped with and failed: see
// internal/ui/detach.go.
func TestDetachLeavesTheFleetRunning(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("\x0f") // ⌃O once: armed, not gone
	s.await("⌃O again cancels")
	// The persistent half. The notice above says it once; this is what is still
	// on screen after the fleet has taken that row for something else.
	s.await("↵ detach")
	s.await("⌃O cancel")
	s.settle()
	if strings.Contains(s.text(), "Detached") {
		t.Fatalf("one ⌃O detached. It is Claude Code's expand key, and the reflex it catches costs "+
			"a window with the whole fleet still behind it.\n%s", s.dump())
	}

	s.send("\x0f") // ⌃O again: the cancel, which is what auto repeat produces
	s.await("↵ send")
	s.settle()
	if strings.Contains(s.text(), "Detached") {
		t.Fatalf("a repeated ⌃O detached. Auto repeat and the reflex of pressing a key again because "+
			"nothing happened are the same bytes as intent, so the confirm is a different key.\n%s", s.dump())
	}

	s.send("\x0f\r") // the whole gesture: arm, then confirm
	s.await("Detached")
	s.await("still running")
}

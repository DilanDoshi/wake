//go:build unix

// Reaching an agent that is neither first, nor blocked, nor already open.
//
// Until this, there was no way: Roster.Move had no caller, a click on the
// sidebar was ignored, and ⌃D took whoever attention ranked first. So the
// roster cursor was set only as a side effect of opening a conversation - and
// ⌃C in the room parked whatever that had left behind, on a sidebar that was
// not even on screen.

package main

import (
	"strings"
	"testing"
)

// ⇧↑↓ walks the roster and ⌃D opens what it lands on.
func TestShiftArrowsPickAnAgentAndCtrlDOpensIt(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.send("/new robin\r")
	s.await("@robin")
	s.send("\x17")
	s.await("group chat")

	s.settle()
	names := agentsOnRoster(s)
	if len(names) != 2 {
		t.Fatalf("want two agents on the roster beside the manager, got %v.\n%s", s.rosterNames(), s.dump())
	}

	// The cursor is on names[0] already: `/new` opens no pane and selects
	// nothing (see starts.go's draftMention), so the selection
	// startWakeInAConversation made when it opened the scripted agent's own
	// conversation survives untouched. Walking it is the subject below, so the
	// second one is walked rather than clicked - and the walk is counted off
	// the roster on screen, because the manager is a row between them now.
	s.send("\x04") // ⌃D on the first
	s.await("@" + names[0])

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.pickRoster(names[0], names[1])
	s.send("\x04")
	s.await("@" + names[1])
}

// A click on a roster row opens that agent - which noPaneAdvice has told
// operators to do since Phase 2, while press took only the x coordinate.
func TestClickingARosterRowOpensThatAgent(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("\x17")
	s.await("group chat")
	s.send("/new robin\r")
	s.await("@robin")
	// No ⌃R: the sidebar is open on arrival and nothing here closes it.
	s.send("\x17")
	s.await("group chat")
	s.settle()

	names := agentsOnRoster(s)
	if len(names) != 2 {
		t.Fatalf("want two agents on the roster beside the manager, got %v.\n%s", s.rosterNames(), s.dump())
	}
	s.openAgent(names[1])

	// And the sidebar the click was aimed at is still there. It used to go, so
	// the row somebody clicked and the rows beside it vanished under the pointer
	// - and picking a second agent meant ⌃R first.
	s.settle()
	if got := agentsOnRoster(s); len(got) != 2 {
		t.Fatalf("opening an agent from a click left %v on the roster, want both.\n%s", got, s.dump())
	}
}

// ⌃C from the room refuses when nothing on screen says who it is about.
//
// This is the report from the first live run: "in the group chat view I hit
// ctrl c and it just parked maya". The cursor was left behind by the last
// conversation that had been open, and the sidebar holding it was closed.
func TestCtrlCFromTheRoomRefusesAnAgentYouCannotSee(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x17") // ⌃W: back to the room
	s.await("group chat")
	s.send("\x12") // ⌃R: close it, so nothing on screen says who ⌃C is about
	s.settle()

	s.send("\x03") // ⌃C
	s.await("⌃C parks one conversation")
	s.settle()
	if strings.Contains(s.text(), "parking @"+name) {
		t.Fatalf("⌃C parked an agent nobody chose.\n%s", s.dump())
	}

	// And with the cursor on screen it parks what the cursor is on. Asserted on
	// the roster's parked glyph rather than the notice, which is transient.
	//
	// ⌃R reopens the sidebar rather than ↑, which used to do both. ↑ moves the
	// cursor as well as showing it, and the room seats a manager now - so on a
	// two-row roster it wrapped off this agent onto the service, and ⌃C parked
	// the wrong one. Reopening is the half this case is about.
	s.send("\x12") // ⌃R
	s.settle()
	s.send("\x03")
	s.await("▪ " + name)
}

// The regions are separated by a rule, which is what the spec draws and what
// the build did not have: sidebars were columns of text with whitespace between
// them. Three regions now - the room, the conversation and the activity sidebar
// - since the left workspaces sidebar is hidden (groups.go), so two rules.
func TestTheSidebarsAreSeparatedByARule(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 200, 45)
	s.await("ready")
	s.settle() // every drawn region is up on arrival

	row := s.lines()[3] // a row below the headers, inside every column
	if n := strings.Count(row, "│"); n != 2 {
		t.Fatalf("want two rules between three regions, got %d in %q.\n%s", n, row, s.dump())
	}
}

// ↵ opens the conversation the cursor is on when there is nothing to send,
// which is what ↑↓ leads somebody to try.
func TestEnterOpensTheAgentTheCursorIsOn(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.settle()

	// No ↑ first. The cursor is already on this agent - opening its
	// conversation selected it, and ⌃W only closed the pane - and ↑ wraps, so
	// on a roster that now seats a manager it moved the cursor off the agent
	// this case is about rather than merely revealing it.
	s.send("\r")
	s.await("@" + name)
}

// ⌃C in a conversation parks it, and the pane says so.
//
// It did say so - on the notice row, one line at the bottom of the screen -
// while the pane itself was unchanged: same transcript, same composer, same
// title. So the key read as having done nothing.
func TestParkingFromAConversationSaysSoInThePane(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x03") // ⌃C
	s.await("@" + name + " · parked")
}

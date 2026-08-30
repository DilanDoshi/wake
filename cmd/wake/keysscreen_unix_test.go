//go:build unix

// The rest of the keyboard, and the frame it draws.
//
// featurescreen_unix_test.go covers the composer's own keys; this file is
// everything that moves a pane, a sidebar or the geometry - and the two paths
// that end the window.

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// ⌃D opens a conversation from the room, with the agent attention ranks first.
func TestCtrlDOpensAConversationFromTheRoom(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x17") // ⌃W
	s.await("group chat")

	s.send("\x04") // ⌃D
	s.await("@" + name)
}

// ⇧⇥ jumps to the agent that needs you and opens its conversation.
func TestShiftTabJumpsToTheBlockedAgent(t *testing.T) {
	withScriptedAgent(t, scriptAsks)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.send("@" + name + " write the file\r")
	// The roster is what says an agent is blocked from the room: the card is in
	// that agent's conversation, which is the pane ⌃W just closed. This used to
	// wait for the tool on the card the room drew.
	s.await("● " + name)

	s.send("\x1b[Z") // ⇧⇥
	s.await("@" + name)
}

// ⌃R toggles the activity sidebar, and with it closed only the panes' own rule
// is left. The left workspaces sidebar is hidden for now (groups.go), so ⌃R is
// the one sidebar key.
func TestTheSidebarsToggle(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 200, 45)
	s.await("ready")

	// Named rather than read off the header: at this width the conversation is
	// the right-hand pane, so its handle is not at the start of a row.
	name := "quill"
	s.send("/name " + name + "\r")
	s.await("@" + name)

	// The sidebar is open on arrival - opening a conversation does not take it
	// away - so the first ⌃R is the one that closes it.
	s.await("○ " + name)

	s.send("\x12") // ⌃R off
	s.settle()
	if strings.Contains(s.text(), "○ "+name) {
		t.Fatalf("the activity sidebar is still drawn after ⌃R.\n%s", s.dump())
	}

	s.send("\x12") // ⌃R on again
	s.await("○ " + name)

	s.send("\x12") // ⌃R off: the room and the conversation, with one rule between
	s.settle()
	if n := strings.Count(s.lines()[3], "│"); n != 1 {
		t.Fatalf("want one rule with the sidebar closed, got %d.\n%s", n, s.dump())
	}
}

// @all reaches every live agent and the room echoes what was said once.
func TestBroadcastReachesEveryAgentAndEchoesOnce(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.send("/new robin\r")
	s.await("@robin")

	// /new opens the new agent's conversation and gives it the keys, so getting
	// back to the room is a keystroke rather than an assumption.
	s.send("\x17") // ⌃W
	s.await("group chat")

	s.send("@all stand up")
	s.await("2 turns") // the price is on screen before the key, not after it

	s.send("\r")
	// Both, and exactly both. settle here asserted the count the moment the
	// frame stopped moving, which is after the *first* agent answered - the
	// second was still coming and the test failed with got=1 under a full-
	// package run. See screen.awaitCount and bugs.md BUG-7.
	s.awaitCount(heardPrefix+"stand up", 2)
	// The echo is its own line and keeps the mention, so the room is a record of
	// who each message went to.
	echoes := 0
	for _, line := range s.lines() {
		if strings.HasPrefix(line, "@all stand up") {
			echoes++
		}
	}
	if echoes != 1 {
		t.Fatalf("one broadcast is one thing you said, echoed %d times.\n%s", echoes, s.dump())
	}
}

// Denying a card reaches the agent, and the refusal carries the operator's own
// words.
//
// The reason reaches the model verbatim as the tool result and is the one
// channel for saying what to do instead of retrying the identical call, so `d`
// opens a box for it rather than arming an invisible settle. The two-press
// property is unchanged - `d` then ↵ - and ⎋ is the way out.
func TestACardIsDeniedWithATypedReason(t *testing.T) {
	withScriptedAgent(t, scriptAsks)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	// In the conversation, with no ⌃W: a card is drawn on no other surface.
	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("write the file\r")
	s.await("[d]eny")

	s.send("d")
	s.await("deny reason")
	s.await("cannot be undone")

	// ⎋ is the way out, and it leaves the card up and answerable.
	s.send("\x1b")
	s.await("[d]eny")

	// And again, this time with a reason, which is visible before it is sent.
	s.send("d")
	s.await("deny reason")
	s.send("the file is generated")
	s.await("the file is generated")

	s.send("\r")
	s.await(heardPrefix + "answered")
}

// ⌃Q parks the fleet on the way out, and the next run comes back **empty**.
//
// This is the whole of what ⌃Q means, and it is the bug this branch exists for.
// The daemon used to read the park book back into the fleet before it accepted
// anything, so quitting and starting again handed back the roster somebody had
// just quit - a parked agent's name in the sidebar with its whole conversation
// one keypress away. Now the room opens on nothing and /resume is the only way
// back.
//
// Asserted on a real screen because that is where it was seen and where nothing
// else looks: the rows were in a rendered sidebar and every in-process test in
// this tree was green over them.
func TestQuitParksTheFleetAndTheNextRunComesBackEmpty(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	// A projects tree the daemon and its agent share, plus the opt-in that has
	// the fake agent leave a transcript there, so the spawned session's parked
	// record is offered back to the `/resume all` below: parkedStatuses drops a
	// parked record with no transcript on disk.
	t.Setenv("WAKE_PROJECTS", t.TempDir())
	t.Setenv(fakeTranscriptEnv, "1")

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x11") // ⌃Q
	s.await("Parking")

	again := startWake(t, 100, 30)
	// A durable fact rather than a notice: the legend is drawn on every frame,
	// and the notice row shows only the newest thing said.
	again.await("detach")
	again.settle()

	// Nothing about the parked session is on screen - no roster row, and no
	// offer either. settle() first, because an assertion about absence is worth
	// nothing until the frame has stopped moving.
	if strings.Contains(again.text(), name) {
		t.Fatalf("@%s is on screen after ⌃Q and a restart. That fleet was parked deliberately, and "+
			"handing it straight back is what this key exists to avoid.\n%s", name, again.dump())
	}
	if strings.Contains(again.text(), "parked") {
		t.Fatalf("the room opened saying something about parked sessions; it comes back empty and "+
			"/resume is how a session returns.\n%s", again.dump())
	}

	// And it is still reachable, which is what makes the empty room a fresh
	// start rather than a loss.
	again.send("/resume all\r")
	again.await(name)
}

// A newline reaches the draft as real bytes through a real terminal.
//
// The unit test drives tea.KeyMsg values, which is the library already having
// decided what the bytes mean. This sends what a terminal actually sends - ESC
// CR, which is what iTerm2 and VS Code emit for ⇧↵ once they are set up for it,
// and LF, which every terminal sends for ⌃J with no setup at all - and reads
// the composer back off a rendered screen.
//
// It is the gap that killed ⌃⇧A: bubbletea naming a key and a terminal sending
// it are different claims, and only one of them is checkable in process.
func TestADraftTakesASecondLineFromRealKeyBytes(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("first")
	s.send("\x1b\r") // ⌥↵ / ⇧↵ after terminal setup
	s.send("second")
	s.settle()

	// Not sent, which is the failure that cannot be taken back: a draft that
	// went to the agent would have emptied the composer and echoed into the
	// transcript.
	if strings.Contains(s.text(), heardPrefix) {
		t.Fatalf("⌥↵ sent the draft instead of breaking it.\n%s", s.dump())
	}
	// And the break happened, which the composer shows by what it does *not*
	// hold: with no newline the box would read `firstsecond` on one line.
	//
	// **Only the cursor's line is on screen, and that is a real limitation
	// rather than a quirk of this test.** composerHeight is one row, so a draft
	// with two lines shows the second and scrolls the first out of sight. The
	// keystroke works; seeing what you typed needs the composer to grow, which
	// is what Claude Code does and Wake does not yet.
	if strings.Contains(s.text(), "firstsecond") {
		t.Fatalf("⌥↵ was swallowed - the draft is one line.\n%s", s.dump())
	}
	if !strings.Contains(s.text(), "second") {
		t.Fatalf("the draft lost the half typed after the break.\n%s", s.dump())
	}
}

// ⌥↑ brings back the last prompt, from the bytes a terminal really sends.
//
// This is the test that can fail while every in-process one passes. App.key
// switches on tea.KeyMsg.Type alone and bubbletea reports ⌥↑ as KeyUp with Alt
// set, so a build with no arm for the modifier moves the roster cursor and
// leaves the draft empty - and a unit test that hands the model a KeyMsg it
// constructed itself passes whatever the arm does. The same trap took ⌥↵ (see
// TestADraftTakesASecondLineFromRealKeyBytes) and the same harness is what
// caught it.
//
// Both encodings, because terminals disagree: `\x1b[1;3A` is the CSI form and
// `\x1b\x1b[A` is what a terminal configured to send Esc+ for ⌥ emits.
func TestAltArrowsWalkThePromptHistoryFromRealKeyBytes(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("the first prompt\r")
	s.await(heardPrefix + "the first prompt")

	// The composer's own row: the transcript draws the same words above, so
	// anything less exact would pass on the echo alone.
	const inTheBox = "> the first prompt"

	s.send("\x1b[1;3A") // ⌥↑
	s.await(inTheBox)

	// And the bare arrow is still the roster's, which is the constraint the
	// modifier exists to keep.
	s.send("\x1b[A")
	s.settle()
	if s.rowOf(inTheBox) < 0 {
		t.Fatalf("a bare ↑ changed the draft, so it is walking the history instead of the roster.\n%s", s.dump())
	}

	s.send("\x1b\x1b[B") // ⌥↓ in the Esc+ encoding: back to the draft it started from
	s.settle()
	if s.rowOf(inTheBox) >= 0 {
		t.Fatalf("⌥↓ left the recalled prompt in the box. The walk comes back the way it went, to the "+
			"empty draft it started from.\n%s", s.dump())
	}
}

// A long message you type is readable in the room, at a real width.
//
// The room used to flatten it to one row and cut it with an ellipsis, which is
// invisible in every in-process test that asks whether a block *fits*: a cut
// line fits perfectly. This asks the question the operator asked - can I read
// what I sent - by reading it back off a rendered screen.
func TestALongMessageIsReadableInTheRoom(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	// A narrow terminal, which is the case this was reported from: the room is
	// a column beside a conversation and an ordinary sentence reaches the cut.
	s := startWakeInAConversation(t, 60, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x17") // ⌃W - close the conversation, leaving the room
	s.await("group chat")

	// Long enough that it cannot fit one row of this pane, so the assertion is
	// about wrapping rather than about a message that happened to be short.
	// Both halves are chosen to survive a wrap intact - a phrase straddling the
	// boundary would fail this test for a reason that has nothing to do with
	// what it asserts.
	const head, tail = "can you make a hil thing", "wire it up to the form"
	s.send("@" + name + " " + head + " asking my favorite colour please and then " + tail + "\r")
	s.settle()

	// The end of the sentence is where the ask is, and it is what the cut lost.
	if !strings.Contains(s.text(), tail) {
		t.Fatalf("the room shows no %q, so the half of the message naming what was asked for is "+
			"not on screen.\n%s", tail, s.dump())
	}
	// On a different row from the start, which is what wrapping means. The
	// ellipsis check is per row and not over the screen: the speaker line cuts
	// a long branch name deliberately, and that is a different truncation.
	rows := strings.Split(s.text(), "\n")
	headRow, tailRow := -1, -1
	for i, r := range rows {
		if headRow < 0 && strings.Contains(r, head) {
			headRow = i
			if strings.Contains(r, "…") {
				t.Fatalf("the room cut your own message with an ellipsis:\n%s", s.dump())
			}
		}
		if tailRow < 0 && strings.Contains(r, tail) {
			tailRow = i
		}
	}
	if headRow < 0 || tailRow < 0 {
		t.Fatalf("could not find both halves of the message on screen.\n%s", s.dump())
	}
	if headRow == tailRow {
		t.Fatalf("the message fit one row, so this asserted nothing about wrapping.\n%s", s.dump())
	}
}

// A window that grows past the takeover draws the room beside the conversation.
func TestGrowingTheWindowBringsTheRoomBack(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()
	if strings.Contains(s.text(), "group chat") {
		t.Fatalf("the room is drawn below the takeover width.\n%s", s.dump())
	}

	s.resize(200, 45)
	s.await("group chat")
}

// ⇞ moves the reader back through the conversation and ⇟ returns.
func TestPageKeysScrollTheConversation(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("!seq 1 120\r")
	// await returns the instant 120 is drawn, which is not the same as the bang
	// output having finished arriving: the completed block replaces what was
	// drawn, and that append re-anchors the reader to the newest line. Pressing
	// ⇞ into that window scrolls, is carried back down, and the frame then
	// settles at the bottom with 120 on it - the observed failure exactly. The
	// settle is what makes the keypress land after the last append rather than
	// among them. See bugs.md BUG-6.
	s.await("120")
	s.settle()

	s.send("\x1b[5~") // ⇞
	// 120 was on screen and has to leave, which settle cannot decide.
	s.awaitGone("120")

	s.send("\x1b[6~\x1b[6~\x1b[6~\x1b[6~") // ⇟ back to the newest line
	s.await("120")
}

// An unaddressed draft has somewhere to go, and the composer says where before
// ↵ is pressed.
//
// **This is the whole of what starting a manager by default buys**, and it is
// asserted on a real screen because that is the only place it can be seen: the
// room used to refuse the first thing anybody typed into it and point them at a
// shell command, which is a front door that has to be repaired from outside.
// The target line is the half that makes it safe - this is the one route nobody
// typed a recipient for, so it is the one the room most has to draw.
func TestAnUnaddressedRoomDraftReachesTheManager(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.send("who is on this")
	s.await("→ @manager")
}

// With the manager parked the room refuses again, and names the command that
// brings it back.
//
// The refusal did not go away with the default - it **moved**, and this is the
// state it moved to. `/manager` is an off switch, so what it leaves behind is
// exactly the state the room used to open in; the sentence for it has to be the
// parked one rather than the missing one, because the name is still claimed and
// starting a second would be refused. That the sentence names a command this
// build has is held in internal/ui against the router's own map. This is only
// that a real screen shows it.
func TestAnUnaddressedRoomDraftIsRefusedWithTheManagerParked(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.send("/manager\r")
	s.await("▪ manager")

	s.send("who is on this\r")
	s.await("the manager is parked")
}

// `/manager-stop` ends it, and a real screen is where park and stop are told
// apart.
//
// The two verbs are one keystroke apart in the composer and produce two states
// that no unit test can confuse but an operator easily could. On screen they are
// opposite in both places it shows: a parked manager **keeps its roster row**
// (`▪ manager`, the parked glyph) and the room's refusal is the *parked* one,
// naming a wake; a stopped manager **has no row at all** — the name went back to
// the pool — and the refusal is the *missing* one, naming a start. Asserting the
// second sentence is what proves the session really ended rather than parking
// under a different word, because `Fleet.manager` is what chooses between those
// two sentences and it reads ended as absent.
func TestManagerStopEndsTheManagerRatherThanParkingIt(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("\x17") // ⌃W
	s.await("group chat")
	// The seated manager, before anything is done to it.
	s.await("manager")

	s.send("/manager-stop\r")

	// **Wait for the fleet to shrink, not for the frame to go quiet.** A stop is
	// asynchronous - stdin closes, the in-flight turn finishes, the process
	// exits, and only then does the row go - and the screen is *quiescent* for
	// part of that, so settle() returns mid-stop and the absence below reads as
	// a manager that never left. Under -race it does so reliably. The strip's
	// count is the positive observable that the session really ended: an ended
	// session leaves the fleet, so `2 idle` becomes `1 idle`.
	s.await("1 idle")

	if rows := s.rosterNames(); slices.Contains(rows, core.ManagerName) {
		s.t.Fatalf("the roster is %v and still holds the manager after /manager-stop. A park leaves the "+
			"row behind and a stop does not - if this is the parked glyph, the command parked under a "+
			"different word.\n%s", rows, s.dump())
	}

	// And the room refuses with the sentence for a manager that is *not there*,
	// not the one for a manager that is parked. Fleet.manager is what chooses
	// between them, so this is what proves the session ended.
	s.send("who is on this\r")
	s.await("the room does not guess who you meant")

	// esc first, because a refused draft is **kept** - the text was typed and
	// only lacks an addressee, so the room leaves it in the composer. Without
	// this the next command is appended to it and `/manager` goes out as the
	// tail of an ordinary message.
	s.send("\x1b")
	s.settle()

	// The other half of "ended releases the name": /manager gets one back. A
	// spawn is *refused* against a name still claimed, so a manager arriving
	// here is the only end-to-end evidence the stop really returned it to the
	// pool rather than leaving a parked row addressable under it.
	s.send("/manager\r")
	s.await("2 idle")
	if rows := s.rosterNames(); !slices.Contains(rows, core.ManagerName) {
		s.t.Fatalf("the roster is %v and /manager brought no manager back after a stop. Either the name "+
			"was never released, or the spawn was refused against one still claimed.\n%s", rows, s.dump())
	}
}

// The roster says an agent is blocked while it is blocked.
//
// It did not: a permission ask changed the state and nothing announced it, so
// the push waited for watchLiveness - up to a 30-second tick - and until then
// the sidebar drew the idle glyph over an agent that was waiting on a human.
// That is the whole job of an attention-ranked roster, so the wrong glyph is
// worse here than a missing one.
func TestTheRosterSaysAnAgentIsBlockedWhileItIs(t *testing.T) {
	withScriptedAgent(t, scriptAsks)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.await("○ " + name)

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.send("@" + name + " write the file\r")

	s.await("● " + name)
}

// ⎋ in the room clears the draft rather than stopping an agent.
//
// It stopped one: the room is not one agent, so ⎋ reached whichever the cursor
// rested on, and an operator pressing it over a half-typed broadcast had a turn
// stopped somewhere they were not looking.
func TestEscapeInTheRoomClearsTheDraft(t *testing.T) {
	withScriptedAgent(t, scriptInterruptible)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x17") // ⌃W
	s.await("group chat")
	s.send("@" + name + " take your time\r")
	s.await(workingMarker)

	s.send("a broadcast half typed")
	s.await("a broadcast half typed")

	s.send("\x1b")
	s.settle()
	if strings.Contains(s.text(), "a broadcast half typed") {
		t.Fatalf("⎋ did not clear the draft.\n%s", s.dump())
	}
	if strings.Contains(s.text(), "turn interrupted") {
		t.Fatalf("⎋ stopped an agent instead of clearing the draft.\n%s", s.dump())
	}
}

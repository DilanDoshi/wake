package ui

// ⌃O arms, ↵ confirms, a second ⌃O cancels - because in Claude Code ⌃O expands
// a truncated tool result and here it closes the window.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// ctrlO and confirm are the two presses, so a test reads as the gesture.
var (
	ctrlO   = tea.KeyMsg{Type: tea.KeyCtrlO}
	confirm = tea.KeyMsg{Type: tea.KeyEnter}
)

// The two legend entries a live arm swaps, spelled as they are drawn - glyph
// and label together, because `detach` on its own is also ⌃O's ordinary label
// and a test that matched the word alone would pass on an unarmed legend.
var (
	armedSend   = sendGlyph + " " + armedSendLabel
	armedCancel = detachGlyph + " " + armedDetachLabel
)

// One ⌃O does not leave, and ↵ after it does.
//
// A Claude Code user reflexively expanding a tool result used to exit Wake with
// the fleet still running - and the window that closes is the one holding the
// fleet's name, which is what `wake --fleet <name>` needs.
func TestTheArmIsCtrlOAndTheConfirmIsEnter(t *testing.T) {
	a := newRoomApp(t).withSize(160, 40).withAgents("alex")

	a, cmd := pressKey(a, ctrlO)
	if quits(cmd) {
		t.Fatal("one ⌃O left the window. It is Claude Code's expand key, so the reflex that expands " +
			"a tool result there closes the workspace here")
	}

	if _, cmd = pressKey(a, confirm); !quits(cmd) {
		t.Fatal("↵ after ⌃O did not detach. Detaching is the property the daemon exists to provide; " +
			"the arm may put a key between the reflex and the exit and may not take it away")
	}
}

// **The finding that rebuilt this.** A repeated ⌃O may not detach, at any count.
//
// There is no key release, no timing and no distinct signal, so terminal auto
// repeat and the natural human reply to a key that appeared to do nothing - press
// it again - are the same bytes as a deliberate second press. Measured:
// TestTwoCtrlOsInOneReadArriveAsTwoPresses, where they arrive as two plain
// messages rather than collapsing the way ⎋⎋ does. A confirm on the same key
// therefore fires on exactly the reflex the arm exists to catch.
func TestNoNumberOfCtrlOsDetaches(t *testing.T) {
	for _, presses := range []int{2, 3, 4, 7} {
		a := newRoomApp(t).withSize(160, 40).withAgents("alex")
		for i := range presses {
			var cmd tea.Cmd
			a, cmd = pressKey(a, ctrlO)
			if quits(cmd) {
				t.Fatalf("⌃O pressed %d times detached on press %d. Auto repeat and the reflex of "+
					"pressing a key again because nothing visible happened both produce this, and "+
					"they are the reflex the arm exists to catch", presses, i+1)
			}
		}
	}
}

// And a second ⌃O is a cancel rather than a re-arm, so the double press leaves
// the model exactly where it started.
//
// That is what makes an odd or even run of auto repeat equally harmless: the
// worst it can do is leave an arm on screen saying what ↵ would do.
func TestASecondCtrlOCancelsTheArm(t *testing.T) {
	a := newRoomApp(t).withSize(160, 40).withAgents("alex")

	a, _ = pressKey(a, ctrlO)
	a, _ = pressKey(a, ctrlO)

	if _, cmd := pressKey(a, confirm); quits(cmd) {
		t.Error("⌃O ⌃O left the detach armed, so the next ↵ closed the workspace. The second press " +
			"is the cancel: after it the model is where it was before the first")
	}
	if a.detachArmed {
		t.Error("⌃O ⌃O left detachArmed set, so the legend is still advertising a detach nothing will do")
	}
}

// **The other finding.** An arm that survives an ordinary fleet event is drawn
// for as long as it is live, so a ↵ pressed much later is never a surprise.
//
// App.disarmed is reached only from key and mouse paths, so a stream frame, a
// heartbeat, a resize or a reattach all leave the arm standing. Broadening the
// disarm to those was considered and refused - see detach.go - so the whole
// defence is that the arm has an account of itself on screen, which is the card
// arm's own contract.
func TestTheArmIsDrawnForAsLongAsItIsLive(t *testing.T) {
	a := newRoomApp(t).withSize(160, 40).withAgents("alex")
	if strings.Contains(stripANSI(a.View()), armedSend) {
		t.Fatalf("an unarmed room already draws %q, so this test cannot tell an arm from a legend", armedSend)
	}

	a, _ = pressKey(a, ctrlO)
	// Everything App.disarmed cannot see, in the order a busy fleet produces
	// it: a batch of frames off the socket carrying an ending and a dropped-frame
	// warning - which is one of the messages that takes the notice slot - then a
	// resize and a heartbeat.
	next, _ := a.Update(streamMsg{gen: a.gen, batch: batch{
		frames:  []rpc.Frame{endedPush(rpc.FrameStatusPush, "s1", "exit status 1")},
		dropped: 3,
	}})
	a = next.(App).withSize(160, 40)
	next, _ = a.Update(heartbeatMsg{})
	a = next.(App)

	out := stripANSI(a.View())
	for _, want := range []string{armedSend, armedCancel} {
		if !strings.Contains(out, want) {
			t.Errorf("after an ending, a resize and a heartbeat the armed legend does not say %q:\n%s\n"+
				"The arm outlives every one of those and App.disarmed sees none of them, so the legend "+
				"is the only thing standing between it and a ↵ pressed later for another reason", want, out)
		}
	}
	if n, said := notice.Latest(); said && strings.Contains(n.String(), detachArmPrefix) {
		t.Fatal("the fleet event did not overwrite the arm's notice, so this test is not measuring what " +
			"the finding was about: internal/notice is one slot and routine activity takes it")
	}
}

// And the arm is taken down when it is taken back, or the legend advertises a
// detach that will not happen.
func TestACancelledArmIsNoLongerDrawn(t *testing.T) {
	a := newRoomApp(t).withSize(160, 40).withAgents("alex")
	a, _ = pressKey(a, ctrlO)
	a, _ = pressKey(a, ctrlO)

	if out := stripANSI(a.View()); strings.Contains(out, armedSend) {
		t.Errorf("a cancelled arm still draws %q:\n%s", armedSend, out)
	}
}

// The arm says what a detach costs and names both keys the press was probably
// aimed at: ↵, which finishes it, and ⌃E, which expands a tool result.
func TestTheArmedDetachNamesTheConfirmAndTheExpandKey(t *testing.T) {
	a := newRoomApp(t).withSize(160, 40)

	pressKey(a, ctrlO)

	n, said := notice.Latest()
	if !said {
		t.Fatal("the first ⌃O said nothing. A key that silently does nothing is worse than one that acts")
	}
	for _, want := range []string{"↵", "⌃O", "⌃E", "fleet"} {
		if !strings.Contains(n.String(), want) {
			t.Errorf("the arm reads %q, which does not name %q: it has to say which key finishes the "+
				"detach, which one takes it back, and where the expand key went", n, want)
		}
	}
}

// Every other input takes the arm back, which is the whole of what makes it an
// arm rather than a counter. The four call sites are App.disarmed's.
func TestAnythingButTheConfirmTakesTheDetachArmBack(t *testing.T) {
	for _, tc := range []struct {
		what string
		then func(App) App
	}{
		{"a typed character", func(a App) App { return a.withDraft("h") }},
		{"a key the App takes", func(a App) App {
			next, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
			return next
		}},
		{"a click, which never reaches App.key at all", func(a App) App {
			next, _ := a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: 2})
			return next.(App)
		}},
	} {
		a := newRoomApp(t).withSize(160, 40).withAgents("alex")
		a, _ = pressKey(a, ctrlO)

		_, cmd := pressKey(tc.then(a), confirm)
		if quits(cmd) {
			t.Errorf("%s left the detach armed, so the next ↵ closed the workspace. An arm that "+
				"outlives what the operator plainly did instead is confirmed by a press meant for "+
				"something else", tc.what)
		}
	}
}

// And a card's own keys take it back too, which is the call site App.key cannot
// reach: cardKey returns before the disarm in the switch below it.
//
// It matters more now that the confirm is ↵, because ↵ is also the key that
// settles a card: two arms live at once would be one key with two meanings and
// no way to tell which fired.
func TestACardKeyTakesTheDetachArmBack(t *testing.T) {
	a, _ := pressKey(blockedPane(t), ctrlO)

	a, _ = press(a, cardDenyKey)
	if _, cmd := pressKey(a, confirm); quits(cmd) {
		t.Error("arming a card left the detach armed, so ↵ detached instead of settling the card. " +
			"Both arms are one mechanism, and the card's keys are read before the disarm every other " +
			"key goes through")
	}
}

// ⌥↵ is a newline and reaches the composer, so it is an input like any other and
// takes the arm back rather than confirming it.
func TestAltEnterDoesNotConfirmADetach(t *testing.T) {
	a := newRoomApp(t).withSize(160, 40).withAgents("alex")
	a, _ = pressKey(a, ctrlO)

	if _, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter, Alt: true}); quits(cmd) {
		t.Error("⌥↵ confirmed the detach. It is the composer's newline - the switch in App.key is on " +
			"m.Type alone, so a confirm placed above that arm would take every newline as a detach")
	}
}

// A permission card hands ↵ back, so an armed detach still confirms under one.
//
// The pairing with the test below is the whole ruling: only a *question* card
// claims ↵ unarmed, and only to move a cursor.
func TestAPermissionCardDoesNotSwallowTheDetachConfirm(t *testing.T) {
	a, _ := pressKey(blockedPane(t), ctrlO)

	if _, cmd := pressKey(a, confirm); !quits(cmd) {
		t.Error("↵ under a permission card did not confirm the armed detach. A yes/no has nothing " +
			"for ↵ to choose, so cardKey hands it back and the confirm below it is what is left")
	}
}

// A drawn question card wins ↵ and takes the arm back, which is the safe way
// round rather than an oversight.
//
// chooseCursored goes through pickOption and writes no frame, so the cost of
// the card winning is a pick that can be re-picked and an arm the legend
// immediately shows as gone. The other way round costs a detach somebody aimed
// at an answer, which is the expensive mistake of the two. See App.key.
func TestAQuestionCardWinsTheConfirmAndTakesTheArmBack(t *testing.T) {
	a, _ := pressKey(paneAsking(t), ctrlO)

	next, cmd := pressKey(a, confirm)
	if quits(cmd) {
		t.Fatal("↵ detached out from under a drawn question card. The card's own key line says ↵ " +
			"chooses, and a detach that overrode it would be the more expensive of the two mistakes")
	}
	if next.detachArmed {
		t.Error("the card took ↵ and left the detach armed, so the legend still says ↵ detaches while " +
			"the next ↵ would answer again")
	}
	if out := stripANSI(next.View()); strings.Contains(out, armedSend) {
		t.Errorf("the legend still draws %q after the card took the confirm:\n%s", armedSend, out)
	}
}

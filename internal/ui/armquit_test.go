package ui

// ⌃Q arms and a second ⌃Q confirms the fleet park, because a single press used
// to park the whole fleet and quit - so an accidental or auto-repeated ⌃Q closed
// the workspace and, through the emergency kill-switch it tripped, left every
// agent running. See park.go's armParkFleet and cmd/wake/killswitch.go.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// ctrlQ is the press, so a test reads as the gesture.
var ctrlQ = tea.KeyMsg{Type: tea.KeyCtrlQ}

// armedQuit is the cue a live ⌃Q arm draws, glyph and label together - because
// `park all & quit` on its own could also be a legend label, and a test matching
// the words alone would pass on an unarmed composer.
var armedQuit = quitGlyph + " " + armedQuitLabel

// One ⌃Q arms and parks nothing: no frame, no quit, and the fleet keeps running
// until a second ⌃Q.
func TestOneCtrlQArmsAndParksNothing(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(160, 30)

	m, cmd, handled := a.key(ctrlQ)
	if !handled {
		t.Fatal("⌃Q was not taken by App.key")
	}
	if quits(cmd) {
		t.Fatal("one ⌃Q closed the window. A single press parked the fleet and quit, so an accidental " +
			"or auto-repeated ⌃Q closed the workspace")
	}
	if _, pressed, _ := m.(App).ParkedFleet(); pressed {
		t.Fatal("one ⌃Q recorded a park. The first press arms and asks the daemon for nothing")
	}
	if !m.(App).quitArmed {
		t.Fatal("one ⌃Q did not arm, so a second press has nothing to confirm")
	}
	select {
	case f := <-sent:
		t.Fatalf("one ⌃Q wrote %+v; the first press writes no frame - the park is the second press", f)
	default:
	}
}

// Two ⌃Q parks the whole fleet and quits - the confirm.
func TestTwoCtrlQsParkTheFleetAndQuit(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(160, 30)

	m, cmd, handled := confirmQuit(t, a)
	if !handled {
		t.Fatal("the confirming ⌃Q was not taken by App.key")
	}
	if n, pressed, _ := m.(App).ParkedFleet(); !pressed || n != 2 {
		t.Errorf("after ⌃Q ⌃Q the model reports (%d, %v), want (2, true): the second press confirms the park", n, pressed)
	}
	go runLikeTheLoop(cmd)
	if f := awaitFrame(t, sent); f.Kind != rpc.FrameParkAll {
		t.Errorf("⌃Q ⌃Q wrote %+v, want a FrameParkAll: one frame for the fleet", f)
	}
}

// Anything between the two presses takes the arm back, so the next ⌃Q arms again
// rather than confirming. This is the ⌃O detach twin - see
// TestAnythingButTheConfirmTakesTheDetachArmBack - and it shares App.disarmed's
// four paths. quits(cmd) cannot tell an arm from a confirm here (parkFleet's
// command does not quit on the press, the reply does), so the discriminator is
// whether a park was recorded.
func TestAnythingButASecondCtrlQTakesTheParkArmBack(t *testing.T) {
	for _, tc := range []struct {
		what string
		then func(App) App
	}{
		{"a key the App takes", func(a App) App {
			next, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
			return next
		}},
		{"a click, which never reaches App.key at all", func(a App) App {
			next, _ := a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: 2})
			return next.(App)
		}},
	} {
		t.Run(tc.what, func(t *testing.T) {
			a := newRoomApp(t).withSize(160, 40).withAgents("alex")
			a, _ = pressKey(a, ctrlQ)
			if !a.quitArmed {
				t.Fatal("⌃Q did not arm, so this test cannot observe the disarm")
			}

			m, _ := pressKey(tc.then(a), ctrlQ)
			if _, pressed, _ := m.ParkedFleet(); pressed {
				t.Errorf("%s left the park armed, so the next ⌃Q confirmed and parked the fleet. An arm "+
					"that outlives what the operator plainly did instead is confirmed by a press meant "+
					"for something else", tc.what)
			}
		})
	}
}

// A card's own keys take the park arm back too, which is the call site the
// unconditional disarm cannot reach: cardKey returns before it, so the arm is
// taken back in the conditional disarm above it - the same reason detach
// captures detachArmed early (TestACardKeyTakesTheDetachArmBack). Without
// quitArmed in that conditional, a card key would strand the arm and the next ⌃Q
// would park the fleet.
func TestACardKeyTakesTheParkArmBack(t *testing.T) {
	a, _ := pressKey(blockedPane(t), ctrlQ)
	if !a.quitArmed {
		t.Fatal("⌃Q did not arm on a blocked pane, so this test cannot observe a card key disarming it")
	}

	a, _ = press(a, cardDenyKey)
	m, _ := pressKey(a, ctrlQ)
	if _, pressed, _ := m.ParkedFleet(); pressed {
		t.Error("a card key left the park armed, so the next ⌃Q confirmed and parked the fleet. cardKey " +
			"returns before the unconditional disarm, so quitArmed has to be taken back in the conditional " +
			"disarm above it - the same seam detach uses")
	}
}

// The park arm is drawn for as long as it is live, so a second ⌃Q pressed later
// is never a surprise. App.disarmed is reached only from key and mouse paths, so
// a stream frame, a resize and a heartbeat all leave the arm standing.
func TestTheParkArmIsDrawnAndSurvivesAFleetEvent(t *testing.T) {
	a := newRoomApp(t).withSize(160, 40).withAgents("alex")
	if strings.Contains(stripANSI(a.View()), armedQuit) {
		t.Fatalf("an unarmed room already draws %q, so this test cannot tell an arm from a legend", armedQuit)
	}

	a, _ = pressKey(a, ctrlQ)
	if out := stripANSI(a.View()); !strings.Contains(out, armedQuit) {
		t.Fatalf("after ⌃Q the composer does not draw the arm cue %q:\n%s", armedQuit, out)
	}

	// Everything App.disarmed cannot see, in the order a busy fleet produces it.
	next, _ := a.Update(streamMsg{gen: a.gen, batch: batch{
		frames:  []rpc.Frame{endedPush(rpc.FrameStatusPush, "s1", "exit status 1")},
		dropped: 3,
	}})
	a = next.(App).withSize(160, 40)
	next, _ = a.Update(heartbeatMsg{})
	a = next.(App)

	if !a.quitArmed {
		t.Fatal("a fleet event, a resize and a heartbeat took the park arm back. App.disarmed is " +
			"reached only from key and mouse paths, so none of these may clear it - or a ⌃Q pressed " +
			"later parks a fleet the operator no longer means to")
	}
	if out := stripANSI(a.View()); !strings.Contains(out, armedQuit) {
		t.Errorf("the park arm is no longer drawn after a fleet event, which is the only account it "+
			"has of itself:\n%s", out)
	}
}

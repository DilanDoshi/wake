package ui

// ⎋⎋ clears the focused pane's draft.
//
// The room has cleared its draft on a single ⎋ since 3f8c662 and keeps doing
// so; a conversation pane had no way to clear one at all. See escape.go for
// why the second press is an arm rather than a timer, and why the room is
// untouched.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// openedRoom is the room with sydney's conversation open beside it, sized
// *after* the open so that the history ask openDMWith queues has already been
// drained. It rides on the next Update, and a test whose first keypress batched
// with it would assert on a tea.Batch whose members never ran.
func openedRoom(t *testing.T) App {
	t.Helper()
	return newRoomApp(t).withAgents("sydney").openDMWith("s1", "sydney").withSize(200, 40)
}

// The older ruling, restated as a guard: one ⎋ stops the turn and leaves the
// draft, because a person types the correction and stops the turn in either
// order. TestEscLeavesTheDraftAlone says the same thing about the frame; this
// says it about the key that now has a second meaning.
func TestOneEscInAConversationStillLeavesTheDraft(t *testing.T) {
	a := openedRoom(t)
	a = a.withDraft("no, the other way")

	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if f := sentFrame(t, a, cmd); f.Kind != rpc.FrameInterrupt {
		t.Fatalf("⎋ sent %+v, want an interrupt", f)
	}
	if got := a.dms["s1"].Composer().Value(); got != "no, the other way" {
		t.Errorf("the draft is %q after one ⎋, want it untouched", got)
	}
}

func TestASecondEscClearsAConversationsDraft(t *testing.T) {
	a := openedRoom(t)
	a = a.withDraft("no, the other way")

	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	sentFrame(t, a, cmd)

	a, cmd = pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if got := a.dms["s1"].Composer().Value(); got != "" {
		t.Errorf("the draft is %q after ⎋⎋, want it cleared", got)
	}
	// Clearing a draft is not stopping a turn twice. The first ⎋ already sent
	// the interrupt; a second frame would be a request about a turn that is
	// already being torn down.
	if cmd != nil {
		t.Errorf("the second ⎋ wrote %+v to the daemon, want nothing", sentFrames(t, a, cmd))
	}
}

// The arm is what separates the two meanings, and every other input takes it
// back - the rule the card keys already keep. Without this a ⎋ pressed at any
// point in the past turns a much later ⎋ into a lost draft.
func TestAKeyBetweenTheTwoEscsTakesTheArmBack(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyMsg
	}{
		{"a typed rune", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}}},
		{"a chord the App owns", tea.KeyMsg{Type: tea.KeyTab}},
		{"an arrow", tea.KeyMsg{Type: tea.KeyUp}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := openedRoom(t)
			a = a.withDraft("keep me")

			a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
			sentFrame(t, a, cmd)
			a, _ = pressKey(a, tc.key)
			a, cmd = pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
			if cmd != nil {
				_ = sentFrames(t, a, cmd)
			}

			if got := a.dms["s1"].Composer().Value(); !strings.HasPrefix(got, "keep me") {
				t.Errorf("the draft is %q, want the ⎋ after %s to have been a first press again", got, tc.name)
			}
		})
	}
}

// ⎋⎋ pressed at the speed a person presses it shares one read and reaches
// bubbletea as a single alt+esc. Both presses' work happens on that one
// message, or the feature is for slow typists only.
func TestAFastDoubleEscBothStopsTheTurnAndClearsTheDraft(t *testing.T) {
	a := openedRoom(t).withDraft("no, the other way")

	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc, Alt: true})
	if f := sentFrame(t, a, cmd); f.Kind != rpc.FrameInterrupt {
		t.Fatalf("a collapsed ⎋⎋ sent %+v, want an interrupt", f)
	}
	if got := a.dms["s1"].Composer().Value(); got != "" {
		t.Errorf("the draft is %q after a collapsed ⎋⎋, want it cleared", got)
	}
	if a.escArmed {
		t.Error("a collapsed ⎋⎋ left an arm behind: both presses already happened")
	}
}

// And with nothing to clear and a turn running, it is exactly one interrupt,
// not an interrupt and a swallowed second meaning. Put into StateWorking
// explicitly - openedRoom's agent is idle by default, and rewind.go gives
// that combination (nothing to clear, nothing owed) a third meaning of its
// own; see TestEscEscOpensRewindOnlyWhenIdleAndEmpty for that case.
func TestAFastDoubleEscWithNoDraftJustInterrupts(t *testing.T) {
	a := openedRoom(t).applyFrame(workingAgentFrame("s1", "sydney"))

	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc, Alt: true})
	if f := sentFrame(t, a, cmd); f.Kind != rpc.FrameInterrupt {
		t.Fatalf("a collapsed ⎋⎋ sent %+v, want an interrupt", f)
	}
}

// Mashing ⎋ at a runaway agent has to keep stopping it - the invariant
// rewind.go's own picker may never break. Put into StateWorking explicitly,
// for TestAFastDoubleEscWithNoDraftJustInterrupts's reason: an idle agent
// with nothing to clear now opens the rewind picker instead, and this test is
// about the case that must never do that.
func TestEscWithNoDraftInterruptsEveryTime(t *testing.T) {
	a := openedRoom(t).applyFrame(workingAgentFrame("s1", "sydney"))

	for i := range 3 {
		var cmd tea.Cmd
		a, cmd = pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
		if f := sentFrame(t, a, cmd); f.Kind != rpc.FrameInterrupt {
			t.Fatalf("press %d sent %+v, want an interrupt", i+1, f)
		}
	}
}

// The room is untouched, and this is the half of the feature that is a
// promise not to change something.
func TestTheRoomStillClearsItsDraftOnTheFirstEsc(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney").withDraft("@sydney hello")

	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Errorf("⎋ in the room wrote %+v, want it to have cleared the draft and sent nothing", sentFrames(t, a, cmd))
	}
	if got := a.room.Composer().Value(); got != "" {
		t.Errorf("the room's draft is %q after one ⎋, want it cleared", got)
	}
}

// A key nobody can find is a key nobody has. The card's precedent is that an
// affordance which exists only in one state advertises itself in that state,
// and the legend is where a pane's keys are named.
func TestAnArmedPaneSaysTheNextEscClears(t *testing.T) {
	a := openedRoom(t)
	a = a.withDraft("keep me")

	before := shown(a)
	if strings.Contains(before, escClearLabel) {
		t.Fatalf("the composer already says %q before the arm, so this test cannot tell the cue from "+
			"the ordinary frame:\n%s", escClearLabel, before)
	}

	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	sentFrame(t, a, cmd)

	after := shown(a)
	if !strings.Contains(after, escClearLabel) {
		t.Errorf("an armed pane does not say the next ⎋ clears the draft:\n%s", after)
	}
}

// escArmed carries two different second-press meanings - clear the draft, or
// open the rewind picker - and the legend has to name whichever one is live.
// An idle, empty conversation has nothing for a second ⎋ to clear, so the arm
// there means "rewind" and must not say "clear draft": there is nothing to
// clear, and the second press does not clear anything. See rewind.go and
// escape.go's rewindable case.
func TestAnArmedEscLabelMatchesWhatTheSecondPressActuallyDoes(t *testing.T) {
	t.Run("idle and empty: the arm says rewind, not clear draft", func(t *testing.T) {
		a := openedRoom(t)

		a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
		sentFrame(t, a, cmd)

		after := shown(a)
		if strings.Contains(after, escGlyph+" "+escClearLabel) {
			t.Errorf("an armed pane with an empty composer says the next ⎋ clears a draft, but there is nothing to clear:\n%s", after)
		}
		// The full "esc rewind" entry rather than a bare "rewind": this
		// worktree's own directory is named rewind and the pane draws its cwd
		// in the header, so a bare substring check would pass for the wrong
		// reason.
		if !strings.Contains(after, escGlyph+" "+escRewindLabel) {
			t.Errorf("an armed pane with nothing to clear does not say the next ⎋ opens the rewind picker:\n%s", after)
		}
	})

	t.Run("a draft in progress: the arm still says clear draft, not rewind", func(t *testing.T) {
		a := openedRoom(t).withDraft("keep me")

		a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
		sentFrame(t, a, cmd)

		after := shown(a)
		if !strings.Contains(after, escGlyph+" "+escClearLabel) {
			t.Errorf("an armed pane with a draft does not say the next ⎋ clears it:\n%s", after)
		}
		if strings.Contains(after, escGlyph+" "+escRewindLabel) {
			t.Errorf("an armed pane with a draft to clear also advertises rewind, which the next ⎋ will not do:\n%s", after)
		}
	})
}

// A click is an input too, and it is the one that does not go through App.key.
func TestAClickTakesTheEscArmBack(t *testing.T) {
	a := openedRoom(t)
	a = a.withDraft("keep me")

	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	sentFrame(t, a, cmd)

	m, _ := a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 5, Y: 5})
	a = m.(App)
	a, cmd = pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		_ = sentFrames(t, a, cmd)
	}

	if got := a.dms["s1"].Composer().Value(); got != "keep me" {
		t.Errorf("the draft is %q, want a click to have taken the arm back", got)
	}
}

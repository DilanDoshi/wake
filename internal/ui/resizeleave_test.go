package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A resize that hides a conversation leaves it and one that brings it back
// resumes it (BUG-69): the rule is drawn over what landed while it was hidden,
// and never over what is said in front of the reader after a round trip that
// missed nothing.
func TestAResizeThereAndBackDrawsTheRuleOnlyOverWhatLandedWhileHidden(t *testing.T) {
	for _, tc := range []struct {
		name     string
		whileOff bool // a block lands while the conversation is hidden
	}{
		{name: "nothing landed while it was hidden", whileOff: false},
		{name: "a block landed while it was hidden", whileOff: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh(t)
			a := NewRoomApp(nil, Stream{}, uxFleet(nil)).WithOpenDM("s1", "alex")
			a = updated(a, tea.WindowSizeMsg{Width: 200, Height: 45})
			say := func(text string) {
				a = updated(a, streamMsg{batch: batch{frames: []rpc.Frame{eventFrame("s1", text)}}, gen: a.gen})
			}
			say("read before the resize.")
			a = updated(a.refocus(""), nudge{})
			resize := func(w int) {
				a = updated(a, tea.WindowSizeMsg{Width: w, Height: 45})
				a = updated(a, geometrySettledMsg{gen: a.geoGen})
			}
			resize(dmTakeoverColumns - 10)
			if strings.Contains(stripANSI(a.View()), "@alex ─") {
				t.Fatal("alex's conversation is still drawn at the narrow width: the case hides nothing")
			}
			if tc.whileOff {
				say("landed while hidden.")
			}
			resize(200)
			say("said in front of the reader.")
			if got := strings.Contains(stripANSI(a.View()), lastReadLabel); got != tc.whileOff {
				t.Fatalf("the frame draws %q: %v, want %v:\n%s", lastReadLabel, got, tc.whileOff, stripANSI(a.View()))
			}
		})
	}
}

// A ⇥ while a widening is pending: the pending width draws both panes, so the
// conversation was never hidden and refocus may not leave it, or the seam, which
// sees it drawn before and after, never resumes it and a rule is drawn over
// words said in front of the reader.
func TestATabWhileAWideningIsPendingLeavesNothing(t *testing.T) {
	fresh(t)
	a := NewRoomApp(nil, Stream{}, uxFleet(nil)).WithOpenDM("s1", "alex")
	a = updated(a, tea.WindowSizeMsg{Width: dmTakeoverColumns - 10, Height: 45})
	say := func(text string) {
		a = updated(a, streamMsg{batch: batch{frames: []rpc.Frame{eventFrame("s1", text)}}, gen: a.gen})
	}
	say("read at the narrow width.")
	a = updated(a.refocus("s1"), nudge{})
	a = updated(a, tea.WindowSizeMsg{Width: 200, Height: 45})
	a = updated(a, tea.KeyMsg{Type: tea.KeyTab})
	if a.focus != "" {
		t.Fatalf("⇥ left the keys with %q, want the room's", a.focus)
	}
	a = updated(a, geometrySettledMsg{gen: a.geoGen})
	say("said in front of the reader.")
	if frame := stripANSI(a.View()); strings.Contains(frame, lastReadLabel) {
		t.Fatalf("the frame draws %q over words said in front of the reader:\n%s", lastReadLabel, frame)
	}
}

// refocus leaves what its move hides at once, not at Update's seam: a rekey moves
// the keys while a stream batch is applied, and the batch's later tokens would be
// refused before the seam ran.
func TestRefocusLeavesWhatItHidesBeforeTheSeam(t *testing.T) {
	fresh(t)
	a := NewRoomApp(nil, Stream{}, uxFleet(nil)).WithOpenDM("s1", "alex")
	a = updated(a, tea.WindowSizeMsg{Width: dmTakeoverColumns - 10, Height: 45})
	a = updated(a.refocus("s1"), nudge{})
	for _, f := range []rpc.Frame{startFrame("s1"), tokenFrame("s1", "qzx001 ")} {
		a = updated(a, streamMsg{batch: batch{frames: []rpc.Frame{f}}, gen: a.gen})
	}
	if !strings.Contains(stripANSI(a.View()), "qzx001") {
		t.Fatal("the preview was never drawn: the case tests nothing")
	}
	if frame := stripANSI(a.refocus("").refocus("s1").View()); strings.Contains(frame, "qzx001") {
		t.Fatalf("alex's preview survived the move that hid it:\n%s", frame)
	}
}

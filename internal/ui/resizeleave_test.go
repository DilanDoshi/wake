package ui

import (
	"slices"
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

// refocus leaves what its move hides within the call, before Update's seam runs.
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

// One absence, one rule (BUG-69's review): a conversation a resize hid and another
// then took the slot of was left once, when it went off screen, so the rule sits
// where the reader stopped and nowhere else.
func TestAHiddenConversationDisplacedLaterDrawsOneRule(t *testing.T) {
	fresh(t)
	a := NewRoomApp(nil, Stream{}, uxFleet(nil)).WithOpenDM("s1", "alex")
	a = updated(a, tea.WindowSizeMsg{Width: 200, Height: 45})
	say := func(text string) {
		a = updated(a, streamMsg{batch: batch{frames: []rpc.Frame{eventFrame("s1", text)}}, gen: a.gen})
	}
	say("read before the resize.")
	a = updated(a.refocus(""), nudge{})
	a = updated(a, tea.WindowSizeMsg{Width: dmTakeoverColumns - 10, Height: 45})
	a = updated(a, geometrySettledMsg{gen: a.geoGen})
	say("landed while hidden.")
	a = updated(a.openDMWith("s2", "bea"), nudge{})
	if a.grid.Has("s1") {
		t.Fatal("bea did not take alex's slot: the case displaces nothing")
	}
	say("landed after bea took the slot.")
	a = updated(a.openDMWith("s1", "alex"), nudge{})
	if n := strings.Count(stripANSI(a.View()), lastReadLabel); n != 1 {
		t.Fatalf("one absence drew %d rules, want 1:\n%s", n, stripANSI(a.View()))
	}
}

// And a fork arriving in the same batch as the parent's next words: the fork
// takes the parent's pane, which is left once, though the parent spoke after.
func TestAForkArrivingBesideItsParentsWordsDrawsOneRule(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)
	say := func(frames ...rpc.Frame) {
		a = updated(a, streamMsg{batch: batch{frames: frames}, gen: a.gen})
	}
	say(eventFrame("s1", "read before the fork."))
	m, cmd, _ := a.key(tea.KeyMsg{Type: tea.KeyCtrlF})
	a = m.(App)
	go func() { _ = runCmdQuietly(cmd) }()
	forkID := awaitFrame(t, sent).SessionID
	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateWorking},
		{ID: forkID, Name: "sydney", State: rpc.StateIdle, ParentID: "s1"},
	}}
	say(rpc.Frame{Kind: rpc.FrameStatusReply, Status: &st}, eventFrame("s1", "the parent's next words."))
	if a.focus != forkID || a.grid.Has("s1") {
		t.Fatalf("the fork did not take alex's pane (focus %q): the case displaces nothing", a.focus)
	}
	say(eventFrame("s1", "and more after."))
	a = updated(a.openDMWith("s1", "alex"), nudge{})
	if n := strings.Count(stripANSI(a.View()), lastReadLabel); n != 1 {
		t.Fatalf("one absence drew %d rules, want 1:\n%s", n, stripANSI(a.View()))
	}
}

// A conversation opened into a lower pane the window has no row for stays hidden,
// so it keeps the mark it was left with, and the rule is drawn when a taller
// window brings it back over what landed meanwhile.
func TestOpeningALowerPaneTheWindowCannotDrawKeepsItsMark(t *testing.T) {
	fresh(t)
	a := NewRoomApp(nil, Stream{}, uxFleet(nil)).WithOpenDM("s1", "alex")
	a = updated(a, tea.WindowSizeMsg{Width: 200, Height: 45})
	a = updated(a.refocus("s1").openBelow("s2", "bea"), nudge{})
	say := func(text string) {
		a = updated(a, streamMsg{batch: batch{frames: []rpc.Frame{eventFrame("s2", text)}}, gen: a.gen})
	}
	say("read before the window shortened.")
	a = updated(a, tea.WindowSizeMsg{Width: 200, Height: 2*minPaneHeight + dividerHeight})
	a.roster = Roster{Selected: "s2"}
	a = updated(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	if slices.Contains(a.drawnIDs(), "s2") {
		t.Fatal("bea is drawn at a height with no row for her: the case opens nothing hidden")
	}
	say("landed while hidden.")
	a = updated(a, tea.WindowSizeMsg{Width: 200, Height: 45})
	if !strings.Contains(stripANSI(a.View()), lastReadLabel) {
		t.Fatalf("bea came back over what landed while she was hidden with no %q:\n%s", lastReadLabel, stripANSI(a.View()))
	}
}

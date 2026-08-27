package ui

// Folding a conversation's past into a pane that has just opened.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func earlierSaid(text string) core.Event {
	return core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: text}
}

func earlierAsked(text string) core.Event {
	return core.Event{Kind: core.KindUserText, SessionID: "s1", Text: text}
}

// The past goes above the present, and both are drawn.
func TestBeforePutsTheEarlierConversationAboveWhatIsThere(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 24).Append(earlierSaid("what arrived since"))
	d = d.Before([]core.Event{earlierAsked("the first thing"), earlierSaid("the answer to it")})

	out := stripANSI(d.View(80, 24))
	first := strings.Index(out, "the first thing")
	middle := strings.Index(out, "the answer to it")
	last := strings.Index(out, "what arrived since")
	switch {
	case first < 0 || middle < 0 || last < 0:
		t.Fatalf("the conversation is missing a turn:\n%s", out)
	case first >= middle || middle >= last:
		t.Errorf("the turns are drawn at %d, %d, %d: the past has to be above the present, or a returning reader reads the conversation backwards", first, middle, last)
	}
}

// Nothing is not an event.
func TestBeforeWithNothingChangesNothing(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 24).Append(earlierSaid("only this"))
	if got := d.Before(nil).View(80, 24); got != d.View(80, 24) {
		t.Error("Before(nil) redrew the conversation")
	}
}

// The last-read boundaries move with the events they index.
//
// The first draft dropped them, on the argument that they were drawn for a
// conversation this window thought it had seen the start of. A review beat it:
// the shift is exact - every index moves by len(earlier) - and a mark means
// "the reader had read N events when they left", which is still true at N+k.
// What dropping cost was the *pending* mark, so an absence in progress lost its
// rule on the one pane where a landmark matters most: it has just grown by
// hundreds of events.
func TestBeforeMovesTheBoundariesWithTheEventsTheyIndex(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 24).Append(earlierSaid("one")).Leave().Append(earlierSaid("two"))
	if len(d.marks) == 0 {
		t.Fatal("leaving and returning left no boundary, so this test is asserting nothing")
	}
	was := append([]int(nil), d.marks...)

	earlier := []core.Event{earlierAsked("older"), earlierSaid("also older")}
	got := d.Before(earlier)
	if len(got.marks) != len(was) {
		t.Fatalf("Before kept %d boundaries of %d", len(got.marks), len(was))
	}
	for i, m := range got.marks {
		if want := was[i] + len(earlier); m != want {
			t.Errorf("boundary %d moved to %d, want %d: it indexes events, and every index moved by exactly what went above it", i, m, want)
		}
	}
}

// The rule an absence in progress is owed still draws after a fold.
//
// This is what dropping the marks actually cost, and it is the sequence: open a
// conversation (the ask is queued), leave it for another, the history lands,
// and then something arrives while you are away. The rule for *that* absence is
// the pending mark.
func TestTheRuleForAnAbsenceInProgressSurvivesAFold(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 24).Append(earlierSaid("before you left")).Leave()
	d = d.Before([]core.Event{earlierAsked("older"), earlierSaid("also older")})
	d = d.Append(earlierSaid("arrived while you were away"))

	if !strings.Contains(stripANSI(d.View(80, 24)), lastReadLabel) {
		t.Errorf("no last-read rule after a fold:\n%s", stripANSI(d.View(80, 24)))
	}
}

// A reply for a conversation this client does not have is dropped.
func TestHistoryForAConversationThatIsNotOpenIsDropped(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex")
	before := a.View()

	held := len(a.dms)
	a = a.historyArrived(rpc.Frame{Kind: rpc.FrameHistoryReply, SessionID: "s2", Events: []core.Event{earlierSaid("somebody else's")}})
	if a.View() != before {
		t.Error("a history reply for a conversation this client does not have changed the frame")
	}
	// On the map rather than on the frame, which a review showed proves
	// nothing: a phantom DM for a session nobody opened draws no rows, and then
	// openDMWith finds an entry and never calls NewDM - so that conversation
	// opens with a zero-value Composer for the life of the window.
	if got := len(a.dms); got != held {
		t.Errorf("a reply for a conversation nobody opened left %d conversations, was %d", got, held)
	}
}

// A conversation is asked about once.
//
// Twice would prepend a second copy of everything under the first, which reads
// as a conversation that happened twice.
func TestAConversationIsAskedAboutOnce(t *testing.T) {
	fresh(t)
	conn, _ := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex")

	// The conversation dmApp opened is already queued; this is the ask for it.
	a, first := a.takeHistoryAsks()
	a = a.askHistory("s1")
	_, second := a.takeHistoryAsks()

	if first == nil {
		t.Error("opening a conversation asked for nothing")
	}
	if second != nil {
		t.Error("the same conversation was asked about twice: the second answer prepends a second copy of everything")
	}
}

// The ask is queued by opening a conversation and written by Update.
func TestOpeningAConversationAsksForItsHistory(t *testing.T) {
	fresh(t)
	conn, _ := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex")
	a, _ = a.takeHistoryAsks() // the conversation dmApp opened

	a = a.openDMWith("s2", "robin")
	if len(a.pendingHistory) != 1 || a.pendingHistory[0] != "s2" {
		t.Errorf("opening a conversation queued %v, want [s2]", a.pendingHistory)
	}
	if _, cmd := a.takeHistoryAsks(); cmd == nil {
		t.Error("the queued ask produced no command, so nothing reaches the daemon")
	}
}

// A reply that raced a live event is dropped rather than folded under it.
//
// The finding an adversarial review made: the first draft's comment claimed
// this check and the code did not make it. claude writes a turn to its
// transcript *and* streams it, so an event that landed while the disk was being
// read is in both - folding then draws it twice, in two places, older copy
// below newer. Losing the history is the safe side: an empty pane is what the
// operator had before the feature, and a conversation that appears to have
// happened twice is worse than one that appears not to have happened.
func TestHistoryThatRacedALiveEventIsDropped(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex")
	a, _ = a.takeHistoryAsks()

	// A turn arrives between the ask and the answer.
	a = a.observe("s1", earlierSaid("arrived while the disk was read"))
	before := a.dms["s1"].events.len()

	a = a.historyArrived(rpc.Frame{
		Kind: rpc.FrameHistoryReply, SessionID: "s1",
		Events: []core.Event{earlierAsked("older"), earlierSaid("also older")},
	})
	if got := a.dms["s1"].events.len(); got != before {
		t.Errorf("the conversation went from %d events to %d: a reply that raced a live event has to be dropped, because the same turn is in both", before, got)
	}
}

// The reader lands on the newest turn, not the oldest of what was restored.
//
// transcript.replace keeps the scroll offset and a fresh pane sits at zero, so
// without the return-to-bottom the operator opens a conversation on the *first*
// of 400 restored events with the newest turn off screen - the feature
// inverted. The existing Before tests could not see it: three short events at
// 80x24 all fit, so there is no offset to observe.
func TestTheReaderLandsOnTheNewestTurnAfterAFold(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(40, 8).Append(earlierSaid("the newest turn"))

	earlier := make([]core.Event, 60)
	for i := range earlier {
		earlier[i] = earlierAsked(fmt.Sprintf("older turn %d", i))
	}
	out := stripANSI(d.Before(earlier).View(40, 8))

	if !strings.Contains(out, "the newest turn") {
		t.Errorf("a fold that overflows the pane left the reader at the top:\n%s", out)
	}
}

// And the ordinary case still folds.
func TestHistoryForAQuietConversationIsFolded(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex")
	a, _ = a.takeHistoryAsks()

	a = a.historyArrived(rpc.Frame{
		Kind: rpc.FrameHistoryReply, SessionID: "s1",
		Events: []core.Event{earlierAsked("older"), earlierSaid("also older")},
	})
	if got := a.dms["s1"].events.len(); got != 2 {
		t.Errorf("a quiet conversation folded %d events, want 2", got)
	}
}

// A dropped reply is forgotten, so the next open asks again.
//
// Without that the drop is permanent: opening a conversation with a working
// agent is the ordinary case at 15-30 agents, and any single event in the
// ask-to-reply window leaves the operator with exactly the empty pane this
// exists to remove, with no second chance and nothing said.
func TestADroppedReplyLetsTheNextOpenAskAgain(t *testing.T) {
	fresh(t)
	conn, _ := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex")
	a, _ = a.takeHistoryAsks()

	a = a.observe("s1", earlierSaid("arrived while the disk was read"))
	a = a.historyArrived(rpc.Frame{
		Kind: rpc.FrameHistoryReply, SessionID: "s1",
		Events: []core.Event{earlierAsked("older")},
	})

	a = a.askHistory("s1")
	if _, cmd := a.takeHistoryAsks(); cmd == nil {
		t.Error("a conversation whose reply was dropped is never asked about again: the drop is permanent silence")
	}
}

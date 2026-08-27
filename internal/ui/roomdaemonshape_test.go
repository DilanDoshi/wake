package ui

// The room's restore against the shape the daemon actually sends.
//
// Every test beside this one hands roomHistoryLines a slice holding several
// sessions' events. **The daemon never sends that**: sendRoomHistory answers
// one session per frame, because it reads one transcript per ask. A rule that
// needs to see two transcripts therefore cannot run where a reply is folded,
// and a test that supplies both in one batch cannot see that it does not.
//
// Found by an adversarial review, against a branch whose own unit tests were
// green - which is this project's recurring shape: a guard whose subject can
// walk out from under it.

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// roomWithHistoryAsked is the room over two live sessions, with its asks
// already written.
func roomWithHistoryAsked(t *testing.T) App {
	t.Helper()
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, seedOf(
		rpc.SessionStatus{ID: "s1", Name: "sydney", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "john", State: rpc.StateIdle},
	)).withSize(200, 40)
	m, cmd := a.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	roomAsks(t, m.(App), cmd)
	return m.(App)
}

// oneReply is what the daemon sends: one session's transcript.
func oneReply(id string, events ...core.Event) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameRoomHistoryReply, SessionID: id, Events: events}
}

// A broadcast is one write fanned out, so it is in N transcripts and arrives in
// N separate replies. It has to come back once.
func TestABroadcastAcrossTwoRepliesIsOneRoomLine(t *testing.T) {
	a := roomWithHistoryAsked(t)
	long := clock().Add(-time.Hour)

	m := a.apply(oneReply("s1", core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "@all stop", At: long}))
	m = m.apply(oneReply("s2", core.Event{Kind: core.KindUserText, SessionID: "s2", Text: "@all stop", At: long.Add(90 * time.Millisecond)}))

	if n := strings.Count(shown(m), "@all stop"); n != 1 {
		t.Errorf("a broadcast that reached two agents came back %d times, want once:\n%s", n, shown(m))
	}
}

// And the other half, which is what makes the test above about the rule rather
// than about a cap: a turn only one transcript holds still does not come back,
// however many replies land afterwards.
func TestATurnInOneReplyStillDoesNotReachTheRoom(t *testing.T) {
	a := roomWithHistoryAsked(t)
	long := clock().Add(-time.Hour)

	m := a.apply(oneReply("s1",
		core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "@all open the turn", At: long},
		core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "told sydney alone", At: long.Add(time.Second)},
		core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "the private answer", At: long.Add(2 * time.Second)},
	))
	m = m.apply(oneReply("s2",
		core.Event{Kind: core.KindUserText, SessionID: "s2", Text: "@all open the turn", At: long.Add(50 * time.Millisecond)},
		core.Event{Kind: core.KindAssistantText, SessionID: "s2", Text: "john said something", At: long.Add(3 * time.Second)},
	))

	view := shown(m)
	for _, hidden := range []string{"told sydney alone", "the private answer"} {
		if strings.Contains(view, hidden) {
			t.Errorf("%q reached the room: it is a turn found in one transcript, or the reply to one:\n%s", hidden, view)
		}
	}
	if !strings.Contains(view, "john said something") {
		t.Errorf("prose inside the broadcast's own turn did not survive:\n%s", view)
	}
}

// A fork's transcript is its parent's up to the fork point, so asking about one
// draws an hour of the parent's prose a second time under a new name. The
// ruling says a fork is not asked about; the seed is where it was still asked.
func TestTheRoomDoesNotAskAboutAForkInItsSeed(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, seedOf(
		rpc.SessionStatus{ID: "s1", Name: "sydney", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "john", State: rpc.StateIdle, ParentID: "s1"},
	))
	m, cmd := a.Update(tea.WindowSizeMsg{Width: 200, Height: 40})

	asked := roomAsks(t, m.(App), cmd)
	if len(asked) != 1 || asked[0] != "s1" {
		t.Errorf("the room asked about %v, want only the parent - a fork's transcript is its parent's", asked)
	}
}

// And a fork that comes back from parked is the same conversation arriving by
// the other door.
func TestTheRoomDoesNotAskAboutAForkThatComesBackFromParked(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, &rpc.Status{
		Running: true,
		Parked:  []rpc.SessionStatus{{ID: "s9", Name: "marco", State: rpc.StateParked, ParentID: "s1"}},
	}).withSize(200, 40).awaitingWake("s9")

	m, cmd := a.Update(frameMsg{Frame: rpc.Frame{Kind: rpc.FrameStatusPush, Status: seedOf(
		rpc.SessionStatus{ID: "s9", Name: "marco", State: rpc.StateIdle, ParentID: "s1"},
	)}})

	if asked := roomAsks(t, m.(App), cmd); len(asked) != 0 {
		t.Errorf("the room asked about a resumed fork: %v", asked)
	}
}

// Park and wake inside one window: the room still holds everything that
// session said before it was parked, and its transcript holds the same turns.
//
// askedAt.lines watches for a session speaking *between* the ask and the reply,
// and a stable non-zero baseline passes that guard cleanly - so the whole
// pre-park conversation was restored on top of the copy already on screen. The
// wake tests around it all start from a session parked before the room opened,
// where the count is zero, which is the one sub-case the guard did handle.
//
// Found by a code review, after two adversarial passes had missed it.
func TestASessionWithRoomLinesAlreadyOnScreenIsNotAskedAboutOnAWake(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, nil).withSize(200, 40)

	// A spawn: it arrives after the seed, so it is never asked about.
	m, cmd := a.Update(frameMsg{Frame: rpc.Frame{Kind: rpc.FrameStatusPush, Status: seedOf(
		rpc.SessionStatus{ID: "s1", Name: "sydney", State: rpc.StateIdle},
	)}})
	if asked := roomAsks(t, m.(App), cmd); len(asked) != 0 {
		t.Fatalf("a spawn was asked about: %v", asked)
	}

	// It talks, in the room, and ⌃C keeps every line of that.
	next := m.(App).apply(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind: core.KindAssistantText, SessionID: "s1", Text: "said before the park",
	}})

	m, cmd = next.awaitingWake("s1").Update(frameMsg{Frame: rpc.Frame{Kind: rpc.FrameStatusPush, Status: seedOf(
		rpc.SessionStatus{ID: "s1", Name: "sydney", State: rpc.StateIdle},
	)}})

	if asked := roomAsks(t, m.(App), cmd); len(asked) != 0 {
		t.Errorf("the room asked about %v on the wake; it already holds that session's lines, and the transcript holds the same turns", asked)
	}
	if n := strings.Count(shown(m), "said before the park"); n != 1 {
		t.Errorf("the line is on screen %d times after a park and a wake", n)
	}
}

// The cutoff is stamped before the ask is even written, and the frames a
// reattach read past on its way in are replayed *after* the model exists - so
// an event timestamped before the cutoff can still reach the room after it, and
// both copies get drawn.
//
// The shipped ruling on this trade is history.go's: losing the history is the
// safe side, because a conversation that appears to have happened twice is
// worse than one that appears not to have happened at all.
func TestHistoryIsDroppedForASessionThatSpokeWhileTheAskWasInFlight(t *testing.T) {
	a := roomWithHistoryAsked(t)
	long := clock().Add(-time.Hour)

	// The live copy lands first, as it does when a frame read before the model
	// existed is replayed into it.
	m := a.apply(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind: core.KindAssistantText, SessionID: "s1", Text: "the same sentence twice",
	}})
	m = m.apply(oneReply("s1", core.Event{
		Kind: core.KindAssistantText, SessionID: "s1", Text: "the same sentence twice", At: long,
	}))

	if n := strings.Count(shown(m), "the same sentence twice"); n != 1 {
		t.Errorf("the sentence is on screen %d times: the restored copy was folded under a live one:\n%s", n, shown(m))
	}
}

// And a session that stayed quiet keeps its history, so the guard above is a
// guard rather than a deletion.
//
// Three sessions, because the guard has a second edge worth seeing: a session
// that speaks loses its batch, and its batch is also the *evidence* that a
// broadcast reached it. Two sessions would leave the surviving one unable to
// prove anything was public, and this test would pass for the wrong reason.
func TestHistoryIsKeptForASessionThatStayedQuiet(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, seedOf(
		rpc.SessionStatus{ID: "s1", Name: "sydney", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "john", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s3", Name: "maya", State: rpc.StateIdle},
	)).withSize(200, 40)
	seeded, cmd := a.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	roomAsks(t, seeded.(App), cmd)
	long := clock().Add(-time.Hour)

	m := seeded.(App).apply(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s3", Event: &core.Event{
		Kind: core.KindAssistantText, SessionID: "s3", Text: "maya is talking",
	}})
	m = m.apply(oneReply("s1",
		core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "@all report", At: long},
		core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "what sydney said before", At: long.Add(time.Second)},
	))
	m = m.apply(oneReply("s2",
		core.Event{Kind: core.KindUserText, SessionID: "s2", Text: "@all report", At: long.Add(50 * time.Millisecond)},
	))

	if !strings.Contains(shown(m), "what sydney said before") {
		t.Errorf("sydney's history was dropped because maya spoke:\n%s", shown(m))
	}
}

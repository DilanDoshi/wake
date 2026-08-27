package ui

// When the room asks for its history, and what happens to the answer.
//
// Two moments, and between them they are every way a session can arrive into a
// room that is missing what it said: the seed, and a wake. See roomhistory.go
// for why a spawn and a fork are neither.

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// seedOf is a fleet report as the caller hands one to NewRoomApp.
func seedOf(sessions ...rpc.SessionStatus) *rpc.Status {
	return &rpc.Status{Running: true, Sessions: sessions}
}

// roomAsks is every session the room asked about in one drained Update.
func roomAsks(t *testing.T, a App, cmd tea.Cmd) []string {
	t.Helper()
	var out []string
	if cmd == nil {
		return out
	}
	for _, f := range sentFrames(t, a, cmd) {
		if f.Kind == rpc.FrameRoomHistory {
			out = append(out, f.SessionID)
		}
	}
	return out
}

// The room opens over a fleet that has been talking since before this window
// existed, so the seed is where it asks.
func TestTheRoomAsksAboutEverySessionInItsSeed(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, seedOf(
		rpc.SessionStatus{ID: "s1", Name: "sydney", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "john", State: rpc.StateWorking},
	))
	m, cmd := a.Update(tea.WindowSizeMsg{Width: 200, Height: 40})

	asked := roomAsks(t, m.(App), cmd)
	if len(asked) != 2 {
		t.Fatalf("the room asked about %v, want both seeded sessions", asked)
	}
}

// A parked session draws no roster row, opens no conversation and claims no
// name. Asking about one would fill the room with a conversation nobody can
// see the other half of.
func TestTheRoomDoesNotAskAboutAParkedSession(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, &rpc.Status{
		Running: true,
		Parked:  []rpc.SessionStatus{{ID: "s9", Name: "marco", State: rpc.StateParked}},
	})
	m, cmd := a.Update(tea.WindowSizeMsg{Width: 200, Height: 40})

	if asked := roomAsks(t, m.(App), cmd); len(asked) != 0 {
		t.Errorf("the room asked about a parked session: %v", asked)
	}
}

// /resume is the case in the ask: ⌃Q, wake, an empty room, and the history
// arrives with the sessions rather than being asked for.
func TestTheRoomAsksAboutASessionThatComesBackFromParked(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, &rpc.Status{
		Running: true,
		Parked:  []rpc.SessionStatus{{ID: "s9", Name: "marco", State: rpc.StateParked}},
	}).withSize(200, 40)

	a = a.awaitingWake("s9")
	m, cmd := a.Update(frameMsg{Frame: rpc.Frame{Kind: rpc.FrameStatusPush, Status: seedOf(
		rpc.SessionStatus{ID: "s9", Name: "marco", State: rpc.StateIdle},
	)}})

	asked := roomAsks(t, m.(App), cmd)
	if len(asked) != 1 || asked[0] != "s9" {
		t.Errorf("the room asked about %v after a wake, want [s9]", asked)
	}
}

// A fresh agent has no transcript, and a fork has its parent's - which the
// room already holds under the parent. Asking would draw an hour of the
// parent's prose a second time under a new name.
func TestTheRoomDoesNotAskAboutASpawnOrAFork(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, seedOf(
		rpc.SessionStatus{ID: "s1", Name: "sydney", State: rpc.StateIdle},
	)).withSize(200, 40)
	// Drain the seed's own asks.
	m, cmd := a.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	roomAsks(t, m.(App), cmd)

	m, cmd = m.Update(frameMsg{Frame: rpc.Frame{Kind: rpc.FrameStatusPush, Status: seedOf(
		rpc.SessionStatus{ID: "s1", Name: "sydney", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "john", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s3", Name: "maya", State: rpc.StateIdle, ParentID: "s1"},
	)}})

	if asked := roomAsks(t, m.(App), cmd); len(asked) != 0 {
		t.Errorf("the room asked about %v; a spawn has no transcript and a fork's is already drawn under its parent", asked)
	}
}

// The two ledgers do not shadow each other. This is the regression the second
// frame kind exists for: one shared ask would leave whichever surface asked
// second holding nothing.
func TestAskingForTheRoomDoesNotSpendTheConversationsAsk(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, seedOf(
		rpc.SessionStatus{ID: "s1", Name: "sydney", State: rpc.StateIdle},
	))
	m, cmd := a.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	if asked := roomAsks(t, m.(App), cmd); len(asked) != 1 {
		t.Fatalf("the room asked about %v, want [s1]", asked)
	}

	next := m.(App).openDMWith("s1", "sydney")
	m, cmd = next.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	for _, f := range sentFrames(t, m.(App), cmd) {
		if f.Kind == rpc.FrameHistory && f.SessionID == "s1" {
			return
		}
	}
	t.Error("opening the conversation asked for no transcript: the room's ask spent the conversation's ledger")
}

// The answer reaches the room. Both replies, because prose is restored only
// inside a turn two transcripts prove was a broadcast.
func TestARoomHistoryReplyIsDrawnInTheRoom(t *testing.T) {
	a := roomWithHistoryAsked(t)
	long := clock().Add(-time.Hour)

	m := a.apply(oneReply("s1",
		core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "@all report", At: long},
		core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "what sydney said before", At: long.Add(time.Second)},
	))
	m = m.apply(oneReply("s2",
		core.Event{Kind: core.KindUserText, SessionID: "s2", Text: "@all report", At: long.Add(50 * time.Millisecond)},
	))

	if view := shown(m); !strings.Contains(view, "what sydney said before") {
		t.Errorf("the room did not draw what the transcript said:\n%s", view)
	}
}

// A reply for a session the room never asked about is not folded. Without this
// a second client's answer, or a daemon answering twice, draws the same
// conversation again under the first one.
func TestARoomHistoryReplyNobodyAskedForIsIgnored(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, nil).withSize(200, 40)

	long := clock().Add(-time.Hour)
	m := a.apply(rpc.Frame{Kind: rpc.FrameRoomHistoryReply, SessionID: "s1", Events: []core.Event{
		{Kind: core.KindAssistantText, SessionID: "s1", Text: "unasked for", At: long},
	}})

	if view := shown(m); strings.Contains(view, "unasked for") {
		t.Errorf("the room folded a reply it never asked for:\n%s", view)
	}
}

// And a second answer to the same ask is not folded twice.
func TestARoomHistoryReplyIsFoldedOnce(t *testing.T) {
	a := roomWithHistoryAsked(t)
	long := clock().Add(-time.Hour)

	reply := oneReply("s1",
		core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "@all once", At: long},
		core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "said once", At: long.Add(time.Second)},
	)
	m := a.apply(reply)
	m = m.apply(oneReply("s2",
		core.Event{Kind: core.KindUserText, SessionID: "s2", Text: "@all once", At: long.Add(50 * time.Millisecond)},
	))
	m = m.apply(reply)

	if n := strings.Count(shown(m), "said once"); n != 1 {
		t.Errorf("the same reply was folded %d times", n)
	}
}

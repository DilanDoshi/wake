package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The answer to a bang typed in a conversation is that conversation's, as a
// typed message's is: claude starting the bang's own turn - even one it opens
// after a busy turn ended - marks it a DM turn, so the room draws none of it
// while the pane is read.
func TestTheAnswerToADMBangStaysOutOfTheRoom(t *testing.T) {
	a := newRoomApp(t).withSize(narrowColumns, 40).withRoster(
		rpc.SessionStatus{ID: "s2", Name: "john", State: rpc.StateIdle},
	).openDMWith("s2", "john").applyGeometry().opened(t)
	a = loopEnter(a.withDraft("!echo hi"))
	id := shellFrameID(t, a)

	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{Kind: core.KindTurnEnd}})
	a = a.applyFrame(messageState("s2", id, "started"))
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
		Kind: core.KindAssistantText, Text: "the private bang answer",
	}})
	if out := roomShown(a.room, 200, 40); strings.Contains(out, "private bang answer") {
		t.Errorf("the answer to a DM bang reached the room while its pane was read:\n%s", out)
	}
}

// A message typed right after a bang waits behind it, as behind a typed send:
// so it is placed when claude takes it up, and its answer stays the DM's even
// though the bang's own turn ended in between.
func TestADMMessageTypedRightAfterABangKeepsItsAnswerPrivate(t *testing.T) {
	a := newRoomApp(t).withSize(narrowColumns, 40).withRoster(
		rpc.SessionStatus{ID: "s2", Name: "john", State: rpc.StateIdle},
	).openDMWith("s2", "john").applyGeometry().opened(t)
	a = loopEnter(a.withDraft("!echo hi"))
	bang := shellFrameID(t, a)
	a = loopEnter(a.withDraft("and what does that mean?"))
	var follow string
	for _, f := range recorderOf(t, a).taken(t) {
		if f.Kind == rpc.FrameSend && f.Shell == nil {
			follow = f.MessageID
		}
	}
	if follow == "" || len(a.queued["s2"]) != 1 {
		t.Fatalf("the follow-up was not written and waiting behind the bang (queued %d)", len(a.queued["s2"]))
	}

	a = a.applyFrame(messageState("s2", bang, "started"))
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{Kind: core.KindTurnEnd}})
	a = a.applyFrame(messageState("s2", follow, "started"))
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
		Kind: core.KindAssistantText, Text: "the private follow-up answer",
	}})
	if out := roomShown(a.room, 200, 40); strings.Contains(out, "private follow-up answer") {
		t.Errorf("the answer to a DM message sent behind a bang reached the room:\n%s", out)
	}
}

// Sent to an idle agent, a bang's turn is placed as it is written, as a typed
// message's is: a lost started frame does not make its answer public.
func TestADMBangsAnswerStaysPrivateWithoutItsStarted(t *testing.T) {
	a := newRoomApp(t).withSize(narrowColumns, 40).withRoster(
		rpc.SessionStatus{ID: "s2", Name: "john", State: rpc.StateIdle},
	).openDMWith("s2", "john").applyGeometry().opened(t)
	a = loopEnter(a.withDraft("!echo hi"))
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
		Kind: core.KindAssistantText, Text: "the private bang answer",
	}})
	if out := roomShown(a.room, 200, 40); strings.Contains(out, "private bang answer") {
		t.Errorf("a DM bang's answer reached the room with its started lost:\n%s", out)
	}
}

// Sent to a busy agent, a bang does not take over the turn already running: the
// prose before claude takes it up is that turn's - here the room's.
func TestABangToABusyAgentLeavesTheRunningTurnItsOwn(t *testing.T) {
	a := newRoomApp(t).withSize(narrowColumns, 40).withRoster(
		rpc.SessionStatus{ID: "s2", Name: "john", State: rpc.StateWorking},
	).openDMWith("s2", "john").applyGeometry().opened(t)
	a.fleet = a.fleet.sending("s2", false)
	a = loopEnter(a.withDraft("!echo hi"))
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
		Kind: core.KindAssistantText, Text: "the room turn's own prose",
	}})
	if out := roomShown(a.room, 200, 40); !strings.Contains(out, "room turn's own prose") {
		t.Errorf("a bang to a busy agent took the room's running turn private:\n%s", out)
	}
}

// Aimed from the room, the bang's turn is the room's, even when the agent's
// last turn was a DM's.
func TestTheAnswerToARoomBangIsTheRooms(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s2", Name: "john", State: rpc.StateIdle},
	).openDMWith("s2", "john").applyGeometry().opened(t).refocus("").applyGeometry()
	if !a.drawnConversations()("s2") {
		t.Fatal("john's DM is not drawn: a DM turn's answer would reach the room anyway")
	}
	a.fleet = a.fleet.sending("s2", true)
	a = loopEnter(a.withDraft("@john !echo hi"))
	id := shellFrameID(t, a)

	a = a.applyFrame(messageState("s2", id, "started"))
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
		Kind: core.KindAssistantText, Text: "the public bang answer",
	}})
	if out := roomShown(a.room, 200, 40); !strings.Contains(out, "public bang answer") {
		t.Errorf("the answer to a bang aimed from the room did not reach it:\n%s", out)
	}
}

// Restored off disk, a DM bang ends the public turn before it, so its answer
// stays out of the room; one aimed from the room opens a public turn.
func TestARestoredBangsAnswerIsOnlyTheRoomsWhenTheRoomAimedIt(t *testing.T) {
	for _, tc := range []struct {
		aimed  bool
		public bool
	}{{aimed: false}, {aimed: true, public: true}} {
		r := restored(
			[]core.Event{
				opener("s1", base),
				heard("s1", "s1 is fine", base.Add(time.Second)),
				shellRecordEvent(t, "s1", newShellSend(tc.aimed), base.Add(2*time.Second)),
				heard("s1", "the key is sk-secret", base.Add(3*time.Second)),
			},
			[]core.Event{opener("s2", base)},
		)
		got := strings.Join(texts(r), "|")
		if strings.Contains(got, "the key is sk-secret") != tc.public {
			t.Errorf("aimed from the room %v: restored %q, want the answer drawn %v", tc.aimed, got, tc.public)
		}
		if strings.Contains(got, "cat .env") {
			t.Errorf("aimed from the room %v: the bang's record itself was restored: %q", tc.aimed, got)
		}
		if !strings.Contains(got, "s1 is fine") {
			t.Errorf("the public turn before the bang was lost: %q", got)
		}
	}
}

// Two conversations running one command print one block at one moment, and
// that is not a broadcast: only the stamp says a bang was the room's.
func TestTwoIdenticalDMBangsAreNoBroadcast(t *testing.T) {
	at := base.Add(2 * time.Second)
	r := restored(
		[]core.Event{shellRecordEvent(t, "s1", newShellSend(false), at), heard("s1", "s1 says sk-secret", at.Add(time.Second))},
		[]core.Event{shellRecordEvent(t, "s2", newShellSend(false), at), heard("s2", "s2 says sk-secret", at.Add(time.Second))},
	)
	if got := strings.Join(texts(r), "|"); strings.Contains(got, "sk-secret") {
		t.Errorf("two DM bangs with one output were restored as a broadcast: %q", got)
	}
}

// The composer says where ↵ sends a bang at a name the live router skips: the
// parked agent it runs for, or the team it refuses - never the manager.
func TestTheTargetLineNamesAParkedOrTeamBangsTarget(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked, Team: "backend"},
		rpc.SessionStatus{ID: "m1", Name: core.ManagerName, State: rpc.StateIdle},
	)
	if got := a.withDraft("@alex !ls").room.Composer().Target(); got != "→ @alex · direct" {
		t.Errorf("@alex !ls with alex parked reads %q, want → @alex · direct", got)
	}
	if got := a.withDraft("@backend !ls").room.Composer().Target(); !strings.Contains(got, "@backend") {
		t.Errorf("@backend !ls with no live member reads %q, want the team named", got)
	}
}

// shellFrameID is the stamp on the one shell run a keystroke wrote.
func shellFrameID(t *testing.T, a App) string {
	t.Helper()
	for _, f := range recorderOf(t, a).taken(t) {
		if f.Shell != nil {
			return f.MessageID
		}
	}
	t.Fatal("no shell run was written")
	return ""
}

// messageState is claude's lifecycle frame for one stamped message.
func messageState(session, id, state string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: session, Event: &core.Event{
		Kind: core.KindMessageState, MessageID: id, Text: state,
	}}
}

// shellRecordEvent is a bang's record as a transcript reads it back.
func shellRecordEvent(t *testing.T, session, id string, at time.Time) core.Event {
	t.Helper()
	line, err := core.EncodeShellRun("cat .env", "KEY=sk-secret", id)
	if err != nil {
		t.Fatal(err)
	}
	evs, err := core.DecodeLine(line)
	if err != nil || len(evs) != 1 {
		t.Fatalf("decoded %d events (err %v)", len(evs), err)
	}
	ev := evs[0]
	ev.SessionID, ev.MessageID, ev.At = session, id, at
	return ev
}

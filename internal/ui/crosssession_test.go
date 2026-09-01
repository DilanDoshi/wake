package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// The room comes back with the peer message after a restore. A cross-session
// line is a first-class room event, not agent prose gated by an open broadcast,
// so collapseBroadcasts must keep it and roomHistoryLines must head it with the
// sender - the surface the feature exists for is the one a resume rebuilds.
func TestACrossSessionMessageSurvivesARoomRestore(t *testing.T) {
	r := restored([]core.Event{
		{Kind: core.KindCrossSession, SessionID: "s1", FromName: "planner", Text: "rerun the build", At: base.Add(time.Second)},
	})
	out := ansi.Strip(r.View(80, 24))
	if !strings.Contains(out, "rerun the build") {
		t.Errorf("a cross-session message was dropped on room restore:\n%s", out)
	}
	if !strings.Contains(out, "planner") {
		t.Errorf("the restored cross-session line is not attributed to the sender:\n%s", out)
	}
}

// Attribution is by from-name: a fleet agent when the sender is one of ours (so
// its colour and label head the line), a bare name when it is an outside session.
func TestCrossSpeakerMatchesFleetElseSynthesizes(t *testing.T) {
	f := newRoomApp(t).withAgents("planner").fleet
	if spk := f.crossSpeaker("planner"); spk.Name != "planner" {
		t.Errorf("crossSpeaker did not match the fleet agent: %+v", spk)
	}
	ext := f.crossSpeaker("stranger")
	if ext.Name != "stranger" || ext.ID != "" || ext.Color != "" {
		t.Errorf("crossSpeaker for an outsider = %+v, want a bare Agent{Name}", ext)
	}
}

// The room line is the sender's, not the receiving session's: a peer's message
// arriving at sydney is headed by planner, who sent it.
func TestObserveAttributesACrossSessionMessageToTheSender(t *testing.T) {
	a := newRoomApp(t).withSize(120, 40).withAgents("planner", "sydney")
	a = a.observe("s2", core.Event{Kind: core.KindCrossSession, SessionID: "s2", FromName: "planner", Text: "rerun the build"})

	out := ansi.Strip(a.View())
	head := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, crossSessionLead) {
			head = line
			break
		}
	}
	if head == "" {
		t.Fatalf("no cross-session line in the room:\n%s", out)
	}
	if !strings.Contains(head, "planner") {
		t.Errorf("cross-session line not headed by the sender: %q", head)
	}
	if strings.Contains(head, "sydney") {
		t.Errorf("cross-session line headed by the receiver, not the sender: %q", head)
	}
	if !strings.Contains(out, "rerun the build") {
		t.Errorf("body missing from the room:\n%s", out)
	}
}

// A peer's message reaches the room: the fold admits it the way it admits an
// agent's own speech, so observe can attribute and Append it.
func TestFoldAdmitsACrossSessionMessageToTheRoom(t *testing.T) {
	ev := core.Event{Kind: core.KindCrossSession, FromName: "planner", Text: "rerun the build"}
	_, forRoom := fold(Agent{}, ev, "receiver")
	if len(forRoom) != 1 || forRoom[0].Kind != core.KindCrossSession {
		t.Fatalf("forRoom = %+v, want one KindCrossSession", forRoom)
	}
}

// The room heads the line with the sender's name and carries what the peer
// wrote - the owner's ask: "the color and name of that agent and their message".
func TestTheRoomHeadsACrossSessionMessageWithTheSender(t *testing.T) {
	ev := core.Event{Kind: core.KindCrossSession, FromName: "planner", Text: "rerun the build"}
	b := roomBlock(ev, Agent{Name: "planner"}, 60, false)
	if !strings.Contains(b.text, "planner") {
		t.Errorf("room block does not name the sender: %q", b.text)
	}
	if !strings.Contains(b.text, "rerun the build") {
		t.Errorf("room block does not carry the body: %q", b.text)
	}
}

// The DM feed drops a replayed copy of the operator's own send: sendDM already
// drew a local echo, and under --replay-user-messages the same message comes
// back Echoed - a second copy would double-render (the DM does not de-duplicate).
func TestTheDMDropsAReplayedOwnSend(t *testing.T) {
	a := newRoomApp(t).withSize(120, 40).withAgents("sydney").openDMWith("s1", "sydney")
	before := a.dms["s1"].events.len()
	a = a.observe("s1", core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "ping the peer", Echoed: true})
	if got := a.dms["s1"].events.len(); got != before {
		t.Errorf("a replayed own-send reached the DM (len %d -> %d): it would double-render", before, got)
	}
}

// The receiver's own DM shows the peer message it got, headed by the sender.
func TestTheDMShowsACrossSessionMessage(t *testing.T) {
	a := newRoomApp(t).withSize(120, 40).withAgents("sydney").openDMWith("s1", "sydney")
	a = a.observe("s1", core.Event{Kind: core.KindCrossSession, SessionID: "s1", FromName: "planner", Text: "rerun the build"})
	out := stripANSI(a.dms["s1"].View(100, 40))
	if !strings.Contains(out, "planner") || !strings.Contains(out, "rerun the build") {
		t.Errorf("the DM did not show the cross-session message from planner:\n%s", out)
	}
}

// @name room-narrowing keeps a peer message in both threads it belongs to - the
// sender's (l.by) and the receiver's (l.ev.SessionID) - and out of any other.
func TestNarrowingAdmitsACrossSessionLineForSenderAndReceiver(t *testing.T) {
	l := roomLine{ev: core.Event{Kind: core.KindCrossSession, SessionID: "receiver"}, by: Agent{ID: "sender"}}
	if !focusAdmits(l, "sender", "mgr") {
		t.Error("narrowing to the sender hid a cross-session line it sent")
	}
	if !focusAdmits(l, "receiver", "mgr") {
		t.Error("narrowing to the receiver hid a cross-session line it got")
	}
	if focusAdmits(l, "other", "mgr") {
		t.Error("narrowing to an unrelated agent showed a cross-session line")
	}
}

// A long peer message folds like an agent's long reply - ⌃E and a click reach
// it through the same roomCollapsible boundary.
func TestALongCrossSessionMessageCollapses(t *testing.T) {
	long := strings.Repeat("a line of a very long peer message\n", 40)
	ev := core.Event{Kind: core.KindCrossSession, Text: long}
	if !roomCollapsible(ev, 60) {
		t.Error("a long cross-session message should be collapsible")
	}
}

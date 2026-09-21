package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// The room comes back with the peer message after a restore. A cross-session
// line is a first-class room event, not agent prose gated by an open broadcast,
// so collapseBroadcasts must keep it and roomHistoryLines must head it
// "sender → recipient" - the surface the feature exists for is the one a resume
// rebuilds. The receiver is the transcript this frame came off (named("s1")).
func TestACrossSessionMessageSurvivesARoomRestore(t *testing.T) {
	r := restored([]core.Event{
		{Kind: core.KindCrossSession, SessionID: "s1", FromName: "planner", Text: "rerun the build", At: base.Add(time.Second)},
	})
	out := ansi.Strip(r.View(80, 24))
	if !strings.Contains(out, "rerun the build") {
		t.Errorf("a cross-session message was dropped on room restore:\n%s", out)
	}
	if !strings.Contains(out, "planner → agent-s1") {
		t.Errorf("the restored cross-session line is not headed sender → recipient:\n%s", out)
	}
}

// The room names both ends of a peer message: the sender who wrote it and the
// receiving session it reached, sender first - so it reads as "planner → sydney",
// a directed message, rather than as planner's own room turn.
func TestTheRoomNamesSenderAndRecipientOfACrossSessionMessage(t *testing.T) {
	a := newRoomApp(t).withSize(120, 40).withAgents("planner", "sydney")
	// s2 is sydney, the receiver whose stream carried the envelope; planner sent it.
	a = a.observe("s2", core.Event{Kind: core.KindCrossSession, SessionID: "s2", FromName: "planner", Text: "rerun the build"})

	out := ansi.Strip(a.View())
	if !strings.Contains(out, "planner → sydney") {
		t.Errorf("room cross-session line should name sender → recipient (\"planner → sydney\"):\n%s", out)
	}
	if !strings.Contains(out, "rerun the build") {
		t.Errorf("body missing from the room:\n%s", out)
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

// The sender still heads the line, before the recipient: a peer's message
// arriving at sydney reads "↪ planner → sydney", not "↪ sydney → planner".
func TestACrossSessionLineIsHeadedByTheSenderNotTheReceiver(t *testing.T) {
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
	sender, receiver := strings.Index(head, "planner"), strings.Index(head, "sydney")
	if sender < 0 || receiver < 0 {
		t.Fatalf("cross-session head should name both ends, got %q", head)
	}
	if sender > receiver {
		t.Errorf("the sender must head the line, before the recipient: %q", head)
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

// The room heads the line "sender → recipient" and carries what the peer wrote.
// With no ToName - an outside receiver the fleet can't name - it falls back to
// heading by the sender alone rather than drawing a dangling arrow.
func TestTheRoomHeadsACrossSessionMessageSenderThenRecipient(t *testing.T) {
	ev := core.Event{Kind: core.KindCrossSession, FromName: "planner", ToName: "sydney", Text: "rerun the build"}
	b := roomBlock(ev, Agent{Name: "planner"}, 60, false)
	if !strings.Contains(b.text, "planner → sydney") {
		t.Errorf("room block does not head sender → recipient: %q", b.text)
	}
	if !strings.Contains(b.text, "rerun the build") {
		t.Errorf("room block does not carry the body: %q", b.text)
	}

	noRecv := roomBlock(core.Event{Kind: core.KindCrossSession, FromName: "planner", Text: "rerun the build"}, Agent{Name: "planner"}, 60, false)
	if !strings.Contains(noRecv.text, "planner") {
		t.Errorf("room block does not name the sender when the receiver is unknown: %q", noRecv.text)
	}
	if strings.Contains(noRecv.text, crossSessionArrow) {
		t.Errorf("room block drew a dangling arrow with no recipient: %q", noRecv.text)
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

// A received peer message reads in a dimmer grey (Subtle) than the agent's own
// white replies, so the operator can tell at a glance what was said *to* their
// agent from what the agent said back. Owner's request, 2026-09-20: the
// cross-session body is Subtle on both the DM and the room, and an assistant
// reply keeps Text - so the distinction is more than the head.
func TestACrossSessionMessageBodyIsDimmed(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor - the exact hues render, so Subtle != Text
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	subtle := fgEscape(t, Subtle)

	// The receiver's own 1:1 DM view.
	dm := crossSessionBlock(core.Event{Kind: core.KindCrossSession, FromName: "planner", Text: "rerun the build"}, 60)
	if !strings.Contains(dm, subtle) {
		t.Errorf("the DM cross-session body is not dimmed to Subtle:\n%q", dm)
	}

	// The group chat.
	room := roomBlock(core.Event{Kind: core.KindCrossSession, FromName: "planner", ToName: "sydney", Text: "rerun the build"}, Agent{Name: "planner"}, 60, false).text
	if !strings.Contains(room, subtle) {
		t.Errorf("the room cross-session body is not dimmed to Subtle:\n%q", room)
	}

	// The distinction is real: an agent's own reply is never dimmed to Subtle,
	// so it is told apart from an incoming peer message by more than the head.
	reply := roomBlock(core.Event{Kind: core.KindAssistantText, Text: "rerun the build"}, Agent{Name: "planner"}, 60, false).text
	if strings.Contains(reply, subtle) {
		t.Errorf("an agent's own reply was dimmed to Subtle, erasing the distinction:\n%q", reply)
	}
}

// crossSessionBody keeps its lines within the width it is given, so a peer
// message never returns a line wider than the room's column - roomBlock's
// invariant, an over-wide line shoves both sidebars out of place. The guarded
// widths (at and below bodyIndent) are the ones a bare Width+PaddingLeft
// overflowed on.
func TestCrossSessionBodyNeverExceedsItsWidth(t *testing.T) {
	body := "an incoming peer message long enough to wrap at any column width"
	for _, w := range []int{1, 2, 3, 8, 20} {
		for i, line := range strings.Split(crossSessionBody(body, w), "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("width %d: line %d is %d cells, wider than the column: %q", w, i, got, line)
			}
		}
	}
}

// fgEscape is the SGR sequence lipgloss emits for a foreground colour at the
// forced profile - derived, not hard-coded, so the test is about whether the
// colour is applied, not about how lipgloss spells it.
func fgEscape(t *testing.T, c lipgloss.TerminalColor) string {
	t.Helper()
	rendered := lipgloss.NewStyle().Foreground(c).Render("x")
	esc, _, ok := strings.Cut(rendered, "x")
	if !ok || esc == "" {
		t.Fatalf("lipgloss emitted no escape for the colour at this profile: %q", rendered)
	}
	return esc
}

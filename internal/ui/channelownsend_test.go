package ui

// Your own send draws in the channel it was typed in (Dilan, 2026-11-03): one
// aimed outside the picked channel used to reach its agents and vanish from the
// window that sent it. Its replies stay their own team's, and a notice says so.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// threeTeamRoom is channelRoom with iris on a third team, infra.
func threeTeamRoom(t *testing.T) App {
	t.Helper()
	sessions := channelSessions()
	sessions[3].Team = "infra"
	a := channelRoom(t)
	return a.applyStatus(&rpc.Status{Running: true, Teams: []string{"backend", "frontend", "infra"}, Sessions: sessions})
}

func sendFrom(a App, channel, draft string) App {
	a, _ = pressKey(a.selectChannel(channel).withDraft(draft), tea.KeyMsg{Type: tea.KeyEnter})
	return a
}

func TestYourOwnSendDrawsInTheChannelItWasTypedIn(t *testing.T) {
	for _, draft := range []string{"@backend ship it", "@sydney look here"} {
		t.Run(draft, func(t *testing.T) {
			a := sendFrom(threeTeamRoom(t), "frontend", draft)
			if got := strings.Contains(shown(a), draft); !got {
				t.Fatalf("%q sent from frontend is not drawn there:\n%s", draft, shown(a))
			}
			for _, c := range []struct {
				channel string
				seen    bool
			}{
				{"backend", true}, {core.BroadcastName, true}, {"infra", false}, {"frontend", true},
			} {
				if seen := strings.Contains(shown(a.selectChannel(c.channel)), draft); seen != c.seen {
					t.Errorf("under %s, %q is drawn = %v, want %v", c.channel, draft, seen, c.seen)
				}
			}
		})
	}
}

// The members' answers are their own team's thread: hidden in the channel the
// send came from, drawn under all and in theirs.
func TestTheirRepliesStayInTheirOwnThread(t *testing.T) {
	a := sendFrom(threeTeamRoom(t), "frontend", "@backend ship it")
	a = a.applyFrame(frameText("s1", "sydney shipping now"))
	assertRoomShows(t, a, []string{"@backend ship it"}, []string{"sydney shipping now"})
	assertRoomShows(t, a.selectChannel(core.BroadcastName), []string{"@backend ship it", "sydney shipping now"}, nil)
	assertRoomShows(t, a.selectChannel("backend"), []string{"@backend ship it", "sydney shipping now"}, nil)
}

// The notice fires on exactly the condition that hides the replies: a send whose
// addressees are all outside the picked channel. Never under all, never for a
// member or the manager's seat, never for a broadcast.
func TestASendOutsideTheChannelSaysWhereItsRepliesShow(t *testing.T) {
	for _, c := range []struct {
		channel, draft, who string
		says                bool
	}{
		{"frontend", "@backend ship it", "@backend", true},
		{"frontend", "@sydney look here", "@sydney", true},
		{"frontend", "@Backend ship it", "@backend", true},
		{"frontend", "@delta hi", "@delta", false},
		{"frontend", "@frontend hi", "@frontend", false},
		{"frontend", "just words", "@" + core.ManagerName, false},
		{"frontend", "@manager hi", "@" + core.ManagerName, false},
		{"frontend", "@all everyone", "@all", false},
		{core.BroadcastName, "@backend ship it", "@backend", false},
	} {
		t.Run(c.channel+" "+c.draft, func(t *testing.T) {
			a := sendFrom(threeTeamRoom(t), c.channel, c.draft)
			assertOutsideNotice(t, c.who, a.room.channel, c.says)
		})
	}
}

// @all to the one live agent, outside the channel, draws by its stamp and says
// nothing: "@all is outside" would read wrong.
func TestABroadcastToOneOutsiderDrawsAndSaysNothing(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyStatus(channelReport(
		rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s3", Name: "delta", Team: "frontend", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s5", Name: core.ManagerName, State: rpc.StateIdle},
	))
	a = sendFrom(a, "frontend", "@all everyone")
	if !strings.Contains(shown(a), "@all everyone") {
		t.Errorf("@all to one outsider is not drawn in the channel it was typed in:\n%s", shown(a))
	}
	assertOutsideNotice(t, "@all", "frontend", false)
}

// A room bang's echo lands when its command returns: it keeps the channel it was
// typed in, not the one the window shows by then, and says so at dispatch.
func TestARoomBangKeepsTheChannelItWasTypedIn(t *testing.T) {
	a := threeTeamRoom(t).selectChannel("backend").withDraft("@delta !echo crossbang")
	m, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assertOutsideNotice(t, "@delta", "backend", true)
	a = loopMsgs(m.(App).selectChannel("infra"), loopMessagesWithin(cmd, bangTestLimit))
	if strings.Contains(shown(a), "crossbang") {
		t.Errorf("the bang typed in backend is drawn in infra:\n%s", shown(a))
	}
	if !strings.Contains(shown(a.selectChannel("backend")), "crossbang") {
		t.Errorf("the bang typed in backend is not drawn there:\n%s", shown(a.selectChannel("backend")))
	}
}

// Restored history carries no stamp: a channel is per window and in memory, so a
// restored send keeps the channel's own rule.
func TestARestoredSendKeepsTheChannelsOwnRule(t *testing.T) {
	send := newRoomSend(false).ForTeam()
	r := restored(
		[]core.Event{roomTyped("s1", "run the migrations", base, send), heard("s1", "sydney migrated", base.Add(time.Second))},
		[]core.Event{roomTyped("s2", "run the migrations", base.Add(80*time.Millisecond), send), heard("s2", "thea migrated", base.Add(time.Second+80*time.Millisecond))},
	)
	if got := roomShown(r.WithChannel("frontend", joinIDs([]string{"s3", "s5"})), 80, 24); strings.Contains(got, "run the migrations") {
		t.Errorf("a restored send to backend draws in frontend:\n%s", got)
	}
	if got := roomShown(r.WithChannel("backend", joinIDs([]string{"s1", "s2", "s5"})), 80, 24); !strings.Contains(got, "run the migrations") {
		t.Errorf("a restored send to backend does not draw in backend:\n%s", got)
	}
}

// A bang at a parked agent runs and is not sent, so no reply will come: it says
// that, never that its replies show under all.
func TestABangAtAParkedOutsiderPromisesNoReplies(t *testing.T) {
	sessions := channelSessions()
	sessions[3].Team, sessions[3].State = "infra", rpc.StateParked
	a := channelRoom(t).applyStatus(&rpc.Status{Running: true, Teams: []string{"backend", "frontend", "infra"}, Sessions: sessions})
	a = loopEnter(a.selectChannel("frontend").withDraft("@iris !echo parkedbang"))
	assertOutsideNotice(t, "@iris", "frontend", false)
	if n := notice.Count(fmt.Sprintf(bangNotSent, "@iris", bangGone[rpc.StateParked])); n != 1 {
		t.Errorf("the not-sent notice was reported %d times, want 1", n)
	}
	if !strings.Contains(shown(a), "parkedbang") {
		t.Errorf("the bang typed in frontend is not drawn there:\n%s", shown(a))
	}
}

// A window's own live line keeps its stamp through a restore's merge and a resize,
// both of which re-render the room.
func TestALiveStampSurvivesARestoreAndAResize(t *testing.T) {
	a := sendFrom(threeTeamRoom(t), "frontend", "@backend ship it")
	for _, batch := range [][]core.Event{
		{opener("s1", base), heard("s1", "restored sydney", base.Add(time.Second))},
		{opener("s3", base.Add(80*time.Millisecond)), heard("s3", "restored delta", base.Add(2*time.Second))},
	} {
		a.room = a.room.Before(roomHistoryLines(batch, base.Add(100*time.Hour), named))
	}
	// restored delta drawing is the proof the merge re-rendered the channel.
	assertRoomShows(t, a, []string{"@backend ship it", "restored delta"}, []string{"restored sydney"})
	assertRoomShows(t, a.withSize(150, 40), []string{"@backend ship it"}, nil)
}

// assertOutsideNotice holds the outside-the-channel notice for who: reported
// exactly once, or never - and, for never, no such notice at all.
func assertOutsideNotice(t *testing.T, who, channel string, reported bool) {
	t.Helper()
	want := 0
	if reported {
		want = 1
	}
	if n := notice.Count(fmt.Sprintf(outsideChannel, who, channel)); n != want {
		t.Errorf("the notice for %s outside %s was reported %d times, want %d", who, channel, n, want)
	}
	if latest, ok := notice.Latest(); !reported && ok && strings.Contains(latest.String(), " is outside the ") {
		t.Errorf("a send the channel admits reported %q", latest.String())
	}
}

// The send that brings the room to another thousand messages draws the room's
// reminder too: the warning about this send is what the operator is left reading.
func TestTheOutsideNoticeOutlastsTheRoomsReminder(t *testing.T) {
	a := threeTeamRoom(t).selectChannel("frontend")
	for a.room.said.count() < roomReminderEvery-1 {
		a = a.withRoom(a.room.appendUser(core.Event{Kind: core.KindUserText, Text: "@delta hi"}, "s3", "frontend"))
	}
	a = sendFrom(a, "frontend", "@backend ship it")
	if a.room.reminded != 1 {
		t.Fatalf("precondition: the send did not pass the reminder (reminded %d)", a.room.reminded)
	}
	if got, want := noticeNow(), fmt.Sprintf(outsideChannel, "@backend", "frontend"); got != want {
		t.Errorf("the notice reads %q, want %q", got, want)
	}
}

package ui

// A bare /clear in the room empties the group chat in this window and touches
// nothing else: no agent, no conversation, no frame to the daemon.

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
)

// noticeNow is the newest notice's text, or "".
func noticeNow() string {
	n, ok := notice.Latest()
	if !ok {
		return ""
	}
	return n.String()
}

// roomSaying is the restored room with a few lines in it, written by the
// operator and by an agent.
func roomSaying(t *testing.T, words ...string) App {
	t.Helper()
	a := roomWithHistoryAsked(t)
	sydney, _ := a.fleet.Agent("s1")
	for _, w := range words {
		a = a.withRoom(a.room.appendUser(core.Event{Kind: core.KindUserText, Text: "@sydney " + w}, "s1", ""))
		a = a.withRoom(a.room.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "re " + w}, sydney))
	}
	return a
}

func submitRoom(t *testing.T, a App, text string) App {
	t.Helper()
	m, cmd := typeAndSubmit(a, text)
	if cmd != nil {
		runCmdQuietly(cmd)
	}
	return m.(App)
}

func TestClearEmptiesTheGroupChatAndWritesNothing(t *testing.T) {
	a := roomSaying(t, "alpha", "bravo")
	if !strings.Contains(shown(a), "re bravo") {
		t.Fatalf("the fixture's room shows nothing to clear:\n%s", shown(a))
	}
	recorderOf(t, a).taken(t)

	a = submitRoom(t, a, "/clear")
	if n := a.room.said.count(); n != 0 {
		t.Errorf("the room still holds %d lines after /clear", n)
	}
	if view := shown(a); strings.Contains(view, "re bravo") || strings.Contains(view, "alpha") {
		t.Errorf("the cleared room still draws its messages:\n%s", view)
	}
	if frames := recorderOf(t, a).taken(t); len(frames) != 0 {
		t.Errorf("/clear in the room wrote %d frames, want none - it may not reach an agent: %+v", len(frames), frames)
	}
	if got := noticeNow(); !strings.Contains(got, roomCleared) {
		t.Errorf("the clear said %q, want %q", got, roomCleared)
	}
	if !a.composerEmpty() {
		t.Errorf("the draft survived the clear: %q", a.composer().Value())
	}
}

// Narrowed to one agent, a bare /clear still clears the room: before, it went
// to that agent and wiped its conversation.
func TestClearInANarrowedRoomStillClearsOnlyTheRoom(t *testing.T) {
	a := roomSaying(t, "alpha")
	a.room = a.room.WithFocus("s1", "sydney")
	recorderOf(t, a).taken(t)

	a = submitRoom(t, a, "/clear")
	if frames := recorderOf(t, a).taken(t); len(frames) != 0 {
		t.Errorf("/clear in a room narrowed to sydney wrote %+v: it reached the agent", frames)
	}
	if a.room.said.count() != 0 {
		t.Error("the narrowed room was not cleared")
	}
}

// With anything after it the draft is not the room's clear, and it reaches no
// agent either: the notice names the way to clear one.
func TestClearWithAnArgumentSaysHowToClearAnAgent(t *testing.T) {
	a := roomSaying(t, "alpha")
	recorderOf(t, a).taken(t)

	a = submitRoom(t, a, "/clear sydney")
	if frames := recorderOf(t, a).taken(t); len(frames) != 0 {
		t.Errorf("/clear sydney wrote %+v", frames)
	}
	if a.room.said.count() == 0 {
		t.Error("/clear with an argument cleared the room")
	}
	if got := noticeNow(); !strings.Contains(got, roomClearUsage) {
		t.Errorf("said %q, want %q", got, roomClearUsage)
	}
}

// History asked before the clear and answered after it is older than the clear,
// so it stays cleared; a record written since does come back.
func TestRestoredHistoryOlderThanTheClearStaysCleared(t *testing.T) {
	// Asked now; john's history restored; cleared, by the clock, two hours ago.
	asked := roomWithHistoryAsked(t)
	now := clock()
	asked = asked.apply(oneReply("s2", core.Event{Kind: core.KindUserText, SessionID: "s2", Text: "restored before", At: now.Add(-4 * time.Hour), MessageID: roomSendID(t)}))
	if !strings.Contains(shown(asked), "restored before") {
		t.Fatalf("the fixture restored nothing to clear:\n%s", shown(asked))
	}
	prev := clock
	t.Cleanup(func() { clock = prev })
	clock = func() time.Time { return now.Add(-2 * time.Hour) }
	a := submitRoom(t, asked, "/clear")
	clock = prev
	before := now.Add(-3 * time.Hour)
	after := now.Add(-time.Hour)

	m := a.apply(oneReply("s1",
		core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "old words", At: before, MessageID: roomSendID(t)},
		core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "new words", At: after, MessageID: roomSendID(t)},
	))
	view := shown(m)
	if strings.Contains(view, "restored before") {
		t.Errorf("a later restore brought back history the clear had removed:\n%s", view)
	}
	if strings.Contains(view, "old words") {
		t.Errorf("history from before the clear came back:\n%s", view)
	}
	if !strings.Contains(view, "new words") {
		t.Errorf("history written after the clear did not come back:\n%s", view)
	}
}

// roomSendID is a uuid stamped by a room send, so the restore keeps the turn.
func roomSendID(t *testing.T) string {
	t.Helper()
	return newRoomSend(true).ID()
}

// ↑ still recalls what was typed before the clear, as Claude Code's prompt
// history outlives its /clear.
func TestClearKeepsThePromptHistory(t *testing.T) {
	a := submitRoom(t, roomSaying(t, "alpha"), "/clear")
	if got := a.room.prompts(); len(got) == 0 || got[len(got)-1] != "@sydney alpha" {
		t.Errorf("after /clear the room recalls %q, want the prompt typed before it", got)
	}
}

// A room past its retention cap has a marker for the history it reclaimed; a
// clear is a fresh room, so the marker goes with everything else.
func TestClearingAReclaimedRoomDropsTheReclaimedMarker(t *testing.T) {
	a := roomSaying(t, "alpha", "bravo")
	a = a.withRoom(a.room.reclaimOldest(1))
	if !a.room.reclaimed || a.room.tr.prefix == "" {
		t.Fatal("the fixture did not reclaim, so there is no marker to clear")
	}
	a = submitRoom(t, a, "/clear")
	if a.room.tr.prefix != "" || strings.Contains(shown(a), roomReclaimedHistory) {
		t.Errorf("the cleared room still says history was reclaimed:\n%s", shown(a))
	}
}

// The order it happens in: asked at the window's open, cleared, then answered.
// Everything the reply holds is older than the clear, so none of it comes back.
func TestAHistoryReplyAfterTheClearRestoresNothingOlder(t *testing.T) {
	a := submitRoom(t, roomWithHistoryAsked(t), "/clear")
	m := a.apply(oneReply("s1", core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "from before", At: clock().Add(-time.Hour), MessageID: roomSendID(t)}))
	if strings.Contains(shown(m), "from before") || m.room.said.count() != 0 {
		t.Errorf("a reply answered after the clear restored history older than it:\n%s", shown(m))
	}
}

// A session the room held lines for is not asked again after the clear, the way
// it was not before it: a park and a wake would restore a turn whose live echo
// is still on screen.
func TestAClearedSessionIsNotAskedForItsHistoryAgain(t *testing.T) {
	a := roomSaying(t, "alpha")
	// sydney as a session that arrived after the room opened - a spawn, which is
	// never asked about - so only her lines on screen refuse an ask.
	at := maps.Clone(a.roomAsk.at)
	delete(at, "s1")
	a.roomAsk.at = at
	if pre := a.askRoomHistory("s1"); slices.Contains(pre.roomAsk.pending, "s1") {
		t.Fatal("before the clear the room already asks sydney, so this proves nothing")
	}
	a = submitRoom(t, a, "/clear")
	if next := a.askRoomHistory("s1"); slices.Contains(next.roomAsk.pending, "s1") {
		t.Error("a session the cleared room held lines for was asked for its history again")
	}
}

// Any other word typed bare in the room is not the room's: it goes to the
// default addressee exactly as it did, the words that begin like clear included.
func TestOtherSlashWordsInTheRoomStillReachTheDefaultAddressee(t *testing.T) {
	for _, text := range []string{"/compact", "/cleanup", "/clear-cache", "/clearance", "/deploy x"} {
		t.Run(text, func(t *testing.T) {
			fresh(t)
			a := newRoomApp(t).withSize(200, 40).withAgents("sydney", core.ManagerName)
			managerID := idOfAgentNamed(t, a, core.ManagerName)
			recorderOf(t, a).taken(t)
			m := submitRoom(t, a, text)
			var sent []string
			for _, f := range recorderOf(t, m).taken(t) {
				if f.SessionID == managerID {
					sent = append(sent, f.Text)
				}
			}
			if !slices.Contains(sent, text) {
				t.Errorf("%q typed in the room reached the manager as %q, want it unchanged", text, sent)
			}
		})
	}
}

package ui

// Channels: a team picked in the roster is the group chat's base, the thread a
// lone @team narrows to, and a lone @name or @team narrows within it.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// channelSessions is two teams, an untagged agent and the manager.
func channelSessions() []rpc.SessionStatus {
	return []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", Team: "backend", Cwd: "/tmp/harbor", State: rpc.StateIdle},
		{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
		{ID: "s3", Name: "delta", Team: "frontend", State: rpc.StateIdle},
		{ID: "s4", Name: "iris", State: rpc.StateIdle},
		{ID: "s5", Name: core.ManagerName, State: rpc.StateIdle},
	}
}

func channelReport(sessions ...rpc.SessionStatus) *rpc.Status {
	return &rpc.Status{Running: true, Teams: []string{"backend", "frontend"}, Sessions: sessions}
}

// channelRoom is the room over channelSessions, each agent having said a line.
func channelRoom(t *testing.T) App {
	t.Helper()
	a := newRoomApp(t).withSize(200, 40).applyStatus(channelReport(channelSessions()...))
	for _, f := range []rpc.Frame{
		frameText("s1", "sydney migrations green"),
		frameText("s2", "thea api rebased"),
		frameText("s3", "delta bundle building"),
		frameText("s4", "iris reading docs"),
		frameText("s5", "manager coordinating"),
	} {
		a = a.applyFrame(f)
	}
	return a
}

// titleOf is the name set into the room composer's top edge.
func titleOf(view string) string {
	lines := strings.Split(ansi.Strip(view), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); strings.HasPrefix(l, "╭") {
			return strings.Trim(l, "╭╮─ ")
		}
	}
	return ""
}

func roomTitleOf(a App) string { return titleOf(a.roomFor().View(a.room.width, a.room.height)) }

// everyKindOfLine is one room line of every kind a channel decides on, each with
// its own words. mgr is the manager's id.
func everyKindOfLine(mgr string) []roomLine {
	agent := func(id, text string) roomLine {
		return roomLine{ev: core.Event{Kind: core.KindAssistantText, SessionID: id, Text: text}, by: Agent{ID: id, Name: id}}
	}
	user := func(to, text string) roomLine {
		return roomLine{ev: core.Event{Kind: core.KindUserText, Text: text}, to: to}
	}
	peer := func(from, to, text string) roomLine {
		return roomLine{ev: core.Event{Kind: core.KindCrossSession, SessionID: to, Text: text},
			by: Agent{ID: from, Name: from}, recipient: Agent{ID: to, Name: to}}
	}
	return []roomLine{
		agent("s1", "member one prose"),
		agent("s2", "member two prose"),
		agent("s3", "outsider prose"),
		agent(mgr, "manager prose"),
		user("", "a broadcast"),
		user("s1", "a turn to member one"),
		user("s3", "a turn to the outsider"),
		user(mgr, "a turn to the manager"),
		user(joinIDs([]string{"s1", "s2"}), "a turn to the team"),
		user(joinIDs([]string{"s3", "s4"}), "a turn to another team"),
		peer("s1", "s3", "member to outsider"),
		peer("s3", "s2", "outsider to member"),
		peer(mgr, "s2", "manager to member"),
		peer("s3", "s4", "outsider to outsider"),
	}
}

func lineText(l roomLine) string { return l.ev.Text }

func appendAll(r Room, lines []roomLine) Room {
	for _, l := range lines {
		r = r.appendLine(l)
	}
	return r
}

// The channel is the one predicate a lone @team narrows by, over its key: every
// kind of line draws in a channel exactly when focusAdmits admits it, on both the
// live path (appendLine, before and after the pick) and a re-render. No second
// filter decides anything.
func TestAChannelDrawsExactlyWhatItsKeyAdmits(t *testing.T) {
	const mgr = "s5"
	lines := everyKindOfLine(mgr)
	for _, key := range []string{joinIDs([]string{"s1", "s2"}), joinIDs([]string{"s1", "s2", mgr})} {
		picked := appendAll(NewRoom().SetSize(120, 120), lines).WithChannel("backend", key)
		live := appendAll(NewRoom().SetSize(120, 120).WithChannel("backend", key), lines)
		for name, r := range map[string]Room{"picked after": picked, "picked before": live} {
			out := ansi.Strip(r.View(120, 120))
			for _, l := range lines {
				want := focusAdmits(l, key)
				if got := strings.Contains(out, lineText(l)); got != want {
					t.Errorf("key %q, %s: %q drawn=%v, want %v", key, name, lineText(l), got, want)
				}
			}
		}
	}
}

// `all` is the room as it was before channels: picking a team and then all
// again draws what a room never picked into draws, byte for byte.
func TestAllIsTheRoomAsItAlwaysWas(t *testing.T) {
	lines := everyKindOfLine("s5")
	never := appendAll(NewRoom().SetSize(120, 120), lines)
	back := never.WithChannel("backend", joinIDs([]string{"s1", "s2"})).WithChannel("", "")
	if got, want := back.View(120, 120), never.View(120, 120); got != want {
		t.Errorf("all after a channel differs from the room that never picked one:\n%s\n---\n%s", got, want)
	}
}

// A mention inside a channel narrows within it: what both the channel and the
// thread it names admit.
func TestAMentionNarrowsWithinTheChannel(t *testing.T) {
	lines := everyKindOfLine("s5")
	key := joinIDs([]string{"s1", "s2"})
	for _, focus := range []string{"s1", "s3", key} {
		r := appendAll(NewRoom().SetSize(120, 120), lines).WithChannel("backend", key).WithFocus(focus, "who")
		out := ansi.Strip(r.View(120, 120))
		for _, l := range lines {
			want := focusAdmits(l, key) && focusAdmits(l, focus)
			if got := strings.Contains(out, lineText(l)); got != want {
				t.Errorf("channel backend, @%s: %q drawn=%v, want %v", focus, lineText(l), got, want)
			}
		}
	}
}

func TestAChannelTitlesTheRoomAndAMentionNarrowsWithinIt(t *testing.T) {
	a := channelRoom(t)
	if got := roomTitleOf(a); got != roomTitle {
		t.Fatalf("a fresh room is titled %q, want %q", got, roomTitle)
	}

	a = a.selectChannel("backend")
	if got := roomTitleOf(a); got != "backend" {
		t.Errorf("the backend channel is titled %q, want the team's name", got)
	}
	assertRoomShows(t, a,
		[]string{"sydney migrations green", "thea api rebased"},
		[]string{"delta bundle building", "iris reading docs"})

	a = a.withDraft("@sydney ")
	if got := roomTitleOf(a); got != "backend › @sydney" {
		t.Errorf("@sydney inside backend is titled %q, want backend › @sydney", got)
	}
	assertRoomShows(t, a, []string{"sydney migrations green"}, []string{"thea api rebased", "delta bundle building"})

	// Clearing the mention returns to the channel, not to all.
	a = a.clearDraft()
	if got := roomTitleOf(a); got != "backend" {
		t.Errorf("a cleared draft left the room titled %q, want backend", got)
	}
	assertRoomShows(t, a,
		[]string{"sydney migrations green", "thea api rebased"},
		[]string{"delta bundle building", "iris reading docs"})

	a = a.selectChannel(core.BroadcastName)
	if got := roomTitleOf(a); got != roomTitle {
		t.Errorf("all is titled %q, want %q", got, roomTitle)
	}
	assertRoomShows(t, a, []string{"delta bundle building", "iris reading docs", "manager coordinating"}, nil)
	if got := roomTitleOf(a.withDraft("@delta ")); got != roomTitle+" › @delta" {
		t.Errorf("@delta under all is titled %q", got)
	}
}

// Entering a channel reads its latest line, a width change's own rule, and the
// re-render renumbers the lines a room selection is anchored to, so it goes.
func TestPickingAChannelShowsItsNewestLineAndDropsTheSelection(t *testing.T) {
	a := channelRoom(t)
	for i := range 80 {
		a = a.applyFrame(frameText("s1", fmt.Sprintf("sydney note %02d", i)))
	}
	a.room = a.room.ScrollUp(30)
	a.sel, a.selecting = selection{anchor: point{line: 3}, head: point{line: 5}}, true
	a = a.selectChannel("backend")
	if !a.room.tr.atBottom() {
		t.Error("picking a channel left the reader scrolled back")
	}
	if !a.sel.empty() || a.selecting {
		t.Errorf("picking a channel kept the room selection %+v", a.sel)
	}
}

// Who is on the team changing under the channel is the same channel: the reader
// keeps their place, as under a standing @team.
func TestAChannelKeepsAScrolledReadersPlaceAsItsMembersChange(t *testing.T) {
	a := channelRoom(t)
	for i := range 80 {
		a = a.applyFrame(frameText("s1", fmt.Sprintf("sydney note %02d", i)))
	}
	a = a.selectChannel("backend")
	a.room = a.room.ScrollUp(30)
	top := func(a App) string { return a.room.tr.lines.slice(a.room.tr.scroll, a.room.tr.scroll+1)[0] }
	before := top(a)

	a = a.applyStatus(channelReport(append(channelSessions(),
		rpc.SessionStatus{ID: "s6", Name: "kit", Team: "backend", State: rpc.StateIdle})...))
	if !addressedTo(a.room.channelKey, "s6") {
		t.Fatalf("kit joined backend and the channel's key %q left it out", a.room.channelKey)
	}
	if a.room.tr.atBottom() || top(a) != before {
		t.Errorf("a member joining moved the reader from %q to %q", before, top(a))
	}
}

// A report that changes who is on the channel re-renders the room, so a selection
// anchored to its lines goes; one that changes nothing renders nothing - a report
// arrives on every state change, and the room is up to 20,000 lines.
func TestAReportReRendersAChannelOnlyWhenItsMembersChange(t *testing.T) {
	a := channelRoom(t).selectChannel("backend")
	renders := 0
	original := renderRoom
	renderRoom = func(r Room, lines []roomLine) []block { renders++; return original(r, lines) }
	t.Cleanup(func() { renderRoom = original })

	a = a.applyStatus(channelReport(channelSessions()...))
	if renders != 0 {
		t.Errorf("a report that changed nobody's team re-rendered the channel %d times", renders)
	}

	a.sel, a.selecting = selection{anchor: point{line: 3}, head: point{line: 5}}, true
	a = a.applyStatus(channelReport(append(channelSessions(),
		rpc.SessionStatus{ID: "s6", Name: "kit", Team: "backend", State: rpc.StateIdle})...))
	if renders == 0 {
		t.Fatal("precondition: kit joining backend did not re-render the channel")
	}
	if !a.sel.empty() || a.selecting {
		t.Errorf("a re-render under a changed membership kept the room selection %+v", a.sel)
	}
}

// A line the channel hides is still the room's: picking all shows it, and a
// width change under the channel does not let it in.
func TestALineAChannelHidesIsKeptForAll(t *testing.T) {
	a := channelRoom(t).selectChannel("backend")
	a = a.applyFrame(frameText("s3", "delta late note"))
	assertRoomShows(t, a, nil, []string{"delta late note"})
	a = a.withSize(150, 40)
	assertRoomShows(t, a, []string{"sydney migrations green"}, []string{"delta late note", "delta bundle building"})
	a = a.selectChannel(core.BroadcastName)
	assertRoomShows(t, a, []string{"delta late note", "delta bundle building"}, nil)
}

// Restored history needs no path of its own: it lands through the channel, and
// all shows what the channel kept back.
func TestRestoredHistoryArrivesThroughTheChannel(t *testing.T) {
	r := NewRoom().SetSize(100, 40).WithChannel("backend", joinIDs([]string{"s1", "s2"}))
	for _, batch := range [][]core.Event{
		{opener("s1", base), heard("s1", "restored sydney", base.Add(time.Second))},
		{opener("s3", base.Add(80*time.Millisecond)), heard("s3", "restored delta", base.Add(2*time.Second))},
	} {
		r = r.Before(roomHistoryLines(batch, base.Add(100*time.Hour), named))
	}
	out := ansi.Strip(r.View(100, 40))
	if !strings.Contains(out, "restored sydney") || !strings.Contains(out, openerText) || strings.Contains(out, "restored delta") {
		t.Errorf("restored history under the backend channel:\n%s", out)
	}
	if out := ansi.Strip(r.WithChannel("", "").View(100, 40)); !strings.Contains(out, "restored delta") {
		t.Errorf("all did not show the restored line the channel kept back:\n%s", out)
	}
}

// ⌃A is the mention's override and nothing else: with a lone target it widens to
// the channel, never past it, and with none it is refused as it always was.
func TestCtrlAInsideAChannelTogglesOnlyTheMention(t *testing.T) {
	a := channelRoom(t).selectChannel("backend").withDraft("@sydney ")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlA})
	if a.room.effectiveFocus() != "" || a.room.channel != "backend" {
		t.Fatalf("⌃A: focus %q, channel %q; want the mention widened and the channel kept", a.room.effectiveFocus(), a.room.channel)
	}
	assertRoomShows(t, a, []string{"thea api rebased"}, []string{"delta bundle building"})
	if got := roomTitleOf(a); got != "backend" {
		t.Errorf("widened inside backend, the room is titled %q", got)
	}

	a = a.clearDraft()
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlA})
	if notice.Count(roomFilterNoTarget) != 1 {
		t.Errorf("⌃A with no target in a channel was not refused (count=%d)", notice.Count(roomFilterNoTarget))
	}
	if a.room.channel != "backend" {
		t.Errorf("⌃A left the channel for %q", a.room.channel)
	}
}

// /groupchat-filter is the mention's default: off, a lone @name leaves the room
// at its channel, and ⌃A narrows within it on demand.
func TestGroupchatFilterOffInsideAChannelKeepsTheChannel(t *testing.T) {
	a := channelRoom(t).selectChannel("backend")
	a, _ = pressKey(a.withDraft(SlashPrefix+groupchatFilterCommand+" off"), tea.KeyMsg{Type: tea.KeyEnter})
	a = a.withDraft("@sydney ")
	if a.room.effectiveFocus() != "" || a.room.channel != "backend" {
		t.Fatalf("filter off, @sydney: focus %q, channel %q", a.room.effectiveFocus(), a.room.channel)
	}
	assertRoomShows(t, a, []string{"thea api rebased"}, []string{"delta bundle building"})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlA})
	assertRoomShows(t, a, []string{"sydney migrations green"}, []string{"thea api rebased", "delta bundle building"})
}

// A team with no header left on the roster has no channel to show: the window
// returns to all and says so.
func TestAChannelWhoseTeamLeavesTheRosterFallsBackToAll(t *testing.T) {
	a := channelRoom(t).selectChannel("frontend")
	sessions := channelSessions()
	sessions[2].Team = "backend" // delta, frontend's only member, moves
	a = a.applyStatus(&rpc.Status{Running: true, Teams: []string{"backend"}, Sessions: sessions})
	if a.room.channel != "" || a.room.channelKey != "" {
		t.Fatalf("frontend emptied and the room stayed on channel %q (key %q)", a.room.channel, a.room.channelKey)
	}
	if got := roomTitleOf(a); got != roomTitle {
		t.Errorf("the room is titled %q, want %q", got, roomTitle)
	}
	if n := notice.Count(fmt.Sprintf(channelGone, "frontend")); n != 1 {
		t.Errorf("the fall back to all was reported %d times, want once", n)
	}
	assertRoomShows(t, a, []string{"iris reading docs"}, nil)
}

// The key follows what the roster draws under the header: a parked member is
// still there, so it stays in the channel, and a team of parked members is still
// a channel. An ended one has left the roster, and the channel.
func TestAChannelKeepsItsParkedMembersAndDropsItsEndedOnes(t *testing.T) {
	a := channelRoom(t).selectChannel("backend")
	sessions := channelSessions()
	sessions[0].State = rpc.StateParked
	a = a.applyStatus(channelReport(sessions...))
	if !addressedTo(a.room.channelKey, "s1") {
		t.Errorf("parked sydney left the backend key %q", a.room.channelKey)
	}
	assertRoomShows(t, a, []string{"sydney migrations green"}, nil)

	sessions[1].State = rpc.StateParked
	a = a.applyStatus(channelReport(sessions...))
	if a.room.channel != "backend" {
		t.Errorf("a team of parked members lost its channel to %q", a.room.channel)
	}

	sessions[1].State = rpc.StateEnded
	a = a.applyStatus(channelReport(sessions...))
	if addressedTo(a.room.channelKey, "s2") {
		t.Errorf("ended thea is still in the backend key %q", a.room.channelKey)
	}
}

// The working line is the channel's: an outsider's turn is not named in it, a
// mention inside the channel does not narrow it further, and all is the fleet.
func TestTheWorkingLineIsTheChannels(t *testing.T) {
	a := channelRoom(t)
	sessions := channelSessions()
	sessions[2].State = rpc.StateWorking // delta, frontend
	a = a.applyStatus(channelReport(sessions...))
	if beat := a.roomFor().beat; !strings.Contains(beat, "delta") {
		t.Fatalf("precondition: under all the working line is %q", beat)
	}
	a = a.selectChannel("backend")
	if beat := a.roomFor().beat; beat != "" {
		t.Errorf("backend's working line named an outsider: %q", beat)
	}
	sessions[1].State = rpc.StateWorking // thea, backend
	a = a.applyStatus(channelReport(sessions...)).withDraft("@sydney ")
	if beat := a.roomFor().beat; !strings.Contains(beat, "thea") || strings.Contains(beat, "more") {
		t.Errorf("backend's working line under @sydney is %q, want thea's turn alone", beat)
	}
}

// Unread counts belong to the roster, which still shows every agent: a line the
// channel keeps out of the room still badges the agent that said it.
func TestALineAChannelHidesStillCountsAsUnread(t *testing.T) {
	a := channelRoom(t).selectChannel("backend")
	was := a.fleet.agents["s3"].Unread
	a = a.applyFrame(frameText("s3", "delta late note"))
	if got := a.fleet.agents["s3"].Unread; got != was+1 {
		t.Errorf("delta's unread went from %d to %d, want one more", was, got)
	}
}

// The manager has a seat in every channel (the owner, 2026-10-24): routing does
// not change in one, so an unaddressed draft is the manager's, and your turn and
// the manager's own lines draw in the channel you sent it from.
func TestTheManagerHasASeatInEveryChannel(t *testing.T) {
	const mgr = "s5"
	a := channelRoom(t).selectChannel("backend")
	if !addressedTo(a.room.channelKey, mgr) {
		t.Errorf("the backend key %q has no seat for the manager", a.room.channelKey)
	}
	if r := a.route("status?"); len(r.Targets) != 1 || r.Targets[0] != mgr {
		t.Fatalf("an unaddressed draft in a channel routes to %v, want the manager", r.Targets)
	}
	a, _ = pressKey(a.withDraft("status?"), tea.KeyMsg{Type: tea.KeyEnter})
	assertRoomShows(t, a, []string{"status?", "manager coordinating"}, []string{"delta bundle building"})
}

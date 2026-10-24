package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestFocusAdmits(t *testing.T) {
	const john, iris, mgr = "john-id", "iris-id", "mgr-id"
	agent := func(id string) roomLine {
		return roomLine{ev: core.Event{Kind: core.KindAssistantText, SessionID: id}}
	}
	user := func(to string) roomLine {
		return roomLine{ev: core.Event{Kind: core.KindUserText}, to: to}
	}
	peer := func(from, to string) roomLine {
		return roomLine{ev: core.Event{Kind: core.KindCrossSession, SessionID: to}, by: Agent{ID: from}}
	}
	cases := []struct {
		name  string
		line  roomLine
		focus string
		want  bool
	}{
		{"unfocused admits agent prose", agent(iris), "", true},
		{"unfocused admits any user line", user(iris), "", true},
		{"unfocused admits the manager", agent(mgr), "", true},
		{"focused admits john's prose", agent(john), john, true},
		{"focused hides iris's prose", agent(iris), john, false},
		{"focused hides the manager's prose", agent(mgr), john, false},
		{"focused hides the manager's turn end", roomLine{ev: core.Event{Kind: core.KindTurnEnd, SessionID: mgr}}, john, false},
		{"focused admits a message the manager sent john", peer(mgr, john), john, true},
		{"focused admits a message john sent the manager", peer(john, mgr), john, true},
		{"focused hides a message the manager sent iris", peer(mgr, iris), john, false},
		{"focused admits a broadcast", user(""), john, true},
		{"focused admits your message to john", user(john), john, true},
		{"focused hides your message to iris", user(iris), john, false},
		{"focused admits john's permission ask", roomLine{ev: core.Event{Kind: core.KindPermissionRequest, SessionID: john}}, john, true},
		{"focused hides iris's turn end", roomLine{ev: core.Event{Kind: core.KindTurnEnd, SessionID: iris}}, john, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := focusAdmits(c.line, c.focus); got != c.want {
				t.Fatalf("focusAdmits(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

func TestRoomEchoCarriesAddressee(t *testing.T) {
	r := NewRoom().SetSize(80, 24)
	r = r.appendUser(core.Event{Kind: core.KindUserText, Text: "@iris rebase"}, "iris-id")
	lines := r.said.slice(r.said.first(), r.said.len())
	if len(lines) == 0 {
		t.Fatal("expected one room line")
	}
	if got := lines[len(lines)-1].to; got != "iris-id" {
		t.Fatalf("room echo to = %q, want %q", got, "iris-id")
	}
}

func TestRoomViewNarrowsToFocus(t *testing.T) {
	const john, iris, mgr = "john-id", "iris-id", "mgr-id"
	johnA := Agent{ID: john, Name: "john"}
	irisA := Agent{ID: iris, Name: "iris"}
	mgrA := Agent{ID: mgr, Name: core.ManagerName}

	r := NewRoom().SetSize(80, 24)
	r = r.appendUser(core.Event{Kind: core.KindUserText, Text: "everyone: status?"}, "")
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: john, Text: "john here, all green"}, johnA)
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: iris, Text: "iris here, still building"}, irisA)
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: mgr, Text: "manager coordinating"}, mgrA)
	r = r.appendUser(core.Event{Kind: core.KindUserText, Text: "@iris hurry"}, iris)

	full := r.View(80, 24)
	for _, want := range []string{"all green", "still building", "manager coordinating", "hurry"} {
		if !strings.Contains(full, want) {
			t.Fatalf("unfocused room missing %q", want)
		}
	}

	r = r.WithFocus(john, "john")
	got := r.View(80, 24)
	for _, want := range []string{"status?", "all green"} {
		if !strings.Contains(got, want) {
			t.Fatalf("focused room missing %q, want it kept", want)
		}
	}
	for _, gone := range []string{"still building", "hurry", "manager coordinating"} {
		if strings.Contains(got, gone) {
			t.Fatalf("focused room still shows %q, want it hidden", gone)
		}
	}
	if !strings.Contains(got, "› @john") {
		t.Fatalf("focused room header missing the affordance:\n%s", got)
	}

	r = r.WithFocus("", "")
	back := r.View(80, 24)
	if !strings.Contains(back, "still building") || !strings.Contains(back, "hurry") || !strings.Contains(back, "manager coordinating") {
		t.Fatalf("unfocus did not restore hidden lines")
	}
}

// The reported bug: with the room narrowed to @john, the operator sees their
// own messages but not john's own replies - "sometimes". The
// "sometimes" is a /clear: the room line is attributed by the event's own
// session_id, which claude mints anew mid-process on a /clear (fanout.go), while
// focus is the id Wake spawned under. When they diverge, the focused agent's own
// prose is hidden. Driven through the live path so the frame id and the event id
// can differ the way they do after a /clear.
func TestFocusStillShowsTheAgentsOwnReplyAfterItsSessionIdDrifts(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("john", "iris", "manager")
	john := idOfAgentNamed(t, a, "john")
	iris := idOfAgentNamed(t, a, "iris")
	mgr := idOfAgentNamed(t, a, "manager")

	// Narrow to john by making @john the composer's target.
	a = a.withDraft("@john ")
	if a.room.focus != john {
		t.Fatalf("precondition: @john did not narrow the room (focus=%q, want %q)", a.room.focus, john)
	}

	// john replies. The frame is addressed by the spawn id, but the event's own
	// session_id has drifted - the shape a /clear produces.
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: john, Event: &core.Event{
		Kind: core.KindAssistantText, SessionID: john + "-after-clear", Text: "john here, tests are green",
	}})
	// The manager replies (its id has not drifted) and iris replies.
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: mgr, Event: &core.Event{
		Kind: core.KindAssistantText, SessionID: mgr, Text: "manager coordinating",
	}})
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: iris, Event: &core.Event{
		Kind: core.KindAssistantText, SessionID: iris, Text: "iris still building",
	}})

	out := shown(a)
	if !strings.Contains(out, "tests are green") {
		t.Errorf("the room narrowed to @john hid john's own reply:\n%s", out)
	}
	if strings.Contains(out, "manager coordinating") {
		t.Errorf("the room narrowed to @john leaked the manager's reply:\n%s", out)
	}
	if strings.Contains(out, "iris still building") {
		t.Errorf("the room narrowed to @john leaked iris's reply:\n%s", out)
	}
}

// The manager's own words are not john's thread, but a message it sends john is
// a message to him, and one it sends iris is not. Driven through the live path,
// where the receiver's stream carries the peer message.
func TestFocusHidesTheManagersWordsButKeepsAMessageItSentTheAgent(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("john", "iris", "manager")
	john := idOfAgentNamed(t, a, "john")
	iris := idOfAgentNamed(t, a, "iris")
	mgr := idOfAgentNamed(t, a, "manager")
	a = a.withDraft("@john ")

	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: mgr, Event: &core.Event{
		Kind: core.KindAssistantText, SessionID: mgr, Text: "manager coordinating",
	}})
	a = a.observe(john, core.Event{Kind: core.KindCrossSession, SessionID: john, FromName: "manager", Text: "rebase onto main"})
	a = a.observe(iris, core.Event{Kind: core.KindCrossSession, SessionID: iris, FromName: "manager", Text: "rerun the suite"})

	out := shown(a)
	if !strings.Contains(out, "rebase onto main") {
		t.Errorf("the room narrowed to @john hid what the manager sent john:\n%s", out)
	}
	for _, gone := range []string{"manager coordinating", "rerun the suite"} {
		if strings.Contains(out, gone) {
			t.Errorf("the room narrowed to @john still shows %q:\n%s", gone, out)
		}
	}

	// The same lines draw once the draft is cleared, so the absence above is the filter's.
	wide, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	for _, want := range []string{"manager coordinating", "rerun the suite"} {
		if !strings.Contains(shown(wide), want) {
			t.Errorf("the unfocused room is missing %q, so the narrowed room hiding it proves nothing", want)
		}
	}
}

// Your turn to the manager reached only the manager, so it is no agent's thread:
// a narrowed @john room hides it with the replies it earned, and keeps a turn to
// john and a broadcast that reached him.
func TestFocusHidesYourTurnsToTheManager(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("john", "iris", core.ManagerName)
	sent := []string{"who is stuck?", "@" + core.ManagerName + " spin up two more", "@all ship it", "@john rebase"}
	for _, text := range sent {
		a, _ = pressKey(a.withDraft(text), tea.KeyMsg{Type: tea.KeyEnter})
	}

	wide, _ := pressKey(a.withDraft("@john "), tea.KeyMsg{Type: tea.KeyEsc})
	if out := shown(wide); !strings.Contains(out, "who is stuck?") {
		t.Fatalf("precondition: the unfocused room is missing the manager-bound turn:\n%s", out)
	}
	out := shown(a.withDraft("@john "))
	for _, want := range []string{"ship it", "rebase"} {
		if !strings.Contains(out, want) {
			t.Errorf("the room narrowed to @john hid %q, a turn that reached him:\n%s", want, out)
		}
	}
	for _, gone := range []string{"who is stuck?", "spin up two more"} {
		if strings.Contains(out, gone) {
			t.Errorf("the room narrowed to @john still shows %q, a turn only the manager received:\n%s", gone, out)
		}
	}
	// A broadcast is every agent's, not the first one's.
	if irisOut := shown(a.withDraft("@iris ")); !strings.Contains(irisOut, "ship it") {
		t.Errorf("the room narrowed to @iris hid the broadcast:\n%s", irisOut)
	}
}

// A restored turn is addressed as the live echo was: the one session whose
// transcript holds it, so a manager-bound turn stays out of john's thread while a
// broadcast in two transcripts stays in.
func TestFocusHidesARestoredTurnOnlyTheManagerReceived(t *testing.T) {
	broadcast := newRoomSend(false)
	r := restored(
		[]core.Event{roomTyped("john", "ship it", base, broadcast)},
		[]core.Event{roomTyped("iris", "ship it", base.Add(80*time.Millisecond), broadcast)},
		[]core.Event{roomTyped("mgr", "who is stuck?", base.Add(time.Second), newRoomSend(false).ForSolo())},
	)
	got := r.WithFocus("john", "john").View(80, 24)
	if !strings.Contains(got, "ship it") {
		t.Errorf("the restored broadcast is hidden under @john:\n%s", got)
	}
	if strings.Contains(got, "who is stuck?") {
		t.Errorf("the restored manager-bound turn is drawn under @john:\n%s", got)
	}
}

// A broadcast whose other transcripts did not come back (a dropped batch, a
// cut, a parked agent) leaves one record, and it must not read as a turn to that
// session alone: it still draws under the agents that did receive it.
func TestFocusKeepsARestoredBroadcastWhoseOtherCopiesAreMissing(t *testing.T) {
	r := restored([]core.Event{roomTyped("iris", "ship it", base, newRoomSend(false))})
	if got := r.WithFocus("john", "john").View(80, 24); !strings.Contains(got, "ship it") {
		t.Errorf("a broadcast with one restored copy is hidden under @john:\n%s", got)
	}
}

func TestTypingAtNameNarrowsTheRoom(t *testing.T) {
	base := newRoomApp(t).withSize(200, 40).withAgents("john", "iris")
	john := idOfAgentNamed(t, base, "john")
	iris := idOfAgentNamed(t, base, "iris")

	if got := base.withDraft("@john ").room.focus; got != john {
		t.Fatalf("@john did not focus john: room.focus = %q, want %q", got, john)
	}
	if got := base.withDraft("@iris ").room.focus; got != iris {
		t.Fatalf("@iris did not focus iris: room.focus = %q, want %q", got, iris)
	}
	if got := base.withDraft("hello team").room.focus; got != "" {
		t.Fatalf("an unaddressed draft focused %q, want none", got)
	}
	if got := base.withDraft("@all ship").room.focus; got != "" {
		t.Fatalf("@all (a broadcast) focused %q, want none", got)
	}

	// Open mode widens the message, not the view: @john under open mode is a
	// broadcast, so it must not narrow.
	openApp := base
	openApp.mention = MentionOpen
	if got := openApp.withDraft("@john ").room.focus; got != "" {
		t.Fatalf("@john in open mode focused %q, want none", got)
	}

	// Clearing the draft widens the room again.
	cleared, _ := pressKey(base.withDraft("@john "), tea.KeyMsg{Type: tea.KeyEsc})
	if got := cleared.room.focus; got != "" {
		t.Fatalf("clearing the draft left focus = %q, want none", got)
	}
}

// Focus narrows restored history with no reconstruction: history holds only
// broadcasts and public prose, and the predicate reads them by kind and session
// id. Built the way the daemon delivers it (one transcript per batch; a
// broadcast in several transcripts is what makes a turn public), so
// collapseBroadcasts keeps the broadcast once and opens each session's prose.
func TestFocusNarrowsRestoredHistory(t *testing.T) {
	const john, iris, mgr = "john", "iris", "mgr"
	r := restored(
		[]core.Event{typed(john, "@all status?", base), heard(john, "john green", base.Add(time.Second))},
		[]core.Event{typed(iris, "@all status?", base.Add(80*time.Millisecond)), heard(iris, "iris building", base.Add(time.Second+80*time.Millisecond))},
		[]core.Event{typed(mgr, "@all status?", base.Add(120*time.Millisecond)), heard(mgr, "mgr coordinating", base.Add(time.Second+120*time.Millisecond))},
	)
	r = r.WithFocus(john, "john")
	got := r.View(80, 24)
	for _, want := range []string{"status?", "john green"} {
		if !strings.Contains(got, want) {
			t.Fatalf("focused history missing %q:\n%s", want, got)
		}
	}
	for _, gone := range []string{"iris building", "mgr coordinating"} {
		if strings.Contains(got, gone) {
			t.Fatalf("focused history still shows %q:\n%s", gone, got)
		}
	}
}

// Every keystroke and fleet report retargets the room; one that leaves it
// unfocused must not re-render and yank a scrolled reader - not even by a row.
func TestRetargetingAnUnfocusedRoomDoesNotYankTheReader(t *testing.T) {
	r := NewRoom().SetSize(80, 8)
	for i := 0; i < 40; i++ {
		r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: fmt.Sprintf("line %d body", i)}, Agent{ID: "s1", Name: "a"})
	}
	r = r.ScrollUp(30)
	if r.tr.atBottom() {
		t.Fatal("precondition: expected the reader to be scrolled back")
	}
	scrollBefore := r.tr.scroll
	r = r.WithFocus("", "")
	if r.tr.atBottom() {
		t.Fatal("retargeting an unfocused room yanked the reader to the bottom")
	}
	if r.tr.scroll != scrollBefore {
		t.Fatalf("retargeting an unfocused room moved the scroll %d -> %d", scrollBefore, r.tr.scroll)
	}
}

// A width change re-wraps through renderAll and must re-apply the filter, not
// re-admit hidden lines - the one real risk of the subset render (spec §5).
func TestReWrapUnderFocusKeepsHiddenLinesHidden(t *testing.T) {
	const john, iris = "john-id", "iris-id"
	r := NewRoom().SetSize(80, 24)
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: john, Text: "john green"}, Agent{ID: john, Name: "john"})
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: iris, Text: "iris building"}, Agent{ID: iris, Name: "iris"})
	r = r.WithFocus(john, "john")
	r = r.SetSize(60, 24)
	got := r.View(60, 24)
	if !strings.Contains(got, "john green") {
		t.Fatalf("re-wrap dropped the focused agent's line:\n%s", got)
	}
	if strings.Contains(got, "iris building") {
		t.Fatalf("re-wrap re-admitted a hidden line:\n%s", got)
	}
}

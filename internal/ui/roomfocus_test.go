package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
)

func TestFocusAdmits(t *testing.T) {
	const john, iris, mgr = "john-id", "iris-id", "mgr-id"
	agent := func(id string) roomLine {
		return roomLine{ev: core.Event{Kind: core.KindAssistantText, SessionID: id}}
	}
	user := func(to string) roomLine {
		return roomLine{ev: core.Event{Kind: core.KindUserText}, to: to}
	}
	cases := []struct {
		name  string
		line  roomLine
		focus string
		want  bool
	}{
		{"unfocused admits agent prose", agent(iris), "", true},
		{"unfocused admits any user line", user(iris), "", true},
		{"focused admits john's prose", agent(john), john, true},
		{"focused hides iris's prose", agent(iris), john, false},
		{"focused admits the manager", agent(mgr), john, true},
		{"focused admits a broadcast", user(""), john, true},
		{"focused admits your message to john", user(john), john, true},
		{"focused hides your message to iris", user(iris), john, false},
		{"focused admits john's permission ask", roomLine{ev: core.Event{Kind: core.KindPermissionRequest, SessionID: john}}, john, true},
		{"focused hides iris's turn end", roomLine{ev: core.Event{Kind: core.KindTurnEnd, SessionID: iris}}, john, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := focusAdmits(c.line, c.focus, mgr); got != c.want {
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

	r = r.WithFocus(john, "john", mgr)
	got := r.View(80, 24)
	for _, want := range []string{"status?", "all green", "manager coordinating"} {
		if !strings.Contains(got, want) {
			t.Fatalf("focused room missing %q, want it kept", want)
		}
	}
	for _, gone := range []string{"still building", "hurry"} {
		if strings.Contains(got, gone) {
			t.Fatalf("focused room still shows %q, want it hidden", gone)
		}
	}
	if !strings.Contains(got, "› @john") {
		t.Fatalf("focused room header missing the affordance:\n%s", got)
	}

	r = r.WithFocus("", "", mgr)
	back := r.View(80, 24)
	if !strings.Contains(back, "still building") || !strings.Contains(back, "hurry") {
		t.Fatalf("unfocus did not restore hidden lines")
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
	r = r.WithFocus(john, "john", mgr)
	got := r.View(80, 24)
	for _, want := range []string{"status?", "john green", "mgr coordinating"} {
		if !strings.Contains(got, want) {
			t.Fatalf("focused history missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "iris building") {
		t.Fatalf("focused history still shows iris's restored prose:\n%s", got)
	}
}

// A fleet report starts the manager (managerID "" -> id) while the room is
// unfocused; that must not re-render and yank a scrolled reader - not even by a
// row - because an unfocused view admits everything regardless of the manager.
func TestManagerStartWhileUnfocusedDoesNotYankTheReader(t *testing.T) {
	r := NewRoom().SetSize(80, 8)
	for i := 0; i < 40; i++ {
		r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: fmt.Sprintf("line %d body", i)}, Agent{ID: "s1", Name: "a"})
	}
	r = r.ScrollUp(30)
	if r.tr.atBottom() {
		t.Fatal("precondition: expected the reader to be scrolled back")
	}
	scrollBefore := r.tr.scroll
	r = r.WithFocus("", "", "mgr-id")
	if r.tr.atBottom() {
		t.Fatal("a manager start while unfocused yanked the reader to the bottom")
	}
	if r.tr.scroll != scrollBefore {
		t.Fatalf("a manager start while unfocused moved the scroll %d -> %d", scrollBefore, r.tr.scroll)
	}
}

// A width change re-wraps through renderAll and must re-apply the filter, not
// re-admit hidden lines - the one real risk of the subset render (spec §5).
func TestReWrapUnderFocusKeepsHiddenLinesHidden(t *testing.T) {
	const john, iris, mgr = "john-id", "iris-id", "mgr-id"
	r := NewRoom().SetSize(80, 24)
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: john, Text: "john green"}, Agent{ID: john, Name: "john"})
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: iris, Text: "iris building"}, Agent{ID: iris, Name: "iris"})
	r = r.WithFocus(john, "john", mgr)
	r = r.SetSize(60, 24)
	got := r.View(60, 24)
	if !strings.Contains(got, "john green") {
		t.Fatalf("re-wrap dropped the focused agent's line:\n%s", got)
	}
	if strings.Contains(got, "iris building") {
		t.Fatalf("re-wrap re-admitted a hidden line:\n%s", got)
	}
}

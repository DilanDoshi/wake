package ui

import (
	"strings"
	"testing"

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

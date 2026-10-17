package ui

// A lone `@team` narrows the group chat the way a lone `@name` does: to the
// thread of the agents it names. A team's thread is the union of its members'
// threads, so every rule an agent's narrowing keeps (its prose, the manager's,
// the operator's broadcasts and turns aimed at it, the peer messages it sent or
// received) holds for each member.

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestFocusAdmitsATeamsThread(t *testing.T) {
	const a, b, out, mgr = "a-id", "b-id", "out-id", "mgr-id"
	team := joinIDs([]string{b, a})
	agent := func(id string) roomLine {
		return roomLine{ev: core.Event{Kind: core.KindAssistantText, SessionID: id}}
	}
	user := func(to string) roomLine { return roomLine{ev: core.Event{Kind: core.KindUserText}, to: to} }
	peer := func(from, to string) roomLine {
		return roomLine{ev: core.Event{Kind: core.KindCrossSession, SessionID: to}, by: Agent{ID: from}}
	}
	cases := []struct {
		name string
		line roomLine
		want bool
	}{
		{"the first member's prose", agent(a), true},
		{"the second member's prose", agent(b), true},
		{"the manager's prose", agent(mgr), true},
		{"an outsider's prose", agent(out), false},
		{"a broadcast", user(""), true},
		{"a turn to the first member", user(a), true},
		{"a turn to the whole team", user(joinIDs([]string{a, b})), true},
		{"a turn to a team that includes one member", user(joinIDs([]string{a, out})), true},
		{"a turn to an outsider", user(out), false},
		{"a turn to another team", user(joinIDs([]string{out, "other-id"})), false},
		{"a message a member sent", peer(a, out), true},
		{"a message a member received", peer(out, b), true},
		{"a message between outsiders", peer(out, "other-id"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := focusAdmits(c.line, team, mgr); got != c.want {
				t.Fatalf("focusAdmits(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

// A lone agent's key is its own id, so one agent's narrowing compares and
// filters exactly as it did before a team could be one; a team's key does not
// depend on the order the fleet lists its members in, or every report that
// reshuffles the roster would read as a new target and redraw the room.
func TestAFocusKeyIsTheIdForOneAgentAndOrderFreeForATeam(t *testing.T) {
	if got := joinIDs([]string{"a-id"}); got != "a-id" {
		t.Errorf("the key of one agent = %q, want its id", got)
	}
	if joinIDs([]string{"a-id", "b-id"}) != joinIDs([]string{"b-id", "a-id"}) {
		t.Error("a team's key changed with the order its members were listed in")
	}
	if joinIDs(nil) != "" {
		t.Error("an empty team has a key; it would narrow the room to nobody")
	}
}

// teamRoom is the room over two backend agents, one frontend agent, one with no
// team and the manager, each having said a line.
func teamRoom(t *testing.T) App {
	t.Helper()
	a := newRoomApp(t).withSize(200, 40)
	a = a.applyStatus(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", Team: "backend", Cwd: "/tmp/harbor", State: rpc.StateIdle},
		{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
		{ID: "s3", Name: "delta", Team: "frontend", State: rpc.StateIdle},
		{ID: "s4", Name: "iris", State: rpc.StateIdle},
		{ID: "s5", Name: core.ManagerName, State: rpc.StateIdle},
	}})
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

func assertRoomShows(t *testing.T, a App, shows, hides []string) {
	t.Helper()
	out := shown(a)
	var wrong []string
	for _, s := range shows {
		if !strings.Contains(out, s) {
			wrong = append(wrong, "hid "+s)
		}
	}
	for _, s := range hides {
		if strings.Contains(out, s) {
			wrong = append(wrong, "drew "+s)
		}
	}
	if len(wrong) > 0 {
		t.Errorf("the room %s:\n%s", strings.Join(wrong, ", "), out)
	}
}

func TestTypingAtTeamNarrowsTheRoomToItsMembers(t *testing.T) {
	a := teamRoom(t).withDraft("@backend ")
	if want := joinIDs([]string{"s1", "s2"}); a.room.focus != want {
		t.Fatalf("@backend focused %q, want its members %q", a.room.focus, want)
	}
	assertRoomShows(t, a,
		[]string{"sydney migrations green", "thea api rebased", "manager coordinating", "› @backend"},
		[]string{"delta bundle building", "iris reading docs"})

	// Typed as the operator may: another case, and the room's own default.
	if got := teamRoom(t).withDraft("@Backend ").room.focus; got != a.room.focus {
		t.Errorf("@Backend focused %q, want what @backend does", got)
	}
	// A lone agent's narrowing is untouched.
	if got := teamRoom(t).withDraft("@sydney ").room.focus; got != "s1" {
		t.Errorf("@sydney focused %q, want its own id", got)
	}
}

// Open mention mode widens a message to the fleet, so a lone `@name` there
// narrows nothing. A team send is not widened by it, so the thread it reaches
// is the one to read.
func TestATeamNarrowsInOpenMentionModeToo(t *testing.T) {
	a := teamRoom(t)
	a.mention = MentionOpen
	if got := a.withDraft("@backend ").room.focus; got != joinIDs([]string{"s1", "s2"}) {
		t.Errorf("@backend in open mode focused %q, want its members", got)
	}
	if got := a.withDraft("@sydney ").room.focus; got != "" {
		t.Errorf("@sydney in open mode focused %q, want none", got)
	}
}

func TestAWordThatNamesNoLiveTeamDoesNotNarrow(t *testing.T) {
	a := teamRoom(t)
	if got := a.withDraft("@nosuch ").room.focus; got != "" {
		t.Errorf("an unknown @team focused %q, want none", got)
	}
	// Every member parked: the team has no live agent, so it is no route either.
	parked := a.applyStatus(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateParked},
		{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateParked},
		{ID: "s5", Name: core.ManagerName, State: rpc.StateIdle},
	}})
	if got := parked.withDraft("@backend ").room.focus; got != "" {
		t.Errorf("a team with no live member focused %q, want none", got)
	}
	// Not a mention at all: text after the word is not what it addresses.
	if got := a.withDraft("ask @backend ").room.focus; got != "" {
		t.Errorf("a team named mid-draft focused %q, want none", got)
	}
}

// A report moves who is on the team while the draft stays put, and the room
// follows it: retargeting runs on a report, and the key is the members.
func TestATeamsThreadFollowsItsMembership(t *testing.T) {
	a := teamRoom(t).withDraft("@backend ")
	a = a.applyStatus(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", Team: "frontend", State: rpc.StateIdle},
		{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
		{ID: "s3", Name: "delta", Team: "backend", State: rpc.StateIdle},
		{ID: "s4", Name: "iris", State: rpc.StateIdle},
		{ID: "s5", Name: core.ManagerName, State: rpc.StateIdle},
	}})
	assertRoomShows(t, a,
		[]string{"thea api rebased", "delta bundle building", "manager coordinating"},
		[]string{"sydney migrations green", "iris reading docs"})
}

// The fleet lists members by attention, so a member starting work moves ahead
// of the idle one. That is no new target: the ⌃A override survives it, as it
// survives typing into the same draft.
func TestAMemberChangingStateKeepsTheOverride(t *testing.T) {
	a := teamRoom(t).withDraft("@backend ")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlA})
	if a.room.effectiveFocus() != "" {
		t.Fatalf("precondition: ⌃A did not widen (effectiveFocus=%q)", a.room.effectiveFocus())
	}
	before := a.route("@backend ").Targets
	a = a.applyStatus(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateIdle},
		{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateWorking},
		{ID: "s5", Name: core.ManagerName, State: rpc.StateIdle},
	}})
	if after := a.route("@backend ").Targets; slices.Equal(before, after) {
		t.Fatalf("precondition: the report did not reorder the team (%v)", after)
	}
	if a.room.effectiveFocus() != "" {
		t.Errorf("a member starting work re-narrowed the room (effectiveFocus=%q)", a.room.effectiveFocus())
	}
}

func TestCtrlAWidensATeamNarrowingAndClearingTheDraftEndsIt(t *testing.T) {
	a := teamRoom(t).withDraft("@backend ")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlA})
	assertRoomShows(t, a, []string{"sydney migrations green", "delta bundle building", "iris reading docs"}, nil)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlA})
	assertRoomShows(t, a, []string{"thea api rebased"}, []string{"delta bundle building"})

	cleared, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if cleared.room.focus != "" {
		t.Errorf("clearing the draft left focus = %q", cleared.room.focus)
	}
	assertRoomShows(t, cleared, []string{"delta bundle building"}, nil)

	// With the default off a lone @team only records its target, like @name.
	off := teamRoom(t)
	off.room = off.room.WithNarrowDefault(false)
	off = off.withDraft("@backend ")
	assertRoomShows(t, off, []string{"delta bundle building"}, nil)
	off, _ = pressKey(off, tea.KeyMsg{Type: tea.KeyCtrlA})
	assertRoomShows(t, off, nil, []string{"delta bundle building"})
}

// A team has no one directory, model or context to put in the bar over the
// composer, and the manager's would say something about someone else.
func TestATeamNarrowingDrawsNoAgentBar(t *testing.T) {
	a := teamRoom(t)
	if one := a.withDraft("@sydney "); !strings.Contains(one.room.bar, "harbor") {
		t.Fatalf("precondition: a lone @sydney drew no bar for its directory:\n%s", one.room.bar)
	}
	if got := a.withDraft("@backend ").room.bar; got != "" {
		t.Errorf("a team narrowing drew a bar:\n%s", got)
	}
}

// A team of one is that agent's thread, so its bar is the agent's own: the key
// is the member's id, and "a team has no one directory" holds from two members.
func TestATeamOfOneDrawsItsMembersBar(t *testing.T) {
	a := teamRoom(t).applyStatus(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", Team: "backend", Cwd: "/tmp/harbor", State: rpc.StateIdle},
		{ID: "s2", Name: "thea", State: rpc.StateIdle},
		{ID: "s3", Name: "delta", Team: "frontend", State: rpc.StateIdle},
		{ID: "s5", Name: core.ManagerName, State: rpc.StateIdle},
	}})
	one := a.withDraft("@backend ")
	if one.room.focus != "s1" || !strings.Contains(one.room.bar, "harbor") {
		t.Errorf("a team of one: focus %q, bar %q; want the member's own", one.room.focus, one.room.bar)
	}
}

// who is on a team changes under a standing draft - a member the manager spawns
// into it, one parked, one woken - with the operator doing nothing. That is no
// new target: the reader keeps their place and the ⌃A override survives.
func addedMember(a App) App {
	return a.applyStatus(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", Team: "backend", Cwd: "/tmp/harbor", State: rpc.StateIdle},
		{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
		{ID: "s3", Name: "delta", Team: "frontend", State: rpc.StateIdle},
		{ID: "s4", Name: "iris", State: rpc.StateIdle},
		{ID: "s5", Name: core.ManagerName, State: rpc.StateIdle},
		{ID: "s6", Name: "kit", Team: "backend", State: rpc.StateIdle},
	}})
}

func TestAMemberJoiningKeepsAScrolledReadersPlace(t *testing.T) {
	a := teamRoom(t)
	for i := range 80 {
		a = a.applyFrame(frameText("s1", fmt.Sprintf("sydney note %02d", i)))
	}
	a = a.withDraft("@backend ")
	a.room = a.room.ScrollUp(30)
	if a.room.tr.atBottom() {
		t.Fatal("precondition: the reader is at the bottom")
	}
	top := func(a App) string { return a.room.tr.lines.slice(a.room.tr.scroll, a.room.tr.scroll+1)[0] }
	before := top(a)

	a = addedMember(a)
	if want := joinIDs([]string{"s1", "s2", "s6"}); a.room.focus != want {
		t.Fatalf("the new member did not join the thread: focus %q, want %q", a.room.focus, want)
	}
	if a.room.tr.atBottom() {
		t.Error("a member joining the team threw the reader to the newest line")
	}
	if got := top(a); got != before {
		t.Errorf("the reader's top line changed from %q to %q", before, got)
	}
}

func TestAMemberJoiningKeepsTheOverride(t *testing.T) {
	a := teamRoom(t).withDraft("@backend ")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlA})
	a = addedMember(a)
	if a.room.effectiveFocus() != "" {
		t.Errorf("a member joining re-narrowed the room the operator had widened (effectiveFocus=%q)", a.room.effectiveFocus())
	}
}

// A reader sitting on the departing member's lines has nowhere to keep: they go
// to the newest line, not back to the oldest history.
func TestAMemberLeavingUnderTheReaderSendsThemToTheNewestLine(t *testing.T) {
	a := teamRoom(t)
	for i := range 60 {
		a = a.applyFrame(frameText("s1", fmt.Sprintf("sydney note %02d", i)))
	}
	for i := range 60 {
		a = a.applyFrame(frameText("s2", fmt.Sprintf("thea note %02d", i)))
	}
	a = a.withDraft("@backend ")
	a.room = a.room.ScrollUp(250)
	if top := a.room.tr.lines.slice(a.room.tr.scroll, a.room.tr.scroll+1)[0]; a.room.tr.atBottom() || !strings.Contains(top, "sydney") {
		t.Fatalf("precondition: the reader is not on one of sydney's lines: %q", top)
	}
	a = a.applyStatus(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", Team: "frontend", State: rpc.StateIdle},
		{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
		{ID: "s5", Name: core.ManagerName, State: rpc.StateIdle},
	}})
	if a.room.focus != "s2" {
		t.Fatalf("precondition: sydney did not leave the team (focus %q)", a.room.focus)
	}
	if !a.room.tr.atBottom() {
		t.Errorf("a member leaving left the reader at line %d of %d, want the newest line", a.room.tr.scroll, a.room.tr.bottom())
	}
}

// A name can be both an agent's and a team's (the router lets the agent win), so
// a draft can move from one to the other under its own name when that agent
// parks. That is a different target - the override does not carry over to it.
func TestAnAgentAndATeamOfTheSameNameAreNotOneTarget(t *testing.T) {
	r := NewRoom().SetSize(80, 24).WithFocus("a1", "backend", "mgr")
	r = r.ToggleNarrow()
	if r.effectiveFocus() != "" {
		t.Fatal("precondition: ⌃A did not widen")
	}
	r = r.WithFocus(joinIDs([]string{"s1", "s2"}), "backend", "mgr")
	if r.effectiveFocus() != joinIDs([]string{"s1", "s2"}) {
		t.Errorf("the team took the agent's override: effectiveFocus=%q", r.effectiveFocus())
	}
}

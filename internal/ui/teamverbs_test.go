package ui

import (
	"maps"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// Acting on a whole team: /park @team, /quit @team behind a confirm, and
// /resume <team>. Membership is the set `@team` reaches - the live, non-manager
// sessions wearing the tag - and a name resolves the way core.Resolve routes it:
// a live agent, then a live team, then a parked agent.

// backendRoom is a room whose backend team is sydney and thea, with delta on
// another team, iris on none, and a manager wearing backend's tag - which @backend
// never reaches, so no team verb may either.
func backendRoom(t *testing.T, sessions ...rpc.SessionStatus) App {
	t.Helper()
	base := []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateIdle},
		{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateWorking},
		{ID: "s3", Name: "delta", Team: "frontend", State: rpc.StateIdle},
		{ID: "s4", Name: "iris", State: rpc.StateIdle},
		{ID: "s5", Name: core.ManagerName, Team: "backend", State: rpc.StateIdle},
	}
	// Teams is the daemon's order, which the @ menu offers teams from.
	return newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true, Sessions: append(base, sessions...), Teams: []string{"backend", "frontend"},
	}})
}

// kindIDs is the session each frame of one kind names, in the order written.
func kindIDs(frames []rpc.Frame, kind string) []string {
	var ids []string
	for _, f := range frames {
		if f.Kind == kind {
			ids = append(ids, f.SessionID)
		}
	}
	return ids
}

// keyed delivers one key to App.key, the handler Update calls, without Update's
// own wrapper - whose notice linger would batch around the write under test.
func keyed(a App, k tea.KeyMsg) (App, tea.Cmd) {
	m, cmd, _ := a.key(k)
	return m.(App), cmd
}

// --- /park -----------------------------------------------------------------

func TestParkIsAWakeCommand(t *testing.T) {
	a := backendRoom(t)
	if _, _, handled := a.slash(parkVerb + " @backend"); !handled {
		t.Fatal("the router did not take /park, so it went to the agent as a message")
	}
}

// Every live member, one frame each, in one command - /reauth's shape - and
// never the manager, which @backend does not reach either.
func TestParkATeamParksEveryLiveMemberInOneWrite(t *testing.T) {
	a := backendRoom(t)

	next, cmd, _ := a.slash(parkVerb + " @backend")

	got := kindIDs(sentFrames(t, next, cmd), rpc.FramePark)
	if !slices.Equal(got, []string{"s1", "s2"}) {
		t.Errorf("/park @backend parked %v, want sydney and thea (s1 s2) and never the manager", got)
	}
	for _, id := range []string{"s1", "s2"} {
		if _, waiting := next.parking[id]; !waiting {
			t.Errorf("%s was not awaited as parking, so its report cannot confirm the park", id)
		}
	}
	if note := latestNotice(t); !strings.Contains(note, "parking 2 @backend agents") {
		t.Errorf("the keypress said %q, want it to name the ask", note)
	}
}

// A blocked member is skipped for park's own reason - closing stdin under its ask
// reads as a denial - and the notice names it and how to get it parked.
func TestParkATeamSkipsABlockedMemberAndSaysWhy(t *testing.T) {
	a := backendRoom(t).withRoster(rpc.SessionStatus{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateBlocked})

	next, cmd, _ := a.slash(parkVerb + " @backend")

	if got := kindIDs(sentFrames(t, next, cmd), rpc.FramePark); !slices.Equal(got, []string{"s1"}) {
		t.Errorf("/park @backend parked %v, want only sydney: thea is blocked on an ask", got)
	}
	note := latestNotice(t)
	for _, want := range []string{"@thea", "blocked", parkVerb + " @thea"} {
		if !strings.Contains(note, want) {
			t.Errorf("the notice %q does not name %q", note, want)
		}
	}
}

func TestParkATeamWhoseLiveMembersAreAllBlockedParksNothing(t *testing.T) {
	a := backendRoom(t).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateBlocked},
		rpc.SessionStatus{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateBlocked},
	).withDraft(parkVerb + " @backend")

	next, cmd, _ := a.slash(parkVerb + " @backend")

	if cmd != nil {
		t.Fatalf("a team whose members are all blocked wrote %+v", sentFrames(t, next, cmd))
	}
	if !strings.Contains(latestNotice(t), "blocked") {
		t.Errorf("the refusal %q does not say why", latestNotice(t))
	}
	if next.composer().Value() == "" {
		t.Error("a refused /park cleared the draft, so the command cannot be fixed and resent")
	}
}

// The report confirms, once for the team: per-member lines would fight over the
// one notice slot and the last would name one agent.
func TestParkATeamConfirmsOnceWhenEveryMemberIsParked(t *testing.T) {
	a := backendRoom(t)
	a, _, _ = a.slash(parkVerb + " @backend")

	half := a.withRoster(rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateParked})
	if note := latestNotice(t); strings.Contains(note, "is parked") {
		t.Errorf("one of two members parking said %q; the team is not parked yet", note)
	}

	_ = half.withRoster(rpc.SessionStatus{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateParked})
	want := "@backend is parked - " + resumeVerb + " @backend brings it back"
	if note := latestNotice(t); note != want {
		t.Errorf("the team's park confirmed as %q, want %q", note, want)
	}
}

// A one-agent park is still confirmed per agent.
func TestParkOneAgentByNameIsTheCtrlCPark(t *testing.T) {
	a := backendRoom(t)

	next, cmd, _ := a.slash(parkVerb + " @sydney")

	if got := kindIDs(sentFrames(t, next, cmd), rpc.FramePark); !slices.Equal(got, []string{"s1"}) {
		t.Fatalf("/park @sydney parked %v, want s1", got)
	}
	_ = next.withRoster(rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateParked})
	if note := latestNotice(t); !strings.Contains(note, "@sydney is parked") {
		t.Errorf("a one-agent park confirmed as %q", note)
	}
}

func TestBareParkInAConversationParksIt(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(200, 40)

	next, cmd, _ := a.slash(parkVerb)

	if f := sentFrame(t, next, cmd); f.Kind != rpc.FramePark || f.SessionID != "s1" {
		t.Errorf("bare /park in alex's pane wrote %q for %q, want a park of s1", f.Kind, f.SessionID)
	}
}

// core.Resolve's order: a live agent wins its name, then a team, then a parked
// agent. The menu and the router already agree on it; the verbs must too.
func TestALiveAgentNamedLikeATeamWinsThePark(t *testing.T) {
	a := backendRoom(t, rpc.SessionStatus{ID: "s6", Name: "backend", State: rpc.StateIdle})

	next, cmd, _ := a.slash(parkVerb + " @backend")

	if got := kindIDs(sentFrames(t, next, cmd), rpc.FramePark); !slices.Equal(got, []string{"s6"}) {
		t.Errorf("/park @backend parked %v, want only the live agent named backend", got)
	}
}

func TestALiveTeamWinsOverAParkedAgentOfItsName(t *testing.T) {
	a := backendRoom(t, rpc.SessionStatus{ID: "s6", Name: "backend", State: rpc.StateParked})

	next, cmd, _ := a.slash(parkVerb + " @backend")

	if got := kindIDs(sentFrames(t, next, cmd), rpc.FramePark); !slices.Equal(got, []string{"s1", "s2"}) {
		t.Errorf("/park @backend parked %v, want the live team over the parked agent", got)
	}
}

func TestParkATeamWithNothingRunningSaysSoAndNamesResume(t *testing.T) {
	a := backendRoom(t).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateParked},
	)

	next, cmd, _ := a.slash(parkVerb + " @backend")

	if cmd != nil {
		t.Fatalf("a team with nothing running wrote %+v", sentFrames(t, next, cmd))
	}
	if note := latestNotice(t); !strings.Contains(note, resumeVerb+" @backend") {
		t.Errorf("the notice %q does not point at %s @backend", note, resumeVerb)
	}
}

// --- the room's @team bridge --------------------------------------------------

// `@backend /park` and `@backend /quit` fold into the typed forms, as `@sydney
// /quit` already folds into `/quit @sydney`.
func TestTheRoomFoldsATeamParkIntoTheTypedForm(t *testing.T) {
	a := backendRoom(t)

	m, cmd := a.withDraft("@backend " + parkVerb).submit(false)

	if got := kindIDs(sentFrames(t, m.(App), cmd), rpc.FramePark); !slices.Equal(got, []string{"s1", "s2"}) {
		t.Errorf("@backend /park parked %v, want both members", got)
	}
}

func TestTheRoomFoldsATeamQuitIntoTheConfirm(t *testing.T) {
	a := backendRoom(t)

	m, _ := a.withDraft("@backend " + quitVerb).submit(false)

	next := m.(App)
	if !next.picker.Open() || next.picker.Word != quitCommand {
		t.Fatalf("@backend /quit opened %+v, want the quit confirm", next.picker)
	}
}

// Every other Wake target-command still refuses a team: it is per-agent, and the
// bridge would hand it to N claude processes as literal text. Derived from the
// two sets, so a command added to either is held to the rule.
func TestEveryOtherTargetCommandAtATeamIsStillRefused(t *testing.T) {
	for word := range roomTargetCommands {
		if _, team := teamTargetCommands[word]; team {
			continue
		}
		t.Run(word, func(t *testing.T) {
			a := backendRoom(t)
			m, cmd := a.withDraft("@backend /" + word + " x").submit(false)
			if cmd != nil {
				t.Fatalf("@backend /%s wrote %+v; a per-agent command may not fan out", word, sentFrames(t, m.(App), cmd))
			}
			if note := latestNotice(t); !strings.Contains(note, "can't fan out") {
				t.Errorf("@backend /%s said %q, want the team refusal", word, note)
			}
		})
	}
}

// --- /quit @team --------------------------------------------------------------

func TestQuitATeamOpensAConfirmWithTheCursorOnNeverMind(t *testing.T) {
	a := backendRoom(t)

	next, cmd, _ := a.slash(quitVerb + " @backend")

	if cmd != nil {
		t.Fatalf("/quit @backend stopped something before any confirm: %+v", sentFrames(t, next, cmd))
	}
	p := next.picker
	if !p.Open() || p.Word != quitCommand {
		t.Fatalf("/quit @backend opened %+v, want the quit confirm", p)
	}
	if p.Options[p.Cursor] != quitNeverMind {
		t.Errorf("the confirm opened on %q, want %q: ↵ alone must do nothing", p.Options[p.Cursor], quitNeverMind)
	}
	if !slices.Equal(p.Targets, []string{"s1", "s2"}) {
		t.Errorf("the confirm targets %v, want sydney and thea and never the manager", p.Targets)
	}
	view := shown(next)
	for _, want := range []string{"@sydney", "@thea", "for good"} {
		if !strings.Contains(view, want) {
			t.Errorf("the confirm does not show %q:\n%s", want, view)
		}
	}
}

// The burst the confirm exists for: ↵ accepts the completion, ↵ sends the
// command, and a third ↵ must land on never mind.
func TestEnterThreeTimesFromAPartialTeamQuitStopsNothing(t *testing.T) {
	a := backendRoom(t).withDraft(quitVerb + " @back")

	enter := tea.KeyMsg{Type: tea.KeyEnter}
	a, _ = keyed(a, enter)
	if got := a.composer().Value(); got != quitVerb+" @backend " {
		t.Fatalf("the first ↵ left %q, want the completion accepted", got)
	}
	a, opened := keyed(a, enter)
	if !a.picker.Open() {
		t.Fatalf("the second ↵ did not open the confirm")
	}
	a, third := keyed(a, enter)
	for i, cmd := range []tea.Cmd{opened, third} {
		if cmd != nil {
			t.Errorf("↵ number %d wrote %+v; nothing may be stopped before ↓ ↵", i+2, sentFrames(t, a, cmd))
		}
	}
	if a.picker.Open() {
		t.Error("↵ on never mind left the confirm up")
	}
}

// Targets are the ones the confirm named, never re-resolved, and only those still
// live are stopped: one parked since is left alone, and one who joined is not
// touched.
func TestConfirmingATeamQuitStopsTheNamedTargetsStillLive(t *testing.T) {
	a := backendRoom(t)
	a, _, _ = a.slash(quitVerb + " @backend")
	a = a.withRoster(
		rpc.SessionStatus{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s6", Name: "otto", Team: "backend", State: rpc.StateIdle},
	)

	a, _ = keyed(a, tea.KeyMsg{Type: tea.KeyDown})
	next, cmd := keyed(a, tea.KeyMsg{Type: tea.KeyEnter})

	if got := kindIDs(sentFrames(t, next, cmd), rpc.FrameStop); !slices.Equal(got, []string{"s1"}) {
		t.Errorf("the confirm stopped %v, want only sydney: thea parked since, otto was never named", got)
	}
	if _, asked := next.quitting["s1"]; !asked {
		t.Error("sydney's stop was not awaited, so its ending cannot drop the row")
	}
}

func TestATeamQuitDropsTheRowsOnceTheEndingsLand(t *testing.T) {
	a := backendRoom(t)
	a, _, _ = a.slash(quitVerb + " @backend")
	a, _ = keyed(a, tea.KeyMsg{Type: tea.KeyDown})
	a, _ = keyed(a, tea.KeyMsg{Type: tea.KeyEnter})

	after := a.withRoster(
		rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateEnded},
		rpc.SessionStatus{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateEnded},
	)

	for _, id := range []string{"s1", "s2"} {
		if _, ok := after.fleet.Agent(id); ok {
			t.Errorf("%s is still in the fleet after its quit ended it", id)
		}
	}
}

func TestEscClosesTheQuitConfirmWithNothingStopped(t *testing.T) {
	a := backendRoom(t)
	a, _, _ = a.slash(quitVerb + " @backend")
	if !a.picker.Open() {
		t.Fatal("test precondition: the confirm did not open")
	}

	next, cmd := keyed(a, tea.KeyMsg{Type: tea.KeyEsc})

	if next.picker.Open() {
		t.Error("esc left the quit confirm up")
	}
	if cmd != nil {
		t.Errorf("esc on the quit confirm wrote %+v", sentFrames(t, next, cmd))
	}
}

// A picker is drawn only over the pane that holds the keys, so one left open when
// the keys move would take ↓ and ↵ there, undrawn.
func TestTheQuitConfirmClosesWhenTheKeysLeaveItsPane(t *testing.T) {
	a := backendRoom(t).WithOpenDM("s4", "iris")
	a = a.refocus("")
	a, _, _ = a.slash(quitVerb + " @backend")
	if !a.picker.Open() {
		t.Fatal("test precondition: the confirm did not open")
	}

	next := a.refocus("s4")

	if next.picker.Open() {
		t.Error("the confirm stayed open, undrawn, after the keys moved to another pane")
	}
}

// ⌃C is not swallowed: it closes the confirm and parks, as it does over the
// resume picker - killswitch.go needs its first press to visibly do something.
func TestCtrlCOverTheQuitConfirmClosesItAndParks(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s4", "iris").withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s4", Name: "iris", State: rpc.StateIdle},
	)
	a, _, _ = a.slash(quitVerb + " @backend")
	if !a.picker.Open() {
		t.Fatal("test precondition: the confirm did not open")
	}

	next, cmd := keyed(a, tea.KeyMsg{Type: tea.KeyCtrlC})

	if next.picker.Open() {
		t.Error("⌃C left the quit confirm up")
	}
	if f := sentFrame(t, next, cmd); f.Kind != rpc.FramePark || f.SessionID != "s4" {
		t.Errorf("⌃C over the confirm wrote %q for %q, want iris parked", f.Kind, f.SessionID)
	}
}

func TestQuitATeamSaysItsParkedMembersStayParked(t *testing.T) {
	a := backendRoom(t, rpc.SessionStatus{ID: "s6", Name: "otto", Team: "backend", State: rpc.StateParked})

	_, _, _ = a.slash(quitVerb + " @backend")

	note := latestNotice(t)
	for _, want := range []string{"@otto", resumeVerb + " @backend"} {
		if !strings.Contains(note, want) {
			t.Errorf("the notice %q does not name %q", note, want)
		}
	}
}

func TestALiveAgentNamedLikeATeamIsQuitWithoutTheTeamConfirm(t *testing.T) {
	a := backendRoom(t, rpc.SessionStatus{ID: "s6", Name: "backend", State: rpc.StateIdle})

	next, cmd, _ := a.slash(quitVerb + " @backend")

	if next.picker.Open() {
		t.Fatal("/quit @backend opened the team confirm over a live agent of that name")
	}
	if f := sentFrame(t, next, cmd); f.Kind != rpc.FrameStop || f.SessionID != "s6" {
		t.Errorf("/quit @backend wrote %q for %q, want the agent s6 stopped", f.Kind, f.SessionID)
	}
}

// --- /resume <team> -----------------------------------------------------------

// Every parked member, from the roster and from a previous daemon's park book
// alike, and never another team's or the manager.
func TestResumeATeamWakesEveryParkedMember(t *testing.T) {
	for _, arg := range []string{"backend", "@backend"} {
		t.Run(arg, func(t *testing.T) {
			a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
				Running: true,
				Sessions: []rpc.SessionStatus{
					{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateParked},
					{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateParked},
					{ID: "s3", Name: "delta", Team: "frontend", State: rpc.StateParked},
					{ID: "s4", Name: "iris", Team: "backend", State: rpc.StateIdle},
					{ID: "s5", Name: core.ManagerName, Team: "backend", State: rpc.StateParked},
				},
				Parked: []rpc.SessionStatus{{ID: "s7", Name: "otto", Team: "backend", State: rpc.StateParked}},
			}})

			next, cmd, _ := a.slash(resumeVerb + " " + arg)

			got := kindIDs(sentFrames(t, next, cmd), rpc.FrameWake)
			slices.Sort(got)
			if !slices.Equal(got, []string{"s1", "s2", "s7"}) {
				t.Errorf("/resume %s woke %v, want sydney, thea and the book's otto", arg, got)
			}
			if note := latestNotice(t); !strings.Contains(note, "@backend") {
				t.Errorf("the notice %q does not name the team", note)
			}
		})
	}
}

// A parked agent wins its name over a team, the order everything else resolves in.
func TestAParkedAgentNamedLikeATeamWinsTheResume(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s6", Name: "backend", State: rpc.StateParked},
	)

	next, cmd, _ := a.slash(resumeVerb + " backend")

	if got := kindIDs(sentFrames(t, next, cmd), rpc.FrameWake); !slices.Equal(got, []string{"s6"}) {
		t.Errorf("/resume backend woke %v, want only the parked agent named backend", got)
	}
}

func TestResumeOfATeamWithNothingParkedSaysNotParked(t *testing.T) {
	a := backendRoom(t, rpc.SessionStatus{ID: "s6", Name: "otto", State: rpc.StateParked})

	next, cmd, _ := a.slash(resumeVerb + " backend")

	if cmd != nil {
		t.Fatalf("/resume backend with no parked member wrote %+v", sentFrames(t, next, cmd))
	}
	if note := latestNotice(t); !strings.Contains(note, "is not parked") {
		t.Errorf("the notice %q does not say nothing of backend is parked", note)
	}
}

// --- completion ---------------------------------------------------------------

func TestTheSlashMenuOffersPark(t *testing.T) {
	a := backendRoom(t).withDraft("/pa")

	if !slices.Contains(a.completion.offers, parkVerb) {
		t.Errorf("/pa offered %v, want %s", a.completion.offers, parkVerb)
	}
}

func TestTeamVerbsOfferALiveTeam(t *testing.T) {
	for _, verb := range []string{parkVerb, quitVerb} {
		a := backendRoom(t).withDraft(verb + " @back")
		if !slices.Contains(a.completion.offers, "@backend") {
			t.Errorf("%s @back offered %v, want @backend", verb, a.completion.offers)
		}
		if tag := a.completion.tags["@backend"]; tag.suffix != teamMenuSuffix {
			t.Errorf("%s @back drew @backend without its (team) tag", verb)
		}
	}
}

// /resume's argument offers what it can bring back: parked sessions, and teams
// with a parked member even when none is live - which the @ menu, keyed on
// routing, leaves out.
func TestResumeOffersParkedSessionsAndTeams(t *testing.T) {
	// A fresh room per draft: copies of an App share one text area underneath.
	typed := func(draft string) completion {
		return newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
			Running: true,
			Sessions: []rpc.SessionStatus{
				{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateParked},
				{ID: "s2", Name: "bob", State: rpc.StateIdle},
			},
			Parked: []rpc.SessionStatus{{ID: "s7", Name: "otto", Team: "infra", State: rpc.StateParked}},
		}}).withDraft(draft).completion
	}

	for draft, want := range map[string]string{
		resumeVerb + " b":  "backend",
		resumeVerb + " @b": "@backend",
		resumeVerb + " in": "infra",
		resumeVerb + " sy": "sydney",
		resumeVerb + " ot": "otto",
	} {
		if got := typed(draft); !slices.Contains(got.offers, want) {
			t.Errorf("%q offered %v, want %q", draft, got.offers, want)
		}
	}
	got := typed(resumeVerb + " b")
	if slices.Contains(got.offers, "bob") {
		t.Errorf("/resume b offered the live bob, who has nothing to resume: %v", got.offers)
	}
	if tag := got.tags["backend"]; tag.suffix != teamMenuSuffix {
		t.Error("/resume b drew backend without its (team) tag")
	}
}

func TestResumeOffersAParkedNameOverATeamOfIt(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s6", Name: "backend", State: rpc.StateParked},
	).withDraft(resumeVerb + " back")

	c := a.completion
	if n := strings.Count(strings.Join(c.offers, " "), "backend"); n != 1 {
		t.Errorf("/resume back offered backend %d times (%v), want once", n, c.offers)
	}
	if tag, tagged := c.tags["backend"]; tagged && tag.suffix == teamMenuSuffix {
		t.Error("/resume back tagged backend a team, but the parked agent wins the name")
	}
}

// Only the two commands that take a team themselves fold at one; widening the
// set is a ruling, not an entry (decisions.md, 2026-11-09).
func TestOnlyParkAndQuitTakeATeam(t *testing.T) {
	got := slices.Sorted(maps.Keys(teamTargetCommands))
	if !slices.Equal(got, []string{parkCommand, quitCommand}) {
		t.Errorf("teamTargetCommands = %v, want exactly park and quit", got)
	}
}

// An accepted `/resume ` is a finished word with something parked too: its
// menu waits for a character, so ⇥ does not insert a name nobody chose.
func TestAnAcceptedResumeIsAFinishedWordWithSessionsParked(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateParked},
	).withDraft("/resum")

	next, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyTab})

	if got := next.composer().Value(); got != resumeVerb+" " {
		t.Fatalf("⇥ over /resum left %q", got)
	}
	if next.completionUp() {
		t.Errorf("a menu is up over the accepted /resume, offering %v", next.completion.offers)
	}
	if next.typingWord() {
		t.Error("an accepted /resume still reads as a word being typed, so ⇥ would stay put instead of moving the keys")
	}
}

// A card arriving in the confirm's pane takes its place, and its page can hide
// it; ↓ ↵ meant for the card must not quit a team nobody can see.
func TestAQuitConfirmYieldsToACardInItsPane(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s1", "john").withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "john", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "sydney", Team: "backend", State: rpc.StateIdle},
	)
	a, _, _ = a.slash(quitVerb + " @backend")
	if !a.picker.Open() {
		t.Fatal("test precondition: the confirm did not open")
	}
	plan := planAsk(t)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &plan})
	if _, carded := a.cardOf("s1"); !carded {
		t.Fatal("test precondition: the plan card did not arrive in john's pane")
	}

	if strings.Contains(shown(a), quitNeverMind) {
		t.Errorf("the confirm is drawn under a card that is the pane's to answer:\n%s", shown(a))
	}
	for _, k := range []tea.KeyType{tea.KeyDown, tea.KeyEnter} {
		var cmd tea.Cmd
		a, cmd = keyed(a, tea.KeyMsg{Type: k})
		if cmd != nil {
			if stops := kindIDs(sentFrames(t, a, cmd), rpc.FrameStop); len(stops) > 0 {
				t.Fatalf("a key meant for the card stopped %v through a confirm nobody could see", stops)
			}
		}
	}
}

// `@team /park` and `@team /quit` at a team with nothing running give the typed
// form's answer, rather than reaching the manager as text.
func TestATeamAtRestFoldsInTheRoomToo(t *testing.T) {
	for _, verb := range []string{parkVerb, quitVerb} {
		a := backendRoom(t).withRoster(
			rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateParked},
			rpc.SessionStatus{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateParked},
		)
		m, cmd := a.withDraft("@backend " + verb).submit(false)
		if cmd != nil {
			t.Fatalf("@backend %s at a team at rest wrote %+v", verb, sentFrames(t, m.(App), cmd))
		}
		if note := latestNotice(t); !strings.Contains(note, "nothing of @backend is running") {
			t.Errorf("@backend %s said %q, want the typed form's answer", verb, note)
		}
	}
}

// A member that ended instead of parking holds back that ask's confirm, never a
// later one: a new team park replaces the team's old wait.
func TestANewTeamParkReplacesAWaitThatNeverLanded(t *testing.T) {
	a := backendRoom(t)
	a, _, _ = a.slash(parkVerb + " @backend")
	a = a.withRoster(
		rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateEnded},
	)
	a = a.withRoster(rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateIdle})

	a, _, _ = a.slash(parkVerb + " @backend")
	_ = a.withRoster(rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateParked})

	if note := latestNotice(t); !strings.Contains(note, "@backend is parked") {
		t.Errorf("the second team park confirmed as %q: the first ask's lost member held it back", note)
	}
}

// A member parked by hand after a team park that missed it is confirmed as one
// agent, not as the team.
func TestAHandParkOfAMemberIsConfirmedAsOne(t *testing.T) {
	a := backendRoom(t)
	a, _, _ = a.slash(parkVerb + " @backend")
	a = a.withRoster(rpc.SessionStatus{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateParked})

	a, _, _ = a.slash(parkVerb + " @thea")
	_ = a.withRoster(rpc.SessionStatus{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateParked})

	if note := latestNotice(t); !strings.Contains(note, "@thea is parked") {
		t.Errorf("thea's own park confirmed as %q, want it named as one agent", note)
	}
}

// A typed /park at an agent already parked says so; a key stays silent.
func TestParkAParkedAgentSaysSo(t *testing.T) {
	a := backendRoom(t, rpc.SessionStatus{ID: "s6", Name: "otto", State: rpc.StateParked})

	next, cmd, _ := a.slash(parkVerb + " @otto")

	if cmd != nil {
		t.Fatalf("/park @otto over a parked otto wrote %+v", sentFrames(t, next, cmd))
	}
	if note := latestNotice(t); !strings.Contains(note, "@otto is parked already") {
		t.Errorf("/park @otto said %q, want it to say otto is parked already", note)
	}
}

package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// withTeamFleet pushes a roster carrying a team, the way the daemon's report
// does: an un-tagged agent, one on the team, and the team in the daemon's order.
func (a App) withTeamFleet() App {
	st := rpc.Status{
		Running: true,
		Teams:   []string{"backend"},
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", State: rpc.StateIdle},
			{ID: "s2", Name: "bob", Team: "backend", State: rpc.StateIdle},
		},
	}
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
}

// A team is an addressee like an agent: `@` in the room offers it, labelled so
// it reads as a team rather than as an agent nobody can find in the roster.
func TestTheRoomOffersATeamAsAMention(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withTeamFleet()
	a = a.withDraft("@back")

	if !slices.Contains(a.completion.offers, agentPrefix+"backend") {
		t.Fatalf("`@back` in the room does not offer the team @backend: %v", a.completion.offers)
	}
	if got, want := a.completion.rowLabel(agentPrefix+"backend", 200), agentPrefix+"backend"+teamMenuSuffix; got != want {
		t.Errorf("the team offer is labelled %q, want %q", got, want)
	}
	if got := a.completion.rowLabel(agentPrefix+"alex", 200); got != agentPrefix+"alex" {
		t.Errorf("an agent offer is labelled %q, want it left alone", got)
	}
	// End to end through the render, not just the helper: a regression to
	// optionRow(offer, …) would leave the pure helper right and the menu wrong.
	if got := a.completionView(200, a.focus); !strings.Contains(got, agentPrefix+"backend"+teamMenuSuffix) {
		t.Errorf("the rendered menu does not tag the team @backend (team):\n%s", got)
	}
}

// The `(team)` tag is what tells a fan-out mention from an ordinary one, so it
// must survive a narrow pane: optionRow truncates from the right, so a long team
// name is trimmed while the tag is kept - and an accept still inserts the whole
// bare mention that routes to the team.
func TestATeamKeepsItsTagOnANarrowPane(t *testing.T) {
	const longTeam = "backend-platform-infra-and-more" // 31 chars, under maxTeamName
	a := newRoomApp(t).withSize(200, 40)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running:  true,
		Teams:    []string{longTeam},
		Sessions: []rpc.SessionStatus{{ID: "s1", Name: "bob", Team: longTeam, State: rpc.StateIdle}},
	}})
	a = a.withDraft("@back")

	if got := a.completion.View(24); !strings.Contains(got, teamMenuSuffix) || strings.Contains(got, longTeam) {
		t.Errorf("at 24 columns the tag was dropped or the full name kept, so a fan-out reads as an ordinary mention:\n%s", got)
	}
	a, _, ok := a.completionKey(tea.KeyMsg{Type: tea.KeyTab})
	if !ok {
		t.Fatal("⇥ was not taken by the completion menu")
	}
	if got := a.composer().Value(); got != agentPrefix+longTeam+" " {
		t.Errorf("a truncated row inserted %q, want the whole bare mention %q", got, agentPrefix+longTeam+" ")
	}
}

// A live agent wins a name it shares with a team: one row, offered untagged, so
// the deferred agent/team name collision cannot draw a duplicate or tag the
// agent's own row as a team.
func TestALiveAgentWinsANameSharedWithATeam(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Teams:   []string{"backend"},
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "backend", State: rpc.StateIdle},
			{ID: "s2", Name: "bob", Team: "backend", State: rpc.StateIdle},
		},
	}})
	a = a.withDraft("@back")

	count := 0
	for _, o := range a.completion.offers {
		if o == agentPrefix+"backend" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("want one @backend row, got %d: %v", count, a.completion.offers)
	}
	if got := a.completion.rowLabel(agentPrefix+"backend", 200); got != agentPrefix+"backend" {
		t.Errorf("the live agent's row was tagged %q, want it untagged - the agent won the name", got)
	}
}

// A parked agent must not hide a team fan-out: it shares the name but does not
// win the route (App.live excludes StateParked), so `@backend` fans out to the
// team - the menu must show the one tagged team row, not a plain parked row.
func TestAParkedAgentDoesNotHideATeamFanOut(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Teams:   []string{"backend"},
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "backend", State: rpc.StateParked},
			{ID: "s2", Name: "bob", Team: "backend", State: rpc.StateIdle},
		},
	}})
	a = a.withDraft("@back")

	count := 0
	for _, o := range a.completion.offers {
		if o == agentPrefix+"backend" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want one @backend row, got %d: %v", count, a.completion.offers)
	}
	if got := a.completion.rowLabel(agentPrefix+"backend", 200); got != agentPrefix+"backend"+teamMenuSuffix {
		t.Errorf("the row is %q, want it tagged as a team - @backend fans out, it does not reach the parked session", got)
	}
}

// A team whose only members are parked fans out to nobody (App.live excludes
// them), so the @ menu must not offer it as a tagged fan-out - the tag would
// promise a route that reaches no one.
func TestAParkedOnlyTeamIsNotOfferedAsAFanOut(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Teams:   []string{"backend"},
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "bob", Team: "backend", State: rpc.StateParked},
			{ID: "s2", Name: "alex", State: rpc.StateIdle},
		},
	}})
	a = a.withDraft("@back")

	if slices.Contains(a.completion.offers, agentPrefix+"backend") {
		t.Errorf("offered @backend for a team with no live member (a fan-out that reaches nobody): %v", a.completion.offers)
	}
}

// Only ASCII space separates /team from its argument, because that is the one
// separator the slash router splits on. Any other whitespace - tab, newline,
// NBSP, CR, VT, FF, reachable through a paste - makes a form sent as prose rather
// than run, so teamArgStem must reject it (strings.Fields would otherwise split
// them). Tested on the pure stem with exact bytes, since the text area normalises
// some whitespace on the way in and would mask the tokenizer under test.
func TestTeamArgStemRequiresAsciiSpaceSeparators(t *testing.T) {
	for name, sep := range map[string]string{
		"tab": "\t", "newline": "\n", "nbsp": " ",
		"carriage-return": "\r", "vertical-tab": "\v", "form-feed": "\f",
	} {
		if _, _, _, _, ok := teamArgStem("/team" + sep + "@alex back"); ok {
			t.Errorf("teamArgStem accepted a %s separator, but the slash router splits only on ASCII space", name)
		}
	}
	// The ASCII-space forms still parse - the three /team runs in.
	for _, draft := range []string{"/team back", "/team @alex back", "@alex /team back"} {
		if _, _, _, _, ok := teamArgStem(draft); !ok {
			t.Errorf("teamArgStem rejected the valid form %q", draft)
		}
	}
}

// The team-name argument of /team completes against the existing teams, the way
// skills and @names do - the whole of the second half of the request.
func TestTheTeamArgumentCompletesExistingTeams(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withTeamFleet()
	a = a.withDraft("@alex /team back")

	if !slices.Contains(a.completion.offers, "backend") {
		t.Fatalf("`@alex /team back` does not offer the existing team backend: %v", a.completion.offers)
	}
	a, _, ok := a.completionKey(tea.KeyMsg{Type: tea.KeyTab})
	if !ok {
		t.Fatal("⇥ was not taken by the completion menu")
	}
	if got := a.composer().Value(); got != "@alex /team backend " {
		t.Errorf("completing the team argument gave %q, want %q", got, "@alex /team backend ")
	}
}

// A bare `/team ` in a DM offers every team: /team runs against the focused
// agent there, and the argument completion is not the room-only @mention one.
func TestTheTeamArgumentCompletesBareInADM(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withTeamFleet()
	a = pick(a, "s2").openDMWith("s2", "bob").applyGeometry()
	a = a.withDraft("/team ")

	if !slices.Contains(a.completion.offers, "backend") {
		t.Errorf("`/team ` in a DM does not offer the team backend: %v", a.completion.offers)
	}
}

// A bare `/team` in the room has no target, so it is not offered there - a
// completion would promise a command that does not run.
func TestTheBareTeamArgumentDoesNotCompleteInTheRoom(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withTeamFleet()
	a = a.withDraft("/team back")

	if slices.Contains(a.completion.offers, "backend") {
		t.Errorf("a bare `/team` in the room offered a team, but it has no target there: %v", a.completion.offers)
	}
}

// The `@who /team` bridge is the room's alone: in a DM `@who /team back` is sent
// verbatim, never run, so it must not be offered there.
func TestTheTeamBridgeDoesNotCompleteInADM(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withTeamFleet()
	a = pick(a, "s1").openDMWith("s1", "alex").applyGeometry()
	a = a.withDraft("@alex /team back")

	if slices.Contains(a.completion.offers, "backend") {
		t.Errorf("a DM offered the @who /team bridge, but it is sent verbatim there: %v", a.completion.offers)
	}
}

// The bridge and the inline `@who` target run only for one live agent: @all
// broadcasts (N literal turns, no team set) and an unresolved name reaches
// nobody, so neither is offered.
func TestTheTeamBridgeRequiresOneLiveAgent(t *testing.T) {
	base := func() App { return newRoomApp(t).withSize(200, 40).withTeamFleet() }
	for _, draft := range []string{"@all /team back", "@ghost /team back", "/team @all back"} {
		a := base().withDraft(draft)
		if slices.Contains(a.completion.offers, "backend") {
			t.Errorf("%q offered a team, but it does not resolve to one live agent: %v", draft, a.completion.offers)
		}
	}
}

// `/team @who <partial>` is the documented inline-target form (teamUsage): it
// completes for a live agent, in the room or a DM.
func TestTheInlineTeamTargetCompletes(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withTeamFleet()
	a = a.withDraft("/team @alex back")

	if !slices.Contains(a.completion.offers, "backend") {
		t.Fatalf("`/team @alex back` does not offer the team backend: %v", a.completion.offers)
	}
	a, _, ok := a.completionKey(tea.KeyMsg{Type: tea.KeyTab})
	if !ok {
		t.Fatal("⇥ was not taken by the completion menu")
	}
	if got := a.composer().Value(); got != "/team @alex backend " {
		t.Errorf("completing the inline target gave %q, want %q", got, "/team @alex backend ")
	}
}

// The completion is the first argument only: a second token /team does not take
// gets no team menu, so it cannot look completable when it is not.
func TestTheTeamArgumentDoesNotCompleteASecondToken(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withTeamFleet()
	a = a.withDraft("/team backend ba")

	if slices.Contains(a.completion.offers, "backend") {
		t.Errorf("`/team backend ba` offered a team for a second token /team does not take: %v", a.completion.offers)
	}
}

// The label is a display hint only: accepting a team offer inserts the bare
// `@team ` the router fans out, never the `(team)` tag drawn beside it.
func TestATeamOfferInsertsTheRoutableMention(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withTeamFleet()
	a = a.withDraft("@back")

	if a.completion.offers[a.completion.cursor] != agentPrefix+"backend" {
		t.Fatalf("the cursored offer is %q, want @backend", a.completion.offers[a.completion.cursor])
	}
	a, _, ok := a.completionKey(tea.KeyMsg{Type: tea.KeyTab})
	if !ok {
		t.Fatal("⇥ was not taken by the completion menu")
	}
	if got := a.composer().Value(); got != agentPrefix+"backend "+"" {
		t.Errorf("accepting the team offer gave %q, want %q", got, agentPrefix+"backend ")
	}
}

// Teams sit after every agent and before @all: narrowest (one agent) to
// broadest (everyone), the team in the middle.
func TestTeamsSitAfterAgentsAndBeforeAll(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withTeamFleet()
	a = a.withDraft("@")

	team := slices.Index(a.completion.offers, agentPrefix+"backend")
	all := slices.Index(a.completion.offers, agentPrefix+"all")
	if team < 0 || all < 0 {
		t.Fatalf("@ offered team at %d and @all at %d: one is missing: %v", team, all, a.completion.offers)
	}
	for i, offer := range a.completion.offers {
		isAgent := offer == agentPrefix+"alex" || offer == agentPrefix+"bob"
		if isAgent && i > team {
			t.Errorf("agent %q at %d sits after the team at %d, want every agent first: %v", offer, i, team, a.completion.offers)
		}
	}
	if team > all {
		t.Errorf("team at %d sits after @all at %d, want the team before @all: %v", team, all, a.completion.offers)
	}
}

// A DM offers no teams: `@name` there is a file reference claude's own CLI
// reads, not Wake routing, so a team completed there would resolve to nothing.
func TestADMDoesNotOfferTeams(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withTeamFleet()
	a = pick(a, "s2").openDMWith("s2", "bob").applyGeometry()
	if a.focus == "" {
		t.Fatal("the DM did not take focus, so this is still testing the room")
	}
	a = a.withDraft("@back")

	if slices.Contains(a.completion.offers, agentPrefix+"backend") {
		t.Errorf("a DM offered the team @backend, but @name there is a file reference not a route: %v", a.completion.offers)
	}
}

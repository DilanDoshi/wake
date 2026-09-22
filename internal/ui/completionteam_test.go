package ui

import (
	"slices"
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
	if got, want := a.completion.label(agentPrefix+"backend"), agentPrefix+"backend"+teamMenuSuffix; got != want {
		t.Errorf("the team offer is labelled %q, want %q", got, want)
	}
	if got := a.completion.label(agentPrefix + "alex"); got != agentPrefix+"alex" {
		t.Errorf("an agent offer is labelled %q, want it left alone", got)
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

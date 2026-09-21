package core

import (
	"slices"
	"testing"
)

// A team fans a message out to its live members - a scoped broadcast, not a
// mention of one agent and not @all. It is the operator's own grouping (the tag
// on Addressee), resolved here so the room can address it before it fires N turns.

func TestATeamFansOutToItsLiveMembers(t *testing.T) {
	live := []Addressee{
		{ID: "id-thea", Name: "thea", Team: "backend"},
		{ID: "id-john", Name: "john", Team: "backend"},
		{ID: "id-delta", Name: "delta", Team: "frontend"},
	}
	got := Resolve("@backend ship the staging build", live, manager)
	if !slices.Equal(got.Targets, []string{"id-thea", "id-john"}) {
		t.Errorf("targets = %v, want backend's two members and only those", got.Targets)
	}
	if got.Team != "backend" || got.Resolved != "backend" {
		t.Errorf("team=%q resolved=%q, want the team named so the room can draw `→ @backend`", got.Team, got.Resolved)
	}
	if got.Text != "ship the staging build" {
		t.Errorf("text = %q, want the @team stripped like @all", got.Text)
	}
	// A team is not @all: Broadcast means the whole fleet, and the composer target
	// line reads it as such. A team carries its own marker instead.
	if got.Broadcast {
		t.Error("a team route is marked Broadcast; Broadcast means @all and the composer would read it as the whole fleet")
	}
}

// `@Backend` reaches team `backend`: a team is stored lower-case (NormalizeTeam),
// so the mention is folded at the address side too. Without the fold the message
// silently fell through to the manager - a reviewer-found HIGH.
func TestATeamMentionIsCaseInsensitive(t *testing.T) {
	live := []Addressee{
		{ID: "id-thea", Name: "thea", Team: "backend"},
		{ID: "id-john", Name: "john", Team: "backend"},
	}
	got := Resolve("@Backend ship it", live, manager)
	if !slices.Equal(got.Targets, []string{"id-thea", "id-john"}) {
		t.Errorf("targets = %v, want backend's members: @Backend must reach team backend", got.Targets)
	}
	if got.Team != "backend" || got.Resolved != "backend" {
		t.Errorf("team=%q resolved=%q, want the canonical lower-case name so the room draws @backend", got.Team, got.Resolved)
	}
}

// An agent name wins a collision with a team name - the header's own rule, and
// why the daemon refuses a /team that equals a live name. Belt and braces here:
// the agent-name loop runs before the team step, so even a slipped-through
// collision resolves to the agent.
func TestAnAgentNameWinsOverATeamOfTheSameName(t *testing.T) {
	live := []Addressee{
		{ID: "id-backend", Name: "backend", Team: "infra"},
		{ID: "id-other", Name: "other", Team: "backend"},
	}
	got := Resolve("@backend hi", live, manager)
	if !slices.Equal(got.Targets, []string{"id-backend"}) || got.Resolved != "backend" || got.Team != "" {
		t.Errorf("targets=%v resolved=%q team=%q, want the live agent named backend, not the team", got.Targets, got.Resolved, got.Team)
	}
}

// A team nobody is in is not a route: it passes through to the service with the
// text intact, exactly like an ended agent's name. A team exists only while a
// live member carries it.
func TestATeamWithNoLiveMembersDoesNotRoute(t *testing.T) {
	live := []Addressee{{ID: "id-thea", Name: "thea", Team: "backend"}}
	got := Resolve("@frontend hello", live, manager)
	if !slices.Equal(got.Targets, []string{"id-manager"}) || got.Text != "@frontend hello" || got.Team != "" {
		t.Errorf("a team with no members routed: targets=%v text=%q team=%q", got.Targets, got.Text, got.Team)
	}
}

// Team is set only on a team route, so a caller can tell a scoped broadcast from
// a single mention and from @all. The mirror of TestOnlyABroadcastIsMarkedAsOne.
func TestOnlyATeamRouteCarriesATeam(t *testing.T) {
	live := []Addressee{
		{ID: "id-sydney", Name: "sydney", Team: "backend"},
		{ID: "id-alex", Name: "alex"},
	}
	for _, in := range []string{
		"@sydney hello", "@alex hello", "@ghost hello", "@ hello",
		"look at @note.txt", "who is stuck?", "@" + BroadcastName + " hello",
		"@" + ManagerName + " hello",
	} {
		if got := Resolve(in, live, manager); got.Team != "" {
			t.Errorf("Resolve(%q) carried team %q; only a team fan-out may", in, got.Team)
		}
	}
	if got := Resolve("@backend hello", live, manager); got.Team != "backend" {
		t.Error("a team route carries no team: this test would pass against a field nothing sets")
	}
}

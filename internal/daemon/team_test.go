package daemon

// A session's team tag: set under the agent's own lock, carried on the
// snapshot, and written into the park book so it survives ⌃Q. The state verdict
// is renameableStates', shared with rename, label and colour, so a parked or
// ended session is refused for the park book's reason.

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func teamAgent(t *testing.T) *agent {
	t.Helper()
	return newAgent(idAlpha, "sydney", "dev-1", "/repo/api", "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})
}

func TestTeamSetsTheTagOnTheSnapshot(t *testing.T) {
	a := teamAgent(t)
	if err := a.setTeam("backend"); err != nil {
		t.Fatalf("setTeam(backend): %v", err)
	}
	if got := a.snapshot().Team; got != "backend" {
		t.Errorf("snapshot().Team = %q, want %q", got, "backend")
	}
}

func TestTeamFoldsCaseAndClears(t *testing.T) {
	a := teamAgent(t)
	if err := a.setTeam("Backend"); err != nil {
		t.Fatalf("setTeam(Backend): %v", err)
	}
	if got := a.snapshot().Team; got != "backend" {
		t.Fatalf("case was not folded: snapshot().Team = %q, want %q", got, "backend")
	}
	if err := a.setTeam(rpc.TeamNone); err != nil {
		t.Fatalf("setTeam(none): %v", err)
	}
	if got := a.snapshot().Team; got != "" {
		t.Errorf("none did not clear: snapshot().Team = %q, want empty", got)
	}
}

func TestARefusedTeamLeavesTheTagUnchanged(t *testing.T) {
	a := teamAgent(t)
	if err := a.setTeam("backend"); err != nil {
		t.Fatalf("setTeam(backend): %v", err)
	}
	if err := a.setTeam("back end"); err == nil {
		t.Fatal("setTeam accepted a two-word team name")
	}
	if got := a.snapshot().Team; got != "backend" {
		t.Errorf("a refused team changed the tag: snapshot().Team = %q, want %q", got, "backend")
	}
}

// The park book is written by the park itself, so a team set on a running
// session is refused once it is parked - the same ruling that refuses a rename.
func TestTeamIsRefusedForAParkedOrEndedSession(t *testing.T) {
	t.Run("parked", func(t *testing.T) {
		a := teamAgent(t)
		a.parked = true
		if err := a.setTeam("infra"); err == nil {
			t.Error("setTeam was allowed on a parked session")
		}
	})
	t.Run("ended", func(t *testing.T) {
		a := teamAgent(t)
		a.ended = true
		if err := a.setTeam("infra"); err == nil {
			t.Error("setTeam was allowed on an ended session")
		}
	})
}

// The team rides into the park book beside the name, label and colour, so ⌃Q
// then wake then /resume brings a session back grouped where it was.
func TestAParkedRecordCarriesTheTeam(t *testing.T) {
	a := teamAgent(t)
	if err := a.setTeam("frontend"); err != nil {
		t.Fatalf("setTeam(frontend): %v", err)
	}
	if got := recordFor(a).Team; got != "frontend" {
		t.Errorf("recordFor(a).Team = %q, want %q: the team did not reach the park book", got, "frontend")
	}
}

func TestAParkedStatusReportsItsTeam(t *testing.T) {
	got := parkedStatus(parkedRecord{ID: idAlpha, Team: "infra"}).Team
	if got != "infra" {
		t.Errorf("parkedStatus(...).Team = %q, want %q", got, "infra")
	}
}

// A team is addressed as @team, so it may not wear a word the router already
// spends: @all broadcasts and @manager reaches the service, and a team of either
// name would be a section nothing could address. reservedNames is the router's
// own set, checked here so a team cannot claim one.
func TestATeamCannotBeAReservedRoutingWord(t *testing.T) {
	for _, word := range []string{core.BroadcastName, core.ManagerName} {
		a := teamAgent(t)
		if err := a.setTeam(word); err == nil {
			t.Errorf("setTeam(%q) was allowed; a team may not wear a reserved routing word", word)
		}
		if got := a.snapshot().Team; got != "" {
			t.Errorf("a refused reserved team was stored: snapshot().Team = %q", got)
		}
	}
}

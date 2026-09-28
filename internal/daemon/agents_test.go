package daemon

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// An agent carries onto its report the subagent types its init advertised, so
// a client that attached after that init - which is every reattach - still
// learns them from the report. Task 5's `@` menu offers `@agent-<type>` from
// exactly this list; Commands' own reason - see rpc.SessionStatus.Agents.
func TestAnAgentReportsTheAgentsItsInitAdvertised(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-5748", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	a.observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{
		Agents: []string{"general-purpose", "Explore"},
	}})

	if got := a.snapshot().Agents; len(got) != 2 {
		t.Errorf("snapshot().Agents = %v after an init advertising two, want both: the report is the only "+
			"route a client that attached after the init has to them", got)
	}
}

// A frame that names no agents - every result and tool frame - leaves the
// advertised set alone rather than blanking it, Commands' own guard.
func TestAReportKeepsTheAgentsWhenAFrameNamesNone(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-5748", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	a.observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{Agents: []string{"Explore"}}})
	a.observe(core.Event{Kind: core.KindTurnEnd, Session: &core.SessionFacts{}}) // a result frame names no agents

	if got := a.snapshot().Agents; len(got) != 1 {
		t.Errorf("snapshot().Agents = %v after a later frame named none, want the init's one kept", got)
	}
}

// A later init replaces the set wholesale rather than appending to it, the
// same replace-not-mutate rule as Commands.
func TestALaterInitReplacesTheAgentsWholesale(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-5748", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	a.observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{Agents: []string{"Explore"}}})
	a.observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{Agents: []string{"Plan", "general-purpose"}}})

	got := a.snapshot().Agents
	if len(got) != 2 || got[0] != "Plan" || got[1] != "general-purpose" {
		t.Errorf("snapshot().Agents = %v after a second init naming a different two, want exactly those two, in order", got)
	}
}

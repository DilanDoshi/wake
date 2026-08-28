package daemon

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// An agent carries onto its report the slash commands its init advertised, so a
// client that attached after that init - which is every reattach - still learns
// them from the report and its completion menu is not empty.
//
// The commands ride the one-per-turn init *event*, and no event is replayed to a
// late client, so the report is the only route it has to them. Same shape as
// Effort - see rpc.SessionStatus.Commands.
func TestAnAgentReportsTheCommandsItsInitAdvertised(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-5748", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	a.observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{
		SlashCommands: []string{"compact", "commit-push"},
	}})

	if got := a.snapshot().Commands; len(got) != 2 {
		t.Errorf("snapshot().Commands = %v after an init advertising two, want both: the report is the only "+
			"route a client that attached after the init has to them", got)
	}
}

// A frame that names no commands - every result and tool frame - leaves the
// advertised set alone rather than blanking it, the same guard withFacts keeps.
func TestAReportKeepsTheCommandsWhenAFrameNamesNone(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-5748", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	a.observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{SlashCommands: []string{"compact"}}})
	a.observe(core.Event{Kind: core.KindTurnEnd, Session: &core.SessionFacts{}}) // a result frame names no commands

	if got := a.snapshot().Commands; len(got) != 1 {
		t.Errorf("snapshot().Commands = %v after a later frame named none, want the init's one kept", got)
	}
}

package daemon

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// The daemon observes the model on a session's init frame and carries it on the
// report, so a client that never saw the init still names the model. The spawn
// alias (a.model) is empty for a default-model spawn, so the observed init id is
// the only source that works there. See rpc.SessionStatus.Model.
func TestSnapshotCarriesTheObservedInitModel(t *testing.T) {
	a := movedAgent(t) // newAgent(...), the neighbouring cwd_test helper
	a.observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{Model: "claude-opus-4-8"}})
	if got := a.snapshot().Model; got != "claude-opus-4-8" {
		t.Errorf("snapshot().Model = %q, want claude-opus-4-8", got)
	}
}

// A result frame names no model; observing one must not blank the model an init
// already set - withFacts' own guard, one wire over.
func TestAResultFrameDoesNotBlankTheObservedModel(t *testing.T) {
	a := movedAgent(t)
	a.observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{Model: "claude-opus-4-8"}})
	a.observe(core.Event{Kind: core.KindTurnEnd, Session: &core.SessionFacts{}})
	if got := a.snapshot().Model; got != "claude-opus-4-8" {
		t.Errorf("snapshot().Model = %q after a result frame, want it kept", got)
	}
}

package ui

// `/new` can say a budget and a failover chain — deferred.md's 2026-08-20
// entry: the room is the ordinary spawn path, and budget and fallback have no
// runtime command, so an agent spawned without them was uncapped for its whole
// life. Two rows in takeNewFlags' table, the entry's own prescription.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestSlashNewCarriesABudgetAndAFallbackChain(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40)

	m, cmd := typeAndSubmit(a, "/new sydney --max-budget-usd 5 --fallback-model sonnet")
	f := sentFrame(t, m.(App), cmd)

	if f.Kind != rpc.FrameSpawn || f.Text != "sydney" {
		t.Fatalf("the frame is %q for %q, want a spawn for sydney", f.Kind, f.Text)
	}
	if f.MaxBudgetUSD != "5" {
		t.Errorf("the frame carries budget %q, want %q — the flag the entry exists for", f.MaxBudgetUSD, "5")
	}
	if f.FallbackModel != "sonnet" {
		t.Errorf("the frame carries fallback %q, want %q", f.FallbackModel, "sonnet")
	}
}

// The refusals are the shell verb's own: a malformed amount and a chain naming
// no model are turned into sentences before a socket is dialled, exactly as
// cmd/wake/spawnflags.go does — neither check is redundant with the daemon's.
func TestSlashNewRefusesABadBudgetAndABadChain(t *testing.T) {
	for _, tc := range []struct {
		draft, names string
	}{
		// ValidModel is deliberately open - a model shipped tomorrow must be
		// usable - so the only refusable chain is one naming nothing at all.
		{"/new --max-budget-usd 5e3", "--max-budget-usd"},
		{"/new --max-budget-usd 0", "--max-budget-usd"},
		// Two disagreeing values exercise this row's own once() - the shared
		// helper is proven through --debug-file, and a per-row bypass survived
		// every other test here.
		{"/new --max-budget-usd 5 --max-budget-usd 6", "--max-budget-usd"},
		{"/new --fallback-model ,", "--fallback-model"},
	} {
		t.Run(tc.draft, func(t *testing.T) {
			fresh(t)
			a := newRoomApp(t).withSize(200, 40)

			m, cmd := typeAndSubmit(a, tc.draft)
			if cmd != nil {
				t.Fatalf("%q spawned anyway: %+v", tc.draft, sentFrames(t, m.(App), cmd))
			}
			if !strings.Contains(shown(m.(App)), tc.names) {
				t.Errorf("%q was refused without naming the flag:\n%s", tc.draft, shown(m.(App)))
			}
		})
	}
}

// The usage line advertises what the command now takes, so a refusal teaches
// the flags rather than hiding them.
func TestTheNewUsageNamesTheSpendFlags(t *testing.T) {
	for _, flag := range []string{budgetNewFlag, fallbackNewFlag} {
		if !strings.Contains(newUsage, flag) {
			t.Errorf("newUsage %q does not name %s", newUsage, flag)
		}
	}
}

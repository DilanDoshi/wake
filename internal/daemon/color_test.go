package daemon

// A session's identity colour: set under the agent's own lock, carried on the
// snapshot, and written into the park book so it survives ⌃Q. The state verdict
// is renameableStates', shared with rename and label, so a parked or ended
// session is refused for the park book's reason.

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func colorAgent(t *testing.T) *agent {
	t.Helper()
	return newAgent(idAlpha, "sydney", "dev-1", "/repo/api", "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})
}

func TestColorSetsTheHueOnTheSnapshot(t *testing.T) {
	a := colorAgent(t)
	if err := a.setColor("green"); err != nil {
		t.Fatalf("setColor(green): %v", err)
	}
	if got := a.snapshot().Color; got != "green" {
		t.Errorf("snapshot().Color = %q, want %q", got, "green")
	}
}

func TestColorFoldsCaseAndClears(t *testing.T) {
	a := colorAgent(t)
	if err := a.setColor("VIOLET"); err != nil {
		t.Fatalf("setColor(VIOLET): %v", err)
	}
	if got := a.snapshot().Color; got != "violet" {
		t.Fatalf("case was not folded: snapshot().Color = %q, want %q", got, "violet")
	}
	if err := a.setColor(rpc.ColorNone); err != nil {
		t.Fatalf("setColor(none): %v", err)
	}
	if got := a.snapshot().Color; got != "" {
		t.Errorf("none did not clear: snapshot().Color = %q, want empty", got)
	}
}

func TestARefusedColorLeavesTheHueUnchanged(t *testing.T) {
	a := colorAgent(t)
	if err := a.setColor("indigo"); err != nil {
		t.Fatalf("setColor(indigo): %v", err)
	}
	if err := a.setColor("chartreuse"); err == nil {
		t.Fatal("setColor accepted a colour not in the set")
	}
	if got := a.snapshot().Color; got != "indigo" {
		t.Errorf("a refused colour changed the hue: snapshot().Color = %q, want %q", got, "indigo")
	}
}

// The park book is written by the park itself, so a colour set on a running
// session is refused once it is parked - the same ruling that refuses a rename,
// for the same reason: rewriting the book out of band is a different contract.
func TestColorIsRefusedForAParkedOrEndedSession(t *testing.T) {
	t.Run("parked", func(t *testing.T) {
		a := colorAgent(t)
		a.parked = true
		if err := a.setColor("red"); err == nil {
			t.Error("setColor was allowed on a parked session")
		}
	})
	t.Run("ended", func(t *testing.T) {
		a := colorAgent(t)
		a.ended = true
		if err := a.setColor("red"); err == nil {
			t.Error("setColor was allowed on an ended session")
		}
	})
}

// The colour rides into the park book beside the name and label, so ⌃Q then wake
// then /resume brings a session back the hue it had.
func TestAParkedRecordCarriesTheColor(t *testing.T) {
	a := colorAgent(t)
	if err := a.setColor("orange"); err != nil {
		t.Fatalf("setColor(orange): %v", err)
	}
	if got := recordFor(a).Color; got != "orange" {
		t.Errorf("recordFor(a).Color = %q, want %q: the colour did not reach the park book", got, "orange")
	}
}

func TestAParkedStatusReportsItsColor(t *testing.T) {
	got := parkedStatus(parkedRecord{ID: idAlpha, Color: "yellow"}).Color
	if got != "yellow" {
		t.Errorf("parkedStatus(...).Color = %q, want %q", got, "yellow")
	}
}

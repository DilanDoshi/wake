package daemon

// The cap an operator sets with `--max-agents`: fifty unless set, set on a
// running fleet by FrameSetMaxAgents, and never more than fifty.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// awaitCap waits for a status report that names this cap.
func awaitCap(c *testClient, n int) {
	c.t.Helper()
	c.send(rpc.Frame{Kind: rpc.FrameStatus})
	c.await("a status naming the cap", func(f rpc.Frame) bool {
		return f.Status != nil && f.Status.MaxAgents == n
	})
}

func TestAFleetRunsFiftyAtOnceUnlessToldOtherwise(t *testing.T) {
	if defaultLiveCap != rpc.MaxAgentsCeiling || rpc.MaxAgentsCeiling != 50 {
		t.Fatalf("the default cap is %d and the ceiling %d, want both 50", defaultLiveCap, rpc.MaxAgentsCeiling)
	}
	d := startDaemon(t)
	awaitCap(attach(t, d.socket), rpc.MaxAgentsCeiling)
}

// Setting the cap on a running fleet is what the next spawn is measured against.
func TestSettingTheCapLowersItForTheNextSpawn(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.send(rpc.Frame{Kind: rpc.FrameSetMaxAgents, MaxAgents: 1})
	awaitCap(c, 1)

	first := uuid.NewString()
	if got := spawnFor(c, first, "", t.TempDir()); got.ID != first {
		t.Fatalf("the first spawn under a cap of 1 was refused: %+v", got)
	}
	second := uuid.NewString()
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: second, Dir: t.TempDir()})
	if why := awaitRefusal(c, second); !strings.Contains(why, "--max-agents") {
		t.Errorf("the refusal under a lowered cap is %q and does not say how to raise it", why)
	}
}

// Lowering the cap below what is running parks and stops nothing.
func TestLoweringTheCapStopsNothing(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	ids := []string{uuid.NewString(), uuid.NewString()}
	for _, id := range ids {
		spawnFor(c, id, "", t.TempDir())
	}
	c.send(rpc.Frame{Kind: rpc.FrameSetMaxAgents, MaxAgents: 1})
	awaitCap(c, 1)
	c.send(rpc.Frame{Kind: rpc.FrameStatus})
	c.await("both sessions still running", func(f rpc.Frame) bool {
		if f.Status == nil || f.Status.MaxAgents != 1 {
			return false
		}
		live := 0
		for _, s := range f.Status.Sessions {
			if s.State != rpc.StateEnded && s.State != rpc.StateParked {
				live++
			}
		}
		return live == len(ids)
	})
}

// Outside one to the ceiling is refused, and the cap stays where it was.
func TestACapOutsideOneToFiftyIsRefused(t *testing.T) {
	d := startDaemon(t)
	c := attach(t, d.socket)
	for _, n := range []int{0, -3, rpc.MaxAgentsCeiling + 1} {
		c.send(rpc.Frame{Kind: rpc.FrameSetMaxAgents, MaxAgents: n})
		c.await("a refusal of the cap", func(f rpc.Frame) bool {
			return f.Kind == rpc.FrameError && strings.Contains(f.Text, "--max-agents")
		})
	}
	awaitCap(c, rpc.MaxAgentsCeiling)
}

// The last door honours the fleet's cap too: admitLive is what holds it when
// two spawns race past maySpawn's count.
func TestTheLastDoorHoldsTheFleetsCap(t *testing.T) {
	s := newServer(filepath.Join(t.TempDir(), "s"))
	s.maxAgents.Store(1)
	first := core.Config{SessionID: idAlpha, Name: "sydney", Dir: t.TempDir()}
	a := newAgent(first.SessionID, first.Name, "task", first.Dir, "", core.NewSession(first), func() {})
	if why := s.admitLive(a, nil, false); why != "" {
		t.Fatalf("the first session under a cap of 1 was refused: %s", why)
	}
	second := core.Config{SessionID: testSessionID("b2b2"), Name: "alex", Dir: t.TempDir()}
	b := newAgent(second.SessionID, second.Name, "task", second.Dir, "", core.NewSession(second), func() {})
	if why := s.admitLive(b, nil, false); !strings.Contains(why, "cap") {
		t.Errorf("a second session under a cap of 1 was admitted at the last door: %q", why)
	}
}

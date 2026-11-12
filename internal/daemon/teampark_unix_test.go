//go:build unix

package daemon

// What /park @team and /resume <team> rely on, which is no frame of their own: a
// window writes one park, then one wake, per member back to back on its one
// connection (ui's App.write), and each member keeps its team through both.

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestATeamParkedAndWokenBackToBackKeepsItsTeam(t *testing.T) {
	fakeClaudeOnPath(t, "argv")
	d := startDaemon(t)
	c := attach(t, d.socket)
	members := []string{idAlpha, idBeta, idGamma}
	for i, id := range members {
		spawnTagged(c, id, []string{"alex", "sam", "riley"}[i], "backend")
	}

	// await reads frames held from before it began, so a woken row is told from the
	// spawned one by its new process, wakeOutcome's test.
	before := c.status()
	for _, id := range members {
		c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: id})
	}
	for _, id := range members {
		if got := c.awaitState(id, rpc.StateParked).Team; got != "backend" {
			t.Errorf("%s parked with team %q, want backend", id, got)
		}
	}

	for _, id := range members {
		c.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: id})
	}
	for _, id := range members {
		row := c.await(id+" woken", func(f rpc.Frame) bool {
			if f.Kind != rpc.FrameStatusPush || f.Status == nil {
				return false
			}
			s := sessionRow(*f.Status, id)
			return s.PID > 0 && s.PID != sessionRow(before, id).PID && s.State != rpc.StateParked
		})
		if got := sessionRow(*row.Status, id).Team; got != "backend" {
			t.Errorf("%s woke with team %q, want backend", id, got)
		}
	}
}

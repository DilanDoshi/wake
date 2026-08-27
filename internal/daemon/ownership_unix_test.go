//go:build unix

package daemon

import (
	"path/filepath"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A supervised agent is recorded through the ownership callback, before the
// supervisor is released to exec the target - so the durable record is on disk
// the moment the target could exist, which is the fork-to-record window BUG-16
// set out to close (the ordering itself is core's
// TestStartObservedOwnershipBlocksTargetUntilRelease). This guards the daemon
// half: a supervised spawn is recorded exactly once, with the supervisor's own
// process group, so the reaper can reach the whole tree. A wiring that recorded
// zero times would leave the reaper nothing to find.
func TestASupervisedAgentIsRecordedOnceThroughTheOwnershipCallback(t *testing.T) {
	useRealSupervisor(t)
	fakeClaudeOnPath(t, "supervised")
	d := startDaemon(t)
	c := attach(t, d.socket)

	id := testSessionID("b16a")
	c.spawn(id, "")
	c.awaitEvent(id, "supervised:")

	recs := loadRoster(rosterPath(d.socket))
	if len(recs) != 1 || recs[0].ID != id {
		t.Fatalf("supervised spawn recorded %+v, want exactly one record for %s", recs, id)
	}
	if recs[0].PID <= 0 {
		t.Fatalf("supervised record has PID %d, want the supervisor's positive pgid", recs[0].PID)
	}
}

// The cost of recording before release is that a start which then fails has a
// durable record to undo - otherwise the next daemon's reaper would KillGroup a
// pgid this start never kept alive, a hazard sharpened by the recorded pgid
// being an already-dead group ripe for recycling. This pins the undo: a
// supervised start whose target cannot chdir (a directory that passes the
// absolute-path fence but does not exist) records its group before release, then
// fails at the supervisor's chdir - and must leave no roster record behind.
func TestAFailedSupervisedStartLeavesNoStaleRosterRecord(t *testing.T) {
	useRealSupervisor(t)
	fakeClaudeOnPath(t, "supervised")
	d := startDaemon(t)
	c := attach(t, d.socket)

	id := testSessionID("b16b")
	missing := filepath.Join(t.TempDir(), "no-such-dir")
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: id, Text: "", Dir: missing})
	c.await("an error for the supervised start whose directory does not exist", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && f.SessionID == id
	})

	if recs := loadRoster(rosterPath(d.socket)); len(recs) != 0 {
		t.Fatalf("a failed supervised start left roster records %+v, want none - the pre-release record was not undone", recs)
	}
}

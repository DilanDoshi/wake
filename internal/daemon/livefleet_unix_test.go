//go:build unix

package daemon

// The one refusal that only the fleet-wide probe needs.
//
// inspect asks `ps -p <pid>` about one process, so "that pid is not there" and
// "ps could not answer" are separated by the *shape* of the failure - silence
// on both streams, and Exited() to rule out the probe this package killed
// itself. Those three shapes are held by inspect_unix_test.go and goneNow
// inherits every one of them.
//
// It also inherits a hazard inspect does not have. goneNow asks for the whole
// machine and reads **absence from the answer** as "this agent's process has
// ended", so a ps that answers *partially* declares live agents dead - and
// there is no per-pid shape left to notice it by, because every row it did
// return parses perfectly. The empty-listing refusal cannot see it either.
//
// What catches it is that the daemon is a process on this machine, so a
// listing of this machine contains the pid doing the asking. It costs nothing,
// no working ps can fail it, and it turns "some processes are missing" from an
// answer into a refusal. This is the test that makes that a checked claim
// rather than a comment: without it the guard is the only one here nothing
// could falsify.

import (
	"context"
	"testing"
	"time"
)

// TestAListingThatDoesNotContainTheAskerIsRefusedRatherThanRead drives the
// partial ps and requires both halves: the pass fails, and no agent in it is
// declared gone.
//
// Both are asserted because they are different failures. An error that still
// filled the map would have every caller acting on it - probeQuietAgents reads
// the map whether or not the error is nil, deliberately, so that a pass which
// established *something* is not thrown away. And a map that were empty because
// the function returned early on some other path would pass the second check
// while proving nothing, which is what the first one denies.
func TestAListingThatDoesNotContainTheAskerIsRefusedRatherThanRead(t *testing.T) {
	shortProbeTimeout(t, 2*time.Second)
	brokenPsOnPath(t, psPartial)

	// A real, living process of this test's own, so "gone" would be a lie
	// about something rather than about nothing.
	agent := startLingererInItsOwnGroup(t)

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	got, err := goneNow(ctx, []watched{{id: idAlpha, pid: agent.Process.Pid}})
	if err == nil {
		t.Errorf("goneNow = %v, nil through a ps that listed the machine without this daemon in it: a listing "+
			"that cannot be complete has to be refused, because absence from it is what marks an agent gone", got)
	}
	if got[idAlpha] {
		t.Errorf("goneNow said session %s was gone because a partial listing did not mention it, while its "+
			"process %d is running: that is every quiet agent reported silent at once, and then SIGKILLed by "+
			"process group at shutdown instead of stopped gently", idAlpha, agent.Process.Pid)
	}
}

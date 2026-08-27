package daemon

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// An effort the daemon does not recognise is refused before a name is claimed
// and before anything is started.
//
// This is the property that makes rpc.Frame.Effort safe to carry at all. The
// field is checked against core's closed set on arrival, so a client that never
// ran cmd/wake's own parser - anything that can dial the socket - still cannot
// put an arbitrary word on a command line. See the field's own comment for why
// that argument does not extend to a path.
func TestASpawnWithAnUnknownEffortStartsNothing(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir(), Effort: "ludicrous"})
	c.await("a refusal naming the effort", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && strings.Contains(f.Text, "ludicrous")
	})
}

// Every level core admits reaches a running session, so the daemon's guard and
// the CLI's parser cannot drift into a level one offers and the other refuses.
func TestEveryLevelCoreAdmitsSpawns(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	for i, level := range core.EffortLevels {
		// One id per level, minted the way the harness mints its own.
		id := testSessionID(fmt.Sprintf("e%03d", i))
		c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: id, Text: "", Dir: t.TempDir(), Effort: level})
		c.await("the session spawned at "+level, func(f rpc.Frame) bool {
			if f.Kind != rpc.FrameStatusReply || f.Status == nil {
				return false
			}
			for _, s := range f.Status.Sessions {
				if s.ID == id && s.State != rpc.StateEnded {
					return true
				}
			}
			return false
		})
	}
}

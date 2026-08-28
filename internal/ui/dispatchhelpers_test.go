package ui

// Shared setup for the subagent-sidebar tests: an open conversation that has
// dispatched a couple of subagents. It lived beside the dispatch list's own
// keys until that surface was replaced by the checklist board; the fold it
// exercises (Fleet.RunningTasks) is what the right sidebar draws.

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func taskFrame(sessionID string, ev core.Event) rpc.Frame {
	ev.SessionID = sessionID
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: sessionID, Event: &ev}
}

// dispatching is an open conversation with two subagents under it, one of which
// has forwarded a line.
func dispatching(t *testing.T) App {
	t.Helper()
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withSize(120, 40)
	for _, ev := range []core.Event{
		started("a1", "toolu_1", "Count lines in alpha.txt", "general-purpose", core.TaskAgent),
		started("a2", "toolu_2", "Grep the tree", "general-purpose", core.TaskAgent),
		spoke("toolu_1", subSaid),
	} {
		a = a.applyFrame(taskFrame("s1", ev))
	}
	return a
}

func viewedBy(t *testing.T, a App, sessionID string) string {
	t.Helper()
	d, ok := a.dms[sessionID]
	if !ok {
		t.Fatalf("no conversation for %s", sessionID)
	}
	return d.Viewed()
}

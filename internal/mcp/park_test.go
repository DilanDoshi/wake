// What the manager is shown about a session that has been parked.

package mcp

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A parked session is not an agent to act on, so list_agents leaves it out.
//
// The same rule ended and orphaned already have, and for the reason
// liveSessions states rather than by analogy: this list is what a model picks a
// recipient from, and a session with no process behind it is a turn spent
// discovering Wake's own bookkeeping. It is worse than the ended case, in fact,
// because a parked agent looks addressable from every other angle - it kept its
// name, its label and its directory, which is exactly what park exists to do.
//
// agent_status still answers for it. That is the same split ended already has:
// the roster is what you choose from, and the status is what you ask about
// something you already named.
func TestListAgentsLeavesOutASessionWithNoProcessBehindIt(t *testing.T) {
	f := fleetOf(
		rpc.SessionStatus{ID: idPeter, Name: "peter", Label: "api-v2", State: rpc.StateIdle},
		rpc.SessionStatus{ID: idJohn, Name: "john", Label: "api-v2", State: rpc.StateParked},
	)

	out := call(t, f, "list_agents", nil)
	if strings.Contains(out, "john") {
		t.Errorf("list_agents offered a parked agent:\n%s\nThe manager cannot send to a session whose "+
			"process has gone, and a row it can name is a row it will address", out)
	}
	if !strings.Contains(out, "peter") {
		t.Fatalf("list_agents lost the live agent too, so this test is asserting nothing about the parked one:\n%s", out)
	}
}

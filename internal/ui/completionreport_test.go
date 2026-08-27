package ui

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A client that learns of an agent only through a report - which is every late
// attach and every reattach - still gets its advertised commands, so the
// completion menu is not empty for it. The commands ride the one-per-turn init
// *event*, and no event is replayed to a client that attached after it, so
// without the report carrying them a reattached client saw no menu for any
// agent: `@alex /co` and `/co` in alex's own DM both showed nothing.
//
// This is the same route Effort and Budget already take, and for the same
// reason - see rpc.SessionStatus.Commands.
func TestAdvertisedCommandsSurviveAnAttachViaTheReport(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40)

	// A status push naming an agent with commands and no prior init event: the
	// reattach case, where the report is the only thing the client has.
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle, Commands: []string{"compact", "commit-push", "complete-linear-ticket"}},
	}}})

	agent, ok := a.fleet.Agent("s1")
	if !ok {
		t.Fatal("no agent s1 after the report")
	}
	if got := agent.advertised.words(); len(got) != 3 {
		t.Errorf("advertised is %v after a report carrying 3 commands; a client that only has the report "+
			"(every reattach) never learns them, so /co shows no menu", got)
	}
}

package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The room draws an info bar above its armed cue when it has an agent to draw
// one for, and none when it does not - the room aggregates a fleet, so a bar
// there is a fact about one named agent or nothing at all. A detach is armed to
// have a cue row at all, the only legend row now.
func TestRoomDrawsInfoBarAboveLegend(t *testing.T) {
	r := NewRoom().
		withBar(Agent{Cwd: "/tmp/repo", Model: "claude-opus-5", Effort: "xhigh"}, "auto", 120)
	r = r.WithComposer(r.Composer().WithArms(legendArms{detach: true})).SetSize(120, 24)

	out := stripANSI(r.View(120, 24))
	lines := strings.Split(out, "\n")
	barAt, hintAt := -1, -1
	for i, l := range lines {
		if strings.Contains(l, effortLabel+"xhigh") {
			barAt = i
		}
		if strings.Contains(l, armedSendLabel) {
			hintAt = i
		}
	}
	if barAt < 0 {
		t.Fatalf("the room info bar was not drawn:\n%s", out)
	}
	if hintAt < 0 {
		t.Fatalf("the armed cue was not drawn:\n%s", out)
	}
	if barAt > hintAt {
		t.Fatalf("the room bar (row %d) must sit above the cue (row %d):\n%s", barAt, hintAt, out)
	}

	empty := NewRoom().SetSize(120, 24).withBar(Agent{}, "", 120)
	if strings.Contains(stripANSI(empty.View(120, 24)), effortLabel) {
		t.Error("a room with no info-agent drew a bar")
	}
}

// The room's bar names the agent the composer is addressing - the manager when
// nothing is, that agent when a lone @name is - so it answers "what am I about
// to talk to", never "what is the roster cursor on".
func TestRoomBarFollowsTheAddressedAgentOrManager(t *testing.T) {
	a := newRoomApp(t).withSize(160, 40)
	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "mgr", Name: core.ManagerName, State: rpc.StateIdle, Cwd: "/tmp/mgr", Effort: "high"},
		{ID: "thea", Name: "thea", State: rpc.StateIdle, Cwd: "/tmp/thea", Effort: "xhigh"},
	}}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})

	// Nothing addressed: the manager's bar (effort high, its own directory).
	if got := stripANSI(a.room.bar); !strings.Contains(got, effortLabel+"high") || !strings.Contains(got, "/tmp/mgr") {
		t.Fatalf("an unaddressed room did not draw the manager's bar: %q", got)
	}

	// Addressing @thea: her bar (effort xhigh, her directory).
	a.room = a.room.WithComposer(a.room.Composer().WithDraft("@thea"))
	a = a.retarget()
	if got := stripANSI(a.room.bar); !strings.Contains(got, effortLabel+"xhigh") || !strings.Contains(got, "/tmp/thea") {
		t.Fatalf("addressing @thea did not draw her bar: %q", got)
	}

	// Clearing the draft returns to the manager.
	a.room = a.room.WithComposer(a.room.Composer().WithDraft(""))
	a = a.retarget()
	if got := stripANSI(a.room.bar); !strings.Contains(got, effortLabel+"high") {
		t.Fatalf("clearing the draft did not return the bar to the manager: %q", got)
	}
}

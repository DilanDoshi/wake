package ui

// ⌃Q interrupts every agent and cancels what claude has queued behind its turn,
// and a cancelled message is in no transcript, so neither ↑ nor a reopened pane
// brings it back. The window names what it drops instead. See parkdropped.go.

import (
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestCtrlQNamesTheMessagesItDropsFromClaudesQueue(t *testing.T) {
	fresh(t)
	a, _ := sendTo(t, busyDM(t), "and then the docs")
	notice.Reset()

	m, _, _ := confirmQuit(t, a)

	got := m.(App).DroppedOnPark()
	if len(got) != 1 || got[0].Text != "and then the docs" || !slices.Equal(got[0].To, []string{"alex"}) {
		t.Errorf("⌃Q reports %+v dropped, want alex's queued message: the interrupt cancels it and nothing on disk keeps it", got)
	}
	if said, _ := notice.Latest(); !strings.Contains(said.String(), "1 message") {
		t.Errorf("⌃Q said %q, and nothing about the queued message it is dropping", said.String())
	}
}

// A room broadcast is one message however many agents were working when it went,
// so it is named once, with every agent it was waiting at.
func TestABroadcastQueuedAtTwoAgentsIsNamedOnce(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "sydney", State: rpc.StateWorking},
			{ID: "s2", Name: "alex", State: rpc.StateWorking},
			{ID: "s6", Name: core.ManagerName, State: rpc.StateIdle},
		},
	}})
	m, _ := typeAndSubmit(a, "@all ship it")

	m, _, _ = confirmQuit(t, m.(App))

	got := m.(App).DroppedOnPark()
	if len(got) != 1 || got[0].Text != "@all ship it" {
		t.Fatalf("⌃Q reports %+v dropped, want the one broadcast", got)
	}
	if to := slices.Sorted(slices.Values(got[0].To)); !slices.Equal(to, []string{"alex", "sydney"}) {
		t.Errorf("the broadcast is named as waiting at %v, want both working agents", got[0].To)
	}
}

func TestCtrlQWithNothingQueuedNamesNothing(t *testing.T) {
	fresh(t)
	notice.Reset()

	m, _, _ := confirmQuit(t, idleDM(t))

	if got := m.(App).DroppedOnPark(); len(got) != 0 {
		t.Errorf("⌃Q reports %+v dropped with nothing queued", got)
	}
	if said, _ := notice.Latest(); strings.Contains(said.String(), "message") {
		t.Errorf("⌃Q said %q with nothing queued", said.String())
	}
}

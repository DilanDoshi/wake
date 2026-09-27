package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A wake after /clear comes back under the conversation it resumed, so the row
// the report stops naming is the same agent: its pane follows it, the resumed
// notice and the room's history ask still fire, and the old row is gone.
func TestAWokenAgentIsFollowedOntoTheConversationItResumed(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, seedOf(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked, Conversation: "c1"},
	)).withSize(200, 40)
	a = a.openDMWith("s1", "alex").awaitingWake("s1")

	m, cmd := a.Update(frameMsg{Frame: rpc.Frame{Kind: rpc.FrameStatusPush, Status: seedOf(
		rpc.SessionStatus{ID: "c1", Name: "alex", State: rpc.StateIdle},
	)}})
	got := m.(App)

	if _, still := got.fleet.Agent("s1"); still {
		t.Error("the pre-clear row is still in the fleet beside the session that replaced it")
	}
	if got.grid.Has("s1") || !got.grid.Has("c1") {
		t.Errorf("the pane did not follow the agent onto its conversation: grid %v", got.grid.Panes())
	}
	if len(got.waking) != 0 {
		t.Errorf("the wake is still awaited after it arrived: %v", got.waking)
	}
	if n, ok := notice.Latest(); !ok || !strings.Contains(n.String(), ResumedNotice("alex")) {
		t.Errorf("no resumed notice after the wake arrived, got %q", n.String())
	}
	var asked []string
	for _, f := range batchFrames(t, got, cmd) {
		if f.Kind == rpc.FrameRoomHistory {
			asked = append(asked, f.SessionID)
		}
	}
	if !slices.Contains(asked, "c1") {
		t.Errorf("the room asked about %v after the wake, want the conversation c1 it came back under", asked)
	}
}

// A window that did not ask for the wake drops the stale row too: the fleet
// never drops a row a report stops listing, so it would stay parked forever.
func TestAnotherWindowDropsTheRowAWakeReKeyed(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, seedOf(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked, Conversation: "c1"},
	)).withSize(200, 40)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: seedOf(
		rpc.SessionStatus{ID: "c1", Name: "alex", State: rpc.StateIdle},
	)})
	if _, still := a.fleet.Agent("s1"); still {
		t.Error("the re-keyed row lingers in a window that did not ask for the wake")
	}
}

// /resume lists neither half of a cleared agent twice: a live agent's current
// conversation is not a free session, and a parked one's disk row is its row.
func TestTheResumePickerKnowsWhatConversationAClearedAgentIsWriting(t *testing.T) {
	st := rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "live1", Name: "alex", State: rpc.StateIdle, Conversation: "c-live"},
			{ID: "park1", Name: "iris", State: rpc.StateParked, Conversation: "c-park"},
		},
	}
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	now := time.Now()
	rows, _ := a.resumeRowsFrom([]DiskSession{
		{ID: "c-live", Dir: "/l", Modified: now},
		{ID: "c-park", Dir: "/p", Modified: now},
		{ID: "stranger", Dir: "/s", Modified: now},
	})
	byID := map[string]resumeRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if _, listed := byID["c-live"]; listed {
		t.Error("the conversation a live agent is writing was offered as a free session: resuming it puts a second process on it")
	}
	if r, ok := byID["park1"]; !ok || !r.Parked {
		t.Errorf("the parked cleared agent is not one parked row: %+v", rows)
	}
	if _, listed := byID["c-park"]; listed {
		t.Error("the parked agent's conversation was listed a second time as a stranger")
	}
	if _, listed := byID["stranger"]; !listed {
		t.Error("an ordinary disk session went missing")
	}
}

// A report can still list a ⌃C row and its park book record together, so the
// parked set folds them, whichever id the record carries.
func TestAParkedAgentIsListedOnceWhateverItsRecordIsCalled(t *testing.T) {
	st := rpc.Status{
		Running:  true,
		Sessions: []rpc.SessionStatus{{ID: "park1", Name: "iris", State: rpc.StateParked, Conversation: "c1"}},
		Parked:   []rpc.SessionStatus{{ID: "c1", Name: "iris", State: rpc.StateParked}},
	}
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	if got := a.parkedAgents(); len(got) != 1 {
		t.Errorf("one parked agent is listed %d times: %+v", len(got), got)
	}
}

// A park keeps what was typed in the composer, and so does the wake that
// re-keys the pane: the draft moves with the agent onto its conversation.
func TestAReKeyedPaneKeepsItsDraft(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, seedOf(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked, Conversation: "c1"},
	)).withSize(200, 40)
	a = a.openDMWith("s1", "alex").withDraft("half a thought")
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: seedOf(
		rpc.SessionStatus{ID: "c1", Name: "alex", State: rpc.StateIdle},
	)})
	if got := a.dms["c1"].Composer().Value(); got != "half a thought" {
		t.Errorf("the draft on the re-keyed pane is %q, want what was typed before the wake", got)
	}
}

// A window attached to an agent reattaches to it after a hang-up, so once a wake
// re-keys the agent the window reattaches to its conversation, not the dead id.
func TestAReKeyedAttachmentReattachesToTheConversation(t *testing.T) {
	fresh(t)
	d := &stubDialer{err: errors.New("stop here")}
	a := NewRoomApp(newRecorder(t), Stream{}, seedOf(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked, Conversation: "c1"},
	)).withSize(200, 40).WithOpenDM("s1", "alex").WithDialer(d.dial)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: seedOf(
		rpc.SessionStatus{ID: "c1", Name: "alex", State: rpc.StateIdle},
	)})
	_, cmd := a.hungUp(errors.New("hung up"))
	if cmd == nil {
		t.Fatal("a hang-up with a dialer wired did not try to reattach")
	}
	cmd()
	if d.asked != "c1" {
		t.Errorf("the reattach asked for %q, want the conversation c1 the agent was re-keyed onto", d.asked)
	}
}

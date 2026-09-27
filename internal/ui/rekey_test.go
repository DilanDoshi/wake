package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A wake after /clear comes back under the conversation it resumed, so the row
// the report stops naming is the same agent: its pane follows it, the resumed
// notice fires and the old row is gone. The room is not asked about the new id:
// it already restored that conversation under the old one at the seed, and a
// second restore would draw it twice and read every private turn as public.
func TestAWokenAgentIsFollowedOntoTheConversationItResumed(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, seedOf(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked, Conversation: "c1"},
	)).withSize(200, 40)
	a = a.openDMWith("s1", "alex").awaitingWake("s1")

	m, cmd := a.Update(frameMsg{Frame: rpc.Frame{Kind: rpc.FrameStatusPush, Status: seedOf(
		rpc.SessionStatus{ID: "c1", Name: "alex", State: rpc.StateIdle, PID: 4242},
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
	for _, f := range batchFrames(t, got, cmd) {
		if f.Kind == rpc.FrameRoomHistory && f.SessionID == "c1" {
			t.Error("the room asked about c1 again after restoring the same conversation under s1")
		}
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
		rpc.SessionStatus{ID: "c1", Name: "alex", State: rpc.StateIdle, PID: 4242},
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
		{ID: "park1", Dir: "/p", Modified: now.Add(-time.Hour)},
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
	parkedRows := 0
	for _, r := range rows {
		if r.ID == "park1" {
			parkedRows++
		}
	}
	if parkedRows != 1 {
		t.Errorf("the parked agent's pre-clear transcript was offered beside its row (%d rows for park1): the daemon refuses it while the agent is held", parkedRows)
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
		rpc.SessionStatus{ID: "c1", Name: "alex", State: rpc.StateIdle, PID: 4242},
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
		rpc.SessionStatus{ID: "c1", Name: "alex", State: rpc.StateIdle, PID: 4242},
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

// A woken row is reported before its process exists, and a wake that then fails
// puts the old row back - so the re-key waits for the woken process.
func TestAWakeIsNotFollowedBeforeItsProcessExists(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, seedOf(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked, Conversation: "c1"},
	)).withSize(200, 40)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: seedOf(
		rpc.SessionStatus{ID: "c1", Name: "alex", State: rpc.StateIdle},
	)})
	if _, still := a.fleet.Agent("s1"); !still {
		t.Error("the row was re-keyed onto a woken session with no process yet; a failed wake would leave a ghost")
	}
}

// Following a woken agent onto its conversation moves nothing the operator
// pointed at: the keys, the roster cursor and the fleet's focus stay where they
// were, so ⌃C still parks whoever was picked.
func TestAReKeyDoesNotMoveTheKeysOrTheCursor(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, seedOf(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked, Conversation: "c1"},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateIdle, PID: 7},
	)).withSize(200, 40)
	a = a.openDMWith("s1", "alex").refocus("")
	a.fleet = a.fleet.Focus("")
	a.roster.Selected = "s2"
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: seedOf(
		rpc.SessionStatus{ID: "c1", Name: "alex", State: rpc.StateIdle, PID: 4242},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateIdle, PID: 7},
	)})
	if a.focus != "" || a.roster.Selected != "s2" || a.fleet.Focused() != "" {
		t.Errorf("the re-key moved focus %q, roster %q, fleet focus %q; want the room, s2 and none", a.focus, a.roster.Selected, a.fleet.Focused())
	}
}

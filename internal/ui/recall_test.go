package ui

// Taking queued messages back (↑) and sending now (⌃]), Claude Code's two
// gestures over its queue. Both start by asking claude for the queued messages
// back; only each message's own lifecycle says whether it was in time. See
// recall.go and docs/superpowers/notes/2026-10-02-mid-turn-delivery-findings.md.

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// queuedTwo is a working s1 with two messages written and pinned.
func queuedTwo(t *testing.T) (App, []string) {
	t.Helper()
	a, f1 := sendTo(t, busyDM(t), "first")
	a, f2 := sendTo(t, a, "second")
	return a, []string{f1.MessageID, f2.MessageID}
}

// roomSays is whether the room draws text.
func roomSays(a App, text string) bool {
	return strings.Contains(ansi.Strip(a.room.View(200, 40)), text)
}

func hit(t *testing.T, a App, k tea.KeyType) (App, tea.Cmd) {
	t.Helper()
	m, cmd := a.Update(tea.KeyMsg{Type: k})
	return m.(App), cmd
}

// recallsOf is the message ids a batch of frames asks claude to give back.
func recallsOf(frames []rpc.Frame) []string {
	var ids []string
	for _, f := range frames {
		if f.Kind == rpc.FrameRecall {
			ids = append(ids, f.MessageID)
		}
	}
	return ids
}

// ↑ from the top of the draft asks for every queued message back; what claude
// gives back returns to the composer, one per line, oldest first, ahead of the
// draft - and is never drawn as sent.
func TestUpTakesTheQueuedMessagesBackIntoTheDraft(t *testing.T) {
	a, ids := queuedTwo(t)
	a = a.withDraft("and this")
	a, cmd := hit(t, a, tea.KeyUp)
	if got := recallsOf(sentFrames(t, a, cmd)); !slices.Equal(got, ids) {
		t.Fatalf("↑ recalled %v, want both queued messages %v", got, ids)
	}
	for _, id := range ids {
		a = a.applyFrame(lifecycleFrame("s1", id, "cancelled"))
	}
	if got := a.composer().Value(); got != "first\nsecond\nand this" {
		t.Errorf("the draft is %q, want the taken-back messages ahead of it", got)
	}
	if len(a.queued["s1"]) != 0 || echoedInDM(a, "s1", "first") {
		t.Errorf("queued = %v: a taken-back message is still pinned or drawn as sent", a.queued["s1"])
	}
}

// One claude took up before the take-back reached it stays delivered; only the
// one it gave back returns to the draft.
func TestATakeBackClaudeWasTooLateForStaysDelivered(t *testing.T) {
	a, ids := queuedTwo(t)
	a, _ = hit(t, a, tea.KeyUp)
	a = a.applyFrame(lifecycleFrame("s1", ids[0], "started"))
	a = a.applyFrame(lifecycleFrame("s1", ids[1], "cancelled"))
	if got := a.composer().Value(); got != "second" {
		t.Errorf("the draft is %q, want only the message claude gave back", got)
	}
	if !echoedInDM(a, "s1", "first") {
		t.Error("the message claude took up was not drawn as sent")
	}
}

// With nothing queued, ↑ is still the prompt history.
func TestUpWithNothingQueuedIsStillHistory(t *testing.T) {
	a, _ := sendTo(t, idleDM(t), "remembered")
	a, cmd := hit(t, a, tea.KeyUp)
	if cmd != nil {
		if got := recallsOf(batchFrames(t, a, cmd)); len(got) != 0 {
			t.Fatalf("↑ with nothing queued recalled %v", got)
		}
	}
	if got := a.composer().Value(); got != "remembered" {
		t.Errorf("↑ put %q in the draft, want the last prompt", got)
	}
}

// A held /rename is not Wake's to take back: its mirror waits with it, and it
// was never written.
func TestAHeldRenameIsNotTakenBack(t *testing.T) {
	m, _ := typeAndSubmit(busyDM(t), "/rename bob")
	a, cmd := hit(t, m.(App), tea.KeyUp)
	if cmd != nil {
		if got := recallsOf(batchFrames(t, a, cmd)); len(got) != 0 {
			t.Fatalf("↑ recalled a held /rename: %v", got)
		}
	}
	if q := a.queued["s1"]; len(q) != 1 || !q[0].held {
		t.Errorf("queued = %+v, want the held /rename untouched", q)
	}
}

// A room broadcast taken back from its target's conversation leaves the room a
// record that it never reached that agent.
func TestATakenBackRoomMessageLeavesTheRoomARecord(t *testing.T) {
	a := idleDM(t).enqueue("s1", newQueued("r1", "ship it", "@alex ship it", nil, true)).
		applyFrame(oneAgent("s1", "alex", rpc.StateWorking))
	a, _ = hit(t, a, tea.KeyUp)
	a = a.applyFrame(lifecycleFrame("s1", "r1", "cancelled"))
	if got := a.composer().Value(); got != "ship it" {
		t.Errorf("the draft is %q, want the broadcast's text", got)
	}
	if !roomSays(a, "took back a message to alex") {
		t.Error("the room was not told the broadcast never reached alex")
	}
}

// ⌃] with nothing queued sends the draft now: priority now, pinned until
// claude takes it up.
func TestSendNowWritesTheDraftNow(t *testing.T) {
	a, cmd := hit(t, busyDM(t).withDraft("stop, use the fixture"), tea.KeyCtrlCloseBracket)
	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameSend || f.Text != "stop, use the fixture" || !f.Now {
		t.Errorf("⌃] wrote %+v, want the draft sent now", f)
	}
	if q := a.queued["s1"]; len(q) != 1 || q[0].id != f.MessageID {
		t.Errorf("queued = %+v, want the now message pinned until claude takes it up", q)
	}
}

// ⌃] with messages queued takes them back first - a now behind a queued
// message ends the turn rather than backgrounding its work
// (midturn-next-then-now.jsonl) - then sends them and the draft as one.
func TestSendNowTakesTheQueueBackThenSendsItWithTheDraft(t *testing.T) {
	a, ids := queuedTwo(t)
	a, cmd := hit(t, a.withDraft("third"), tea.KeyCtrlCloseBracket)
	if got := recallsOf(sentFrames(t, a, cmd)); !slices.Equal(got, ids) {
		t.Fatalf("⌃] recalled %v, want both queued messages %v", got, ids)
	}
	a = a.applyFrame(lifecycleFrame("s1", ids[0], "cancelled"))
	var cmds []tea.Cmd
	m, c := a.Update(streamMsg{gen: a.gen, batch: batch{frames: []rpc.Frame{lifecycleFrame("s1", ids[1], "cancelled")}}})
	a, cmds = m.(App), append(cmds, c)
	var now []rpc.Frame
	for _, f := range batchFrames(t, a, tea.Batch(cmds...)) {
		if f.Kind == rpc.FrameSend {
			now = append(now, f)
		}
	}
	if len(now) != 1 || now[0].Text != "first\nsecond\nthird" || !now[0].Now {
		t.Fatalf("after the take-back ⌃] wrote %+v, want one now message carrying all three", now)
	}
	if a.composer().Value() != "" {
		t.Errorf("the draft %q was left in the composer", a.composer().Value())
	}
}

// A queued command is left where it is: claude runs commands after the turn,
// and one sent inside a message would be text.
func TestSendNowLeavesAQueuedCommandQueued(t *testing.T) {
	a, _ := sendTo(t, busyDM(t), "/compact")
	a, cmd := hit(t, a.withDraft("now this"), tea.KeyCtrlCloseBracket)
	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameSend || f.Text != "now this" || !f.Now {
		t.Errorf("⌃] wrote %+v, want the draft alone sent now", f)
	}
	if q := a.queued["s1"]; len(q) != 2 || q[0].wire != "/compact" || q[0].recalling {
		t.Errorf("queued = %+v, want the /compact left as it was", q)
	}
}

// To an idle agent ⌃] is an ordinary send; with nothing to send it does nothing.
func TestSendNowToAnIdleAgentIsAnOrdinarySend(t *testing.T) {
	a, cmd := hit(t, idleDM(t).withDraft("hello"), tea.KeyCtrlCloseBracket)
	if f := sentFrame(t, a, cmd); f.Text != "hello" || f.Now || !echoedInDM(a, "s1", "hello") {
		t.Errorf("⌃] to an idle agent wrote %+v, want an ordinary send, drawn", f)
	}
	if _, cmd := hit(t, idleDM(t), tea.KeyCtrlCloseBracket); cmd != nil {
		t.Error("⌃] with no draft and nothing queued wrote something")
	}
}

// In the room ⌃] sends the draft now to each working target and as an ordinary
// send to each idle one.
func TestSendNowInTheRoomHurriesOnlyTheWorkingTargets(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "sydney", State: rpc.StateIdle},
			{ID: "s2", Name: "alex", State: rpc.StateWorking},
			{ID: "s6", Name: core.ManagerName, State: rpc.StateIdle},
		},
	}})
	a, cmd := hit(t, a.withDraft("@all stop"), tea.KeyCtrlCloseBracket)
	now := map[string]bool{}
	for _, f := range sentFrames(t, a, cmd) {
		now[f.SessionID] = f.Now
	}
	if len(now) != 2 || !now["s2"] || now["s1"] { // @all is the fleet, the manager aside
		t.Errorf("now by target = %v, want only the working alex hurried", now)
	}
}

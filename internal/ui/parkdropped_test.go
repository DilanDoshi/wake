package ui

// ⌃Q interrupts every agent and cancels what claude has queued behind its turn,
// and a cancelled message is in no transcript, so neither ↑ nor a reopened pane
// brings it back. The window names what claude had not read instead. See
// parkdropped.go.

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/version"
)

func queuedTexts(a App) []string {
	var out []string
	for _, q := range a.QueueAtQuit().Messages {
		out = append(out, q.Text)
	}
	return out
}

func TestCtrlQNamesTheMessagesClaudeHadNotRead(t *testing.T) {
	fresh(t)
	a, _ := sendTo(t, busyDM(t), "and then the docs")
	notice.Reset()

	m, _, _ := confirmQuit(t, a)

	got := m.(App).QueueAtQuit().Messages
	if len(got) != 1 || got[0].Text != "and then the docs" || !slices.Equal(got[0].To, []string{"alex"}) {
		t.Errorf("⌃Q reports %+v queued, want alex's message: the interrupt cancels it and nothing on disk keeps it", got)
	}
	if said, _ := notice.Latest(); !strings.Contains(said.String(), "1 message") {
		t.Errorf("⌃Q said %q, and nothing about the queued message", said.String())
	}
}

// Only a daemon on this build interrupts every turn and cancels the queue; one on
// another build may let a turn run, so the notice keeps the grace's hedge.
func TestCtrlQPromisesAnInterruptOnlyFromADaemonOnThisBuild(t *testing.T) {
	for _, tc := range []struct {
		build string
		says  string
	}{{version.Build(), "interrupted"}, {"", "finished in time"}} {
		fresh(t)
		notice.Reset()
		a := busyDM(t).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
			Running: true, Build: tc.build,
			Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateWorking}},
		}})

		m, _, _ := confirmQuit(t, a)

		if got := m.(App).QueueAtQuit().Cancelled; got != (tc.build == version.Build()) {
			t.Errorf("daemon build %q: ⌃Q reports cancelled %v", tc.build, got)
		}
		if said, _ := notice.Latest(); !strings.Contains(said.String(), tc.says) {
			t.Errorf("daemon build %q: ⌃Q said %q, want %q", tc.build, said.String(), tc.says)
		}
	}
}

// A take-back or send-now in flight holds messages outside the pin: what claude
// has given back so far, and the send-now's own words. Closing drops them too.
func TestCtrlQNamesASendNowStillOnItsWay(t *testing.T) {
	fresh(t)
	a, ids := queuedTwo(t)
	a, _ = hit(t, a.withDraft("third"), tea.KeyCtrlCloseBracket)
	a = a.applyFrame(lifecycleFrame("s1", ids[0], "cancelled"))

	m, _, _ := confirmQuit(t, a)

	if got := queuedTexts(m.(App)); !slices.Equal(got, []string{"first", "second", "third"}) {
		t.Errorf("⌃Q mid send-now reports %v, want the one given back, the one still pinned and the draft", got)
	}
}

// A room broadcast is one message however many agents were working when it went,
// so it is named once, with every agent it was waiting at.
func TestABroadcastQueuedAtTwoAgentsIsNamedOnce(t *testing.T) {
	fresh(t)
	m, _ := typeAndSubmit(twoWorking(t), "@all ship it")

	m, _, _ = confirmQuit(t, m.(App))

	got := m.(App).QueueAtQuit().Messages
	if len(got) != 1 || got[0].Text != "@all ship it" {
		t.Fatalf("⌃Q reports %+v queued, want the one broadcast", got)
	}
	if to := slices.Sorted(slices.Values(got[0].To)); !slices.Equal(to, []string{"alex", "sydney"}) {
		t.Errorf("the broadcast is named as waiting at %v, want both working agents", got[0].To)
	}
}

// A room ⌃] joins what each agent gave back to the draft under the send's own
// uuid, so one send's copies can carry different words; each wording is named.
func TestASendNowsCopiesWithDifferentWordsAreEachNamed(t *testing.T) {
	fresh(t)
	m, cmd := typeAndSubmit(twoWorking(t), "@sydney first the logs")
	a := m.(App)
	first := sentFrame(t, a, cmd).MessageID
	a, _ = hit(t, a.withDraft("@all stop"), tea.KeyCtrlCloseBracket)
	m, _ = a.Update(streamMsg{gen: a.gen, batch: batch{frames: []rpc.Frame{lifecycleFrame("s1", first, "cancelled")}}})
	if q := m.(App).queued["s1"]; len(q) != 1 || !strings.Contains(q[0].echo, "first the logs") {
		t.Fatalf("sydney's queue is %+v, want the given-back message joined to the draft", q)
	}

	m, _, _ = confirmQuit(t, m.(App))

	got := queuedTexts(m.(App))
	if len(got) != 2 || !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, "first the logs") }) {
		t.Errorf("⌃Q reports %q, want sydney's joined message and alex's draft each named", got)
	}
}

func TestCtrlQWithNothingQueuedNamesNothing(t *testing.T) {
	fresh(t)
	notice.Reset()

	m, _, _ := confirmQuit(t, idleDM(t))

	if got := m.(App).QueueAtQuit().Messages; len(got) != 0 {
		t.Errorf("⌃Q reports %+v queued with nothing queued", got)
	}
	if said, _ := notice.Latest(); strings.Contains(said.String(), "message") {
		t.Errorf("⌃Q said %q with nothing queued", said.String())
	}
}

// twoWorking is a room whose sydney and alex are both mid-turn.
func twoWorking(t *testing.T) App {
	t.Helper()
	return newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "sydney", State: rpc.StateWorking},
			{ID: "s2", Name: "alex", State: rpc.StateWorking},
			{ID: "s6", Name: core.ManagerName, State: rpc.StateIdle},
		},
	}})
}

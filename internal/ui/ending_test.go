package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A session ending, and what a composer does about it.
//
// The daemon's push on retire is its only unsolicited account of a session
// dying, and a client that ignores it leaves a live-looking conversation whose
// composer swallows every keystroke. endedPush lives here because these are the
// tests it exists for; reattach_test.go borrows it for the one ending that must
// not be reattached to.

func endedPush(kind, sessionID, why string) rpc.Frame {
	return rpc.Frame{Kind: kind, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "other", State: rpc.StateWorking},
			{ID: sessionID, State: rpc.StateEnded, Error: why},
		},
	}}
}

// The daemon's push on retire is its only unsolicited account of a session
// dying. A crash also produces a FrameError, so ignoring the push leaves
// exactly the clean and quiet endings unreported - the common ones - and the
// conversation looks alive with a composer that swallows every keystroke.
func TestASessionEndingIsVisible(t *testing.T) {
	frames, errs := openStream(t, endedPush(rpc.FrameStatusPush, "s1", "exit status 1"))
	var m tea.Model = sizedApp(t, frames, errs, "s1")

	m, _ = step(t, m, m.(App).listen())

	out := shown(m)
	if !strings.Contains(out, "ended") {
		t.Errorf("the session ending left no trace in the view:\n%s", out)
	}
	if !strings.Contains(out, "exit status 1") {
		t.Errorf("the view does not say why the session ended:\n%s", out)
	}
}

// A clean exit carries no Error, and is the case most likely to be reported as
// nothing at all.
func TestACleanEndingIsAlsoVisible(t *testing.T) {
	frames, errs := openStream(t, endedPush(rpc.FrameStatusPush, "s1", ""))
	var m tea.Model = sizedApp(t, frames, errs, "s1")

	m, _ = step(t, m, m.(App).listen())

	if !strings.Contains(shown(m), "ended") {
		t.Errorf("a clean ending left no trace in the view:\n%s", shown(m))
	}
}

// The spawning client is sent a reply rather than a push, so both kinds carry
// this news.
func TestAnEndingInAReplyIsVisibleToo(t *testing.T) {
	frames, errs := openStream(t, endedPush(rpc.FrameStatusReply, "s1", "exit status 1"))
	var m tea.Model = sizedApp(t, frames, errs, "s1")

	m, _ = step(t, m, m.(App).listen())

	if !strings.Contains(shown(m), "ended") {
		t.Errorf("an ending carried by a reply was ignored:\n%s", shown(m))
	}
}

// Another agent ending is the fleet's business, not this conversation's.
func TestAnotherSessionEndingIsNotReportedHere(t *testing.T) {
	frames, errs := openStream(t, endedPush(rpc.FrameStatusPush, "s2", "exit status 1"))
	var m tea.Model = sizedApp(t, frames, errs, "s1")

	m, _ = step(t, m, m.(App).listen())

	if n := notice.Count(endedText(rpc.SessionStatus{Error: "exit status 1"})); n != 0 {
		t.Errorf("another session's ending was reported here %d times", n)
	}
	if strings.Contains(shown(m), "ended") {
		t.Errorf("another session's ending reached this view:\n%s", shown(m))
	}
}

// A session that is merely working must not be mourned.
func TestALiveSessionInAStatusPushIsNotAnEnding(t *testing.T) {
	frames, errs := openStream(t, rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running:  true,
		Sessions: []rpc.SessionStatus{{ID: "s1", State: rpc.StateWorking}},
	}})
	var m tea.Model = sizedApp(t, frames, errs, "s1")

	m, _ = step(t, m, m.(App).listen())

	if n, reported := notice.Latest(); reported {
		t.Errorf("a working session produced the notice %q", n.Text)
	}
	if strings.Contains(shown(m), "ended") {
		t.Errorf("a working session was reported as ended:\n%s", shown(m))
	}
}

// The keystrokes were being swallowed silently, which is the half of the
// finding that costs someone their afternoon.
func TestSendingIntoAnEndedSessionSaysSoInsteadOfSwallowingIt(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	var m tea.Model = dmApp(conn, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = m.Update(frameMsg{Frame: endedPush(rpc.FrameStatusPush, "s1", "exit status 1")})

	m = typeText(m, "are you there")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if msg := runCmdQuietly(cmd); msg != nil {
			t.Errorf("sending into an ended session produced %v", msg)
		}
	}

	select {
	case f := <-sent:
		t.Errorf("a message was written to an ended session: %+v", f)
	case <-time.After(50 * time.Millisecond):
	}
	if !strings.Contains(shown(m), "nothing more can be sent") {
		t.Errorf("the keystroke was swallowed with no explanation:\n%s", shown(m))
	}
	// And the draft survives, because there is nowhere for it to have gone.
	if got := m.(App).dm().Composer().Value(); got != "are you there" {
		t.Errorf("the draft was destroyed as well as undelivered: %q", got)
	}
}

// One ending, one notice, however many pushes carry it afterwards.
func TestTheEndingIsReportedOnce(t *testing.T) {
	fresh(t)
	var m tea.Model = dmApp(nil, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for range 5 {
		m, _ = m.Update(frameMsg{Frame: endedPush(rpc.FrameStatusPush, "s1", "exit status 1")})
	}

	if n := notice.Count(endedText(rpc.SessionStatus{Error: "exit status 1"})); n != 1 {
		t.Errorf("the ending was reported %d times, want 1", n)
	}
}

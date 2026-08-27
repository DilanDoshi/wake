package ui

// Esc, and what a stopped turn looks like once it lands.
//
// Two halves that have to agree: the key has to reach the daemon, and the
// legend under the composer has to name it - CLAUDE.md's rule is that the hint
// line describes only what the build actually does, in both directions. A key
// that works and is not advertised is a feature nobody finds; one that is
// advertised and does nothing is worse, because it is believed.

import (
	"net"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestEscSendsAnInterruptToTheDaemon(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	var m tea.Model = dmApp(conn, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	go func() { _ = runCmdQuietly(cmd) }()

	f := awaitFrame(t, sent)
	if f.Kind != rpc.FrameInterrupt {
		t.Errorf("frame kind = %q, want %q", f.Kind, rpc.FrameInterrupt)
	}
	if f.SessionID != "s1" {
		t.Errorf("frame session = %q, want s1 - an interrupt with no session is one the daemon cannot route", f.SessionID)
	}
	// The correlator is minted where the write happens, and nowhere else: a
	// client that invented one would be naming a request core never sent.
	if f.RequestID != "" {
		t.Errorf("frame carries RequestID %q, want none - core.Session mints it as it writes", f.RequestID)
	}
	if f.Text != "" {
		t.Errorf("frame carries text %q, want none", f.Text)
	}
}

// Esc is not a cancel-the-message key and must not behave like one. A person
// who is halfway through typing while the agent runs off in the wrong
// direction presses this exactly then, and losing the draft would be a second
// thing gone wrong in the same keystroke.
func TestEscLeavesTheDraftAlone(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	var m tea.Model = dmApp(conn, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m = typeText(m, "no, do it the other way")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	go func() { _ = runCmdQuietly(cmd) }()
	awaitFrame(t, sent)

	if got := m.(App).dm().Composer().Value(); got != "no, do it the other way" {
		t.Errorf("the draft is %q after Esc, want it untouched", got)
	}
}

// There is no turn to stop on a session that has ended, and nothing on the
// other end of the socket that would answer. It is silent rather than a
// notice: unlike a message, an interrupt loses nothing by not being sent.
func TestEscOnAnEndedSessionSendsNothing(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	var m tea.Model = dmApp(conn, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m.(App).apply(rpc.Frame{
		Kind:   rpc.FrameStatusPush,
		Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "s1", State: rpc.StateEnded}}},
	})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	go func() { _ = runCmdQuietly(cmd) }()

	select {
	case f := <-sent:
		t.Errorf("an interrupt was sent to a session that has ended: %+v", f)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAFailedInterruptIsVisible(t *testing.T) {
	fresh(t)
	mine, theirs := net.Pipe()
	_ = theirs.Close()
	_ = mine.Close()

	var m tea.Model = dmApp(mine, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = m.Update(runCmd(t, cmd))

	// The failure's own words, not the word "interrupt", which is in the hint
	// line and would make this pass with the whole error path deleted.
	if !strings.Contains(shown(m), "closed pipe") {
		t.Errorf("an interrupt that could not be written left no trace in the view:\n%s", shown(m))
	}
}

// The other half of what an operator sees. Claude's abort marker arrives as an
// ordinary user frame with nothing on it to say otherwise, so drawn naively it
// is the operator being shown words they did not write, under their own name,
// every single time they stop a turn.
func TestAnInterruptMarkerIsNotDrawnAsTheUsersTurn(t *testing.T) {
	for _, marker := range []string{
		"[Request interrupted by user]",
		"[Request interrupted by user for tool use]",
	} {
		d := NewDM("s1", "alex").SetSize(80, 20)
		d = d.Append(core.Event{
			Kind:      core.KindUserText,
			SessionID: "s1",
			Text:      marker,
			Notice:    core.NoticeTurnInterrupted,
		})
		view := stripANSI(d.View(80, 20))

		if strings.Contains(view, userLabel) {
			t.Errorf("%q is drawn under %q, the label for something the human typed:\n%s", marker, userLabel, view)
		}
		if strings.Contains(view, marker) {
			t.Errorf("Claude's own wording reached the transcript verbatim:\n%s", view)
		}
		if !strings.Contains(view, interruptedLabel) {
			t.Errorf("the transcript says nothing about the turn being stopped:\n%s", view)
		}
	}
}

// A notice this view has no label for must not cost the operator their
// message.
//
// KindUserText is the one kind where an unrendered notice is not a missing
// decoration: the notice is drawn *instead of* the text, so a notice with no
// row in noticeLabel would silently delete somebody's own words from their
// conversation. The airlock resolves notices from content now, so the next one
// added is the way in - and it would be invisible, because nothing about a
// blank block says a message went missing.
func TestAUserTurnCarryingAnUnlabelledNoticeKeepsItsText(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 20)
	d = d.Append(core.Event{
		Kind:      core.KindUserText,
		SessionID: "s1",
		Text:      "do not lose this",
		Notice:    core.Notice("a_notice_nobody_gave_a_label"),
	})
	view := stripANSI(d.View(80, 20))

	if !strings.Contains(view, "do not lose this") {
		t.Errorf("the message vanished behind a notice with no label:\n%s", view)
	}
	if !strings.Contains(view, userLabel) {
		t.Errorf("the message lost its speaker:\n%s", view)
	}
}

// An ordinary user turn still reads as one. Without this the test above is
// satisfied by a DM that never draws the user's side at all.
func TestAnOrdinaryUserTurnIsStillDrawnAsOne(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 20)
	d = d.Append(core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "run the tests"})
	view := stripANSI(d.View(80, 20))

	if !strings.Contains(view, userLabel) {
		t.Errorf("a user turn is no longer headed %q:\n%s", userLabel, view)
	}
	if !strings.Contains(view, "run the tests") {
		t.Errorf("a user turn lost its text:\n%s", view)
	}
}

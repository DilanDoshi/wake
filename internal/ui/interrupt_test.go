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
	"github.com/charmbracelet/x/ansi"

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

// A pending AskUserQuestion is not withdrawn by an interrupt the way a
// permission is. The CLI cancels a pending permission on a
// control_cancel_request the moment esc lands (interrupt-permission-findings),
// so its card clears; a question is preserved instead (question-findings.md §9,
// tengu_auq_park_preserved_at_shutdown), so the ask stays open and its card
// never clears - the reported bug. So esc on a question settles it with a deny,
// the same frame [d] writes, which unblocks the agent and takes the card down.
func TestEscOnAQuestionDeniesItInsteadOfInterrupting(t *testing.T) {
	a, _ := asking(t, 200)

	card, ok := a.cards.For("s1")
	if !ok {
		t.Fatal("the question is not open, so this test asserts nothing")
	}
	if card.Shape() != ShapeQuestion {
		t.Fatalf("the fixture ask is a %v, not a question: this test is not exercising the bug", card.Shape())
	}

	m, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})

	f := escFrame(t, m, cmd)
	if f.Kind != rpc.FrameDeny {
		t.Errorf("esc on a question sent %q, want %q - an interrupt does not withdraw a question", f.Kind, rpc.FrameDeny)
	}
	if f.SessionID != "s1" {
		t.Errorf("the deny is addressed to %q, want s1", f.SessionID)
	}
	if f.RequestID != card.RequestID {
		t.Errorf("the deny names request %q, want %q - the daemon matches the ask on this", f.RequestID, card.RequestID)
	}
	if f.Reason == "" {
		t.Error("the deny carries no reason, so the model is told a tool failed for nothing")
	}
	if _, ok := m.cards.For("s1"); ok {
		t.Error("the card is still open after esc denied it - it should be settled locally too, or the next report re-draws a question nobody is waiting on")
	}
}

// Dismissing a question with esc is a refusal, so the room records it the same
// way [d] does - a muted "question cancelled" close, not a stale warn line and
// never the green "question answered". esc writes the identical deny frame [d]
// writes (above), so the two refusal paths must leave the same room record;
// interrupt() forgot to, so the ask's "⚠ has a question" line went stale while
// any earlier "question answered" stayed on screen.
func TestEscDismissingAQuestionIsRecordedCancelledInTheRoom(t *testing.T) {
	a, _ := asking(t, 200)

	if card, ok := a.cards.For("s1"); !ok || card.Shape() != ShapeQuestion {
		t.Fatal("the question is not open as a question, so this test asserts nothing")
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})

	out := ansi.Strip(a.room.View(roomWidth, 40))
	if !strings.Contains(out, resolvedCancelled) {
		t.Errorf("esc-dismissing a question left no cancelled record in the room, so the ask's warn line just goes stale:\n%s", out)
	}
	if strings.Contains(out, resolvedAnswered) {
		t.Errorf("a question dismissed with esc was recorded as answered:\n%s", out)
	}
}

// A permission dismissed with esc is an interrupt, not a refusal, so it leaves
// no question-resolution line at all - the counterpart of the [d]-deny guard in
// askroom_test.go, one path over.
func TestEscOnAPermissionLeavesNoResolutionLineInTheRoom(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("john")
	a = pick(a, "s1").openDMWith("s1", "john").applyGeometry()
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind: core.KindPermissionRequest, RequestID: "r1", Ask: core.AskPermission,
		Tool: &core.ToolCall{Name: "Bash", Display: "ls"},
	}})
	if card, ok := a.cards.For("s1"); !ok || card.Shape() != ShapePermission {
		t.Fatal("the permission ask is not open as a permission, so this test asserts nothing")
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})

	out := ansi.Strip(a.room.View(roomWidth, 40))
	if strings.Contains(out, resolvedAnswered) || strings.Contains(out, resolvedCancelled) {
		t.Errorf("esc on a permission drew a question-resolution line in the room:\n%s", out)
	}
}

// A permission is the case esc must NOT change: the CLI genuinely withdraws it
// on the interrupt (interrupt-permission-findings), so esc stays an interrupt
// and the daemon's own control_cancel_request clears the card. Denying it
// instead would tell the model "the operator refused this tool" about a tool
// the operator only wanted to stop.
func TestEscOnAPermissionStillInterrupts(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("john")
	a = pick(a, "s1").openDMWith("s1", "john").applyGeometry()
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind: core.KindPermissionRequest, RequestID: "r1", Ask: core.AskPermission,
		Tool: &core.ToolCall{Name: "Bash", Display: "ls"},
	}})

	if card, ok := a.cards.For("s1"); !ok || card.Shape() != ShapePermission {
		t.Fatal("the permission ask is not open as a permission, so this test asserts nothing")
	}
	m, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})

	f := escFrame(t, m, cmd)
	if f.Kind != rpc.FrameInterrupt {
		t.Errorf("esc on a permission sent %q, want %q - the CLI withdraws a permission on the interrupt", f.Kind, rpc.FrameInterrupt)
	}
}

// escFrame runs the command esc produces and returns the one action frame that
// reached the daemon. escape batches its write with the pending history ask a
// freshly opened conversation carries, so the single-run sentFrame helper - which
// calls the command once and reads the recorder - never sees the write, and a
// history ask riding alongside it is not what esc is about.
func escFrame(t *testing.T, a App, cmd tea.Cmd) rpc.Frame {
	t.Helper()
	if cmd == nil {
		t.Fatal("esc produced no command, so nothing was sent")
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				c()
			}
		}
	}
	var acted []rpc.Frame
	for _, f := range recorderOf(t, a).taken(t) {
		if f.Kind == rpc.FrameHistory || f.Kind == rpc.FrameRoomHistory {
			continue
		}
		acted = append(acted, f)
	}
	if len(acted) != 1 {
		t.Fatalf("%d action frames reached the daemon, want exactly 1: %+v", len(acted), acted)
	}
	return acted[0]
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

	// The failure's own words: the report has to name the pipe, not just say a
	// turn was interrupted, or the whole error path could be deleted unnoticed.
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

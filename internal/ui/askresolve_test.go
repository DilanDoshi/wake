package ui

// A question the room announced resolves in place: the yellow "has a question"
// line itself becomes the purple answered record with the answers under ⎿ (or a
// muted cancelled one), rather than going stale above a second line. The
// owner's 2026-08-28 request; live-only, the entry's settled Persistence ruling.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// answered is paneAsking's question answered and submitted from its card.
func answered(t *testing.T) App {
	t.Helper()
	a := answerEvery(paneAsking(t))
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter}) // Submit on the review
	return a
}

// Mutation check: append the record again rather than replacing the ask's line
// and the yellow line is still drawn above it.
func TestAnAnsweredQuestionResolvesItsOwnRoomLineInPlace(t *testing.T) {
	out := ansi.Strip(answered(t).room.View(roomWidth, 40))
	if strings.Contains(out, cardHasQuestion) {
		t.Errorf("the ask's yellow line is still in the room after the question was answered:\n%s", out)
	}
	if n := strings.Count(out, resolvedAnswered); n != 1 {
		t.Errorf("the room records the answer %d times, want once, in the ask's own place:\n%s", n, out)
	}
}

// The answer is under the headline, one ⎿ row per question: which question
// (its header chip, else its words) and what was chosen - the review's own
// label, so the room and the card that sent it say the same thing.
func TestAnAnsweredQuestionShowsEachAnswerUnderTheHeadline(t *testing.T) {
	a := answerEvery(paneAsking(t))
	card := topCard(t, a)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	out := ansi.Strip(a.room.View(roomWidth, 40))
	for i, q := range card.Detail.Questions {
		want := questionChip(q) + resolvedArrow + card.answerLabel(i)
		if !strings.Contains(out, want) {
			t.Errorf("answer %d is not under the headline; want a row holding %q in:\n%s", i, want, out)
		}
	}
	if !strings.Contains(out, "⎿") {
		t.Errorf("the answers are not drawn under a ⎿:\n%s", out)
	}
}

// A refusal resolves the same line, muted, with nothing under it.
func TestARefusedQuestionResolvesItsOwnRoomLineInPlace(t *testing.T) {
	a := paneAsking(t)
	a, _ = press(a, cardDenyKey)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	out := ansi.Strip(a.room.View(roomWidth, 40))
	if strings.Contains(out, cardHasQuestion) {
		t.Errorf("the ask's yellow line is still in the room after the question was refused:\n%s", out)
	}
	if n := strings.Count(out, resolvedCancelled); n != 1 {
		t.Errorf("the room records the refusal %d times, want once:\n%s", n, out)
	}
	if strings.Contains(out, "⎿") {
		t.Errorf("a refused question drew answer rows:\n%s", out)
	}
}

// In place means in place: something said after the ask stays after the
// resolution, rather than the record jumping to the bottom.
func TestTheResolutionKeepsTheAsksPlaceInTheRoom(t *testing.T) {
	a := paneAsking(t).applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
		Kind: core.KindAssistantText, Text: "sydney speaks after the ask",
	}})
	a = answerEvery(a)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	out := ansi.Strip(a.room.View(roomWidth, 40))
	at, after := strings.Index(out, resolvedAnswered), strings.Index(out, "sydney speaks after the ask")
	if at < 0 || after < 0 || at > after {
		t.Errorf("the resolution did not take the ask's place (resolved at %d, later line at %d):\n%s", at, after, out)
	}
}

// An ask the room no longer holds - evicted past retention - still leaves its
// resolution: appended, the way it always was, so an answer is never silent.
func TestAResolutionWithNoAskLineLeftIsAppended(t *testing.T) {
	a := newRoomApp(t).withSize(220, 40).withAgents("john")
	a = a.recordQuestionResolved("s1", "gone", true, []string{"Format" + resolvedArrow + "JSON"})
	out := ansi.Strip(a.room.View(roomWidth, 40))
	if !strings.Contains(out, resolvedAnswered) || !strings.Contains(out, "Format"+resolvedArrow+"JSON") {
		t.Errorf("a resolution whose ask line had left the room was not recorded:\n%s", out)
	}
}

// The purple is its own style over LastRead's colour, and the refusal is not it.
func TestTheAnsweredHeadlineIsPurpleAndTheRefusalIsNot(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor, so the hues render apart
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	by := Agent{Name: "iris"}
	answered := resolvedLine(core.Event{Notice: core.NoticeQuestionAnswered}, by, roomWidth)
	if want := AnsweredStyle.Render(resolvedLead + "iris" + resolvedSep + resolvedAnswered); !strings.HasPrefix(answered, want) {
		t.Errorf("the answered headline is not drawn in AnsweredStyle:\n got %q\nwant %q", answered, want)
	}
	cancelled := resolvedLine(core.Event{Notice: core.NoticeQuestionCancelled}, by, roomWidth)
	if strings.HasPrefix(cancelled, AnsweredStyle.Render(resolvedLead)) {
		t.Errorf("a refusal was drawn in the answered purple: %q", cancelled)
	}
}

// Every row is bounded by the width, answers included: one over-wide line
// shoves both sidebars out of place.
func TestTheAnswerRowsFitTheWidth(t *testing.T) {
	long := strings.Repeat("word ", 40)
	line := resolvedLine(core.Event{Notice: core.NoticeQuestionAnswered, Text: "Q" + resolvedArrow + long}, Agent{Name: "iris"}, 30)
	for _, row := range strings.Split(line, "\n") {
		if w := ansi.StringWidth(row); w > 30 {
			t.Errorf("a resolution row is %d wide at width 30: %q", w, ansi.Strip(row))
		}
	}
}

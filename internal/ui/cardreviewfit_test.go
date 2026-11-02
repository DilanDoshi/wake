package ui

// A question card's review step is answerable wherever its questions were. The
// owner's 2026-11-01 card - two single-select questions under long texts - walked
// its questions on ↵ and went dead on the review, which drew every question whole
// and so outgrew the pane the questions fit (BUG-59).

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
)

// questionShape is an ask by its lengths: words are invented, the counts decide
// how many rows each step takes.
type questionShape []struct {
	question, header int
	labels, details  []int
}

// longQuestionShape is the owner's card by the numbers in its transcript: the
// first question 475 characters over two options whose descriptions run 274 and
// 198, the second 346 over three. The lengths are what made the review outgrow a
// pane the questions fit.
var longQuestionShape = questionShape{
	{475, 8, []int{36, 22}, []int{274, 198}},
	{346, 9, []int{27, 16, 14}, []int{88, 98, 65}},
}

// bareQuestionShape is the smallest ask there is: two one-row questions over a
// yes and a no, nothing explained - the question steps a review can least afford
// to outgrow.
var bareQuestionShape = questionShape{
	{7, 4, []int{3, 2}, []int{0, 0}},
	{7, 4, []int{3, 2}, []int{0, 0}},
}

// wideQuestionShape is two long questions over short options with nothing
// explained: a question step that is all question.
var wideQuestionShape = questionShape{
	{600, 5, []int{5, 5}, []int{0, 0}},
	{600, 5, []int{5, 5}, []int{0, 0}},
}

// longProse is n characters of ordinary words, so a wrapped row count is a real one.
func longProse(n int) string {
	words := strings.Fields("the merge agent waits for a decision on how the held replay should be handled before the next release goes out and which of the two routes the team prefers")
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(words[i%len(words)])
	}
	return b.String()[:n]
}

// shapedAsk is shape as the airlock decodes it, from the control request a
// session writes (question-answer.jsonl's shape).
func shapedAsk(t *testing.T, shape questionShape) *core.Event {
	t.Helper()
	questions := make([]any, 0, len(shape))
	for _, q := range shape {
		options := make([]any, 0, len(q.labels))
		for i, n := range q.labels {
			options = append(options, map[string]any{"label": fmt.Sprintf("option %d %s", i+1, longProse(n))[:n], "description": longProse(q.details[i])})
		}
		questions = append(questions, map[string]any{"question": longProse(q.question) + "?", "header": longProse(q.header), "options": options, "multiSelect": false})
	}
	line, err := json.Marshal(map[string]any{
		"type": "control_request", "request_id": "req-shaped",
		"request": map[string]any{"subtype": "can_use_tool", "tool_name": "AskUserQuestion", "requires_user_interaction": true, "tool_use_id": "toolu_shaped", "input": map[string]any{"questions": questions}},
	})
	if err != nil {
		t.Fatal(err)
	}
	evs, err := core.DecodeLine(line)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.Kind == core.KindPermissionRequest {
			return &ev
		}
	}
	t.Fatal("the shaped ask decoded to no permission request")
	return nil
}

// longQuestionAsk is the owner's card.
func longQuestionAsk(t *testing.T) *core.Event { return shapedAsk(t, longQuestionShape) }

// reviewShapes are the asks the review is held to: the owner's, the recorded one,
// and the two the review can least afford to outgrow.
var reviewShapes = []struct {
	name string
	ask  func(*testing.T) *core.Event
}{
	{"long", longQuestionAsk},
	{"recorded", questionAsk},
	{"bare", func(t *testing.T) *core.Event { return shapedAsk(t, bareQuestionShape) }},
	{"wide", func(t *testing.T) *core.Event { return shapedAsk(t, wideQuestionShape) }},
}

// Wherever every question of an ask is answerable the review is too, at the
// widths a conversation is read at: a pane that fit each question fits the step
// that sends them, or ↵ walks the operator into a dead end. A height at which a
// question itself is cut is skipped: that is the question's own height, drawn
// whole by design, and the pane has to grow for it either way.
func TestTheReviewIsAnswerableWhereverTheQuestionsAre(t *testing.T) {
	var checked int
	for _, shape := range reviewShapes {
		for _, width := range []int{80, 100, 140} {
			for height := 8; height <= 44; height++ {
				a := newRoomApp(t).withSize(width, height).withAgents("john", "sydney")
				a.layout.ShowRoster = true
				a = a.openDMWith("s1", "john").opened(t)
				a = a.applyFrame(rpcEvent("s1", shape.ask(t)))
				every := true
				for range questionCount(a) {
					if !a.cardFullyDrawn() {
						every = false
						break
					}
					a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
				}
				if !every {
					continue
				}
				if !topCard(t, a).OnReview() {
					t.Fatalf("%s ask, %dx%d: ↵ through every question did not reach the review", shape.name, width, height)
				}
				if !a.cardFullyDrawn() {
					t.Errorf("%s ask, %dx%d: every question was answerable and the review is not:\n%s",
						shape.name, width, height, strings.Join(pageRows(a), "\n"))
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatal("no height fit every question: the sweep asserted nothing")
	}
}

// The review of a two-question ask is never taller than the tallest of its
// question steps, at any width a card is drawn at: that is what makes the sweep
// above a rule rather than a run of luck, and it holds the review to the rows the
// questions take - a bare yes-and-no pair's seven included.
func TestTheReviewIsNoTallerThanTheTallestQuestion(t *testing.T) {
	for _, shape := range reviewShapes {
		c := newCard("s1", *shape.ask(t))
		for i := range c.Detail.Questions {
			c = c.Pick(i, 0)
		}
		for width := minBlockWidth; width <= 200; width++ {
			tallest := 0
			for step := range len(c.Detail.Questions) {
				tallest = max(tallest, lipgloss.Height(c.toStep(step).body(width)))
			}
			if review := lipgloss.Height(c.toStep(c.lastStep()).body(width)); review > tallest {
				t.Errorf("%s ask at %d columns: the review is %d rows and the tallest question %d", shape.name, width, review, tallest)
			}
		}
	}
}

// A question on the review is cut to reviewQuestionRows with an ellipsis, as an
// answer is to reviewAnswerRows: the review names what is about to be sent, and
// the question step is where a question is read whole.
func TestAReviewQuestionIsBoundedToItsRows(t *testing.T) {
	c := newCard("s1", *longQuestionAsk(t))
	c = c.Pick(0, 0).Pick(1, 0).toStep(c.lastStep())
	rows := strings.Split(stripANSI(c.reviewBody(70)), "\n")
	var question []string
	for i := paneRow(rows, reviewQuestionLead); i < len(rows) && !strings.HasPrefix(rows[i], reviewAnswerLead); i++ {
		question = append(question, rows[i])
	}
	if len(question) != reviewQuestionRows {
		t.Fatalf("a %d-character question takes %d rows on the review, want %d:\n%s", longQuestionShape[0].question, len(question), reviewQuestionRows, strings.Join(rows, "\n"))
	}
	if !strings.HasSuffix(strings.TrimRight(question[len(question)-1], " "), ellipsis) {
		t.Errorf("the cut question does not end in an ellipsis:\n%s", strings.Join(question, "\n"))
	}
}

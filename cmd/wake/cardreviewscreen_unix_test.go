//go:build unix

// A two-question ask's review step on a real screen: the pane that fit each
// question fits the step that sends them, and ↵ there answers the agent (BUG-59:
// the owner's card walked its questions on ↵ and went dead on the review).

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The owner's card by the numbers in its transcript, as internal/ui's
// longQuestionShape: the first question 475 characters over two options whose
// descriptions run 274 and 198, the second 346 over three.
var longQuestionShape = []struct {
	question, header int
	labels, details  []int
}{
	{475, 8, []int{36, 22}, []int{274, 198}},
	{346, 9, []int{27, 16, 14}, []int{88, 98, 65}},
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

// sayLongQuestions emits longQuestionShape as the control request a session
// writes for an interactive ask (sayQuestion's shape, two questions).
func sayLongQuestions(requestID string) {
	questions := make([]string, 0, len(longQuestionShape))
	for _, q := range longQuestionShape {
		options := make([]string, 0, len(q.labels))
		for i, n := range q.labels {
			options = append(options, fmt.Sprintf(`{"label":%q,"description":%q}`, fmt.Sprintf("option %d %s", i+1, longProse(n))[:n], longProse(q.details[i])))
		}
		questions = append(questions, fmt.Sprintf(`{"question":%q,"header":%q,"multiSelect":false,"options":[%s]}`, longProse(q.question)+"?", longProse(q.header), strings.Join(options, ",")))
	}
	fmt.Printf(`{"type":"control_request","request_id":%q,"request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","requires_user_interaction":true,"tool_use_id":"toolu_long","input":{"questions":[%s]}}}`+"\n",
		requestID, strings.Join(questions, ","))
}

// fakeAgentLongQuestions is fakeAgentQuestions putting the owner's two-question ask.
func fakeAgentLongQuestions(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)
	asked := 0
	for line := range agentStdin() {
		if _, ok := userTextOf(line); ok {
			asked++
			sayLongQuestions(fmt.Sprintf("q-%d", asked))
			continue
		}
		if answeredID(line) != "" {
			behavior, answers := answerOf(line)
			sayText(sid, fmt.Sprintf("%s%s with %d answers", heardPrefix, behavior, len(answers)))
			for _, label := range answers { // one row each, so a screen can find them
				sayText(sid, heardPrefix+"chose "+label)
			}
			sayResult(sid)
		}
	}
	return 0
}

// answerOf reads what a permission answer carries: its behavior, and the labels
// chosen under its updated input in question order - so a deny, an allow with
// nothing chosen, or the wrong choice is not taken for the answers ↵ sent.
func answerOf(line string) (behavior string, answers []string) {
	var f struct {
		Response struct {
			Response struct {
				Behavior     string `json:"behavior"`
				UpdatedInput struct {
					Answers map[string]string `json:"answers"`
				} `json:"updatedInput"`
			} `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(line), &f); err != nil {
		return "", nil
	}
	for _, q := range longQuestionShape {
		if label, ok := f.Response.Response.UpdatedInput.Answers[longProse(q.question)+"?"]; ok {
			answers = append(answers, label)
		}
	}
	return f.Response.Response.Behavior, answers
}

func TestEnterSubmitsTheReviewOfATwoQuestionAskInAShortPane(t *testing.T) {
	withScriptedAgent(t, scriptLongQuestions)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 24)
	s.await("ready")
	s.send("decide\r")
	s.await("[1-3] choose") // the first question, whole, with its keys
	s.send("\r")            // its first option, and on to the second
	s.await("[1-4] choose")
	s.send("\r") // the review
	s.await("Submit answers")
	if strings.Contains(s.text(), cardCutLine) {
		t.Fatalf("the review step is cut in a pane that held both questions:\n%s", s.dump())
	}

	s.send("\r")
	s.await(heardPrefix + "allow with 2 answers")
	// The first option of each question, as the cursor stood: not any two labels.
	for _, q := range longQuestionShape {
		s.await(heardPrefix + "chose " + fmt.Sprintf("option 1 %s", longProse(q.labels[0]))[:q.labels[0]])
	}
}

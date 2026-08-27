// A clarifying question, end to end: a real daemon on a real socket, a real
// client speaking rpc, and a real process on the other side reading Wake's
// answer off its own stdin.
//
// # Why this cannot be an encoder test
//
// The defect this closes produced *valid bytes*. A bare allow is well-formed
// JSON, the right envelope, the right request_id; the tool runs; the turn ends
// subtype "success" with permission_denials empty and is_error absent. Every
// assertion about the shape of what Wake writes passed while the operator's
// answer was being thrown away. The only thing that differs is what the
// process on the far end hands back to the model, so that is what is asserted
// here - by content, never by the turn succeeding.
//
// # What is replayed and what is real
//
// Real: the daemon, the socket, the client, the spawn, the process, its stdin
// and stdout pipes, core's decoder, the answer path from FrameAnswer down to
// the bytes on that process's stdin.
//
// Replayed: the agent. It is this test binary wearing a symlink, and every
// frame it emits is a line lifted from testdata/stream - the ask, and both
// branches of what came back. It never invokes a live model, per CLAUDE.md,
// and the recordings are what make the branch honest rather than invented: the
// two outcomes are `question-answer.jsonl` and `question-bare-allow.jsonl`,
// which differ in one variable and are committed side by side for this.
//
// The one thing the fake decides for itself is *which* branch, and it decides
// it the way the recording says the CLI does - on whether the allow carried
// answers at all. That is the whole mechanism under test, so it is stated in
// one function and nowhere else.
//
// Like main_test.go, and for the same narrow reason, this file may name
// Claude's JSON: something has to speak the wire to prove the daemon never
// has to.

package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// fixtureDirEnv carries testdata's absolute path into the fake, which runs
// with the spawned session's working directory rather than the test's.
const fixtureDirEnv = "WAKE_FAKE_FIXTURE_DIR"

// The recordings this replays, and the one variable between the first two.
const (
	answeredFixture = "question-answer.jsonl"
	bareFixture     = "question-bare-allow.jsonl"
	planFixture     = "question-plan-bare.jsonl"
)

// The tool names are here, in a test, and deliberately nowhere else. The fake
// has to pick a specific recorded ask out of a specific file; Wake classifies
// an ask from its wire fields and never from its name, which is what
// core.askKind and airlock_test.go's excuse for these two exist to keep true.
const (
	questionTool = "AskUserQuestion"
	planTool     = "ExitPlanMode"
)

// --- the fake ---------------------------------------------------------------

// fakeQuestion replays a recorded AskUserQuestion and answers it the way the
// two recordings say the CLI does.
//
// The branch is the finding. An allow carrying answers produced
// question-answer.jsonl's tool_result ("Your questions have been answered:
// …"); a bare allow, byte-for-byte the frame Wake used to write, produced
// question-bare-allow.jsonl's ("The user did not answer the questions."). Both
// tails are replayed whole, so what a client sees after answering is a
// recorded turn rather than a fabricated one.
func fakeQuestion() int {
	head, answeredTail, asked := splitAtAsk(answeredFixture, questionTool)
	_, bareTail, bareAsked := splitAtAsk(bareFixture, questionTool)

	// The two branches come from two recordings, so the bare one's frames name
	// their own recording's tool_use_id. Rewritten to the ask actually put, and
	// nothing else about those bytes is touched: a tool result has to name the
	// call it answers, and a test that joined on anything looser would be back
	// to matching on English.
	bareTail = retag(bareTail, bareAsked, asked)
	emitLines(head)

	for line := range stdinLines() {
		if !strings.Contains(line, `"type":"control_response"`) {
			continue
		}
		if len(answersIn(line)) == 0 {
			emitLines(bareTail)
			continue
		}
		emitLines(answeredTail)
	}
	return 0
}

// fakePlan replays a recorded ExitPlanMode ask, which carries
// requires_user_interaction exactly as a question does and whose bare allow is
// nonetheless a complete approval.
//
// It is here because this is the half Wake was already right about, and the
// half most likely to break while its neighbour is being fixed. Nothing about
// the answer is inspected: the recording approves on the allow itself, so a
// fake that looked at updatedInput would be asserting something no recording
// establishes (question-plan.jsonl approved a deliberately wrong one).
func fakePlan() int {
	head, tail, _ := splitAtAsk(planFixture, planTool)
	emitLines(head)

	for line := range stdinLines() {
		if !strings.Contains(line, `"type":"control_response"`) {
			continue
		}
		emitLines(tail)
	}
	return 0
}

// splitAtAsk cuts a recording at the control_request for one tool: the ask
// itself and the assistant frame that precedes it, then everything after.
//
// The last matching ask, because the plan recordings hold two and the plan is
// the closing one. Everything before it is dropped rather than replayed - a
// head containing an *earlier* ask would block the fake on an answer no test
// is sending.
func splitAtAsk(fixture, tool string) (head, tail []string, toolUseID string) {
	lines := readFixture(fixture)
	at := -1
	for i, line := range lines {
		if strings.Contains(line, `"subtype":"can_use_tool"`) && strings.Contains(line, `"tool_name":"`+tool+`"`) {
			at = i
		}
	}
	if at < 1 {
		fmt.Fprintf(os.Stderr, "fake: %s holds no %s ask\n", fixture, tool)
		os.Exit(1)
	}
	var f struct {
		Request struct {
			ToolUseID string `json:"tool_use_id"`
		} `json:"request"`
	}
	if err := json.Unmarshal([]byte(lines[at]), &f); err != nil || f.Request.ToolUseID == "" {
		fmt.Fprintf(os.Stderr, "fake: %s's %s ask names no tool_use_id\n", fixture, tool)
		os.Exit(1)
	}
	// The assistant tool_use block rides immediately in front of the ask -
	// the same call arriving as both shapes at once - and a client that saw
	// only the control_request would be missing the frame a renderer draws.
	return lines[at-1 : at+1], lines[at+1:], f.Request.ToolUseID
}

func retag(lines []string, from, to string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, strings.ReplaceAll(line, from, to))
	}
	return out
}

func readFixture(name string) []string {
	dir := os.Getenv(fixtureDirEnv)
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake: read fixture:", err)
		os.Exit(1)
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func emitLines(lines []string) {
	for _, line := range lines {
		fmt.Println(line)
	}
}

// answersIn reads updatedInput.answers off a control_response, which is where
// the recording puts the operator's choices and the only place they can be.
func answersIn(line string) map[string]string {
	var f struct {
		Response struct {
			Response struct {
				UpdatedInput struct {
					Answers map[string]string `json:"answers"`
				} `json:"updatedInput"`
			} `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(line), &f); err != nil {
		return nil
	}
	return f.Response.Response.UpdatedInput.Answers
}

// --- the tests --------------------------------------------------------------

// recordedChoices is the answer the recording carried, read out of the CLI's
// own echo of what it received (question-answer.jsonl's tool_use_result). A
// test sends exactly this, so the process is being handed the one answer
// anybody has watched a real `claude` accept.
func recordedChoices(t *testing.T) (asked map[string]any, answers map[string]string) {
	t.Helper()
	for _, line := range fixtureLinesFor(t, answeredFixture) {
		var f struct {
			Request *struct {
				Input map[string]any `json:"input"`
			} `json:"request"`
			ToolUseResult *struct {
				Answers map[string]string `json:"answers"`
			} `json:"tool_use_result"`
		}
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			continue
		}
		if f.Request != nil && f.Request.Input["questions"] != nil {
			asked = f.Request.Input
		}
		if f.ToolUseResult != nil && len(f.ToolUseResult.Answers) > 0 {
			answers = f.ToolUseResult.Answers
		}
	}
	if asked == nil || len(answers) == 0 {
		t.Fatalf("%s does not carry both an ask and the answers it received", answeredFixture)
	}
	return asked, answers
}

func fixtureLinesFor(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir(t), name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "testdata", "stream"))
	if err != nil {
		t.Fatalf("locate testdata: %v", err)
	}
	return dir
}

// replayingClaudeOnPath is fakeClaudeOnPath with the recordings reachable.
func replayingClaudeOnPath(t *testing.T, script string) {
	t.Helper()
	fakeClaudeOnPath(t, script)
	t.Setenv(fixtureDirEnv, fixtureDir(t))
}

// awaitAsk waits for the ask itself, which carries three things no other frame
// does: the request id to answer, the tool_use_id its result will name, and
// the classification core resolved for it.
func (c *testClient) awaitAsk(sessionID string, kind core.AskKind) core.Event {
	c.t.Helper()
	f := c.await(fmt.Sprintf("a %q ask for %s", kind, sessionID), func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == sessionID &&
			f.Event != nil && f.Event.Kind == core.KindPermissionRequest
	})
	ev := *f.Event
	if ev.Ask != kind {
		c.t.Fatalf("the ask crossed the socket as %q, want %q - the classification is what decides both "+
			"what the room draws and whether a bare allow loses anything", ev.Ask, kind)
	}
	if ev.RequestID == "" || ev.Tool == nil || ev.Tool.ID == "" {
		c.t.Fatalf("the ask carries request %q and tool %+v; both ids are needed to answer it and to find its result",
			ev.RequestID, ev.Tool)
	}
	return ev
}

// awaitAnswerTo returns what the model was told for one ask, joined on the
// tool_use_id rather than on any text.
//
// The join is the whole reason this exists. Waiting for an event *containing*
// the good answer makes a dropped answer fail as "it never arrived", which
// says nothing about what did; this fails with the sentence the model actually
// received, which is the only evidence that separates the two outcomes.
func (c *testClient) awaitAnswerTo(sessionID, toolUseID string) string {
	c.t.Helper()
	f := c.await(fmt.Sprintf("the tool result for %s", toolUseID), func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == sessionID && f.Event != nil &&
			f.Event.Kind == core.KindToolResult && f.Event.Tool != nil && f.Event.Tool.ID == toolUseID
	})
	return f.Event.Text
}

// The whole loop, and the only assertion that would have caught the defect:
// the operator's choices reach the model, named.
//
// It asserts on the labels rather than on the turn, because the turn succeeds
// either way - that is precisely what made this silent. A Wake that drops the
// answer gets question-bare-allow.jsonl's tail here, and this fails naming the
// label that never arrived.
func TestTheChoicesAnOperatorMakesReachTheModel(t *testing.T) {
	replayingClaudeOnPath(t, "question")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	ask := c.awaitAsk(idAlpha, core.AskChoice)
	if blocked := c.pollState(idAlpha, rpc.StateBlocked); soleAsk(t, blocked) != ask.RequestID {
		t.Fatalf("the roster says the session is blocked on %q, the ask says %q", soleAsk(t, blocked), ask.RequestID)
	}

	asked, answers := recordedChoices(t)
	c.send(rpc.Frame{
		Kind:         rpc.FrameAnswer,
		SessionID:    idAlpha,
		RequestID:    ask.RequestID,
		UpdatedInput: asked,
		Answers:      answers,
	})

	told := c.awaitAnswerTo(idAlpha, ask.Tool.ID)
	for _, choice := range answers {
		if !strings.Contains(told, choice) {
			t.Errorf("the model was told %q, which does not name the chosen option %q", told, choice)
		}
	}
}

// The old behaviour, still reachable and no longer silent.
//
// Both halves are asserted, and the second is the point of the task: the model
// really is told nobody answered - that is the recording, not a Wake bug - and
// the client is told so too, which it never was.
func TestApprovingAQuestionWithNoAnswerStillHappensAndIsReported(t *testing.T) {
	replayingClaudeOnPath(t, "question")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	ask := c.awaitAsk(idAlpha, core.AskChoice)
	c.send(rpc.Frame{Kind: rpc.FrameAllow, SessionID: idAlpha, RequestID: ask.RequestID})

	said := c.await("the daemon reporting the lost answer", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && f.SessionID == idAlpha && strings.Contains(f.Text, "without an answer")
	})
	if !strings.Contains(said.Text, "nobody answered") {
		t.Errorf("the report reads %q, and has to say what the agent was actually told", said.Text)
	}

	// The agent is not left stopped dead. The allow still went out, which is
	// why refusing it here would be the worse failure - and what came back is
	// the recorded consequence rather than an error.
	if told := c.awaitAnswerTo(idAlpha, ask.Tool.ID); !strings.Contains(told, "did not answer") {
		t.Errorf("the model was told %q; the recording says a bare allow reaches it as the user not answering", told)
	}

	// And the session is not marked unreachable for it.
	if s := c.pollState(idAlpha, rpc.StateIdle); s.Error != "" {
		t.Errorf("the session reports an error after a reported bare allow: %q", s.Error)
	}
}

// The neighbouring tool, unchanged. A plan ask carries
// requires_user_interaction exactly as a question does, and a bare allow is
// still its whole approval - so nothing about fixing the question may start
// pushing an updatedInput at this one, and nothing may start warning about it.
func TestABareAllowIsStillTheWholeApprovalForAPlan(t *testing.T) {
	replayingClaudeOnPath(t, "plan")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	ask := c.awaitAsk(idAlpha, core.AskApproval)
	c.send(rpc.Frame{Kind: rpc.FrameAllow, SessionID: idAlpha, RequestID: ask.RequestID})

	if told := c.awaitAnswerTo(idAlpha, ask.Tool.ID); !strings.Contains(told, "User has approved your plan.") {
		t.Errorf("the model was told %.120q, want the recorded approval", told)
	}
	for _, f := range c.seen {
		if f.Kind == rpc.FrameError && strings.Contains(f.Text, "without an answer") {
			t.Errorf("approving a plan was reported as a lost answer (%q) - a plan ask has no answer to lose, "+
				"and a warning on every approval is a warning nobody reads", f.Text)
		}
	}
}

// Only the ask actually outstanding counts as one whose answer can be lost.
//
// Unit rather than end-to-end because three of these five cases cannot be
// reached through a socket at all - a client answering an ask that is already
// settled, or naming nothing - and a warning fired on any of them would put a
// notice on the operator's screen about an answer nobody lost. The empty-id
// case is the one worth having: a can_use_tool need not carry a request_id,
// and an answer that names nothing must not match an ask that also names
// nothing.
func TestOnlyTheOutstandingAskCountsAsOneWhoseAnswerCanBeLost(t *testing.T) {
	newBlockedAgent := func(ev core.Event) *agent {
		a := newAgent(idAlpha, "sydney", "dev-5748", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
		a.observe(ev)
		return a
	}
	choice := core.Event{Kind: core.KindPermissionRequest, RequestID: "req-1", Ask: core.AskChoice}

	for _, tc := range []struct {
		what    string
		agent   *agent
		asking  string
		want    bool
		because string
	}{
		{"the ask outstanding", newBlockedAgent(choice), "req-1", true,
			"this is the whole case: a choice ask settled by a bare allow"},
		{"a different ask", newBlockedAgent(choice), "req-2", false,
			"an answer to something else says nothing about this ask"},
		{"a plan approval", newBlockedAgent(core.Event{Kind: core.KindPermissionRequest, RequestID: "req-1", Ask: core.AskApproval}), "req-1", false,
			"a bare allow is a plan's whole approval"},
		{"an ordinary permission ask", newBlockedAgent(core.Event{Kind: core.KindPermissionRequest, RequestID: "req-1"}), "req-1", false,
			"an allow is the whole answer to may-I-run-this"},
		{"an answer naming nothing, against an ask naming nothing", newBlockedAgent(core.Event{Kind: core.KindPermissionRequest, Ask: core.AskChoice}), "", false,
			"an empty id must not match an empty id"},
	} {
		t.Run(tc.what, func(t *testing.T) {
			if got := tc.agent.awaitsChoice(tc.asking); got != tc.want {
				t.Errorf("awaitsChoice(%q) = %v, want %v: %s", tc.asking, got, tc.want, tc.because)
			}
		})
	}

	// And once it is settled there is nothing left to lose.
	a := newBlockedAgent(choice)
	a.noteAnswered("req-1")
	if a.awaitsChoice("req-1") {
		t.Error("an ask that was already answered still reports an answer to lose, so a second allow would warn about nothing")
	}
}

// An answer that cannot be delivered is refused, said out loud, and leaves the
// ask exactly as it was - so the operator gets a second attempt rather than an
// agent that has already been told nobody answered.
func TestARefusedAnswerIsReportedAndLeavesTheAskAnswerable(t *testing.T) {
	replayingClaudeOnPath(t, "question")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	ask := c.awaitAsk(idAlpha, core.AskChoice)
	asked, answers := recordedChoices(t)

	// One question answered out of two, which is the shape a UI produces when
	// it forgets one - and is indistinguishable, to the model, from the drop
	// this whole change exists to close.
	partial := map[string]string{}
	for q, a := range answers {
		partial[q] = a
		break
	}
	c.send(rpc.Frame{
		Kind: rpc.FrameAnswer, SessionID: idAlpha, RequestID: ask.RequestID,
		UpdatedInput: asked, Answers: partial,
	})

	said := c.await("the daemon refusing the partial answer", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && f.SessionID == idAlpha && strings.Contains(f.Text, "nothing was chosen")
	})
	if !strings.Contains(said.Text, "nothing was written") {
		t.Errorf("the refusal reads %q, and has to say that nothing reached the agent", said.Text)
	}

	// Still blocked on the same ask, still answerable, and not reported dead:
	// a refusal is not a failed write, and reading it as one would take a
	// healthy agent off the roster at the moment somebody is looking at it.
	still := c.pollState(idAlpha, rpc.StateBlocked)
	if got := soleAsk(t, still); got != ask.RequestID {
		t.Fatalf("the ask is now %q, want it still %q - a refused answer must not settle anything", got, ask.RequestID)
	}
	if still.Error != "" {
		t.Errorf("the session reports %q after a refused answer: nothing was written, so nothing was learned about the process", still.Error)
	}

	c.send(rpc.Frame{
		Kind: rpc.FrameAnswer, SessionID: idAlpha, RequestID: ask.RequestID,
		UpdatedInput: asked, Answers: answers,
	})
	if told := c.awaitAnswerTo(idAlpha, ask.Tool.ID); !strings.Contains(told, "have been answered") {
		t.Errorf("after a second, complete answer the model was told %q", told)
	}
}

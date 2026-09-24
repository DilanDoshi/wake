package ui

// The /workflows view's agent level: drilling into one workflow agent for its
// prompt, activity and outcome, the transcript ask that feeds it, and its keys.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	bTxtAgentID = "a4d025ad4f589bce7" // count b.txt: the agent workflow-agent.jsonl is the transcript of
	sumAgentID  = "a8ca1238d00df98a5" // sum: still running in countLinesSnap
)

// bTxtEvents is count b.txt's own transcript, decoded the way the daemon
// decodes it before a FrameWorkflowAgentReply carries it.
func bTxtEvents(t *testing.T) []core.Event {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript", "workflow-agent.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var out []core.Event
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		evs, err := core.DecodeSidechainLine([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, evs...)
	}
	return out
}

func bTxtAgent() core.WorkflowAgent { return countLinesSnap().Agents[1] }

// agentOpen drills into one agent from the run level - the phase row, → onto
// the agents, down to the agent's row, ↵ - and returns what the ↵ wrote.
func agentOpen(t *testing.T, phase, row int) (App, []rpc.Frame) {
	t.Helper()
	a := runOpen(t)
	for range phase {
		a, _ = pressKey(a, wfKey(tea.KeyDown))
	}
	a, _ = pressKey(a, wfKey(tea.KeyRight))
	for range row {
		a, _ = pressKey(a, wfKey(tea.KeyDown))
	}
	a, cmd := pressKey(a, wfKey(tea.KeyEnter))
	if v := a.workflow.view; v.Level != levelAgent {
		t.Fatalf("↵ on an agent row left %+v, want the agent level", v)
	}
	return a, writtenFrames(t, a, cmd)
}

// agentAsks is every FrameWorkflowAgent Update's drain would write now.
func agentAsks(t *testing.T, a App) (App, []rpc.Frame) {
	t.Helper()
	a, cmd := a.takeHistoryAsks()
	var out []rpc.Frame
	for _, f := range writtenFrames(t, a, cmd) {
		if f.Kind == rpc.FrameWorkflowAgent {
			out = append(out, f)
		}
	}
	return a, out
}

func requireAgentAsk(t *testing.T, frames []rpc.Frame, agentID, why string) {
	t.Helper()
	var asks []rpc.Frame
	for _, f := range frames {
		if f.Kind == rpc.FrameWorkflowAgent {
			asks = append(asks, f)
		}
	}
	if len(asks) != 1 {
		t.Fatalf("%s wrote %d FrameWorkflowAgent, want exactly 1: %+v", why, len(asks), asks)
	}
	if f := asks[0]; f.SessionID != "s1" || f.Workflow == nil || f.Workflow.Agent != agentID {
		t.Errorf("%s asked for %+v (workflow %+v), want alex's agent %s", why, f, f.Workflow, agentID)
	}
}

// snapWith is countLinesSnap with one change made to the sum agent.
func snapWith(change func(*core.WorkflowAgent)) core.WorkflowSnapshot {
	s := countLinesSnap()
	change(&s.Agents[2])
	return s
}

func progressedTo(a App, s core.WorkflowSnapshot) App {
	return a.applyFrame(taskFrame("s1", workflowProgressed(wfTask, wfDispatch, s)))
}

func agentReply(a App, agentID string, events []core.Event) App {
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameWorkflowAgentReply, SessionID: "s1",
		Workflow: &rpc.WorkflowFrame{Agent: agentID}, Events: events})
}

// --- the ask --------------------------------------------------------------

func TestEnteringAnAgentAsksForItsTranscriptOnce(t *testing.T) {
	_, frames := agentOpen(t, 0, 1)
	requireAgentAsk(t, frames, bTxtAgentID, "↵ on count b.txt")
}

// Re-read on an event, never a timer: a snapshot that moved the open agent's
// tool calls, state or tokens is one more ask, and one that did not is none.
func TestAChangedSnapshotReasksOnceAndAnUnchangedOneNever(t *testing.T) {
	changes := map[string]func(*core.WorkflowAgent){
		"tool calls": func(ag *core.WorkflowAgent) { ag.ToolCalls = 1 },
		"state":      func(ag *core.WorkflowAgent) { ag.State = core.WorkflowAgentDone },
		"tokens":     func(ag *core.WorkflowAgent) { ag.Tokens = 15284 },
	}
	for name, change := range changes {
		a, _ := agentOpen(t, 1, 0)
		a, asks := agentAsks(t, progressedTo(a, countLinesSnap()))
		if len(asks) != 0 {
			t.Fatalf("%s: an unchanged snapshot asked again: %+v", name, asks)
		}
		a, asks = agentAsks(t, progressedTo(a, snapWith(change)))
		requireAgentAsk(t, asks, sumAgentID, "a snapshot that changed the sum's "+name)
		_, asks = agentAsks(t, progressedTo(a, snapWith(change)))
		if len(asks) != 0 {
			t.Errorf("%s: the same snapshot again asked again: %+v", name, asks)
		}
	}
}

// Every entry is a fresh read, even of an agent whose entry has not moved.
func TestReenteringAnAgentAsksAgain(t *testing.T) {
	a, _ := agentOpen(t, 0, 1)
	a, _ = pressKey(a, wfKey(tea.KeyEsc))
	a, cmd := pressKey(a, wfKey(tea.KeyEnter))
	requireAgentAsk(t, writtenFrames(t, a, cmd), bTxtAgentID, "↵ on count b.txt a second time")
}

// Another agent's progress is not this one's.
func TestAnotherAgentsChangeAsksNothing(t *testing.T) {
	a, _ := agentOpen(t, 1, 0)
	s := countLinesSnap()
	s.Agents[0].Tokens++
	if _, asks := agentAsks(t, progressedTo(a, s)); len(asks) != 0 {
		t.Errorf("a change to count a.txt asked for the sum's transcript: %+v", asks)
	}
}

func TestLeavingTheAgentLevelStopsAsking(t *testing.T) {
	a, _ := agentOpen(t, 1, 0)
	a, _ = pressKey(a, wfKey(tea.KeyEsc))
	if v := a.workflow.view; v.Level != levelRun || v.Column != 1 {
		t.Fatalf("esc at the agent level left %+v, want the run level's agents column", v)
	}
	changed := snapWith(func(ag *core.WorkflowAgent) { ag.ToolCalls = 1 })
	if _, asks := agentAsks(t, progressedTo(a, changed)); len(asks) != 0 {
		t.Errorf("a snapshot after leaving the agent level asked: %+v", asks)
	}
}

// A reply is kept under its session and agent, replaced by the next one.
func TestAReplyIsKeptPerAgentAndReplacedByTheNext(t *testing.T) {
	a, _ := agentOpen(t, 0, 1)
	events := bTxtEvents(t)
	a = agentReply(a, bTxtAgentID, events)
	if got := a.workflow.transcripts[transcriptKey("s1", bTxtAgentID)]; len(got) != len(events) {
		t.Fatalf("the reply kept %d events, want %d", len(got), len(events))
	}
	a = agentReply(a, bTxtAgentID, events[:1])
	if got := a.workflow.transcripts[transcriptKey("s1", bTxtAgentID)]; len(got) != 1 {
		t.Errorf("a second reply did not replace the first: %d events", len(got))
	}
	if out := stripANSI(a.View()); !strings.Contains(out, "no tool calls") || strings.Contains(out, "Activity unavailable") {
		t.Errorf("a transcript with no tool call in it does not say so:\n%s", out)
	}
}

// --- what it draws ----------------------------------------------------------

func TestTheAgentLevelDrawsStatusFiguresPromptActivityAndOutcome(t *testing.T) {
	block := renderWorkflowAgent(bTxtAgent(), bTxtEvents(t), false, 90, 30, 0)
	requireFits(t, block, 90, 30)
	lines := wfLines(block)
	for _, want := range []string{
		"count b.txt",
		"✔ Completed · haiku",
		"16.5k tok · 2 tool calls · 4s",
		"Prompt", "Count the lines in ./b.txt with wc -l. Return just the number.",
		"Activity", "⏺ Bash(wc -l /Users/dev/wf-probe/proj1b/b.txt)", "⏺ StructuredOutput",
		"Outcome", `{"n":5}`,
		"j/k scroll · ↵ expand · esc back",
	} {
		if _, ok := wfLine(lines, want); !ok {
			t.Errorf("the agent level does not draw %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	if n := headlines(lines); n != 2 {
		t.Errorf("Activity drew %d headlines, want one per tool call (2)", n)
	}
	for _, not := range []string{"$ wc -l", "Structured output provided successfully"} {
		if _, ok := wfLine(lines, not); ok {
			t.Errorf("collapsed Activity drew %q, which only ↵ shows", not)
		}
	}
}

func headlines(lines []string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "⏺") {
			n++
		}
	}
	return n
}

func TestExpandedActivityAddsEachCallsInputAndTheStartOfItsResult(t *testing.T) {
	block := renderWorkflowAgent(bTxtAgent(), bTxtEvents(t), true, 90, 40, 0)
	requireFits(t, block, 90, 40)
	lines := wfLines(block)
	for _, want := range []string{
		"$ wc -l /Users/dev/wf-probe/proj1b/b.txt",
		"5 /Users/dev/wf-probe/proj1b/b.txt",
		"Structured output provided successfully",
		"↵ collapse",
	} {
		if _, ok := wfLine(lines, want); !ok {
			t.Errorf("expanded Activity does not draw %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	if n := headlines(lines); n != 2 {
		t.Errorf("expanded Activity drew %d headlines, want 2", n)
	}
}

// Only the start of a long result, and no hint naming a key this view does not
// bind: ⌃E is the conversation's, and here every key is the view's.
func TestExpandedActivityShowsOnlyTheStartOfALongResult(t *testing.T) {
	call := &core.ToolCall{ID: "toolu_x", Name: "Read", Display: "big.txt"}
	body := strings.Repeat("a line of the file\n", 30)
	events := []core.Event{
		{Kind: core.KindToolUse, Tool: call},
		{Kind: core.KindToolResult, Text: body, Tool: &core.ToolCall{ID: "toolu_x"}},
	}
	block := stripANSI(renderWorkflowAgent(bTxtAgent(), events, true, 90, 60, 0))
	if n := strings.Count(block, "a line of the file"); n == 0 || n >= 30 {
		t.Errorf("the result drew %d of its 30 lines, want only its start:\n%s", n, block)
	}
	if strings.Contains(block, expandKey) {
		t.Errorf("the fold names %s, which the view does not bind:\n%s", expandKey, block)
	}
}

// Review Focus 5: a transcript that is missing, unreadable or not yet read
// leaves the snapshot's own previews and says the activity is unavailable.
func TestWithNoTranscriptTheAgentLevelFallsBackToThePreviews(t *testing.T) {
	lines := wfLines(renderWorkflowAgent(bTxtAgent(), nil, false, 90, 30, 0))
	for _, want := range []string{
		"Prompt", "Count the lines in ./b.txt with wc -l. Return just the number.",
		"Activity unavailable",
		"Outcome", `{"n":5}`,
	} {
		if _, ok := wfLine(lines, want); !ok {
			t.Errorf("with no transcript the agent level does not draw %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
}

func TestARunningAgentReadsRunningAndHasNoOutcomeYet(t *testing.T) {
	lines := wfLines(renderWorkflowAgent(countLinesSnap().Agents[2], nil, false, 90, 30, 0))
	if _, ok := wfLine(lines, "⏺ Running · haiku"); !ok {
		t.Errorf("a running agent does not say so:\n%s", strings.Join(lines, "\n"))
	}
	if _, ok := wfLine(lines, "0 tool calls"); !ok {
		t.Errorf("a running agent's figures are not drawn:\n%s", strings.Join(lines, "\n"))
	}
	if _, ok := wfLine(lines, "Outcome"); ok {
		t.Errorf("an agent with no result draws an Outcome:\n%s", strings.Join(lines, "\n"))
	}
}

// Review Focus 5, through the App: an empty reply is no notice and no re-ask.
func TestAnEmptyReplyIsNoNoticeAndNoReask(t *testing.T) {
	a, _ := agentOpen(t, 1, 0)
	if out := stripANSI(a.View()); !strings.Contains(out, "Add 3 and 5. Return just the sum.") ||
		!strings.Contains(out, "Activity unavailable") {
		t.Fatalf("before any reply the agent level does not fall back to the previews:\n%s", out)
	}
	notice.Reset()
	a = agentReply(a, sumAgentID, nil)
	if n, ok := notice.Latest(); ok {
		t.Errorf("an empty transcript reported %q", n.String())
	}
	a, asks := agentAsks(t, a)
	if len(asks) != 0 {
		t.Errorf("an empty reply asked again: %+v", asks)
	}
	if out := stripANSI(a.View()); !strings.Contains(out, "Activity unavailable") {
		t.Errorf("after an empty reply the agent level does not say the activity is unavailable:\n%s", out)
	}
}

// The block is exactly the rows it was given at any scroll, and a scroll past
// either end draws what the nearest end draws.
func TestTheAgentLevelFitsEveryHeightAndClampsItsScroll(t *testing.T) {
	events := bTxtEvents(t)
	for _, expanded := range []bool{false, true} {
		for _, w := range []int{1, 12, 40, 90} {
			for _, h := range []int{1, 2, 4, 6, 9, 40} {
				for _, scroll := range []int{-3, 0, 2, 1000} {
					requireFits(t, renderWorkflowAgent(bTxtAgent(), events, expanded, w, h, scroll), w, h)
					if t.Failed() {
						t.Fatalf("expanded=%v %dx%d scroll %d", expanded, w, h, scroll)
					}
				}
			}
		}
	}
	top := renderWorkflowAgent(bTxtAgent(), events, false, 60, 8, 0)
	if got := renderWorkflowAgent(bTxtAgent(), events, false, 60, 8, -3); got != top {
		t.Errorf("a scroll above the top drew something other than the top")
	}
	end := renderWorkflowAgent(bTxtAgent(), events, false, 60, 8, 1000)
	if !strings.Contains(stripANSI(end), `{"n":5}`) {
		t.Errorf("scrolled to the end, the outcome is not drawn:\n%s", stripANSI(end))
	}
}

// --- the keys -------------------------------------------------------------

func TestEnterTogglesExpandedAndRightExpands(t *testing.T) {
	a, _ := agentOpen(t, 0, 1)
	a = agentReply(a, bTxtAgentID, bTxtEvents(t))
	a, _ = pressKey(a, wfKey(tea.KeyEnter))
	if !a.workflow.view.Expanded || !strings.Contains(stripANSI(a.View()), "$ wc -l") {
		t.Fatalf("↵ did not expand Activity:\n%s", stripANSI(a.View()))
	}
	a, _ = pressKey(a, wfKey(tea.KeyEnter))
	if a.workflow.view.Expanded {
		t.Error("a second ↵ did not collapse Activity")
	}
	a, _ = pressKey(a, wfKey(tea.KeyRight))
	a, _ = pressKey(a, wfKey(tea.KeyRight))
	if !a.workflow.view.Expanded {
		t.Error("→ did not expand Activity")
	}
}

func TestLeftBacksOutToTheRunLevelOnTheSameAgent(t *testing.T) {
	a, _ := agentOpen(t, 0, 1)
	a, _ = pressKey(a, wfKey(tea.KeyLeft))
	if v := a.workflow.view; v.Level != levelRun || v.Column != 1 || v.Agent != 1 || v.Expanded || v.Scroll != 0 {
		t.Errorf("← at the agent level left %+v, want the run level on count b.txt", v)
	}
}

// j and k move the detail and stop at either end; the wheel is the same move.
func TestJAndKScrollWithinBounds(t *testing.T) {
	a, _ := agentOpen(t, 0, 1)
	a = agentReply(a, bTxtAgentID, bTxtEvents(t)).withSize(160, 14)
	a, _ = pressKey(a, wfRune('k'))
	if s := a.workflow.view.Scroll; s != 0 {
		t.Fatalf("k at the top scrolled to %d", s)
	}
	a, _ = pressKey(a, wfRune('j'))
	if s := a.workflow.view.Scroll; s != 1 {
		t.Fatalf("j scrolled to %d, want 1 - the detail must overflow a 14-row terminal", s)
	}
	for range 50 {
		a, _ = pressKey(a, wfRune('j'))
	}
	end := a.workflow.view.Scroll
	if a, _ = pressKey(a, wfRune('j')); a.workflow.view.Scroll != end {
		t.Errorf("j past the end moved the scroll from %d to %d", end, a.workflow.view.Scroll)
	}
	if out := stripANSI(a.View()); !strings.Contains(out, `{"n":5}`) {
		t.Errorf("scrolled to the end, the outcome is not on screen:\n%s", out)
	}
	a, _ = pressKey(a, wfRune('k'))
	if a.workflow.view.Scroll != end-1 {
		t.Errorf("k from the end left %d, want %d", a.workflow.view.Scroll, end-1)
	}
	x, y, ok := viewCell(a, "count b.txt") // the head, which never scrolls
	if !ok {
		t.Fatalf("the agent level's head is not drawn:\n%s", stripANSI(a.View()))
	}
	for range 50 {
		a = wheel(a, true, x, y)
	}
	if a.workflow.view.Scroll != 0 {
		t.Errorf("the wheel up left the scroll on %d, want the top", a.workflow.view.Scroll)
	}
}

// A press over the agent level moves nothing under it: the run level's cursors
// are what esc goes back to. The figures row is where the run level's geometry
// would find its first agent row.
func TestAPressOnTheAgentLevelLeavesTheRunLevelsCursors(t *testing.T) {
	a, _ := agentOpen(t, 0, 1)
	before := a.workflow.view
	x, y, ok := viewCell(a, "tool calls")
	if !ok {
		t.Fatalf("the agent level is not drawn:\n%s", stripANSI(a.View()))
	}
	a, _ = a.mouse(pressAt(x, y))
	if v := a.workflow.view; v.Level != levelAgent || v.Cursor != before.Cursor || v.Column != before.Column || v.Agent != before.Agent {
		t.Errorf("a press on the agent level left %+v, want %+v", v, before)
	}
}

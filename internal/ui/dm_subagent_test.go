package ui

// Where a subagent's work is drawn, which is the whole of the reported bug: it
// used to be every frame of it, inline, in the conversation the operator
// opened.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	subDispatch = "toolu_dispatch"
	subSaid     = "SUBAGENT COUNTED TWELVE LINES"
	parentSaid  = "PARENT IS STILL TALKING"
)

// spoke is one frame a subagent forwarded.
func spoke(dispatch, text string) core.Event {
	return core.Event{
		Kind: core.KindAssistantText,
		Text: text,
		Subagent: &core.Subagent{
			Dispatch: dispatch, Type: "general-purpose", Task: "Count lines",
		},
	}
}

func spokePlainly(text string) core.Event {
	return core.Event{Kind: core.KindAssistantText, Text: text}
}

func conversation(t *testing.T) DM {
	t.Helper()
	return NewDM("s1", "alex").SetSize(80, 24)
}

// The bug, stated as a test. 55-77% of the message frames in a dispatching
// turn are the subagent's, and every one of them landed here.
func TestASubagentsWorkDoesNotReachTheConversation(t *testing.T) {
	d := conversation(t).
		Append(spokePlainly(parentSaid)).
		Append(spoke(subDispatch, subSaid)).
		Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{Name: "Grep", Display: "func main"},
			Subagent: &core.Subagent{Dispatch: subDispatch, Task: "Count lines"}})

	assertShows(t, d, 80, 24, parentSaid)
	assertHides(t, d, 80, 24, subSaid)
	assertHides(t, d, 80, 24, "Grep")
}

// A receipt is the agent reporting *about* a subagent rather than the subagent
// speaking, so it stays where the dispatch was made. Without this the parent's
// account of its own tool call would follow the subagent into a pane the
// operator has not opened.
func TestADispatchReceiptStaysInTheConversation(t *testing.T) {
	d := conversation(t).Append(core.Event{
		Kind: core.KindToolResult,
		Text: "the report",
		Subagent: &core.Subagent{
			Dispatch: subDispatch, Agent: "a1", Result: core.SubagentFinished,
		},
	})

	if out := visible(d, 80, 24); !strings.Contains(out, "finished") {
		t.Errorf("the receipt is not in the conversation:\n%s", out)
	}
}

// A subagent's permission ask carries an agent id and **no dispatch** - the
// envelope names none - so there is no transcript to file it under. It has to
// stay where asks are answered: a blocked agent reachable only by drilling into
// it is a blocked agent nobody unblocks.
func TestASubagentsAskStaysWhereItCanBeAnswered(t *testing.T) {
	d := conversation(t).Append(core.Event{
		Kind:     core.KindPermissionRequest,
		Tool:     &core.ToolCall{Name: "Write", Display: "/tmp/tally.txt"},
		Subagent: &core.Subagent{Agent: "ab1b72d53680ae187"},
	})

	assertShows(t, d, 80, 24, "permission request")
}

// The other half of the fix: the work is not lost, it is somewhere. Opening a
// dispatch swaps the pane onto that subagent's own transcript.
func TestOpeningADispatchDrawsItsOwnTranscript(t *testing.T) {
	d := conversation(t).
		Append(spokePlainly(parentSaid)).
		Append(spoke(subDispatch, subSaid))

	open := d.Viewing(subDispatch)
	assertShows(t, open, 80, 24, subSaid)
	assertHides(t, open, 80, 24, parentSaid)
}

// And back, which is ↵ on the main row. The conversation is exactly as it was.
func TestLeavingADispatchReturnsTheConversation(t *testing.T) {
	d := conversation(t).
		Append(spokePlainly(parentSaid)).
		Append(spoke(subDispatch, subSaid))

	back := d.Viewing(subDispatch).Viewing("")
	assertShows(t, back, 80, 24, parentSaid)
	assertHides(t, back, 80, 24, subSaid)
}

// The conversation keeps arriving while somebody is reading a subagent, and it
// is all there on the way back. Nothing is dropped for being off screen.
func TestTheConversationKeepsArrivingWhileADispatchIsRead(t *testing.T) {
	d := conversation(t).
		Append(spoke(subDispatch, subSaid)).
		Viewing(subDispatch).
		Append(spokePlainly(parentSaid))

	assertHides(t, d, 80, 24, parentSaid)
	assertShows(t, d.Viewing(""), 80, 24, parentSaid)
}

// And the reverse: a frame arriving for the dispatch being read is drawn as it
// arrives, which is what makes this a live view rather than a snapshot.
func TestAFrameArrivingForTheOpenDispatchIsDrawn(t *testing.T) {
	d := conversation(t).
		Append(spoke(subDispatch, "first")).
		Viewing(subDispatch).
		Append(spoke(subDispatch, "second"))

	assertShows(t, d, 80, 24, "first")
	assertShows(t, d, 80, 24, "second")
}

// Two concurrent subagents are two transcripts, not one interleaved stream.
// subagent-parallel.jsonl interleaves two of them line by line.
func TestTwoDispatchesAreTwoTranscripts(t *testing.T) {
	d := conversation(t).
		Append(spoke("toolu_a", "ALPHA WORK")).
		Append(spoke("toolu_b", "BETA WORK")).
		Append(spoke("toolu_a", "ALPHA AGAIN"))

	alpha := d.Viewing("toolu_a")
	assertShows(t, alpha, 80, 24, "ALPHA WORK")
	assertShows(t, alpha, 80, 24, "ALPHA AGAIN")
	assertHides(t, alpha, 80, 24, "BETA WORK")

	beta := d.Viewing("toolu_b")
	assertShows(t, beta, 80, 24, "BETA WORK")
	assertHides(t, beta, 80, 24, "ALPHA WORK")
}

// Opening a dispatch nothing forwarded draws an empty transcript rather than
// the conversation: silently staying put would look like the key did nothing.
// Task.Openable is what keeps this off the keys, and this is the floor under it.
func TestOpeningADispatchWithNothingInItIsEmptyRatherThanTheConversation(t *testing.T) {
	d := conversation(t).Append(spokePlainly(parentSaid)).Viewing("toolu_nothing")

	assertHides(t, d, 80, 24, parentSaid)
}

// A width change re-wraps whatever is on screen. The subagent's transcript is
// rendered from its own events, so it has to survive one - this is the path
// where a second event store most easily goes stale.
func TestASubagentsTranscriptSurvivesAReWrap(t *testing.T) {
	d := conversation(t).
		Append(spoke(subDispatch, subSaid)).
		Viewing(subDispatch).
		SetSize(120, 24)

	assertShows(t, d, 120, 24, subSaid)
}

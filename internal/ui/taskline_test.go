package ui

// The line a dispatch leaves in the conversation when it ends.

import (
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// finished is the ending frame that names its dispatch and carries the final
// usage - task_notification, the one of the two endings a line can be built
// from. See taskline.go.
func finished(dispatch, label string, kind core.TaskKind, status core.TaskStatus, elapsed time.Duration) core.Event {
	return core.Event{Kind: core.KindSystem, Text: "task_notification", Task: &core.TaskUpdate{
		ID: "a1", Dispatch: dispatch, Kind: kind, Phase: core.TaskEnded,
		Status: status, Label: label, Elapsed: elapsed,
	}}
}

// ingested folds events the way App.observe does: through the Fleet, which owns
// the dispatch fold, then into the conversation with the ending named from the
// row the fold now holds. Naming an ending needs the fold to have already seen
// the frame, which is what makes this the real seam rather than a convenience -
// a test that appended straight to the DM would name no ending.
func ingested(d DM, evs ...core.Event) DM {
	f := NewFleet()
	for _, ev := range evs {
		f, _ = f.Observe(ev, "s1")
		d = d.Append(f.named("s1", ev))
	}
	return d
}

// conversationRegion is the conversation itself, without the task board or the
// composer under it. The board sits between the two, so a test about what the
// *transcript* holds has to cut it off or it asserts nothing.
func conversationRegion(t *testing.T, d DM, w, h int) string {
	t.Helper()
	lines := strings.Split(visible(d, w, h), "\n")
	box := -1
	for i, l := range lines {
		if strings.Contains(l, "╭") {
			box = i
			break
		}
	}
	if box < 0 {
		t.Fatalf("no composer box on screen, so the transcript cannot be bounded:\n%s", visible(d, w, h))
	}
	end := box - d.checklistRows()
	if end < 0 {
		t.Fatalf("the board does not fit above the composer: box at %d, %d rows", box, d.checklistRows())
	}
	return strings.Join(lines[:end], "\n")
}

// The line itself, which is what the screenshot asked for.
func TestADispatchEndingLeavesALineInTheConversation(t *testing.T) {
	d := conversation(t).Append(
		finished("toolu_1", "Counting lines in alpha.txt", core.TaskAgent, core.TaskDone, 24*time.Second))

	out := visible(d, 80, 24)
	for _, want := range []string{`Subagent "Counting lines in alpha.txt"`, "finished", "24s"} {
		if !strings.Contains(out, want) {
			t.Errorf("the line is missing %q:\n%s", want, out)
		}
	}
}

// It is drawn from the ending that names its dispatch, and only that one. Both
// task_updated and task_notification report the same ending, so a line built
// from "the phase is ended" draws two of them one row apart.
func TestOnlyOneLineIsDrawnForOneEnding(t *testing.T) {
	// The ending that carries no dispatch - task_updated's whole payload is a
	// task id and a patch.
	bare := core.Event{Kind: core.KindSystem, Text: "task_updated", Task: &core.TaskUpdate{
		ID: "a1", Phase: core.TaskEnded, Status: core.TaskDone, Kind: core.TaskAgent,
	}}
	d := conversation(t).
		Append(bare).
		Append(finished("toolu_1", "Counting lines", core.TaskAgent, core.TaskDone, 24*time.Second))

	if n := strings.Count(visible(d, 80, 24), "finished"); n != 1 {
		t.Errorf("%d lines say finished, want 1:\n%s", n, visible(d, 80, 24))
	}
}

// A halted dispatch did not finish, and the line has to say which. The whole
// point of TaskStopped is that a row must never report a killed subagent as a
// completed one.
func TestAHaltedDispatchDoesNotSayItFinished(t *testing.T) {
	d := conversation(t).Append(
		finished("toolu_1", "Counting lines", core.TaskAgent, core.TaskStopped, 3*time.Second))

	out := visible(d, 80, 24)
	if strings.Contains(out, "finished") {
		t.Errorf("a halted dispatch reports as finished:\n%s", out)
	}
	if !strings.Contains(out, "halted") {
		t.Errorf("the line does not say what happened:\n%s", out)
	}
}

// An ending this build cannot classify says so rather than claiming either
// outcome. The binary names statuses the corpus has never recorded - failed,
// paused - so this is expected traffic rather than a corner.
func TestAnUnrecordedEndingClaimsNeitherOutcome(t *testing.T) {
	d := conversation(t).Append(
		finished("toolu_1", "Counting lines", core.TaskAgent, core.TaskStatusUnknown, time.Second))

	out := visible(d, 80, 24)
	if strings.Contains(out, "finished") || strings.Contains(out, "halted") {
		t.Errorf("an unmodelled ending was reported as one of the two known ones:\n%s", out)
	}
	if !strings.Contains(out, "ended") {
		t.Errorf("the line says nothing at all about the ending:\n%s", out)
	}
}

// A background shell is not an agent, and the line must not call it one - the
// same distinction TaskKind exists for.
func TestAShellEndingIsNotCalledAnAgent(t *testing.T) {
	d := conversation(t).Append(
		finished("toolu_1", "waiting for the sentinel", core.TaskShell, core.TaskStopped, time.Second))

	out := conversationRegion(t, d, 80, 24)
	if strings.Contains(out, taskLineAgent) {
		t.Errorf("a background shell is drawn as a subagent:\n%s", out)
	}
	if !strings.Contains(out, taskLineShell) {
		t.Errorf("the line does not say it was a shell:\n%s", out)
	}
	if !strings.Contains(out, "waiting for the sentinel") {
		t.Errorf("the shell's own line is missing from the transcript:\n%s", out)
	}
}

// Starting and working leave nothing: the ⏺ Agent(…) tool call the parent
// already draws is the start marker, and a second line one row under it is the
// same fact twice.
func TestStartingAndWorkingLeaveNoLine(t *testing.T) {
	d := conversation(t).
		Append(started("a1", "toolu_1", "Counting lines", "general-purpose", core.TaskAgent)).
		Append(progressed("a1", "toolu_1", "Reading alpha.txt", "Read", 100, time.Second))

	if out := conversationRegion(t, d, 80, 24); strings.Contains(out, "Counting lines") || strings.Contains(out, "Reading alpha.txt") {
		t.Errorf("a lifecycle frame that is not an ending drew a line:\n%s", out)
	}
}

// An ending with no duration reported says nothing about how long it took,
// rather than claiming zero.
func TestAnEndingWithNoDurationClaimsNoTime(t *testing.T) {
	d := conversation(t).Append(
		finished("toolu_1", "Counting lines", core.TaskAgent, core.TaskDone, 0))

	out := visible(d, 80, 24)
	if !strings.Contains(out, "finished") {
		t.Fatalf("the line is missing entirely:\n%s", out)
	}
	if strings.Contains(out, "0s") {
		t.Errorf("the line claims a duration nothing reported:\n%s", out)
	}
}

// The line belongs to the conversation, not to the subagent: it is the parent
// reporting that something it dispatched is over. Drawn inside the subagent's
// own transcript it would be the one line there that the subagent did not say.
func TestTheLineStaysInTheConversation(t *testing.T) {
	d := conversation(t).
		Append(spoke("toolu_1", subSaid)).
		Append(finished("toolu_1", "Counting lines", core.TaskAgent, core.TaskDone, 24*time.Second))

	assertShows(t, d, 80, 24, "finished")
	assertHides(t, d.Viewing("toolu_1"), 80, 24, "finished")
}

// A width change re-derives every block from its event, so the line has to be
// a function of the event alone. This is why it is keyed on the ending that
// names a dispatch rather than on a transition the fold would have to
// remember: a block built from state cannot survive a re-wrap.
func TestTheLineSurvivesAReWrap(t *testing.T) {
	d := conversation(t).
		Append(finished("toolu_1", "Counting lines", core.TaskAgent, core.TaskDone, 24*time.Second)).
		SetSize(120, 24)

	if n := strings.Count(visible(d, 120, 24), "finished"); n != 1 {
		t.Errorf("%d lines say finished after a re-wrap, want 1:\n%s", n, visible(d, 120, 24))
	}
}

// The receipt keeps its own line. Two lines about one dispatch is the owner's
// call, recorded here so it cannot be quietly undone: the lifecycle line is
// the event, and the receipt's is the pointer into the transcript.
func TestTheReceiptKeepsItsOwnLine(t *testing.T) {
	d := conversation(t).
		Append(finished("toolu_1", "Counting lines", core.TaskAgent, core.TaskDone, 24*time.Second)).
		Append(core.Event{
			Kind: core.KindToolResult,
			Text: "the report",
			Subagent: &core.Subagent{
				Dispatch: "toolu_1", Agent: "a1", Result: core.SubagentFinished,
			},
		})

	if out := visible(d, 80, 24); !strings.Contains(out, subagentFinishedNote) {
		t.Errorf("the receipt's own line is gone:\n%s", out)
	}
}

// The ending frame does not say what ended. task_notification's keys are the
// id, the dispatch, the status, the output file, the summary and the usage -
// no description and no task_type - so a line built from that frame alone
// reads "subagent finished" for every dispatch a fleet ever runs. The name and
// the kind come from the row task_started established.
func TestTheEndingLineNamesWhatEndedThoughItsFrameDoesNot(t *testing.T) {
	bare := core.Event{Kind: core.KindSystem, Text: "task_notification", Task: &core.TaskUpdate{
		ID: "a1", Dispatch: "toolu_1", Phase: core.TaskEnded, Status: core.TaskDone,
		Kind: core.TaskKindUnknown, Elapsed: 5 * time.Second,
	}}
	d := ingested(conversation(t),
		started("a1", "toolu_1", "Counting lines in alpha.txt", "general-purpose", core.TaskAgent),
		bare)

	out := conversationRegion(t, d, 80, 24)
	if !strings.Contains(out, `Subagent "Counting lines in alpha.txt"`) {
		t.Errorf("the line does not name what ended:\n%s", out)
	}
}

// And the name survives a re-wrap, which is the half the enrichment exists for:
// it is filled at ingest onto the stored event, so re-deriving the block at a
// new width reads the same event rather than consulting a fold that has moved
// on.
func TestTheNameOnTheEndingLineSurvivesAReWrap(t *testing.T) {
	bare := core.Event{Kind: core.KindSystem, Text: "task_notification", Task: &core.TaskUpdate{
		ID: "a1", Dispatch: "toolu_1", Phase: core.TaskEnded, Status: core.TaskDone,
		Kind: core.TaskKindUnknown, Elapsed: 5 * time.Second,
	}}
	d := ingested(conversation(t),
		started("a1", "toolu_1", "Counting lines in alpha.txt", "general-purpose", core.TaskAgent),
		bare).
		SetSize(120, 24)

	if out := conversationRegion(t, d, 120, 24); !strings.Contains(out, `Subagent "Counting lines in alpha.txt"`) {
		t.Errorf("the name is gone after a re-wrap:\n%s", out)
	}
}

// Enriching must not edit the event it was handed: core.Event.Task is a
// pointer, and the daemon's frame is read by more than this conversation.
func TestEnrichingDoesNotEditTheEventItWasGiven(t *testing.T) {
	ev := core.Event{Kind: core.KindSystem, Text: "task_notification", Task: &core.TaskUpdate{
		ID: "a1", Dispatch: "toolu_1", Phase: core.TaskEnded, Status: core.TaskDone,
		Kind: core.TaskKindUnknown,
	}}
	ingested(conversation(t),
		started("a1", "toolu_1", "Counting lines", "general-purpose", core.TaskAgent),
		ev)

	if ev.Task.Label != "" || ev.Task.Kind != core.TaskKindUnknown {
		t.Errorf("the caller's event was edited: %+v", ev.Task)
	}
}

// What a dispatch was *asked to do*, not the last thing it was seen doing.
//
// The row's label is a live status and is meant to move - task_progress
// rewrites it, which is what makes the row a status line. The ending line is
// not a status: it names the work that is now over. Measured over the corpus,
// **9 of the 9 dispatches carrying both** have a final progress description
// that differs from their dispatch description, so reading the row's label here
// misnames every one of them - `Subagent "Reading beta.txt" finished` for a
// dispatch that was asked to count lines in two files.
func TestTheEndingLineNamesTheDispatchNotTheLastThingItWasDoing(t *testing.T) {
	bare := core.Event{Kind: core.KindSystem, Text: "task_notification", Task: &core.TaskUpdate{
		ID: "a1", Dispatch: "toolu_1", Phase: core.TaskEnded, Status: core.TaskDone,
		Kind: core.TaskKindUnknown, Elapsed: 5 * time.Second,
	}}
	d := ingested(conversation(t),
		started("a1", "toolu_1", "Count lines in alpha.txt and beta.txt", "general-purpose", core.TaskAgent),
		progressed("a1", "toolu_1", "Reading beta.txt", "Read", 100, 4*time.Second),
		bare)

	out := conversationRegion(t, d, 90, 24)
	if !strings.Contains(out, `Subagent "Count lines in alpha.txt and beta.txt"`) {
		t.Errorf("the ending line does not name the dispatch:\n%s", out)
	}
	if strings.Contains(out, "Reading beta.txt") {
		t.Errorf("the ending line names the last progress status instead of the dispatch:\n%s", out)
	}
}

// Enrichment resolves the row by task id, which is what both the ending frames
// and the rows are keyed on. Matching on the dispatch id instead returns the
// first row carrying it, so an ending would inherit another task's name if two
// ever shared one.
func TestEnrichmentResolvesTheRowByTaskID(t *testing.T) {
	shared := "toolu_shared"
	d := ingested(conversation(t),
		started("a1", shared, "the first dispatch", "general-purpose", core.TaskAgent),
		started("a2", shared, "the second dispatch", "general-purpose", core.TaskAgent),
		core.Event{Kind: core.KindSystem, Text: "task_notification", Task: &core.TaskUpdate{
			ID: "a2", Dispatch: shared, Phase: core.TaskEnded, Status: core.TaskDone,
			Kind: core.TaskKindUnknown, Elapsed: time.Second,
		}})

	out := conversationRegion(t, d, 90, 24)
	if !strings.Contains(out, "the second dispatch") {
		t.Errorf("the ending was named after another task:\n%s", out)
	}
}

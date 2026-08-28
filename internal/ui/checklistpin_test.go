package ui

// The task board pinned above the composer, and the checklist ops that feed it
// without ever drawing in the transcript.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// The board draws the list above the composer, and the transcript holds none of
// it: a TaskCreate is the board, not a block.
func TestTheChecklistBoardPinsAboveTheComposer(t *testing.T) {
	fresh(t)
	d := NewDM("s1", "alex").SetSize(80, 40).
		Append(taskCreateID("c1", "explore the code", "Exploring")).
		Append(taskUpdateID("u1", "1", core.TodoActive))

	if board := stripANSI(d.checklistPin(80)); !strings.Contains(board, "explore the code") {
		t.Errorf("the board did not pin the list:\n%s", board)
	}
	if view := stripANSI(d.View(80, 40)); !strings.Contains(view, "explore the code") {
		t.Errorf("View did not draw the board above the composer:\n%s", view)
	}
	if region := stripANSI(conversationRegion(t, d, 80, 40)); strings.Contains(region, "explore the code") {
		t.Errorf("the checklist drew in the transcript instead of the board:\n%s", region)
	}
}

// A checklist op and its result draw nothing in the transcript, and the prose
// around them is untouched - the op is folded into the board, not a block.
func TestAChecklistOpAndItsResultVanishFromTheTranscript(t *testing.T) {
	fresh(t)
	d := NewDM("s1", "alex").SetSize(80, 40).
		Append(prose("starting the work")).
		Append(taskCreateID("c1", "explore the code", "Exploring")).
		Append(result("c1", "Task 1 created", false))

	region := stripANSI(conversationRegion(t, d, 80, 40))
	for _, gone := range []string{"TaskCreate", "explore the code", "Task 1 created"} {
		if strings.Contains(region, gone) {
			t.Errorf("a checklist op or its result drew in the transcript: found %q\n%s", gone, region)
		}
	}
	if !strings.Contains(region, "starting the work") {
		t.Errorf("the prose around the op was dropped:\n%s", region)
	}
}

// The board's rows are chrome, counted in chromeHeight - and re-settled when a
// checklist op moves them, so DM.chrome stays current off the draw path. A stale
// memo makes View re-size the pane on a throwaway copy every frame for the life
// of any conversation that has a board. This guards the surface that replaced
// the dispatch list against the same per-frame re-size.
func TestAConversationWithAChecklistDoesNotReSizeOnEveryDraw(t *testing.T) {
	fresh(t)
	d := NewDM("s1", "alex").SetSize(80, 40).
		Append(taskCreateID("c1", "explore the code", "Exploring"))

	if d.checklistRows() == 0 {
		t.Fatal("no board rows, so this test is measuring nothing")
	}
	if got, want := d.chromeHeight(), d.chrome; got != want {
		t.Errorf("chromeHeight = %d but the sized chrome is %d, so View re-sizes on every frame", got, want)
	}
}

// A board restored off disk is chrome too: Before rebuilds d.checklist from the
// history, and the viewport has to be sized for the rows it grew - or the frame
// is taller than the pane and scrolls the alt screen away. The old checklist was
// drawn in the transcript, so restoring it never touched the chrome; the board
// does, so Before must re-settle.
func TestARestoredChecklistSettlesTheChrome(t *testing.T) {
	fresh(t)
	earlier := []core.Event{
		taskCreateID("c1", "explore the code", "Exploring"),
		taskCreateID("c2", "write the patch", "Writing"),
	}
	d := NewDM("s1", "alex").SetSize(80, 40).Before(earlier)

	if d.checklistRows() == 0 {
		t.Fatal("the restored board has no rows, so this test measures nothing")
	}
	if got, want := d.chromeHeight(), d.chrome; got != want {
		t.Errorf("chromeHeight = %d but the sized chrome is %d after a restore: the board's rows are unbudgeted, so the frame is too tall", got, want)
	}
	if board := stripANSI(d.checklistPin(80)); !strings.Contains(board, "explore the code") {
		t.Errorf("the restored board is empty:\n%s", board)
	}
}

// The proof a checklist op is *stored*, not merely absent from the transcript.
// Before resets d.checklist and re-derives it from d.events over the whole
// restored-plus-live sequence, so a live op that Append dropped rather than
// stored invisibly would vanish from the board the moment history is prepended -
// a passing "it's not in the transcript" test cannot tell the two apart.
func TestALiveChecklistOpIsStoredAndSurvivesARestore(t *testing.T) {
	fresh(t)
	d := NewDM("s1", "alex").SetSize(80, 40).
		Append(taskCreateID("c1", "live item", "Liveing"))
	if board := stripANSI(d.checklistPin(80)); !strings.Contains(board, "live item") {
		t.Fatalf("the live op is not on the board to begin with:\n%s", board)
	}

	d = d.Before([]core.Event{prose("an earlier turn")})

	if board := stripANSI(d.checklistPin(80)); !strings.Contains(board, "live item") {
		t.Errorf("the live checklist op was dropped from d.events, so the restore re-fold lost it:\n%s", board)
	}
}

// A deleted item shrinks the board, and the chrome shrinks with it: the same
// re-settle has to fire when the row count falls, not only when it rises.
func TestDeletingAChecklistItemShrinksTheBoardAndItsChrome(t *testing.T) {
	fresh(t)
	d := NewDM("s1", "alex").SetSize(80, 40).
		Append(taskCreateID("c1", "one", "Oneing")).
		Append(taskCreateID("c2", "two", "Twoing"))
	full := d.checklistRows()

	d = d.Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
		ID: "u1", Name: "TaskUpdate", Checklist: &core.ChecklistOp{Update: true, ID: "2", Deleted: true},
	}})

	if got := d.checklistRows(); got >= full {
		t.Errorf("the board kept %d rows after a delete, was %d - it did not shrink", got, full)
	}
	if got, want := d.chromeHeight(), d.chrome; got != want {
		t.Errorf("chromeHeight = %d but the sized chrome is %d after a delete: the re-settle did not fire", got, want)
	}
}

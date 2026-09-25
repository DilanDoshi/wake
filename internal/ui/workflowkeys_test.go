package ui

// Keys the /workflows view owns reach it before anything that acts on a draft
// it hides - a highlighted draft's ⌫, a pasted image path - and ⌃C in its save
// dialog cancels the dialog alone.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// roomSelectedThenView is the room's draft with "world" highlighted, and then
// alex's view focused with its save dialog up - the highlight left standing,
// since nothing between the two cleared it.
func roomSelectedThenView(t *testing.T) App {
	t.Helper()
	a := workflowFleet(t).openDMWith("s1", "alex").refocus("").withDraft("hello world")
	r := a.regions()
	draftTop, _, _, _, ok := a.composerRegion("", r.Room(), 0, a.paneHeight())
	if !ok {
		t.Fatal("no room composer region")
	}
	left := a.layout.PaneLeft(r, 0) + composerTextLeft
	a, _ = drag(a, left+6, left+10, draftTop)
	if !a.sel.inComposer || a.sel.empty() {
		t.Fatalf("no room draft selection was taken: %+v", a.sel)
	}
	a = a.refocus("s1").openWorkflow("s1", wfTask)
	a.workflow.view.Save = &saveDialog{Name: "count-lines", Scope: rpc.ScopeProject}
	return a
}

func TestBackspaceInTheViewNeverDeletesAnotherPanesHighlightedDraft(t *testing.T) {
	a, _ := pressKey(roomSelectedThenView(t), wfKey(tea.KeyBackspace))
	if got := a.room.Composer().Value(); got != "hello world" {
		t.Errorf("⌫ in alex's view deleted the room's highlighted draft: %q", got)
	}
	if d := a.workflow.view.Save; d == nil || d.Name != "count-line" {
		t.Errorf("⌫ did not reach the save dialog: %+v", d)
	}
}

func TestAnImagePastedIntoTheViewAttachesNothingToTheHiddenDraft(t *testing.T) {
	a, cmd := pastePath(runOpen(t), writePNG(t, "shot.png"))
	for _, msg := range drainBatch(cmd) {
		m, _ := a.Update(msg)
		a = m.(App)
	}
	if imgs := a.dms["s1"].Composer().Images(); len(imgs) != 0 {
		t.Errorf("an image pasted over the view became %d chip(s) in alex's hidden draft", len(imgs))
	}
	if !a.workflow.view.Open() {
		t.Error("the paste closed the view")
	}
}

// ⌃C in the dialog is a cancel reflex: it closes the dialog, visibly, and
// parks nothing - parking would end the agent and the run it is saving.
func TestCtrlCInTheSaveDialogCancelsTheDialogAlone(t *testing.T) {
	a, _ := pressKey(runOpen(t), wfRune('s'))
	a, cmd := pressKey(a, wfKey(tea.KeyCtrlC))
	if a.workflow.view.Save != nil || !a.workflow.view.Open() {
		t.Errorf("⌃C in the dialog left dialog %+v, view open %v; want the dialog closed and the view up",
			a.workflow.view.Save, a.workflow.view.Open())
	}
	if _, parking := a.parking["s1"]; parking {
		t.Error("⌃C in the save dialog parked alex")
	}
	if got := kindsFor(writtenFrames(t, a, cmd), rpc.FramePark); len(got) != 0 {
		t.Errorf("⌃C in the save dialog wrote a park for %v", got)
	}
}

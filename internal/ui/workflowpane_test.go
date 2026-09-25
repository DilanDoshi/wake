package ui

// The /workflows view belongs to the pane that holds the keys: it closes when
// they leave, and it never hides another pane-scoped modal that esc would
// uncover.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// clickRosterAgent clicks id's own roster row - not a dispatch under it.
func clickRosterAgent(t *testing.T, a App, id string) App {
	t.Helper()
	for y := range a.paneHeight() {
		if ag, dispatch, ok := a.clickedAgent(y); ok && ag.ID == id && dispatch == "" {
			a, _ = click(a, a.layout.Width-2, y)
			return a
		}
	}
	t.Fatalf("no roster row is %s's", id)
	return a
}

// The reviewer's reproduction: the save dialog up in alex's view, a roster
// click puts sydney in alex's slot, and a second brings alex back - to the
// conversation, never to a view or a dialog that outlived the pane.
func TestTheViewDoesNotOutliveItsPane(t *testing.T) {
	a, _ := pressKey(runOpen(t), wfRune('s'))
	if a.workflow.view.Save == nil {
		t.Fatal("s opened no save dialog")
	}
	a = clickRosterAgent(t, a, "s2")
	if a.focus != "s2" || a.grid.Has("s1") {
		t.Fatalf("the click left focus %q and alex on screen %v, want sydney in alex's slot", a.focus, a.grid.Has("s1"))
	}
	a = clickRosterAgent(t, a, "s1")
	if a.focus != "s1" {
		t.Fatalf("the second click left the keys on %q", a.focus)
	}
	if a.workflow.view.Open() {
		t.Fatalf("alex came back under the view it left: %+v", a.workflow.view)
	}
	if out := stripANSI(a.View()); strings.Contains(out, saveDialogTitle) || strings.Contains(out, "Phases") {
		t.Errorf("the frame still draws the view or its dialog:\n%s", out)
	}
	a, _ = pressKey(a, wfRune('h'))
	a, _ = pressKey(a, wfRune('i'))
	if got := a.composer().Value(); got != "hi" {
		t.Errorf("typing into alex's conversation reached %q, want the draft", got)
	}
}

// Every other way the keys leave the pane closes it too - the room keys a
// click, a spawn's refocus - and the pane leaving the grid under them.
func TestTheViewClosesWhenTheKeysLeaveItsPane(t *testing.T) {
	for name, leave := range map[string]func(App) App{
		"refocus":        func(a App) App { return a.refocus("") },
		"another agent":  func(a App) App { return a.refocus("s2") },
		"its pane quits": func(a App) App { return a.forgetConversation("s1") },
	} {
		a := leave(runOpen(t))
		if a.workflow.view.Open() {
			t.Errorf("%s: the view is still open over %q", name, a.workflow.view.Pane)
		}
	}
}

func TestOpeningTheViewClosesTheMCPMenuItCovers(t *testing.T) {
	a := runOpen(t).closeWorkflow()
	a, _ = a.mcp("")
	if !a.mcpUI.menu.Open() || a.mcpUI.menu.Pane != "s1" {
		t.Fatalf("/mcp opened %+v, want alex's menu", a.mcpUI.menu)
	}
	a = a.openWorkflow("s1", wfTask)
	if a.mcpUI.menu.Open() {
		t.Fatalf("the view opened over an open /mcp menu: %+v", a.mcpUI.menu)
	}
	a, _ = pressKey(a, wfKey(tea.KeyEsc))
	if a.workflow.view.Open() || a.mcpUI.menu.Open() {
		t.Errorf("esc left view %+v and menu %+v, want neither", a.workflow.view, a.mcpUI.menu)
	}
}

func TestOpeningTheViewClosesTheRewindPickerItCovers(t *testing.T) {
	a := runOpen(t).closeWorkflow()
	a.rewind = RewindPicker{Session: "s1", Prompts: []string{"an earlier prompt"}, UUIDs: []string{"u1"}}
	a, _ = openedWorkflows(t, a)
	if a.rewind.Session != "" {
		t.Errorf("the view opened over the rewind picker: %+v", a.rewind)
	}
}

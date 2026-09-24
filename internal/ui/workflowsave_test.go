package ui

// s saves the open run's script as a reusable /<name> command: the dialog, its
// keys, the one frame ↵ writes, and the notice the daemon's answer becomes.

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// savesWritten is the FrameSaveWorkflow frames a command tree wrote.
func savesWritten(t *testing.T, a App, cmd tea.Cmd) []rpc.Frame {
	t.Helper()
	var out []rpc.Frame
	for _, f := range writtenFrames(t, a, cmd) {
		if f.Kind == rpc.FrameSaveWorkflow {
			out = append(out, f)
		}
	}
	return out
}

func saving(t *testing.T) App {
	t.Helper()
	a, _ := pressKey(runOpen(t), wfRune('s'))
	d := a.workflow.view.Save
	if d == nil {
		t.Fatalf("s did not open the save dialog: %+v", a.workflow.view)
	}
	if d.Name != "count-lines" || d.Scope != rpc.ScopeProject {
		t.Errorf("the dialog opened on %+v, want the run's own name in the project", *d)
	}
	return a
}

func dialogKeys(a App, keys ...tea.KeyMsg) App {
	for _, k := range keys {
		a, _ = pressKey(a, k)
	}
	return a
}

func TestSOpensTheDialogOverTheViewWithTheRunsName(t *testing.T) {
	a := saving(t)
	lines := drawnView(t, a)
	for _, want := range []string{"Save workflow", "project · .claude/workflows/count-lines.js", "Save as: > count-lines",
		"↵ save · ⇥ project/personal · esc cancel"} {
		if _, ok := wfLine(lines, want); !ok {
			t.Errorf("the dialog does not draw %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	// The dialog's keys are the only ones live, so the view's own line is covered.
	if _, ok := wfLine(lines, "f filter"); ok {
		t.Errorf("the view's key line is still drawn under the dialog:\n%s", strings.Join(lines, "\n"))
	}
	if _, ok := wfLine(lines, "Phases"); !ok {
		t.Errorf("the dialog hid the whole view rather than drawing over its foot:\n%s", strings.Join(lines, "\n"))
	}
}

func TestTypingEditsTheNameAndBackspaceDeletes(t *testing.T) {
	a := dialogKeys(saving(t), wfRune('-'), wfKey(tea.KeySpace), wfKey(tea.KeyBackspace),
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v2x")}, wfKey(tea.KeyBackspace))
	if got := a.workflow.view.Save.Name; got != "count-lines-v2" {
		t.Errorf("the name is %q after typing, want count-lines-v2", got)
	}
	if _, ok := wfLine(drawnView(t, a), "Save as: > count-lines-v2"); !ok {
		t.Errorf("the dialog does not draw the edited name:\n%s", strings.Join(drawnView(t, a), "\n"))
	}
	a = dialogKeys(a, wfKey(tea.KeyBackspace), wfKey(tea.KeyBackspace))
	for range 20 {
		a = dialogKeys(a, wfKey(tea.KeyBackspace))
	}
	if got := a.workflow.view.Save.Name; got != "" {
		t.Errorf("⌫ past the start left %q", got)
	}
}

func TestTabTogglesTheScopeAndItsPath(t *testing.T) {
	a := dialogKeys(saving(t), wfKey(tea.KeyTab))
	if got := a.workflow.view.Save.Scope; got != rpc.ScopeUser {
		t.Fatalf("⇥ left the scope %q, want %q", got, rpc.ScopeUser)
	}
	lines := drawnView(t, a)
	if _, ok := wfLine(lines, "personal · ~/.claude/workflows/count-lines.js"); !ok {
		t.Errorf("the personal scope does not draw its path:\n%s", strings.Join(lines, "\n"))
	}
	a = dialogKeys(a, wfKey(tea.KeyTab))
	if got := a.workflow.view.Save.Scope; got != rpc.ScopeProject {
		t.Errorf("a second ⇥ left the scope %q, want %q", got, rpc.ScopeProject)
	}
	if l, ok := wfLine(drawnView(t, a), ".claude/workflows/count-lines.js"); !ok || strings.Contains(l, "~") {
		t.Errorf("back in the project the path is %q", l)
	}
}

func TestEnterWritesOneSaveAndClosesTheDialog(t *testing.T) {
	a := dialogKeys(saving(t), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-v2")}, wfKey(tea.KeyTab))
	a, cmd := pressKey(a, wfKey(tea.KeyEnter))
	got := savesWritten(t, a, cmd)
	if len(got) != 1 {
		t.Fatalf("↵ wrote %d saves, want 1", len(got))
	}
	want := rpc.WorkflowFrame{Task: wfTask, Name: "count-lines-v2", Scope: rpc.ScopeUser}
	if f := got[0]; f.SessionID != "s1" || f.Workflow == nil || !reflect.DeepEqual(*f.Workflow, want) {
		t.Errorf("the save is %+v %+v, want alex's %+v", f, f.Workflow, want)
	}
	if v := a.workflow.view; v.Save != nil || !v.Open() || v.Level != levelRun {
		t.Errorf("after ↵ the view is %+v, want the dialog closed over the run", v)
	}
}

func TestEscClosesTheDialogOnly(t *testing.T) {
	a, cmd := pressKey(saving(t), wfKey(tea.KeyEsc))
	if v := a.workflow.view; v.Save != nil || !v.Open() || v.Level != levelRun || v.Task != wfTask {
		t.Errorf("esc in the dialog left %+v, want the run level with no dialog", v)
	}
	if got := savesWritten(t, a, cmd); len(got) != 0 {
		t.Errorf("esc wrote %d saves", len(got))
	}
}

// A name the daemon would refuse is refused where it was typed: nothing is
// written, the dialog stays for a fix, and the notice says why.
func TestAnInvalidNameIsRefusedInTheDialogWithoutWriting(t *testing.T) {
	for name, keys := range map[string][]tea.KeyMsg{
		"upper case": {tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")}},
		"a slash":    {tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/..")}},
		"empty":      backspaces(len("count-lines")),
	} {
		a := dialogKeys(saving(t), keys...)
		bad := a.workflow.view.Save.Name
		a, cmd := pressKey(a, wfKey(tea.KeyEnter))
		if got := savesWritten(t, a, cmd); len(got) != 0 {
			t.Errorf("%s: ↵ on %q wrote %d saves", name, bad, len(got))
		}
		if d := a.workflow.view.Save; d == nil || d.Name != bad {
			t.Errorf("%s: the refusal closed or changed the dialog: %+v", name, d)
		}
		n, ok := notice.Latest()
		if want := rpc.ValidWorkflowName(bad); !ok || want == nil || n.Text != want.Error() {
			t.Errorf("%s: the notice is %q, want the fence's own reason %v", name, n.Text, want)
		}
	}
}

func backspaces(n int) []tea.KeyMsg {
	out := make([]tea.KeyMsg, n)
	for i := range out {
		out[i] = wfKey(tea.KeyBackspace)
	}
	return out
}

func TestKeysInTheDialogNeverReachTheViewOrTheComposer(t *testing.T) {
	a := saving(t)
	before := a.workflow.view
	a = dialogKeys(a, wfKey(tea.KeyDown), wfKey(tea.KeyRight), wfKey(tea.KeyLeft))
	if v := a.workflow.view; v.Cursor != before.Cursor || v.Column != before.Column || v.Level != before.Level {
		t.Errorf("arrow keys in the dialog moved the view underneath: %+v", v)
	}
	a = dialogKeys(a, wfRune('x'))
	if a.workflow.view.Armed {
		t.Error("x typed into the dialog's name armed a stop")
	}
	if got := a.composer().Value(); got != "" {
		t.Errorf("keys in the dialog reached the draft: %q", got)
	}
}

func TestTheSavedAnswerIsANoticeNamingTheCommand(t *testing.T) {
	a := runOpen(t)
	const path = "/tmp/proj/.claude/workflows/count-lines.js"
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameWorkflowSaved, SessionID: "s1", Workflow: &rpc.WorkflowFrame{Path: path}})
	n, ok := notice.Latest()
	want := "Saved /count-lines → " + path + " · runs as /count-lines in new sessions"
	if !ok || n.Text != want {
		t.Errorf("the notice is %q, want %q", n.Text, want)
	}
	if !a.workflow.view.Open() {
		t.Error("the answer closed the view")
	}
}

// A refusal is the daemon's FrameError, which the one error path already
// reports with the agent's name and the daemon's reason.
func TestARefusedSaveIsANoticeWithTheReason(t *testing.T) {
	const why = "/tmp/proj/.claude/workflows/count-lines.js already exists"
	_ = runOpen(t).applyFrame(rpc.Frame{Kind: rpc.FrameError, SessionID: "s1", Text: why})
	if n, ok := notice.Latest(); !ok || n.Text != "@alex: "+why {
		t.Errorf("the notice is %q, want the refusal's reason", n.Text)
	}
}

// The dialog is drawn inside the view's block at every size a pane can be.
func TestTheDialogFitsEveryPaneSize(t *testing.T) {
	v := runLevel(midRun())
	v.Save = &saveDialog{Name: "count-lines", Scope: rpc.ScopeUser}
	for _, w := range []int{1, 8, 20, 33, 60, 140} {
		for _, h := range []int{1, 2, 3, 5, 7, 30} {
			requireFits(t, v.render([]workflowRunView{midRun()}, w, h), w, h)
			if t.Failed() {
				t.Fatalf("the dialog at %dx%d", w, h)
			}
		}
	}
	if _, ok := wfLine(wfLines(v.render([]workflowRunView{midRun()}, 60, 30)), "Save as: > count-lines"); !ok {
		t.Error("the dialog is not drawn at all, so fitting proves nothing")
	}
}

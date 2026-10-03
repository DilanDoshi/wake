package ui

// The rewind picker's second step: ↵ on a prompt previews what restoring its
// files would change, then offers Claude Code's choices. See rewindmenu.go.

import (
	"net"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const restoreCwd = "/work/repo"

// restoreApp is alex (s1) idle in restoreCwd with its picker open on two
// prompts, u2 the newest; blair (s2) shares the directory in the state given.
func restoreApp(t *testing.T, conn net.Conn, blairState string) App {
	t.Helper()
	a := dmApp(conn, Stream{}, "s1", "alex").withSize(160, 40)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle, Cwd: restoreCwd},
		{ID: "s2", Name: "blair", State: blairState, Cwd: restoreCwd},
	}}})
	a.rewind = RewindPicker{Session: "s1", Prompts: []string{"newest prompt", "add the retry loop"},
		UUIDs: []string{"u2", "u1"}, LastSeen: "u2"}
	return a
}

// previewOf is the dry run's receipt for u1, as the session labels it.
func previewOf(files ...string) core.Event {
	return core.Event{Kind: core.KindFilesRewindReceipt, Files: &core.FilesRewind{
		Target: "u1", Preview: true, Restorable: true, Files: files, Insertions: 12, Deletions: 40}}
}

// chooseOlder moves onto u1 and presses ↵, the step that asks for its preview.
func chooseOlder(t *testing.T, a App, sent <-chan rpc.Frame) App {
	t.Helper()
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if sent != nil {
		go func() { _ = runCmdQuietly(cmd) }()
		if f := awaitFrame(t, sent); f.Kind != rpc.FrameRewindPreview || f.SessionID != "s1" || f.RewindTarget != "u1" {
			t.Fatalf("↵ on a prompt wrote %+v, want a preview of u1", f)
		}
	}
	return a
}

func noFrame(t *testing.T, sent <-chan rpc.Frame, cmd tea.Cmd, what string) {
	t.Helper()
	if cmd != nil {
		go func() { _ = runCmdQuietly(cmd) }()
	}
	select {
	case f := <-sent:
		t.Fatalf("%s wrote %+v; it should send nothing", what, f)
	default:
	}
}

func TestEnterOnAPromptAsksForItsPreviewAndWaits(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := chooseOlder(t, restoreApp(t, conn, rpc.StateIdle), sent)
	if !a.rewind.Open() || a.rewind.Restore.Target != "u1" || a.rewind.Restore.Preview != nil {
		t.Fatalf("after ↵ the picker is %+v, want step two on u1 waiting for its preview", a.rewind)
	}
	if view := a.rewindView(160, "s1"); !strings.Contains(view, "checking files") {
		t.Errorf("the pending step draws %q, want it to say it is checking", view)
	}

	// A fast second ↵ - today's esc esc ↵ reflex, doubled - lands before the
	// reply and must not queue a choice.
	after, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	noFrame(t, sent, cmd, "↵ before the preview")
	if !after.rewind.Open() || after.rewind.Restore.Target != "u1" {
		t.Fatalf("↵ before the preview moved the picker to %+v", after.rewind)
	}

	// esc at the pending stage goes back to the list; a second esc closes it.
	after, _ = pressKey(after, tea.KeyMsg{Type: tea.KeyEsc})
	if !after.rewind.Open() || after.rewind.Restore.open() {
		t.Fatalf("esc while checking left %+v, want the prompt list back", after.rewind)
	}
	after, _ = pressKey(after, tea.KeyMsg{Type: tea.KeyEsc})
	if after.rewind.Open() {
		t.Error("esc on the list left the picker up")
	}
}

func TestAPreviewOffersClaudeCodesChoices(t *testing.T) {
	for _, c := range []struct {
		name    string
		preview core.Event
		want    []string
		absent  []string
	}{
		{"files would change", previewOf(restoreCwd+"/internal/retry.go", "/elsewhere/notes.md"),
			[]string{"2 files would change · +12 −40", "internal/retry.go", "/elsewhere/notes.md",
				"Restore conversation", "Restore code and conversation", "Restore code", "Never mind"}, nil},
		{"nothing to undo", previewOf(),
			[]string{"no file changes since this prompt", "Restore conversation", "Never mind"},
			[]string{"Restore code"}},
		{"refused", core.Event{Kind: core.KindFilesRewindReceipt, Files: &core.FilesRewind{
			Target: "u1", Preview: true, Error: "File rewinding is not enabled."}},
			[]string{"File rewinding is not enabled.", "Restore conversation", "Never mind"},
			[]string{"Restore code"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			a := chooseOlder(t, restoreApp(t, nil, rpc.StateIdle), nil)
			a = a.observe("s1", c.preview)
			view := a.rewindView(160, "s1")
			for _, w := range c.want {
				if !strings.Contains(view, w) {
					t.Errorf("view lacks %q:\n%s", w, view)
				}
			}
			for _, w := range c.absent {
				if strings.Contains(view, w) {
					t.Errorf("view offers %q with no files to restore:\n%s", w, view)
				}
			}
		})
	}
}

// A preview for anything but the step it was asked for is dropped.
func TestAStalePreviewIsDropped(t *testing.T) {
	fresh(t)
	a := chooseOlder(t, restoreApp(t, nil, rpc.StateIdle), nil)
	other := previewOf("/x")
	other.Files.Target = "u2"
	if got := a.observe("s1", other); got.rewind.Restore.Preview != nil {
		t.Error("a preview of another prompt filled the step")
	}
	if got := a.observe("s2", previewOf("/x")); got.rewind.Restore.Preview != nil {
		t.Error("another session's preview filled the step")
	}
	if got := a.closeRewind().observe("s1", previewOf("/x")); got.rewind.Open() {
		t.Error("a preview reopened a closed picker")
	}
}

func TestRestoreConversationSendsAFrameRewindAtOnce(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := chooseOlder(t, restoreApp(t, conn, rpc.StateIdle), sent).observe("s1", previewOf("/work/repo/a.go"))
	after, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter}) // the cursor rests on Restore conversation
	if after.rewind.Open() {
		t.Error("restoring the conversation left the picker up")
	}
	go func() { _ = runCmdQuietly(cmd) }()
	if f := awaitFrame(t, sent); f.Kind != rpc.FrameRewind || f.RewindTarget != "u1" || f.RewindLastSeen != "u2" {
		t.Errorf("wrote %+v, want a FrameRewind to u1 from u2", f)
	}
}

// Restoring code is destructive: the first ↵ arms and the second sends.
func TestACodeRestoreIsArmedThenSent(t *testing.T) {
	for _, c := range []struct {
		name  string
		downs int
		kind  string
	}{{"code and conversation", 1, rpc.FrameRewindBoth}, {"code", 2, rpc.FrameRewindFiles}} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			conn, sent := pipeClient(t)
			a := chooseOlder(t, restoreApp(t, conn, rpc.StateIdle), sent).observe("s1", previewOf("/work/repo/a.go", "/work/repo/b.go"))
			for range c.downs {
				a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
			}
			armed, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
			noFrame(t, sent, cmd, "the first ↵ on a code restore")
			if !armed.rewind.Restore.Armed || !strings.Contains(armed.rewindView(160, "s1"), "↵ again restores 2 files") {
				t.Fatalf("the first ↵ did not arm:\n%s", armed.rewindView(160, "s1"))
			}
			after, cmd := pressKey(armed, tea.KeyMsg{Type: tea.KeyEnter})
			if after.rewind.Open() {
				t.Error("the confirming ↵ left the picker up")
			}
			go func() { _ = runCmdQuietly(cmd) }()
			if f := awaitFrame(t, sent); f.Kind != c.kind || f.RewindTarget != "u1" || f.RewindLastSeen != "u2" {
				t.Errorf("wrote %+v, want %s to u1 from u2", f, c.kind)
			}
		})
	}
}

// Any input but the confirm takes the arm back - a key, esc, or a mouse press,
// through App.disarmed like every other arm.
func TestAnythingButTheConfirmDisarmsACodeRestore(t *testing.T) {
	arm := func(t *testing.T) App {
		a := chooseOlder(t, restoreApp(t, nil, rpc.StateIdle), nil).observe("s1", previewOf("/work/repo/a.go"))
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
		if !a.rewind.Restore.Armed {
			t.Fatal("setup: not armed")
		}
		return a
	}
	for _, c := range []struct {
		name string
		msg  tea.Msg
	}{
		{"a cursor move", tea.KeyMsg{Type: tea.KeyUp}},
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}},
		{"a mouse press", tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: 2}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			m, _ := arm(t).Update(c.msg)
			after := m.(App)
			if after.rewind.Restore.Armed {
				t.Errorf("%s left the code restore armed", c.name)
			}
			if c.name == "esc" && !after.rewind.Restore.open() {
				t.Error("esc on an armed restore left step two; it should only take the arm back")
			}
		})
	}
}

// Another live agent in the same directory is named, working or not, because
// a restore puts back files it may have changed since.
func TestTheChoicesNameAnotherAgentInTheDirectory(t *testing.T) {
	fresh(t)
	a := chooseOlder(t, restoreApp(t, nil, rpc.StateWorking), nil).observe("s1", previewOf("/work/repo/a.go"))
	if view := a.rewindView(160, "s1"); !strings.Contains(view, "@blair (working) also runs in this directory") {
		t.Errorf("the choices do not warn about blair:\n%s", view)
	}
	manager := restoreApp(t, nil, rpc.StateWorking).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle, Cwd: restoreCwd},
		{ID: "s3", Name: core.ManagerName, State: rpc.StateWorking, Cwd: restoreCwd},
	}}})
	manager = chooseOlder(t, manager, nil).observe("s1", previewOf("/work/repo/a.go"))
	if view := manager.rewindView(160, "s1"); strings.Contains(view, core.ManagerName) {
		t.Errorf("the manager, which has no tools to edit with, is named as sharing the directory:\n%s", view)
	}
	gone := restoreApp(t, nil, rpc.StateParked)
	gone = chooseOlder(t, gone, nil).observe("s1", previewOf("/work/repo/a.go"))
	if view := gone.rewindView(160, "s1"); strings.Contains(view, "blair") {
		t.Errorf("a parked agent is named as sharing the directory:\n%s", view)
	}
}

func TestARestoresReceiptsAreReported(t *testing.T) {
	restore := func(f core.FilesRewind) core.Event {
		f.Target = "u1"
		return core.Event{Kind: core.KindFilesRewindReceipt, Files: &f}
	}
	for _, c := range []struct {
		name string
		ev   core.Event
		want string
	}{
		{"restored", restore(core.FilesRewind{Restorable: true}), "@alex's files were restored"},
		{"restored past links", restore(core.FilesRewind{Restorable: true, Skipped: 2}), "2 linked files left as they are"},
		{"refused", restore(core.FilesRewind{Error: "No files were restored"}), "@alex's files were not restored: No files were restored"},
		{"refused, both", restore(core.FilesRewind{Both: true, Error: "No files were restored"}),
			"the conversation was left as it was"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			restoreApp(t, nil, rpc.StateIdle).observe("s1", c.ev)
			if n, said := notice.Latest(); !said || !strings.Contains(n.Text, c.want) {
				t.Errorf("notice = %+v (said %v), want %q", n, said, c.want)
			}
		})
	}
}

// Both's two halves can fail apart: files restored, then the conversation
// rewind refused. The notice must say the files are already back.
func TestAConversationRefusedAfterARestoreSaysTheFilesAreBack(t *testing.T) {
	fresh(t)
	a := restoreApp(t, nil, rpc.StateIdle)
	a = a.observe("s1", core.Event{Kind: core.KindFilesRewindReceipt, Files: &core.FilesRewind{Target: "u1", Both: true, Restorable: true}})
	a.observe("s1", core.Event{Kind: core.KindRewindReceipt, Rewind: &core.RewindResult{Error: "stale target"}})
	n, said := notice.Latest()
	if !said || !strings.Contains(n.Text, "files were restored, but the conversation was not rewound: stale target") {
		t.Errorf("notice = %+v (said %v), want it to say the files are back and the conversation is not", n, said)
	}
}

// confirmRestore's own guard, called directly as TestChooseRewindRefusesARunningAgent
// calls chooseRewind's: an armed restore must not reach an agent that has
// started a turn since, even by a path that skipped rewindKey's check.
func TestConfirmRestoreRefusesARunningAgent(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := chooseOlder(t, restoreApp(t, conn, rpc.StateIdle), sent).observe("s1", previewOf("/work/repo/a.go"))
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	a = a.applyFrame(workingAgentFrame("s1", "alex"))
	a.rewind.Restore.Armed = true // as if the report had not closed it
	after, cmd := a.confirmRestore()
	if after.rewind.Open() {
		t.Error("confirmRestore against a running agent left the picker open")
	}
	noFrame(t, sent, cmd, "confirmRestore against a running agent")
}

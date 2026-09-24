package ui

// The /workflows view: opening it, the keys it owns, and the sidebar row's way
// into it. The drawing itself is workflowdraw_test.go's.

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// countLinesSnap is the recorded count-lines run mid-flight - both counts done,
// the sum still running - with the ids, model and figures workflow-run.jsonl
// carries.
func countLinesSnap() core.WorkflowSnapshot {
	const haiku = "claude-haiku-4-5-20251001"
	return core.WorkflowSnapshot{
		Phases: []core.WorkflowPhase{{Index: 1, Title: "Count"}, {Index: 2, Title: "Sum"}},
		Agents: []core.WorkflowAgent{
			{Index: 1, Phase: 1, Label: "count a.txt", AgentID: "adc8cb9f8b7dc021f", Model: haiku,
				State: core.WorkflowAgentDone, Tokens: 16497, ToolCalls: 2, Duration: 4587 * time.Millisecond},
			{Index: 2, Phase: 1, Label: "count b.txt", AgentID: "a4d025ad4f589bce7", Model: haiku,
				State: core.WorkflowAgentDone, Tokens: 16459, ToolCalls: 2, Duration: 4015 * time.Millisecond},
			{Index: 3, Phase: 2, Label: "sum", AgentID: "a8ca1238d00df98a5", Model: haiku,
				State: core.WorkflowAgentRunning},
		},
	}
}

const (
	wfTask     = "w557669ss"
	wfDispatch = "toolu_wf"
	wfSummary  = "Count lines of a.txt and b.txt in parallel, then sum them"
)

// workflowFleet is a room over alex (running count-lines) and sydney (running
// nothing), with the sidebar drawn so its cursor counts.
func workflowFleet(t *testing.T) App {
	t.Helper()
	a := newRoomApp(t).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateWorking},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateIdle},
	).withSize(160, 40)
	for _, ev := range []core.Event{
		workflowStarted(wfTask, wfDispatch, wfSummary, "count-lines"),
		workflowProgressed(wfTask, wfDispatch, countLinesSnap()),
	} {
		a = a.applyFrame(taskFrame("s1", ev))
	}
	return a
}

// secondRun adds an ended run to alex, so /workflows has a list to show.
func secondRun(a App) App {
	a = a.applyFrame(taskFrame("s1", workflowStarted("w2", "toolu_w2", "Deploy it", "deploy")))
	return a.applyFrame(taskFrame("s1", ended("w2", core.TaskDone)))
}

// onWorkflowRow walks the roster cursor down until it rests on alex's workflow.
func onWorkflowRow(t *testing.T, a App) App {
	t.Helper()
	for range 8 {
		a = a.pickAgent(1)
		if a.roster.Selected == "s1" && a.roster.SelectedTask == wfDispatch {
			return a
		}
	}
	t.Fatalf("the roster cursor never reached alex's workflow row: %+v", a.roster)
	return a
}

// requireRunOpen holds a view to the run level on count-lines in alex's pane,
// with the pane never sent into the dispatch's (empty) transcript.
func requireRunOpen(t *testing.T, a App, how string) {
	t.Helper()
	v := a.workflow.view
	if !v.Open() {
		t.Fatalf("%s on the workflow row opened no view", how)
	}
	if v.Pane != "s1" || v.Level != levelRun || v.Task != wfTask {
		t.Errorf("%s opened %+v, want the run level on %s in alex's pane", how, v, wfTask)
	}
	if a.focus != "s1" {
		t.Errorf("%s left the keys on %q, want alex's pane", how, a.focus)
	}
	if d := a.dms["s1"]; d != nil && d.Viewed() != "" {
		t.Errorf("%s sent the pane into dispatch %q, which forwards nothing: a blank transcript", how, d.Viewed())
	}
	if !strings.Contains(stripANSI(a.View()), "Phases") {
		t.Errorf("%s: the frame does not draw the run level:\n%s", how, stripANSI(a.View()))
	}
}

// writtenFrames runs a command tree of writes and returns what reached the
// daemon. Only for commands built from writes: a blink in the tree would sleep.
func writtenFrames(t *testing.T, a App, cmd tea.Cmd) []rpc.Frame {
	t.Helper()
	for _, msg := range drainBatch(cmd) {
		if e, ok := msg.(errMsg); ok {
			t.Fatalf("a write failed: %v", e.Err)
		}
	}
	return recorderOf(t, a).taken(t)
}

func kindsFor(frames []rpc.Frame, kind string) []string {
	var out []string
	for _, f := range frames {
		if f.Kind == kind {
			out = append(out, f.SessionID)
		}
	}
	return out
}

func wfKey(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k} }
func wfRune(r rune) tea.KeyMsg       { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// openedWorkflows runs /workflows through the router, the way a submitted
// draft reaches it, and returns the App and whatever it wrote.
func openedWorkflows(t *testing.T, a App) (App, []rpc.Frame) {
	t.Helper()
	next, cmd, mine := a.slash("/workflows")
	if !mine {
		t.Fatal("/workflows is not one of Wake's commands")
	}
	return next, writtenFrames(t, next, cmd)
}

// --- /workflows -----------------------------------------------------------

func TestWorkflowsInADMWithOneRunOpensItsRunLevel(t *testing.T) {
	a := workflowFleet(t).openDMWith("s1", "alex")
	a, frames := openedWorkflows(t, a)
	v := a.workflow.view
	if !v.Open() || v.Level != levelRun || v.Task != wfTask || v.Pane != "s1" {
		t.Fatalf("one run in scope opened %+v, want its run level - the list is skipped", v)
	}
	if got := kindsFor(frames, rpc.FrameWorkflows); len(got) != 1 || got[0] != "s1" {
		t.Errorf("opening in alex's DM asked for runs of %v, want exactly [s1]", got)
	}
}

func TestWorkflowsInADMWithTwoRunsOpensTheList(t *testing.T) {
	a := secondRun(workflowFleet(t)).openDMWith("s1", "alex")
	a, _ = openedWorkflows(t, a)
	if v := a.workflow.view; !v.Open() || v.Level != levelList {
		t.Fatalf("two runs in scope opened %+v, want the list", v)
	}
	out := stripANSI(a.View())
	for _, want := range []string{"count-lines", "deploy"} {
		if !strings.Contains(out, want) {
			t.Errorf("the list does not name %q:\n%s", want, out)
		}
	}
}

func TestWorkflowsInADMWithNoRunsSaysSoAndEscCloses(t *testing.T) {
	a := workflowFleet(t).openDMWith("s2", "sydney")
	a, _ = openedWorkflows(t, a)
	if !a.workflow.view.Open() {
		t.Fatal("/workflows with nothing to show opened nothing - it should say so in the pane")
	}
	if out := stripANSI(a.View()); !strings.Contains(out, "No workflows in this session.") {
		t.Errorf("an empty session's view does not say so:\n%s", out)
	}
	a, _ = pressKey(a, wfKey(tea.KeyEsc))
	if a.workflow.view.Open() {
		t.Error("esc on the empty view left it open")
	}
}

func TestWorkflowsInTheRoomListsEveryAgentsRunsUnderTheirNames(t *testing.T) {
	a := workflowFleet(t)
	a = a.applyFrame(taskFrame("s2", workflowStarted("w9", "toolu_w9", "Lint", "lint-all")))
	a, frames := openedWorkflows(t, a)
	if v := a.workflow.view; !v.Open() || v.Pane != "" || v.Session != "" || v.Level != levelList {
		t.Fatalf("/workflows in the room opened %+v, want the room's fleet-wide list", v)
	}
	out := stripANSI(a.View())
	alex, count := strings.Index(out, "@alex"), strings.Index(out, "count-lines")
	sydney, lint := strings.Index(out, "@sydney"), strings.Index(out, "lint-all")
	if alex < 0 || count < alex || sydney < 0 || lint < sydney {
		t.Errorf("the room's list does not group each run under its agent:\n%s", out)
	}
	got := kindsFor(frames, rpc.FrameWorkflows)
	if len(got) != 2 || !contains(got, "s1") || !contains(got, "s2") {
		t.Errorf("the room asked for runs of %v, want one FrameWorkflows per agent", got)
	}
}

func TestWorkflowsInAnEmptyFleetSaysSo(t *testing.T) {
	a := newRoomApp(t).withRoster(rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateIdle}).withSize(120, 30)
	a, _ = openedWorkflows(t, a)
	if out := stripANSI(a.View()); !strings.Contains(out, "No workflows in this fleet.") {
		t.Errorf("the room's empty view does not say so:\n%s", out)
	}
}

// A run that is only on disk arrives after the view opened; with exactly one in
// scope the list is still skipped, because nothing has been pressed since.
func TestADiskRunArrivingIntoAnUntouchedViewOpensItsRunLevel(t *testing.T) {
	a := workflowFleet(t).openDMWith("s2", "sydney")
	a, _ = openedWorkflows(t, a)
	snap := countLinesSnap()
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameWorkflowsReply, SessionID: "s2", Workflow: &rpc.WorkflowFrame{
		Runs: []core.WorkflowRun{{TaskID: "wdisk", Name: "old-run", Status: core.TaskDone, Duration: 9 * time.Second, Progress: &snap}},
	}})
	if v := a.workflow.view; v.Level != levelRun || v.Task != "wdisk" {
		t.Errorf("the one disk run did not open at its run level: %+v", v)
	}
	if out := stripANSI(a.View()); !strings.Contains(out, "old-run") {
		t.Errorf("the disk run is not drawn:\n%s", out)
	}
}

// Live wins over the disk record of the same run, and disk fills what live
// lacks: one row per task id.
func TestWorkflowRunsDeduplicatesByTaskIDWithLiveWinning(t *testing.T) {
	a := workflowFleet(t)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameWorkflowsReply, SessionID: "s1", Workflow: &rpc.WorkflowFrame{
		Runs: []core.WorkflowRun{
			{TaskID: wfTask, Name: "stale-name", Status: core.TaskDone, Started: time.Unix(100, 0)},
			{TaskID: "wolder", Name: "older", Status: core.TaskFailed, Started: time.Unix(50, 0)},
		},
	}})
	runs := a.workflowRuns("s1")
	if len(runs) != 2 {
		t.Fatalf("workflowRuns = %d rows, want 2 (one per task id): %+v", len(runs), runs)
	}
	if runs[0].Task != wfTask || !runs[0].Live || runs[0].Status != core.TaskRunning || runs[0].Name != "count-lines" {
		t.Errorf("the live run did not win its own disk record: %+v", runs[0])
	}
	if !runs[0].Started.Equal(time.Unix(100, 0)) {
		t.Errorf("the disk record's start did not fill the live row: %v", runs[0].Started)
	}
	if runs[1].Task != "wolder" || runs[1].Live {
		t.Errorf("the disk-only run is not listed after the live one: %+v", runs[1])
	}
}

// A workflow's description survives its short name taking Task.Name, so the
// header can say what the run is for.
func TestAWorkflowKeepsItsDescriptionAsItsSummary(t *testing.T) {
	row := folded(workflowStarted("w1", "toolu_1", wfSummary, "count-lines")).Rows()[0]
	if row.Summary != wfSummary {
		t.Errorf("Summary = %q, want the description task_started carried", row.Summary)
	}
}

// --- the sidebar row's way in ---------------------------------------------

func TestEnterOnAWorkflowRowOpensItsRunLevel(t *testing.T) {
	a, _ := pressKey(onWorkflowRow(t, workflowFleet(t)), wfKey(tea.KeyEnter))
	requireRunOpen(t, a, "↵")
}

func TestCtrlDOnAWorkflowRowOpensItsRunLevel(t *testing.T) {
	a, _ := pressKey(onWorkflowRow(t, workflowFleet(t)), wfKey(tea.KeyCtrlD))
	requireRunOpen(t, a, "⌃D")
}

func TestCtrlYOnAWorkflowRowOpensItsRunLevel(t *testing.T) {
	a, _ := pressKey(onWorkflowRow(t, workflowFleet(t)), wfKey(tea.KeyCtrlY))
	requireRunOpen(t, a, "⌃Y")
}

func TestCtrlBOnAWorkflowRowOpensItsRunLevel(t *testing.T) {
	a := workflowFleet(t).openDMWith("s2", "sydney")
	a, _ = pressKey(onWorkflowRow(t, a), wfKey(tea.KeyCtrlB))
	requireRunOpen(t, a, "⌃B")
}

func TestAClickOnAWorkflowRowOpensItsRunLevel(t *testing.T) {
	a := workflowFleet(t)
	for y := range a.paneHeight() {
		if _, dispatch, ok := a.clickedAgent(y); ok && dispatch == wfDispatch {
			a, _ = click(a, a.layout.Width-2, y)
			requireRunOpen(t, a, "a click")
			return
		}
	}
	t.Fatal("no screen row of the sidebar is the workflow's")
}

func TestEnterOnABoardWorkflowRowOpensItsRunLevel(t *testing.T) {
	m, _ := typeAndSubmit(workflowFleet(t), boardVerb)
	a := m.(App)
	a.board.Selected, a.board.SelectedTask = "s1", wfDispatch
	a, _, _ = a.boardKey(wfKey(tea.KeyEnter))
	requireRunOpen(t, a, "the board's ↵")
}

// Every way in queues the runs-on-disk ask, and Update's drain writes it.
func TestOpeningFromTheSidebarAsksForTheAgentsRuns(t *testing.T) {
	a := onWorkflowRow(t, workflowFleet(t))
	next, _, _ := a.key(wfKey(tea.KeyCtrlD))
	a = next.(App)
	a, cmd := a.takeHistoryAsks()
	if got := kindsFor(writtenFrames(t, a, cmd), rpc.FrameWorkflows); len(got) != 1 || got[0] != "s1" {
		t.Errorf("opening from the sidebar asked for runs of %v, want [s1]", got)
	}
	if len(a.workflow.asks) != 0 {
		t.Errorf("the drain left asks queued: %v", a.workflow.asks)
	}
}

// --- the keys -------------------------------------------------------------

func runOpen(t *testing.T) App {
	t.Helper()
	a, _ := openedWorkflows(t, workflowFleet(t).openDMWith("s1", "alex"))
	if a.workflow.view.Level != levelRun {
		t.Fatalf("the fixture did not open at the run level: %+v", a.workflow.view)
	}
	return a
}

func TestDownMovesThePhaseAndRightMovesToTheAgents(t *testing.T) {
	a := runOpen(t)
	a, _ = pressKey(a, wfKey(tea.KeyDown))
	if v := a.workflow.view; v.Cursor != 1 || v.Column != 0 {
		t.Fatalf("↓ on the phases left %+v, want the second phase", v)
	}
	a, _ = pressKey(a, wfKey(tea.KeyUp))
	a, _ = pressKey(a, wfKey(tea.KeyRight))
	if v := a.workflow.view; v.Column != 1 || v.Agent != 0 {
		t.Fatalf("→ left %+v, want the agents column", v)
	}
	a, _ = pressKey(a, wfKey(tea.KeyDown))
	if v := a.workflow.view; v.Agent != 1 {
		t.Errorf("↓ in the agents column left %+v, want the second agent", v)
	}
	a, _ = pressKey(a, wfKey(tea.KeyLeft))
	a, _ = pressKey(a, wfKey(tea.KeyEnter))
	if v := a.workflow.view; v.Column != 1 {
		t.Errorf("↵ on a phase left %+v, want the agents column", v)
	}
}

func TestFCyclesTheFilterAndHidesAgentsItDoesNotAdmit(t *testing.T) {
	a := runOpen(t)
	want := []workflowFilter{filterRunning, filterDone, filterFailed, filterAll}
	for _, w := range want {
		a, _ = pressKey(a, wfRune('f'))
		if a.workflow.view.Filter != w {
			t.Fatalf("f moved the filter to %v, want %v", a.workflow.view.Filter, w)
		}
	}
	a, _ = pressKey(a, wfRune('f')) // running: the Count phase has none
	out := stripANSI(a.View())
	if strings.Contains(out, "count a.txt") {
		t.Errorf("the running filter still shows a finished agent:\n%s", out)
	}
	a, _ = pressKey(a, wfRune('f')) // done
	if out := stripANSI(a.View()); !strings.Contains(out, "count a.txt") {
		t.Errorf("the done filter hides a finished agent:\n%s", out)
	}
}

func TestEscBacksOutAgentsThenPhasesThenTheList(t *testing.T) {
	a, _ := openedWorkflows(t, secondRun(workflowFleet(t)).openDMWith("s1", "alex"))
	a, _ = pressKey(a, wfKey(tea.KeyEnter)) // the list's first row is the newest run
	a, _ = pressKey(a, wfKey(tea.KeyRight))
	steps := []struct {
		name string
		ok   func(WorkflowView) bool
	}{
		{"agents → phases", func(v WorkflowView) bool { return v.Open() && v.Level == levelRun && v.Column == 0 }},
		{"phases → the list", func(v WorkflowView) bool { return v.Open() && v.Level == levelList }},
		{"the list → closed", func(v WorkflowView) bool { return !v.Open() }},
	}
	for _, s := range steps {
		a, _ = pressKey(a, wfKey(tea.KeyEsc))
		if !s.ok(a.workflow.view) {
			t.Fatalf("esc, %s: view is %+v", s.name, a.workflow.view)
		}
	}
}

func TestEscFromTheOnlyRunClosesTheView(t *testing.T) {
	a, _ := pressKey(runOpen(t), wfKey(tea.KeyEsc))
	if a.workflow.view.Open() {
		t.Errorf("esc on the only run's phases left %+v, want it closed - there is no list to go back to", a.workflow.view)
	}
}

// ⌃C closes the view and still parks: the kill switch needs its first press to
// do something visible (cmd/wake/killswitch.go).
func TestCtrlCClosesTheViewAndIsNotSwallowed(t *testing.T) {
	a, _ := pressKey(runOpen(t), wfKey(tea.KeyCtrlC))
	if a.workflow.view.Open() {
		t.Error("⌃C left the view open")
	}
	if _, parking := a.parking["s1"]; !parking {
		t.Error("⌃C was swallowed by the view: alex was not parked")
	}
}

func TestKeysTypedIntoTheViewNeverReachTheComposer(t *testing.T) {
	a := runOpen(t)
	for _, k := range []tea.KeyMsg{wfRune('h'), wfRune('i'), wfKey(tea.KeySpace), wfKey(tea.KeyBackspace), wfKey(tea.KeyTab)} {
		a, _ = pressKey(a, k)
	}
	if got := a.composer().Value(); got != "" {
		t.Errorf("keys pressed in the view reached the draft: %q", got)
	}
	if !a.workflow.view.Open() || a.focus != "s1" {
		t.Errorf("a key the view does not bind closed it or moved the keys: %+v focus %q", a.workflow.view, a.focus)
	}
}

// With the keys elsewhere the view is left alone and the keys do their own job.
func TestTheViewTakesNoKeysWhileItsPaneIsNotFocused(t *testing.T) {
	a := runOpen(t).refocus("")
	a, _ = pressKey(a, wfRune('x'))
	if got := a.composer().Value(); got != "x" {
		t.Errorf("the room's draft is %q: the view took a key its pane does not hold", got)
	}
	if !a.workflow.view.Open() {
		t.Error("typing in another pane closed the view")
	}
}

// --- the mouse ------------------------------------------------------------

// A press over the view is the view's: it never starts a transcript selection,
// and a phase row under the pointer becomes the cursor.
func TestAPressInTheViewSelectsARowAndNeverATranscript(t *testing.T) {
	a := runOpen(t)
	x, y, ok := viewCell(a, "2 Sum")
	if !ok {
		t.Fatalf("the Sum phase is not on screen:\n%s", stripANSI(a.View()))
	}
	a, _ = a.mouse(pressAt(x, y))
	if a.selecting || !a.sel.empty() {
		t.Errorf("a press on the view started a selection: %+v", a.sel)
	}
	if v := a.workflow.view; v.Column != 0 || v.Cursor != 1 {
		t.Errorf("a press on the Sum phase left %+v, want it the cursor", v)
	}
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: x, Y: y})
	if !a.workflow.view.Open() {
		t.Error("the release closed the view")
	}
}

// viewCell is the screen cell of the first line whose words run s, at the
// start of s's last word - a cell column, not a byte offset.
func viewCell(a App, s string) (x, y int, ok bool) {
	words := strings.Fields(s)
	for y, line := range strings.Split(stripANSI(a.View()), "\n") {
		if strings.Contains(strings.Join(strings.Fields(line), " "), s) {
			return ansi.StringWidth(line[:strings.Index(line, words[len(words)-1])]), y, true
		}
	}
	return 0, 0, false
}

// ⌃B from a lower pane is refused by name, and a workflow row under the cursor
// must not turn the refusal into ⌃D's replace: the grid holds and no view opens.
func TestARefusedCtrlBOnAWorkflowRowOpensNothing(t *testing.T) {
	a := workflowFleet(t).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateWorking},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s3", Name: "carol", State: rpc.StateIdle},
	)
	a = a.openDMWith("s1", "alex").openDMWith("s2", "sydney").openBelow("s3", "carol")
	if a.focus != "s3" {
		t.Fatalf("carol's lower pane does not hold the keys (%q): the refusal is never reached", a.focus)
	}
	a = onWorkflowRow(t, a)
	before := a.grid
	a, _ = pressKey(a, wfKey(tea.KeyCtrlB))
	if !reflect.DeepEqual(a.grid, before) {
		t.Errorf("a refused ⌃B moved the grid from %+v to %+v", before, a.grid)
	}
	if a.workflow.view.Open() || a.focus != "s3" {
		t.Errorf("a refused ⌃B opened %+v with the keys on %q, want nothing and carol", a.workflow.view, a.focus)
	}
}

// chatty gives alex n lines of prose, enough to overflow a pane, so a scroll of
// the room or alex's conversation is a real move rather than a clamp.
func chatty(a App, n int) App {
	for i := range n {
		a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
			Kind: core.KindAssistantText, SessionID: "s1", Text: fmt.Sprintf("line %d", i),
		}})
	}
	return a
}

func wheel(a App, up bool, x, y int) App {
	button := tea.MouseButtonWheelDown
	if up {
		button = tea.MouseButtonWheelUp
	}
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: button, X: x, Y: y})
	return a
}

// The wheel over the view walks its rows; the transcript under it is not drawn,
// so scrolling it would leave it silently scrolled back after esc.
func TestTheWheelOverTheViewInADMWalksItsRowsNotTheTranscript(t *testing.T) {
	a, _ := openedWorkflows(t, chatty(workflowFleet(t).openDMWith("s1", "alex"), 80))
	if tr := a.dms["s1"].tr; !tr.atBottom() || tr.bottom() <= tr.first() {
		t.Fatal("alex's transcript does not overflow its pane, so a scroll would prove nothing")
	}
	x, y, ok := viewCell(a, "2 Sum")
	if !ok {
		t.Fatalf("the view is not drawn:\n%s", stripANSI(a.View()))
	}
	a = wheel(a, false, x, y)
	if a.workflow.view.Cursor != 1 {
		t.Errorf("the wheel down left the phase cursor on %d, want 1", a.workflow.view.Cursor)
	}
	a = wheel(a, true, x, y)
	a = wheel(a, true, a.layout.Width-2, y) // over the sidebar: the focused pane's
	if a.workflow.view.Cursor != 0 {
		t.Errorf("the wheel up left the phase cursor on %d, want 0", a.workflow.view.Cursor)
	}
	if !a.dms["s1"].tr.atBottom() {
		t.Error("the wheel scrolled alex's transcript under the view")
	}
}

func TestTheWheelOverTheViewInTheRoomWalksItsRowsNotTheGroupChat(t *testing.T) {
	a, _ := openedWorkflows(t, chatty(secondRun(workflowFleet(t)), 80))
	if tr := a.room.tr; !tr.atBottom() || tr.bottom() <= tr.first() {
		t.Fatal("the group chat does not overflow its pane, so a scroll would prove nothing")
	}
	x, y, ok := viewCell(a, "deploy")
	if !ok {
		t.Fatalf("the room's list is not drawn:\n%s", stripANSI(a.View()))
	}
	a = wheel(a, false, x, y)
	if a.workflow.view.Cursor != 1 {
		t.Errorf("the wheel down left the list cursor on %d, want 1", a.workflow.view.Cursor)
	}
	a = wheel(a, true, x, y)
	if a.workflow.view.Cursor != 0 || !a.room.tr.atBottom() {
		t.Errorf("the wheel up left cursor %d and the group chat at bottom=%v, want 0 and true",
			a.workflow.view.Cursor, a.room.tr.atBottom())
	}
}

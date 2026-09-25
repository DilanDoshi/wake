package ui

// What the /workflows view draws, as a pure function of the view and its runs.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// midRun is count-lines as the view holds it mid-run.
func midRun() workflowRunView {
	return workflowRunView{
		Session: "s1", Agent: "alex", Task: wfTask, Name: "count-lines", Summary: wfSummary,
		Status: core.TaskRunning, Elapsed: 28 * time.Second, Snap: countLinesSnap(), Live: true,
	}
}

func runLevel(run workflowRunView) WorkflowView {
	return WorkflowView{Up: true, Pane: "s1", Session: "s1", Task: run.Task, Level: levelRun}
}

// wfLines is a drawn block with styling stripped and runs of spaces folded,
// so an assertion reads the words rather than the padding between them.
func wfLines(block string) []string {
	lines := strings.Split(stripANSI(block), "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}
	return lines
}

func wfLine(lines []string, s string) (string, bool) {
	for _, l := range lines {
		if strings.Contains(l, s) {
			return l, true
		}
	}
	return "", false
}

// keyRow is the view's last row with anything on it: the key line, which sits
// directly under the box or list rather than at the pane's foot.
func keyRow(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i] != "" {
			return lines[i]
		}
	}
	return ""
}

// requireFits holds a block to exactly h rows, every one exactly w cells - the
// pane's column keeps its width in the frame's join, and a row more than it was
// given scrolls the alt screen on every draw.
func requireFits(t *testing.T, block string, w, h int) {
	t.Helper()
	lines := strings.Split(block, "\n")
	if len(lines) != h {
		t.Fatalf("the view drew %d rows into %d:\n%s", len(lines), h, stripANSI(block))
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Errorf("row %d is %d cells wide, want %d: %q", i, got, w, stripANSI(l))
		}
	}
}

func TestTheRunLevelDrawsTheHeaderPhasesAgentsAndKeys(t *testing.T) {
	run := midRun()
	block := runLevel(run).render([]workflowRunView{run}, 90, 20)
	requireFits(t, block, 90, 20)
	lines := wfLines(block)

	header := lines[0]
	for _, want := range []string{"count-lines", "2/3 agents", "28s", "running"} {
		if !strings.Contains(header, want) {
			t.Errorf("the header %q does not say %q", header, want)
		}
	}
	for _, want := range []string{"✔ Count 2/2", "2 Sum 0/1", "Phases", "Count · 2 agents"} {
		if _, ok := wfLine(lines, want); !ok {
			t.Errorf("the run level does not draw %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	row, ok := wfLine(lines, "✔ count a.txt")
	if !ok {
		t.Fatalf("no agent row for count a.txt:\n%s", strings.Join(lines, "\n"))
	}
	for _, want := range []string{"haiku", "16.5k", "4s"} {
		if !strings.Contains(row, want) {
			t.Errorf("the agent row %q does not carry %q", row, want)
		}
	}
	keys := keyRow(lines)
	if keys != "↑↓ select · ↵ open · f filter · x stop · s save · esc back" {
		t.Errorf("the key line is %q", keys)
	}
}

func TestAnEndedRunOffersNoStop(t *testing.T) {
	run := midRun()
	run.Status = core.TaskDone
	keys := keyRow(wfLines(runLevel(run).render([]workflowRunView{run}, 90, 20)))
	if strings.Contains(keys, "x stop") {
		t.Errorf("an ended run still offers x stop: %q", keys)
	}
	if !strings.Contains(keys, "s save") {
		t.Errorf("the ended run's key line lost the rest of its keys: %q", keys)
	}
}

func TestAFailedRunShowsItsErrorsFirstLineUnderTheHeader(t *testing.T) {
	run := midRun()
	run.Status, run.Error = core.TaskFailed, "Error: deliberate probe failure\n    at script.js:4"
	lines := wfLines(runLevel(run).render([]workflowRunView{run}, 90, 20))
	if lines[1] != "Error: deliberate probe failure" {
		t.Errorf("the row under the header is %q, want the error's first line", lines[1])
	}
}

// Review Focus 1: thirty agents in one phase, a pane twelve rows tall - the
// agents window around the cursor and the block is exactly the rows it was given.
func TestManyAgentsWindowAroundTheCursorInAShortPane(t *testing.T) {
	run := midRun()
	run.Snap = core.WorkflowSnapshot{Phases: []core.WorkflowPhase{{Index: 1, Title: "Fan"}}}
	for i := range 30 {
		run.Snap.Agents = append(run.Snap.Agents, core.WorkflowAgent{
			Index: i + 1, Phase: 1, Label: fmt.Sprintf("agent-%02d", i+1), State: core.WorkflowAgentRunning,
		})
	}
	for _, cursor := range []int{0, 14, 24, 29} {
		v := runLevel(run)
		v.Column, v.Agent = 1, cursor
		block := v.render([]workflowRunView{run}, 80, 12)
		requireFits(t, block, 80, 12)
		want := fmt.Sprintf("agent-%02d", cursor+1)
		row, ok := wfLine(wfLines(block), want)
		if !ok || !strings.Contains(row, workflowCursor) {
			t.Errorf("cursor %d: %s is not drawn under the cursor:\n%s", cursor, want, stripANSI(block))
		}
	}
}

// Many phases window the same way.
func TestManyPhasesWindowAroundTheCursor(t *testing.T) {
	run := midRun()
	run.Snap = core.WorkflowSnapshot{}
	for i := range 12 {
		run.Snap.Phases = append(run.Snap.Phases, core.WorkflowPhase{Index: i + 1, Title: fmt.Sprintf("P%02d", i+1)})
	}
	v := runLevel(run)
	v.Cursor = 11
	block := v.render([]workflowRunView{run}, 60, 9)
	requireFits(t, block, 60, 9)
	if _, ok := wfLine(wfLines(block), "P12"); !ok {
		t.Errorf("the cursored last phase is off the drawn window:\n%s", stripANSI(block))
	}
}

// The view never draws wider or taller than its pane, at any size a pane can be.
func TestTheViewFitsEveryPaneSize(t *testing.T) {
	run := midRun()
	run.Status, run.Error = core.TaskFailed, "boom"
	views := map[string]WorkflowView{
		"run":  runLevel(run),
		"list": {Up: true, Pane: "", Level: levelList},
	}
	for name, v := range views {
		for _, w := range []int{1, 8, 20, 33, 60, 140} {
			for _, h := range []int{1, 2, 3, 5, 7, 30} {
				requireFits(t, v.render([]workflowRunView{run}, w, h), w, h)
				if t.Failed() {
					t.Fatalf("%s level at %dx%d", name, w, h)
				}
			}
		}
	}
}

func TestTheRoomListGroupsRunsUnderTheirAgent(t *testing.T) {
	alexRun := midRun()
	sydRun := workflowRunView{Session: "s2", Agent: "sydney", Task: "w9", Name: "lint-all", Status: core.TaskDone}
	v := WorkflowView{Up: true, Pane: "", Level: levelList, Cursor: 1}
	block := v.render([]workflowRunView{alexRun, sydRun}, 90, 10)
	requireFits(t, block, 90, 10)
	lines := wfLines(block)
	if lines[0] != "@alex" || !strings.Contains(lines[1], "count-lines") ||
		lines[2] != "@sydney" || !strings.Contains(lines[3], "lint-all") {
		t.Errorf("the room's list is not grouped by agent:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(lines[3], strings.TrimSpace(workflowCursor)) {
		t.Errorf("the cursor (run 1) is not on sydney's run: %q", lines[3])
	}
	if keys := keyRow(lines); keys != "↑↓ select · ↵ open · esc close" {
		t.Errorf("the list's key line is %q", keys)
	}
}

func TestAnEmptyListSaysWhoseItIs(t *testing.T) {
	session := wfLines(WorkflowView{Up: true, Pane: "s1", Session: "s1"}.render(nil, 60, 6))
	if session[0] != "No workflows in this session." {
		t.Errorf("an empty session's view reads %q", session[0])
	}
	fleet := wfLines(WorkflowView{Up: true}.render(nil, 60, 6))
	if fleet[0] != "No workflows in this fleet." {
		t.Errorf("an empty fleet's view reads %q", fleet[0])
	}
}

// cellOf is where s starts in a drawn block: its row, and its cell column.
func cellOf(t *testing.T, block, s string) (x, y int) {
	t.Helper()
	for y, line := range strings.Split(stripANSI(block), "\n") {
		if i := strings.Index(line, s); i >= 0 {
			return ansi.StringWidth(line[:i]), y
		}
	}
	t.Fatalf("%q is not drawn:\n%s", s, stripANSI(block))
	return 0, 0
}

// A press resolves against the geometry the draw used: an agent row takes the
// agents column, a run row takes the list's cursor, and a heading takes nothing.
func TestAPressLandsOnTheRowTheDrawPutThere(t *testing.T) {
	run := midRun()
	runs := []workflowRunView{run}
	v := runLevel(run)
	x, y := cellOf(t, v.render(runs, 90, 20), "count b.txt")
	if got := v.hit(runs, 90, 20, x, y); got.Column != 1 || got.Agent != 1 {
		t.Errorf("a press on count b.txt left %+v, want the agents column on it", got)
	}

	syd := workflowRunView{Session: "s2", Agent: "sydney", Task: "w9", Name: "lint-all", Status: core.TaskDone}
	list := WorkflowView{Up: true, Level: levelList}
	both := []workflowRunView{run, syd}
	block := list.render(both, 90, 10)
	x, y = cellOf(t, block, "lint-all")
	if got := list.hit(both, 90, 10, x, y); got.Cursor != 1 {
		t.Errorf("a press on sydney's run left the cursor on %d, want 1", got.Cursor)
	}
	x, y = cellOf(t, block, "@sydney")
	if got := list.hit(both, 90, 10, x, y); got.Cursor != 0 {
		t.Errorf("a press on a heading moved the cursor to %d", got.Cursor)
	}
}

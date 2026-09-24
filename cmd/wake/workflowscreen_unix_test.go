//go:build unix

package main

// A dynamic workflow driven through the real binary: a sidebar row while it
// runs, ↵ on that row into the /workflows view, down to one agent's own
// transcript on disk, and out again to stop the run or save its script. The
// unit tests decide what each level draws; this is the only thing that puts
// the keys, the daemon's disk reads and the stop_task round trip through a
// real pty.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hinshun/vt10x"
)

const (
	// workflowSidebarRow is the running run's row: its glyph, its name, and
	// the last snapshot's agents done of started.
	workflowSidebarRow = "◈ " + workflowName + " 2/3"
	// workflowTitle is the view's pane title, over whichever pane it covers.
	workflowTitle = "› workflows"
	// workflowHalted is the stopped run's ending line, in the conversation and
	// - headed by its agent - in the room.
	workflowHalted = `Workflow "` + workflowName + `" halted`
)

// workflowScreen starts a conversation in dir, runs the scripted workflow in
// it, and waits for the last snapshot to reach the sidebar.
func workflowScreen(t *testing.T, dir string) *screen {
	t.Helper()
	withScriptedAgent(t, scriptWorkflows)
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	t.Setenv("WAKE_PROJECTS", t.TempDir())

	s := startWakeIn(t, dir, 160, 44, cmdNew)
	s.await("ready")
	s.settle()
	s.send("go\r")
	s.await(heardPrefix + "workflow launched")
	s.await(workflowSidebarRow)
	return s
}

// openWorkflowRow walks the roster cursor onto the workflow's row with ⇧↓ and
// opens it with ↵. The row counts as cursored once its glyph leaves the hint
// colour it wears off the cursor, so the walk stops on it whatever order the
// roster ranked the agent and the manager in.
func (s *screen) openWorkflowRow() {
	s.t.Helper()
	off, ok := s.workflowGlyphFG()
	if !ok {
		s.t.Fatalf("no workflow row in the sidebar.\n%s", s.dump())
	}
	for range len(s.rosterNames()) + 2 {
		s.send("\x1b[1;2B") // ⇧↓
		s.settle()
		if fg, ok := s.workflowGlyphFG(); ok && fg != off {
			s.send("\r")
			s.await(workflowTitle)
			return
		}
	}
	s.t.Fatalf("⇧↓ never put the roster cursor on the workflow row.\n%s", s.dump())
}

// workflowGlyphFG is the colour of the sidebar row's ◈, and whether the row is
// on screen. The rightmost ◈ on its row, so a glyph a pane draws is never read.
func (s *screen) workflowGlyphFG() (vt10x.Color, bool) {
	for y, line := range s.lines() {
		if !strings.Contains(line, workflowSidebarRow) {
			continue
		}
		x := utf8.RuneCountInString(line[:strings.LastIndex(line, "◈")])
		s.term.Lock()
		defer s.term.Unlock()
		return s.term.Cell(x, y).FG, true
	}
	return 0, false
}

// The row, the view and the stop: the row sits under the agent that runs it;
// ↵ on it opens the run level; → and ↵ reach an agent whose activity the
// daemon read off its own transcript; esc climbs back out; and x then ↵ stops
// the run - its row leaves the sidebar, and its ending is the room's line at
// once and the conversation's once the view over it closes.
func TestAWorkflowRowOpensItsViewAndStopsTheRun(t *testing.T) {
	s := workflowScreen(t, "")
	name := s.agentName()
	lines := s.lines()
	row := s.rowOf(workflowSidebarRow)
	if row < 1 || !strings.Contains(sidebar(lines[row-1]), name) {
		t.Fatalf("the workflow row is not in the sidebar under %s's row.\n%s", name, s.dump())
	}

	s.openWorkflowRow()
	s.await("Phases")
	s.await("count a.txt")

	s.send("\x1b[C") // → to the agents column
	s.settle()
	s.send("\r") // into count a.txt
	s.await("Prompt")
	s.await("Activity")
	s.await("Bash(" + workflowAgentCommand + ")") // only its transcript on disk says so

	// One esc at a time: two in one read arrive as a single alt+esc.
	s.send("\x1b") // back to the run level
	s.await("Phases")
	s.settle()
	s.send("\x1b") // agents column to phases
	s.settle()

	s.send("x")
	s.await("↵ stop " + workflowName)
	s.send("\r")
	s.awaitGone(workflowSidebarRow)
	s.await(" · halted") // the run's own header
	s.awaitCount(workflowHalted, 1)

	s.send("\x1b") // close the view over the conversation
	s.awaitGone(workflowTitle)
	s.awaitCount(workflowHalted, 2)
}

// s then ↵ saves the running run's script where Claude Code keeps a project's
// workflows, byte for byte - in a scratch repository under a scratch HOME, so
// neither scope can reach the machine's own.
func TestAWorkflowSavesItsScriptFromTheView(t *testing.T) {
	dir := realisticWorkspace(t)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	s := workflowScreen(t, dir)
	s.openWorkflowRow()
	s.await("Phases")

	s.send("s")
	s.await("project · .claude/workflows/" + workflowName + ".js")
	s.await("Save as: > " + workflowName)
	s.send("\r")
	s.await("Saved /" + workflowName)

	got, err := os.ReadFile(filepath.Join(dir, ".claude", "workflows", workflowName+".js"))
	if err != nil {
		t.Fatalf("the saved workflow is not in the project's .claude/workflows: %v", err)
	}
	if string(got) != workflowScript {
		t.Errorf("the saved file is %q, want the run's own script %q", got, workflowScript)
	}
}

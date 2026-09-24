package ui

// The save dialog: s at the run level saves the run's script as a reusable
// /<name> command, Claude Code's own "s save". The wire carries a name and a
// scope and the daemon owns the path (--debug-file's ruling), so the path the
// dialog shows is a hint and the notice its answer becomes names the real one.

import (
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	workflowSaveFailed = "saving a workflow" // sendFailed's pattern
	workflowFileExt    = ".js"
	saveDialogTitle    = "Save workflow"
	saveDialogKeys     = "↵ save · ⇥ project/personal · esc cancel"
)

// saveCursor marks where typing lands in the name.
var saveCursor = lipgloss.NewStyle().Reverse(true).Render(" ")

type saveDialog struct {
	Name  string
	Scope string // rpc.ScopeProject / rpc.ScopeUser
}

// where is the scope in Claude Code's docs' words, and the file it would be.
func (d saveDialog) where() (scope, path string) {
	file := oneLine(d.Name) + workflowFileExt
	if d.Scope == rpc.ScopeUser {
		return "personal", "~/.claude/workflows/" + file
	}
	return "project", ".claude/workflows/" + file
}

// saveKey is one key against the dialog, which takes every one: typing edits
// the name, ⇥ moves it between the project and personal, ↵ saves, and esc
// closes the dialog alone.
func (a App) saveKey(m tea.KeyMsg) (App, tea.Cmd) {
	d := *a.workflow.view.Save
	switch m.Type {
	case tea.KeyEnter:
		return a.saveWorkflow()
	case tea.KeyEsc:
		a.workflow.view.Save = nil
		return a, nil
	case tea.KeyTab:
		if d.Scope == rpc.ScopeUser {
			d.Scope = rpc.ScopeProject
		} else {
			d.Scope = rpc.ScopeUser
		}
	case tea.KeyBackspace:
		r := []rune(d.Name)
		d.Name = string(r[:max(len(r)-1, 0)])
	case tea.KeySpace:
		d.Name += " "
	case tea.KeyRunes:
		d.Name += string(m.Runes)
	}
	a.workflow.view.Save = &d
	return a, nil
}

// saveWorkflow writes the dialog's FrameSaveWorkflow to the run's own agent and
// closes it. A name the daemon's fence would refuse is refused here instead,
// with the fence's reason, and the dialog stays up to be fixed.
func (a App) saveWorkflow() (App, tea.Cmd) {
	d := *a.workflow.view.Save
	if err := rpc.ValidWorkflowName(d.Name); err != nil {
		notice.Report("%v", err)
		return a, nil
	}
	a.workflow.view.Save = nil
	run, ok := a.openRun()
	if !ok { // the run left the list under the dialog, and the view says so
		return a, nil
	}
	return a, a.write(workflowSaveFailed, rpc.Frame{Kind: rpc.FrameSaveWorkflow, SessionID: run.Session,
		Workflow: &rpc.WorkflowFrame{Task: run.Task, Name: d.Name, Scope: d.Scope}})
}

// workflowSaved is the daemon's answer: the path it chose, and the command the
// file now answers to - its own name, the one the daemon wrote. A refusal is a
// FrameError instead, which apply already reports with its reason.
func (a App) workflowSaved(f rpc.Frame) App {
	if f.Workflow == nil {
		return a
	}
	name := strings.TrimSuffix(filepath.Base(f.Workflow.Path), workflowFileExt)
	notice.Report("Saved /%s → %s · runs as /%s in new sessions", name, f.Workflow.Path, name)
	return a
}

// rows is the dialog w wide: a box naming itself over where the save lands and
// the name being typed, with its keys in the bottom edge.
func (d saveDialog) rows(w int) []string {
	edge, inner := max(w-2, 0), max(w-4, 0)
	wall := HintStyle.Render("│")
	row := func(s string) string { return wall + " " + fitRow(s, inner) + " " + wall }
	scope, path := d.where()
	return []string{
		titledEdge("╭", "─", "╮", saveDialogTitle, edge, 0, HintStyle, AccentStyle),
		row(AccentStyle.Render(scope) + HintStyle.Render(" · "+path)),
		row(TextStyle.Render("Save as: > "+oneLine(d.Name)) + saveCursor),
		titledEdge("╰", "─", "╯", saveDialogKeys, edge, 0, HintStyle, HintStyle),
	}
}

// over draws the dialog onto a view's rows from its key line down - the last
// row stacked did not pad - pushed up only as far as the pane needs, so the
// view's own keys, which are the dialog's while it is up, are never drawn.
func (d saveDialog) over(rows []string, w int) []string {
	foot := len(rows) - 1
	for foot > 0 && rows[foot] == "" {
		foot--
	}
	box := d.rows(w)
	out := slices.Clone(rows)
	copy(out[max(min(foot, len(out)-len(box)), 0):], box)
	return out
}

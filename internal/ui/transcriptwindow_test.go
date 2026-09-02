package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
)

// transcriptWindow is what a view-only board tile draws: the tail of the
// conversation at the cell size, following the newest line, no composer chrome.
func TestTranscriptWindowRendersTheTailAtCellSize(t *testing.T) {
	d := NewDM("s1", "luca").SetSize(80, 20)
	d = d.Append(core.Event{Kind: core.KindAssistantText, Text: "first line of prose"})
	d = d.Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{ID: "t1", Name: "Bash", Display: "grep -n Model"}})
	_, body := d.transcriptWindow(30, 4)
	if lipgloss.Height(body) != 4 {
		t.Fatalf("window height = %d, want exactly 4 rows\n%s", lipgloss.Height(body), body)
	}
	// The real transcript, at DM fidelity: the prose block and the tool activity,
	// on separate rows - not a one-line summary. A lone tool use folds to a rollup
	// ("1 tool use · 1 bash") exactly as the DM pane draws it; the tile is view-only
	// so it inherits that fold rather than expanding it.
	if !strings.Contains(body, "first line of prose") {
		t.Errorf("window did not show the prose block:\n%s", body)
	}
	if !strings.Contains(body, "tool use") {
		t.Errorf("window did not show the tool activity:\n%s", body)
	}
}

// The DM's cost model, one method over: only a width change re-wraps through
// glamour. A tile drawn at a stable width per frame therefore pays no glamour.
func TestTranscriptWindowReWrapsOnlyOnWidthChange(t *testing.T) {
	renders := 0
	restore := renderTranscript
	renderTranscript = func(d DM) []block { renders++; return restore(d) }
	defer func() { renderTranscript = restore }()

	d := NewDM("s1", "luca").SetSize(80, 20)
	d = d.Append(core.Event{Kind: core.KindAssistantText, Text: "prose"})
	renders = 0
	d, _ = d.transcriptWindow(30, 4) // width 80→30: one re-wrap
	d.transcriptWindow(30, 6)        // width same, height changed: no re-wrap, so no render
	if renders != 1 {
		t.Errorf("renderTranscript ran %d times, want 1 (only the width change re-wraps)", renders)
	}
}

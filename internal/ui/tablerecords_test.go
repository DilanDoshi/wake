package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// A conversation draws an agent's table at its pane's width the way Claude Code
// draws one at its terminal's: a grid while it fits, records once the pane is too
// narrow, and the grid again when the pane widens, since a width change re-wraps
// every block.
func TestAConversationRedrawsATableAsItsPaneResizes(t *testing.T) {
	table := "| Flag | Default | What it does |\n|:---|:---:|---:|\n" +
		"| `--effort` | medium | Sets how hard the model thinks before it answers, from low all the way up to max |\n" +
		"| --model | sonnet | Picks the model the session runs on |"
	d := NewDM("s1", "alex").SetSize(120, 40).Append(core.Event{Kind: core.KindAssistantText, Text: table})
	for _, step := range []struct {
		width   int
		records bool
	}{{120, false}, {44, true}, {120, false}} {
		d = d.SetSize(step.width, 40)
		view := ansi.Strip(d.View(step.width, 40))
		if got := strings.Contains(view, "Flag: --effort"); got != step.records {
			t.Errorf("at %d records %v, want %v:\n%s", step.width, got, step.records, view)
		}
		if grid := strings.Contains(view, "┼"); grid == step.records {
			t.Errorf("at %d grid %v, want %v:\n%s", step.width, grid, !step.records, view)
		}
	}
}

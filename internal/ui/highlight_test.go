package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestAHighlightRestylesCellsRatherThanRewritingThem(t *testing.T) {
	forceColour(t)
	got := highlighted("alpha bravo", 6, 11)
	if plain := ansi.Strip(got); plain != "alpha bravo" {
		t.Errorf("highlighted() changed the text to %q, want %q", plain, "alpha bravo")
	}
	before, _, _ := strings.Cut(got, "\x1b")
	if before != "alpha " {
		t.Errorf("the styling starts after %q, want after %q", before, "alpha ")
	}
}

func TestAHighlightSurvivesAResetInsideTheSelection(t *testing.T) {
	// glamour ends every span with an SGR reset. A background applied *around*
	// a string containing one ends at the reset, which is why the selected part
	// is stripped before it is restyled - see highlighted's comment.
	forceColour(t)
	line := "\x1b[31mred\x1b[0m green"
	got := highlighted(line, 0, 9)
	if plain := ansi.Strip(got); plain != "red green" {
		t.Errorf("highlighted() = %q (plain %q), want the text unchanged", got, plain)
	}
	if strings.Contains(ansi.Strip(got), "\x1b") {
		t.Error("stripping the selected part left an escape behind")
	}
}

func TestATranscriptWithNothingSelectedDrawsNoSelectionStyling(t *testing.T) {
	forceColour(t)
	tr := transcript{}.sized(20, 3).replace([]block{{text: "alpha"}})
	if strings.Contains(tr.view(marked{}), "\x1b") {
		t.Errorf("view(marked{}) = %q: the zero selection must draw no styling", tr.view(marked{}))
	}
}

func TestATranscriptHighlightsOnlyTheLinesTheSelectionCovers(t *testing.T) {
	forceColour(t)
	tr := transcript{}.sized(20, 3).replace([]block{{text: "alpha\nbravo\ncharlie"}})
	m := selection{anchor: point{0, 0}, head: point{0, 4}}.marked()
	lines := strings.Split(tr.view(m), "\n")
	if !strings.Contains(lines[0], "\x1b") {
		t.Errorf("line 0 = %q: the selection covers it and it carries no styling", lines[0])
	}
	if strings.Contains(lines[1], "\x1b") {
		t.Errorf("line 1 = %q: the selection does not cover it, so it must carry none", lines[1])
	}
}

func TestAHighlightStopsAtThePaneEdgeForALineEndSelection(t *testing.T) {
	forceColour(t)
	// A middle line of a multi-line drag is covered to lineEnd, which the
	// transcript resolves to its own width rather than the line's length.
	tr := transcript{}.sized(20, 3).replace([]block{{text: "alpha\nbravo\ncharlie"}})
	m := selection{anchor: point{0, 0}, head: point{2, 6}}.marked()
	for i, l := range strings.Split(tr.view(m), "\n") {
		if w := ansi.StringWidth(l); w != 20 {
			t.Errorf("line %d measures %d columns, want 20: a highlight may not change a pane's width", i, w)
		}
	}
}

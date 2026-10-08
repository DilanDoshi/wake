package render

// The wrap the open block of a streaming answer is drawn with, held to the one
// glamour lays prose out with: its left margin and its width, so a block that
// finishes does not jump a column or re-break its rows as it turns formatted.

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func proseSources() []string {
	out := []string{hyphenatedProse}
	r := rand.New(rand.NewSource(7))
	for range 40 {
		parts := make([]string, 6+r.Intn(30))
		for i := range parts {
			parts[i] = wrapWords[r.Intn(len(wrapWords))]
		}
		out = append(out, strings.Join(parts, " ")+".")
	}
	return out
}

// markdownFree is a recorded line with nothing in it markdown would act on, so
// Markdown lays it out as one plain paragraph.
func markdownFree(line string) bool {
	return line != "" && !strings.ContainsAny(line, "*_`[]#|<>&\\~!") && !strings.HasPrefix(line, "-") &&
		!strings.HasPrefix(line, "+") && (line[0] < '0' || line[0] > '9') && !strings.HasPrefix(line, ":")
}

// linked is a line holding a bare file name or domain (`tally.txt`), which glamour
// turns into a styled link. A styled row is left at glamour's own wrap, which can
// break it a word earlier than the plain wrap does: the one place the open block and
// the finished one may break a row differently, and a row at most, until it lands.
var linked = regexp.MustCompile(`[A-Za-z0-9]\.[A-Za-z]`)

// Rows are compared as a reader sees them: glamour pads each one to the width.
func TestProseWrapsTheWayMarkdownDoes(t *testing.T) {
	sources := proseSources()
	for _, doc := range corpusTexts(t) {
		for _, line := range strings.Split(doc, "\n") {
			if markdownFree(line) && len(line) > 60 && !linked.MatchString(line) {
				sources = append(sources, line)
			}
		}
	}
	var rows, runs, bad int
	for _, src := range sources {
		for width := 20; width <= 140; width += 3 {
			want := plainRows(Markdown(src, width))
			got := plainRows(Prose(src, width))
			rows, runs = rows+len(want), runs+1
			if d := divergence(want, got); d != "" {
				bad++
				if bad <= 3 {
					t.Errorf("width %d: %s\n%q", width, d, src)
				}
			}
		}
	}
	t.Logf("%d paragraphs at %d widths, %d rows: %d wrapped differently", len(sources), runs/len(sources), rows, bad)
}

func TestProseKeepsTheSourceLinesAndBoundsEveryRow(t *testing.T) {
	src := "first line\n\nsecond paragraph is a good deal longer than the pane is wide, so it wraps\n    indented stays"
	for _, w := range []int{20, 30, 60} {
		rows := strings.Split(Prose(src, w), "\n")
		if rows[1] != "" {
			t.Errorf("width %d: a blank source line drew %q, want a blank row", w, rows[1])
		}
		for _, r := range rows {
			if got := ansi.StringWidth(r); got > w {
				t.Errorf("width %d: row %q is %d cells", w, r, got)
			}
			if r != "" && !strings.HasPrefix(r, "  ") {
				t.Errorf("width %d: row %q is outside the document margin", w, r)
			}
		}
	}
	if got := Prose("", 40); got != "" {
		t.Errorf("no text drew %q", got)
	}
}

// Narrower than glamour can lay out a document, the floor is the same one Markdown uses.
func TestProseBelowTheFloorWrapsAtTheFloor(t *testing.T) {
	src := strings.Repeat("word ", 20)
	if got, want := Prose(src, 3), Prose(src, minMarkdownWidth); got != want {
		t.Errorf("a 3-column pane wrapped differently from the %d-column floor:\n%s\n%s", minMarkdownWidth, got, want)
	}
}

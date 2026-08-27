package render

import (
	"encoding/json"

	"github.com/charmbracelet/lipgloss"
	"os"
	"strings"
	"testing"
)

// The markdown style is Claude's, held to the same fixture internal/ui is.
//
// # Why this file reaches into another package's testdata
//
// Because there is one palette and it must stay one. The colours are spelled in
// markdownstyle.go rather than imported, since internal/ui imports this package
// and not the reverse - so without this the two copies could drift silently, and
// the failure would be a conversation pane whose prose and whose chrome are
// almost the same colour. The fixture is the extraction's output and neither
// package owns it.
const paletteFromUI = "../ui/testdata/claude-palette.json"

type claudePalette struct {
	Source string            `json:"_source"`
	Light  map[string]string `json:"light"`
	Dark   map[string]string `json:"dark"`
}

func loadClaudePalette(t *testing.T) claudePalette {
	t.Helper()

	raw, err := os.ReadFile(paletteFromUI)
	if err != nil {
		t.Fatalf("read %s: %v\nthe fixture is maintained by hand", paletteFromUI, err)
	}
	var p claudePalette
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("parse %s: %v", paletteFromUI, err)
	}
	return p
}

// Every colour this style spends is one of Claude's, by key.
//
// The table is also the enumeration, the way internal/ui's is: a colour added
// to the style without a line here is a colour nothing holds to the palette.
func TestTheMarkdownStyleSpendsOnlyClaudesColours(t *testing.T) {
	p := loadClaudePalette(t)

	for _, tc := range []struct {
		name string
		key  string
		dark string
		lite string
	}{
		{"inline code and links", "suggestion", suggestionDark, suggestionLight},
		{"rules", "subtle", subtleDark, subtleLight},
		{"quotes and alt text", "inactive", inactiveDark, inactiveLight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantDark, ok := p.Dark[tc.key]
			if !ok {
				t.Fatalf("the style binds Claude key %q, which %s (from %s) does not define",
					tc.key, paletteFromUI, p.Source)
			}
			if tc.dark != wantDark {
				t.Errorf("dark = %q, want %q (Claude's %s in %s)", tc.dark, wantDark, tc.key, p.Source)
			}
			if want := p.Light[tc.key]; tc.lite != want {
				t.Errorf("light = %q, want %q (Claude's %s in %s)", tc.lite, want, tc.key, p.Source)
			}
		})
	}
}

// A heading is bold and uncoloured, at every level and on both backgrounds.
//
// This is the regression, named exactly. glamour's stock dark style paints
// Heading in ANSI 39 and H1 in 228 on a 63 block, so every reply an agent wrote
// arrived with a blue bar of a title that Claude Code does not draw - which is
// what "it should look identical" was about first.
func TestHeadingsAreBoldAndUncoloured(t *testing.T) {
	for _, dark := range []bool{true, false} {
		s := claudeStyle(dark)
		for name, h := range map[string]struct {
			color *string
			bg    *string
			bold  *bool
		}{
			"Heading": {s.Heading.Color, s.Heading.BackgroundColor, s.Heading.Bold},
			"H1":      {s.H1.Color, s.H1.BackgroundColor, s.H1.Bold},
			"H2":      {s.H2.Color, s.H2.BackgroundColor, s.H2.Bold},
			"H3":      {s.H3.Color, s.H3.BackgroundColor, s.H3.Bold},
		} {
			if h.color != nil {
				t.Errorf("dark=%v: %s is coloured %q; Claude renders a heading in the body colour",
					dark, name, *h.color)
			}
			if h.bg != nil {
				t.Errorf("dark=%v: %s has background %q; nothing in Claude Code puts a block behind a title",
					dark, name, *h.bg)
			}
			if h.bold == nil || !*h.bold {
				t.Errorf("dark=%v: %s is not bold, so a heading is indistinguishable from prose", dark, name)
			}
		}
	}
}

// Inline code carries no background, and the document keeps glamour's margin.
//
// The 236 block behind every code span was a default nobody chose: at the
// density an agent writes about `this` and `that`, it turns a paragraph into a
// row of grey boxes. The **margin** is the opposite - it is a default this file
// deliberately does not touch, because internal/ui aligns to it, and the two
// tests that caught an attempt to remove it are the reason it is asserted here.
func TestInlineCodeHasNoBlockAndTheDocumentKeepsItsMargin(t *testing.T) {
	for _, dark := range []bool{true, false} {
		s := claudeStyle(dark)
		if s.Code.BackgroundColor != nil {
			t.Errorf("dark=%v: inline code sits on %q", dark, *s.Code.BackgroundColor)
		}
		if s.Code.Color == nil {
			t.Errorf("dark=%v: inline code has no colour, so it reads as ordinary prose", dark)
		}
		if s.Document.Margin == nil || *s.Document.Margin != defaultMargin {
			t.Errorf("dark=%v: the document margin moved off %d; dm_blocks.go indents a plain body by "+
				"exactly that much so thinking lines up with prose, and a subagent's gutter is drawn "+
				"inside the same budget", dark, defaultMargin)
		}
	}
}

// The two backgrounds differ, which is the whole reason the probe exists.
func TestTheTwoBackgroundsGetDifferentStyles(t *testing.T) {
	d, l := claudeStyle(true), claudeStyle(false)
	if d.Code.Color == nil || l.Code.Color == nil {
		t.Fatal("inline code is uncoloured on one of the two backgrounds")
	}
	if *d.Code.Color == *l.Code.Color {
		t.Errorf("both backgrounds render inline code %q: Claude's periwinkle is darker on a light "+
			"terminal for contrast, and resolving the background would buy nothing", *d.Code.Color)
	}
}

// The diff bands are Claude's four, and nothing else held them to the palette.
//
// They were right and unasserted: the markdown body was in the same state until
// this branch, and it had drifted to glamour's stock theme without anything
// failing. A colour nothing checks is a colour that is correct until somebody
// touches it, and these four are the ones that carry meaning by *ground* rather
// than by letterform - a diff whose bands drift is a diff that stops reading as
// one.
func TestTheDiffBandsAreClaudesFour(t *testing.T) {
	p := loadClaudePalette(t)

	for _, tc := range []struct {
		what  string
		key   string
		style lipgloss.Style
	}{
		{"an added line's band", "diffAdded", addBand},
		{"a removed line's band", "diffRemoved", delBand},
		{"the changed words inside an added line", "diffAddedWord", addWord},
		{"the changed words inside a removed line", "diffRemovedWord", delWord},
	} {
		t.Run(tc.key, func(t *testing.T) {
			want, ok := p.Dark[tc.key]
			if !ok {
				t.Fatalf("%s binds Claude key %q, which %s (from %s) does not define",
					tc.what, tc.key, paletteFromUI, p.Source)
			}
			got, ok := tc.style.GetBackground().(lipgloss.AdaptiveColor)
			if !ok {
				t.Fatalf("%s is not an AdaptiveColor, so it cannot answer for both backgrounds", tc.what)
			}
			if got.Dark != want {
				t.Errorf("%s is %q on a dark terminal, want %q (Claude's %s in %s)",
					tc.what, got.Dark, want, tc.key, p.Source)
			}
			if light := p.Light[tc.key]; got.Light != light && light != "" {
				t.Logf("NOTE: %s is %q on light, Claude's %s is %q - Wake brightens the light "+
					"bands deliberately, since Claude's light diffAdded is a mid green that "+
					"black text does not sit on", tc.what, got.Light, tc.key, light)
			}
		})
	}
}

// The header counts exactly the lines the bands draw.
//
// Two walks of the prefix/suffix rule would drift, and the drift is a header
// claiming a different edit from the diff underneath it - which is worse than
// no header, because it is the half somebody reads when the pane is narrow.
func TestTheDiffSummaryCountsWhatTheDiffDraws(t *testing.T) {
	for _, tc := range []struct{ what, old, new, want string }{
		{"an edit in place", "a\nb\nc\n", "a\nB\nc\n", "Added 1 line, removed 1 line"},
		{"a pure addition", "", "x\ny\n", "Added 2 lines"},
		{"a pure deletion", "x\ny\n", "", "Removed 2 lines"},
		{"an append", "a\n", "a\nb\nc\n", "Added 2 lines"},
		{"nothing at all", "a\n", "a\n", ""},
	} {
		t.Run(tc.what, func(t *testing.T) {
			if got := DiffSummary(tc.old, tc.new); got != tc.want {
				t.Errorf("DiffSummary = %q, want %q", got, tc.want)
			}
			// And the count is the number of banded rows Diff actually emits.
			if tc.want == "" {
				return
			}
			del, add := changedSides(tc.old, tc.new)
			rows := 0
			for _, l := range strings.Split(Diff(tc.old, tc.new, 60), "\n") {
				if strings.TrimSpace(l) != "" {
					rows++
				}
			}
			if rows != len(del)+len(add) {
				t.Errorf("the summary counts %d changed lines and Diff drew %d rows",
					len(del)+len(add), rows)
			}
		})
	}
}

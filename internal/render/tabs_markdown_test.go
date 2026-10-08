package render

// No tab reaches the terminal through markdown.
//
// ansi.StringWidth counts a tab as zero cells and a terminal moves to the next
// eight-column stop without erasing the cells it skips, so a row that holds one
// is measured short, drawn long, and shows whatever was there before through
// the gap. These pin the render, not the helper: what Markdown returns has to
// be drawn at the width it was measured at.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// terminalStop is where a terminal's tab stops sit, which is not tabWidth: the
// terminal's eight is why a tab left in a row cannot be measured.
const terminalStop = 8

// terminalWidth is the columns a terminal occupies drawing row from its first
// cell: escapes take none, a tab runs to the next stop, anything else its own
// width.
func terminalWidth(row string) int {
	col := 0
	for _, r := range ansi.Strip(row) {
		if r == '\t' {
			col = (col/terminalStop + 1) * terminalStop
			continue
		}
		col += ansi.StringWidth(string(r))
	}
	return col
}

// tabbedSources are the constructs a tab arrives in: code (the common one, Go is
// tab-indented), prose, an inline span, an item, a heading, and the two entity
// spellings of a tab, which no source fence sees because they carry no tab.
var tabbedSources = map[string]string{
	"a tab-indented fence": "```go\nfunc main() {\n\tx := 1\t// aligned\n\t\treturn\n\t\t\tvar harbor = openHarbor(\"north\", 12)\n}\n```",
	"a tab in prose":       "The harbour\topens at dawn\tand closes at dusk, and a long enough sentence to wrap.",
	"a tab in a code span": "See `a\tb` for the layout, then `\t\tindented` after it.",
	"a tab in a list item": "- one\ttwo\n- three\tfour\n\t- nested\tfive",
	"a tab in a heading":   "# Harbor\tnotes\n\nbody",
	"an entity":            "a&Tab;b a&#9;c a&#x9;d",
}

func TestAMarkdownRowIsDrawnAtTheWidthItIsMeasured(t *testing.T) {
	for name, src := range tabbedSources {
		for _, width := range []int{20, 40, 80} {
			out := Markdown(src, width)
			if out == "" {
				t.Fatalf("%s at %d: nothing rendered", name, width)
			}
			for _, row := range strings.Split(out, "\n") {
				if strings.Contains(row, "\t") {
					t.Errorf("%s at %d: a tab reached the terminal: %q", name, width, stripANSI(row))
				}
				if w := terminalWidth(row); w > width {
					t.Errorf("%s at %d: a row is drawn %d cells wide: %q", name, width, w, stripANSI(row))
				}
			}
		}
	}
}

// A tab inside a code line is content, not indentation: it runs to the line's next
// four-column stop, so the code keeps its shape. A tab reduced to one space - all
// the output fence alone would do - draws `x := 1` flush against its `{`.
func TestATabInsideCodeRunsToItsStop(t *testing.T) {
	out := stripANSI(Markdown("```go\nfunc main() {\n\tx := 1\t// set\n}\n```", 60))
	if want := "    x := 1  // set"; !strings.Contains(out, want) {
		t.Errorf("the code line's tabs did not run to their stops (%q):\n%s", want, out)
	}
}

// A fence's content starts where its fence does: inside a list item or a quote the
// container's prefix is not part of the code, so a tab-indented code line indents
// from the code's own edge, as its four spaces would.
func TestATabInANestedFenceIndentsFromTheCodesEdge(t *testing.T) {
	for _, tc := range []struct{ name, tabbed, spaced string }{
		{"a fence in an ordered item",
			"1. step\n\n   ```go\n   func x() {\n   \tif ok {\n   \t\treturn\n   \t}\n   }\n   ```",
			"1. step\n\n   ```go\n   func x() {\n       if ok {\n           return\n       }\n   }\n   ```"},
		{"a fence in a bullet",
			"- step\n\n  ```\n  a\n  \tb\n  ```",
			"- step\n\n  ```\n  a\n      b\n  ```"},
		{"a fence in a quote",
			"> ```\n> func x() {\n> \treturn\n> }\n> ```",
			"> ```\n> func x() {\n>     return\n> }\n> ```"},
		{"a fence opened on its item's line",
			"1. ```\n   a\n   \tb\n   ```",
			"1. ```\n   a\n       b\n   ```"},
		{"a fence opened on its bullet's line",
			"- ```\n  a\n  \tb\n  ```",
			"- ```\n  a\n      b\n  ```"},
		{"inline code that opens no fence",
			"- step\n\n  ```x``` is code\n\n  ```\n  \tb\n  ```",
			"- step\n\n  ```x``` is code\n\n  ```\n      b\n  ```"},
		{"a line after the fence has closed",
			"1. step\n\n   ```\n   a\n   ```\n\n   \tafter",
			"1. step\n\n   ```\n   a\n   ```\n\n    after"},
		{"a tilde fence",
			"- step\n\n  ~~~\n  a\n  \tb\n  ~~~",
			"- step\n\n  ~~~\n  a\n      b\n  ~~~"},
		{"a longer closer",
			"- step\n\n  ```\n  a\n  ````\n\n  \tafter",
			"- step\n\n  ```\n  a\n  ````\n\n    after"},
		{"a quote's closer",
			"> ```\n> a\n> ```\n>\n> \tafter",
			"> ```\n> a\n> ```\n>\n>   after"},
		{"a marker followed by two spaces",
			"-  ```\n   a\n   \tb\n   ```",
			"-  ```\n   a\n       b\n   ```"},
		{"a quote line with no space after its mark",
			"> ```\n>x\n> \tb\n> ```",
			"> ```\n>x\n>     b\n> ```"},
		{"a fence-shaped line indented past a closer's reach",
			"- a\n\n  ```\n  x\n      ```\n  y\n  ```\n\n  \tafter",
			"- a\n\n  ```\n  x\n      ```\n  y\n  ```\n\n    after"},
		{"an unclosed fence its item ends",
			"1. a\n   ```\n   x\n2. b\n\n   \tcont",
			"1. a\n   ```\n   x\n2. b\n\n    cont"},
		{"two tabs straddling a quote's marker",
			"> ```\n>\t\treturn\n> ```",
			"> ```\n>     return\n> ```"},
		{"a tab straddling a quote's marker",
			"> ```\n>\treturn\n> ```",
			"> ```\n>   return\n> ```"},
		{"a line inside the code that a closer cannot be",
			"- step\n\n  ```\n  ```go\n  \tb\n  ```",
			"- step\n\n  ```\n  ```go\n      b\n  ```"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, width := range []int{40, 80} {
				if got, want := stripANSI(Markdown(tc.tabbed, width)), stripANSI(Markdown(tc.spaced, width)); got != want {
					t.Errorf("width %d: the tabbed code drew differently from its spaces:\n%s\n--- want\n%s", width, got, want)
				}
			}
		})
	}
}

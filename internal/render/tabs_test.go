package render

import (
	"strings"
	"testing"
)

func TestExpandTabs(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"empty", "", ""},
		{"no tab", "plain text\nover two lines", "plain text\nover two lines"},
		{"a leading tab", "\tfoo", "    foo"},
		{"two leading tabs", "\t\tfoo", "        foo"},
		{"to the next stop, not a fixed four", "a\tb", "a   b"},
		{"one cell short of a stop", "abc\td", "abc d"},
		{"on a stop", "abcd\te", "abcd    e"},
		{"several tabs", "a\tb\tc", "a   b   c"},
		{"adjacent tabs", "a\t\tb", "a       b"},
		// A wide rune is two cells, so the tab after it is two short of the
		// stop. Counting runes pads three and draws the column one cell out.
		{"a wide rune before a tab", "世\tx", "世  x"},
		{"a combining mark takes no cell", "é\tx", "é   x"},
		{"an escape takes no cell", "\x1b[31mab\x1b[0m\tc", "\x1b[31mab\x1b[0m  c"},
		// The column is the row's, and a row starts at a newline.
		{"the column resets at a newline", "ab\n\tc", "ab\n    c"},
		{"and again at each one", "abc\n\td\nab\n\te", "abc\n    d\nab\n    e"},
		{"a trailing tab", "a\t", "a   "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExpandTabs(tc.in); got != tc.want {
				t.Errorf("ExpandTabs(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A string with no tab comes back as it came, which is nearly every string:
// the preview calls this per token.
func TestExpandTabsReturnsATabFreeStringUntouched(t *testing.T) {
	s := strings.Repeat("a harbour at dawn\n", 8)
	if allocs := testing.AllocsPerRun(100, func() { _ = ExpandTabs(s) }); allocs != 0 {
		t.Errorf("a tab-free string cost %v allocations, want none", allocs)
	}
}

// CommonMark reads a tab in a block's indentation as the next four-column stop,
// which is the rule ExpandTabs is, so a construct a tab opens renders the way
// its spaces do - and the way glamour renders the raw tab, which is what
// expanding first must not change. Both are asserted: the raw render is the
// claim about CommonMark, the Markdown one is the claim about Wake. Every tab
// here is spent on indentation; one left in a code line's content survives
// glamour as a tab, which is the bug.
func TestATabIndentedConstructRendersAsItsSpacesDo(t *testing.T) {
	for _, tc := range []struct{ name, tabbed, spaced string }{
		{"a nested list", "- a\n\t- b\n\t\t- c\n- d", "- a\n    - b\n        - c\n- d"},
		{"an indented code block", "text\n\n\tcode line\n\tmore code\n", "text\n\n    code line\n    more code\n"},
		{"a quote", "> quoted\n>\tmore", "> quoted\n>   more"},
		{"a code block in a quote", ">\t\tcode in a quote", ">       code in a quote"},
		{"code in a list item", "- item\n\n\t\tcode in the item\n", "- item\n\n        code in the item\n"},
		{"a fence inside an item", "1. step\n\n\t```\n\tgo build\n\t```\n", "1. step\n\n    ```\n    go build\n    ```\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, width := range []int{40, 80} {
				if got, want := Markdown(tc.tabbed, width), Markdown(tc.spaced, width); got != want {
					t.Errorf("width %d: the tabbed source drew differently from its spaces:\n%s\n--- want\n%s", width, stripANSI(got), stripANSI(want))
				}
				if raw, spaced := rawGlamour(t, tc.tabbed, width), rawGlamour(t, tc.spaced, width); raw != spaced {
					t.Errorf("width %d: glamour reads the raw tab differently from the spaces, so S1 changes the structure:\n%s\n--- want\n%s", width, stripANSI(raw), stripANSI(spaced))
				}
			}
		})
	}
}

// rawGlamour is what glamour draws for src as given - none of Markdown's own
// passes, and no tab expansion.
func rawGlamour(t *testing.T, src string, width int) string {
	t.Helper()
	r, err := rendererFor(boundedWidth(width))
	if err != nil {
		t.Fatalf("building a renderer: %v", err)
	}
	out, err := lockAndRender(r, src)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	return out
}

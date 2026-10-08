package render

// Where a streamed answer's finished blocks end. The splitter is a pure function
// of the text, so these are tables: a text goes in, the chunks it cuts and what is
// left open come out - fed whole, and a few bytes at a time the way tokens arrive.

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// scan feeds text to a splitter delta bytes at a time, the way the preview does:
// the open text grows, and every cut it names is taken off the front.
func scan(text string, delta int) (chunks []string, open string, s Splitter) {
	for i := 0; i < len(text); i += delta {
		open += text[i:min(i+delta, len(text))]
		for {
			cut, next := s.Next(open)
			s = next
			if cut == 0 {
				break
			}
			chunks, open = append(chunks, open[:cut]), open[cut:]
		}
	}
	return chunks, open, s
}

// splitAll is scan with the whole text handed over at once - the batch the
// incremental result has to agree with.
func splitAll(text string) ([]string, string) {
	chunks, open, _ := scan(text, max(len(text), 1))
	return chunks, open
}

type splitCase struct {
	name   string
	text   string
	chunks []string
	open   string
}

func runSplitCases(t *testing.T, cases []splitCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			chunks, open := splitAll(c.text)
			if !reflect.DeepEqual(chunks, c.chunks) || open != c.open {
				t.Errorf("%q\n chunks %q open %q\n want   %q open %q", c.text, chunks, open, c.chunks, c.open)
			}
		})
	}
}

func TestABlankLineEndsABlockOnceTheNextOneHasStarted(t *testing.T) {
	runSplitCases(t, []splitCase{
		{"two paragraphs", "one\n\ntwo", []string{"one\n\n"}, "two"},
		{"three", "one\n\ntwo\n\nthree", []string{"one\n\n", "two\n\n"}, "three"},
		{"the next block has not started", "one\n\n", nil, "one\n\n"},
		{"a block with no blank line after it", "one\ntwo", nil, "one\ntwo"},
		{"lines of one paragraph", "one\ntwo\n\nthree", []string{"one\ntwo\n\n"}, "three"},
		{"several blank lines belong to the block above", "one\n\n\n\ntwo", []string{"one\n\n\n\n"}, "two"},
		{"a space-only line is blank", "one\n  \t \ntwo", []string{"one\n  \t \n"}, "two"},
		{"blank lines before anything are not a block", "\n\none\n\ntwo", []string{"\n\none\n\n"}, "two"},
		{"a heading is a block", "# Title\n\nbody", []string{"# Title\n\n"}, "body"},
		{"the next line is one character in", "one\n\nt", []string{"one\n\n"}, "t"},
		{"an indented line goes on with the block above", "one\n\n  two\n\nthree", []string{"one\n\n  two\n\n"}, "three"},
		{"a tab-indented line too", "one\n\n\ttwo\n\nthree", []string{"one\n\n\ttwo\n\n"}, "three"},
	})
}

// A list is cut where its last item ends, never between two items - a loose list
// has blank lines between items and a block cut there would renumber.
func TestAListIsOneChunkLooseOrTight(t *testing.T) {
	runSplitCases(t, []splitCase{
		{"tight", "- a\n- b\n- c\n\nafter", []string{"- a\n- b\n- c\n\n"}, "after"},
		{"loose", "- a\n\n- b\n\n- c\n\nafter", []string{"- a\n\n- b\n\n- c\n\n"}, "after"},
		{"ordered, loose", "1. a\n\n2. b\n\nafter", []string{"1. a\n\n2. b\n\n"}, "after"},
		{"ordered with a paren", "1) a\n\n2) b\n\nafter", []string{"1) a\n\n2) b\n\n"}, "after"},
		{"star and plus markers", "* a\n\n* b\n\n+ c\n\nafter", []string{"* a\n\n* b\n\n+ c\n\n"}, "after"},
		{"an item's own paragraph", "- a\n\n  more\n\n- b\n\nafter", []string{"- a\n\n  more\n\n- b\n\n"}, "after"},
		{"a nested list", "- a\n\n  - b\n\n- c\n\nafter", []string{"- a\n\n  - b\n\n- c\n\n"}, "after"},
		{"a paragraph between two lists is a block of its own", "- a\n\nmid\n\n- b", []string{"- a\n\n", "mid\n\n"}, "- b"},
		{"a list after a paragraph is cut off it", "intro\n\n- a\n- b", []string{"intro\n\n"}, "- a\n- b"},
		{"bold is not a marker", "- a\n\n**b** text\n\nafter", []string{"- a\n\n", "**b** text\n\n"}, "after"},
		{"a dash that is not a marker", "- a\n\n-b\n\nafter", []string{"- a\n\n", "-b\n\n"}, "after"},
		{"a number that is not a marker", "- a\n\n2026 was a year\n\nafter", []string{"- a\n\n", "2026 was a year\n\n"}, "after"},
		{"ten digits are not a marker", "- a\n\n1234567890. no\n\nafter", []string{"- a\n\n", "1234567890. no\n\n"}, "after"},
		{"an empty item is a marker", "- a\n\n-\n\nafter", []string{"- a\n\n-\n\n"}, "after"},
	})
}

// The first characters of the next line decide, and until they are there nothing
// is cut: "-" may be a bullet or the start of "-b".
func TestAListMarkerThatHasNotFinishedArrivingWaits(t *testing.T) {
	for _, c := range []struct {
		open string
		cut  bool
	}{
		{"- a\n\n-", false}, {"- a\n\n- ", false}, {"- a\n\n-b", true},
		{"1. a\n\n2", false}, {"1. a\n\n2.", false}, {"1. a\n\n2. ", false}, {"1. a\n\n2x", true},
		{"1. a\n\n12345678", false}, {"1. a\n\n1234567890", true},
		{"- a\n\nx", true},
		{"one\n\n-", true}, {"one\n\n2", true}, // no list in this block: nothing to continue
		{"one\n\n ", false}, {"one\n\n  x", false}, // an indent: not a cut either way
	} {
		cut, _ := Splitter{}.Next(c.open)
		if (cut > 0) != c.cut {
			t.Errorf("%q cut = %d, want a cut: %v", c.open, cut, c.cut)
		}
	}
}

func TestAFencedBlockIsNeverCutInsideIt(t *testing.T) {
	runSplitCases(t, []splitCase{
		{"backticks", "```go\nfoo\n\nbar\n```\nafter", []string{"```go\nfoo\n\nbar\n```\n"}, "after"},
		{"tildes", "~~~\na\n\nb\n~~~\nafter", []string{"~~~\na\n\nb\n~~~\n"}, "after"},
		{"a four-tick fence holding a three-tick one", "````\n```\ninner\n\nmore\n```\n````\nafter",
			[]string{"````\n```\ninner\n\nmore\n```\n````\n"}, "after"},
		{"a longer closer closes", "```\nx\n\n`````\nafter", []string{"```\nx\n\n`````\n"}, "after"},
		{"a closer of the other character does not", "```\nx\n~~~\n\ny\n```\nafter", []string{"```\nx\n~~~\n\ny\n```\n"}, "after"},
		{"a closer with trailing spaces", "```\nx\n```  \nafter", []string{"```\nx\n```  \n"}, "after"},
		{"a closer with text after it is code", "```\nx\n```y\n\nz\n```\nafter", []string{"```\nx\n```y\n\nz\n```\n"}, "after"},
		{"unclosed", "```\nfoo\n\nbar\n\nbaz", nil, "```\nfoo\n\nbar\n\nbaz"},
		{"unclosed after a paragraph", "intro\n\n```\nfoo\n\nbar", []string{"intro\n\n"}, "```\nfoo\n\nbar"},
		{"the closer's line has not ended", "```\nx\n```", nil, "```\nx\n```"},
		{"a fence is its own block", "text\n\n```\ncode\n```\nafter", []string{"text\n\n", "```\ncode\n```\n"}, "after"},
		{"a fence interrupting a paragraph", "text\n```\ncode\n```\nafter", []string{"text\n```\ncode\n```\n"}, "after"},
		{"backticks in the info string are not a fence", "```a`b\n\nx", []string{"```a`b\n\n"}, "x"},
		{"inline code opening a line is not a fence", "```inline``` text\n\nnext", []string{"```inline``` text\n\n"}, "next"},
		{"a fence in a list item is read through and gives no cut", "1. Run:\n\n   ```sh\n   a\n\n   b\n   ```\n\nnext",
			[]string{"1. Run:\n\n   ```sh\n   a\n\n   b\n   ```\n\n"}, "next"},
		{"an indented fence is read through, blank lines and a line at column 0 included", "intro\n\n  ```\n  code\n\nnot indented\n  ```\n\nafter",
			[]string{"intro\n\n  ```\n  code\n\nnot indented\n  ```\n\n"}, "after"},
		{"a fence after a list ends the block with the list", "- a\n- b\n```\ncode\n```\nafter", []string{"- a\n- b\n```\ncode\n```\n"}, "after"},
	})
}

// CommonMark lets a closer sit 1-3 spaces in. The splitter reads a closer at column
// 0 only, so it stays in the fence and cuts nothing for the rest of the block: the
// answer stays the raw preview it is today. Named here so it is a decision rather
// than a surprise - the other way round, a cut inside a code block, is the failure
// worth never having.
func TestACloserIndentedOneToThreeSpacesLeavesTheBlockRaw(t *testing.T) {
	for _, closer := range []string{" ```", "  ```", "   ```"} {
		text := "intro\n\n```\nx\n\ny\n" + closer + "\n\nafter\n\nlast"
		chunks, open := splitAll(text)
		if !reflect.DeepEqual(chunks, []string{"intro\n\n"}) || open != "```\nx\n\ny\n"+closer+"\n\nafter\n\nlast" {
			t.Errorf("closer %q: chunks %q open %q, want only the paragraph above the fence cut", closer, chunks, open)
		}
	}
}

// What the splitter reads is the text so far, so the cuts must not depend on how
// the text was handed over.
func TestTheIncrementalScanAgreesWithTheBatchAtAnyDeltaSize(t *testing.T) {
	doc := "# Title\n\nSome **bold** prose.\nIt runs two lines.\n\n- one\n\n- two\n  - nested\n\n- three\n\n" +
		"```go\nfunc main() {\n\n\tprintln(1)\n}\n```\nafter the fence\n\n1. first\n2. second\n\n10. tenth\n\n" +
		"> quoted\n\n~~~\ntilde\n\n~~~\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\ntrailing words"
	// The answer past MaxChunk is the case a batch read of everything unread would
	// freeze at once while the stream cut paragraph by paragraph.
	long := ""
	for i := 0; len(long) <= 2*MaxChunk; i++ {
		long += fmt.Sprintf("Paragraph %03d says a few ordinary words about the harbor parser.\n\n", i)
	}
	for _, doc := range []string{doc, long} {
		want, wantOpen := splitAll(doc)
		if len(want) < 8 {
			t.Fatalf("the fixture cuts only %d chunks, too few to tell a delta-dependent scan from a steady one", len(want))
		}
		for _, delta := range []int{1, 2, 3, 5, 11} {
			got, open, _ := scan(doc, delta)
			if !reflect.DeepEqual(got, want) || open != wantOpen {
				t.Errorf("%d bytes by %d: cut %d chunks (open %q), the batch cut %d (open %q)", len(doc), delta, len(got), open, len(want), wantOpen)
			}
		}
	}
}

// A line opening with `<` may start an HTML block, which the splitter cannot read
// the end of - a <pre> runs through blank lines - so cutting stops for the rest of
// the block and the block stays the raw preview it is today.
func TestHTMLAndOversizeFreezeToRaw(t *testing.T) {
	t.Run("html", func(t *testing.T) {
		for _, c := range []struct {
			text, chunk string // what was cut before the HTML, "" for nothing
		}{
			{"one\n\n<div>\n\nbody\n\nmore", ""},
			{"one\n  <pre>\nbody\n\nmore\n\nlast", ""},
			{"<details>\n\nbody\n\nmore", ""},
			{"first\n\nsecond\n\n<div>\n\nbody\n\nmore", "first\n\n"},
		} {
			for _, delta := range []int{1, 3, len(c.text)} {
				chunks, open, s := scan(c.text, delta)
				var want []string
				if c.chunk != "" {
					want = []string{c.chunk}
				}
				if !reflect.DeepEqual(chunks, want) || open != strings.TrimPrefix(c.text, c.chunk) || !s.Frozen() {
					t.Errorf("%q by %d: chunks %q open %q frozen %v, want only %q cut and the splitter frozen", c.text, delta, chunks, open, s.Frozen(), c.chunk)
				}
			}
		}
		// Frozen is for the rest of the block: later text cuts nothing.
		_, _, s := scan("one\n\n<div>\n\nbody", 4)
		if cut, next := s.Next("one\n\n<div>\n\nbody\n\nmore\n\nlast"); cut != 0 || !next.Frozen() {
			t.Errorf("a frozen splitter cut at %d (frozen %v)", cut, next.Frozen())
		}
	})

	t.Run("a chunk past the bound", func(t *testing.T) {
		if MaxChunk != 4096 {
			t.Fatalf("MaxChunk is %d, the ruling says 4 KiB", MaxChunk)
		}
		// One line and the blank after it: a block of exactly MaxChunk bytes is cut,
		// one byte more is not.
		atBound := strings.Repeat("a", MaxChunk-2) + "\n\n"
		if chunks, _, _ := scan(atBound+"next", 7); len(chunks) != 1 || chunks[0] != atBound {
			t.Errorf("a block of exactly %d bytes was not cut: %d chunks", MaxChunk, len(chunks))
		}
		over := strings.Repeat("a", MaxChunk-1) + "\n\n"
		chunks, open, s := scan(over+"next\n\nlast", 7)
		if len(chunks) != 0 || !s.Frozen() || open != over+"next\n\nlast" {
			t.Errorf("a block of %d bytes: %d chunks, frozen %v, want no cut and frozen", len(over), len(chunks), s.Frozen())
		}
		many := strings.Repeat(strings.Repeat("a", 63)+"\n", MaxChunk/64+1) + "\nnext"
		if chunks, _, s := scan(many, 5); len(chunks) != 0 || !s.Frozen() {
			t.Errorf("lines adding up past the bound: %d chunks, frozen %v, want none and frozen", len(chunks), s.Frozen())
		}
	})

	t.Run("one line that never ends", func(t *testing.T) {
		_, _, s := scan(strings.Repeat("word ", MaxChunk), 97)
		if !s.Frozen() {
			t.Error("a 20 KB line did not freeze the splitter")
		}
	})
}

// The cuts are positions in what has been read, so a splitter that has cut starts
// afresh on the remainder: a list in the block before does not make "- x" after a
// paragraph a continuation.
func TestACutLeavesTheSplitterReadyForTheRemainder(t *testing.T) {
	chunks, open, _ := scan("- a\n- b\n\nmid\n\n- c\n\n- d\n\nend\n\nlast", 1)
	want := []string{"- a\n- b\n\n", "mid\n\n", "- c\n\n- d\n\n", "end\n\n"}
	if !reflect.DeepEqual(chunks, want) || open != "last" {
		t.Errorf("chunks %q open %q, want %q open \"last\"", chunks, open, want)
	}
}

// One blank row between two blocks, as Markdown draws them. A blank row the block
// above ends in is that block's own and takes the separator too; a horizontal
// rule's trailing row is the separator.
func TestStackSeparatesBlocksByOneBlankRow(t *testing.T) {
	for _, c := range []struct{ above, rows, want []string }{
		{nil, []string{"a"}, []string{"a"}},
		{[]string{"a"}, []string{"b"}, []string{"a", "", "b"}},
		{[]string{"a", ""}, []string{"b"}, []string{"a", "", "", "b"}},
		{[]string{"  ─────", ""}, []string{"b"}, []string{"  ─────", "", "b"}},
		{[]string{"  ─────", "  "}, []string{"b"}, []string{"  ─────", "  ", "b"}},
		{[]string{"a"}, nil, []string{"a"}},
	} {
		if got := Stack(c.above, c.rows); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Stack(%q, %q) = %q, want %q", c.above, c.rows, got, c.want)
		}
	}
	above := make([]string, 1, 8)
	above[0] = "a"
	_ = Stack(above, []string{"b"})
	if above = above[:2]; above[1] != "" {
		t.Errorf("Stack wrote into the slice it was given: %q", above)
	}
}

package render

// The property the whole preview rests on: a block rendered on its own, under the
// ones above it, draws the rows the whole answer draws. If it does not, a finished
// paragraph is drawn one way while the answer streams and another when it lands.
// Held against what agents actually wrote (the committed corpus, decoded through
// core as every other corpus test here is) and against a seeded grammar of
// everything markdown puts between two blank lines, whole and a token at a time.

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// corpusWidths are a narrow pane, the idle terminal's conversation pane and a wide one.
var corpusWidths = []int{40, 79, 120}

// plainRows is what a reader sees of a render, row by row: colours are chroma's
// and vary from run to run, and the padding after a row's last glyph is invisible.
func plainRows(s string) []string {
	rows := strings.Split(s, "\n")
	for i, r := range rows {
		rows[i] = strings.TrimRight(ansi.Strip(r), " ")
	}
	return rows
}

// stitched is the answer as the preview draws it: every finished block rendered
// once, one after another, and the open remainder after them.
func stitched(text string, width int) []string {
	chunks, open := splitAll(text)
	var rows []string
	for _, piece := range append(chunks, open) {
		if out := Markdown(piece, width); out != "" {
			rows = Stack(rows, strings.Split(out, "\n"))
		}
	}
	return rows
}

// documentWide is what resolves across the whole document, which a block rendered
// alone cannot see: a reference-style link definition and a footnote definition are
// found wherever they sit, so a use above them renders differently on its own.
// Transient by construction - the answer lands whole and is corrected then - and
// excluded here by name rather than by luck.
var documentWide = regexp.MustCompile(`(?m)^ {0,3}\[\^?[^\]\n]+\]:`)

// divergence is the first row two renders disagree on, or "" for none.
func divergence(whole, got []string) string {
	for i := 0; i < max(len(whole), len(got)); i++ {
		var a, b string
		if i < len(whole) {
			a = whole[i]
		}
		if i < len(got) {
			b = got[i]
		}
		if a != b {
			return fmt.Sprintf("row %d: whole %q, blocks %q (%d rows vs %d)", i, a, b, len(whole), len(got))
		}
	}
	return ""
}

// sameCuts holds the cuts to being a function of the text and not of how its
// tokens arrived: the batch scan never reaches a decision that waits for more
// characters, and a stream does at every other character.
func sameCuts(t *testing.T, name, doc string) (bad int) {
	t.Helper()
	want, wantOpen := splitAll(doc)
	for _, delta := range []int{1, 3} {
		if got, open, _ := scan(doc, delta); !reflect.DeepEqual(got, want) || open != wantOpen {
			bad++
			if bad <= 3 {
				t.Errorf("%s by %d bytes cut %d blocks, the whole cut %d\n--- source ---\n%s", name, delta, len(got), len(want), doc)
			}
		}
	}
	return bad
}

// checkSplit holds one document to the property at every width and returns how
// many widths disagreed, and how many delta sizes cut it differently.
func checkSplit(t *testing.T, name, doc string) (bad int) {
	t.Helper()
	bad = sameCuts(t, name, doc)
	for _, w := range corpusWidths {
		whole := plainRows(Markdown(doc, w))
		if d := divergence(whole, plainRows(strings.Join(stitched(doc, w), "\n"))); d != "" {
			bad++
			if bad <= 3 {
				t.Errorf("%s at width %d: %s\n--- source ---\n%s", name, w, d, doc)
			}
		}
	}
	return bad
}

func TestEveryChunkSplitRendersAsTheWholeDoes(t *testing.T) {
	t.Run("corpus", func(t *testing.T) {
		docs := corpusTexts(t)
		var bad, excluded, split int
		for name, doc := range docs {
			if documentWide.MatchString(doc) {
				excluded++
				continue
			}
			if chunks, _ := splitAll(doc); len(chunks) > 0 {
				split++
			}
			bad += checkSplit(t, name, doc)
		}
		t.Logf("corpus: %d answers, %d cut into blocks, %d held out for a document-wide definition, %d diverging widths",
			len(docs), split, excluded, bad)
		if split < 20 {
			t.Fatalf("only %d recorded answers split into blocks: the corpus no longer exercises the property", split)
		}
	})

	t.Run("grammar", func(t *testing.T) {
		const answers = 300
		r := rand.New(rand.NewSource(20261007))
		var bad, split int
		for i := range answers {
			doc := fuzzAnswer(r)
			if chunks, _ := splitAll(doc); len(chunks) > 0 {
				split++
			}
			bad += checkSplit(t, fmt.Sprintf("grammar answer %d", i), doc)
		}
		t.Logf("grammar: %d answers, %d cut into blocks, %d diverging widths", answers, split, bad)
		if split < answers/2 {
			t.Fatalf("only %d of %d generated answers split into blocks", split, answers)
		}
	})
}

// corpusTexts is every distinct assistant block in the recordings, by where it came from.
func corpusTexts(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	seen := map[string]bool{}
	for _, c := range []struct {
		glob   string
		decode func([]byte) ([]core.Event, error)
	}{
		{"../../testdata/stream/*.jsonl", core.DecodeLine},
		{"../../testdata/transcript/*.jsonl", core.DecodeTranscriptLine},
	} {
		files, err := filepath.Glob(c.glob)
		if err != nil || len(files) == 0 {
			t.Fatalf("no fixtures under %s (%v): every assertion here would pass over nothing", c.glob, err)
		}
		for _, path := range files {
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			for n, line := range strings.Split(string(body), "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				evs, err := c.decode([]byte(line))
				if err != nil {
					continue // a recorded stderr line or a frame this build does not decode
				}
				for _, ev := range evs {
					if ev.Kind != core.KindAssistantText || ev.Subagent != nil || strings.TrimSpace(ev.Text) == "" || seen[ev.Text] {
						continue
					}
					seen[ev.Text] = true
					out[fmt.Sprintf("%s:%d", filepath.Base(path), n+1)] = ev.Text
				}
			}
		}
	}
	if len(out) < 50 {
		t.Fatalf("the corpus decodes to %d assistant blocks, too few to mean anything", len(out))
	}
	return out
}

// --- the grammar ----------------------------------------------------------

var fuzzWords = []string{
	"wake", "agent", "the", "session", "park", "resume", "and", "fix", "retry", "header",
	"`code`", "**bold**", "*em*", "[link](https://example.com/a)", "DEV-3035", "end-to-end",
	"--resume", "2026-10-07", "parent", "transcript", "stays", "first", "load", "tools",
}

func fuzzSentence(r *rand.Rand, n int) string {
	parts := make([]string, 3+r.Intn(n))
	for i := range parts {
		parts[i] = fuzzWords[r.Intn(len(fuzzWords))]
	}
	return strings.Join(parts, " ") + "."
}

func fuzzItems(r *rand.Rand, marker func(int) string, loose bool) string {
	var b strings.Builder
	for i := range 2 + r.Intn(3) {
		b.WriteString(marker(i) + fuzzSentence(r, 8) + "\n")
		switch r.Intn(5) {
		case 0:
			b.WriteString("  - " + fuzzSentence(r, 5) + "\n")
		case 1:
			b.WriteString("\n  " + fuzzSentence(r, 6) + "\n")
		case 2:
			b.WriteString("  ```sh\n  " + fuzzWords[r.Intn(len(fuzzWords))] + "\n\n  more\n  ```\n")
		}
		if loose {
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func fuzzFence(r *rand.Rand) string {
	tick := []string{"```", "~~~", "````"}[r.Intn(3)]
	info := []string{"", "go", "sh", "diff", "text"}[r.Intn(5)]
	lines := make([]string, 1+r.Intn(5))
	for i := range lines {
		switch r.Intn(4) {
		case 0:
			lines[i] = ""
		case 1:
			lines[i] = "    " + fuzzWords[r.Intn(len(fuzzWords))]
		case 2:
			if tick == "````" {
				lines[i] = "```" // a shorter fence inside a longer one is code
			} else {
				lines[i] = "# " + fuzzWords[r.Intn(len(fuzzWords))]
			}
		default:
			lines[i] = fuzzWords[r.Intn(len(fuzzWords))] + " " + fuzzWords[r.Intn(len(fuzzWords))]
		}
	}
	return tick + info + "\n" + strings.Join(lines, "\n") + "\n" + tick
}

// fuzzOddities are blocks an agent writes less often, each a way two blocks can
// join or part that the common ones do not reach: setext headings, rules in three
// spellings, task lists, quotes holding lists and code, a hard break, a fence
// indented under no list, an HTML comment and an autolink (which freeze the
// splitter) and a list item holding indented code.
var fuzzOddities = []string{
	"Setext title\n============", "Setext two\n----------", "* * *", "___",
	"- [ ] todo item\n- [x] done item",
	"> - quoted list\n> - second\n>\n> after list in quote",
	"> ```\n> code in quote\n>\n> still\n> ```",
	"line with hard break  \nnext line",
	"a | b\n--|--\n1 | 2",
	"1. a\n   - b\n   - c\n2. d",
	"<!-- comment -->\n\nafter the comment", "<https://example.com/x> autolink at the start",
	"  ```\n  indented fence\n\n  more\n  ```",
	"   ~~~\n   three-space fence\n\n   more\n   ~~~",
	"- item\n\n      indented code in item\n\n- next",
	"1. one\n\n   ```\n   code\n   ```\n\n2. two",
	"***bold italic*** and ~~strike~~ text",
	"> quote\n\n> second quote", "> quote\nlazy continuation",
	"- a\n-\n- c", "2. starts at two\n3. three", "0. zero\n1. one", "+ plus\n+ list",
	"Term\n: its definition", "Term\n\n: its definition", "Term\n: one\n: two",
	"Term\n: its definition\n\nAnother\n: and its own",
}

// fuzzBlock is one block of the kinds an agent's answer holds.
func fuzzBlock(r *rand.Rand) string {
	switch r.Intn(15) {
	case 13, 14:
		return fuzzOddities[r.Intn(len(fuzzOddities))]
	case 0:
		return "# " + fuzzSentence(r, 3)
	case 1:
		return "### " + fuzzSentence(r, 3)
	case 2:
		return fuzzItems(r, func(int) string { return "- " }, r.Intn(2) == 0)
	case 3:
		return fuzzItems(r, func(i int) string { return fmt.Sprintf("%d. ", i+1) }, r.Intn(2) == 0)
	case 4:
		return fuzzFence(r)
	case 5:
		return "> " + fuzzSentence(r, 8) + "\n> " + fuzzSentence(r, 6)
	case 6:
		return "| a | b |\n|---|---|\n| " + fuzzWords[r.Intn(len(fuzzWords))] + " | 2 |"
	case 7:
		return "---"
	case 8:
		return "    " + fuzzWords[r.Intn(len(fuzzWords))] + "\n    indented code"
	case 9:
		return fmt.Sprintf("%d. %s", 2026-r.Intn(3), fuzzSentence(r, 4)) // a number that opens a line
	case 10:
		return fuzzSentence(r, 10) + "\n" + fuzzSentence(r, 10)
	default:
		return fuzzSentence(r, 30)
	}
}

func fuzzAnswer(r *rand.Rand) string {
	blocks := make([]string, 2+r.Intn(7))
	for i := range blocks {
		blocks[i] = fuzzBlock(r)
	}
	gap := []string{"\n\n", "\n\n", "\n\n\n", "\n"}
	var b strings.Builder
	for i, blk := range blocks {
		if i > 0 {
			b.WriteString(gap[r.Intn(len(gap))])
		}
		b.WriteString(blk)
	}
	return b.String()
}

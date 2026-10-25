package render

// A table too narrow for its pane is drawn as records, one line per column, the
// way Claude Code draws one on a narrow terminal. The behaviour matched is in
// docs/superpowers/notes/2026-10-24-narrow-table-findings.md, the rule Wake
// switches by in docs/notes/decisions.md (2026-10-24).

import (
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// maxRowLines is the most lines a grid's row may take before its table is drawn
// as records, and recordRuleWidth the widest the rule between two records gets.
// Both are matched against Claude Code, maintained by hand.
const (
	maxRowLines     = 4
	recordRuleWidth = 40
)

// placeholderMark brackets the paragraph a table stands in as while the document
// around it renders. A private-use rune, and a source holding one is left whole.
const placeholderMark = "\uE000"

func placeholder(i int) string { return placeholderMark + strconv.Itoa(i) + placeholderMark }

// tableParser reads a source the way glamour's own goldmark does, so a table
// here is a table there.
var tableParser = sync.OnceValue(func() parser.Parser {
	return goldmark.New(goldmark.WithExtensions(extension.GFM, extension.DefinitionList)).Parser()
})

// table is one top-level table: its lines in the source, its header's text, each
// body row's cells as markdown, and the addresses of its links.
type table struct {
	start, end int
	text       string
	labels     []string
	rows       [][]string
	links      []string
}

// withTables renders src with each top-level table drawn alone in its place, as
// glamour's grid while that shows the table whole, and as records otherwise or
// when records is set. A source with no table, or one holding the placeholder's
// mark, is one render, as before.
func withTables(src string, width int, records bool) string {
	if strings.Contains(src, placeholderMark) {
		return rendered(src, width)
	}
	tables := tablesIn(src)
	if len(tables) == 0 {
		return rendered(src, width)
	}
	var doc strings.Builder
	at := 0
	for i, t := range tables {
		// A blank line each side, or the placeholder would continue a paragraph.
		doc.WriteString(src[at:t.start] + "\n" + placeholder(i) + "\n\n")
		at = t.end
	}
	doc.WriteString(src[at:])
	rows := strings.Split(rendered(doc.String(), width), "\n")
	out := make([]string, 0, len(rows))
	next := 0
	for _, row := range rows {
		if next < len(tables) && strings.TrimSpace(ansi.Strip(row)) == placeholder(next) {
			out = append(out, tables[next].drawn(width, records)...)
			next++
			continue
		}
		out = append(out, row)
	}
	return strings.Join(out, "\n")
}

// drawn is the table's rows at width.
func (t table) drawn(width int, records bool) []string {
	if !records {
		if grid := renderedWith(t.text, width, reflowedBelowGrid); t.whole(grid) {
			return strings.Split(grid, "\n")
		}
	}
	return t.records(width)
}

// tablesIn is src's top-level tables with a body. A table inside a list item or
// a quote, and every table of a source that defines a link reference (one drawn
// alone would lose it), are left to glamour (deferred.md).
func tablesIn(src string) []table {
	if !hasDelimiterRow(src) {
		return nil
	}
	source := []byte(src)
	doc, refs := parsed(source)
	if refs {
		return nil
	}
	var out []table
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		if tn, ok := n.(*east.Table); ok && tn.ChildCount() > 1 {
			if t, ok := tableOf(tn, source); ok {
				out = append(out, t)
			}
		}
	}
	return out
}

// parsed is source's tree and whether it defines a link reference. goldmark
// promises nothing about concurrent use, so mu serializes it with every render.
func parsed(source []byte) (ast.Node, bool) {
	mu.Lock()
	defer mu.Unlock()
	ctx := parser.NewContext()
	doc := tableParser().Parse(text.NewReader(source), parser.WithContext(ctx))
	return doc, len(ctx.References()) > 0
}

// hasDelimiterRow is the cheap test before a parse: a table's second line.
func hasDelimiterRow(src string) bool {
	for line := range strings.Lines(src) {
		if delimiterRow(line) {
			return true
		}
	}
	return false
}

// delimiterRow reports a line of only pipes, dashes, colons and blanks that
// holds a pipe and a dash.
func delimiterRow(line string) bool {
	t := strings.TrimSpace(line)
	return strings.Contains(t, "|") && strings.Contains(t, "-") && strings.Trim(t, "|-: \t") == ""
}

// tableOf reads one table node: its header's text, its body cells' markdown,
// and its lines. goldmark reads a table one line per row - the header, the
// delimiter row under it, then each body row - so the lines are counted from the
// first cell holding a segment, and the second must be the delimiter row. A
// table with no such cell is glamour's.
func tableOf(tn *east.Table, source []byte) (table, bool) {
	t := table{links: linksIn(tn, source)}
	anchor, line := -1, 0
	for row, i := tn.FirstChild(), 0; row != nil; row, i = row.NextSibling(), i+1 {
		cells, at := cellsOf(row, source)
		if anchor < 0 && at >= 0 {
			anchor, line = at, i
			if i > 0 {
				line++ // the delimiter row sits between the header and the body
			}
		}
		if i == 0 {
			for c := row.FirstChild(); c != nil; c = c.NextSibling() {
				t.labels = append(t.labels, plainText(c, source))
			}
			continue
		}
		t.rows = append(t.rows, cells)
	}
	if anchor < 0 {
		return table{}, false
	}
	src := string(source)
	t.start = strings.LastIndexByte(src[:anchor], '\n') + 1
	for range line {
		t.start = strings.LastIndexByte(src[:max(t.start-1, 0)], '\n') + 1
	}
	t.end = t.start
	for range 2 + len(t.rows) {
		nl := strings.IndexByte(src[t.end:], '\n')
		if nl < 0 {
			t.end = len(src)
			break
		}
		t.end += nl + 1
	}
	t.text = src[t.start:t.end]
	lines := strings.SplitN(t.text, "\n", 3)
	return t, len(lines) > 1 && delimiterRow(lines[1])
}

// cellsOf is a row's cells as markdown, an escaped pipe made plain (outside a
// table it is just a pipe), and where its first cell holding a segment starts,
// or -1. A cell goldmark padded the row with holds none.
func cellsOf(row ast.Node, source []byte) ([]string, int) {
	at := -1
	var cells []string
	for c := row.FirstChild(); c != nil; c = c.NextSibling() {
		cell := ""
		if lines := c.Lines(); lines.Len() > 0 {
			seg := lines.At(0)
			cell = strings.ReplaceAll(string(seg.Value(source)), `\|`, "|")
			if at < 0 {
				at = seg.Start
			}
		}
		cells = append(cells, cell)
	}
	return cells, at
}

// plainText is the text a cell shows, its markup gone: escapes and entities
// resolved outside code, as goldmark's own writer resolves them.
func plainText(n ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch c := c.(type) {
		case *ast.Text:
			v := c.Segment.Value(source)
			if _, code := c.Parent().(*ast.CodeSpan); !code {
				v = util.UnescapePunctuations(util.ResolveEntityNames(util.ResolveNumericReferences(v)))
			}
			b.Write(v)
		case *ast.String:
			b.Write(c.Value)
		case *ast.AutoLink:
			b.Write(c.Label(source))
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(b.String())
}

// linksIn is the address of every link and image under n, an autolink's as it
// was typed: glamour lists each under a grid, and may add a scheme to a bare one.
func linksIn(n ast.Node, source []byte) []string {
	var out []string
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch c := c.(type) {
		case *ast.Link:
			out = append(out, string(c.Destination))
		case *ast.Image:
			out = append(out, string(c.Destination))
		case *ast.AutoLink:
			out = append(out, string(c.Label(source)))
		}
		return ast.WalkContinue, nil
	})
	return out
}

// records is the table as Claude Code draws it on a narrow terminal: every body
// row a record, every column a `**Header:** cell` line, a rule between records.
// Each line is its own paragraph, so it wraps back to the margin; the blank rows
// between them go.
func (t table) records(width int) []string {
	var b strings.Builder
	for i, row := range t.rows {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		for c, label := range t.labels {
			if c > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString("**" + escaped(label) + ":** ")
			if c < len(row) {
				b.WriteString(row[c])
			}
		}
	}
	rule := strings.Repeat("─", min(recordRuleWidth, proseBudget(width, int(defaultMargin))))
	var out []string
	for _, row := range strings.Split(rendered(b.String(), width), "\n") {
		switch {
		case blankRow(row):
		case strings.TrimSpace(ansi.Strip(row)) == horizontalRule:
			out = append(out, strings.Replace(trimRightCells(row), horizontalRule, rule, 1))
		default:
			out = append(out, row)
		}
	}
	return out
}

// escaped is text that glamour draws as itself: a backslash before each mark
// glamour unescapes (any other keeps its backslash on screen), and `&` and `~`
// as the entities it decodes, so no entity or strikethrough forms.
var escaped = strings.NewReplacer(
	"\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "{", "\\{", "}", "\\}",
	"[", "\\[", "]", "\\]", "<", "\\<", ">", "\\>", "(", "\\(", ")", "\\)",
	"#", "\\#", "+", "\\+", "-", "\\-", ".", "\\.", "!", "\\!", "|", "\\|",
	"&", "&amp;", "~", "&#126;",
).Replace

// whole reports whether glamour's grid shows the table whole: the header uncut,
// every word of every cell in its place, no row past maxRowLines lines, and
// every link's address uncut in the list under it - measured against the same
// table rendered where nothing wraps. A grid that cannot be read back that way
// is glamour's to draw.
func (t table) whole(grid string) bool {
	plain := ansi.Strip(grid)
	for _, link := range t.links {
		if !strings.Contains(plain, link) {
			return false
		}
	}
	w := t.naturalWidth()
	if w > maxUnwrappedWidth {
		return false // a cell no pane holds
	}
	want, ok := t.natural(w)
	if !ok {
		return true
	}
	lines, ok := gridCells(grid, len(t.labels))
	return ok && keepsText(lines, want)
}

// naturalWidth is a first guess at the width the table draws at unwrapped: its
// cells' source, at least as wide as what they show, and a grid's spacing.
func (t table) naturalWidth() int {
	widest := make([]int, len(t.labels))
	for _, row := range append([][]string{t.labels}, t.rows...) {
		for c := range min(len(row), len(widest)) {
			widest[c] = max(widest[c], ansi.StringWidth(row[c]))
		}
	}
	w := 2 * int(defaultMargin)
	for _, n := range widest {
		w += n + 3 // a column's own blank each side and its divider
	}
	return w
}

// natural is the table's grid read back with nothing wrapped: its header's cells,
// then each body row's. The width doubles from w while a row still wraps,
// through renderers built here, since a cached one per width would evict those
// a frame draws with. False when the grid cannot be read back at all.
func (t table) natural(w int) ([][]string, bool) {
	for ; w <= maxUnwrappedWidth; w *= 2 {
		r, err := newRenderer(rendererOptions(resolvedStyle(), w)...)
		if err != nil {
			return nil, false // the render the grid came from would have failed too, and said so
		}
		out, err := lockAndRender(r, t.text)
		if err != nil {
			return nil, false
		}
		cells, ok := gridCells(out, len(t.labels))
		if !ok || len(cells) < len(t.rows)+1 {
			return nil, false
		}
		if len(cells) == len(t.rows)+1 {
			return cells, true
		}
	}
	return nil, false
}

// gridCells reads glamour's grid back: one entry per drawn line, the header's
// and then each body line's, every cell trimmed. False when the rows are not a
// grid of cols columns under a header rule.
func gridCells(grid string, cols int) ([][]string, bool) {
	var lines []string
	for _, row := range strings.Split(ansi.Strip(grid), "\n") {
		if strings.TrimSpace(row) == "" {
			if len(lines) > 0 {
				break // the table ends at its first blank row; links follow it
			}
			continue
		}
		lines = append(lines, row)
	}
	if len(lines) < 2 || strings.Trim(lines[1], " ─┼") != "" {
		return nil, false
	}
	cells := make([][]string, 0, len(lines)-1)
	for i, line := range lines {
		if i == 1 {
			continue
		}
		parts := strings.Split(line, "│")
		if len(parts) != cols {
			return nil, false
		}
		for k := range parts {
			parts[k] = strings.TrimSpace(parts[k])
		}
		cells = append(cells, parts)
	}
	return cells, true
}

// keepsText reports whether a grid's lines show want - the natural grid's cells,
// header first - whole: the same header, each body cell's words whole and in
// order on the lines of its row, and no row past maxRowLines. A row starts on
// the line after every cell of the one above is done.
func keepsText(lines, want [][]string) bool {
	if len(lines) == 0 || !slices.Equal(lines[0], want[0]) {
		return false
	}
	r, height, rest := 1, 0, wordsOf(want[1])
	for _, line := range lines[1:] {
		if height > 0 && done(rest) {
			if r++; r == len(want) {
				return false
			}
			height, rest = 0, wordsOf(want[r])
		}
		var ok bool
		if height++; height > maxRowLines {
			return false
		}
		if rest, ok = drawnFrom(rest, line); !ok {
			return false
		}
	}
	return r == len(want)-1 && done(rest)
}

// drawnFrom is each cell's words still to come once line is drawn, or false
// when a word on it is not the next its cell holds.
func drawnFrom(rest [][]string, line []string) ([][]string, bool) {
	out := make([][]string, len(rest))
	for c, cell := range line {
		words := rest[c]
		for _, word := range strings.Fields(cell) {
			if len(words) == 0 || words[0] != word {
				return nil, false
			}
			words = words[1:]
		}
		out[c] = words
	}
	return out, true
}

// wordsOf is each cell's words.
func wordsOf(cells []string) [][]string {
	out := make([][]string, len(cells))
	for c, cell := range cells {
		out[c] = strings.Fields(cell)
	}
	return out
}

// done reports every cell's words drawn.
func done(rest [][]string) bool {
	for _, words := range rest {
		if len(words) > 0 {
			return false
		}
	}
	return true
}

// reflowedBelowGrid is reflowProse for a table rendered alone: its grid - the
// rows from the first down to the next blank one - stays as glamour drew it, and
// what follows (glamour's list of the table's links) is prose as ever. A
// one-column grid has no `│` to keep reflowProse off its rows, which would
// otherwise run together.
func reflowedBelowGrid(s string, width int) string {
	lines := strings.Split(s, "\n")
	i := 0
	for i < len(lines) && blankRow(lines[i]) {
		i++
	}
	for i < len(lines) && !blankRow(lines[i]) {
		i++
	}
	if i == len(lines) {
		return s
	}
	return strings.Join(lines[:i], "\n") + "\n" + reflowProse(strings.Join(lines[i:], "\n"), width)
}

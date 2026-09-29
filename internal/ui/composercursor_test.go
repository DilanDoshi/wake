package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// caretTestDraft is a composer's drawn draft as composerRegion captures it at a
// press: the box's interior only, dropping both borders so there is one row per
// drawn draft row, with where each begins in the draft.
func caretTestDraft(c Composer, width int) drawnDraft {
	rows := strings.Split(c.box(width), "\n")[1 : 1+c.ta.Height()]
	return drawnDraft{rows, c.drawnRowStarts(len(rows))}
}

// A click lands the caret onto the clicked character - the block cursor sits on
// it, which an inserted marker before it proves. The marker is the only
// unambiguous read of where the caret went.
func TestCaretAtPointOnASingleLineDraft(t *testing.T) {
	width := 30
	c := NewComposer().SetWidth(width)
	c.ta.InsertString("hello world")
	c = c.fit()

	c = c.caretAtPoint(point{0, 6}, caretTestDraft(c, width), width) // the 'w'
	c.ta.InsertRune('|')
	if got := c.ta.Value(); got != "hello |world" {
		t.Errorf("a click on the 'w' placed the caret so a marker landed %q, want %q", got, "hello |world")
	}
}

// A click on the second line moves the caret down a row and into that line, not
// the first - the row navigation, done through the text area's own wrap.
func TestCaretAtPointOnASecondLine(t *testing.T) {
	width := 30
	c := NewComposer().SetWidth(width)
	c.ta.InsertString("hello\nworld")
	c = c.fit()

	c = c.caretAtPoint(point{1, 2}, caretTestDraft(c, width), width) // "wo|rld"
	c.ta.InsertRune('|')
	if got := c.ta.Value(); got != "hello\nwo|rld" {
		t.Errorf("a click on row 1 col 2 placed the caret so a marker landed %q, want %q", got, "hello\nwo|rld")
	}
}

// A click at column 0 lands the caret at the row's start, and a click past the
// end clamps to the end of that row's text - both stay within the typed
// characters. A fresh composer per click, because bubbles shares the draft
// buffer by pointer, so a marker inserted into one copy would change the other.
func TestCaretAtPointClampsToTheRowsText(t *testing.T) {
	width := 30
	clickMarker := func(col int) string {
		c := NewComposer().SetWidth(width)
		c.ta.InsertString("hi")
		c = c.fit()
		c = c.caretAtPoint(point{0, col}, caretTestDraft(c, width), width)
		c.ta.InsertRune('|')
		return c.ta.Value()
	}

	if got := clickMarker(0); got != "|hi" {
		t.Errorf("a click at column 0 landed the marker %q, want %q", got, "|hi")
	}
	if got := clickMarker(40); got != "hi|" { // far past the two characters
		t.Errorf("a click well past the text landed the marker %q, want %q", got, "hi|")
	}
}

// A draft taller than the box has scrolled inside it, so drawn row 0 is not the
// draft's first row. Three lines in a two-row box is the tightest case - one row
// off screen - and a click on the top drawn row lands on the second line.
func TestCaretAtPointInAScrolledDraft(t *testing.T) {
	width := 30
	c := NewComposer().SetWidth(width).WithMaxRows(2)
	c.ta.InsertString("l1\nl2\nl3")
	c = c.fit().reposition()

	c = c.caretAtPoint(point{0, 0}, caretTestDraft(c, width), width)
	c.ta.InsertRune('|')
	if got := c.ta.Value(); got != "l1\n|l2\nl3" {
		t.Errorf("a click on the top drawn row of a scrolled draft landed the marker %q, want %q", got, "l1\n|l2\nl3")
	}
}

// End to end through the real mouse pipeline: a click (press then release, no
// motion) on a typed character places the caret onto it and leaves no selection.
func TestAClickOnQueryBoxTextPlacesTheCaret(t *testing.T) {
	a := splitApp(t, 200, 40, 4).withDraft("hello world")
	r := a.regions()
	w, h := r.Room(), a.paneHeight()
	draftTop, _, _, _, ok := a.composerRegion("", w, 0, h)
	if !ok {
		t.Fatal("the room drew no composer region to click in")
	}
	left := a.layout.PaneLeft(r, 0) + composerTextLeft

	a, _ = a.mouse(pressAt(left+6, draftTop))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: left + 6, Y: draftTop})

	c := a.composer()
	c.ta.InsertRune('|')
	if got := c.ta.Value(); got != "hello |world" {
		t.Errorf("after clicking the 'w', a typed marker landed %q, want %q", got, "hello |world")
	}
	if !a.sel.empty() {
		t.Errorf("a click on the query box left a selection behind: %+v", a.sel)
	}
}

// A click that places the caret must not highlight the row: it is a click, not a
// drag, so composerSelectionIn stays empty and nothing is copied.
func TestAClickToPlaceTheCaretCopiesNothing(t *testing.T) {
	a := splitApp(t, 200, 40, 4).withDraft("hello world")
	r := a.regions()
	w, h := r.Room(), a.paneHeight()
	draftTop, _, _, _, _ := a.composerRegion("", w, 0, h)
	left := a.layout.PaneLeft(r, 0) + composerTextLeft

	a, _ = a.mouse(pressAt(left+6, draftTop))
	got, _ := a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: left + 6, Y: draftTop})

	if got.composerSelectionIn("") != (marked{}) {
		t.Errorf("a caret click highlighted the row: %+v", got.composerSelectionIn(""))
	}
}

// tallDraft is n numbered lines, each distinct, so a drawn row names its line.
func tallDraft(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %02d", i)
	}
	return strings.Join(lines, "\n")
}

// pastedDraftApp is roomDraftApp at a chosen size with the draft pasted in one
// insert, for drafts too long to type a rune at a time.
func pastedDraftApp(t *testing.T, w, h int, draft string) App {
	t.Helper()
	a := newRoomApp(t).withSize(w, h).withAgents("alex")
	a.layout.ShowRoster = false
	a.focus = ""
	a = a.applyGeometry()
	return a.withComposer(a.composer().InsertText(draft))
}

// drawnDraftText is the typed text of each draft row the room's box draws, read
// off the rows a press captures.
func drawnDraftText(t *testing.T, a App) []string {
	t.Helper()
	r := a.regions()
	_, _, boxWidth, drawn, ok := a.composerRegion("", r.Room(), 0, a.paneHeight())
	if !ok {
		t.Fatal("the room drew no composer region")
	}
	out := make([]string, len(drawn.rows))
	for i, row := range drawn.rows {
		out[i] = strings.TrimRight(ansi.Strip(ansi.Cut(row, composerTextLeft, boxWidth-composerRightInset)), " ")
	}
	return out
}

// clickDraftRow clicks the room's drawn draft row at a column into its text.
func clickDraftRow(t *testing.T, a App, row, col int) App {
	t.Helper()
	r := a.regions()
	draftTop, _, _, _, ok := a.composerRegion("", r.Room(), 0, a.paneHeight())
	if !ok {
		t.Fatal("the room drew no composer region")
	}
	a, _ = click(a, a.layout.PaneLeft(r, 0)+composerTextLeft+col, draftTop+row)
	return a
}

// caretMarked is the room's draft with a marker typed at the caret - the one
// unambiguous read of where the caret is.
func caretMarked(a App) string {
	c := a.composer()
	c.ta.InsertRune('|')
	return c.ta.Value()
}

// markedAt is draft with the marker at rune col of logical line line.
func markedAt(draft string, line, col int) string {
	lines := strings.Split(draft, "\n")
	r := []rune(lines[line])
	lines[line] = string(r[:col]) + "|" + string(r[col:])
	return strings.Join(lines, "\n")
}

// A draft taller than the box scrolls inside it, and a click on any drawn row
// still lands on the character drawn there - the drawn rows are a window into
// the draft, found from the caret, not its first rows.
func TestAClickInAScrolledDraftPlacesTheCaret(t *testing.T) {
	draft := tallDraft(20)
	lines := strings.Split(draft, "\n")
	for _, row := range []int{0, 4} {
		a := roomDraftApp(t, draft)
		drawn := drawnDraftText(t, a)
		if len(drawn) >= len(lines) || drawn[0] == lines[0] {
			t.Fatalf("the draft is not scrolled: drawn %q", drawn)
		}
		line := slices.Index(lines, drawn[row])
		a = clickDraftRow(t, a, row, 5)
		if got, want := caretMarked(a), markedAt(draft, line, 5); got != want {
			t.Errorf("clicking drawn row %d (%q) put the marker at\n%q\nwant\n%q", row, drawn[row], got, want)
		}
	}
}

// With the caret moved near the top of a tall draft the box shows its first
// rows, and a click below the caret lands on the row clicked.
func TestAClickBelowTheCaretNearTheTopOfATallDraft(t *testing.T) {
	draft := tallDraft(20)
	var m tea.Model = roomDraftApp(t, draft)
	for range 16 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	a := m.(App)
	if drawn := drawnDraftText(t, a); drawn[0] != "line 00" || a.composer().ta.Line() != 3 {
		t.Fatalf("want the caret on line 3 under a box drawn from line 0: caret line %d, drawn %q", a.composer().ta.Line(), drawn)
	}
	a = clickDraftRow(t, a, 7, 2)
	if got, want := caretMarked(a), markedAt(draft, 7, 2); got != want {
		t.Errorf("clicking drawn row 7 put the marker at\n%q\nwant\n%q", got, want)
	}
}

// One long line wrapped far past the box: a click on a wrapped row lands on the
// first word drawn there, not on the row the same index would be from the top.
func TestAClickOnAWrappedRowOfAScrolledLine(t *testing.T) {
	words := make([]string, 500)
	for i := range words {
		words[i] = fmt.Sprintf("t%03d", i)
	}
	draft := strings.Join(words, " ")
	a := pastedDraftApp(t, 120, 40, draft)
	drawn := drawnDraftText(t, a)
	if strings.HasPrefix(drawn[0], "t000") {
		t.Fatalf("the line is not scrolled: drawn %q", drawn)
	}
	first := strings.Fields(drawn[3])[0]
	a = clickDraftRow(t, a, 3, 0)
	if got, want := caretMarked(a), strings.Replace(draft, first, "|"+first, 1); got != want {
		t.Errorf("clicking the wrapped row that starts %q put the marker %d runes in, want %d",
			first, strings.Index(got, "|"), strings.Index(want, "|"))
	}
}

// A line that exactly fills the box's width wraps to a trailing blank row, and
// the caret at its end sits on that row. As the last line of a draft exactly as
// tall as the box, that row scrolls the first line off: a click on the top drawn
// row must land on line 1, not on line 0 where counting from the top puts it.
func TestAClickWhenTheLastLineExactlyFillsTheBox(t *testing.T) {
	a := pastedDraftApp(t, 120, 40, "")
	full := strings.Repeat("x", a.composer().ta.Width())
	draft := tallDraft(9) + "\n" + full
	a = a.withComposer(a.composer().InsertText(draft))
	drawn := drawnDraftText(t, a)
	if drawn[0] != "line 01" || drawn[len(drawn)-1] != "" {
		t.Fatalf("want the box scrolled one row to a blank trailing row: drawn %q", drawn)
	}
	a = clickDraftRow(t, a, 0, 0)
	if got, want := caretMarked(a), markedAt(draft, 1, 0); got != want {
		t.Errorf("clicking drawn row 0 (%q) put the marker at\n%q\nwant\n%q", drawn[0], got, want)
	}
}

// A full-width line in the middle of a draft draws a blank row after it; a click
// on a row below that blank still lands on its own line.
func TestAClickBelowAFullWidthLine(t *testing.T) {
	a := pastedDraftApp(t, 120, 40, "")
	full := strings.Repeat("x", a.composer().ta.Width())
	draft := "alpha\n" + full + "\nbravo\ncharlie"
	a = a.withComposer(a.composer().InsertText(draft))
	drawn := drawnDraftText(t, a)
	if len(drawn) < 4 || drawn[2] != "" || drawn[3] != "bravo" {
		t.Fatalf("want a blank row drawn after the full line: drawn %q", drawn)
	}
	a = clickDraftRow(t, a, 3, 0)
	if got, want := caretMarked(a), markedAt(draft, 2, 0); got != want {
		t.Errorf("clicking drawn row 3 (bravo) put the marker at\n%q\nwant\n%q", got, want)
	}
}

// The blank row after a full-width line counts as a drawn row when the box has
// scrolled past it: a click on the top drawn row, with that blank row between it
// and the caret, lands on the line drawn there and not the one above.
func TestAClickAboveAFullWidthLineInAScrolledDraft(t *testing.T) {
	a := pastedDraftApp(t, 120, 40, "")
	full := strings.Repeat("x", a.composer().ta.Width())
	draft := tallDraft(15) + "\n" + full + "\nline 16\nline 17\nline 18\nline 19"
	a = a.withComposer(a.composer().InsertText(draft))
	drawn := drawnDraftText(t, a)
	if drawn[0] != "line 11" || !slices.Contains(drawn, "") {
		t.Fatalf("want a scrolled box with the full line's blank row drawn: drawn %q", drawn)
	}
	a = clickDraftRow(t, a, 0, 0)
	if got, want := caretMarked(a), markedAt(draft, 11, 0); got != want {
		t.Errorf("clicking drawn row 0 (line 11) put the marker at\n%q\nwant\n%q", got, want)
	}
}

// A draft that fits, ending in a line that exactly fills its last row, leaves
// the caret on the blank row below it - which the box does not draw. The drawn
// rows then start at the draft's top, however far the caret is below them.
func TestAClickWhenTheCaretSitsBelowTheDrawnRows(t *testing.T) {
	a := pastedDraftApp(t, 120, 40, "")
	w := a.composer().ta.Width()
	draft := strings.Repeat("a", w) + strings.Repeat("b", w) + strings.Repeat("c", w)
	a = a.withComposer(a.composer().InsertText(draft))
	if drawn := drawnDraftText(t, a); len(drawn) != 3 || a.composer().ta.LineInfo().RowOffset != 3 {
		t.Fatalf("want three drawn rows over a caret on the fourth: drawn %q, caret row %d",
			drawn, a.composer().ta.LineInfo().RowOffset)
	}
	a = clickDraftRow(t, a, 1, 0)
	if got, want := caretMarked(a), markedAt(draft, 0, w); got != want {
		t.Errorf("clicking the first 'b' put the marker %d runes in, want %d", strings.Index(got, "|"), w)
	}
}

// An open completion menu changes how tall the box may draw, so the drawn rows
// are mapped from the composer as drawn, not the one the pane stores.
func TestAClickInAScrolledDraftUnderACompletionMenu(t *testing.T) {
	draft := tallDraft(19) + "\n@"
	a := newRoomApp(t).withSize(120, 16).withAgents("alex")
	a.layout.ShowRoster = false
	a.focus = ""
	a = a.applyGeometry().withDraft(draft)
	if menu, _ := a.menuBlock("", a.regions().Room(), a.paneHeight()); menu == "" {
		t.Fatal("no completion menu is open over the draft")
	}
	drawn := drawnDraftText(t, a)
	lines := strings.Split(draft, "\n")
	line := slices.Index(lines, drawn[0])
	if line <= 0 {
		t.Fatalf("the draft is not scrolled: drawn %q", drawn)
	}
	a = clickDraftRow(t, a, 0, 0)
	if got, want := caretMarked(a), markedAt(draft, line, 0); got != want {
		t.Errorf("clicking drawn row 0 (%q) put the marker at\n%q\nwant\n%q", drawn[0], got, want)
	}
}

// A compacting conversation draws its bar a row taller, which in a short pane
// takes rows from the box. The press must map against that box, not the one the
// pane would draw without the bar: with the caret near the top both start at
// the draft's first row, but on different screen rows.
func TestAClickInACompactingConversationsTallDraft(t *testing.T) {
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(80, 16)
	draft := tallDraft(20)
	a = a.withComposer(a.composer().InsertText(draft))
	a = a.observe("s1", core.Event{Kind: core.KindSystem, Notice: core.NoticeCompacting})
	var m tea.Model = a
	for range 18 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	a = m.(App)
	frame := strings.Split(ansi.Strip(a.View()), "\n")
	y := slices.IndexFunc(frame, func(l string) bool { return strings.Contains(l, "> line 00") })
	if y < 0 || strings.Count(strings.Join(frame, "\n"), "> line ") >= 10 {
		t.Fatalf("want a box cut short by the compacting bar, drawn from line 00:\n%s", strings.Join(frame, "\n"))
	}
	x := len([]rune(frame[y][:strings.Index(frame[y], "line 00")]))
	a, _ = click(a, x, y)
	if got, want := caretMarked(a), markedAt(draft, 0, 0); got != want {
		t.Errorf("clicking the drawn 'line 00' put the marker at\n%q\nwant\n%q", got, want)
	}
}

// wideRunes is n distinct double-width runes, so no two drawn rows of them read
// alike and a row mapped one off cannot pass for the right one.
func wideRunes(from, n int) string {
	r := make([]rune, n)
	for i := range r {
		r[i] = rune(0x4e00 + from + i)
	}
	return string(r)
}

// Every start drawnRowStarts reports must begin with the text the box draws on
// that row - checked against the renderer across tall, wrapped, full-width and
// wide-rune drafts, with the caret walked up through each and then to its line's
// end, since the window is found from the caret.
func TestDrawnRowStartsMatchTheDrawnRows(t *testing.T) {
	const width = 40
	w := NewComposer().SetWidth(width).ta.Width()
	full := strings.Repeat("x", w)
	words := make([]string, 60)
	for i := range words {
		words[i] = fmt.Sprintf("t%02d", i)
	}
	drafts := map[string]string{
		"tall":       tallDraft(20),
		"wrapped":    strings.Join(words, " "),
		"full-width": tallDraft(6) + "\n" + full + "\nmid\n" + full + "\n" + tallDraft(6),
		"wide":       "a0\n" + wideRunes(0, 40) + "\nb1 " + wideRunes(100, 30) + "\n" + wideRunes(200, 17) + "\nc2",
		// The caret at the end of a line one cell short of full carries its column
		// into the wide line below, far enough to reach that line's second row.
		"carry": strings.Repeat("e", w-1) + "\n" + wideRunes(300, 40) + "\n" + tallDraft(6),
		// Three full rows fit the box, and the caret on the row wrapped after them
		// is not drawn: the drawn rows still start at the top.
		"fits-under": strings.Repeat("a", w) + strings.Repeat("b", w) + strings.Repeat("c", w),
	}
	for name, draft := range drafts {
		lines := strings.Split(draft, "\n")
		for step := 0; step <= 50; step++ {
			c := NewComposer().SetWidth(width).WithMaxRows(5)
			c.ta.InsertString(draft)
			c = c.fit().reposition()
			ups := step / 2
			for range ups {
				c, _ = c.Update(tea.KeyMsg{Type: tea.KeyUp})
			}
			if step%2 == 1 {
				c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnd})
			}
			d := caretTestDraft(c, width)
			if len(d.starts) != len(d.rows) {
				t.Fatalf("%s, %d up: %d starts for %d drawn rows", name, ups, len(d.starts), len(d.rows))
			}
			for i, s := range d.starts {
				drawn := strings.TrimRight(ansi.Strip(ansi.Cut(d.rows[i], composerTextLeft, width-composerRightInset)), " ")
				if rest := string([]rune(lines[s.line])[s.col:]); !strings.HasPrefix(rest, drawn) {
					t.Errorf("%s, %d up: drawn row %d reads %q, but its start (line %d, rune %d) reads %q",
						name, ups, i, drawn, s.line, s.col, rest)
				}
			}
		}
	}
}

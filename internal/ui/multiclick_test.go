package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// frozenClock pins clock for a test, so the clicks of a run are as close
// together as the test says they are.
func frozenClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	return &now
}

// clicks presses and releases one cell n times in a row, returning the last
// release's command - the copy, when the run selected something.
func clicks(a App, x, y, n int) (App, tea.Cmd) {
	var cmd tea.Cmd
	for range n {
		a, cmd = click(a, x, y)
	}
	return a, cmd
}

// selectedNow is the text the current selection covers, read the way its own
// release reads it.
func selectedNow(a App) string {
	switch {
	case a.sel.onScreen:
		return a.screenSelectedText()
	case a.sel.inComposer:
		return a.composerSelectedText()
	}
	m := a.sel.marked()
	lines, first := a.transcriptIn(a.sel.pane).selectionLines(m)
	return selectedText(lines, first, m)
}

// frameRow is one screen row of the drawn frame, without its styling.
func frameRow(a App, y int) string { return strings.Split(stripANSI(a.View()), "\n")[y] }

// A second click on the same cell selects the word under it - any run of
// characters that are not spaces - a third selects the whole row, and more
// clicks leave the row highlighted. Each copies on release, as a drag does.
func TestMoreClicksSelectAWordThenTheRow(t *testing.T) {
	frozenClock(t)
	a := splitApp(t, 200, 40, 4)
	const quick = 16 // inside "quick" on "  turn 0. the quick brown fox ..."
	row := strings.TrimSpace(frameRow(a, textRow)[:a.regions().Room()])

	a, cmd := clicks(a, quick, textRow, 2)
	if got := selectedNow(a); got != "quick" {
		t.Errorf("a double-click selected %q, want the word under it, %q", got, "quick")
	}
	if cmd == nil {
		t.Error("a double-click produced no copy")
	}

	a, cmd = click(a, quick, textRow)
	if got := selectedNow(a); got != row {
		t.Errorf("a triple-click selected %q, want the row %q", got, row)
	}
	if cmd == nil {
		t.Error("a triple-click produced no copy")
	}

	before := a.sel
	a, cmd = click(a, quick, textRow)
	if a.sel != before {
		t.Errorf("a fourth click moved the selection from %+v to %+v; the row stays highlighted", before, a.sel)
	}
	if cmd != nil {
		t.Error("a fourth click copied again; it does nothing")
	}
}

// A run is one cell, clicked quickly: a click too late, or on another cell, is
// a click of its own and clears the highlight the way any click does.
func TestAClickRunEndsWithTimeOrDistance(t *testing.T) {
	now := frozenClock(t)
	a := splitApp(t, 200, 40, 4)
	const quick = 16

	a, _ = click(a, quick, textRow)
	*now = now.Add(multiClickWindow + time.Millisecond)
	a, _ = click(a, quick, textRow)
	if !a.sel.empty() {
		t.Errorf("two clicks further apart than the window selected %q; each is a click", selectedNow(a))
	}

	a, _ = click(a, quick, textRow)
	a, _ = click(a, quick+1, textRow)
	if !a.sel.empty() {
		t.Errorf("clicks on two cells selected %q; a run is one cell", selectedNow(a))
	}
}

// A drag between two presses breaks the run: the press after it is a first
// click, not a second.
func TestADragEndsAClickRun(t *testing.T) {
	frozenClock(t)
	a := splitApp(t, 200, 40, 4)
	a, _ = drag(a, 16, 30, textRow)
	a, _ = click(a, 16, textRow)
	if !a.sel.empty() {
		t.Errorf("a click after a drag selected %q; it starts a run of its own", selectedNow(a))
	}
}

// A double-click on a space has no word to take, and is not the click it would
// otherwise be either: it leaves nothing highlighted.
func TestADoubleClickOnASpaceSelectsNothing(t *testing.T) {
	frozenClock(t)
	a := splitApp(t, 200, 40, 4)
	a, cmd := clicks(a, 13, textRow, 2) // the space after "the"
	if !a.sel.empty() || cmd != nil {
		t.Errorf("a double-click on a space selected %q; there is no word there", selectedNow(a))
	}
}

// The query box takes the same run over what was typed.
func TestMoreClicksSelectInTheQueryBox(t *testing.T) {
	frozenClock(t)
	a := splitApp(t, 200, 40, 4)
	for _, r := range "hello brave world" {
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	y := -1
	for i := range a.layout.Height {
		if strings.Contains(frameRow(a, i), "> hello brave world") {
			y = i
		}
	}
	if y < 0 {
		t.Fatal("the draft is not drawn")
	}
	x := strings.Index(frameRow(a, y), "brave") + 1

	a, _ = clicks(a, x, y, 2)
	if got := selectedNow(a); got != "brave" || !a.sel.inComposer {
		t.Errorf("a double-click in the query box selected %q (inComposer %v), want %q", got, a.sel.inComposer, "brave")
	}
	a, _ = click(a, x, y)
	if got := selectedNow(a); got != "hello brave world" {
		t.Errorf("a triple-click in the query box selected %q, want the whole row", got)
	}
}

// Chrome takes it too, bounded by the surface clicked: a roster row's first
// click opens that conversation as it always has, and the two after it select
// the name and then the row - the roster's row, not the frame's.
func TestMoreClicksSelectInTheRoster(t *testing.T) {
	frozenClock(t)
	a := rosterApp(t)
	y, ok := rosterRow(a)
	if !ok {
		t.Fatal("no roster row on screen")
	}
	line := frameRow(a, y)
	name := strings.Fields(line[strings.LastIndex(line, "│")+len("│"):])
	x := len([]rune(line[:strings.Index(line, name[1])])) + 1

	a, _ = click(a, x, y)
	if a.focus == "" {
		t.Fatal("the first click on a roster row did not open its conversation")
	}
	a, _ = click(a, x, y)
	if got := selectedNow(a); got != name[1] {
		t.Errorf("a double-click on the roster selected %q, want the name %q", got, name[1])
	}
	a, _ = click(a, x, y)
	if got := selectedNow(a); got != strings.Join(name, " ") {
		t.Errorf("a triple-click on the roster selected %q, want the roster row %q", got, strings.Join(name, " "))
	}
}

// A word is measured in cells, so a wide character is one character of the
// word however many columns it takes.
func TestAWordIsMeasuredInCells(t *testing.T) {
	for _, tc := range []struct {
		line   string
		col    int
		c0, c1 int
		ok     bool
	}{
		{"  turn 0. the quick", 16, 14, 19, true},
		{"日本語 text", 2, 0, 6, true},
		{"日本語 text", 8, 7, 11, true},
		{"a  b", 1, 0, 0, false},
		{"path/to/file.go:44, then", 3, 0, 19, true},
		{"👨‍💻 hello", 4, 3, 8, true}, // a ZWJ sequence is one character, two cells wide
		{"👨‍💻 hello", 0, 0, 2, true},
		{"e\u0301te x", 1, 0, 3, true}, // a combining accent is part of its letter
	} {
		c0, c1, ok := wordAt(tc.line, tc.col, 0, 100)
		if c0 != tc.c0 || c1 != tc.c1 || ok != tc.ok {
			t.Errorf("wordAt(%q, %d) = %d, %d, %v, want %d, %d, %v", tc.line, tc.col, c0, c1, ok, tc.c0, tc.c1, tc.ok)
		}
	}
}

// A drag after the second click ends the run too, so the press after it is a
// first click rather than a third.
func TestADragAfterADoubleClickEndsTheRun(t *testing.T) {
	frozenClock(t)
	a := splitApp(t, 200, 40, 4)
	a, _ = click(a, 16, textRow)
	a, _ = drag(a, 16, 24, textRow)
	a, _ = click(a, 16, textRow)
	if !a.sel.empty() {
		t.Errorf("the click after a double-click-and-drag selected %q; it starts a run of its own", selectedNow(a))
	}
}

// A run belongs to the surface it began on. When the cell under the pointer
// belongs to another one by the next press - a narrow grid slides to show the
// pane a click focused - that press is a first click there, not a second.
func TestAClickRunEndsWhenTheCellChangesSurface(t *testing.T) {
	now := frozenClock(t)
	a := splitApp(t, 200, 40, 4)
	a.clicks = clickRun{x: 16, y: textRow, n: 1, at: *now, on: surface{pane: "s1"}}
	a, cmd := click(a, 16, textRow) // the room's cell now
	if !a.sel.empty() || cmd != nil {
		t.Errorf("a second press on another surface selected %q; it is a first click there", selectedNow(a))
	}
}

// A keystroke between two clicks ends the run, the way it clears a highlight:
// the press after it is a first click.
func TestAKeystrokeEndsAClickRun(t *testing.T) {
	frozenClock(t)
	a := splitApp(t, 200, 40, 4)
	a, _ = click(a, 16, textRow)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	a, _ = click(a, 16, textRow)
	if !a.sel.empty() {
		t.Errorf("a click after a keystroke selected %q; it starts a run of its own", selectedNow(a))
	}
}

// Copies write in the order they were asked for: a double-click's word and the
// triple-click's row run as two commands at once, and the row - asked for last
// - must be what the clipboard keeps even when the word's command runs later.
func TestTheNewestCopyIsTheOneThatStays(t *testing.T) {
	var order inOrder
	word, row := uint64(1), uint64(2)
	var wrote []string
	if !order.write(row, func() { wrote = append(wrote, "row") }) {
		t.Fatal("the newest copy was refused")
	}
	if order.write(word, func() { wrote = append(wrote, "word") }) {
		t.Errorf("an older copy wrote after a newer one: %v", wrote)
	}
}

// A handled ⌫ on a highlighted draft ends the run too, like any keystroke.
func TestDeletingASelectionEndsAClickRun(t *testing.T) {
	frozenClock(t)
	a := splitApp(t, 200, 40, 4)
	for _, r := range "hello brave world" {
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	y := -1
	for i := range a.layout.Height {
		if strings.Contains(frameRow(a, i), "> hello brave world") {
			y = i
		}
	}
	x := strings.Index(frameRow(a, y), "brave") + 1
	a, _ = clicks(a, x, y, 2)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyBackspace})
	a, _ = click(a, x, y)
	if a.sel.span {
		t.Errorf("a click after deleting a double-clicked word selected %q; it is a first click", selectedNow(a))
	}
}

// The terminal's copy keeps the same order: an older copy's OSC 52 reaching the
// writer after a newer one's is dropped, so it cannot restore the older text.
func TestAnOlderCopyDoesNotOverwriteTheTerminalsClipboard(t *testing.T) {
	var out bytes.Buffer
	a := App{out: &out}
	older, newer := clipboardAsked.Add(1), clipboardAsked.Add(1)
	a.writeSequence(newer, "row")()
	a.writeSequence(older, "word")()
	if got := out.String(); got != "row" {
		t.Errorf("the terminal was written %q, want only the newest copy, %q", got, "row")
	}
}

// A divider drag ends the run as a text drag does, so a click where the divider
// was, once the panes have settled, is a first click rather than a second.
func TestADividerDragEndsAClickRun(t *testing.T) {
	frozenClock(t)
	a := splitApp(t, 120, 40, 4) // narrow, so the divider's cell is room text once it settles
	x := dividerColumnOf(a)
	a, _ = a.mouse(pressAt(x, textRow))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x + 30, Y: textRow})
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: x + 30, Y: textRow})
	a = a.applyGeometry()
	a, cmd := click(a, x, textRow)
	if a.sel.span || cmd != nil {
		t.Errorf("a click where the divider was selected %q; the drag ended the run", selectedNow(a))
	}
}

// A fourth press is still a press: dragged, it selects what it crosses, as any
// drag does; released where it went down, it leaves the row highlighted.
func TestAFourthPressCanStillDrag(t *testing.T) {
	frozenClock(t)
	a := splitApp(t, 200, 40, 4)
	a, _ = clicks(a, 16, textRow, 3)
	row := a.sel
	a, cmd := click(a, 16, textRow)
	if a.sel != row || cmd != nil {
		t.Errorf("a fourth click changed the selection from %+v to %+v; it leaves the row", row, a.sel)
	}
	a, cmd = drag(a, 16, 24, textRow)
	if got := selectedNow(a); got != "ick brown" || cmd == nil {
		t.Errorf("a fourth press dragged selected %q (copy %v), want the cells it crossed, %q", got, cmd != nil, "ick brown")
	}
}

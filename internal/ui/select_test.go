package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// textRow is a screen row of splitApp's room that carries a turn's body rather
// than the banner, a blank or an attribution. Row 4 is empty, and a drag across
// an empty row copying nothing is correct behaviour rather than a test.
const textRow = 6

// pressAt is a left button going down on a cell.
func pressAt(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y}
}

// drag is a press, some motion and a release across one row of a pane.
func drag(a App, x0, x1, y int) (App, tea.Cmd) {
	a, _ = a.mouse(pressAt(x0, y))
	for x := x0 + 1; x <= x1; x++ {
		a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x, Y: y})
	}
	return a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: x1, Y: y})
}

// Mutation check: dropping the `a.sel.empty()` guard from endSelection fails
// this at "a click produced a copy".
func TestAClickFocusesAPaneAndCopiesNothing(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	a, _ = a.mouse(pressAt(10, textRow))
	a, cmd := a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: 10, Y: textRow})
	if !a.sel.empty() {
		t.Errorf("sel = %+v: a press and release on one cell is a click, not a selection", a.sel)
	}
	if cmd != nil {
		t.Error("a click produced a copy: there was nothing under it")
	}
}

// Mutation check: dropping the copy from endSelection fails this at "a drag
// across the room produced no copy".
func TestADragSelectsAndCopiesOnRelease(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	a, cmd := drag(a, 10, 30, textRow)
	if a.sel.empty() {
		t.Fatal("a drag across twenty cells selected nothing")
	}
	if cmd == nil {
		t.Error("a drag across the room produced no copy: the clipboard write is the point of the gesture")
	}
	if a.selecting {
		t.Error("the button came up and the drag is still live")
	}
}

// The highlight outlives the release - it is what says what was taken - so a
// later drag elsewhere must not go on extending it.
func TestMotionAfterAReleaseDoesNotExtendTheOldSelection(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	a, _ = drag(a, 10, 30, textRow)
	before := a.sel
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 60, Y: 9})
	if a.sel != before {
		t.Errorf("sel moved from %+v to %+v with no button down on it", before, a.sel)
	}
}

func TestASelectionStaysInThePaneItWasTakenIn(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	room := a.regions().Room()
	// Drag off the right-hand edge of the room, into the DM beside it.
	a, _ = drag(a, 10, room+40, textRow)
	if a.sel.pane != "" {
		t.Errorf("the selection moved to pane %q: a drag belongs to the pane it started in", a.sel.pane)
	}
	if got := max(a.sel.anchor.col, a.sel.head.col); got >= room {
		t.Errorf("a column of %d reaches past the room's %d: a drag into the next column continues at this pane's edge", got, room)
	}
}

func TestOnlyThePaneHoldingTheSelectionDrawsAHighlight(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	a, _ = drag(a, 10, 30, textRow)
	if a.selectionIn("") == (marked{}) {
		t.Fatal("the room took the drag and resolves to nothing")
	}
	if a.selectionIn("s1") != (marked{}) {
		t.Errorf("the DM resolves to %+v: the drag was not taken in it", a.selectionIn("s1"))
	}
}

// Mutation check: dropping the width guard in applyGeometry fails this at "a
// height change cleared it".
func TestAWidthChangeClearsASelectionAndAHeightChangeDoesNot(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	a, _ = drag(a, 10, 30, textRow)
	if a.sel.empty() {
		t.Fatal("nothing was selected to begin with")
	}

	tall := a
	tall.pending.width, tall.pending.height = tall.layout.Width, tall.layout.Height+5
	if tall.applyGeometry().sel.empty() {
		t.Error("a height change cleared it: it re-wraps nothing, so the lines it is anchored to still mean what they meant")
	}

	narrow := a
	narrow.pending.width, narrow.pending.height = narrow.layout.Width-10, narrow.layout.Height
	if !narrow.applyGeometry().sel.empty() {
		t.Error("a width change kept it: a re-wrap renumbers transcript.lines, so its indices stop pointing at the text it was taken on")
	}
}

// Mutation check: making esc *only* clear the selection fails this at "esc
// cleared the selection instead of interrupting".
func TestAKeystrokeClearsASelectionAndStillDoesItsOwnJob(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	a, _ = drag(a, 10, 30, textRow)
	if a.sel.empty() {
		t.Fatal("nothing was selected to begin with")
	}
	model, _ := a.update(tea.KeyMsg{Type: tea.KeyEsc})
	got := model.(App)
	if !got.sel.empty() {
		t.Error("a keystroke left the highlight up")
	}
	// The press must still have reached App.key. A highlight left over from a
	// copy two minutes ago swallowing esc is an agent that does not stop.
	if !esc(t, a) {
		t.Error("esc cleared the selection instead of interrupting")
	}
}

// esc reports whether App.key claims the escape key at all, which is what the
// clear above must not take away.
func esc(t *testing.T, a App) bool {
	t.Helper()
	_, _, handled := a.key(tea.KeyMsg{Type: tea.KeyEsc})
	return handled
}

func TestTheCopiedTextIsWhatWasUnderTheDrag(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	a, _ = drag(a, 10, 30, textRow)
	m := a.sel.marked()
	tr := a.transcriptIn(a.sel.pane)
	got := selectedText(tr.lines.slice(m.from.line, m.to.line+1), m.from.line, m)
	// The exact cells, not merely "something without escapes": "" satisfies
	// every negative assertion here, which is how this test passed while a drag
	// on a blank row was copying nothing at all.
	line := ansi.Strip(tr.lines.slice(m.from.line, m.from.line+1)[0])
	if want := line[10:31]; got != want {
		t.Errorf("selectedText = %q, want %q (columns 10..30 of %q)", got, want, line)
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("the copied text carries escapes: %q", got)
	}
	if strings.HasSuffix(got, " ") {
		t.Errorf("the copied text keeps lipgloss's pad: %q", got)
	}
}

// --- the rows under a transcript ---------------------------------------

// queryBarRows is every row of splitApp's room below its transcript: the
// composer's box and its draft, the target line and the key line. A press on
// one of them is a press on the query bar rather than on the conversation.
//
// Derived rather than written down, because the composer grows with the draft
// and the working line comes and goes - a row number here would be right until
// somebody typed.
func queryBarRows(a App) []int {
	var rows []int
	for y := a.transcriptIn("").height; y < a.paneHeight(); y++ {
		rows = append(rows, y)
	}
	return rows
}

// Mutation check: dropping the fence from startSelection fails this on every
// row, naming the line it clamped into.
func TestADragOnTheQueryBarTakesNothing(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	last := a.transcriptIn("").height - 1
	for _, y := range queryBarRows(a) {
		got, cmd := drag(a, 10, 30, y)
		if !got.sel.empty() {
			t.Errorf("a drag on row %d selected %+v: the transcript's last row is %d, and pointIn clamps every row under it into a line nobody dragged over",
				y, got.sel, last)
		}
		if cmd != nil {
			t.Errorf("a drag on row %d wrote to the clipboard: there is no conversation on it", y)
		}
	}
}

// The reported bug. Read back through a conversation and then drag across the
// query bar: the anchor landed a windowful further in - mid-answer - and every
// motion message read that same row as "the drag has left the pane's bottom
// edge" and scrolled a line, so the highlight ran from there to the end of the
// conversation and the reader was carried back to the newest line.
func TestADragOnTheQueryBarLeavesTheReaderWhereTheyWere(t *testing.T) {
	a := splitApp(t, 200, 40, 40).scrollPane("", 20)
	was := a.transcriptIn("").scroll
	got, _ := drag(a, 10, 30, a.transcriptIn("").height+1)
	if !got.sel.empty() {
		t.Errorf("a drag on the query bar selected %+v, %d lines into a conversation the reader was not on",
			got.sel, got.sel.anchor.line)
	}
	if now := got.transcriptIn("").scroll; now != was {
		t.Errorf("the drag moved the reader from line %d to %d: a row under the transcript is not the pane's bottom edge", was, now)
	}
}

// The fence is the transcript's own last row and not the row above it.
func TestTheTranscriptsLastRowIsStillSelectable(t *testing.T) {
	a := splitApp(t, 200, 40, 40)
	got, cmd := drag(a, 10, 30, a.transcriptIn("").height-1)
	if got.sel.empty() {
		t.Error("a drag across the transcript's last row selected nothing: the fence took a row of conversation with it")
	}
	if cmd == nil {
		t.Error("a drag across the transcript's last row copied nothing")
	}
}

// A drag that started in the transcript still reaches past the pane, which is
// what lets a selection take more than one window - see extendSelection.
func TestADragOutOfTheTranscriptStillScrollsToReachFurther(t *testing.T) {
	a := splitApp(t, 200, 40, 40).scrollPane("", 20)
	was := a.transcriptIn("").scroll
	b, _ := a.mouse(pressAt(10, textRow))
	b, _ = b.mouse(tea.MouseMsg{
		Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft,
		X: 30, Y: b.transcriptIn("").height + 1,
	})
	if now := b.transcriptIn("").scroll; now <= was {
		t.Errorf("the reader stayed on line %d: a drag off the bottom of the pane scrolls it to reach further", now)
	}
	if b.sel.empty() {
		t.Error("the drag that started on the conversation selected nothing")
	}
}

// The fence reads the transcript's height off the pane the press landed in, so
// a conversation opened and clicked before any resize must already carry one.
// It does - show ends in resizePanes - and this is what says so: without it a
// freshly opened pane would refuse every drag, silently and only there.
func TestAFreshlyOpenedConversationCanBeSelectedAtOnce(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	a = a.openDMWith("s2", "jesse")
	for i := range 40 {
		a = said(a, "s2", "line "+string(rune('a'+i%26)))
	}
	if h := a.dms["s2"].tr.height; h <= 0 {
		t.Fatalf("the new pane's transcript is %d rows tall: nothing in it could be selected", h)
	}
	x := midOf(a.regions(), 1)
	if got, _ := drag(a, x, x+20, 5); got.sel.pane != "s2" || got.sel.empty() {
		t.Errorf("a drag in the conversation just opened selected %+v: it is drawn, so it is selectable", got.sel)
	}
}

// A card, a picker and a completion menu are pinned between the transcript and
// the composer, and they arrive without a resize: they shorten what is *drawn*
// while the stored transcript keeps the height the last geometry gave it. So
// the fence is measured off the draw - a menu's own rows are chrome, and a
// press on one of them used to anchor a card's height into the conversation.
//
// The composer's own draft row is in this loop too, and takes nothing here only
// because the drag (x 10..30) lands on the blank past the three-character "/re"
// - the box's typed text *is* selectable with a menu open, which
// TestAComposerSelectionWorksWhileAMenuIsPinnedAboveIt covers.
func TestADragOnAMenuPinnedOverTheComposerTakesNothing(t *testing.T) {
	a := splitApp(t, 200, 40, 40).withDraft("/re")
	if !a.completionUp() {
		t.Fatal("no menu is drawn, so this proves nothing about one")
	}
	w, h := a.regions().Room(), a.paneHeight()
	// Found on the pane as it is actually drawn, so the row this asserts about
	// is not the arithmetic under test read back.
	rows := strings.Split(a.roomPane(w, h), "\n")
	first := slices.IndexFunc(rows, func(r string) bool {
		return strings.Contains(ansi.Strip(r), "/resume")
	})
	if first < 0 {
		t.Fatalf("the menu is not on the pane that draws it:\n%s", strings.Join(rows, "\n"))
	}
	for y := first; y < h; y++ {
		if got, _ := drag(a, 10, 30, y); !got.sel.empty() {
			t.Errorf("a drag on row %d selected %+v: the menu starts at row %d and nothing from there down is conversation",
				y, got.sel, first)
		}
	}
	if got, _ := drag(a, 10, 30, first-1); got.sel.empty() {
		t.Error("the row above the menu selected nothing: the fence took a row of conversation with it")
	}
}

// The frame that was clicked is the one drawn *before* the keys moved, and both
// halves of that matter: refocus re-sizes the panes, and a picker follows the
// focus - so a fence measured afterwards counts another pane's menu against this
// one and refuses a row that was conversation on screen.
func TestAPressIsMeasuredAgainstTheFrameThatWasClicked(t *testing.T) {
	a := splitApp(t, 200, 40, 40) // the keys are the room's
	a = a.openPicker(effortCommand, []string{"s1"})
	if !a.picker.Open() {
		t.Fatal("no picker is up, so this proves nothing about one")
	}
	x := midOf(a.regions(), 1)
	last := a.dms["s1"].tr.height - 1
	got, _ := drag(a, x, x+20, last)
	if got.sel.empty() {
		t.Errorf("a drag on row %d of the conversation selected nothing: the picker was drawn over the room's composer, not this pane's, when the press landed", last)
	}
	if got.sel.pane != "s1" {
		t.Errorf("the drag was taken in pane %q, want s1", got.sel.pane)
	}
}

// A streamed answer is the other chrome that grows without a resize, and the
// commonest: an agent writing takes rows from its own transcript for as long as
// the turn runs. A press on one of them is a press on the preview.
func TestADragOnAStreamingPreviewTakesNothing(t *testing.T) {
	a := splitApp(t, 200, 40, 40).openDMWith("s1", "alex")
	col := a.columnOf("s1")
	d := *a.dms["s1"]
	for range 6 {
		d = d.Append(core.Event{Kind: core.KindPartialText, SessionID: "s1", Text: "streamingnow and more of it, at length, "})
	}
	a = a.withDM("s1", d)

	w, h := a.regions().Cols[col], a.paneHeight()
	rows := strings.Split(a.dmPane("s1", w, h), "\n")
	first := slices.IndexFunc(rows, func(r string) bool {
		return strings.Contains(ansi.Strip(r), "streamingnow")
	})
	if first < 0 {
		t.Fatalf("no preview is drawn, so this proves nothing about one:\n%s", strings.Join(rows, "\n"))
	}
	x := midOf(a.regions(), col)
	for y := first; y < h; y++ {
		if got, _ := drag(a, x, x+20, y); !got.sel.empty() {
			t.Errorf("a drag on row %d selected %+v: the preview starts at row %d and is not conversation",
				y, got.sel, first)
		}
	}
}

// The card is the third producer of the same chrome and the one that matters
// most: it goes up while an agent is blocked, which is exactly when somebody is
// reading that pane - and it is the tallest, so it moved a drag furthest.
func TestADragOnAPinnedQuestionCardTakesNothing(t *testing.T) {
	a, _ := asking(t, 200)
	col := a.columnOf("s1")
	w, h := a.regions().Cols[col], a.paneHeight()
	rows := strings.Split(a.dmPane("s1", w, h), "\n")
	// Where the card's own first row landed, rather than a phrase from it: the
	// question wraps, and the tool call that asked it is in the transcript above
	// under the same words.
	head := ansi.Strip(strings.Split(a.cardBlock("s1", w), "\n")[0])
	first := -1
	for y := len(rows) - 1; y >= 0; y-- {
		if strings.TrimSpace(head) != "" && strings.Contains(ansi.Strip(rows[y]), strings.TrimSpace(head)) {
			first = y
			break
		}
	}
	if first < 0 {
		t.Fatalf("no card is drawn, so this proves nothing about one:\n%s", strings.Join(rows, "\n"))
	}
	x := midOf(a.regions(), col)
	for y := first; y < h; y++ {
		if got, _ := drag(a, x, x+20, y); !got.sel.empty() {
			t.Errorf("a drag on row %d selected %+v: the card is drawn there and is not conversation", y, got.sel)
		}
	}
}

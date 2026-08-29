package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The query box draws each draft row as `│ > text   │`: a border, a column of
// padding, the "> " prompt, then the text, padded to the box and closed by the
// padding and border on the right. Text starts at column composerTextLeft and
// stops composerRightInset short of the far edge.
func draftRowsFor(width int, draft string) []string {
	c := NewComposer().SetWidth(width)
	c.ta.InsertString(draft)
	c = c.fit()
	all := strings.Split(c.box(width), "\n")
	// box is the top border then one row per draft line; drop the border.
	return all[1:]
}

func TestComposerTextTakesOnlyTheTypedCharacters(t *testing.T) {
	width := 30
	rows := draftRowsFor(width, "hello world")
	right := width - composerRightInset
	for _, c := range []struct {
		name string
		sel  selection
		want string
	}{
		{"a word", selection{inComposer: true, anchor: point{0, 0}, head: point{0, 4}}, "hello"},
		{"mid draft", selection{inComposer: true, anchor: point{0, 6}, head: point{0, 10}}, "world"},
		{"whole line past the text", selection{inComposer: true, anchor: point{0, 0}, head: point{0, 40}}, "hello world"},
	} {
		got := composerText(rows, c.sel.marked(), composerTextLeft, right)
		if got != c.want {
			t.Errorf("%s: composerText = %q, want %q", c.name, got, c.want)
		}
		if strings.Contains(got, "│") || strings.Contains(got, ">") {
			t.Errorf("%s: copied border or prompt: %q", c.name, got)
		}
	}
}

func TestComposerTextSpansRows(t *testing.T) {
	rows := draftRowsFor(22, "one two three four five six seven")
	right := 22 - composerRightInset
	// From the middle of the first row to the middle of the second.
	sel := selection{inComposer: true, anchor: point{0, 4}, head: point{1, 3}}
	got := composerText(rows, sel.marked(), composerTextLeft, right)
	if !strings.Contains(got, "\n") {
		t.Errorf("a two-row selection came back as one line: %q", got)
	}
}

// The constants are geometry, not guesses: render an empty box and a one-word
// box and read the border, the prompt and the text back off them. A theme change
// that moved the padding or the prompt fails here rather than silently copying a
// "│ > " into someone's clipboard.
func TestComposerTextRegion(t *testing.T) {
	width := 30
	rows := draftRowsFor(width, "x")
	if got := composerRowTextLen(rows[0], width); got != 1 {
		t.Errorf("one typed character measured as %d columns of text: the prompt or padding moved", got)
	}
	if got := composerRowTextLen(draftRowsFor(width, "")[0], width); got != 0 {
		t.Errorf("an empty box measured as %d columns of text: the prompt is being counted", got)
	}
}

func TestADragOverTheQueryBoxTextSelectsAndCopiesIt(t *testing.T) {
	a := splitApp(t, 200, 40, 4).withDraft("hello world")
	r := a.regions()
	w, h := r.Room(), a.paneHeight()
	draftTop, _, _, _, ok := a.composerRegion("", w, 0, h)
	if !ok {
		t.Fatal("the room drew no composer region to select in")
	}
	left := a.layout.PaneLeft(r, 0) + composerTextLeft
	got, cmd := drag(a, left, left+10, draftTop)

	if !got.sel.inComposer {
		t.Fatalf("a drag over the query box did not take a composer selection: %+v", got.sel)
	}
	if got.sel.empty() {
		t.Fatal("the drag over eleven cells of text selected nothing")
	}
	if cmd == nil {
		t.Error("a query-box drag produced no copy: the clipboard write is the point of the gesture")
	}
	if s := got.composerSelectedText(); s != "hello world" {
		t.Errorf("composerSelectedText = %q, want %q", s, "hello world")
	}
}

// The old bug the query-box fence was written for must stay fixed: a drag in the
// box selects the box's own text, never a transcript line, and never carries a
// scrolled reader back to the bottom.
func TestADragOverTheQueryBoxDoesNotDisturbTheTranscript(t *testing.T) {
	a := splitApp(t, 200, 40, 40).withDraft("hello world")
	a = a.scrollPane("", 20)
	was := a.transcriptIn("").scroll
	r := a.regions()
	w, h := r.Room(), a.paneHeight()
	draftTop, _, _, _, ok := a.composerRegion("", w, 0, h)
	if !ok {
		t.Fatal("the room drew no composer region")
	}
	left := a.layout.PaneLeft(r, 0) + composerTextLeft
	got, _ := drag(a, left, left+8, draftTop)

	if got.selectionIn("") != (marked{}) {
		t.Errorf("the query-box drag highlighted a transcript line: %+v", got.selectionIn(""))
	}
	if now := got.transcriptIn("").scroll; now != was {
		t.Errorf("the query-box drag moved the reader from line %d to %d", was, now)
	}
}

// A drag that runs off the end of a short line keeps the trailing blank out of
// the copy, the way selectedText trims it in the transcript.
func TestADragPastTheDraftCopiesOnlyTheText(t *testing.T) {
	a := splitApp(t, 200, 40, 4).withDraft("hi")
	r := a.regions()
	w, h := r.Room(), a.paneHeight()
	draftTop, _, _, _, _ := a.composerRegion("", w, 0, h)
	left := a.layout.PaneLeft(r, 0) + composerTextLeft
	got, _ := drag(a, left, left+40, draftTop) // far past the two characters
	if s := got.composerSelectedText(); s != "hi" {
		t.Errorf("composerSelectedText = %q, want %q: the pad past the text was copied", s, "hi")
	}
}

// A composer selection is anchored to draft rows, not transcript lines, so a
// room update - which translates and bounds line indices against the room's
// transcript - must leave it alone. Without the inComposer guard in withRoom, a
// line-move miss clears it, so a broadcast landing between press and release
// copies nothing.
func TestARoomUpdateLeavesAComposerSelectionAlone(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	a.sel = selection{pane: "", anchor: point{0, 0}, head: point{0, 5}, inComposer: true}
	before := a.sel

	room := a.room
	room.lineMoves = roomLineMoves{{from: 999, to: 0, rows: 1}} // does not cover draft row 0
	got := a.withRoom(room)

	if got.sel != before {
		t.Errorf("a room update changed the composer selection from %+v to %+v: draft rows are not transcript lines", before, got.sel)
	}
}

// A drag begun on the box's chrome - the border, the "> " prompt, or the blank
// past a short line - takes nothing: only the typed characters are selectable.
func TestADragBegunOnComposerChromeTakesNothing(t *testing.T) {
	a := splitApp(t, 200, 40, 4).withDraft("hi")
	r := a.regions()
	w, h := r.Room(), a.paneHeight()
	draftTop, _, _, _, ok := a.composerRegion("", w, 0, h)
	if !ok {
		t.Fatal("the room drew no composer region")
	}
	paneLeft := a.layout.PaneLeft(r, 0)

	// On the border and prompt, left of the first character.
	if got, _ := drag(a, paneLeft, paneLeft+3, draftTop); !got.sel.empty() {
		t.Errorf("a drag begun on the border/prompt selected %+v: chrome is not text", got.sel)
	}
	// On the blank past the two typed characters.
	past := paneLeft + composerTextLeft + 10
	if got, _ := drag(a, past, past+4, draftTop); !got.sel.empty() {
		t.Errorf("a drag begun on the blank past 'hi' selected %+v", got.sel)
	}
}

// A pane too short to draw the composer inside its allocation overflows and is
// clipped, so the bottom-up placement would point at rows the frame never drew.
// composerRegion declines it rather than anchoring on a phantom row.
func TestComposerRegionDeclinesATinyPane(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	if _, _, _, _, ok := a.composerRegion("", a.regions().Room(), 0, 3); ok {
		t.Error("composerRegion accepted a 3-row pane, too short to draw the composer fully")
	}
}

// A menu pinned above the composer (here a completion menu) shortens what is
// drawn but does not sit on the draft rows, so a drag over the typed text still
// selects it - composerRegion measures the box from the bottom, past the menu.
func TestAComposerSelectionWorksWhileAMenuIsPinnedAboveIt(t *testing.T) {
	a := splitApp(t, 200, 40, 40).withDraft("/re")
	if !a.completionUp() {
		t.Fatal("no menu is drawn, so this proves nothing about one")
	}
	r := a.regions()
	w, h := r.Room(), a.paneHeight()
	draftTop, _, _, _, ok := a.composerRegion("", w, 0, h)
	if !ok {
		t.Fatal("the room drew no composer region")
	}
	left := a.layout.PaneLeft(r, 0) + composerTextLeft
	got, _ := drag(a, left, left+2, draftTop) // across "/re"
	if !got.sel.inComposer || got.sel.empty() {
		t.Errorf("a drag over the draft text with a menu open selected %+v: the menu does not block the box's own text", got.sel)
	}
	if s := got.composerSelectedText(); s != "/re" {
		t.Errorf("composerSelectedText = %q, want %q", s, "/re")
	}
}

// A composer selection clears on a width change and survives a height change,
// the transcript's own rule: only a re-wrap renumbers the draft rows it is
// anchored to, and the composer re-wraps on width.
func TestAWidthChangeClearsAComposerSelectionAndAHeightChangeDoesNot(t *testing.T) {
	a := splitApp(t, 200, 40, 4).withDraft("hello world")
	r := a.regions()
	w, h := r.Room(), a.paneHeight()
	draftTop, _, _, _, _ := a.composerRegion("", w, 0, h)
	left := a.layout.PaneLeft(r, 0) + composerTextLeft
	a, _ = drag(a, left, left+8, draftTop)
	if !a.sel.inComposer || a.sel.empty() {
		t.Fatal("nothing was selected in the composer to begin with")
	}

	tall := a
	tall.pending.width, tall.pending.height = tall.layout.Width, tall.layout.Height+5
	if tall.applyGeometry().sel.empty() {
		t.Error("a height change cleared the composer selection: it re-wraps nothing")
	}

	narrow := a
	narrow.pending.width, narrow.pending.height = narrow.layout.Width-10, narrow.layout.Height
	if !narrow.applyGeometry().sel.empty() {
		t.Error("a width change kept the composer selection: a re-wrap moves the draft rows it is anchored to")
	}
}

// highlightComposerBlock puts a background on the dragged cells of the draft
// rows and leaves the border row above them alone.
func TestHighlightComposerBlockMarksTheDraftRowNotTheBorder(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(2) // termenv.ANSI - any colour profile proves the style applied
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	width := 30
	c := NewComposer().SetWidth(width).WithTitle("room")
	c.ta.InsertString("hello world")
	rendered := c.fit().View(width)
	plain := strings.Split(rendered, "\n")

	sel := selection{inComposer: true, anchor: point{0, 0}, head: point{0, 4}}
	out := highlightComposerBlock(rendered, sel.marked(), composerTextLeft, width-composerRightInset)
	lines := strings.Split(out, "\n")

	if lines[0] != plain[0] {
		t.Errorf("the top border was restyled:\n before %q\n after  %q", plain[0], lines[0])
	}
	if lines[1] == plain[1] {
		t.Errorf("the draft row was not highlighted: still %q", lines[1])
	}
}

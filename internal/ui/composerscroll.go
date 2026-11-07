package ui

// The wheel over a draft taller than its box scrolls the box's rows (the owner's
// 2026-11-06 ask): the view only, never the caret, as an editor's wheel does, and
// the next key brings the view back to the caret.
//
// bubbles keeps the view's offset to itself and moves it only to follow the caret,
// so the view is put where the wheel left it the one way that allows: the caret
// walked onto the rows wanted, an Update to follow it, and the caret put back with
// no Update after. Nothing is redone per frame - View draws bubbles' viewport as
// it always has - and reposition leaves a held view alone, which is what keeps it
// across a re-lay and every draw copy's SetSize.

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// wheelFoot is the most rows a query box and what is drawn under it take at the
// foot of its pane: the draft rows and the two borders, the target and armed-cue
// lines, and a status bar wrapped to its rows. A wheel above them is not on the
// box, which is decided before the pane is laid out to find it.
const wheelFoot = maxComposerRows + boxBorderRows + 2 + dmBarRows

// draftScroll is where the wheel left the box: the draft row drawn first, and the
// draft and wrap width it was taken over. Any other draft or width lets it go.
type draftScroll struct {
	top   rowStart
	value string
	width int
	set   bool
}

// followMsg matches nothing the text area handles, so its Update only brings the
// view onto the caret.
type followMsg struct{}

// Wheel scrolls a draft taller than its box by lines rows, up for a positive
// count, and reports whether the box took the wheel. A draft the box shows whole
// leaves it to the pane. It runs on the composer as drawn: its fit set the height
// of the viewport the walk below scrolls, and that height is the view's.
func (c Composer) Wheel(lines int) (Composer, bool) {
	h := c.ta.Height()
	if h < c.bound() { // drawn short of its bound: the whole draft shows
		return c, false
	}
	top := c.firstDrawn()
	if top == (rowStart{}) && len(c.rowsFrom(top, h+1)) <= h {
		return c, false // exactly full: nothing above the box or below it
	}
	next := c.movedTop(top, lines, h)
	switch next {
	case top:
		// At the draft's first or last row the wheel stops with the box (wake-orch,
		// 2026-11-06). Returning false here would carry it on into the pane.
		return c, true
	case c.followedTop():
		c.scroll = draftScroll{} // back over the caret: following it again
		return c.reposition(), true
	}
	c.scroll = draftScroll{top: next, value: c.ta.Value(), width: c.taWidth, set: true}
	return c.showFrom(next), true
}

// firstDrawn is the draft row the box draws first: where the wheel left it, or
// where reposition puts it.
func (c Composer) firstDrawn() rowStart {
	if c.held() {
		return c.scroll.top
	}
	return c.followedTop()
}

// movedTop is the first row of an h-row window lines rows up from top, or down
// for a negative count, never past the draft's first row or its last.
func (c Composer) movedTop(top rowStart, lines, h int) rowStart {
	if lines < 0 {
		rows := c.rowsFrom(top, h-lines)
		return rows[max(min(-lines, len(rows)-h), 0)]
	}
	probe := c.ta
	caretTo(&probe, top)
	for range lines {
		if !rowUp(&probe) {
			break
		}
	}
	return startOf(probe)
}

// showFrom puts top on the box's first row and leaves the caret where it was. The
// caret is walked to the window's last row and then its first, each followed by
// an Update - whichever side the view was on, the pair leaves it starting at top.
func (c Composer) showFrom(top rowStart) Composer {
	info := c.ta.LineInfo()
	home := rowStart{line: c.ta.Line(), col: info.StartColumn + info.ColumnOffset}
	rows := c.rowsFrom(top, c.ta.Height())
	_ = c.ta.View() // a viewport holding no lines does not scroll; see reposition
	for _, at := range []rowStart{rows[len(rows)-1], top} {
		caretTo(&c.ta, at)
		c.ta, _ = c.ta.Update(followMsg{})
	}
	caretTo(&c.ta, home)
	return c
}

// held reports whether the wheel's view stands: taken over this draft at this
// width. Anything that changed the draft brings the view back to the caret.
func (c Composer) held() bool {
	return c.scroll.set && c.scroll.width == c.taWidth && c.scroll.value == c.ta.Value()
}

// heldUpdate is Update for a message that is not a key while the wheel holds the
// view: the caret's blink, or a paste's text arriving after ⌃V. The text area's
// Update ends by bringing the view back to the caret, on a viewport it replaces,
// so it runs on a copy: a message that left the draft alone hands back only the
// caret, and one that changed it is taken whole, view and all.
func (c Composer) heldUpdate(msg tea.Msg) (Composer, tea.Cmd) {
	next, cmd := c.ta.Update(msg)
	if next.Value() == c.scroll.value {
		c.ta.Cursor = next.Cursor
		return c, cmd
	}
	c.ta, c.scroll = next, draftScroll{}
	return c.fit().reposition(), cmd
}

// scrollDraft gives the wheel to pane id's query box when the pointer is on it -
// its borders included - and its draft is taller than the box, reporting whether
// the box took it. The box is the one drawn, with whatever chrome is drawn above
// it, and its view is written back to the composer the pane holds. A page moved
// off its rest draws the box where drawnBox does not read it, so the wheel stays
// the page's there. A wheel that moves the view drops a selection in the box: the
// rows its press captured are gone.
func (a App) scrollDraft(id string, col, top, height, lines, y int) (App, bool) {
	held, ok := a.composerFor(id)
	oneRow := held.ta.LineCount() == 1 && ansi.StringWidth(held.Value()) < held.ta.Width() // fits any box
	if !ok || oneRow || y < top+height-wheelFoot {
		return a, false
	}
	if moved, away := a.pageHides(id); moved || away {
		return a, false
	}
	c, draftTop, ok := a.drawnBox(id, a.regions().Cols[col], top, height)
	if !ok || y < draftTop-1 || y > draftTop+c.ta.Height() {
		return a, false
	}
	next, took := c.Wheel(lines)
	if !took {
		return a, false
	}
	held.ta, held.scroll = next.ta, next.scroll
	a = a.withComposerFor(id, held.fit())
	if next.scroll != c.scroll {
		a = a.droppedComposerSelection(id)
	}
	return a, true
}

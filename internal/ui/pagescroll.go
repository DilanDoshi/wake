package ui

// A conversation drawing a card scrolls as one page: the transcript, the card and
// the composer move together under the wheel. Nothing is laid out for it - the
// page is the pane drawn taller, and what the wheel moves is which rows of that
// drawing the window shows. Without a card the wheel moves the transcript alone.
//
// up is how far the page is scrolled from its bottom. Up one row slides one row of
// chrome off the bottom and takes one older transcript line in at the top, so the
// pane is drawn height+up rows tall and its top height rows are shown. A card the
// pane has no room for opens on its foot - the rows the keys act on and the key
// line in view, the rest a wheel notch above (the owner's 2026-11-02 ruling).

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// pageState is where a conversation's page was scrolled to, and for which card
// and step of it: another ask, or another question of the same one, starts at
// rest. Unset is the default - the card's foot, or its top when the pane holds it.
// draft is the draft as the page last saw it, because a Composer shares its text
// area with the copy it was taken from, so a change cannot be read off the stored
// one.
type pageState struct {
	req   string
	step  int
	up    int
	set   bool
	draft string
}

// pageWindow is the rows a pane is drawn at and how many of its top rows the
// window leaves out.
type pageWindow struct{ render, skip int }

// windowPage is the window over a pane of height rows whose menu block overflows
// it by clipped rows, scrolled up rows from the bottom. Drawn at height plus the
// larger of the two, the card is never clipped; the part of that past the window
// is skipped from the top.
func windowPage(height, clipped, up int) pageWindow {
	extra := max(up, clipped)
	return pageWindow{render: height + extra, skip: extra - up}
}

// pagePlace is where a pane's page stands for one frame.
type pagePlace struct {
	height  int // the pane's own rows: what the window shows
	win     pageWindow
	up      int  // where the page is, never past limit
	limit   int  // the furthest up it goes: the clipped rows, then every older line
	clipped int  // menu rows the pane has no room for at rest
	top     int  // the transcript's top line at the pane's own height
	follow  bool // the reader is on the newest line, and the taller layout keeps them there
	drawn   int  // menu rows the pane has room for at the height it is drawn at
	cardTop int  // the card's first row in the pane as drawn at win.render
	tall    DM   // the pane laid out at win.render, for the draw to use - zero at rest
}

// pagePlace measures the pane id at width by height with menu pinned, for a card
// of cardRows.
func (a App) pagePlace(id string, width, height int, menu string, card Card, cardRows int) pagePlace {
	return a.placeOf(a.sizedDM(id, menu, width, height), id, width, menu, card, cardRows)
}

// placeOf is pagePlace for a pane already laid out at its own height with menu
// pinned, which is the one the draw lays out: the frame measures it once. cardRows
// is the card's own height - the top of the pinned block, which a picker can
// stack under.
func (a App) placeOf(dm DM, id string, width int, menu string, card Card, cardRows int) pagePlace {
	menuRows := lipgloss.Height(menu)
	// Past the block, the chrome a pane too short for it has no room for either.
	clipped := max(menuRows-dm.menuRoomAt(dm.composerRows()), 0)
	// A page at rest is drawn height+clipped rows tall wherever its rest is, so the
	// layout that finds the foot is the one the frame draws, and whatever the pane
	// pins above the card is counted. Chrome that grows with the height - a draft's
	// box, a preview the pane cut - takes rows from the block again; the page grows
	// by what it took, and both are bounded (maxComposerRows, the preview's rows).
	tall, laid := DM{}, dm.height
	for clipped > 0 {
		tall, laid = a.sizedDM(id, menu, width, dm.height+clipped), dm.height+clipped
		short := menuRows - tall.menuRoomAt(tall.composerRows())
		if short <= 0 {
			break
		}
		clipped += short
	}
	top := min(max(dm.tr.scroll, dm.tr.first()), dm.tr.bottom())
	limit := clipped + max(top-dm.tr.first(), 0)
	up, rested := dm.restingAt(card)
	if !rested {
		up = clipped
		if clipped > 0 {
			_, cardTop := menuPlace(tall, menuRows)
			up = clipped - max(cardTop+cardRows-dm.height, 0)
		}
	}
	up = min(up, limit)
	win := windowPage(dm.height, clipped, up)
	drawn := dm
	if win.render != dm.height {
		if win.render != laid {
			tall = a.sizedDM(id, menu, width, win.render)
		}
		drawn = tall
	}
	rows, cardTop := menuPlace(drawn, menuRows)
	return pagePlace{height: dm.height, win: win, up: up, limit: limit, clipped: clipped, top: top, follow: dm.tr.atBottom(), tall: tall,
		drawn: rows, cardTop: cardTop}
}

// menuPlace is the rows of the pinned block a layout draws and the row its top
// is on: the composer's box and the bar, which sit under it, counted up from the
// pane's last row.
func menuPlace(d DM, menuRows int) (rows, top int) {
	box := d.composerRows()
	rows = max(min(menuRows, d.menuRoomAt(box)), 0)
	return rows, d.height - box - barRows(d.bar) - rows
}

// holds reports whether every one of a card's rows is inside the window: the
// card's keys are live only then, for the rule they have always had - an
// affordance on a surface that is not drawn is not live.
func (p pagePlace) holds(cardRows int) bool {
	return p.drawn >= cardRows && p.cardTop >= p.win.skip && p.cardTop+cardRows <= p.win.skip+p.height
}

// restingAt is where the operator left the page for card, or where a draft puts
// it - the bottom, with the box in view, since the draft is theirs to see - and
// whether either applies. Otherwise the page opens as far up as the card's last
// row stays in view (placeOf): the card's top for one the pane holds under the
// transcript's rows, with the composer scrolled away, lower for one it cannot,
// which opens on its foot with the key line and the rows the keys act on in view
// (the owner's 2026-11-02 ruling, reversing 2026-10-20's top).
func (d DM) restingAt(card Card) (int, bool) {
	switch {
	case d.page.set && d.page.req == card.RequestID && d.page.step == card.Cursor:
		return d.page.up, true
	case !blank(d.composer.Value()):
		return 0, true
	}
	return 0, false
}

// sizedDM is the conversation id laid out for a pane of width by height with menu
// pinned - transcriptRows' measurement, and the page's.
func (a App) sizedDM(id, menu string, width, height int) DM {
	return a.dmFor(id).WithMenu(menu).WithCompacting(a.compactingSince(id)).SetSize(width, height)
}

// pageOf is the page of a drawn conversation that has a card up.
func (a App) pageOf(id string) (pagePlace, Card, bool) {
	card, ok := a.cardOf(id)
	width, height, drawn := a.paneSize(id)
	if !ok || !drawn {
		return pagePlace{}, Card{}, false
	}
	block := a.cardBlock(id, width, false)
	menu := a.menuAround(block, id, width, height)
	return a.pagePlace(id, width, height, menu, card, lipgloss.Height(block)), card, true
}

// scrollPage moves id's page by lines, up for a positive count. It reports false
// when there is no page to move - no card, or a page at rest asked to go down -
// and the transcript takes the wheel as it always has. The top of the page ends
// the wheel there: the transcript's own scroll would shorten the page under it.
func (a App) scrollPage(id string, lines int) (App, bool) {
	place, card, ok := a.pageOf(id)
	if !ok {
		return a, false
	}
	up := min(max(place.up+lines, 0), place.limit)
	if up == place.up {
		return a, lines > 0
	}
	d := a.dms[id]
	if d == nil {
		return a, false
	}
	next := *d
	next.page.req, next.page.step, next.page.up, next.page.set = card.RequestID, card.Cursor, up, true
	return a.withDM(id, next), true
}

// pageHome brings id's page to its bottom, where the composer is: what opening
// an answer, or a draft that changed, needs on screen.
func (a App) pageHome(id string) App {
	card, ok := a.cardOf(id)
	d := a.dms[id]
	if !ok || d == nil || (d.page.set && d.page.req == card.RequestID && d.page.step == card.Cursor && d.page.up == 0) {
		return a
	}
	next := *d
	next.page.req, next.page.step, next.page.up, next.page.set = card.RequestID, card.Cursor, 0, true
	return a.withDM(id, next)
}

// draftSeen brings id's page home when c changes what its draft holds, whichever
// key wrote it: a draft nobody can see is one a later ↵ sends unread. A box that
// emptied forgets the rest instead, unless an answer is being written in it - the
// card settles again, so a stray character deleted or an answer in your own words
// sent leaves the step whole and its keys live.
func (a App) draftSeen(id string, c Composer) App {
	d := a.dms[id]
	if d == nil {
		return a
	}
	now := c.Value()
	if d.page.draft == now {
		return a
	}
	next := *d
	next.page.draft = now
	if blank(now) && a.answerTitle(id) == "" {
		next.page.set = false
		return a.withDM(id, next)
	}
	return a.withDM(id, next).pageHome(id)
}

// boxAway reports whether id's page has the composer out of view. What it holds is
// sent, or answered, only from a box the operator can see.
func (a App) boxAway(id string) bool {
	place, _, ok := a.pageOf(id)
	return ok && place.up != 0
}

// pageHides reports what the page has moved out from under a press: every row the
// transcript's anchors read (rows), or only the composer's place (box). At rest
// in a pane too short for its card the transcript's row is where it was and the
// composer is not.
func (a App) pageHides(id string) (rows, box bool) {
	place, _, ok := a.pageOf(id)
	return ok && place.up != place.clipped, ok && place.up != 0
}

// transcriptPage is the window when the chrome has scrolled out of it entirely:
// height lines of the transcript, drawn without laying out the rest of the pane.
func (d DM) transcriptPage(height int) string {
	return d.tr.sized(d.tr.width, height).view(d.sel)
}

// skipRows drops the first n rows of a block.
func skipRows(s string, n int) string {
	for range n {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			return ""
		}
		s = s[i+1:]
	}
	return s
}

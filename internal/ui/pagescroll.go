package ui

// A conversation drawing a card scrolls as one page: the transcript, the card and
// the composer move together under the wheel. Nothing is laid out for it - the
// page is the pane drawn taller, and what the wheel moves is which rows of that
// drawing the window shows. Without a card the wheel moves the transcript alone.
//
// up is how far the page is scrolled from its bottom. Up one row slides one row of
// chrome off the bottom and takes one older transcript line in at the top, so the
// pane is drawn height+up rows tall and its top height rows are shown. A card the
// pane has no room for opens with the clipped rows already behind the window, on
// the card's own top, and the wheel brings the rest up from under it.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// pageState is where a conversation's page was scrolled to, and for which card:
// another ask starts at rest. Unset is the default - the card's own top. draft is
// the draft as the page last saw it, because a Composer shares its text area with
// the copy it was taken from, so a change cannot be read off the stored one.
type pageState struct {
	req   string
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
	up      int // where the page is, never past limit
	limit   int // the furthest up it goes: the clipped rows, then every older line
	clipped int // menu rows the pane has no room for at rest
	top     int // the transcript's top line at the pane's own height
	drawn   int // menu rows the pane has room for at the height it is drawn at
	cardTop int // the card's first row in the pane as drawn at win.render
	tall    DM  // the pane laid out at win.render, for the draw to use - zero at rest
}

// pagePlace measures the pane id at width by height with menu pinned.
func (a App) pagePlace(id string, width, height int, menu string, card Card) pagePlace {
	return a.placeOf(a.sizedDM(id, menu, width, height), id, width, menu, card)
}

// placeOf is pagePlace for a pane already laid out at its own height with menu
// pinned, which is the one the draw lays out: the frame measures it once.
func (a App) placeOf(dm DM, id string, width int, menu string, card Card) pagePlace {
	menuRows := lipgloss.Height(menu)
	box := dm.composerRows()
	clipped := max(menuRows-max(dm.menuRoomAt(box), 0), 0)
	top := min(max(dm.tr.scroll, dm.tr.first()), dm.tr.bottom())
	limit := clipped + max(top-dm.tr.first(), 0)
	up := min(dm.pageAt(card, clipped), limit)
	win := windowPage(dm.height, clipped, up)
	// The card is placed by the layout it is drawn in: the rows a scrolled page
	// adds let a draft or a streamed preview grow, and the card sits above them.
	drawn, drawnBox, tall := dm, box, DM{}
	if win.render != dm.height {
		tall = a.sizedDM(id, menu, width, win.render)
		drawn, drawnBox = tall, tall.composerRows()
	}
	rows := max(min(menuRows, drawn.menuRoomAt(drawnBox)), 0)
	return pagePlace{height: dm.height, win: win, up: up, limit: limit, clipped: clipped, top: top, tall: tall,
		drawn: rows, cardTop: win.render - drawnBox - barRows(drawn.bar) - rows}
}

// holds reports whether every one of a card's rows is inside the window: the
// card's keys are live only then, for the rule they have always had - an
// affordance on a surface that is not drawn is not live.
func (p pagePlace) holds(cardRows int) bool {
	return p.drawn >= cardRows && p.cardTop >= p.win.skip && p.cardTop+cardRows <= p.win.skip+p.height
}

// pageAt is where this conversation's page stands for card: where the operator
// left it, or the default, which is the clipped rows already behind the window.
func (d DM) pageAt(card Card, clipped int) int {
	if d.page.set && d.page.req == card.RequestID {
		return d.page.up
	}
	return clipped
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
	menu := a.menuAround(a.cardBlock(id, width, false), id, width, height)
	return a.pagePlace(id, width, height, menu, card), card, true
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
	next.page.req, next.page.up, next.page.set = card.RequestID, up, true
	return a.withDM(id, next), true
}

// pageHome brings id's page to its bottom, where the composer is: what opening
// an answer, or a draft that changed, needs on screen.
func (a App) pageHome(id string) App {
	card, ok := a.cardOf(id)
	d := a.dms[id]
	if !ok || d == nil || (d.page.set && d.page.req == card.RequestID && d.page.up == 0) {
		return a
	}
	next := *d
	next.page.req, next.page.up, next.page.set = card.RequestID, 0, true
	return a.withDM(id, next)
}

// draftSeen brings id's page home when c changes what its draft holds, whichever
// key wrote it: a draft nobody can see is one a later ↵ sends unread.
func (a App) draftSeen(id string, c Composer) App {
	d := a.dms[id]
	if d == nil {
		return a
	}
	now, attached := c.Value(), len(c.attachments) != len(d.Composer().attachments)
	if d.page.draft == now && !attached {
		return a
	}
	next := *d
	next.page.draft = now
	return a.withDM(id, next).pageHome(id)
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
	page := d.tr.sized(d.tr.width, height).view(d.sel)
	if d.behindAsk {
		page = quieted(page)
	}
	return page
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

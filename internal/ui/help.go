package ui

// The /wake-help view: every key and command Wake has, in place of one pane's
// body, narrowed as you type. Read-only and drawn from what Wake already holds,
// so nothing is written to the daemon or to an agent.
//
// A pane view like /workflows and for its reasons: a menu over the composer is
// bounded by what the pane can spare and closes on a key, and this has a hundred
// rows to scroll and a query to take. Pane-scoped - drawn only in Pane, reading
// keys only while that pane holds them - and key-capturing: read above App.key's
// switch, it owns every key while it is up, so it adds no legendEntries entry and
// advertises its own on its foot. ⌃C is the one key it hands back, after closing,
// for cmd/wake/killswitch.go's invariant. What it lists is helpcontent.go's.

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	// helpKeys is the view's own key line, drawn on its foot.
	helpKeys = "↑↓ ⇞⇟ scroll · type to filter · ⌫ widen · esc close"

	// helpQueryMax bounds what a paste can put in the filter.
	helpQueryMax = 40

	// helpBodyTop is the rows above the body: the pane's title and the filter.
	helpBodyTop = 2

	// helpFootRows is the rows under it: the key line.
	helpFootRows = 1
)

// HelpView is the open view; the zero value is none.
type HelpView struct {
	Up     bool
	Pane   string // the pane it is drawn in, "" the room
	Query  string // what the rows are narrowed to
	Scroll int    // the first body row drawn

	laid helpLayout // the rows as last laid out; see helpRows
}

// helpLayout is the rows for one width and query. It is kept so a frame does not
// lay them out again - the draw runs per message, the layout changes per key and
// per resize - and it is only ever read against the key it was made for.
type helpLayout struct {
	ok    bool
	width int
	query string
	rows  []string
}

func (v HelpView) Open() bool { return v.Up }

// helpState is the view and the one thing it draws that only cmd/wake holds.
type helpState struct {
	view  HelpView
	shell []helpRow // `wake help`'s own lines, parsed once; none draws no section
}

// WithUsage hands the view the shell usage it lists. It is cmd/wake's own text,
// so the section is `wake help` by identity rather than by a copy.
func (a App) WithUsage(usage string) App { a.help.shell = shellRows(usage); return a }

// helpIn reports whether the view is drawn in this pane.
func (a App) helpIn(id string) bool { return a.help.view.Open() && a.help.view.Pane == id }

// openHelp is /wake-help: the view over this pane, its argument the first filter.
func (a App) openHelp(arg string) (App, tea.Cmd) {
	a = a.clearDraft()
	a.help.view = HelpView{Up: true, Pane: a.focus}.typed(strings.TrimSpace(arg))
	return a, nil
}

func (a App) closeHelp() App {
	a.help.view = HelpView{}
	return a
}

// helpKey owns every key while the view's pane holds them and the board is not
// drawn over it. ⌃C closes the view and is handed on to park; the model comes
// back either way, as workflowKey's does.
func (a App) helpKey(m tea.KeyMsg) (App, tea.Cmd, bool) {
	v := a.help.view
	if !v.Open() || a.focus != v.Pane || a.board.Up {
		return a, nil, false
	}
	switch m.Type {
	case tea.KeyCtrlC:
		return a.closeHelp(), nil, false
	case tea.KeyEsc:
		return a.closeHelp(), nil, true
	case tea.KeyUp:
		a = a.helpMoved(-1)
	case tea.KeyDown:
		a = a.helpMoved(1)
	case tea.KeyPgUp:
		a = a.helpMoved(-a.helpPage())
	case tea.KeyPgDown:
		a = a.helpMoved(a.helpPage())
	case tea.KeyBackspace:
		a.help.view = v.backspaced()
	case tea.KeySpace:
		a.help.view = v.typed(" ")
	case tea.KeyRunes:
		a.help.view = v.typed(string(m.Runes))
	}
	return a, nil, true
}

// helpWheel is the wheel over the view. Like any wheel it moves no keys.
func (a App) helpWheel(up bool) App {
	if up {
		return a.helpMoved(-wheelLines)
	}
	return a.helpMoved(wheelLines)
}

// helpLaidOut keeps the rows for the pane's width and the query as they stand, so
// the frames that follow read them instead of laying them out. App.Update calls
// it after every message, which catches a key, a resize and an open alike; it is
// not called from openHelp, because /wake-help is a value in the commands map and
// the layout reads that map - an initialization cycle. A draw that finds the
// layout made for another width or query lays the rows out itself.
func (a App) helpLaidOut() App {
	v := a.help.view
	w, _, ok := a.paneSize(v.Pane)
	if !v.Open() || !ok || (v.laid.ok && v.laid.width == w && v.laid.query == v.Query) {
		return a
	}
	a.help.view.laid = helpLayout{ok: true, width: w, query: v.Query, rows: a.helpRows(w)}
	return a
}

// helpPress is a press on the view's pane: it takes the keys there. Its text is
// copied by the frame-wide screen selection the press starts, as any chrome's.
func (a App) helpPress(id string) App {
	a.fleet = a.fleet.Focus(id)
	return a.refocus(id)
}

// helpMoved scrolls by rows, from where the draw clamped it and within what the
// pane draws, so a scroll left past a shorter list moves from what is on screen.
func (a App) helpMoved(rows int) App {
	w, h, ok := a.paneSize(a.help.view.Pane)
	if !ok {
		return a
	}
	_, _, limit := a.helpBody(w, h)
	a.help.view.Scroll = clamp(min(a.help.view.Scroll, limit)+rows, 0, limit)
	return a
}

// helpPage is a page's rows: the body less one, so a row of context stays.
func (a App) helpPage() int {
	w, h, ok := a.paneSize(a.help.view.Pane)
	if !ok {
		return 1
	}
	_, room, _ := a.helpBody(w, h)
	return max(room-1, 1)
}

// typed adds to the filter: one space between words, none leading, nothing
// unprintable, and no more than helpQueryMax runes. A new query starts at the top.
func (v HelpView) typed(text string) HelpView {
	for _, r := range text {
		if unicode.IsSpace(r) {
			r = ' '
		}
		switch {
		case unicode.IsControl(r):
		case r == ' ' && (v.Query == "" || strings.HasSuffix(v.Query, " ")):
		case utf8.RuneCountInString(v.Query) >= helpQueryMax:
		default:
			v.Query += string(r)
		}
	}
	v.Scroll = 0
	return v
}

// backspaced drops the filter's last rune.
func (v HelpView) backspaced() HelpView {
	if r := []rune(v.Query); len(r) > 0 {
		v.Query = string(r[:len(r)-1])
	}
	v.Scroll = 0
	return v
}

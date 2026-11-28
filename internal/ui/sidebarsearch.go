package ui

// The sidebar's search bar (owner's 2026-11-20 pick, D1 of the mockups): three rows
// atop the roster that filter it in place with the jump picker's matcher. A click
// focuses it, typing edits the filter and the sidebar narrows as it goes - a team
// with no match drops out, and the count says how many of the roster matched. It is
// not a modal: the keys that are not its own let go of it and do their job, and the
// filter stands after they have, drawn with its accent and count until esc clears it.

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
)

const (
	// searchBarRows is the bar's height: a box around one line.
	searchBarRows = 3

	// searchListFloor is the rows the roster keeps under the bar: all and three more. A
	// sidebar shorter than that draws no bar, since a filter with no room to show
	// what it found is worse than none.
	searchListFloor = 4

	sidebarSearchVerb = "sidebar search"
	searchHint        = "search agents"
	searchNoMatch     = "no agent matches"

	// pickFilteredOut is the refusal for a selection the filter hides, the sibling of
	// pickOutOfView for one the wheel scrolled away.
	pickFilteredOut = "the row selected in the sidebar is filtered out. esc in the search bar clears the filter, ⇧↑↓ moves on from it"

	barKeys = "type to filter · ↑↓ ⇧↑↓ move · ↵ open · ⌃B below · ⌃D here · esc clear · ⌃C let go"
)

// SideSearch is the sidebar's filter: what is typed, and whether the bar holds the keys.
type SideSearch struct {
	Query string
	Focus bool
}

// searchBarShown is whether the sidebar is drawn tall enough for its bar.
func (a App) searchBarShown() bool {
	return a.layout.ShowRoster && a.regions().Roster > 0 && !a.board.Up &&
		a.rosterHeight() >= searchBarRows+searchListFloor
}

// searchRows is the rows the bar takes off the top of the sidebar: none when it is not drawn.
func (a App) searchRows() int {
	if a.searchBarShown() {
		return searchBarRows
	}
	return 0
}

// rosterListHeight is the rows the roster itself has: under the bar, above the usage bar.
func (a App) rosterListHeight() int { return a.rosterHeight() - a.searchRows() }

// barFocused is whether the bar holds the keys. It must be drawn to: a hold on a bar
// nobody can see would take keys silently.
func (a App) barFocused() bool { return a.sideSearch.Focus && a.searchBarShown() }

// activeQuery is the filter the roster is drawn under: none while the bar is not drawn.
func (a App) activeQuery() string {
	if a.sideSearch.Query == "" || !a.searchBarShown() {
		return ""
	}
	return a.sideSearch.Query
}

// rosterOf is the ranked roster the sidebar draws: ranked as it came, narrowed to
// the agents the filter finds - in place, with no second ranking.
func (a App) rosterOf(ranked []Agent) []Agent {
	q := a.activeQuery()
	if q == "" {
		return ranked
	}
	return slices.DeleteFunc(slices.Clone(ranked), func(ag Agent) bool {
		_, ok := findClass(q, ag.Name)
		return !ok
	})
}

// rosterAgents is the sidebar's agents: what every click, key and window reads, so
// they cannot disagree with what is drawn.
func (a App) rosterAgents() []Agent { return a.rosterOf(a.fleet.OnRoster()) }

// searchEdited changes the filter and puts the cursor where the sidebar still shows it.
func (a App) searchEdited(edit func(string) string) App {
	a.sideSearch.Query = edit(a.sideSearch.Query)
	a.rosterView = rosterView{} // a held window was cut for the old list
	return a.searchHomed()
}

// searchHomed leaves the cursor where it is while the sidebar still draws it, and
// otherwise puts it on the first match, so ↵ opens what the operator can see.
func (a App) searchHomed() App {
	shown := a.fleet.sectioned(a.rosterAgents())
	r := a.roster
	switch {
	case r.Selected != "" && slices.ContainsFunc(shown, func(ag Agent) bool { return ag.ID == r.Selected }):
		return a
	case r.Header == core.BroadcastName:
		return a
	case r.Header != "" && slices.ContainsFunc(shown, func(ag Agent) bool { return ag.Team == r.Header }):
		return a
	case len(shown) == 0:
		a.roster = Roster{}
	default:
		a.roster = Roster{Selected: shown[0].ID}
	}
	return a
}

// cursorFilteredOut is whether the cursor is on an agent or a channel the filter hides.
func (a App) cursorFilteredOut() bool {
	if a.sideSearch.Query == "" || a.activeQuery() == "" { // asked on every key and every frame: the cheap test first
		return false
	}
	r, shown := a.roster, a.fleet.sectioned(a.rosterAgents())
	switch {
	case r.Selected != "":
		return !slices.ContainsFunc(shown, func(ag Agent) bool { return ag.ID == r.Selected })
	case r.Header != "" && r.Header != core.BroadcastName:
		return !slices.ContainsFunc(shown, func(ag Agent) bool { return ag.Team == r.Header })
	}
	return false
}

// letGoOfBar is the bar not holding the keys. A key that goes on to the composer is
// let through by App.key, whose returned App Update then discards, so Update calls
// this too, beside the closes it does for the same reason.
func (a App) letGoOfBar() App {
	a.sideSearch.Focus = false
	return a
}

// barKey is the bar's keys while it holds them: typing, ⌫ and space edit the
// filter, the arrows walk the matches, ↵ ⌃B and ⌃D open the cursored agent the way
// the jump box does, esc clears the filter and gives the keys back. Any other key
// lets go of the bar and goes on to do its own job, so the bar never swallows one -
// including ⌃F, which opens the jump box and never focuses this.
func (a App) barKey(m tea.KeyMsg) (App, bool) {
	if !a.barFocused() {
		return a.letGoOfBar(), false // a hold the sidebar's hiding left behind is no hold
	}
	switch m.Type {
	case tea.KeyRunes:
		return a.searchEdited(func(q string) string { return typedQuery(q, string(m.Runes)) }), true
	case tea.KeySpace:
		return a.searchEdited(func(q string) string { return typedQuery(q, " ") }), true
	case tea.KeyBackspace:
		return a.searchEdited(backspacedQuery), true
	case tea.KeyUp, tea.KeyShiftUp:
		return a.pickAgent(-1), true
	case tea.KeyDown, tea.KeyShiftDown:
		return a.pickAgent(1), true
	case tea.KeyEnter:
		return a.openSearched(App.openInNewPane), true
	case tea.KeyCtrlB:
		return a.openSearched(App.openBelow), true
	case tea.KeyCtrlD:
		return a.openSearched(App.openDMWith), true
	case tea.KeyEsc: // alt+esc, two fast escapes, is one
		a.sideSearch = SideSearch{}
		a.rosterView = rosterView{}
		return a, true
	case tea.KeyCtrlC: // the habit that cancels a search must not park the agent the cursor homed on
		return a.letGoOfBar(), true
	}
	return a.letGoOfBar(), false
}

// openSearched opens what the cursor is on, as the jump box does and a click does:
// a channel header picks that channel, an agent opens where place says. The bar lets
// go of the keys once something opened; a refused placement leaves it as it was.
func (a App) openSearched(place func(App, string, string) App) App {
	if header, ok := a.pickedHeader(); ok {
		a.sideSearch.Focus = false
		return a.selectChannel(header)
	}
	ag, ok := a.pickedAgent()
	if !ok {
		advice := a.unseenOr(noPaneAdvice)
		if len(a.rosterAgents()) == 0 && a.activeQuery() != "" {
			advice = searchNoMatch
		}
		notice.Report("%s", advice)
		return a
	}
	task := a.roster.SelectedTask
	next, opened := a.openedBy(place, ag)
	if !opened {
		return a
	}
	next.sideSearch.Focus = false
	if task != "" { // a dispatch row opens its agent showing that dispatch, as a click on it does
		next.roster = Roster{Selected: ag.ID, SelectedTask: task}
		return next.viewingPicked(ag.ID)
	}
	return next.selectOpened(ag.ID)
}

// searchBar is the three rows drawn over the roster: the filter in a box that is lit
// while it holds the keys, with how many of the roster's total it found once there is
// a filter. The counts come from the frame, which ranks the roster once.
func (a App) searchBar(width, found, total int) string {
	focus, query := a.sideSearch.Focus, a.sideSearch.Query // it is drawn, so shown
	edge, lit := HintStyle, query != "" || focus
	if focus {
		edge = AccentStyle
	}
	glyph := HintStyle.Render("⌕ ")
	if lit {
		glyph = AccentStyle.Render("⌕ ")
	}
	count := ""
	if query != "" {
		style := HintStyle
		if !focus {
			style = AccentStyle // a filter standing while the keys are elsewhere stays visible
		}
		count = style.Render(fmt.Sprintf("%d/%d", found, total))
	}
	room := max(max(width-2*boxInset, 1)-ansi.StringWidth(count)-ansi.StringWidth(glyph), 0)
	text := ansi.Truncate(HintStyle.Render(searchHint), room, "…")
	switch {
	case query != "":
		text = TextStyle.Render(tailOf(query, room-ansi.StringWidth(jumpCaret)))
		if focus {
			text += AccentStyle.Render(jumpCaret)
		}
	case focus:
		text = AccentStyle.Render(jumpCaret) + ansi.Truncate(HintStyle.Render(searchHint), max(room-ansi.StringWidth(jumpCaret), 0), "…")
	}
	text = ansi.Truncate(text, room, "") // a caret over a column too narrow for it
	pad := strings.Repeat(" ", max(room-ansi.StringWidth(text), 0))
	rule := strings.Repeat(boxBorder.Top, max(width-2, 0))
	return strings.Join([]string{
		edge.Render(boxBorder.TopLeft + rule + boxBorder.TopRight),
		edge.Render(boxBorder.Left) + " " + glyph + text + pad + count + " " + edge.Render(boxBorder.Right),
		edge.Render(boxBorder.BottomLeft + rule + boxBorder.BottomRight),
	}, "\n")
}

// tailOf is the end of s that fits room cells, the start elided: what was typed last is
// where the caret is. A wide rune is whole or gone.
func tailOf(s string, room int) string {
	if ansi.StringWidth(s) <= room {
		return s
	}
	if room < 1 {
		return ""
	}
	rs, used, at := []rune(s), 1, len([]rune(s)) // the ellipsis takes a cell
	for ; at > 0; at-- {
		w := ansi.StringWidth(string(rs[at-1]))
		if used+w > room {
			break
		}
		used += w
	}
	return "…" + string(rs[at:])
}

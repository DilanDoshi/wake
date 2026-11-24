package ui

// The jump picker: ⌃F or `/jump [filter]` opens a box over the composer; a few
// letters of a name narrow the fleet (agentfind.go) and ↵ opens the cursored agent
// as a click on its sidebar row does. A modal search box in the resume picker's
// shape, read above App.key's switch. Its cursor is an agent's id, never a row.

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/render"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// jumpRows is the results drawn at once; jumpChrome the box's other rows: its two
	// edges, the filter line and the rule under it.
	jumpRows   = 8
	jumpChrome = 4

	// jumpNameCol and jumpTeamCol are the columns a result's name and team are padded to.
	jumpNameCol = 22
	jumpTeamCol = 10

	// jumpKeys is the key line the box draws in its lower edge, and the help carries;
	// jumpClose is the key it does not have room for.
	jumpKeys  = "↑↓ move · ↵ new pane · ⌃B below · ⌃D here · esc"
	jumpClose = "⌃F again closes it"

	noJumpTarget = "no agent to jump to yet. " + newVerb + " starts one"
	jumpTooShort = "this pane is too short for the jump box. Make the window taller, or ⇧↑↓ walks the sidebar"
	jumpNoMatch  = "no agent matches %q - ⌫ to widen"
	jumpHint     = "type to find an agent"
	jumpCaret    = "▏"
)

// JumpPicker is the open box. The zero value is closed.
type JumpPicker struct {
	up     bool
	Pane   string // the pane it was opened over; "" is the room
	Query  string
	Back   int    // runes between the caret and the end of Query; zero is the end
	Cursor string // the cursored agent's id; "" only while nothing matches
}

func (p JumpPicker) Open() bool { return p.up }

// jumpMatches is who the query finds, and how many agents there are to find.
func (a App) jumpMatches() (found []Agent, total int) {
	all := a.fleet.OnRoster()
	return matchAgents(a.jumpPicker.Query, all), len(all)
}

func (a App) jumpAgents() []Agent {
	found, _ := a.jumpMatches()
	return found
}

// jumpIndex is where the cursor's agent stands among found, the first row when it
// is not there.
func jumpIndex(found []Agent, cursor string) int {
	return max(slices.IndexFunc(found, func(ag Agent) bool { return ag.ID == cursor }), 0)
}

// jumpCursorAgent is the agent ↵ would open.
func (a App) jumpCursorAgent() (Agent, bool) {
	found := a.jumpAgents()
	if len(found) == 0 {
		return Agent{}, false
	}
	return found[jumpIndex(found, a.jumpPicker.Cursor)], true
}

// jumpRoom is the rows the pane holding the keys has for the box, under a card it
// is drawing. Below one it cannot be drawn, and a box nobody can see must not take ↵.
func (a App) jumpRoom() int {
	w, h, ok := a.paneSize(a.focus)
	if !ok {
		return 0
	}
	room := a.menuRoom(a.focus, w, h)
	if card := a.cardBlock(a.focus, w, false); card != "" {
		room -= lipgloss.Height(card)
	}
	return room
}

// openJump puts the box up over the pane that holds the keys, with the filter
// already typed. One menu at a time; the draft is left alone.
func (a App) openJump(query string) App {
	switch {
	case len(a.fleet.OnRoster()) == 0:
		notice.Report("%s", noJumpTarget)
		return a
	case a.jumpRoom() < 1:
		notice.Report("%s", jumpTooShort)
		return a
	}
	a = a.closePicker().closeRewind().pageHome(a.focus) // a card's page the wheel moved has the box off screen
	a.statusUI = StatusView{}
	a.jumpPicker = JumpPicker{up: true, Pane: a.focus, Query: query}
	return a.jumpHomed()
}

func (a App) closeJump() App {
	a.jumpPicker = JumpPicker{}
	return a
}

// jumpHomed puts the cursor on the best match, pinned by its id from the first
// look: a report that re-ranks the fleet cannot then move ↵ onto another agent.
func (a App) jumpHomed() App {
	a.jumpPicker.Cursor = ""
	if found := a.jumpAgents(); len(found) > 0 {
		a.jumpPicker.Cursor = found[0].ID
	}
	return a
}

// jump is `/jump [filter]`.
func (a App) jump(arg string) (App, tea.Cmd) {
	return a.clearDraft().openJump(typedQuery("", arg)), nil
}

// jumpKey drives the box: typing and the edit keys (queryedit.go) work the filter, ↑↓ walk the matches, ↵ ⌃B
// and ⌃D open the cursored agent, esc and ⌃F close it. ⌃C closes it and parks the
// agent in front of you - above every guard, so its first press stays visible for the
// ⌃C⌃C kill switch (resumePickerKey's reasoning). Every other key is swallowed.
func (a App) jumpKey(m tea.KeyMsg) (App, tea.Cmd, bool) {
	if !a.jumpPicker.Open() {
		return a, nil, false
	}
	if m.Type == tea.KeyCtrlC {
		model, cmd, _ := a.closeJump().park()
		return model.(App), cmd, true
	}
	if a.focus != a.jumpPicker.Pane || a.jumpRoom() < 1 {
		// withFocus closes the first; a resize can leave no room for the box. Handled,
		// because a key a surface declines is one whose App its caller throws away.
		return a.closeJump(), nil, true
	}
	if a.boxAway(a.focus) {
		// The wheel moved a card's page off the box: this key only brings it back, as a
		// send does, so ↵ opens what the operator can see.
		return a.pageHome(a.focus), nil, true
	}
	switch m.Type {
	case tea.KeyEsc, tea.KeyCtrlF: // alt+esc, two fast escapes, is one close
		return a.closeJump(), nil, true
	case tea.KeyUp:
		return a.moveJump(-1), nil, true
	case tea.KeyDown:
		return a.moveJump(1), nil, true
	case tea.KeyEnter:
		return a.openJumped(App.openInNewPane), nil, true
	case tea.KeyCtrlB:
		return a.openJumped(App.openBelow), nil, true
	case tea.KeyCtrlD:
		return a.openJumped(App.openDMWith), nil, true
	case tea.KeyBackspace, tea.KeySpace, tea.KeyRunes, tea.KeyLeft, tea.KeyRight, tea.KeyHome, tea.KeyEnd,
		tea.KeyDelete, tea.KeyCtrlH, tea.KeyCtrlU, tea.KeyCtrlK:
		return a.editJump(m), nil, true
	}
	return a, nil, true
}

// editJump is one key at the filter's caret. The cursor homes on the best match only
// when the text changed: a caret that moves leaves the agent it was on.
func (a App) editJump(m tea.KeyMsg) App {
	query, back := editedQuery(a.jumpPicker.Query, a.jumpPicker.Back, m)
	changed := query != a.jumpPicker.Query
	a.jumpPicker.Query, a.jumpPicker.Back = query, back
	if changed {
		return a.jumpHomed()
	}
	return a
}

// moveJump walks the matches without wrapping: a cursor that wraps makes the ends of
// a list indistinguishable at a glance.
func (a App) moveJump(by int) App {
	found := a.jumpAgents()
	if len(found) == 0 {
		return a
	}
	a.jumpPicker.Cursor = found[clamp(jumpIndex(found, a.jumpPicker.Cursor)+by, 0, len(found)-1)].ID
	return a
}

// openJumped opens the cursored agent where place says, closes the box and selects
// the agent as a click does. A refused placement leaves the box up, so the keystroke
// costs nothing.
func (a App) openJumped(place func(App, string, string) App) App {
	ag, ok := a.jumpCursorAgent()
	if !ok {
		return a
	}
	next, opened := a.openedBy(place, ag)
	if !opened {
		return a
	}
	return next.closeJump().selectOpened(ag.ID)
}

// openedBy puts an agent on screen where place says, and whether it is on screen after.
// An agent already drawn is only focused, whichever key it was: a refusal (⌃B from a
// lower pane says why itself) is then an agent that was not drawn and still is not.
func (a App) openedBy(place func(App, string, string) App, ag Agent) (App, bool) {
	if a.grid.Has(ag.ID) {
		place = App.openInNewPane
	}
	next := place(a, ag.ID, ag.Name)
	return next, next.grid.Has(ag.ID)
}

// jumpView is the box if it belongs to this pane, told the rows the pane has left
// under what is stacked over it, because the pane clips a menu from the bottom and
// a box drawn taller would hide the row ↵ opens.
func (a App) jumpView(id string, width, height int, over string) string {
	if !a.jumpPicker.Open() || a.jumpPicker.Pane != id || a.focus != id {
		return ""
	}
	room := a.menuRoom(id, width, height)
	if over != "" {
		room -= lipgloss.Height(over)
	}
	return a.jumpBox(width, room)
}

func (a App) jumpBox(width, room int) string {
	w := max(width, minBlockWidth)
	inner := max(w-cardFrameWidth, 1)
	found, total := a.jumpMatches()
	cur := jumpIndex(found, a.jumpPicker.Cursor)
	// The height is the roster's, never the matches': a box that shrank as the filter
	// narrowed would move the room above it, and the composer under it, on every key.
	size := min(room-jumpChrome, jumpRows, total)
	if size < 1 {
		return a.jumpCompact(found, total, cur, w, room)
	}
	var listed []string
	if len(found) == 0 {
		listed = append(listed, detailRow(fmt.Sprintf(jumpNoMatch, a.jumpPicker.Query), inner))
	}
	from, to := windowAround(cur, len(found), size)
	for i := from; i < to; i++ {
		listed = append(listed, a.jumpRow(found[i], i == cur, inner))
	}
	rows := append([]string{jumpFilter(a.jumpPicker.Query, a.jumpPicker.Back), HintStyle.Render(strings.Repeat("─", max(inner-1, 1)))}, filledTo(listed, size)...)
	head := fmt.Sprintf("jump to agent · %d of %d", len(found), total)
	foot := joinedWords(" · ", countWords(from, len(found)-to), jumpKeys)
	body := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(rows, "\n"))
	return titledBox(body, w, BoxStyle.Padding(0), head, foot, AccentStyle, HintStyle)
}

// jumpCompact is the box in a pane with too few rows for it: the filter line over
// the cursored agent, or the agent alone, so ↵ never opens a row the pane did not draw.
func (a App) jumpCompact(found []Agent, total, cur, width, room int) string {
	row := detailRow(fmt.Sprintf(jumpNoMatch, a.jumpPicker.Query), width)
	if len(found) > 0 {
		row = a.jumpRow(found[cur], true, width)
	}
	if room <= 1 {
		return row
	}
	status := fmt.Sprintf("⌕ %s · %d of %d", a.jumpPicker.Query, len(found), total)
	return HintStyle.Render(ansi.Truncate(status, width, ellipsis)) + "\n" + row
}

// jumpFilter is the filter line: the search glyph, what is typed or the hint, a caret.
func jumpFilter(query string, back int) string {
	return AccentStyle.Render("⌕ ") + withCaret(query, back, jumpHint, jumpCaret)
}

// jumpRow is one agent: the cursor, its glyph and name in its own hue, its team, and
// what it is doing - the sidebar's tool line, or what it is waiting on you for.
func (a App) jumpRow(ag Agent, cursored bool, width int) string {
	lead := cardUnchosen
	if cursored {
		lead = cardCursor
	}
	head := ansi.Truncate(rowGlyph(ag, a.fleet.done(ag.ID))+" "+oneLine(ag.Name), jumpNameCol, ellipsis)
	name := identityRowStyle(ag, cursored).Render(head + strings.Repeat(" ", max(jumpNameCol-ansi.StringWidth(head), 0)))
	team := ansi.Truncate(oneLine(ag.Team), jumpTeamCol, ellipsis)
	team = HintStyle.Render(team + strings.Repeat(" ", max(jumpTeamCol-ansi.StringWidth(team), 0)))
	room := max(width-ansi.StringWidth(lead)-jumpNameCol-jumpTeamCol-2, 0)
	return ansi.Truncate(lead+name+" "+team+" "+jumpDoing(ag, room), width, ellipsis)
}

func jumpDoing(ag Agent, room int) string {
	switch {
	case ag.State == rpc.StateBlocked:
		return warnStyle.Render("waiting for you")
	case ag.Tool != "":
		return HintStyle.Render(render.ToolCall(render.Call{Name: ag.Tool, Display: shortArg(ag.ToolArg)}, render.ToolStyle{}, room))
	}
	return HintStyle.Render(ansi.Truncate(idleDetail(ag), room, ellipsis))
}

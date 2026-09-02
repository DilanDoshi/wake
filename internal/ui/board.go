package ui

// The board: the fleet as one row per agent, and nothing else drawn.
//
// Spec §8 names it in one line - "one row per agent with current task and
// progress" - and the owner's 2026-08-12 ruling (phase-4 scope §2c) is what
// bounds it: an OVERVIEW, not panes. No transcripts, ever; a tiled grid of
// thirty conversations is unreadable by arithmetic, and chasing it is the
// multiplexer the non-negotiables refuse. What a row carries instead is what
// the operator triages by - state, activity, the agent's own last words - and
// the verbs are the triage verbs: jump to one, park one, leave.
//
// # A command rather than a key
//
// The spec wrote ⌃B, and ⌃B has since been spent on stacking a pane below
// (keys.go: the ⌃⇧-arrow story). /manager's argument covers the rest: the
// legend is a bijection with App.key's switch, so a chord costs the entry an
// 80-column pane loses first, and the board is opened occasionally rather than
// per minute. The board advertises its own keys on itself, the card's rule.
//
// # A modal, and the picker's lifecycle rather than the card's
//
// Wake opens it, Wake closes it, the daemon never hears of it. It is drawn
// instead of the panes rather than over them - a frame is rows, not layers,
// and a board sharing the frame with four panes is the grid it exists to not
// be. Every key it does not claim closes it and then does its own job, the
// selection rule's shape: nothing decorative may swallow a keystroke, and the
// key that matters most - ⎋ at a runaway agent - is one pane-focus away.
//
// # A second render path narrows the ruling, rather than breaking it
//
// The owner's 2026-08-27 ruling on deferred.md's tiled-board idea: what §2c
// actually refuses is panes you operate inside - transcripts you scroll and
// stdin you type - not the visual shape of a cell. So "an overview, not
// panes" narrows to an overview, not panes you *operate*, and Board grows a
// second render path rather than a second modal: a Tiled bool on this same
// model, boardView branching to a tile renderer (tileView, boardtile.go),
// toggled by ⇥ while the board is up. Four guardrails hold the narrower line
// - cross any of them and this is the multiplexer the non-negotiables
// already refuse:
//
//  1. View-only. No keystroke reaches an agent's stdin from a tile; keys
//     drive the board itself (move, jump in, park, close).
//  2. Bounded live tail, no scrollback. A tile shows live text bounded to its
//     own body (tileTailCap kept per agent - the cell's own height, so a big
//     cell fills and a dense grid keeps little) and nothing you can scroll back
//     through - the tail lives in App.tails (tail.go), gated on Tiled so a
//     closed or row-mode board holds none of it.
//  3. Fixed grid, no per-tile resize, no pane tree. Equal cells sized to fill
//     the frame, no divider to drag, no split, no nesting.
//  4. Act from it, never in it. ↵ and click leave the wall for the agent's
//     real DM rather than working inside the tile.
//
// Full argument: docs/superpowers/specs/2026-08-27-tiled-board-design.md
// §§1-2.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// boardTitle heads the overview; the count beside it is the whole fleet,
	// so a window showing fewer rows is not silently the fleet.
	boardTitle = "BOARD"

	// boardKeyLineRows and boardKeyLineTiles are the board's own legend, one
	// per geometry - the card's rule, that an affordance existing only while a
	// surface is up belongs on the surface, plus "the legend names only keys
	// that work" one surface over: rows have no working ←→ (boardKey's ←/→
	// cases fall through to close the board there), so the row line must not
	// claim it, where tiles' is real (tileNav). Neither brackets anything, so
	// the card-key bijection guard reads no rune from either - which makes the
	// rest a judgment call, held to the same rule: never name a key that is
	// not bound. Leaving, opening and parking lead; ⌃Y is folded into ↵'s
	// entry rather than repeated, since openBoardRow now binds them to the
	// same placement (open beside the room's own focus, a new column).
	boardKeyLineRows  = "esc close  ↵/⌃Y column  ⌃D open here  ⌃B below  ⌃C park  ↑↓ move  ⇥ rows/tiles"
	boardKeyLineTiles = "esc close  ↵/⌃Y column  ⌃D open here  ⌃B below  ⌃C park  ↑↓←→ move  ⇥ rows/tiles"

	// boardChromeRows is what sits above the first row - the title - and the
	// mouse's row arithmetic reads it too: the draw and the mouse measure one
	// number, so a selection cannot land on the wrong row.
	boardChromeRows = 1

	// lastLineCap bounds what a fleet of thirty holds of each agent's prose:
	// a row's worth to draw, not a paragraph to store.
	lastLineCap = 120
)

// boardTakesNoArgument is managerTakesNoArgument's rule one command over: a
// verb firing under a word it did not read does something nobody typed.
var boardTakesNoArgument = boardVerb + " opens the fleet overview. It takes no argument"

// Board is the overview's whole state: whether it is up, and which row the
// cursor holds. The cursor is an agent id rather than an index for the
// roster's own reason - the list re-ranks between frames, and an index would
// hand the cursor to whichever agent moved into the slot.
type Board struct {
	Up       bool
	Selected string
	// Tiled draws the fleet as a grid of live tiles rather than one row per
	// agent. The row view is the default; ⇥ toggles (Task 3).
	Tiled bool
}

// openBoard is /board: it opens the overview over the whole frame.
func (a App) openBoard(arg string) (App, tea.Cmd) {
	a = a.clearDraft()
	if arg != "" {
		notice.Report("%s", boardTakesNoArgument)
		return a, nil
	}
	a.board = Board{Up: true}
	return a, nil
}

func (a App) closeBoard() App {
	a.board = Board{}
	a.tails = nil
	a.boardDMs = nil
	a.boardHistoryAsked = nil
	return a
}

// boardKey reads one key against the board, reporting whether it took it.
//
// Read above everything else App.key owns: nothing under the board is drawn,
// so a card's keys or the roster's arrows acting from under it would be keys
// answering a surface nobody can see. A key it does not claim closes the
// board and is handed back to do its own job - which is why the not-handled
// return still carries the model.
func (a App) boardKey(m tea.KeyMsg) (App, tea.Cmd, bool) {
	if !a.board.Up {
		return a, nil, false
	}
	// The Alt variants are other keys wearing these types - ⌥↵ is the
	// composer's newline, and ⌥↑↓ behave as the bare arrows do (the prompt
	// history) - so they are unclaimed: the board closes and they do their own
	// job in App.key.
	if m.Alt {
		return a.closeBoard(), nil, false
	}
	switch m.Type {
	case tea.KeyTab:
		a.board.Tiled = !a.board.Tiled
		if !a.board.Tiled {
			// Rows draw no transcripts; drop what the wall accumulated.
			a.tails = nil
			a.boardDMs = nil
			a.boardHistoryAsked = nil
		}
		return a, nil, true
	case tea.KeyUp:
		return a.stepBoard(tileUp), nil, true
	case tea.KeyDown:
		return a.stepBoard(tileDown), nil, true
	case tea.KeyLeft:
		if !a.board.Tiled {
			break // rows have no horizontal axis; close and let ← do its job
		}
		return a.stepBoard(tileLeft), nil, true
	case tea.KeyRight:
		if !a.board.Tiled {
			break
		}
		return a.stepBoard(tileRight), nil, true
	case tea.KeyEsc:
		// Close and nothing else. The modal was opened by the operator, so ⎋
		// here is "put it away" - pickerKey's meaning - and must not reach an
		// agent as an interrupt nobody aimed.
		return a.closeBoard(), nil, true
	case tea.KeyEnter:
		// Beside the room, keeping it - the room's own ⌃Y placement, per the
		// spec's table. Unclaimed it would fall through to the roster's pick -
		// a different agent from the row the cursor is on, which is the
		// surprise every one of these cases closes.
		return a.openBoardRow(App.openRight)
	case tea.KeyCtrlD:
		// The room's own "open into the focused pane" - ↵'s old placement,
		// now its own key rather than a duplicate of it.
		return a.openBoardRow(App.openHere)
	case tea.KeyCtrlY:
		return a.openBoardRow(App.openRight)
	case tea.KeyCtrlB:
		return a.openBoardRow(App.openBelow)
	case tea.KeyCtrlC:
		// Park the cursored row and stay up: "park a few" is the ruling's own
		// verb, and closing per park would make it one park per open.
		return a.parkBoardRow()
	}
	return a.closeBoard(), nil, false
}

// stepBoard walks the cursor one step. In rows it is ↑↓ by one; in tiles it is
// the 2-D walk, cols derived from the frame width the tiles are laid out at.
func (a App) stepBoard(dir tileDir) App {
	agents := a.fleet.OnRoster()
	if len(agents) == 0 {
		return a
	}
	cur := a.boardCursor(agents)
	var at int
	if a.board.Tiled {
		at = tileNav(cur, a.boardTileGrid(len(agents)).cols, len(agents), dir)
	} else {
		switch dir {
		case tileUp:
			at = clamp(cur-1, 0, len(agents)-1)
		case tileDown:
			at = clamp(cur+1, 0, len(agents)-1)
		default:
			at = cur
		}
	}
	a.board.Selected = agents[at].ID
	return a
}

// boardMouse is the wheel walking the cursor and a click opening the tile or
// row it lands on - the roster's own meaning for both, in whichever geometry
// is drawn. Everything else the mouse does belongs to surfaces the board is
// not drawing.
func (a App) boardMouse(m tea.MouseMsg) (App, tea.Cmd) {
	switch {
	case m.Button == tea.MouseButtonWheelUp:
		return a.stepBoard(tileUp), nil
	case m.Button == tea.MouseButtonWheelDown:
		return a.stepBoard(tileDown), nil
	case m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft:
		agents := a.fleet.OnRoster()
		i := a.boardHit(m.X, m.Y, agents)
		if i < 0 || i >= len(agents) {
			return a, nil
		}
		return a.closeBoard().openRight(agents[i].ID, agents[i].Name), nil
	}
	return a, nil
}

// boardHit is the agent index a click lands on, or -1. It reads the same
// geometry the draw used, so a click and a tile cannot disagree - the row
// view's boardChromeRows rule, in two dimensions.
func (a App) boardHit(x, y int, agents []Agent) int {
	if a.board.Tiled {
		g := a.boardTileGrid(len(agents))
		start := tileWindowStart(a.boardCursor(agents), len(agents), g.cols, g.rows)
		// The row view's own line < 0 check, taken before the division: Go
		// truncates integer division toward zero rather than flooring, so
		// (y-boardChromeRows)/g.cellH on the title row (y==0) computes 0 rather
		// than a negative row, and a click there would have resolved to the
		// first tile instead of nothing.
		line := y - boardChromeRows
		if line < 0 {
			return -1
		}
		r := line / g.cellH
		c := x / (g.cellW + tileGap)
		if r < 0 || r >= g.rows || c < 0 || c >= g.cols {
			return -1
		}
		return start + r*g.cols + c
	}
	// Row view: bounded to the drawn window before the offset is added -
	// Roster.At's rule. Without the upper bound a click on the key line, the
	// strip or the notice row under it resolved to a valid index past the
	// window and opened an agent that was never on screen.
	line := y - boardChromeRows
	if line < 0 || line >= a.boardRowsVisible() {
		return -1
	}
	row := boardWindowStart(a.boardCursor(agents), len(agents), a.boardRowsVisible()) + line
	if row >= len(agents) {
		return -1
	}
	return row
}

// boardCursor is the cursored row's index in this draw's order: the selected
// agent's, or the top for a selection that is empty or gone - an agent that
// ended between frames must not strand the cursor off the list.
func (a App) boardCursor(agents []Agent) int {
	for i, ag := range agents {
		if ag.ID == a.board.Selected {
			return i
		}
	}
	return 0
}

// openBoardRow is the jump the dashboard exists for, with the room's own
// placement keys: ↵ and ⌃Y a new column beside the room's own focus, ⌃D into
// the focused pane, ⌃B below it. The board closes on the way - the
// destination is a conversation, and a modal left up over it would take the
// keys the conversation needs. All four act on the *cursored* row: unclaimed,
// these keys fell through to the roster's pick, a different agent entirely.
func (a App) openBoardRow(open func(App, string, string) App) (App, tea.Cmd, bool) {
	agents := a.fleet.OnRoster()
	if len(agents) == 0 {
		return a.closeBoard(), nil, true
	}
	ag := agents[a.boardCursor(agents)]
	return open(a.closeBoard(), ag.ID, ag.Name), nil, true
}

// openHere is ⌃D's placement - ↵'s old one - named so the open keys share one
// shape.
func (a App) openHere(sessionID, name string) App { return a.openDMWith(sessionID, name) }

// parkBoardRow is ⌃C on the cursored row, through parkTarget so a blocked
// agent is refused with park.go's own sentence rather than a second one.
func (a App) parkBoardRow() (App, tea.Cmd, bool) {
	agents := a.fleet.OnRoster()
	if len(agents) == 0 {
		return a, nil, true
	}
	ag := agents[a.boardCursor(agents)]
	// Already parked: nothing to park and nothing lost, park()'s own silent
	// case. Reaching parkTarget instead wrote a FramePark at a process that is
	// gone and registered a wait the next report falsely settled as
	// "parked". Ended rows cannot appear here - OnRoster excludes them.
	if a.parkedAgent(ag.ID) {
		return a, nil, true
	}
	next, cmd, _ := a.parkTarget(ag.ID, ag.Name)
	return next, cmd, true
}

// boardRowsVisible is how many rows fit under the title and above the key
// line, measured off the same paneHeight the panes are given.
func (a App) boardRowsVisible() int {
	return max(a.paneHeight()-boardChromeRows-1, 1)
}

// boardWindowStart is which row the window opens on, derived from the cursor
// rather than stored - the sidebars' rule, for the sidebars' reason: the list
// re-ranks between frames, and a stored offset would need maintaining against
// every one. The cursor rides the bottom edge once it is past the first
// window, so walking down reads as scrolling.
func boardWindowStart(cursor, total, visible int) int {
	return clamp(cursor-visible+1, 0, max(total-visible, 0))
}

// boardView is the whole frame's worth of overview: title, one row per agent
// in attention order, the key line.
func (a App) boardView(agents []Agent, width int) string {
	if a.board.Tiled {
		return a.tileView(agents, width)
	}
	visible := a.boardRowsVisible()
	cursor := a.boardCursor(agents)
	start := boardWindowStart(cursor, len(agents), visible)

	nameW, stateW := boardColumns(agents)
	rows := make([]string, 0, visible+2)
	rows = append(rows, mutedLine(fmt.Sprintf("%s — %d agents", boardTitle, len(agents)), width))
	for i := start; i < len(agents) && i < start+visible; i++ {
		rows = append(rows, boardRow(agents[i], nameW, stateW, width, i == cursor))
	}
	// Padded to the height it was given, so the key line and everything under
	// the frame sit where every other view puts them.
	for len(rows) < visible+boardChromeRows {
		rows = append(rows, "")
	}
	rows = append(rows, mutedLine(boardKeyLineRows, width))
	return strings.Join(rows, "\n")
}

// boardColumns is the two padded column widths, measured off the drawn fleet
// rather than stated: a number nothing asserts is wrong by default.
func boardColumns(agents []Agent) (name, state int) {
	for _, ag := range agents {
		name = max(name, ansi.StringWidth(ag.Name))
		state = max(state, ansi.StringWidth(labelOf(ag.State)))
	}
	return name, state
}

// boardRow is one agent: cursor, liveness, name, state, and what it is doing
// in its own words - the tool it wants or runs, then its last line of prose.
func boardRow(ag Agent, nameW, stateW, width int, cursored bool) string {
	lead := cardUnchosen
	if cursored {
		lead = cardCursor
	}
	head := fmt.Sprintf("%s%s %-*s  %-*s  ", lead, rowGlyph(ag), nameW, ag.Name, stateW, labelOf(ag.State))
	style := TextStyle
	switch {
	case cursored:
		style = AccentStyle
	case ag.State == rpc.StateBlocked:
		style = warnStyle
	}
	// oneLine over the assembled row, because half of it is agent-authored -
	// Doing is a TodoWrite activeForm, Tool an argument, LastLine prose - and
	// a control byte in any of them redraws or forges the row. mcp.oneLine's
	// lesson, on the surface where a forged row impersonates a fleet state.
	return style.MaxWidth(width).Render(ansi.Truncate(oneLine(head+boardDetail(ag)), width, ellipsis))
}

// boardDetail is the row's account of the work: what the agent is on now,
// then its last words. Both are the agent's own - Wake asserts nothing about
// progress it cannot see, the turn-end rule.
func boardDetail(ag Agent) string {
	parts := make([]string, 0, 2)
	switch {
	case ag.State == rpc.StateBlocked && ag.Tool != "":
		parts = append(parts, fmt.Sprintf(cardWantsFmt, ag.Tool))
	case ag.Doing != "":
		parts = append(parts, ag.Doing)
	case ag.State == rpc.StateWorking && ag.Tool != "":
		parts = append(parts, ag.Tool)
	}
	if ag.LastLine != "" {
		parts = append(parts, ag.LastLine)
	}
	return strings.Join(parts, cardDot)
}

// lastProseLine is the last non-blank line of a block of prose, bounded to
// lastLineCap runes: a row's worth to draw, not a paragraph to store.
func lastProseLine(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		// oneLine at the store as well as at the draw: the field outlives this
		// draw path, and a stored CR or ESC is a forgery waiting on whichever
		// surface reads it next.
		s := strings.TrimSpace(oneLine(lines[i]))
		if s == "" {
			continue
		}
		if r := []rune(s); len(r) > lastLineCap {
			return string(r[:lastLineCap])
		}
		return s
	}
	return ""
}

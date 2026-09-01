package ui

// The frame: four regions across, a divider between the two panes, and the row
// reserved for the newest thing that has gone wrong.
//
// Nothing here decides anything. Layout.Regions says how wide each column is
// and this draws them, which is what keeps every responsive rule a table test
// over a pure function rather than a golden frame.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/notice"
)

// dividerGlyph is the column between the two panes. A real column, because it
// is a thing a mouse has to be able to hit - see Layout.Hit.
const dividerGlyph = "│"

// dividerRow is the rule between two stacked panes, the horizontal reading of
// the same idea.
const dividerRow = "─"

// View draws the layout: sidebars, the room, the DM beside it, and the notice
// row.
//
// The ranked roster is taken once. Fleet.Agents sorts on every call - 6.8µs at
// 30 agents against a View that costs ~250µs - which is affordable once per
// frame and is exactly the sort of per-frame cost that multiplies by thirty if
// it is paid three times.
func (a App) View() string {
	r := a.regions()
	h := a.paneHeight()
	agents := a.fleet.OnRoster()

	// The board replaces the panes and the sidebars, never layers over them -
	// a frame is rows, not layers. The strip and the notice row keep their
	// places: the strip is in every view by spec §8's own sentence, and the
	// reserved row is where failures go under a TUI.
	if a.board.Up {
		frame := a.boardView(agents, a.layout.Width) + "\n" +
			awarenessStrip(agents, a.stripWorkspace(), a.layout.Width) + "\n" + a.noticeLine()
		// clipMidDrag for the grid path's reason: mid-resize the rows were
		// built for the old width, and a row wider than the window wraps -
		// which is the frame taller than the terminal on every draw.
		return a.clipMidDrag(firstRows(frame, a.layout.Height))
	}

	// A sidebar's rule is drawn out of its own width rather than out of the
	// layout, so every region still starts at the column Layout.Hit says it
	// does. Without them the four regions are columns of text with nothing
	// between them, which is not the frame the spec draws.
	// Every drawn region, then one divider between each neighbouring pair. A
	// sidebar's own rule comes out of its width - which is why it is drawn
	// `-dividerWidth` narrower - so the arithmetic is the same for all of them
	// and every region still starts at the terminal column Layout.Hit says it
	// does. Without the rules the regions are columns of text with nothing
	// between them, which is not the frame the spec draws.
	panels := make([]string, 0, len(r.Cols)+2)
	if r.Groups > 0 {
		panels = append(panels, a.groups.View(agents, r.Groups-dividerWidth, h))
	}
	for i, w := range r.Cols {
		if w > 0 {
			panels = append(panels, a.column(i, w, h))
		}
	}
	if r.Roster > 0 {
		panels = append(panels, a.roster.View(agents, a.fleet.RunningTasks, r.Roster-dividerWidth, h))
	}

	cols := make([]string, 0, 2*len(panels))
	for i, p := range panels {
		if i > 0 {
			cols = append(cols, dividerColumn(h))
		}
		cols = append(cols, p)
	}

	frame := lipgloss.JoinHorizontal(lipgloss.Top, cols...) + "\n" +
		awarenessStrip(agents, a.stripWorkspace(), a.layout.Width) + "\n" + a.noticeLine()
	// Cut to the terminal, because below their own floors the panes stop
	// shrinking rather than drawing a broken box - so at a height under that
	// floor plus these two rows, something has to give. It is the bottom of the
	// pane: a conversation one row shorter is legible, and a frame one row
	// taller than the terminal scrolls the alt screen on every single draw.
	//
	// **firstRows rather than a lipgloss MaxHeight.** The first cut was a style
	// and cost 44% of every frame - a Render re-processes the whole assembled
	// block, every row and every escape, on every draw, to cut rows that only
	// need cutting below about seven. Measured 283µs to 407µs against
	// idle_bench_test.go's own 248-268µs budget for this function. That is the
	// "no work per frame that could be work per change" rule, broken by the fix
	// for a geometry bug. A style would also have been a no-op at the heights
	// it was written for: lipgloss reads a non-positive maximum as *no* maximum.
	frame = firstRows(frame, a.layout.Height)
	return a.clipMidDrag(frame)
}

// regions is the layout's column widths for the grid this model is holding.
//
// Layout knows how wide a column may be and the grid knows how many there are,
// and this is the one place they meet - so there is no second count of the
// columns to disagree with the grid's. A pane drawn for a conversation that is
// not there is a nil dereference inside the draw loop, which is the one place a
// panic costs the whole window rather than one frame.
func (a App) regions() Regions {
	return a.layout.Regions(len(a.grid.Cols), a.focusedCol())
}

// focusedCol is the index of the column holding the keys, which is what decides
// the drawn window at a width too narrow for all of them.
func (a App) focusedCol() int { return a.columnOf(a.focus) }

// column draws one column: a conversation, or two stacked with a rule between.
//
// A column too short for two readable panes draws its upper one alone -
// SplitRows says so - and the lower conversation keeps its transcript for when
// the window is tall enough again, exactly as a column that does not fit keeps
// its width for when it does.
func (a App) column(col, width, height int) string {
	c := a.grid.Cols[col]
	if c.Bottom == "" {
		return a.pane(c.Top, width, height)
	}
	top, bottom := a.layout.SplitRowsIn(col, height)
	if bottom == 0 {
		return a.pane(c.Top, width, top)
	}
	// Each pane is clipped to its own allocation before joining. A pane can draw
	// taller than it was given - a task board is unbounded and is not in the
	// composer's growth bound - and an unclipped overflow in the top pane pushes
	// the bottom one down, so App.View's final clip cuts the bottom pane's own
	// rows. On a focused card that means its key line off screen while
	// cardFullyDrawn, measuring that pane's own height, still calls it drawn - an
	// answerable permission decision nobody can see. Clipping here keeps one
	// pane's overflow out of the other.
	return lipgloss.JoinVertical(lipgloss.Left,
		firstRows(a.pane(c.Top, width, top), top),
		HintStyle.Render(strings.Repeat(dividerRow, width)),
		firstRows(a.pane(c.Bottom, width, bottom), bottom),
	)
}

// pane draws one conversation, the room included. "" is the room, which is the
// same convention the grid and the focus both use.
func (a App) pane(id string, width, height int) string {
	if id == "" {
		return a.roomPane(width, height)
	}
	return a.dmPane(id, width, height)
}

// cardFullyDrawn reports whether every row of the card the *focused* pane draws
// reaches the screen, which is what makes its keys safe to read.
//
// Not "is there a card": the card is pinned in the menu block above the composer
// (see menuBlock) and DM.menuRows clips that block from the bottom to what is
// left above the transcript's floor. The card is first in the block, so it is
// the last thing clipped - but its key line is the card's own last row, so a
// card taller than the room draws with its keys cut, still blocking an agent and
// still looking answerable. So the whole card has to fit, and paneFloor is
// baseChrome plus a transcript row.
//
// The card is a menu, and a pinned menu drops the composerGap - it hugs the box
// where an empty pane keeps a blank row above it - so the card gets that row
// back. paneFloor measures the stored DM, which carries no menu, so it charges
// the gap; without adding it back a card that fills the pane exactly is called
// clipped, and its answer keys stay live with nothing on screen to account for
// them while the agent stays blocked. The reverse of the stale-bar direction
// below, and menuposition_test.go now sweeps both.
//
// paneFloor is asked at the pane width, which is not a nicety: menuRows runs
// inside DM.View, after withBar has re-rendered the status bar from the live
// agent, and a status push updates the fleet without re-sizing the stored DM -
// so the cached bar can lag by a row. A floor measured against the stale bar
// would call a card fully drawn whose key line the draw then clips, leaving the
// answer keys live with nothing on screen to account for them.
//
// Measured against the focused pane rather than the room, which is the fix
// itself: below dmTakeoverColumns the room has no width, so a room-only
// measurement said "not drawn" about every card while a conversation was open -
// correctly, and with no other surface to say it about.
func (a App) cardFullyDrawn() bool {
	width, height, ok := a.focusedPane()
	if !ok {
		return false
	}
	card := a.cardBlock(a.focus, width)
	room := height - a.paneFloor(a.focus, width) + composerGap
	return card != "" && room > 0 && lipgloss.Height(card) <= room
}

// focusedPane is the width and height of the pane holding the keys, and whether
// it is drawn at all. A column the window slid past has no width, and neither
// has the lower half of a column too short to split.
func (a App) focusedPane() (width, height int, ok bool) {
	col := a.focusedCol()
	r := a.regions()
	if col >= len(r.Cols) || r.Cols[col] <= 0 {
		return 0, 0, false
	}
	if a.grid.Cols[col].Bottom == "" {
		return r.Cols[col], a.paneHeight(), true
	}
	top, bottom := a.rowsOf(col)
	if a.grid.Cols[col].Bottom == a.focus {
		return r.Cols[col], bottom, bottom > 0
	}
	return r.Cols[col], top, true
}

// paneFloor is the shortest this conversation draws, measured rather than
// stated: both panes' chrome grows a row when a heartbeat or a status bar
// arrives, and a floor one row out is a card drawn one row too tall.
//
// Asked of the same values the panes draw from, never of the stored ones - a
// legend carrying a mode is what decides whether the composer's hint wraps, and
// a floor measured without it is a floor for a pane nobody draws. The bar is the
// one the draw will show: dmFor carries the live agent but leaves the bar
// cached, and withBar re-renders it from that agent at the pane width - the same
// refresh DM.View does before it clips the menu block. width is the room's for
// the room, which has no bar of its own.
func (a App) paneFloor(id string, width int) int {
	if id == "" {
		return a.roomFor().minHeight()
	}
	return a.dmFor(id).withBar(max(width, minComposerWidth)).minHeight()
}

// roomFor is the room as roomPane draws it. The legend names the mode of
// whoever ⇧⇥ would act on, which the room cannot know - it has no agent of its
// own - so it is set for the draw and not held, and cannot go stale against the
// pick that decides it.
func (a App) roomFor() Room {
	// The working line is a fact about the fleet and the room holds no roster,
	// so it is set for the draw for the mode's reason and cannot go stale.
	return a.room.WithComposer(a.room.Composer().
		WithMode(a.hintMode()).
		WithArms(a.armsFor(""))).WithWorking(a.fleet.OnRoster()).WithWriting(a.answerTitle(""))
}

// dmFor is one conversation as dmPane draws it: its agent's own facts, and a
// legend naming that agent's mode. A DM has exactly one agent, so there is no
// ambiguity to resolve - the room's is the case that needs a rule.
func (a App) dmFor(id string) DM {
	var d DM
	if p := a.dms[id]; p != nil {
		d = *p
	}
	if agent, ok := a.fleet.Agent(id); ok {
		d.Agent = agent
	}
	return d.WithComposer(d.Composer().WithMode(a.modeOf(id)).WithArms(a.armsFor(id)))
}

// armsFor is which arm this pane's legend names.
//
// ⎋'s belongs to the pane that holds the keys, so a second conversation on
// screen goes on advertising ⎋ as the key that stops its own turn. **⌃O's
// belongs to every pane**, because it is one arm over the whole window and ↵
// finishes it wherever the keys are - a legend that withheld it would be a pane
// still advertising `↵ send` while ↵ closes the workspace.
//
// esc and rewind split the one escArmed bit by rewindArmable: an idle, empty
// conversation's second ⎋ opens the rewind picker rather than clearing a
// draft, and the two are mutually exclusive by construction (rewindArmable
// needs an empty composer, clearsOnEscape needs a non-empty one), so exactly
// one of the two is ever true here. See rewind.go and escape.go.
func (a App) armsFor(id string) legendArms {
	focused := a.escArmed && a.focus == id
	rewind := focused && a.rewindArmable()
	return legendArms{esc: focused && !rewind, rewind: rewind, detach: a.detachArmed}
}

// cardBlock is the card this pane draws, or "" for a pane with no ask to put.
func (a App) cardBlock(id string, width int) string {
	card, ok := a.cardOf(id)
	if !ok {
		return ""
	}
	agent, _ := a.fleet.Agent(card.AgentID)
	// The pause is the focused pane's own: cardKey reads a.cardOf(a.focus), so
	// a draft gates exactly that pane's card, and pausing another pane's on it
	// would blame a draft that is not in that pane's composer.
	return a.cards.View(card, width, agent, id == a.focus && !a.composerEmpty())
}

// cardOf is the ask a pane is putting: a conversation puts its own agent's, and
// the room puts none.
//
// The room used to take the oldest ask whose agent had no pane on screen, and
// that is the rule this reverses. It is wrong for the surface twice over. The
// room holds one card's worth of rows, so a fleet with several agents blocked
// at once - the case this build exists for - saw one of them and a count of the
// rest; and it interleaves thirty agents, so the question arrived stripped of
// the turn that raised it. What says somebody is waiting is the roster row and
// the strip's "N need you", and ⌃X opens the conversation the card is in.
//
// Drawn-ness does not enter into it any more: a conversation puts its ask
// whether its column is on screen or slid past, so moving the keys moves
// nothing else.
func (a App) cardOf(id string) (Card, bool) {
	if id == "" {
		return Card{}, false
	}
	return a.cards.For(id)
}

// drawnConversations answers "does this agent's conversation have a pane with a
// width on screen", with the layout resolved once.
//
// Once, and that is the whole reason it hands back a function rather than being
// one: App.wants asks it per streamed token, at ~1,300/s across a fleet. A
// Regions per token is the per-agent cost on a ticker the non-negotiables price
// at thirty.
//
// Drawn, not open: below dmTakeoverColumns a conversation can be in the grid and
// off the frame, and a preview accumulated for a pane nobody can see is the
// whole cost that gate exists to refuse. It decides no card - an ask belongs to
// its agent's conversation whether that column is on screen or slid past. See
// App.cardOf.
func (a App) drawnConversations() func(string) bool {
	r := a.regions()
	return func(agentID string) bool {
		if agentID == "" {
			return false
		}
		for i, c := range a.grid.Cols {
			if i >= len(r.Cols) || r.Cols[i] <= 0 {
				continue
			}
			if c.Top == agentID {
				return true
			}
			if c.Bottom == agentID {
				if _, bottom := a.rowsOf(i); bottom > 0 {
					return true
				}
			}
		}
		return false
	}
}

func (a App) roomPane(width, height int) string {
	// The selection is set for the draw and not held, the same way the mode is -
	// App owns the one selection, and it changes what a row looks like rather
	// than how many rows there are, so paneFloor does not ask for it.
	room := a.roomFor().WithSelection(a.selectionIn("")).WithComposerSelection(a.composerSelectionIn(""))

	// Handed over whole: the pane clips it, because only the pane knows what is
	// left after the rest of its chrome. See Room.menuRows.
	menu, ask := a.menuBlock("", width)
	return room.WithMenu(menu).WithAsk(ask).View(width, height)
}

// menuBlock is everything a pane pins between its transcript and its composer,
// in the order it is stacked: the ask that pane is putting, then the picker,
// then the rewind picker, then the completion menu. "" is a pane with none of
// them up.
//
// All four are things waiting on a keypress, and all belong at the query
// bar - the completion menu follows the word being typed, the picker was opened
// by the draft, the rewind picker by esc esc, and the card is answered by
// typing an option's digit. Pinning them above the *transcript* instead put
// them rows away from where they are acted on, which is what "the /effort menu
// appears at the top instead of the query bar" reported.
//
// The card is first, so it is the row the pane keeps when there is not room for
// all of it - DM.menuRows clips from the bottom - and its keys are the last row
// of it, which is what cardFullyDrawn holds. The completion menu is last,
// directly above the composer, where the word it completes is.
//
// One block, handed to the pane whole through WithMenu, because only the pane
// knows what its transcript can spare; it clips and draws it. Nothing goes above
// the transcript any more, which is what keeps mouse.go's startSelection a plain
// `top`: a screen row is a transcript line with nothing to offset it by.
func (a App) menuBlock(id string, width int) (string, bool) {
	card := a.cardBlock(id, width)
	stack := above(above(card, a.pickerView(width, id)), a.rewindView(width, id))
	return above(stack, a.completionView(width, id)), card != ""
}

// pickerView is the menu if it belongs to this pane, and "" otherwise.
//
// It is drawn over the composer that opened it, which is the pane holding the
// keys - so a picker opened in a DM does not appear in the room, where its ↵
// would go somewhere else.
func (a App) pickerView(width int, id string) string {
	if !a.picker.Open() || a.focus != id {
		return ""
	}
	return a.picker.View(width)
}

// rewindView is the rewind picker if it belongs to this pane, and ""
// otherwise - pickerView's own reason, matched on Session rather than on
// focus: the reply that opens it can arrive after the operator has moved on,
// and it is drawn only in the conversation it answered for. See
// rewindTargetsArrived.
func (a App) rewindView(width int, id string) string {
	if !a.rewind.Open() || a.rewind.Session != id {
		return ""
	}
	return a.rewind.View(width, a.agentName(a.rewind.Session))
}

// above joins two blocks, either of which may be absent. lipgloss.JoinVertical
// would render an empty string as a blank row, which is a gap on screen.
func above(top, bottom string) string {
	switch {
	case top == "":
		return bottom
	case bottom == "":
		return top
	}
	return lipgloss.JoinVertical(lipgloss.Left, top, bottom)
}

// dmPane is one conversation column, told what its agent is doing so the title
// can say a session is parked and at what effort it runs.
//
// It pins whatever the room pins, by the same rule and through the same bound.
// A conversation that drew none was the whole of the question bug: below
// dmTakeoverColumns the room has no width, so an ask that lived only there was
// on no surface at all, and the agent stayed blocked with nothing to answer.
func (a App) dmPane(id string, width, height int) string {
	// Handed over whole, for roomPane's reason. See DM.menuRows.
	menu, ask := a.menuBlock(id, width)
	return a.dmFor(id).WithSelection(a.selectionIn(id)).WithComposerSelection(a.composerSelectionIn(id)).
		WithMenu(menu).WithAsk(ask).WithWriting(a.answerTitle(id)).WithCompacting(a.compactingSince(id)).View(width, height)
}

// transcriptRows is how many of a pane's rows are conversation, measured the
// way the draw measures them rather than read back off the model.
//
// A pane's stored transcript was sized by the last *geometry* change, and its
// chrome moves without one: View re-lays a copy when a card goes up, a
// completion menu appears or the draft grows a row, and that copy is drawn and
// dropped. So the stored height is too big by exactly the rows the menu took,
// and a mouse trusting it reads a card's own rows as conversation. Sized here
// through the same SetSize the draw would call, so the two cannot disagree.
func (a App) transcriptRows(id string, width, height int) int {
	// The ask flag is the draw's business, not the measurement's: this needs the
	// rows the menu takes, and a card contributes those whether or not the pane
	// goes on to treat it as answerable.
	menu, _ := a.menuBlock(id, width)
	if id == "" {
		return a.roomFor().WithMenu(menu).SetSize(width, height).tr.height
	}
	return a.dmFor(id).WithMenu(menu).SetSize(width, height).tr.height
}

// dividerColumn is the one column between the panes, drawn its whole height so
// the two sides do not run together.
func dividerColumn(height int) string {
	rows := make([]string, max(height, 0))
	for i := range rows {
		rows[i] = dividerGlyph
	}
	return HintStyle.Render(strings.Join(rows, "\n"))
}

// firstRows cuts a block to n rows, by finding the nth newline.
//
// A byte scan rather than a style: it allocates nothing and reads only as far
// as it cuts, where lipgloss re-renders the whole block. See App.View.
func firstRows(s string, n int) string {
	if n <= 0 {
		return ""
	}
	at := 0
	for range n {
		i := strings.IndexByte(s[at:], '\n')
		if i < 0 {
			return s
		}
		at += i + 1
	}
	return s[:at-1]
}

// clipMidDrag keeps the frame inside a terminal the panes have not been
// re-wrapped for yet.
//
// Clipping is not the same as re-wrapping and is not meant to be - it costs an
// ANSI-aware truncate per row instead of the whole conversation through
// glamour, and it is what keeps the frame from being wider than the window it
// is drawn in, which would wrap every row and scroll the alt screen on every
// draw. The text re-flows the moment the drag stops. See resized.
func (a App) clipMidDrag(frame string) string {
	if a.pending.width > 0 && a.pending.width < a.layout.Width {
		return lipgloss.NewStyle().MaxWidth(a.pending.width).Render(frame)
	}
	return frame
}

// noticeLine is the reserved row: empty when nothing has failed.
//
// One row means one row. lipgloss's MaxWidth truncates each line but preserves
// the line *count*, so a notice carrying a newline would make the frame one
// row taller than the window and scroll the alt screen on every draw - and the
// text is not ours: it is a daemon's error frame, which reaches it from an
// agent's stderr. See oneLine.
func (a App) noticeLine() string {
	n, ok := notice.Latest()
	if !ok {
		return ""
	}
	return warnStyle.MaxWidth(max(a.layout.Width, minComposerWidth)).Render(oneLine(noticePrefix + n.String()))
}

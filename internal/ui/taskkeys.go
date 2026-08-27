package ui

// Walking the dispatch list, and opening what the cursor is on.
//
// # Why ⌃N and ⌃P rather than ↑↓
//
// Claude Code's footer takes `up`/`ctrl+p` and `down`/`ctrl+n` for exactly this
// job - measured off the shipped 2.1.233 binary's Footer key context. Wake can
// have the second pair and not the first: `↑↓` here are the roster's, and they
// open the sidebar as they move (App.pickAgent), so a conversation claiming
// them would make the fleet cursor mean something different depending on which
// pane had the focus - and the roster is where ⌃C parks and ⎋ interrupts from.
//
// So the keys are the alternates Claude Code already binds for this list, which
// is parity on the half that fits. One legend entry covers the pair, the way
// `↑↓` and `⇞⇟` each cover two.
//
// # Where the cursor lives
//
// On the DM, because the list does. Each conversation has its own dispatches
// and its own place in them, and a cursor on the App would move under somebody
// every time the focus changed panes.

// taskCursorNone is the zero value and means nothing is selected: the list is
// drawn, and no row on it is the one a key would be about.
//
// Rows are 1-based against it, so the conversation's own row is 1 and its
// dispatches follow. That keeps "nothing selected" as the zero value of the
// field rather than a sentinel somebody has to remember to set.
const taskCursorNone = 0

// walkTasks moves the cursor in the focused conversation's list.
//
// The key is taken whether or not there is a list, which is what every other
// case in App.key does - ⌃X with nothing blocked and ⌃G with no groups both
// take theirs and do nothing. A key that is bound only sometimes cannot be in
// the legend, and the legend is what tells an operator this surface exists.
//
// It costs the composer bubbles' own ctrl+n/ctrl+p line navigation, the way
// ⌃D, ⌃F, ⌃W and ⌃B already cost it theirs. The arrows still walk a draft.
func (a App) walkTasks(delta int) App {
	d, ok := a.dms[a.focus]
	rows := a.fleet.Tasks(a.focus).Rows()
	if !ok || len(rows) == 0 {
		return a
	}
	return a.withDM(a.focus, d.moveCursor(len(rows), delta))
}

// moveCursor walks the list and stops at both ends.
//
// No wrapping: a cursor that jumps from the last dispatch back to the
// conversation opens the wrong thing on a key somebody is holding down, and
// what it opens is a pane swap rather than a scroll.
//
// From nothing, either direction lands on the conversation's own row. It is
// the row that leads back, so it is the one place where a mistaken keypress
// costs nothing.
func (d DM) moveCursor(rows, delta int) DM {
	last := rows + 1
	if d.taskCursor == taskCursorNone {
		d.taskCursor = 1
		return d
	}
	d.taskCursor = min(max(d.taskCursor+delta, 1), last)
	return d
}

// openTask opens whatever the cursor is on: a dispatch's transcript, or the
// conversation itself for the row that leads back. It reports whether there
// was anything to open.
//
// A row that cannot be opened - a background shell, or a kind this build does
// not model - moves the cursor and does nothing here. Task.Openable is the one
// place that decides it, so the keys cannot disagree with the list about which
// rows have a conversation behind them.
func (a App) openTask() (App, bool) {
	d, ok := a.dms[a.focus]
	if !ok || d.taskCursor == taskCursorNone {
		return a, false
	}
	if d.taskCursor == 1 {
		return a.withDM(a.focus, d.Viewing("")), true
	}
	rows := a.fleet.Tasks(a.focus).Rows()
	i := d.taskCursor - 2
	if i < 0 || i >= len(rows) || !rows[i].Openable() {
		return a, false
	}
	return a.withDM(a.focus, d.Viewing(rows[i].Dispatch)), true
}

// clearTaskCursor takes the selection back, and is called from esc alongside
// the interrupt rather than instead of it - the rule the text selection
// already keeps, for the reason it keeps it: a stale highlight that swallows
// the key which stops a runaway agent is an agent that does not stop.
func (a App) clearTaskCursor() App {
	d, ok := a.dms[a.focus]
	if !ok || d.taskCursor == taskCursorNone {
		return a
	}
	next := *d
	next.taskCursor = taskCursorNone
	return a.withDM(a.focus, next)
}

// cursorOn says whether the cursor is on a row, by the same 1-based numbering
// moveCursor walks. Read by the draw, so the list shows where the keys are.
func (d DM) cursorOn(row int) bool { return d.taskCursor != taskCursorNone && d.taskCursor == row }

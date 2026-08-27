package ui

// withRoom installs a changed room and clears only a room selection whose
// endpoints no longer name retained rendered lines.
func (a App) withRoom(room Room) App {
	moves := room.lineMoves
	room.lineMoves = nil
	a.room = room
	if a.sel.pane != "" || (a.sel == (selection{}) && !a.selecting) {
		return a
	}
	if moves != nil {
		anchor, anchorOK := moves.translate(a.sel.anchor.line)
		head, headOK := moves.translate(a.sel.head.line)
		if !anchorOK || !headOK {
			a.sel, a.selecting = selection{}, false
			return a
		}
		a.sel.anchor.line, a.sel.head.line = anchor, head
	}
	first, end := room.tr.first(), room.tr.lines.len()
	if a.sel.anchor.line < first || a.sel.anchor.line >= end || a.sel.head.line < first || a.sel.head.line >= end {
		a.sel, a.selecting = selection{}, false
	}
	return a
}

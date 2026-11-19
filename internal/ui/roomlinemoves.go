package ui

// Where each room line's rows sit, and how a rebuild moves them: what a reader's
// scroll offset and a selection are translated by when the room re-lays itself
// (Before, relaid). Split from chat.go by subject.

type roomSpan struct{ first, rows int }

type roomLineMove struct{ from, to, rows int }

type roomLineMoves []roomLineMove

func (moves roomLineMoves) translate(line int) (int, bool) {
	for _, move := range moves {
		if line >= move.from && line < move.from+move.rows {
			return move.to + line - move.from, true
		}
	}
	return 0, false
}

// roomSpans records each event block's absolute rendered range. IDs survive a
// history merge; positions deliberately do not.
func (r Room) roomSpans(lines []roomLine) map[uint64]roomSpan {
	spans := make(map[uint64]roomSpan, len(lines))
	at := r.tr.lines.first()
	if !r.reclaimed {
		at += len(blockLines(roomBanner(r.blockWidth()), true))
	}
	for _, line := range lines {
		if line.id != 0 && line.rows > 0 {
			spans[line.id] = roomSpan{first: at, rows: line.rows}
		}
		at += line.rows
	}
	return spans
}

// roomLineMoves translates every retained rendered row from one Before layout
// to the next. Boundary rows have identities too: the banner while it exists,
// and the reclamation marker after it replaces the banner.
func lineMovesBetween(old Room, oldSpans map[uint64]roomSpan, next Room, nextSpans map[uint64]roomSpan) roomLineMoves {
	moves := make(roomLineMoves, 0, len(oldSpans)+1)
	for id, before := range oldSpans {
		after, ok := nextSpans[id]
		if !ok {
			continue
		}
		moves = append(moves, roomLineMove{from: before.first, to: after.first, rows: min(before.rows, after.rows)})
	}
	switch {
	case old.reclaimed && next.reclaimed:
		moves = append(moves, roomLineMove{from: old.tr.first(), to: next.tr.first(), rows: 1})
	case !old.reclaimed && !next.reclaimed:
		rows := min(len(blockLines(roomBanner(old.blockWidth()), true)), len(blockLines(roomBanner(next.blockWidth()), true)))
		moves = append(moves, roomLineMove{from: old.tr.lines.first(), to: next.tr.lines.first(), rows: rows})
	}
	return moves
}

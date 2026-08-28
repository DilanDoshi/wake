package ui

// Expanding a room response: ⌃E on the whole room, a click on one line.
//
// The Room-side half of the ⌃E feature whose App-level key handling lives in
// expand.go. A long agent reply draws as a pointer in the room (agentSaid's
// boundary); these methods flip it open - globally with ⌃E (toggleExpandAll),
// one line with a click (toggleLine, hit-tested by blockAt) - and keep the
// expand set bounded as lines leave the room (forgetExpanded, keptExpanded).
// Split from chat.go to keep that file under the hard max; no logic changed.

// blockAt is the room line whose rendered rows contain an absolute transcript
// line, and whether one does. Built from roomSpans so it reads the same layout
// the click landed on; the banner and any row past the last block match
// nothing.
func (r Room) blockAt(line int) (roomLine, bool) {
	lines := r.said.slice(r.said.first(), r.said.len())
	spans := r.roomSpans(lines)
	for _, l := range lines {
		if s, ok := spans[l.id]; ok && line >= s.first && line < s.first+s.rows {
			return l, true
		}
	}
	return roomLine{}, false
}

// toggleLine flips whether the response drawn on an absolute transcript line is
// expanded in place, and reports whether the line held an expandable one - a
// click's gesture. A short reply, your own turn and a marker are not
// expandable and are left alone.
//
// The reader keeps their place, which is the whole difference from ⌃E:
// expanding renumbers only the lines below the click, so lineMoves carries a
// scrolled offset and any live selection across the re-render the way Before
// does. A click to read one long reply must not also throw the reader to the
// newest line.
func (r Room) toggleLine(line int) (Room, bool) {
	l, ok := r.blockAt(line)
	if !ok || !roomCollapsible(l.ev, r.blockWidth()) {
		return r, false
	}
	old := r
	following := old.tr.atBottom()
	held := r.said.slice(r.said.first(), r.said.len())
	oldSpans := r.roomSpans(held)

	// Copied on write: Room is handed around by value, so flipping in place
	// would mutate the map a discarded copy still draws from.
	expanded := make(map[uint64]bool, len(r.expanded)+1)
	for k, v := range r.expanded {
		expanded[k] = v
	}
	if expanded[l.id] {
		delete(expanded, l.id)
	} else {
		expanded[l.id] = true
	}
	r.expanded = expanded

	first := r.said.first()
	base := r.tr.lines.first()
	combined := append([]roomLine(nil), held...)
	blocks := renderRoom(r, combined)
	r.said = chunked[roomLine]{base: first, n: first}.append(combined...)
	r.tr = r.tr.replaceFrom(blocks, base, r.tr.prefix)
	newSpans := r.roomSpans(combined)
	r.lineMoves = lineMovesBetween(old, oldSpans, r, newSpans)
	switch {
	case following:
		r.tr = r.tr.toBottom()
	default:
		if moved, ok := r.lineMoves.translate(old.tr.scroll); ok {
			r.tr.scroll = moved
			break
		}
		// The offset lands nowhere only when the reader had scrolled into rows
		// this toggle removed - a collapse from deep inside an expanded reply.
		// Anchor to the block's own new top rather than the transcript start, so
		// folding what you are reading keeps it in view instead of throwing the
		// reader thousands of lines back to the oldest history.
		if s, ok := newSpans[l.id]; ok {
			r.tr.scroll = min(max(s.first, r.tr.first()), r.tr.bottom())
		} else {
			r.tr.scroll = r.tr.first()
		}
	}
	return r, true
}

// toggleExpandAll flips ⌃E's global expand and re-renders. It returns the
// reader to the newest line the way the DM's ⌃E does, and for the same reason:
// the offset a scroll held points at lines that have renumbered, so following
// the conversation is a truer answer than restoring it. Collapsing also drops
// the per-line opens a click made, so ⌃E means show-everything then
// hide-everything, exactly as DM.toggleExpanded does with its own per-item
// opens. No lineMoves: the keystroke that reaches this already cleared the
// selection (App.cleared), so there is nothing to carry across the re-render.
func (r Room) toggleExpandAll() Room {
	r.expandAll = !r.expandAll
	if !r.expandAll {
		r.expanded = nil
	}
	r.lineMoves = nil
	lines := r.said.slice(r.said.first(), r.said.len())
	first := r.said.first()
	base := r.tr.lines.first()
	blocks := renderRoom(r, lines)
	r.said = chunked[roomLine]{base: first, n: first}.append(lines...)
	r.tr = r.tr.replaceFrom(blocks, base, r.tr.prefix).toBottom()
	return r
}

// forgetExpanded drops the per-line opens of blocks that are leaving the room,
// so the expand set stays bounded by what is retained rather than by every
// response ever clicked. Copied on write for Room's value contract, and only
// when an evicted block was actually open - reclamation runs once per event
// past the cap, so its common path (nothing opened, or nothing opened among the
// evicted) must allocate nothing.
func (r Room) forgetExpanded(evicted []roomLine) Room {
	if len(r.expanded) == 0 {
		return r
	}
	drop := false
	for _, l := range evicted {
		if r.expanded[l.id] {
			drop = true
			break
		}
	}
	if !drop {
		return r
	}
	expanded := make(map[uint64]bool, len(r.expanded))
	for k, v := range r.expanded {
		expanded[k] = v
	}
	for _, l := range evicted {
		delete(expanded, l.id)
	}
	r.expanded = expanded
	return r
}

// keptExpanded rebuilds the open set to only the ids still present in lines, for
// the merge path where ids both leave and are re-minted. Returns the same map
// when there is nothing to keep track of and nil once the last open is gone, so
// an empty set never carries an allocation. A live open survives, because a held
// live line keeps its id across a merge; a restored one may not, which is the
// documented, harmless re-collapse.
func keptExpanded(expanded map[uint64]bool, lines []roomLine) map[uint64]bool {
	if len(expanded) == 0 {
		return expanded
	}
	kept := make(map[uint64]bool, len(expanded))
	for _, l := range lines {
		if expanded[l.id] {
			kept[l.id] = true
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

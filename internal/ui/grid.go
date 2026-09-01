package ui

// Which conversations are on screen and where. Pure: ids in, ids out, no widths
// and nothing drawn - layout.go spends the columns this names.
//
// Spec §8's shape, and its bound is the design: columns left to right, each
// holding one conversation or two stacked. **Not a pane tree.** The bound is
// what keeps this out of §17's "arbitrary pane layouts" and out of the
// multiplexer the non-negotiables rule against - a second split is a slot to
// take, never a third row to grow.
//
// The grid says what is *on screen*. App.dms says what exists, and the two are
// different facts: a conversation displaced from a slot keeps its transcript
// and comes back holding what it held.

import "slices"

// Column is one column: a conversation, and optionally a second under it.
type Column struct {
	// Top is the conversation in the column, "" for the room.
	Top string

	// Bottom is the conversation under Top, or "" for an unsplit column. The
	// room is only ever a Top - it is Cols[0] and cannot be closed - so "" is
	// unambiguous here rather than needing a flag beside it.
	Bottom string
}

// Grid is the columns, left to right. Cols[0] holds the room.
type Grid struct{ Cols []Column }

// NewGrid is the room alone, which is every window before anything is opened.
func NewGrid() Grid { return Grid{Cols: []Column{{}}} }

// Has reports whether a conversation is on screen, in either slot.
func (g Grid) Has(id string) bool { return g.at(id) >= 0 }

// at is the index of the column holding a conversation, or -1.
func (g Grid) at(id string) int {
	return slices.IndexFunc(g.Cols, func(c Column) bool { return c.Top == id || c.Bottom == id })
}

// Panes is every conversation on screen, left to right and top to bottom -
// which is the order a reader's eye takes them in, and the order ⇥ walks.
func (g Grid) Panes() []string {
	out := make([]string, 0, len(g.Cols))
	for _, c := range g.Cols {
		out = append(out, c.Top)
		if c.Bottom != "" {
			out = append(out, c.Bottom)
		}
	}
	return out
}

// OpenRight puts a conversation in a new column immediately right of the one
// holding `beside`.
//
// Immediately right rather than at the end: the new column is about the pane
// somebody is in, so it belongs next to it. Appending would put it past
// conversations they opened earlier and never looked at again.
func (g Grid) OpenRight(beside, id string) Grid {
	if id == "" || g.Has(id) {
		// Already on screen: the caller moves the focus instead. One conversation
		// in two panes is two composers writing to one agent.
		return g
	}
	return g.withCols(slices.Insert(slices.Clone(g.Cols), g.after(beside), Column{Top: id}))
}

// OpenBelow splits the column holding `under` and puts a conversation in the
// lower slot.
//
// A column that is already split has its lower slot taken rather than growing a
// third row. That is the bound, and the displaced conversation is only off
// screen: App.dms still holds it.
func (g Grid) OpenBelow(under, id string) Grid {
	next, ok := g.CanOpenBelow(under)
	if !ok || id == "" || g.Has(id) {
		return g
	}
	at := next.at(under)
	cols := slices.Clone(next.Cols)
	cols[at].Bottom = id
	return g.withCols(cols)
}

// CanOpenBelow answers whether there is a row under a pane, and gives back the
// grid the split would apply to.
//
// Separate from OpenBelow because the refusal has to reach the operator: a key
// that does nothing and says nothing is the failure the legend rule exists for,
// and the caller turns a false here into the notice naming ⌃Y instead.
func (g Grid) CanOpenBelow(under string) (Grid, bool) {
	at := g.at(under)
	// Asked as "is this the upper pane" rather than "is it the lower one", which
	// reads the room right: the room is "", so an unsplit column's empty Bottom
	// would otherwise answer that the room is its own lower half.
	return g, at >= 0 && g.Cols[at].Top == under
}

// after is the index a new column beside this conversation goes at. A pane the
// grid does not hold puts it on the right-hand end, which is where a
// conversation opened from the sidebar with nothing focused belongs.
func (g Grid) after(beside string) int {
	if at := g.at(beside); at >= 0 {
		return at + 1
	}
	return len(g.Cols)
}

// HasDMs reports whether anything but the room is on screen, which is what the
// right sidebar yields to and comes back from.
func (g Grid) HasDMs() bool { return len(g.Cols) > 1 || g.Cols[0].Bottom != "" }

// Replace swaps the conversation in the slot holding `at` for another, which is
// what ⌃D does: open here, in the pane I am in.
//
// The room's slot is never replaced - Cols[0].Top is the room - so a caller
// asking for that gets the grid back unchanged and places the conversation
// somewhere it can go instead.
func (g Grid) Replace(at, id string) Grid {
	i := g.at(at)
	if id == "" || i < 0 || g.Has(id) {
		return g
	}
	cols := slices.Clone(g.Cols)
	switch {
	case cols[i].Bottom == at:
		cols[i].Bottom = id
	case at == "":
		return g
	default:
		cols[i].Top = id
	}
	return g.withCols(cols)
}

// Close takes a conversation off screen.
//
// The room is refused: it is Cols[0] and the one pane always drawn, which is
// what "the group chat is the product; the panes are substrate" means
// structurally rather than as a convention every caller has to remember.
//
// A closed upper pane promotes the lower one instead of taking the column with
// it - the pane nobody closed is still a conversation somebody wanted open.
func (g Grid) Close(id string) Grid {
	at := g.at(id)
	if id == "" || at < 0 {
		return g
	}
	c := g.Cols[at]
	cols := slices.Clone(g.Cols)
	switch {
	case c.Bottom == id:
		cols[at].Bottom = ""
	case c.Bottom != "":
		cols[at] = Column{Top: c.Bottom}
	default:
		cols = slices.Delete(cols, at, at+1)
	}
	return g.withCols(cols)
}

// Neighbour is where the keys go when a pane closes: whatever takes the space
// it was using.
//
// The lower slot's space goes back to the pane above it, an upper slot's to the
// one promoted into it, and a whole column's to the column on its left - which
// walks toward the room, so this always names a pane that is still drawn. A
// focus on a pane nobody can see is the split-brain composer App.withFocus
// exists to prevent.
func (g Grid) Neighbour(id string) string {
	at := g.at(id)
	if id == "" || at < 0 {
		return ""
	}
	switch c := g.Cols[at]; {
	case c.Bottom == id:
		return c.Top
	case c.Bottom != "":
		return c.Bottom
	case at > 0:
		return g.Cols[at-1].Top
	default:
		return ""
	}
}

// Direction is which way ⇧←→ moves the keys: Left and Right step a column.
// Vertical movement is no longer bound - ⇧↑↓ move the roster now.
type Direction int

const (
	Left Direction = iota
	Right
)

// Toward is the pane one step from a conversation in a direction, and whether
// there is one at all.
//
// Two return values because "" is the room *and* "nothing that way", and a
// single one cannot tell a move onto the group chat from a move to refuse.
// Neighbour dodges the same ambiguity by treating both as "focus the room",
// which is right when a pane has just closed and wrong for a key somebody aimed.
//
// A horizontal step keeps the row it was on wherever the column it lands in has
// one, so ⇧→ out of a lower pane is one move rather than two.
func (g Grid) Toward(id string, d Direction) (string, bool) {
	at := g.at(id)
	if at < 0 {
		return "", false
	}
	// The id has to be checked before the slot: an unsplit column's Bottom is
	// "", which is also the room, so a bare `Bottom == id` calls the room a
	// lower pane. lower is what keeps a horizontal step on the row it was on.
	lower := id != "" && g.Cols[at].Bottom == id

	to := at - 1
	if d == Right {
		to = at + 1
	}
	if to < 0 || to >= len(g.Cols) {
		return "", false
	}
	if c := g.Cols[to]; lower && c.Bottom != "" {
		return c.Bottom, true
	}
	return g.Cols[to].Top, true
}

// withCols is the one write path, so a Grid handed out never shares a backing
// array with the one it came from. An append on a value receiver otherwise
// writes into a row a discarded copy still points at - the bug App.withDM and
// Fleet both carry a comment about.
func (g Grid) withCols(cols []Column) Grid {
	g.Cols = cols
	return g
}

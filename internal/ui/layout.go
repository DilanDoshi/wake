package ui

// The bounded layout: the two sidebars, the conversation columns between them,
// and where the dividers sit.
//
// Pure. A width, some flags and a column count in, column widths out - it knows
// nothing about content and draws nothing, which is what makes every responsive
// rule here a table test rather than a golden frame. grid.go owns *which*
// conversation is in a column; this owns how wide it is.
//
// # Why a conversation opens beside another and not under it by default
//
// Because vertical space is the scarce resource. A conversation is a vertical
// list, and a top/bottom split halves the lines each side can show. The cost of
// choosing side-by-side is stated plainly because it is the opposite of the
// obvious: a vertical split is the *more* expensive one for this codebase.
// DM.SetSize re-wraps only on a width change, so stacking gives both panes one
// width, one glamour cache and one re-wrap, while side-by-side gives two widths
// and two of everything - and a draggable divider turns a width change from
// "occasionally, when I resize my terminal" into "whenever I want more room".
//
// That is a deliberate trade of render cost for reading benefit, and it is the
// right direction for a tool somebody stares at all day. It is also why ⌃Y is
// the ordinary key and ⌃B the one you reach for: stacking is offered because
// two short conversations side by side read worse than two tall ones, not
// because it is cheap.
//
// What it obliges is the settle: a divider drag goes through the same 80ms
// quiet the window drag does, so a drag costs one re-wrap per pane rather than
// one per column. App owns that - see geometry.go.

import "math"

// Region is one part of the frame a click can land in.
type Region int

const (
	RegionNone Region = iota
	RegionGroups
	RegionPane
	RegionDivider
	RegionRoster
)

// String names a region in a failure message.
func (r Region) String() string {
	switch r {
	case RegionGroups:
		return "groups"
	case RegionPane:
		return "pane"
	case RegionDivider:
		return "divider"
	case RegionRoster:
		return "roster"
	default:
		return "none"
	}
}

const (
	// groupsWidth and rosterWidth are the two sidebars, in columns: workspaces
	// on the left, what agents are doing on the right.
	groupsWidth = 16

	// rosterWidth is 24 rather than the brief's 20: one of those columns is the
	// rule between this sidebar and the pane beside it, and the row it draws is
	// a glyph, a name, an unread badge and - indented under them - what the
	// agent is doing, which is the column that ran out of room first.
	rosterWidth = 24

	// dmTakeoverColumns is where a second column stops being worth having.
	//
	// Below this a split leaves 42 columns a pane with both sidebars open,
	// which the brief's own measurement calls poor, and 52 with the right one
	// closed, which it calls marginal. So below it one conversation takes the
	// whole width and the rest keep their scrollback for when there is room
	// again. §8 already collapses the groups sidebar between 120 and 160; this
	// is the same rule with one more trigger.
	dmTakeoverColumns = 120

	// groupsCollapseColumns and sidebarsHideColumns are §8's own breakpoints:
	// at or above 160 all four regions show, between 120 and 160 the groups
	// sidebar collapses, below 100 both sidebars auto-hide.
	groupsCollapseColumns = 160
	sidebarsHideColumns   = 100

	// minPaneWidth is the narrowest a conversation pane is allowed to become.
	// Below it a message is a column of single words, which is not reading.
	minPaneWidth = 24

	// minPaneHeight is the shortest a stacked pane may be: a composer and a
	// line of conversation over it. Below twice this plus the rule between
	// them, a column draws its upper pane alone - the row equivalent of the
	// takeover, and for the same reason.
	minPaneHeight = 6

	// dividerWidth is the column a divider occupies, and dividerHeight the row
	// the rule between stacked panes takes. Real cells, because a divider is a
	// thing a mouse has to be able to hit.
	dividerWidth  = 1
	dividerHeight = 1

	// defaultSplit is a stacked column nobody has dragged the rule of: even,
	// because neither of two conversations is the primary one.
	defaultSplit = 0.5

	// defaultWeight is a column nobody has dragged: every column weighs the
	// same, so the space divides evenly. Neither pane is the primary one - the
	// brief's amendment supersedes the body on exactly this point, and room and
	// DM are peers.
	defaultWeight = 1.0
)

// Layout is the terminal and what is currently shown in it.
type Layout struct {
	Width  int
	Height int

	// ShowRoster is moved by ⌃R and by nothing else. Opening a conversation used
	// to close the right sidebar for the ~10 columns it bought, which put the
	// sidebar away under the cursor that had just been used to pick from it.
	//
	// ShowGroups is the left workspaces sidebar. Hidden for now: the app never
	// sets it and ⌃G is gone, so it stays false, but the geometry it drives is
	// kept (see Regions) for the multi-groupchat version. groups.go.
	ShowGroups bool
	ShowRoster bool

	// Weights is each column's share of the space it divides with its
	// neighbours, indexed by column.
	//
	// A column with no entry, or a non-positive one, weighs defaultWeight - so
	// a layout nobody has dragged divides evenly, and opening a column needs no
	// entry written for it. That is what keeps this from being a second copy of
	// the column count that could disagree with the grid's.
	Weights []float64

	// Rows is the upper pane's share of each stacked column's height, indexed by
	// column, read the same way: unset or out of [0,1] is an even split.
	//
	// Per column rather than one figure for all of them, because two stacks on
	// screen are two different conversations - a diff worth the rows next to a
	// question worth two lines.
	Rows []float64
}

// Regions is the column count for each part of the frame. Zero means not drawn.
type Regions struct {
	Groups int

	// Cols is each conversation column's width, left to right, one entry per
	// column in the grid. Cols[0] is the room's. A zero is a column the width
	// cannot afford - kept in the grid, not drawn, and back the moment there is
	// room, which is how the takeover has always treated the room.
	Cols []int

	Roster int
}

// Room is the room's column, which is always the first one.
func (r Regions) Room() int {
	if len(r.Cols) == 0 {
		return 0
	}
	return r.Cols[0]
}

// Drawn is how many conversation columns have a width.
func (r Regions) Drawn() int {
	var n int
	for _, w := range r.Cols {
		if w > 0 {
			n++
		}
	}
	return n
}

// Regions computes the layout for a grid of `cols` columns with the keys in
// `focused`. The sum of every field plus its dividers is exactly Width whenever
// anything is drawn at all.
func (l Layout) Regions(cols, focused int) Regions {
	r := Regions{Cols: make([]int, max(cols, 1))}
	rest := l.Width

	// Sidebars first, and they give way first. They are context; the
	// conversation is the work.
	if l.ShowRoster && l.Width >= sidebarsHideColumns {
		r.Roster = rosterWidth
		rest -= rosterWidth
	}
	if l.ShowGroups && l.Width >= groupsCollapseColumns {
		r.Groups = groupsWidth
		rest -= groupsWidth
	}

	// How many columns the width can carry. Below the takeover exactly one is
	// drawn however many are open, which is the rule the room and a single DM
	// have always followed, read for any number of them.
	fit := 1
	if l.Width >= dmTakeoverColumns {
		fit = fits(rest, len(r.Cols))
	}
	first := window(len(r.Cols), fit, focused)

	space := rest - (fit-1)*dividerWidth
	for i, w := range l.share(space, first, fit) {
		r.Cols[first+i] = w
	}
	return r
}

// fits is how many columns of minPaneWidth the space carries, at least one and
// never more than there are.
func fits(space, cols int) int {
	n := (space + dividerWidth) / (minPaneWidth + dividerWidth)
	return clamp(n, 1, cols)
}

// window is the leftmost column drawn when not all of them fit.
//
// It stays at the left while it can, so the room keeps its place rather than
// scrolling off the moment a third conversation opens, and shifts right only as
// far as it must to keep the focused column on screen. A pane holding the keys
// that nobody can see is the split-brain composer App.withFocus prevents.
func window(cols, fit, focused int) int {
	if focused < fit {
		return 0
	}
	return clamp(focused-fit+1, 0, max(cols-fit, 0))
}

// share divides the space between the drawn columns by weight, honouring the
// floor and spending every column.
func (l Layout) share(space, first, n int) []int {
	if n == 1 {
		// One column takes what there is, floor included. The floor keeps
		// *neighbouring* panes readable, and a lone column has no neighbour to
		// be unreadable beside - applying it to a 0-column terminal would make
		// the frame wider than the window it is drawn in.
		return []int{max(space, 0)}
	}
	var total float64
	for i := range n {
		total += l.weight(first + i)
	}

	// Rounded on the *running* total rather than per column, which is what makes
	// a drag stable: a column's width is the difference between two cumulative
	// edges, so an edge nobody moved lands on the same cell whatever happened to
	// the columns beyond it. Rounding each column on its own and spending the
	// remainder afterwards moved the room by a cell when the divider two columns
	// over was dragged. It also sums to `space` by construction rather than by
	// repair - a frame one column wide of the terminal wraps every row.
	out := make([]int, n)
	var seen float64
	prev := 0
	for i := range out {
		seen += l.weight(first + i)
		edge := int(math.Round(float64(space) * seen / total))
		out[i], prev = edge-prev, edge
	}
	return floorPanes(out, space)
}

// floorPanes lifts any column below the floor and takes it from the widest.
//
// It cannot fire on a drag - DragDivider clamps its own pair and leaves every
// other column's edge alone - so this is the resize case: weights an operator
// skewed, carried into a terminal narrow enough that the small side falls under
// the floor. `fits` guarantees the space for the drawn columns exists; this is
// what hands it to them.
func floorPanes(out []int, space int) []int {
	for {
		low := -1
		for i, w := range out {
			if w < minPaneWidth {
				low = i
				break
			}
		}
		if low < 0 {
			return out
		}
		at := widestCol(out)
		give := min(minPaneWidth-out[low], out[at]-minPaneWidth)
		if give <= 0 {
			// Nothing can give: at this width the frame is clipped rather than
			// re-wrapped. See App.clipMidDrag.
			return out
		}
		out[low], out[at] = out[low]+give, out[at]-give
	}
}

// widestCol is the index of the widest column, the first of them on a tie.
func widestCol(out []int) int {
	at := 0
	for i, w := range out {
		if w > out[at] {
			at = i
		}
	}
	return at
}

// weight is a column's share, with anything unset or out of range reading as
// even. Most columns are: only a dragged divider writes one.
func (l Layout) weight(col int) float64 {
	if col < 0 || col >= len(l.Weights) || l.Weights[col] <= 0 {
		return defaultWeight
	}
	return l.Weights[col]
}

// SplitRows divides a column's height between a stacked pair, less the rule
// between them.
//
// A column too short for two readable panes draws only its upper one - the row
// equivalent of the takeover, and the second return says so rather than
// handing back a height nothing can be drawn in.
func SplitRows(height int) (top, bottom int) { return splitRowsAt(height, defaultSplit) }

// SplitRowsIn is SplitRows for one column of a layout, honouring a rule the
// operator has dragged.
func (l Layout) SplitRowsIn(col, height int) (top, bottom int) {
	return splitRowsAt(height, l.rowShare(col))
}

// splitRowsAt divides the height at a fraction, clamped so neither pane is
// below the floor. The rule's own row belongs to neither.
func splitRowsAt(height int, share float64) (top, bottom int) {
	if height < 2*minPaneHeight+dividerHeight {
		return height, 0
	}
	rest := height - dividerHeight
	top = clamp(int(math.Round(float64(rest)*share)), minPaneHeight, rest-minPaneHeight)
	return top, rest - top
}

// rowShare is the upper pane's share of a column, with anything unset or out of
// range reading as even - which is most columns, since only a dragged rule
// writes one.
func (l Layout) rowShare(col int) float64 {
	if col < 0 || col >= len(l.Rows) || l.Rows[col] <= 0 || l.Rows[col] >= 1 {
		return defaultSplit
	}
	return l.Rows[col]
}

// DragRule moves the rule inside a stacked column to a terminal row, clamped so
// neither pane becomes unreadable.
//
// The vertical twin of DragDivider, and deliberately the same shape: a fraction
// is stored rather than a row count, so a terminal that gets taller gives both
// panes their share instead of pinning one and handing every new row to the
// other.
func (l Layout) DragRule(col, y, height int) Layout {
	if _, bottom := l.SplitRowsIn(col, height); bottom == 0 {
		// Nothing to divide. Reachable with a drag in flight: the terminal can
		// lose the rows while a button is held.
		return l
	}
	rest := height - dividerHeight
	top := clamp(y, minPaneHeight, rest-minPaneHeight)

	rows := make([]float64, max(len(l.Rows), col+1))
	copy(rows, l.Rows)
	rows[col] = float64(top) / float64(rest)
	l.Rows = rows
	return l
}

// DragDivider moves the divider right of column `at` to a terminal column,
// clamped so neither of the two panes it separates becomes unreadable.
//
// Only the pair either side of it moves: a drag is a statement about one
// boundary, and spreading it over every column would shuffle panes the operator
// is not touching.
func (l Layout) DragDivider(r Regions, at, x int) Layout {
	if at < 0 || at+1 >= len(r.Cols) || r.Cols[at] == 0 || r.Cols[at+1] == 0 {
		// No divider to move. Reachable with a drag in flight: the terminal can
		// cross the takeover width while a button is held, and motion events
		// keep arriving after the second pane has gone.
		return l
	}
	space := r.Cols[at] + r.Cols[at+1]
	left := clamp(x-edgeOf(r, at), minPaneWidth, space-minPaneWidth)

	// The pair's combined weight is preserved, so the columns beyond them keep
	// the share they had.
	pair := l.weight(at) + l.weight(at+1)
	w := make([]float64, max(len(l.Weights), at+2))
	for i := range w {
		w[i] = l.weight(i)
	}
	w[at] = pair * float64(left) / float64(space)
	w[at+1] = pair - w[at]
	l.Weights = w
	return l
}

// edgeOf is the terminal column a conversation column starts at.
func edgeOf(r Regions, at int) int {
	x := r.Groups
	for i := 0; i < at && i < len(r.Cols); i++ {
		if r.Cols[i] > 0 {
			x += r.Cols[i] + dividerWidth
		}
	}
	return x
}

// Hit names the part of the frame a terminal column falls in, and which column
// or divider it was, so a click can focus a pane and a drag can find a divider.
//
// The regions tile the terminal exactly, so every column inside it belongs to
// one of them and the default catches a column outside. A negative one returns
// early: it would otherwise satisfy the first `x < edge` and land in the
// leftmost region, which is a click nobody made.
func (l Layout) Hit(r Regions, x int) (Region, int) {
	if x < 0 || x >= l.Width {
		return RegionNone, 0
	}
	if r.Groups > 0 && x < r.Groups {
		return RegionGroups, 0
	}
	if r.Roster > 0 && x >= l.Width-r.Roster {
		return RegionRoster, 0
	}
	edge := r.Groups
	for i, w := range r.Cols {
		if w == 0 {
			continue
		}
		if x < edge+w {
			return RegionPane, i
		}
		edge += w
		if x < edge+dividerWidth {
			return RegionDivider, i
		}
		edge += dividerWidth
	}
	return RegionNone, 0
}

// PaneLeft is the terminal column a conversation column starts at.
//
// It accumulates exactly as Hit does, and for the same reason: a zero-width
// column is skipped without taking a divider with it. Hit answers "what is at
// this x" and cannot also hand back the edge it stopped at, which is what turns
// a terminal coordinate into one local to the pane - so this walks the same
// widths to answer the other question.
func (l Layout) PaneLeft(r Regions, col int) int {
	edge := r.Groups
	for i, w := range r.Cols {
		if w == 0 {
			continue
		}
		if i == col {
			return edge
		}
		edge += w + dividerWidth
	}
	return edge
}

func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }

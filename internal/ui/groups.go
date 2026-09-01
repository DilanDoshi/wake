package ui

// The left sidebar: the conversation list. One row per workspace, with what
// has accumulated in it.
//
// # A workspace is a directory, not a branch
//
// Label is the git branch, so two repositories both on `main` would be one
// row - and running agents across several repositories at once is the case
// this sidebar exists for. rpc.SessionStatus grew Dir for exactly this, and
// its own comment says so. The row reads as the directory's basename; the
// branch belongs beside an agent's name, where it already is.
//
// This is the v1 stand-in for spec §11's configured groups. Nothing here
// persists and nothing is named by hand: the grouping is derived from where
// each session was started, which the daemon already reports.
//
// # The count is what the sidebar is for
//
// The amendment: unread has to be real, per-agent in the room and
// per-workspace here, because an hour inside a DM must not cost you what
// accumulated. Nothing is counted in this file - Fleet.Observe already
// increments Agent.Unread as the room's own lines go past, including the quiet
// turn-end marker, which for 8 of 52 recorded turns is the only line the room
// shows. Summing what the fold already decided is what keeps the badge and the
// room from ever disagreeing about what arrived.
//
// So the badge is spent before the name is: a repository name longer than the
// column loses letters, and never the number. A name is something you already
// know; the count is the thing you came back for.
//
// # Widths
//
// The only widths this is asked for are Layout.Regions().Groups, which is
// groupsWidth or zero - zero being how the layout spells a collapsed sidebar.
// Zero draws nothing at all rather than falling back to a floor: the panes are
// joined side by side and Layout.Hit resolves a mouse column against the widths
// the layout reserved, so a sidebar drawn where none was reserved moves the
// conversation, the divider and every click to its right.

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	// selectedMark and rowIndent are the two leads a row can have. Both are two
	// columns wide, so the names line up whether or not the cursor is on one -
	// a list whose text shifts sideways as the cursor moves is one the eye has
	// to re-find on every keystroke.
	selectedMark = "● "
	rowIndent    = "  "

	// badgeGap is the one column that keeps a count from touching the name it
	// belongs to when the name runs the width of the column.
	badgeGap = 1

	// unnamedWorkspace is a session the daemon could not name a directory for.
	// An empty Dir is legitimate - rpc.SessionStatus says so - and its agents
	// still have lines arriving, so they get a row rather than being dropped
	// into a workspace nobody can see.
	unnamedWorkspace = "(unknown)"

	// maxBadge is the largest unread count drawn exactly. Past it the badge
	// reads 99+, because a 16- or 20-column sidebar cannot spend four of them
	// on a number nobody needs precisely - "more than you are going to read" is
	// the whole of what a large count means.
	maxBadge = 99
)

// Workspace is one directory and what is happening in it.
type Workspace struct {
	Dir    string
	Name   string
	Agents int
	Unread int
}

// Groups is the sidebar and which workspace the cursor is on.
//
// Selected is a directory rather than a name, because that is what a row is:
// basenames collide - a worktree beside its origin, two checkouts of one
// repository - and a cursor keyed on the name would light both rows up.
type Groups struct{ Selected string }

// Workspaces groups agents by the directory they were started in, in a stable
// order.
//
// Sorted by directory, and that is two properties rather than one. Go
// randomises map iteration, so a sidebar assembled straight out of a map
// reshuffles on every frame - and the input is Fleet.Agents(), which is in
// attention order, so a list that kept its input's order would reshuffle
// whenever any agent changed state. Sorting is what makes the list something a
// hand can aim at.
func Workspaces(agents []Agent) []Workspace {
	by := make(map[string]Workspace, len(agents))
	for _, a := range agents {
		w, ok := by[a.Cwd]
		if !ok {
			w = Workspace{Dir: a.Cwd, Name: workspaceName(a.Cwd)}
		}
		w.Agents++
		// Read, never recounted: Fleet.Observe decides what counts as unread
		// as the room's lines go past, and a second opinion here would be a
		// second answer to draw beside the first.
		w.Unread += a.Unread
		by[a.Cwd] = w
	}

	out := make([]Workspace, 0, len(by))
	for _, w := range by {
		out = append(out, w)
	}
	slices.SortFunc(out, func(a, b Workspace) int { return strings.Compare(a.Dir, b.Dir) })
	return out
}

// workspaceName is what a directory is called. The basename, because that is
// what a person calls a repository; the full path is not something a
// 16-column sidebar can show and not something anybody reads off one.
func workspaceName(dir string) string {
	if dir == "" {
		return unnamedWorkspace
	}
	return filepath.Base(dir)
}

// View draws one row per workspace, measuring exactly width by height.
//
// It pads to the full height rather than returning the rows it has: the panes
// are joined side by side, so a short column pulls whatever is under it up
// across the whole frame.
func (g Groups) View(agents []Agent, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	all := Workspaces(agents)
	room := height
	if len(all) > height {
		// One row goes to the count, so a column that cannot hold the list says
		// so rather than appearing to end. See Roster.moreLine, which is the
		// same decision on the other sidebar and the reason this one is not a
		// scroll: a workspace list has no cursor, so there is nothing to follow
		// and nothing to reach the rows below with. The count is what an
		// operator can act on - the room has the whole fleet. (This sidebar is
		// hidden for now; see the header.)
		room = height - 1
	}

	rows := make([]string, 0, height)
	for _, ws := range all[:min(len(all), room)] {
		rows = append(rows, g.row(ws, width))
	}
	if hidden := len(all) - len(rows); hidden > 0 {
		// Padded to the column like every other row here, not merely cut. A row
		// narrower than the column makes the whole block narrower - g.row pads
		// for that reason and this one has to as well - and lipgloss then joins
		// the room several columns early while Layout.Hit still uses the width
		// the layout reserved, so every click lands on the wrong region.
		rows = append(rows, moreRow(hidden, width))
	}

	blank := strings.Repeat(" ", width)
	for len(rows) < height {
		rows = append(rows, blank)
	}
	return strings.Join(rows, "\n")
}

// row is one workspace: a lead, the name, and what has accumulated in it.
//
// The badge is measured out of the column first and the name takes what is
// left, so a long repository name loses letters rather than its count. It is
// right-aligned so the counts form a column the eye can run down, which is the
// only reason to have numbers in a sidebar at all.
//
// Padded here rather than by a lipgloss width: a style's Width wraps a line
// that overruns, and a wrapped row would push the next workspace off the
// bottom of a column whose height the layout has already spent.
//
// The MaxWidth at the end is the only part of this a caller cannot reach: the
// arithmetic above already lands on the column exactly at every width where a
// lead, a name and a count all fit, and Layout.Regions asks for groupsWidth or
// nothing. It is kept because View's contract is unconditional, and the
// measurement test carries widths 1 to 3 so that it is a guard something can
// fail rather than a comment with syntax.
func (g Groups) row(ws Workspace, width int) string {
	lead, style := rowIndent, HintStyle
	if ws.Dir == g.Selected {
		lead, style = selectedMark, AccentStyle
	}

	badge := ""
	room := width - ansi.StringWidth(lead)
	if ws.Unread > 0 {
		badge = unreadBadge(ws.Unread)
		room -= ansi.StringWidth(badge) + badgeGap
	}

	name := ansi.Truncate(ws.Name, max(room, 0), ellipsis)
	gap := width - ansi.StringWidth(lead) - ansi.StringWidth(name) - ansi.StringWidth(badge)
	line := lead + name + strings.Repeat(" ", max(gap, 0)) + badge
	return style.MaxWidth(width).Render(line)
}

// unreadBadge is a count, bounded to three columns.
//
// Both sidebars draw one and it is meant to be spelled once. The right
// sidebar (roster.go) is being built in parallel and carries a byte-identical
// copy of this function and of maxBadge; whichever branch merges second
// deletes one of the two. They are the same function on purpose, so that
// resolution is a deletion rather than a decision.
func unreadBadge(n int) string {
	if n > maxBadge {
		return fmt.Sprintf("%d+", maxBadge)
	}
	return fmt.Sprintf("%d", n)
}

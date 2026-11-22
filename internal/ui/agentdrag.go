package ui

// Pressing an agent's row in the sidebar and dragging it out onto the panes.
//
// A press arms the drag and takes no screen selection: that drag moves the agent
// now. Motion past dragSlop makes it live, and it is drawn at the pointer
// (agentdragdraw.go) with nothing laid out again until release - the divider
// drags' rule. A click, which never went live, opens the agent in a new column, or
// in place of the pane shown where the window fits one (openInNewPane); a Shift-click, where the terminal delivers one, opens it
// below the focused pane as ⌃B does. A live drag drops where dropZoneFor says,
// and esc, any other key, a resize, a lost release or letting go back on the
// sidebar leaves everything as it was.

import (
	"regexp"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	// dragVerb and dragKeys are the help's row for the drag: the gesture and the
	// keys that end it.
	dragVerb = "drag an agent from the sidebar"
	dragKeys = "drop on a pane's lower half to open below it, anywhere else for a new column (in place of the pane shown, in a window that fits one) · esc cancels, and so does letting go on the sidebar"
)

// dragSlop is how many cells the pointer moves from the press before it is a
// drag and not a click with a shaky hand.
const dragSlop = 2

// zoneKind is what a drop does.
type zoneKind int

const (
	zoneNone   zoneKind = iota + 1 // the sidebar: letting go cancels; the zero value is no zone yet
	zoneColumn                     // a new column at the grid's right end, or the pane shown in a one-column window
	zoneBelow                      // under zone.pane, by Grid.OpenBelow's rules
	zoneFocus                      // the agent is on screen already: its pane takes the keys
)

// dropZone is where a release would land: its kind, and the pane it is about.
type dropZone struct {
	kind zoneKind
	pane string
}

// agentDrag is a sidebar agent row held down. The zero value is no drag.
type agentDrag struct {
	id, name string
	x0, y0   int  // the press
	shifted  bool // pressed with Shift: a click opens below the focused pane
	live     bool // past the slop: drawn at the pointer, and a release drops
	x, y     int  // the pointer, while live
	zone     dropZone
	outline  []edit // the zone's outline, built when the zone changes
}

// dragSource is the agent row a press landed on, resolved at press: the roster
// re-ranks on every report, so a release must not open whoever slid under it.
func (a App) dragSource(y int) (agentDrag, bool) {
	spot, ok := a.clickedRow(y)
	if !ok || spot.header != "" || spot.dispatch != "" || spot.agent.ID == "" {
		return agentDrag{}, false
	}
	return agentDrag{id: spot.agent.ID, name: spot.agent.Name}, true
}

// pressedBy is a left press: the agent row under it arms a drag, and Shift held
// makes a click on it open below the focused pane.
func (a App) pressedBy(m tea.MouseMsg) App {
	a = a.pressed(m.X, m.Y)
	a.dragged.shifted = a.dragged.id != "" && m.Shift
	return a
}

// dragMouse is a release or a motion while an agent row is held. A release also
// ends a divider drag whose own release was lost, as every release did before.
func (a App) dragMouse(m tea.MouseMsg) App {
	if m.Action == tea.MouseActionRelease {
		return a.letGo().dropped(m.X, m.Y)
	}
	return a.dragMoved(m)
}

// dragMoved follows the pointer. It goes live once it has left the slop, then
// recomputes the zone and rebuilds the outline only when the zone changed.
func (a App) dragMoved(m tea.MouseMsg) App {
	if m.Button == tea.MouseButtonNone {
		a.dragged = agentDrag{} // a release that never arrived
		return a
	}
	d := a.dragged
	if !d.live && max(abs(m.X-d.x0), abs(m.Y-d.y0)) < dragSlop {
		return a
	}
	d.live, d.x, d.y = true, m.X, m.Y
	if zone := a.dropZoneFor(d.id, m.X, m.Y); zone != d.zone {
		d.zone, d.outline = zone, a.outlineOf(zone, d.id)
	}
	a.dragged = d
	return a
}

// dropped ends the drag on release: a click opens, a live drag lands where the
// pointer let go.
func (a App) dropped(x, y int) App {
	d := a.dragged
	a.dragged = agentDrag{}
	if _, ok := a.fleet.Agent(d.id); !ok {
		return a // it ended between press and release; opening it would focus nothing
	}
	zone := a.dropZoneFor(d.id, x, y)
	switch {
	case !d.live && d.shifted && !a.grid.Has(d.id):
		a = a.openBelow(d.id, d.name)
	case d.live && zone.kind == zoneBelow:
		a = a.withFocus(zone.pane).openBelow(d.id, d.name)
	case !d.live || zone.kind != zoneNone: // a click, a new column (or the shown pane), an open agent's pane
		a = a.openInNewPane(d.id, d.name)
	default:
		return a // back on the sidebar
	}
	if !a.grid.Has(d.id) {
		return a // refused: nothing opened, so the cursor stays where it was
	}
	// Both fields together, always: an opened agent must not keep the previous
	// agent's dispatch.
	a.roster.Selected, a.roster.SelectedTask = d.id, ""
	return a.viewingPicked(d.id)
}

// strayReport is what an Esc leaves behind when the pump reads it and the next
// mouse report in one go: Bubble Tea decodes `\x1b\x1b[<32;61;6M` as alt+esc and
// the runes `[<32;61;6M`, which would land in the composer. A drag in flight is
// exactly when reports are arriving, so the Esc that ends one can bring a report
// with it.
var strayReport = regexp.MustCompile(`^\[<\d+;\d+;\d+[Mm]$`)

// dragKey is a key arriving with a drag armed: any key ends it, and esc is only
// that - it must not also interrupt the pane that holds the keys. An esc read as
// alt+esc drops the mouse report that came with it (see strayReport).
func (a App) dragKey(m tea.KeyMsg) (App, bool) {
	if a.afterDragEsc {
		a.afterDragEsc = false
		if m.Type == tea.KeyRunes && strayReport.MatchString(string(m.Runes)) {
			return a, true
		}
	}
	if a.dragged.id == "" {
		return a, false
	}
	a.dragged = agentDrag{}
	a.afterDragEsc = m.Type == tea.KeyEsc && m.Alt
	return a, m.Type == tea.KeyEsc
}

// dropZoneFor is what a release at a cell would do with an agent. Over the
// sidebar's rows it is nothing; an agent already on screen is focused wherever
// else it is dropped; the bottom half of a pane that can split opens below it;
// everything else is a new column (the pane shown, in a window that fits one), as
// a click is.
func (a App) dropZoneFor(id string, x, y int) dropZone {
	r := a.regions()
	region, col := a.layout.Hit(r, x)
	switch {
	case region == RegionRoster:
		return dropZone{kind: zoneNone}
	case a.grid.Has(id):
		return dropZone{kind: zoneFocus, pane: id}
	case region != RegionPane || y >= a.paneHeight():
		return dropZone{kind: zoneColumn}
	}
	pane, top, height, ok := a.paneAt(col, y)
	if _, bottom := a.layout.SplitRowsIn(col, a.paneHeight()); !ok || bottom == 0 || y < top+height/2 {
		return dropZone{kind: zoneColumn}
	}
	if _, below := a.grid.CanOpenBelow(pane); !below {
		return dropZone{kind: zoneColumn}
	}
	return dropZone{kind: zoneBelow, pane: pane}
}

func abs(n int) int { return max(n, -n) }

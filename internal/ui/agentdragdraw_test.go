package ui

// How a live agent drag is drawn: a tag at the pointer, the zone outlined,
// nothing laid out again, and no cost when nobody is dragging.

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A frame with no live drag is exactly what assembleFrame drew.
func TestAnIdleFrameIsUntouchedByTheDragOverlay(t *testing.T) {
	a := dragFleet(t)
	if a.View() != a.assembleFrame() {
		t.Error("View differs from assembleFrame with nothing held")
	}
	a = grabAgent(t, a, "s3") // armed, not live: a click may still follow
	if a.View() != a.assembleFrame() {
		t.Error("an armed press that never went live drew something")
	}
}

func TestALiveDragDrawsTheNameAtThePointerAndOutlinesTheZone(t *testing.T) {
	a := dragFleet(t)
	r := a.regions()
	x, y := midOf(r, 0), bottomHalf(a)
	a = pullAgent(t, a, "s3", x, y)
	plain := strings.Split(a.assembleFrame(), "\n")
	got := strings.Split(a.View(), "\n")
	if len(got) != len(plain) {
		t.Fatalf("the overlay changed the frame's height: %d rows, was %d", len(got), len(plain))
	}
	for i := range got {
		if ansi.StringWidth(got[i]) != ansi.StringWidth(plain[i]) {
			t.Errorf("row %d is %d cells wide, was %d: a row wider than the window wraps", i, ansi.StringWidth(got[i]), ansi.StringWidth(plain[i]))
		}
	}
	if row := ansi.Strip(got[y]); !strings.Contains(row, "agent03") {
		t.Errorf("the pointer's row %q carries no tag", row)
	}
	// Dropping below the room outlines its lower half: the box's corners are there.
	_, lowerY, w, h, ok := a.zoneRect(dropZone{kind: zoneBelow, pane: ""}, "s3")
	if !ok {
		t.Fatal("no rect for a drop below the room")
	}
	if top := ansi.Strip(got[lowerY]); !strings.Contains(top, "┌"+strings.Repeat("─", w-2)+"┐") {
		t.Errorf("the lower half's top edge %q is not outlined", top)
	}
	if bottom := ansi.Strip(got[lowerY+h-1]); !strings.Contains(bottom, "└") {
		t.Errorf("the lower half's bottom edge %q is not outlined", bottom)
	}
}

// Over the sidebar there is nothing to land on, so nothing is outlined; only the tag follows.
func TestOverTheSidebarOnlyTheTagIsDrawn(t *testing.T) {
	a := dragFleet(t)
	a = pullAgent(t, a, "s3", sidebarX(a), 12)
	if a.dragged.zone.kind != zoneNone || len(a.dragged.outline) != 0 {
		t.Fatalf("zone %+v outline %d rows over the sidebar", a.dragged.zone, len(a.dragged.outline))
	}
	if !strings.Contains(ansi.Strip(a.View()), "agent03") {
		t.Error("the tag is not drawn over the sidebar")
	}
}

// The tag stays inside the frame wherever the pointer is: at the right edge it is
// pulled back, never run past the row, which would wrap it.
func TestTheTagIsKeptInsideTheFrame(t *testing.T) {
	a := dragFleet(t)
	a = pullAgent(t, a, "s3", a.layout.Width-1, 12)
	plain := strings.Split(a.assembleFrame(), "\n")
	for i, row := range strings.Split(a.View(), "\n") {
		if ansi.StringWidth(row) != ansi.StringWidth(plain[i]) {
			t.Fatalf("row %d is %d cells wide with the pointer at the right edge, was %d", i, ansi.StringWidth(row), ansi.StringWidth(plain[i]))
		}
	}
	if !strings.Contains(ansi.Strip(strings.Split(a.View(), "\n")[12]), "agent03") {
		t.Error("the tag at the right edge was cut off the row")
	}
}

// The outline is rebuilt when the zone changes and not per motion.
func TestTheOutlineIsRebuiltOnlyWhenTheZoneChanges(t *testing.T) {
	a := dragFleet(t)
	x := midOf(a.regions(), 0)
	a = pullAgent(t, a, "s3", x, topHalf())
	first := reflect.ValueOf(a.dragged.outline).Pointer()
	for dy := 1; dy < 6; dy++ { // still the room's top half: a new column
		a = motionTo(a, x+dy, topHalf()+dy)
	}
	if got := reflect.ValueOf(a.dragged.outline).Pointer(); got != first {
		t.Error("the outline was rebuilt by motion inside one zone")
	}
	a = motionTo(a, x, bottomHalf(a)) // the room's bottom half: below it
	if got := reflect.ValueOf(a.dragged.outline).Pointer(); got == first {
		t.Error("the outline survived a change of zone")
	}
	if a.dragged.zone != (dropZone{kind: zoneBelow, pane: ""}) {
		t.Errorf("zone %+v, want below the room", a.dragged.zone)
	}
}

// Nothing lays out again until the drop: the grid, the geometry and every pane's
// width are what they were at the press, however far the pointer goes.
func TestNothingReLaysOutWhileDragging(t *testing.T) {
	a := dragFleet(t)
	grid, layout, regions := a.grid, a.layout, a.regions()
	x := midOf(regions, 0)
	a = pullAgent(t, a, "s3", x, topHalf())
	for dx := range 20 {
		a = motionTo(a, x+dx, bottomHalf(a)-dx%3)
	}
	if !reflect.DeepEqual(a.grid, grid) || !reflect.DeepEqual(a.layout, layout) || !reflect.DeepEqual(a.regions(), regions) {
		t.Error("the drag moved the grid or the layout before the drop")
	}
}

// landsAt fails unless id's pane is drawn in the rect (x, y, w, h) in a. It reads
// neither paneRect nor the arithmetic behind it: the size the pane was laid out at
// (resizePanes, from Regions and SplitRowsIn) must be the rect's, and the draw's own
// hit-testing (Layout.Hit, App.paneAt - what a press resolves with) must name id at
// the rect's four corners and not at the cells just outside it.
func landsAt(t testing.TB, label string, a App, id string, x, y, w, h int) {
	t.Helper()
	if d := a.dms[id]; d == nil || d.width != w || d.height != h {
		t.Errorf("%s: the outline is %dx%d, the pane is laid out %v", label, w, h, laidOut(a, id))
	}
	paneAt := func(px, py int) string { // "?" where no pane is
		if py < 0 || py >= a.paneHeight() {
			return "?"
		}
		region, col := a.layout.Hit(a.regions(), px)
		if pane, _, _, ok := a.paneAt(col, py); region == RegionPane && ok {
			return pane
		}
		return "?"
	}
	for _, c := range [][2]int{{x, y}, {x + w - 1, y}, {x, y + h - 1}, {x + w - 1, y + h - 1}} {
		if got := paneAt(c[0], c[1]); got != id {
			t.Errorf("%s: the outline's corner (%d,%d) is %q, want %q", label, c[0], c[1], got, id)
		}
	}
	for _, c := range [][2]int{{x - 1, y}, {x + w, y}, {x, y - 1}, {x, y + h}} {
		if got := paneAt(c[0], c[1]); got == id {
			t.Errorf("%s: (%d,%d), outside the outline (%d,%d %dx%d), is %q's pane too", label, c[0], c[1], x, y, w, h, id)
		}
	}
}

// laidOut is a pane's laid-out size, for a failure message.
func laidOut(a App, id string) string {
	if d := a.dms[id]; d != nil {
		return fmt.Sprintf("%dx%d", d.width, d.height)
	}
	return "absent"
}

// An outline is where the agent is drawn once it is dropped, to the cell: at every
// width from a window too narrow for two columns to a wide one, with up to five
// columns already open (so the window has slid), for each kind of drop. It is
// compared with the real result, not with the formula that computed it.
func TestAnOutlineIsExactlyWhereTheAgentLands(t *testing.T) {
	for _, width := range []int{99, 110, dmTakeoverColumns - 2*SideMargin - 1, dmTakeoverColumns - 2*SideMargin, 125, 150, 200} {
		for open := range 6 {
			base := fleetApp(t, width, 40, 12)
			for i := 1; i <= open; i++ {
				base = clickAgent(t, base, "s"+strconv.Itoa(i))
			}
			// A drop in every drawn pane's bottom half, a new column, and a refocus of each open agent.
			var zones []dropZone
			for _, p := range base.grid.Panes() {
				if _, can := base.grid.CanOpenBelow(p); can && drawnPane(base, p) {
					zones = append(zones, dropZone{kind: zoneBelow, pane: p})
				}
			}
			zones = append(zones, dropZone{kind: zoneColumn})
			for _, p := range base.grid.Panes() {
				if p != "" {
					zones = append(zones, dropZone{kind: zoneFocus, pane: p})
				}
			}
			for _, z := range zones {
				x, y, w, h, ok := base.zoneRect(z, "s12")
				after, id := base, "s12"
				switch z.kind {
				case zoneColumn:
					after = after.openInNewPane(id, "agent12")
				case zoneBelow:
					after = after.withFocus(z.pane).openBelow(id, "agent12")
				case zoneFocus:
					id = z.pane
					after = after.openInNewPane(id, "x")
				}
				label := fmt.Sprintf("width %d, %d open, zone %+v", width, open, z)
				if !ok {
					if slices.Contains(after.drawnIDs(), id) {
						t.Errorf("%s: no outline, and the agent is drawn", label)
					}
					continue
				}
				landsAt(t, label, after, id, x, y, w, h)
			}
		}
	}
}

func drawnPane(a App, id string) bool {
	_, _, _, _, ok := a.paneRect(id)
	return ok
}

func TestTheSidebarHasNoRect(t *testing.T) {
	if _, _, _, _, ok := dragFleet(t).zoneRect(dropZone{kind: zoneNone}, "s3"); ok {
		t.Error("the sidebar has a rect")
	}
}

// A row spliced over wide characters keeps its width and puts the segment on the
// cell it was given, whichever half of a wide rune the cut lands in.
func TestASplicedRowKeepsItsWidthOverWideCharacters(t *testing.T) {
	for _, line := range []string{"ab你好cdefgh", "你好你好你好", "a\x1b[31m你好\x1b[0mbcdef", "😀😀ab😀cd"} {
		w := ansi.StringWidth(line)
		for col := 0; col < w; col++ {
			for _, text := range []string{"|", "||", "┌─┐"} {
				got := spliced(line, []seg{{col, text}})
				if gw := ansi.StringWidth(got); gw != w && col+ansi.StringWidth(text) <= w {
					t.Errorf("%q spliced with %q at %d is %d cells, was %d", line, text, col, gw, w)
				}
				if at := ansi.Strip(ansi.Cut(got, col, col+ansi.StringWidth(text))); at != text && col+ansi.StringWidth(text) <= w {
					t.Errorf("%q spliced with %q at %d has %q on those cells", line, text, col, at)
				}
			}
		}
	}
	// Two segments on one row, each in its own place.
	got := spliced("你好你好你好", []seg{{1, "│"}, {6, "│"}})
	if ansi.StringWidth(got) != 12 || ansi.Strip(ansi.Cut(got, 1, 2)) != "│" || ansi.Strip(ansi.Cut(got, 6, 7)) != "│" {
		t.Errorf("two segments over wide runes: %q", ansi.Strip(got))
	}
}

// The tag is the agent's own hue, so a drag reads as that agent across thirty.
func TestTheTagWearsTheAgentsHue(t *testing.T) {
	trueColor(t)
	a := dragFleet(t)
	tag := func(color string) string {
		b := a
		agent := b.fleet.agents["s3"]
		agent.Color = color
		b.fleet = b.fleet.copy()
		b.fleet.agents["s3"] = agent
		_, s, _ := b.tagAt(agentDrag{id: "s3", name: "agent03", x: 10, y: 4})
		return s.text
	}
	if tag("blue") == tag("green") {
		t.Error("two hues drew the same tag")
	}
	if tag("blue") == tag("") {
		t.Error("a coloured agent drew the default tag")
	}
}

// The overlay's own cost per motion: a splice into the assembled frame, a small
// share of what each motion's View already pays to assemble it.
func BenchmarkLiveDragOverlay(b *testing.B) {
	a := dragFleet(b)
	a = pullAgent(b, a, "s3", midOf(a.regions(), 0), bottomHalf(a))
	frame := a.assembleFrame()
	b.ResetTimer()
	for b.Loop() {
		_ = a.overlayAgentDrag(frame)
	}
}

// In a one-column window a new-column drop replaces the pane that holds the keys,
// or the one beside the room when the room does, so the outline is that pane's own
// rect - half a column when it is half of a split one - to the cell, for each pane
// that can hold the keys, and in a window that shrank with conversations still in
// the grid (a one-column window holding three or more columns).
func TestTheNewColumnOutlineInAOneColumnLayoutIsThePaneItReplaces(t *testing.T) {
	split := func(t *testing.T) App { // the room, then s1 over s2; s2, the lower pane, holds the keys
		return clickAgent(t, fleetApp(t, 100, 40, 6), "s1").openBelow("s2", "agent02")
	}
	shrunk := func(t *testing.T, splitFirst bool) App { // three conversations, then the window narrows
		a := fleetApp(t, 200, 40, 6)
		for _, id := range []string{"s1", "s2", "s3"} {
			a = clickAgent(t, a, id)
		}
		if splitFirst {
			a = a.refocus("s2").openBelow("s4", "agent04")
		}
		return settle(a.withSize(100, 40))
	}
	setups := map[string]func(*testing.T) App{
		"the room alone":                  func(t *testing.T) App { return fleetApp(t, 100, 40, 6) },
		"a whole column holds the keys":   func(t *testing.T) App { return clickAgent(t, fleetApp(t, 100, 40, 6), "s1") },
		"the lower half of a column":      split,
		"the upper half of a column":      func(t *testing.T) App { return split(t).refocus("s1") },
		"the room, beside a split column": func(t *testing.T) App { return split(t).showRoom() },
		"the room, beside a whole column": func(t *testing.T) App { return clickAgent(t, fleetApp(t, 100, 40, 6), "s1").showRoom() },
	}
	for _, splitFirst := range []bool{false, true} {
		for focus := range 5 { // the room, then each pane the shrunk grid holds
			name := fmt.Sprintf("a shrunk window, split %v, focus #%d", splitFirst, focus)
			setups[name] = func(t *testing.T) App {
				a := shrunk(t, splitFirst)
				if panes := append([]string{""}, a.grid.Panes()[1:]...); focus < len(panes) {
					return a.refocus(panes[focus])
				}
				return a
			}
		}
	}
	for name, setup := range setups {
		a := setup(t)
		if a.layout.Capacity() != 1 {
			t.Fatalf("%s: capacity %d, the test proves nothing", name, a.layout.Capacity())
		}
		x, y, w, h, ok := a.zoneRect(dropZone{kind: zoneColumn}, "s5")
		after := a.openInNewPane("s5", "agent05")
		if !ok {
			t.Errorf("%s: no outline for a drop into a drawn window", name)
			continue
		}
		landsAt(t, name, after, "s5", x, y, w, h)
	}
}

// The room is a pane too: whole when its column is alone, the upper half when it is
// split, and a lower pane starts under the rule - none of it by the id test alone,
// since the room's id is "" and an unsplit column's Bottom is "" as well.
func TestPaneRectOfTheRoomAndALowerPane(t *testing.T) {
	a := dragFleet(t)
	r := a.regions()
	if x, y, w, h, ok := a.paneRect(""); !ok || x != 0 || y != 0 || w != r.Cols[0] || h != a.paneHeight() {
		t.Errorf("an unsplit room: (%d,%d %dx%d ok=%v), want the whole column %d x %d", x, y, w, h, ok, r.Cols[0], a.paneHeight())
	}
	a = a.refocus("").openBelow("s3", "agent03") // the room over s3
	upper, lower := a.layout.SplitRowsIn(0, a.paneHeight())
	if _, y, _, h, ok := a.paneRect(""); !ok || y != 0 || h != upper {
		t.Errorf("a split room: y %d h %d ok=%v, want the upper half %d", y, h, ok, upper)
	}
	if _, y, _, h, ok := a.paneRect("s3"); !ok || y != upper+dividerHeight || h != lower {
		t.Errorf("the pane under it: y %d h %d ok=%v, want y %d h %d", y, h, ok, upper+dividerHeight, lower)
	}
	if _, _, _, _, ok := a.paneRect("nobody"); ok {
		t.Error("a conversation that is not in the grid has a rect")
	}
}

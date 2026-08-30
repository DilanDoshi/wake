package ui

// The breathing room around the working/done line and above the composer, so
// the `✻ Chunking…` line is not crammed against the last line of output nor
// against the query box - matching Claude Code, where the spinner sits clear of
// both. The frame-height invariant (the extra rows are budgeted in chrome, so
// the pane never grows past the terminal) is held by the …StaysInBounds… and
// …MeasuresExactly… tests; these pin that the blank rows are actually drawn.

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// beatRowIn is the index of the working or done line, found by a marker only it
// carries, or -1. The DM spinner trails its word with the ellipsis, the done
// line carries the "· done " clause, and the room's minimal line reads
// "<word> for <age>". Safe here because the test content carries none of these.
func beatRowIn(lines []string) int {
	for i, l := range lines {
		if strings.Contains(l, heartbeatEllipsis) || strings.Contains(l, "· done ") || strings.Contains(l, " for ") {
			return i
		}
	}
	return -1
}

// The DM's working line has a blank row above it and a blank row below it: it
// sits clear of the last line of output above and clear of the query bar below.
func TestDMWorkingLineHasBlankRowsAroundIt(t *testing.T) {
	forceTrueColour(t)
	d := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindAssistantText, Text: "some output"})
	d.Agent = Agent{ID: "s1", State: rpc.StateWorking, startedAt: clock()}

	lines := strings.Split(stripANSI(d.View(60, 20)), "\n")
	beat := beatRowIn(lines)
	if beat < 1 {
		t.Fatalf("no working line in the DM:\n%s", strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[beat-1]) != "" {
		t.Errorf("the row above the working line is not blank - it is crammed against the output:\n%s", strings.Join(lines, "\n"))
	}
	if beat+1 >= len(lines) || strings.TrimSpace(lines[beat+1]) != "" {
		t.Errorf("the row below the working line is not blank - it is crammed against the query bar:\n%s", strings.Join(lines, "\n"))
	}
}

// The DM's done line gets the same breathing room as the working line - it is
// the same row, just past-tense.
func TestDMDoneLineHasBlankRowsAroundIt(t *testing.T) {
	forceTrueColour(t)
	start := time.Now()
	d := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindAssistantText, Text: "some output"})
	d.Agent = Agent{ID: "s1", State: rpc.StateIdle, startedAt: start, doneAt: start.Add(time.Minute), turnDur: time.Minute}

	lines := strings.Split(stripANSI(d.View(60, 20)), "\n")
	beat := beatRowIn(lines)
	if beat < 1 {
		t.Fatalf("no done line in the DM:\n%s", strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[beat-1]) != "" {
		t.Errorf("the row above the done line is not blank:\n%s", strings.Join(lines, "\n"))
	}
	if beat+1 >= len(lines) || strings.TrimSpace(lines[beat+1]) != "" {
		t.Errorf("the row below the done line is not blank:\n%s", strings.Join(lines, "\n"))
	}
}

// An idle conversation keeps one blank row above the composer, so the input box
// is not crammed against the last line of output. The transcript is filled so
// the row directly above the box is the inserted gap rather than viewport
// padding - which the content row above it proves.
func TestIdleDMKeepsABlankRowAboveTheComposer(t *testing.T) {
	forceTrueColour(t)
	d := NewDM("s1", "alex").SetSize(50, 12)
	for i := range 12 {
		d = d.Append(core.Event{Kind: core.KindAssistantText, Text: "content " + string(rune('a'+i))})
	}
	d.Agent = Agent{ID: "s1", State: rpc.StateIdle}

	lines := strings.Split(stripANSI(d.View(50, 12)), "\n")
	top := composerTopIn(lines, "@alex")
	if top < 2 {
		t.Fatalf("could not find the composer top:\n%s", strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[top-1]) != "" {
		t.Errorf("no blank row above the idle composer:\n%s", strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[top-2]) == "" {
		t.Errorf("the blank above the composer is viewport padding, not the inserted gap - the transcript should be full:\n%s", strings.Join(lines, "\n"))
	}
}

// composerTopIn is the index of the composer's top border, found by the pane
// name set into its top edge. The last such row, since the box is at the bottom.
func composerTopIn(lines []string, title string) int {
	top := -1
	for i, l := range lines {
		if strings.Contains(l, title) {
			top = i
		}
	}
	return top
}

// The room's working line has the same blank row above and below it.
func TestRoomWorkingLineHasBlankRowsAroundIt(t *testing.T) {
	forceTrueColour(t)
	r := NewRoom().SetSize(80, 20).
		Append(core.Event{Kind: core.KindAssistantText, Text: "some output"}, Agent{ID: "s1", Name: "noah"})
	r = r.WithWorking([]Agent{working("s1", "noah", time.Second, 100)})

	lines := strings.Split(stripANSI(r.View(80, 20)), "\n")
	beat := beatRowIn(lines)
	if beat < 1 {
		t.Fatalf("no working line in the room:\n%s", strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[beat-1]) != "" {
		t.Errorf("the row above the room's working line is not blank:\n%s", strings.Join(lines, "\n"))
	}
	if beat+1 >= len(lines) || strings.TrimSpace(lines[beat+1]) != "" {
		t.Errorf("the row below the room's working line is not blank:\n%s", strings.Join(lines, "\n"))
	}
}

// The gap rows are budgeted in the room's composer bound too, not only its
// transcript sizing: a draft grown near its cap while the working line is up
// must not push the frame past the pane. Without the beat and its gaps in
// composerRowsIn the box outgrows its allowance and the room scrolls the alt
// screen - the DM's own aboveComposerExtra rule, applied to the room.
func TestRoomStaysInBoundsWithAWorkingLineAndAGrownComposer(t *testing.T) {
	forceTrueColour(t)
	const w, h = 60, 10
	r := NewRoom().SetSize(w, h).WithWorking([]Agent{working("s1", "noah", time.Second, 100)})

	// Typed through the real rune path, long enough to wrap past maxComposerRows.
	c := r.Composer()
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("word ", 200))})
	r = r.WithComposer(c)

	if got := lipgloss.Height(r.View(w, h)); got != h {
		t.Fatalf("a working room with a grown draft drew %d rows into a %d-row pane:\n%s",
			got, h, stripANSI(r.View(w, h)))
	}
}

// The idle room keeps a blank row above its composer too.
func TestIdleRoomKeepsABlankRowAboveTheComposer(t *testing.T) {
	forceTrueColour(t)
	r := NewRoom().SetSize(80, 12)
	for i := range 12 {
		r = r.Append(core.Event{Kind: core.KindAssistantText, Text: "content " + string(rune('a'+i))}, Agent{ID: "s1", Name: "noah"})
	}

	lines := strings.Split(stripANSI(r.View(80, 12)), "\n")
	top := composerTopIn(lines, roomTitle)
	if top < 2 {
		t.Fatalf("could not find the room composer top:\n%s", strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[top-1]) != "" {
		t.Errorf("no blank row above the idle room composer:\n%s", strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[top-2]) == "" {
		t.Errorf("the blank above the room composer is viewport padding, not the inserted gap:\n%s", strings.Join(lines, "\n"))
	}
}

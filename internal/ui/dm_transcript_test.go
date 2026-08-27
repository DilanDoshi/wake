package ui

// What the DM's transcript has to guarantee however long the conversation
// gets: one more event costs the same at any length, and the pane still
// measures exactly the box it was handed even when its content cannot be
// wrapped to fit.
//
// Everything here reaches the DM through NewDM/SetSize/Append/View and
// nothing else, deliberately. The cost guards have to keep compiling against
// whatever the transcript is made of, so that reverting an implementation and
// watching them fail stays possible - a test for a complexity fix that cannot
// be run against the version it was written for proves nothing.

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	// scaleShort and scaleLong are the two transcript lengths every cost here
	// is compared at. Roughly an order of magnitude apart: quadratic append
	// costs an order of magnitude more per event at the longer one, linear
	// append costs the same at both.
	scaleShort = 300
	scaleLong  = 3000

	// scaleSamples is how many appends one measurement averages over. It is
	// well above the number of events a transcript batches internally, so the
	// average covers a whole batch and does not depend on where in one a
	// measurement happened to start.
	scaleSamples = 512

	// scaleRounds is how many times a timing measurement is repeated. The
	// fastest round is the reported one: scheduler noise on a loaded machine
	// only ever adds time, so the minimum is the measurement least polluted
	// by whatever else the machine was doing.
	scaleRounds = 5

	// maxCostRatio is how much more one append is allowed to cost on the
	// longer transcript. Quadratic append lands near the ratio of the two
	// lengths themselves; linear append lands near 1. The gap is wide enough
	// that a loaded machine cannot push a linear implementation over.
	maxCostRatio = 3.0

	// maxAllocRatio is the same bound on bytes rather than time, and it is the
	// exact half of this pair: allocation does not vary with machine load. A
	// rebuild of the whole transcript allocates the whole transcript.
	maxAllocRatio = 2.0

	// scaleWidth and scaleHeight are an ordinary pane.
	scaleWidth  = 80
	scaleHeight = 24
)

// sink keeps the compiler from deciding a measured Append had no effect.
var sink DM

// scaleEvent is one short event, deliberately of a kind that is cheap to
// render.
//
// Prose would be the realistic choice and it is the wrong one here: glamour
// spends roughly half a megabyte and 17µs rendering one paragraph, and a fixed
// cost that large is exactly what hides a per-append cost that grows with the
// transcript. Measured against prose, an implementation that copies the whole
// event history on every append comes in at 1.6x rather than the 9x it
// deserves. A block that costs almost nothing to render leaves the transcript
// machinery as the only thing these measurements can be reading.
func scaleEvent(i int) core.Event {
	return core.Event{Kind: core.KindThinking, Text: fmt.Sprintf("weighing approach %d", i)}
}

// transcriptOf returns a DM that has already been fed n events.
func transcriptOf(n int) DM {
	d := NewDM("s1", "alex").SetSize(scaleWidth, scaleHeight)
	for i := range n {
		d = d.Append(scaleEvent(i))
	}
	return d
}

// appendCost is the time one Append costs on top of base, averaged over
// scaleSamples consecutive appends and taken from the fastest of scaleRounds.
func appendCost(base DM) time.Duration {
	best := time.Duration(math.MaxInt64)
	for range scaleRounds {
		d := base
		start := time.Now()
		for i := range scaleSamples {
			d = d.Append(scaleEvent(i))
		}
		elapsed := time.Since(start) / scaleSamples
		sink = d
		if elapsed < best {
			best = elapsed
		}
	}
	return best
}

// bytesPerAppend is the heap one Append allocates on top of base, averaged the
// same way. TotalAlloc is cumulative, so a garbage collection between the two
// readings cannot hide anything.
func bytesPerAppend(base DM) uint64 {
	var before, after runtime.MemStats
	d := base
	runtime.ReadMemStats(&before)
	for i := range scaleSamples {
		d = d.Append(scaleEvent(i))
	}
	runtime.ReadMemStats(&after)
	sink = d
	return (after.TotalAlloc - before.TotalAlloc) / scaleSamples
}

// Every session's events go through Append whether its DM is on screen or
// not, in Bubble Tea's single Update goroutine. If one append costs more on a
// long transcript than on a short one, the whole app gets slower the longer it
// runs - the exact inverse of what §8 of the spec promises the DM, and the
// thing a user notices an hour into the session they care most about.
//
// This is the assertion the fix exists for, so it has to be able to fail:
// against a transcript rebuilt on every event it reports a ratio near the
// ratio of the two lengths, not near 1.
func TestAppendCostsTheSameOnALongTranscriptAsOnAShortOne(t *testing.T) {
	short := appendCost(transcriptOf(scaleShort))
	long := appendCost(transcriptOf(scaleLong))

	t.Logf("one append: %v at %d events, %v at %d (%.2fx)", short, scaleShort, long, scaleLong, float64(long)/float64(short))
	if ratio := float64(long) / float64(short); ratio > maxCostRatio {
		t.Errorf("one append costs %v at %d events and %v at %d - %.1fx for %dx the transcript, want at most %.1fx",
			short, scaleShort, long, scaleLong, ratio, scaleLong/scaleShort, maxCostRatio)
	}
}

// The same bound on allocation, which is the half of this pair that does not
// depend on how busy the machine is. Rebuilding the transcript on every event
// allocates a copy of the whole transcript on every event, and no amount of
// machine noise makes that look flat.
func TestAppendAllocatesTheSameOnALongTranscriptAsOnAShortOne(t *testing.T) {
	short := bytesPerAppend(transcriptOf(scaleShort))
	long := bytesPerAppend(transcriptOf(scaleLong))

	t.Logf("one append: %d bytes at %d events, %d at %d (%.2fx)", short, scaleShort, long, scaleLong, float64(long)/float64(short))
	if ratio := float64(long) / float64(short); ratio > maxAllocRatio {
		t.Errorf("one append allocates %d bytes at %d events and %d at %d - %.1fx for %dx the transcript, want at most %.1fx",
			short, scaleShort, long, scaleLong, ratio, scaleLong/scaleShort, maxAllocRatio)
	}
}

// The grid joins panes on their widest line, so a DM that renders one column
// too wide shoves every neighbour out of place. Content with no break
// opportunity is where that gets decided: the transcript has to bound it
// itself rather than trusting it to wrap.
func TestViewMeasuresExactlyItsWidthForContentThatCannotWrap(t *testing.T) {
	for name, ev := range map[string]core.Event{
		"600 cells of Japanese": {Kind: core.KindAssistantText, Text: strings.Repeat("日本語", 200)},
		"an unbreakable token":  {Kind: core.KindAssistantText, Text: strings.Repeat("x", 400)},
		"a fenced code block": {Kind: core.KindAssistantText,
			Text: "```go\n" + strings.Repeat("\tfmt.Println(\"a line far past any pane's right edge\")\n", 4) + "```"},
		"a long MCP tool name": {Kind: core.KindToolUse, Tool: &core.ToolCall{
			Name:    "mcp__filesystem__read_text_file_with_a_long_name",
			Display: strings.Repeat("deep/", 30) + "file.go",
		}},
		"a wide tool result": {Kind: core.KindToolResult, Text: strings.Repeat("結果", 300)},
	} {
		for _, w := range []int{minComposerWidth, 20, 80} {
			d := NewDM("s1", "alex").SetSize(w, scaleHeight).Append(ev)

			out := d.View(w, scaleHeight)
			if got := lipgloss.Width(out); got != w {
				t.Errorf("%s at width %d measures %d columns:\n%s", name, w, got, out)
			}
			if got := lipgloss.Height(out); got != scaleHeight {
				t.Errorf("%s at width %d measures %d rows, want %d", name, w, got, scaleHeight)
			}
		}
	}
}

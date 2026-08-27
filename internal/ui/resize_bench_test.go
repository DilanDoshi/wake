package ui

// What a width change costs, and what a drag costs, as a function of how much
// conversation is behind it. These are the numbers C1 rests on: the cost of one
// re-wrap is what Update is blocked for, and while Update is blocked the socket
// is not being drained.
//
// Run: go test ./internal/ui -run XXX -bench 'Resize|Drag' -benchtime 1x

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// benchParagraph is one assistant turn: two sentences of prose that wrap. It
// is deliberately the *cheap* shape, because the point is a floor rather than a
// worst case - a turn carrying a code span, a list and a blank line between
// them measures 1,020 ms where this one measures 248 ms at the same 3,000
// events, and the argument does not need the bigger number.
const benchParagraph = "I have read through the handler and the failure is in the retry path. " +
	"The call to applyDefaults runs before the context is checked, so a cancelled " +
	"request still allocates the buffer and then discards it."

// benchTranscript is a DM carrying n assistant turns, laid out at width.
func benchTranscript(n, width, height int) DM {
	d := NewDM("s1", "alex").SetSize(width, height)
	for i := range n {
		d = d.Append(core.Event{
			Kind:      core.KindAssistantText,
			SessionID: "s1",
			Text:      fmt.Sprintf("Turn %d. %s", i, benchParagraph),
		})
	}
	return d
}

// BenchmarkResize is one width change: the unit of work Update performs inside
// a tea.WindowSizeMsg, and the thing a drag does many of.
func BenchmarkResize(b *testing.B) {
	for _, n := range []int{100, 1000, 3000, 5000} {
		b.Run(fmt.Sprintf("events=%d", n), func(b *testing.B) {
			d := benchTranscript(n, 100, 40)
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				d = d.SetSize(100+i%2, 40)
			}
		})
	}
}

// BenchmarkDrag is 80 sequential width changes - a window dragged across 80
// columns, with every intermediate size delivered. It is an upper bound rather
// than a prediction: bubbletea coalesces WindowSizeMsg, so how many arrive
// depends on how fast Update consumes them.
func BenchmarkDrag(b *testing.B) {
	for _, n := range []int{500, 3000} {
		b.Run(fmt.Sprintf("events=%d", n), func(b *testing.B) {
			d := benchTranscript(n, 160, 40)
			b.ResetTimer()
			for b.Loop() {
				for w := 160; w > 80; w-- {
					d = d.SetSize(w, 40)
				}
				d = d.SetSize(160, 40)
			}
		})
	}
}

// BenchmarkDividerDragAcrossFortyColumns is the drag this task exists to make
// affordable, through the real App: a press on the divider, forty motion
// messages as fast as the loop will take them, and the settle that follows.
//
// Measured at 3,000 events: 93ms here, against 4,681ms for the same drag with
// the settle removed - 39 re-wraps rather than one. That is the 4.5x draw-path
// win not being handed back.
//
// The endpoint alternates by a column, for the same reason BenchmarkResize
// alternates its width: a drag that ends where the last one ended settles to a
// split the panes are already wrapped for, SetSize returns without doing
// anything, and every iteration after the first measures the motion messages
// alone. Written the obvious way it reported 5.8ms - one real re-wrap
// amortised over twenty iterations that did none - which is a benchmark
// reporting the absence of the work it exists to price. The count below is what
// stops that coming back.
func BenchmarkDividerDragAcrossFortyColumns(b *testing.B) {
	a := splitApp(b, 200, 40, 3000)
	a = grab(b, a, dividerColumnOf(a))

	renders := renderCounter(b)
	drags := 0
	for i := 0; b.Loop(); i++ {
		for x := 100; x < 140-i%2; x++ {
			a = dragTo(a, x)
		}
		a = settle(a)
		drags++
	}
	if *renders != drags {
		b.Fatalf("%d drags cost %d re-wraps: this prices a drag that re-wraps exactly once, and the fixture stopped producing one", drags, *renders)
	}
}

// BenchmarkRegions is the layout arithmetic itself, which App.paneWidth derives
// once per frame rather than storing a second copy of. The number this reports
// is what "work per frame" means here: set it against BenchmarkView, which is
// the frame it happens inside.
func BenchmarkRegions(b *testing.B) {
	l := Layout{Width: 200, Height: 40, ShowGroups: true, ShowRoster: true}
	for b.Loop() {
		regionSink = l.Regions(1, 0)
	}
}

// regionSink keeps the compiler from eliding the benchmark above.
var regionSink Regions

// stripANSIForBench keeps the linter from deciding benchParagraph is unused if
// a future edit drops one of the benchmarks.
var _ = strings.TrimSpace

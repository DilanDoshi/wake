package ui

// What the TUI costs when nothing is happening, and what one Bubble Tea
// message costs when something is.
//
// Wake sits open all day next to 15-30 agents, so the idle number is a product
// requirement rather than a curiosity. It is measured as process CPU time over
// a fixed wall-clock window with a real tea.Program running - the renderer's
// framerate ticker and the cursor blink are exactly the things being counted,
// and neither of them shows up in a benchmark of View alone.
//
// Run: go test ./internal/ui -run XXX -bench 'Idle|View' -benchtime 1x

import (
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// idleWindow is how long an idle measurement watches. Long enough that a 60Hz
// ticker is hundreds of wakeups and the startup cost amortises away.
const idleWindow = 10 * time.Second

// blockingReader never produces a byte and never ends, which is what an
// attached terminal with nobody typing looks like.
type blockingReader struct{ done chan struct{} }

func (r blockingReader) Read([]byte) (int, error) {
	<-r.done
	return 0, io.EOF
}

// cpuSeconds is this process's user+system CPU time.
func cpuSeconds(t testing.TB) float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatalf("getrusage: %v", err)
	}
	return time.Duration(ru.Utime.Nano()).Seconds() + time.Duration(ru.Stime.Nano()).Seconds()
}

// BenchmarkIdleCPU runs the real program against a transcript of n events with
// no input at all, and reports the share of one core it spends doing nothing.
func BenchmarkIdleCPU(b *testing.B) {
	for _, n := range []int{0, 3000} {
		b.Run(name(n), func(b *testing.B) {
			for b.Loop() {
				b.ReportMetric(idleCPUPercent(b, n), "%cpu")
			}
		})
	}
}

func name(n int) string {
	if n == 0 {
		return "empty"
	}
	return "events=3000"
}

func idleCPUPercent(b *testing.B, events int) float64 {
	app := dmApp(nil, Stream{}, "s1", "alex")
	m, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	app = m.(App)
	for i := range events {
		mm, _ := app.Update(eventMsg{Event: core.Event{
			Kind: core.KindAssistantText, SessionID: "s1", Text: benchParagraph,
		}})
		app = mm.(App)
		_ = i
	}
	return idleCPUOf(b, app)
}

// idleCPUOf runs one already-built model under a real tea.Program with nothing
// arriving, and reports the share of one core it spends doing nothing.
//
// One implementation, two callers: the shape being measured is the argument,
// so a second copy of this would be a second way for the window, the settle
// and the accounting to drift.
func idleCPUOf(b *testing.B, app App) float64 {
	done := make(chan struct{})
	p := tea.NewProgram(app, tea.WithInput(blockingReader{done}), tea.WithOutput(io.Discard))
	ran := make(chan struct{})
	go func() { _, _ = p.Run(); close(ran) }()

	time.Sleep(500 * time.Millisecond) // let the first render and the blink settle
	before := cpuSeconds(b)
	start := time.Now()
	time.Sleep(idleWindow)
	spent := cpuSeconds(b) - before
	elapsed := time.Since(start).Seconds()

	p.Quit()
	close(done)
	<-ran
	return 100 * spent / elapsed
}

// What the room costs to leave open at the fleet size Wake is for, which is
// the one measurement this project keeps asking for and had never taken.
//
// BenchmarkIdleCPU above is a **120-column takeover**: at width 100 the DM
// takes the whole pane (layout.dmTakeoverColumns), so what it measures is one
// pane with one agent in it - close to the shape the recorded Phase 1 baseline
// of 0.452-0.499% was taken on, and no longer the shape somebody leaves open.
// This is: a wide terminal, two panes, both sidebars, and a roster with the
// whole fleet on it.
//
// # What the pairing isolates
//
// Terminal size, transcript length, pane count and sidebar state are identical
// across all three arms. The **only** thing that varies is how many agents are
// in the fleet report, so the difference between agents=1 and agents=30 is the
// per-agent cost of an idle frame and nothing else - the roster's rows, Rank's
// sort, and the fleet map's size.
//
// agents=30+manager is the same question asked about the one session Phase 3
// added: the manager is a 31st row in every report, and it is on this roster
// like any other session. It says nothing about the manager's own `claude`
// process, which is not in this address space, and nothing about roll_up -
// that is internal/mcp's BenchmarkRollUp.
//
// # What it does not isolate
//
// Not the room against the DM. Every arm draws both panes, because that is
// what a 200-column terminal with a conversation open actually draws. The
// one-pane comparison is BenchmarkViewSecondPane, which holds pane width fixed
// instead.
//
// # Why the roster is set open
//
// A fleet nothing draws is a fleet size that costs nothing by construction -
// the benchmark would then be measuring its own fixture. Opening a conversation
// leaves the sidebar alone, so this only states the default rather than undoing
// anything, and the count below is what stops the arms silently collapsing into
// each other again.
//
// Run: go test ./internal/ui -run XXX -bench BenchmarkIdleCPUAtFleetScale -benchtime 1x
func BenchmarkIdleCPUAtFleetScale(b *testing.B) {
	for _, c := range []struct {
		name    string
		agents  int
		manager bool
	}{
		{"agents=1", 1, false},
		{"agents=30", benchFleetSize, false},
		{"agents=30+manager", benchFleetSize, true},
	} {
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				b.ReportMetric(idleCPUOf(b, benchFleetApp(b, c.agents, c.manager)), "%cpu")
			}
		})
	}
}

// idleTerminal is the terminal these are measured on: wide enough for two
// panes and both sidebars, which is the arrangement §8 describes and the one
// the takeover width is defined against.
const idleTerminalWidth, idleTerminalHeight = 200, 40

// benchFleetApp is the room as somebody actually leaves it open: a wide
// terminal, both sidebars, a conversation open beside the room, and n agents
// that have all been talking into it.
//
// It fails rather than returns if the fleet it built is not on screen. A
// roster the layout closed, a height too short for the rows, or a name the
// sidebar truncated would each make every arm draw the same frame - and three
// identical arms reporting three identical numbers is exactly what "the room
// costs nothing at 30 agents" would look like if it were false.
func benchFleetApp(b *testing.B, agents int, manager bool) App {
	b.Helper()
	fresh(b)

	seed := &rpc.Status{Running: true}
	for i := range agents {
		seed.Sessions = append(seed.Sessions, rpc.SessionStatus{
			ID: benchAgentID(i), Name: benchAgentName(i), Label: "main",
			Dir: "/Users/someone/code/api-v2", State: rpc.StateIdle,
		})
	}
	if manager {
		seed.Sessions = append(seed.Sessions, rpc.SessionStatus{
			ID: "mgr", Name: core.ManagerName, State: rpc.StateIdle,
		})
	}

	a := NewRoomApp(nil, Stream{}, seed).WithOpenDM(benchAgentID(0), benchAgentName(0))
	a.layout.ShowGroups, a.layout.ShowRoster = true, true
	a = a.withSize(idleTerminalWidth, idleTerminalHeight)

	// The conversation, spread across the fleet, so the room's transcript is
	// the same length in every arm and the attribution column is doing work.
	//
	// Through applyFrame - a real rpc.FrameEvent - and **not** through eventMsg.
	// eventMsg is App.appendEvent, which puts an event straight into whichever
	// conversation is open and never touches Fleet.Observe; with a DM open,
	// every one of these would land in the DM and leave the room empty. The
	// first draft of this did exactly that, and the mutation that should have
	// exploded the idle number - re-wrapping the room on every frame - moved it
	// by 0.03 points, because there was nothing in the room to re-wrap.
	for i := range benchEvents {
		id := benchAgentID(i % agents)
		a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: id, Event: &core.Event{
			Kind: core.KindAssistantText, SessionID: id, Text: benchParagraph,
		}})
	}

	frame := a.View()
	drawn := 0
	for i := range agents {
		if strings.Contains(frame, benchAgentName(i)) {
			drawn++
		}
	}
	switch {
	case drawn != agents:
		b.Fatalf("%d of %d agents are on screen: the arms are not distinguishable, so this measures the fixture rather than the fleet", drawn, agents)
	case a.room.said.len() == 0:
		b.Fatal("the room is empty: an idle frame over an empty transcript is not the frame anybody leaves open, " +
			"and a re-wrap made unconditional on purpose would move nothing")
	case a.dms[benchAgentID(0)].events.len() == 0:
		b.Fatal("the open conversation is empty, so only one of the two panes has anything in it")
	}
	return a
}

// benchAgentID and benchAgentName are fixed width, so no name is a prefix of
// another - a Contains count over a frame would otherwise find `a1` inside
// `a10` and report a fleet that is not drawn as drawn.
func benchAgentID(i int) string   { return fmt.Sprintf("s%02d", i) }
func benchAgentName(i int) string { return fmt.Sprintf("a%02d", i) }

// BenchmarkView is one steady-state frame: what every Bubble Tea message pays
// after the size is already applied.
func BenchmarkView(b *testing.B) {
	app := dmApp(nil, Stream{}, "s1", "alex")
	m, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	app = m.(App)
	for range 3000 {
		mm, _ := app.Update(eventMsg{Event: core.Event{
			Kind: core.KindAssistantText, SessionID: "s1", Text: benchParagraph,
		}})
		app = mm.(App)
	}
	b.ResetTimer()
	for b.Loop() {
		_ = app.View()
	}
}

// BenchmarkViewWithPlanCard is BenchmarkView with a plan card pinned in the
// focused pane. A plan is markdown through glamour, behind internal/render's one
// process-global mutex, so without the memo it re-renders on every steady frame -
// every blink, every mouse-motion during a drag - which is the per-frame cost
// this pairs against BenchmarkView to show (a recorded plan is milliseconds a
// frame, dwarfing the ~150µs of a card-free frame). With the memo the fatal-check
// frame below renders it once and b.Loop measures cache hits, so the two converge
// on the card's framing cost alone.
func BenchmarkViewWithPlanCard(b *testing.B) {
	app := dmApp(nil, Stream{}, "s1", "alex")
	m, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	app = m.(App)
	for range 3000 {
		mm, _ := app.Update(eventMsg{Event: core.Event{
			Kind: core.KindAssistantText, SessionID: "s1", Text: benchParagraph,
		}})
		app = mm.(App)
	}
	ev := planAsk(b)
	app = app.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev})
	if !strings.Contains(app.View(), cardHasPlan) {
		b.Fatal("the plan card is not on screen: this would measure a frame without the card it names")
	}
	b.ResetTimer()
	for b.Loop() {
		_ = app.View()
	}
}

// What the second pane costs.
//
// The deliverable of the split is the measurement as much as the split: the
// room and the DM are two widths behind internal/render's one process-global
// mutex where stacking them would have been one, and that trade was made
// deliberately (see layout.go) on the argument that vertical space is the
// scarce resource. This is where the bill is read out.
//
// The number to beat is App.View's recorded 248-268µs at any transcript length.
// textarea.Blink re-renders forever while nothing happens and View is
// unmemoized with a value receiver, so a blink redraws whatever is on screen -
// doubled, once there are two panes.
//
// # Two pairings, because one of them cannot answer the question
//
// The brief's stop condition is "two panes exceeding roughly twice one pane".
// Read against a **fixed terminal** that condition is close to unfalsifiable:
// one 200-column pane and a 99+100 split both draw 200x39, so the pairing varies
// pane count and pane width together and holds drawn area constant. It is still
// worth measuring - it is the product question, *does opening a conversation
// beside the room cost more per frame* - but it is not a measurement of pane
// count, and the first version of this block drew two conclusions from it that
// it could not support.
//
// So there are two, and each says only what its own construction allows:
//
//	fixed terminal, 200x40           what opening a DM costs in the window you have
//	  BenchmarkViewOnePane   379-404µs   one 200-column room pane
//	  BenchmarkViewTwoPanes  299-317µs   a 99-column room and a 100-column DM
//
//	fixed pane width, 100 columns    what a second pane costs, isolated
//	  BenchmarkViewSecondPane/panes=1      122-123µs   one 100-column pane, whole frame
//	  BenchmarkViewSecondPane/panes=2      302-307µs   two 100-column panes, whole frame, terminal 201
//	  BenchmarkViewSecondPane/pane-only    119-123µs   the same one pane, frame assembly excluded
//	  BenchmarkViewSecondPane/panes-only   230-233µs   the same two panes, frame assembly excluded
//
// The second is the one the stop condition belongs to, and read on the whole
// frame it trips it: 2.5x, not "roughly twice". So it was chased with the seams
// the brief names rather than explained away.
//
//   - **Nothing re-wraps.** renderRoom and renderTranscript count 0 over the
//     frames TestASteadyFrameReWrapsNeitherPane draws, at both *two-pane*
//     widths - the table quotes three, and that test structurally cannot run at
//     100, where there is only one pane to re-wrap. A frame allocates ~1,200
//     times; a frame that re-wraps allocates
//     2.1 million (measured, by making resizePanes a no-op). Superlinearity from
//     a per-frame re-wrap is ruled out by counting, which is the only thing that
//     can rule it out.
//   - **The panes themselves are 1.9x**, which is under the bound: 231µs against
//     121µs, both arms bare. Comparing two bare panes against one *framed* pane
//     would assume one-column assembly is free in order to prove it, which is
//     this file's own confound one level down - so pane-only exists and measures
//     it instead: ~1µs, which is what lipgloss.JoinHorizontal returning its only
//     argument costs.
//   - **The remaining ~73µs is two-column frame assembly**, which a one-pane
//     frame does not pay at all: JoinHorizontal does a line-wise join over three
//     blocks when there are two panes and a divider, and the divider column is 39
//     rows built per draw. It is the fixed cost of there being more than one
//     column, not a per-pane cost that keeps multiplying.
//
// The first pairing then says the thing the product actually asks: opening a
// conversation in the terminal you already have does not cost more per frame. It
// costs less, because the room is the more expensive of the two views and it
// gives up half its columns to do it.
//
// **The stop condition stands for whoever measures this next**, not just for the
// run above: if two panes ever exceed roughly twice one pane *of the same width*,
// something is re-wrapping per frame, and the counter seams renderRoom and
// renderTranscript are the first thing to reach for - before timing anything.
// Compare bare against bare, or the frame assembly will answer for the panes.
//
// Run: go test ./internal/ui -run XXX -bench 'BenchmarkView' -benchtime 200x -count 5
func BenchmarkViewOnePane(b *testing.B)  { benchView(b, 200, false) }
func BenchmarkViewTwoPanes(b *testing.B) { benchView(b, 200, true) }

// BenchmarkViewSecondPane holds the pane width fixed at 100 columns and varies
// only the number of panes, which is what the brief's "twice one pane" needs in
// order to be a statement about anything.
//
// panes-only draws the same two panes without the join, the divider column, the
// notice row or the mid-drag clip. It is what separates "a second pane costs
// more than a pane" from "a frame with two columns in it costs more than a frame
// with one", which are different claims and only the first is superlinear.
func BenchmarkViewSecondPane(b *testing.B) {
	b.Run("panes=1", func(b *testing.B) { benchView(b, 100, false) })
	// 201 rather than 200 so each pane is exactly 100 once the divider takes its
	// column - the same width the single pane above is measured at.
	b.Run("panes=2", func(b *testing.B) { benchView(b, 201, true) })
	// Both arms without the frame, because comparing two bare panes against one
	// *framed* pane silently assumes one-column assembly is free - which is the
	// proposition the decomposition exists to establish, assumed in order to
	// establish it. That is this file's own I3 one level down.
	b.Run("pane-only", func(b *testing.B) {
		a := wrapped(b, 100, 40, benchEvents).(App).applyGeometry()
		benchPanes(b, a)
	})
	b.Run("panes-only", func(b *testing.B) { benchPanes(b, benchSplit(b, 201)) })
}

// benchPanes draws whatever panes the layout is showing, with no join, no
// divider column, no notice row and no mid-drag clip.
func benchPanes(b *testing.B, a App) {
	r, h := a.regions(), a.paneHeight()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for i, w := range r.Cols {
			if w > 0 {
				_ = a.column(i, w, h)
			}
		}
	}
}

// benchEvents is how much conversation each pane carries. Three thousand is the
// figure every other cost in this package is quoted at.
const benchEvents = 3000

func benchView(b *testing.B, width int, split bool) {
	a := wrapped(b, width, 40, benchEvents).(App)
	if split {
		a = benchSplit(b, width)
	} else {
		// Sized before the loop, so what is measured is a *steady-state* frame -
		// the one a cursor blink draws - and not a re-wrap. A benchmark that
		// re-wraps every iteration measures glamour, which is already measured
		// in resize_bench_test.go, and would hide the thing this exists to catch.
		a = a.applyGeometry()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = a.View()
	}
}

// benchSplit is a settled two-pane App with a full transcript in each.
func benchSplit(b *testing.B, width int) App {
	a := wrapped(b, width, 40, benchEvents).(App)
	a = a.openDMWith("s1", "sydney")
	a = withDMEvents(a, "s1", benchEvents)
	return a.applyGeometry()
}

// withDMEvents fills one conversation directly rather than through the daemon's
// frames, so the room does not also draw every line - the two panes are being
// priced separately and each has to carry the same amount. Same sentence as
// wrapped uses, for the same reason.
func withDMEvents(a App, sessionID string, n int) App {
	d := *a.dms[sessionID]
	for i := range n {
		d = d.Append(core.Event{
			Kind: core.KindAssistantText, SessionID: sessionID,
			Text: fmt.Sprintf("turn %d. %s", i, wrappingSentence),
		})
	}
	return a.withDM(sessionID, d)
}

// BenchmarkFold is the cost of taking 1,024 frames off the daemon and drawing
// the result, at two batch sizes.
//
// Bubble Tea calls View once per message, so the batch size is exactly the
// number of whole frames drawn for a given amount of agent output. batch=1 is
// what the model did before - one message per frame off the socket - and
// batch=256 is what it does now. Both are folded through the real Update and
// followed by the real View, which is what the event loop does.
func BenchmarkFold(b *testing.B) {
	const frames = 1024
	for _, size := range []int{1, takeLimit} {
		b.Run(fmt.Sprintf("batch=%d", size), func(b *testing.B) {
			fs := make([]rpc.Frame, frames)
			for i := range fs {
				fs[i] = rpc.Frame{
					Kind: rpc.FrameEvent, SessionID: "s1",
					Event: &core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: benchParagraph},
				}
			}
			for b.Loop() {
				b.StopTimer()
				app := dmApp(nil, Stream{}, "s1", "alex")
				m, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
				b.StartTimer()

				for i := 0; i < frames; i += size {
					m, _ = m.Update(streamMsg{batch: batch{frames: fs[i:min(i+size, frames)]}})
					_ = m.View()
				}
			}
		})
	}
}

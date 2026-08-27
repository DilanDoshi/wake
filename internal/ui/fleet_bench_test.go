package ui

// What one event off the daemon costs the client-side fold, at the fleet size
// Wake is for.
//
// This is a per-event path, so the number that matters is per event and not per
// frame: the daemon fans every session's events out to every client, so at
// 15-30 agents this runs on everything the whole fleet produces. The shapes
// below are split by what they cost rather than by what they mean - chatter
// changes nothing and must not pay for a copy, a tool call moves the sidebar's
// row, and prose moves the row *and* returns a line to the room.
//
// Run: go test ./internal/ui -run XXX -bench 'Observe|Agents'

import (
	"fmt"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// benchFleetSize is the top of the range in CLAUDE.md's opening line.
const benchFleetSize = 30

// benchSession is an agent in the middle of that fleet, so the map lookup is
// not measuring a one-entry map.
const benchSession = "s15"

func benchFleet() Fleet {
	sessions := make([]rpc.SessionStatus, 0, benchFleetSize)
	for i := range benchFleetSize {
		sessions = append(sessions, rpc.SessionStatus{
			ID:      fmt.Sprintf("s%d", i),
			Name:    fmt.Sprintf("agent-%d", i),
			Label:   "main",
			Dir:     "/Users/someone/code/api-v2",
			State:   rpc.StateWorking,
			QuietMS: int64(i) * 1_000,
		})
	}
	return NewFleet().WithStatus(&rpc.Status{Sessions: sessions})
}

func BenchmarkObserve(b *testing.B) {
	f := benchFleet()
	cases := []struct {
		name string
		ev   core.Event
	}{
		{"chatter", core.Event{Kind: core.KindSystem, Text: "lifecycle"}},
		{"tool_call", core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{Name: "Edit", Display: "auth/token.go"}}},
		{"prose", core.Event{Kind: core.KindAssistantText, Text: "Fixed the retry header, tests pass"}},
		{"turn_end", core.Event{Kind: core.KindTurnEnd}},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				_, _ = f.Observe(c.ev, benchSession)
			}
		})
	}
}

// BenchmarkAgents is the roster a view asks for. It ranks on every call - see
// Fleet.Agents for why it does not cache - so this is what a caller drawing it
// per frame adds to a View that costs ~250µs.
func BenchmarkAgents(b *testing.B) {
	f := benchFleet()
	for b.Loop() {
		_ = f.Agents()
	}
}

// BenchmarkRoomFold is the whole client-side per-event path at fleet size:
// Fleet.Observe, and then Room.Append for whatever it hands back.
//
// # What this pairing isolates
//
// The three arms differ in what the fold does with the event, and nothing else:
// all of them walk the same 30-agent map, compare the same Agent struct and run
// at the same room width. `chatter` changes no record and returns nothing;
// `tool_call` moves the sidebar's row, so it pays the copy-on-write and returns
// nothing; `prose` pays the copy *and* is drawn. So chatter-to-tool_call is the
// price of the immutability and tool_call-to-prose is the price of the room.
//
// BenchmarkObserve alone is the first two thirds of that and quoting it as
// "what an event costs the room" is the mistake this exists to stop: Observe
// returns the event, and the room then renders it. A fold that got 30x cheaper
// while Append got 30x more expensive would not move that number at all.
//
// **Every event this drives is different from the one before it**, which is not
// a detail. Observe returns the receiver untouched when an agent's record did
// not move, so an arm repeating one identical tool call measures a map lookup
// and a struct compare - about 40ns - and reports the copy-on-write as free. A
// real fleet emits a different tool call every time. The counter below cannot
// see that, because a no-op fold draws exactly as many lines as a real one.
//
// The events are built **before** the timer starts, cycling through a ring, for
// the same reason: a Sprintf inside the loop is 60ns of fixture on top of a
// chatter arm that is barely 60ns of subject, which is half the number the
// "chatter must not pay for a copy" argument rests on.
//
// # What it does not isolate
//
// Not the frame. Bubble Tea calls View once per message and inbox.go folds up
// to takeLimit frames into one message, so this is paid per *event* while View
// is paid per *batch* - which is the whole reason the batch exists. Set it
// beside BenchmarkView; do not add the two.
//
// Not the daemon's side either: the events here are already decoded and
// already off the socket.
//
// Run: go test ./internal/ui -run XXX -bench BenchmarkRoomFold -benchtime 20000x -count 5
func BenchmarkRoomFold(b *testing.B) {
	cases := []struct {
		name string
		// nth is the i-th event of this shape, and it differs from the one
		// before it - see the header for why an identical one measures a fold
		// that returned early.
		nth func(i int) core.Event
		// draws is what fold is expected to hand the room for this shape. It
		// is asserted rather than described: an arm that stopped drawing
		// would get faster and read as an improvement, and an arm that
		// started drawing would get slower with nothing saying why.
		draws int
	}{
		{"chatter", func(i int) core.Event {
			return core.Event{Kind: core.KindSystem, Text: fmt.Sprintf("lifecycle %d", i)}
		}, 0},
		{"tool_call", func(i int) core.Event {
			return core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{Name: "Edit", Display: fmt.Sprintf("auth/token%d.go", i)}}
		}, 0},
		{"prose", func(i int) core.Event {
			return core.Event{Kind: core.KindAssistantText, Text: fmt.Sprintf("Fixed the retry header on attempt %d, tests pass", i)}
		}, 1},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			f := benchFleet()
			r := NewRoom().SetSize(benchRoomWidth, benchRoomHeight)
			who, _ := f.Agent(benchSession)

			ring := make([]core.Event, benchEventRing)
			for i := range ring {
				ring[i] = c.nth(i)
			}

			drawn, i := 0, 0
			b.ReportAllocs()
			for b.Loop() {
				var out []core.Event
				f, out = f.Observe(ring[i%benchEventRing], benchSession)
				i++
				drawn += len(out)
				for _, ev := range out {
					r = r.Append(ev, who)
				}
			}
			sinkRoom = r

			if want := c.draws * b.N; drawn != want {
				b.Fatalf("%d room lines from %d %s events, want %d: this arm is not measuring the path it names", drawn, b.N, c.name, want)
			}
		})
	}
}

// benchEventRing is how many distinct events each arm cycles through. Any
// number above one gives every iteration an event unlike the one before it,
// which is the property that matters; 256 is takeLimit, so the ring is also one
// batch of the size inbox.go actually folds.
const benchEventRing = takeLimit

// benchRoomWidth and benchRoomHeight are the room pane on a 200-column
// terminal with both sidebars open: 200 - 16 groups - 20 roster - 1 divider,
// halved. Derived here rather than picked, so a breakpoint change moves it.
var benchRoomWidth, benchRoomHeight = func() (int, int) {
	l := Layout{Width: 200, Height: 40, ShowGroups: true, ShowRoster: true}
	return l.Regions(1, 0).Room(), 40
}()

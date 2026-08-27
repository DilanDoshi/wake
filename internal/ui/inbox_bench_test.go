package ui

// What the drain costs per frame, and what the fold changed about it.
//
// add runs on the pump goroutine, under the one lock between the socket and the
// draw loop - so anything it costs is paid once per frame from every session,
// and since --include-partial-messages that is once per output token. The fold
// spends a map lookup and a string append there to buy the ring slot back, and
// this is the price of that trade rather than an assertion about it.
//
// The arms are the two things that arrive: a second of a working fleet's
// tokens, and a second of its record. Both are measured with nothing taking
// frames out, which is the stall the ring exists for and the only state where
// the fold decides anything.
//
// Measured on an M5 Max over 1,290 frames - thirty agents at the corpus median
// - so read these per fleet-second: **161-192µs for the tokens and 54-62µs for
// the record**, 125-149ns against 42-48ns a frame. The fold's price is that
// ~100ns and ~350 bytes a token, two allocations of the three the append and
// the new Event cost between them; what it buys is 1,290 ring slots a second
// that used to be spent on previews. Against the 7.4-8.3ms one fleet-second
// costs through the real Update and View (BenchmarkStreamingFleetSecond), the
// whole of this is ~2% of it, and 447KB/s against that path's 19MB/s.
//
// Run: go test ./internal/ui -run XXX -bench InboxStall -count 5

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func BenchmarkInboxStall(b *testing.B) {
	for _, c := range []struct {
		name   string
		frames []rpc.Frame
	}{
		{"tokens", fleetSecondOfTokens()},
		{"record", fleetSecondOfRecord()},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				in := newInbox()
				b.StartTimer()
				for _, f := range c.frames {
					in.add(f)
				}
				b.StopTimer()
				sinkHeld, _ = in.held()
				b.StartTimer()
			}
		})
	}
}

// fleetSecondOfRecord is the same fleet-second at the same frame count, as
// completed blocks rather than tokens - so the two arms differ in what the
// frames are and in nothing else.
func fleetSecondOfRecord() []rpc.Frame {
	tokens := fleetSecondOfTokens()
	frames := make([]rpc.Frame, len(tokens))
	for i, f := range tokens {
		frames[i] = rpc.Frame{
			Kind: rpc.FrameEvent, SessionID: f.SessionID,
			Event: &core.Event{Kind: core.KindAssistantText, SessionID: f.SessionID, Text: f.Event.Text},
		}
	}
	return frames
}

// sinkHeld keeps the compiler from deciding the frames were never buffered.
var sinkHeld int

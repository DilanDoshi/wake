package ui

// What token-level streaming costs, and what it would have cost at the
// granularity that was rejected.
//
// This is the measurement partial.go's ruling rests on, and it is written as a
// *pairing* rather than as one number: "the preview costs 4µs" says nothing on
// its own, because the question was never whether streaming is expensive in the
// abstract. It is whether the obvious implementation - re-render the block that
// is growing - is affordable at the fleet size Wake is for. Both arms do the
// same work to the same block at the same pane width; the only thing that
// varies is what is done per token.
//
// # Why total CPU is the right measure and not per-op latency
//
// internal/render renders behind one process-global mutex shared by every
// session in the process. So glamour time is not parallelisable across agents:
// thirty agents' renders serialize, and the sum is what any one pane waits
// behind. Single-goroutine total time is therefore the honest figure, and a
// parallel benchmark would flatter the rejected arm by hiding exactly the
// property that kills it.
//
// Run: go test ./internal/ui -run XXX -bench 'OneBlockStreamed|StreamingFleetSecond' -benchtime 10x -count 5

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The two figures the fleet-scale arm is built from, both measured over
// testdata/stream by scripts/measure-stream-rate.py rather than assumed.
// TestTheStreamingConstantsStillDescribeTheCorpus holds them to that script's
// fixture, so a re-recorded corpus reddens these rather than leaving the
// benchmark quoting a rate that was true once.
const (
	// medianTokensPerSecond is the median output rate over the 47 recorded
	// turns of 50 tokens or more: 43.5/s, with a p90 of 76 and a maximum of
	// 93.9. One content_block_delta carries one token, so it is also the
	// partial-frame rate of one working agent. Truncated, because a frame rate
	// is a whole number of frames.
	medianTokensPerSecond = 43

	// medianBlockChars is the mean recorded assistant text block, 252
	// characters over 131 blocks. The distribution is what makes the pairing
	// below use several sizes: the median block is 48 characters and the
	// longest 13,499, and the rejected granularity is superlinear in exactly
	// that dimension - so an arm priced at one block length says little about
	// the others.
	medianBlockChars = 252
)

// streamedBlock is one assistant block arriving token by token, as the tokens
// themselves. Four characters a token is the ratio the corpus's own
// output_tokens and text lengths agree on.
func streamedBlock(tokens int) []string {
	out := make([]string, tokens)
	src := strings.Fields(strings.Repeat(benchParagraph+" ", 1+tokens/8))
	for i := range out {
		out[i] = src[i%len(src)] + " "
	}
	return out
}

// BenchmarkOneBlockStreamed is one whole assistant block, from its first token
// to its last, under each granularity.
//
// Per *block* rather than per token, deliberately: the rejected arm's cost
// grows with the block, so a per-token figure averages away the thing that
// decides this. What is being compared is the total work one answer costs.
//
// The preview arm is the shipped path - DM.Append on a KindPartialText, which
// bounds the tail and wraps it. The glamour arm is the granularity partial.go
// rejects: render the block so far, through the renderer the transcript uses,
// at the same width.
func BenchmarkOneBlockStreamed(b *testing.B) {
	// 64 tokens is about the recorded mean block, 256 is a long paragraph, and
	// 1024 is a third of the corpus's longest turn.
	for _, tokens := range []int{64, 256, 1024} {
		toks := streamedBlock(tokens)

		b.Run(fmt.Sprintf("preview/tokens=%d", tokens), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				d := NewDM("s1", "alex").SetSize(benchPaneWidth, 40)
				for _, tok := range toks {
					d = d.Append(core.Event{Kind: core.KindPartialText, SessionID: "s1", Text: tok})
				}
				sinkPreview = d.partial.view
			}
		})

		b.Run(fmt.Sprintf("glamour-per-token/tokens=%d", tokens), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var block strings.Builder
				for _, tok := range toks {
					block.WriteString(tok)
					sinkPreview = render.Markdown(block.String(), benchPaneWidth)
				}
			}
		})
	}
}

// BenchmarkStreamingFleetSecond is one second of a working fleet: thirty agents
// each emitting the corpus's median token rate, folded through the real Update
// and drawn by the real View, in the batches inbox.go actually hands over.
//
// The number to read is ns/op against one second. 10,000,000 ns/op is 1% of one
// core; 1,000,000,000 is a core saturated, which for the glamour arm means a
// core saturated *inside the lock every other pane's draw needs*.
//
// A second rather than one event, because a per-event figure cannot be
// multiplied back up: the rejected granularity's cost depends on how far into a
// block each token is, so its "average event" is a number about no event.
//
// Run: go test ./internal/ui -run XXX -bench StreamingFleetSecond -benchtime 10x
func BenchmarkStreamingFleetSecond(b *testing.B) {
	// open=1 is the ordinary shape - one conversation open beside the room.
	// open=30 is the case App.wants exists for: App.dms holds **every
	// conversation ever opened**, not the ones on screen, and withDM copied the
	// whole map of DM values per write - so before the gate an operator who had
	// looked at all thirty agents paid thirty large struct copies per token,
	// measured at 106-123ms against 12.7-12.9ms here.
	//
	// **The gate stops the 28 undrawn conversations being *written*; what is left
	// is the map copy itself.** That residual - withDM copying the whole map on
	// every write, proportional to how many conversations have ever been opened
	// and paid by every event kind - is now a copy of *pointers* rather than of
	// DM values: keying App.dms on *DM makes a write clone N 8-byte pointers, not
	// N DM headers, which TestWithDMWriteClonesPointersNotWholeDMs pins. What this
	// pairing is still for is the order of magnitude between 106ms and 10ms; a gap
	// that returns toward the former is the gate having stopped working.
	for _, open := range []int{1, benchFleetSize} {
		b.Run(fmt.Sprintf("preview/open=%d", open), func(b *testing.B) {
			frames := fleetSecondOfTokens()
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				a := streamingApp(b, open)
				b.StartTimer()
				for i := 0; i < len(frames); i += takeLimit {
					m, _ := a.Update(streamMsg{gen: a.gen, batch: batch{frames: frames[i:min(i+takeLimit, len(frames))]}})
					a = m.(App)
					sinkPreview = a.View()
				}
				// The arm has to reach the thing it prices, and "reach it" is
				// exactly what App.wants narrows: every drawn conversation holds
				// a preview and no undrawn one does. A fold that discarded them
				// all is the fastest possible result and reads as the strongest
				// possible pass; a gate that let them all through is the cost
				// this pairing exists to show being paid.
				b.StopTimer()
				drawn, previews := a.drawnConversations(), 0
				for i := range open {
					held := a.dms[benchAgentID(i)].partial.view != ""
					if held != drawn(benchAgentID(i)) {
						b.Fatalf("conversation %d: preview held = %v, on screen = %v - App.wants is not gating what it claims to", i, held, drawn(benchAgentID(i)))
					}
					if held {
						previews++
					}
				}
				if previews == 0 {
					b.Fatal("no conversation on screen holds a preview after a second of tokens: this arm is measuring a fold that discarded them")
				}
				b.StartTimer()
			}
		})
	}

	// The same second of tokens, priced at the rejected granularity: every
	// token re-renders the block it is part of. Nothing is folded through the
	// App - this is the glamour time alone, which is the floor of what that
	// design costs and already decides it.
	//
	// Each block starts at the recorded mean length and is reset every
	// iteration, so the figure is a second of streaming *in the middle of an
	// average answer* and does not depend on -benchtime. That matters because
	// this arm is superlinear: run against blocks that accumulate across
	// iterations it reports whatever duration it was given.
	b.Run("glamour-per-token", func(b *testing.B) {
		toks := streamedBlock(medianTokensPerSecond)
		b.ReportAllocs()
		for b.Loop() {
			for range benchFleetSize {
				block := strings.Builder{}
				block.WriteString(blockPrefix(medianBlockChars))
				for _, tok := range toks {
					block.WriteString(tok)
					sinkPreview = render.Markdown(block.String(), benchPaneWidth)
				}
			}
		}
	})
}

// blockPrefix is n characters of real assistant prose, for a block that is
// already part-written when a measurement starts.
func blockPrefix(n int) string {
	var b strings.Builder
	for b.Len() < n {
		b.WriteString(benchParagraph)
		b.WriteString(" ")
	}
	return b.String()[:n]
}

// fleetSecondOfTokens is one second of partial frames from a whole fleet,
// interleaved across agents the way the daemon's fan-out delivers them.
func fleetSecondOfTokens() []rpc.Frame {
	toks := streamedBlock(medianTokensPerSecond)
	frames := make([]rpc.Frame, 0, benchFleetSize*len(toks))
	for _, tok := range toks {
		for i := range benchFleetSize {
			id := benchAgentID(i)
			frames = append(frames, rpc.Frame{
				Kind: rpc.FrameEvent, SessionID: id,
				Event: &core.Event{Kind: core.KindPartialText, SessionID: id, Text: tok},
			})
		}
	}
	return frames
}

// streamingApp is the whole fleet working, with `open` of its conversations
// held in App.dms. The grid draws at most two of them whatever open is, which
// is the point of the pairing above.
func streamingApp(tb testing.TB, open int) App {
	tb.Helper()
	fresh(tb)
	seed := &rpc.Status{Running: true}
	for i := range benchFleetSize {
		seed.Sessions = append(seed.Sessions, rpc.SessionStatus{
			ID: benchAgentID(i), Name: benchAgentName(i), Dir: "/Users/someone/code/api-v2",
			State: rpc.StateWorking,
		})
	}
	a := NewRoomApp(nil, Stream{}, seed)
	for i := range open {
		a = a.WithOpenDM(benchAgentID(i), benchAgentName(i))
	}
	a.layout.ShowGroups, a.layout.ShowRoster = true, true
	return a.withSize(idleTerminalWidth, idleTerminalHeight)
}

// benchPaneWidth is the conversation pane on the terminal the idle numbers are
// taken on, so every figure in this package is quoted at one width.
var benchPaneWidth = func() int {
	l := Layout{Width: idleTerminalWidth, Height: idleTerminalHeight, ShowGroups: true, ShowRoster: true}
	return l.Regions(2, 1).Cols[1]
}()

// sinkPreview keeps the compiler from deciding a measured render had no effect.
var sinkPreview string

// sinkApp is sinkPreview for a measured withDM write.
var sinkApp App

// TestWithDMWriteClonesPointersNotWholeDMs is BenchmarkStreamingFleetSecond's
// open= arms turned into a pin: keying dms on *DM makes a withDM write clone N
// pointers rather than N DM values, so its per-write allocation stops scaling
// with sizeof(DM). Measured as the byte delta between a 30-entry map and a
// 2-entry one, which cancels the constant every write pays - the escaped dm, the
// hmap header - and isolates the map backing, the one term that told a value map
// from a pointer map. Before the fix the delta was ~28*sizeof(DM); after it is a
// machine word an entry. Delta-based, so it survives sizeof(DM) drift.
func TestWithDMWriteClonesPointersNotWholeDMs(t *testing.T) {
	const extra = benchFleetSize - 2
	dm := NewDM("probe", "probe")

	perWrite := func(open int) int64 {
		a := streamingApp(t, open)
		return testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sinkApp = a.withDM("probe", dm)
			}
		}).AllocedBytesPerOp()
	}

	delta := perWrite(benchFleetSize) - perWrite(2)
	if delta < 0 {
		delta = 0
	}
	if perEntry := delta / int64(extra); perEntry >= int64(unsafe.Sizeof(DM{})) {
		t.Errorf("a withDM write allocates ~%d B per extra conversation (delta %d B over %d entries, %d open vs 2); sizeof(DM)=%d. The dms map is being copied as DM values, not *DM pointers.",
			perEntry, delta, extra, benchFleetSize, unsafe.Sizeof(DM{}))
	}
}

package ui

// The preview: the block an agent is writing, shown while it is being written.
//
// # Why this is plain text, and why that is the whole feature
//
// Claude Code renders assistant prose as it is generated. Wake renders whole
// blocks, so before this the heartbeat stood in for progress instead of
// accompanying it. What --include-partial-messages adds is a token a frame -
// the recorded corpus's median assistant turn runs at 43.5 output tokens a
// second, and its fastest at 93.9 - so the *only* question this file answers is
// what may be done per token at thirty of those at once.
//
// The obvious answer is to re-render the block that is growing, and it is a
// non-starter. internal/render renders behind ONE process-global mutex shared
// by every session in the process, so glamour time does not parallelise across
// agents - it serializes, and the sum is what every other pane's draw waits
// behind. Streaming a block through it costs the integral rather than one
// render, which BenchmarkOneBlockStreamed prices against this file at three
// block sizes: 7.3x at 64 tokens, 19x at 256, and 65x at 1,024 - 4.6ms here
// against 303ms there. Read the shape rather than the ratio: four times the
// tokens costs this design 4.2x and then 4.2x, and costs that one 11x and then
// 14x.
//
// The four candidates and what the numbers do to them:
//
//   - re-render per token: the table above, and it keeps getting worse with the
//     answer. Dead.
//   - re-render only the last block: the last block IS the one growing, so this
//     is the same measurement with the word "only" in front of it. Dead.
//   - coalesce on a tick: beat.go's shared ticker is the precedent and it is
//     the wrong one - the heartbeat's per-tick work is *constant* and this one's
//     grows with the answer, so a tick lowers the rate and not the growth. It is
//     also a poll where a wait will do, which is the first non-negotiable, and
//     the deltas themselves are the wait. Dead.
//   - **plain text, never glamour**: what shipped. One second of thirty
//     streaming agents costs 7.4-8.3ms - under 1% of one core - through the
//     real Update and the real View (BenchmarkStreamingFleetSecond).
//
// It works because the preview is not the record. The same words arrive a
// moment later as a complete assistant frame and go through glamour exactly
// once, as they always did - so the transcript is byte-identical to what this
// build drew before, and the preview costs a wrap of at most maxPreviewRows
// rows. Nothing about the conversation's length enters that.
//
// # The four properties, each with a test named for it
//
// A preview never enters the transcript, so it cannot make Append superlinear
// and a resize has nothing extra to re-wrap. It is **bounded to what a pane can
// draw**, so the work per token is flat rather than growing with the answer -
// the retained text is the tail, because the newest tokens are the ones being
// read. It is **cleared by the block that supersedes it**, or by the turn
// ending, which is the interrupted case where no block ever arrives. And it is
// **only accumulated for a pane on screen** - see wants, which is the one of the
// four that is App's rather than this type's.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// wants reports whether an event is worth a write into the conversation it
// belongs to. Everything is, except a token for a pane that is not on screen.
//
// The DM is unfiltered and this is not a filter. A partial is a *preview*: it is
// replaced by the completed block, and the block - and the clear it performs -
// arrive unconditionally, so a conversation coming back on screen has lost
// nothing that outlives the turn. That asymmetry is load-bearing and
// TestClosingAPaneMidBlockLeavesNothingToComeBackTo is what holds it; gating the
// clear as well would leave a half-sentence from a finished block waiting behind
// every conversation somebody closed mid-turn.
//
// It is a decision for App rather than for DM because the cost is App's:
// App.dms holds every conversation ever *opened* - hideDM keeps the transcript
// so that closing a pane is reversible - and withDM copied the whole map of DM
// values on every write. So an operator who had looked at all thirty agents paid
// thirty large struct copies per token, which is a per-agent cost multiplied by
// thirty and exactly what the first non-negotiable is about. Measured in one
// run before this gate existed: 106-123ms per fleet-second with thirty
// conversations open against 12.7-12.9ms with one, and 530MB/s of allocation
// against 2MB/s. With it, thirty costs 10.3-10.6ms and 19MB/s.
//
// It does not close the gap to zero and is not meant to: withDM still copies a
// thirty-entry map for each write to the panes that *are* drawn. That map is now
// keyed on *DM, so the copy is of pointers rather than of DM values - the
// residual streaming made visible, pinned by
// TestWithDMWriteClonesPointersNotWholeDMs. BenchmarkStreamingFleetSecond's
// open= arms are the pairing.
// Accumulation stops here; the tail itself is dropped by DM.Leave, which
// runs on every path a pane stops being drawn on. Freezing without dropping
// let a reopened pane splice new tokens onto old ones.
func (a App) wants(sessionID string, ev core.Event) bool {
	return ev.Kind != core.KindPartialText || a.drawnConversations()(sessionID)
}

const (
	// maxPreviewRows is how much of the pane a preview may take.
	//
	// It is a preview of the sentence being written rather than of the message,
	// which arrives whole a moment later and is rendered properly. Three rows
	// read a sentence at any pane width, and in one of four grid panes - about
	// eighteen rows of transcript - spending three is affordable where spending
	// ten would push the conversation off screen to show something temporary.
	maxPreviewRows = 3

	// previewSlack is how many rows of text are kept beyond the drawn ones. The
	// tail is cut by byte to bound the work, so the slack is what absorbs the
	// cut: a word split at the front, or a multi-byte rune, is dropped by the
	// wrap rather than drawn as a fragment on the top row.
	previewSlack = 2
)

// previewChars is how many characters of the block are retained at width w.
// Everything before that is dropped as it arrives: it can never be drawn, and
// keeping it would make the wrap below cost the length of the answer.
func previewChars(w int) int { return max(w, minBlockWidth) * (maxPreviewRows + previewSlack) }

// partial is the tail of the block being written, and the rows it draws.
//
// view is rendered when the text or the width changes and never per frame, for
// the reason DM.bar is cached: this sits under a working agent, which is
// exactly when something is redrawing.
//
// Its methods take value receivers and return a new partial, like everything
// else a DM holds.
type partial struct {
	text  string
	view  string
	width int
	// rowCap caps how many rows the tail retains; 0 means the default
	// maxPreviewRows. The tiled board sets it (maxTileTailRows) so a big cell
	// fills with output; the DM preview and the inbox fold leave it zero.
	rowCap int
}

// cap is how many rows this tail retains: rowCap when set, else the default.
func (p partial) cap() int {
	if p.rowCap > 0 {
		return p.rowCap
	}
	return maxPreviewRows
}

// chars is how many characters of the block are retained at this tail's width
// and row cap - previewChars, aware of a raised cap.
func (p partial) chars() int { return max(p.width, minBlockWidth) * (p.cap() + previewSlack) }

// add appends the tokens that just arrived, keeping only what can be drawn.
func (p partial) add(s string) partial {
	p.text += s
	if len(p.text) > p.chars() {
		// Bytes rather than runes: this is a bound on work, and a multi-byte
		// rune cut in half at the front is dropped by the wrap below rather
		// than drawn - which is what the slack is for.
		p.text = p.text[len(p.text)-p.chars():]
	}
	return p.wrapped()
}

// cleared is the preview after the block it was previewing has landed.
func (p partial) cleared() partial {
	p.text, p.view = "", ""
	return p
}

// sized re-wraps for a new pane width, and returns the receiver untouched when
// the width has not moved - a height change does not re-wrap here for the same
// reason it does not re-wrap the transcript.
func (p partial) sized(w int) partial {
	if w == p.width {
		return p
	}
	p.width = w
	return p.wrapped()
}

// wrapped lays the tail out for the pane and keeps the last rows of it.
//
// The floor is the same one previewChars applies, so the width the tail is cut
// to and the width it is laid out at cannot drift - and a pane that has not been
// sized yet wraps at the floor rather than at zero, which is what render.Markdown
// does one package over and for the same reason.
func (p partial) wrapped() partial {
	if p.text == "" {
		p.view = ""
		return p
	}
	// ToValidUTF8 drops the rune the byte-wise cut in add may have halved.
	lines := strings.Split(ansi.Wrap(strings.ToValidUTF8(p.text, ""), max(p.width, minBlockWidth), ""), "\n")
	if len(lines) > p.cap() {
		lines = lines[len(lines)-p.cap():]
	}
	p.view = strings.Join(lines, "\n")
	return p
}

// rows is how many rows the preview draws, and 0 for one with nothing in it.
// Read by chromeHeight, which is why it is counted rather than measured off a
// render.
func (p partial) rows() int {
	if p.view == "" {
		return 0
	}
	return strings.Count(p.view, "\n") + 1
}

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
// build drew before, and the preview costs a wrap of at most the rows the pane
// can spare (DM.previewCap) - the floor over a full transcript, more over one it
// does not fill. Nothing about the conversation's length enters that.
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

	"github.com/charmbracelet/lipgloss"
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
	// minPreviewRows is the floor the preview always gets, and its cap when the
	// transcript already fills the pane.
	//
	// It is a preview of the sentence being written rather than of the message,
	// which arrives whole a moment later and is rendered properly. Three rows
	// read a sentence at any pane width; over a full transcript spending more
	// would push read conversation off screen to show something temporary, so
	// three is the floor. Over an empty or short one there is nothing to push
	// off, so DM.previewCap grows the preview into the unused rows instead - the
	// same relaxation boardtile.go took for a tile's live tail.
	minPreviewRows = 3

	// previewSlack is how many rows of text are kept beyond the drawn ones. The
	// tail is cut by byte to bound the work, so the slack is what absorbs the
	// cut: a word split at the front, or a multi-byte rune, is dropped by the
	// wrap rather than drawn as a fragment on the top row.
	previewSlack = 2
)

// previewChars is how many characters of the block are retained at width w for a
// preview that may draw rows rows. Everything before that is dropped as it
// arrives: it can never be drawn, and keeping it would make the wrap below cost
// the length of the answer. rows below the floor is treated as the floor, so an
// unset cap (a partial the DM never sized) still retains the three-row minimum.
func previewChars(w, rows int) int {
	return max(w, minBlockWidth) * (max(rows, minPreviewRows) + previewSlack)
}

// partial is the tail of the block being written, and the rows it draws.
//
// view is rendered when the text or the width changes and never per frame, for
// the reason DM.bar is cached: this sits under a working agent, which is
// exactly when something is redrawing.
//
// cap is how many rows the preview may draw, set by DM.previewCap: the floor
// over a full pane, more over one the transcript does not fill. It is held on
// the partial so a token can wrap against it without recomputing the pane, and
// is refreshed by SetSize (which the growing preview retriggers through View).
//
// Its methods take value receivers and return a new partial, like everything
// else a DM holds.
type partial struct {
	text  string
	view  string
	width int
	cap   int
}

// add appends the tokens that just arrived, keeping only what can be drawn.
func (p partial) add(s string) partial {
	p.text += s
	if keep := previewChars(p.width, p.cap); len(p.text) > keep {
		// Bytes rather than runes: this is a bound on work, and a multi-byte
		// rune cut in half at the front is dropped by the wrap below rather
		// than drawn - which is what the slack is for.
		p.text = p.text[len(p.text)-keep:]
	}
	return p.wrapped()
}

// capped sets how many rows the preview may draw and re-wraps to it. A no-op
// when the cap has not moved, so streaming a token past a settled cap costs
// nothing here.
func (p partial) capped(n int) partial {
	if n == p.cap {
		return p
	}
	p.cap = n
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
	// Honour the cap exactly - previewCap keeps it no larger than the pane can
	// spare over the transcript's floor, so respecting it (including a cap of
	// zero, a pane too tight for any preview) is what keeps the pane in bounds. A
	// partial the DM never sized has cap zero and is never drawn (View sizes the
	// pane before rendering), so it wraps to nothing here, which is harmless.
	if keep := max(p.cap, 0); len(lines) > keep {
		lines = lines[len(lines)-keep:]
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

// previewCap is how many rows the preview may draw in this pane: the floor over
// a full transcript, and the rows the transcript is not using over a short one -
// so a long answer streaming into an empty pane fills it rather than scrolling
// inside a three-row box, while one over a full pane still yields to the floor
// and pushes nothing read off screen.
//
// It is derived from the pool the transcript and preview share (the pane less
// the preview-free chrome) minus the rows the transcript's own content wants,
// and is capped a floor short of the pool so the transcript keeps at least
// minTranscriptHeight - which is what keeps DM.View exactly its height, the
// alt-screen invariant the fixed cap held. A menu present takes the floor: its
// own allowance already leaves the transcript that floor, so the pool accounting
// this walks would double-count it.
func (d DM) previewCap() int {
	if d.height <= 0 || d.menu != "" {
		return minPreviewRows
	}
	pool := d.height - d.chromeSansPreview()
	room := pool - minTranscriptHeight // leave the transcript its own floor
	blank := pool - d.tr.lines.count() // rows the transcript is not using
	// Floor the target at minPreviewRows, then cap it at room. room is the ceiling
	// and can be zero or negative in a pane too tight to hold the transcript's
	// floor and a preview both; there the ceiling wins and the preview yields to
	// zero rows rather than drawing one that does not fit and overflowing the pane.
	return max(0, min(max(blank, minPreviewRows), room))
}

// chromeSansPreview is chromeHeight without the preview's own rows: the pool the
// preview competes with the transcript for is the pane less this. Summed rather
// than taken as chromeHeight()-partial.rows(), because menuRows is itself a
// function of the preview - and previewCap is only ever reached with no menu up
// (its floor branch handles the rest), so menuRows is zero here by construction.
func (d DM) chromeSansPreview() int {
	composer := lipgloss.Height(d.composer.View(max(d.width, minComposerWidth)))
	return composer + d.beatBarRows() + d.checklistRows() + d.queuedRows()
}

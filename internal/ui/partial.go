package ui

// The preview: the block an agent is writing, shown while it is being written.
//
// # Why the open block is plain text, and why a finished one is not
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
// block sizes: 3.9x at 64 tokens, 9.7x at 256, and 17x at 1,024 - 22ms here
// against 363ms there (2026-10-07, a 40-row pane over an empty transcript).
// Read the shape rather than the ratio: this design's per-token cost stops
// growing once the retained tail has filled the pane - 20, 24 and 25 microseconds
// a token at 1,024, 4,096 and 16,384 - and that one's keeps growing with the
// answer.
//
// The five candidates and what the numbers do to them:
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
//   - **plain text, never glamour**: what shipped first, and still draws the
//     block being written. One second of thirty streaming agents costs 23ms -
//     about 2% of one core, against 74ms for glamour (BenchmarkStreamingFleetSecond;
//     the ruling in decisions.md has the table). Its price was a long answer
//     streaming as raw markdown and snapping to formatted when it landed.
//   - **render each finished block once** (2026-10-07, the owner's ruling): the
//     integral above is the price of re-rendering a block that keeps growing, and
//     a finished one does not. render.Splitter says where one ends and each is
//     rendered once as it completes (partialchunks.go); only the open block stays
//     plain text. 1,024 tokens cost 6.4ms against 21ms plain and 363ms per token,
//     1ms of it glamour (BenchmarkOneBlockStreamed's formatted/ arm).
//
// It works because the preview is not the record. The same words arrive a
// moment later as a complete assistant frame and go through glamour exactly
// once, as they always did - so the transcript is byte-identical to what this
// build drew before, and the preview costs a wrap of at most the rows the pane
// can spare (DM.previewCap): all of them for a reader following the newest line,
// each streamed row pushing the transcript up one, as Claude Code's does, and a
// floor for one scrolled back over a full transcript. Nothing about the
// conversation's length enters that: finished blocks are kept only while their rows
// can be drawn, and the open one whole only up to render.MaxChunk. The reader pays
// a beat of lag, and the open block as plain text at the formatted rows' left edge
// and wrap (render.Prose), so the block that finishes does not jump.
//
// # The five properties, each with a test named for it
//
// A preview never enters the transcript, so it cannot make Append superlinear
// and a resize has nothing extra to re-wrap. It is **bounded to what a pane can
// draw**, so the work per token is flat rather than growing with the answer -
// the retained text is the tail, because the newest tokens are the ones being
// read. It is **cleared by the block that supersedes it**, or by the turn
// ending, which is the interrupted case where no block ever arrives. And it is
// **only accumulated for a pane on screen** - see wants, which is the one of the
// four that is App's rather than this type's. The fifth: a pane **reads finished
// blocks only if it has heard every token of the one it is in** (synced; see
// partialchunks.go), or it would cut inside a fence it never saw open.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
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
// let a reopened pane splice new tokens onto old ones. And a token refused here
// is a token the pane lost, so observe unsyncs it: whichever way it went off
// screen, it will not read the rest of that block (partialchunks.go).
func (a App) wants(sessionID string, ev core.Event) bool {
	return ev.Kind != core.KindPartialText || a.drawnConversations()(sessionID)
}

const (
	// minPreviewRows is the cap of a reader who has scrolled back over a full
	// transcript, and of a preview the pane never sized (a board tile's).
	//
	// It is a preview of the sentence being written rather than of the message,
	// which arrives whole a moment later and is rendered properly. Three rows
	// read a sentence at any pane width, and for a reader who is reading back
	// nothing may move: more would push what they read off screen to show
	// something temporary. A reader who is following gets the pane's room instead
	// (DM.previewCap), as does a scrolled-back one over a transcript that does not
	// fill the pane - there is nothing to push off.
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

// partial is the block being written, and the rows it draws.
//
// view is rendered when the text or the width changes and never per frame, for
// the reason DM.bar is cached: this sits under a working agent, which is
// exactly when something is redrawing.
//
// text is the open block, raw, since the last cut; fin the blocks finished above it
// (partialchunks.go), nil for a pane that reads nothing. cap is how many rows the
// preview may draw, set by DM.previewCap and refreshed by SetSize, a landing block,
// ScrollUp and DM.followed. synced is whether this pane has heard every token of
// the block since its message began; only then are finished blocks read out of it.
// raw is a pane that never formats: a board tile's.
//
// Its methods take value receivers and return a new partial, like everything else a
// DM holds; fin is shared by every copy and never written.
type partial struct {
	text  string
	view  string
	width int
	cap   int

	fin    *blocks
	scan   render.Splitter
	synced bool
	raw    bool
}

// add appends the tokens that just arrived and reads any block they finished,
// keeping only what can be drawn.
func (p partial) add(s string) partial {
	p.text += s
	if p.formats() {
		p = p.cut()
	}
	if !p.formats() {
		// Bytes rather than runes: this is a bound on work, and a multi-byte
		// rune cut in half at the front is dropped by the wrap below rather
		// than drawn - which is what the slack is for.
		p.text = tail(p.text, previewChars(p.width, p.cap))
	}
	return p.wrapped()
}

// tail is the last n bytes of s.
func tail(s string, n int) string { return s[max(len(s)-n, 0):] }

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
	p.text, p.view, p.fin, p.scan = "", "", nil, render.Splitter{}
	return p
}

// sized re-wraps for a new pane width, and returns the receiver untouched when
// the width has not moved - a height change does not re-wrap here for the same
// reason it does not re-wrap the transcript. A finished block is rendered again
// at the new width, as the transcript's are.
func (p partial) sized(w int) partial {
	if w == p.width {
		return p
	}
	p.width = w
	if done := p.fin.chunks(); len(done) > 0 {
		redone := make([]chunk, 0, len(done))
		for _, c := range done {
			if rows := p.draw(c.src); len(rows) > 0 {
				redone = append(redone, chunk{src: c.src, rows: rows})
			}
		}
		p.fin = finishedBlocks(redone, p.wanted())
	}
	return p.wrapped()
}

// wrapped lays the preview out for the pane and keeps the last rows of it.
//
// The floor is the same one previewChars applies, so the width the tail is cut
// to and the width it is laid out at cannot drift - and a pane that has not been
// sized yet wraps at the floor rather than at zero, which is what render.Markdown
// does one package over and for the same reason.
func (p partial) wrapped() partial {
	if p.prose() {
		return p.proseView()
	}
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

// previewCap is how many rows the preview may draw: the pane's room for a reader
// who follows the newest line, the floor over a full transcript for one scrolled
// back, and the floor under a menu or a subagent's view (the parent's words are
// not what they opened). The pool is the pane less the chrome the preview does not
// own, the composer as drawn, so the draft wins. following is the caller's, never
// tr.atBottom(), which is stale after a width re-wrap.
func (d DM) previewCap(following bool) int {
	if d.height <= 0 {
		return minPreviewRows // a pane never sized: a board tile's
	}
	pool := d.height - d.chromeSansPreview()
	room := max(pool-minTranscriptHeight, 0) // the transcript keeps its own floor
	switch {
	case d.menu != "" || d.viewing != "":
		return min(minPreviewRows, room)
	case following:
		return room
	}
	blank := pool - d.tr.lines.count() // rows the transcript is not using
	return min(max(blank, minPreviewRows), room)
}

// chromeSansPreview is chromeHeight without the preview's rows or the menu's: the
// pool the preview, the menu and the transcript share. Summed rather than taken
// as chromeHeight()-partial.rows(), because menuRows is itself a function of the
// preview.
func (d DM) chromeSansPreview() int {
	composer := lipgloss.Height(d.composer.View(max(d.width, minComposerWidth)))
	return composer + d.beatBarRows() + d.checklistRows() + d.queuedRows()
}

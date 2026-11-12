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
// Read the shape rather than the ratio: this design's per-token cost does not grow
// with the answer - 10.6, 8.0 and 7.3 microseconds a token at 1,024, 4,096 and
// 16,384 tokens of a formatted one (2026-11-08) - and that one's does.
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
// build drew before. The preview's rows are drawn under the transcript's last
// line, in the same window (transcript.tail): the answer being written is the
// newest part of the conversation, so a wheel notch moves it and everything above
// it by the notch, a follower watches each row push the conversation up, and a
// reader scrolled back reads it standing still (the owner's 2026-11-08 ruling,
// decisions.md). It keeps the whole message, the most the landed block holds a
// moment later, and the work per token stays flat: a finished block is rendered
// once, a finished line wrapped once, and only the line or open block a token
// lands on is laid out again, at most render.MaxChunk of it. The reader pays a beat
// of lag, and the open block as plain text at the formatted rows' left edge and
// wrap (render.Prose), so the block that finishes does not jump.
//
// # The five properties, each with a test named for it
//
// A preview never enters the record, so it cannot make Append superlinear.
// Its **rows above the open block never change** (previewRows), so the work per
// token is flat rather than growing with the answer and nothing a reader scrolled
// back over them is reading moves; the open block's own rows are laid out once
// more as it finishes, plain to formatted, as they always were. It is **cleared by the block that
// supersedes it**, or by the turn ending, which is the interrupted case where no
// block ever arrives. And it is **only accumulated for a pane on screen** - see
// wants, which is the one of the four that is App's rather than this type's. The
// fifth: a pane **reads finished blocks only if it has heard every token of the
// one it is in** (synced; see partialchunks.go), or it would cut inside a fence it
// never saw open.

import (
	"slices"
	"strings"

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
// tokensUnseen widens this to everything a token can reach: what apply and the
// inbox let a token skip.
func (a App) wants(sessionID string, ev core.Event) bool {
	return ev.Kind != core.KindPartialText || a.drawnConversations()(sessionID)
}

// tokensUnseen is every session whose next token changes nothing on screen: one
// the fleet holds (a token from one it does not adds its row), whose conversation
// is not drawn (wants), with no tile on a tiled board (foldBoard) and no standing
// retry, which a token clears (requestOver). Such a token only unsyncs a pane
// nobody sees, so applied does that alone and the inbox lets it wait for the next
// frame that wakes the loop rather than drawing a frame for it (BUG-67). Every
// other token rings. Sorted, and a new slice each call: the inbox holds it.
func (a App) tokensUnseen() []string {
	unseen := a.unseenTokens()
	var ids []string
	for _, id := range a.fleet.order {
		if unseen(id) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// unseenTokens answers tokensUnseen for one session, with the layout resolved once.
func (a App) unseenTokens() func(string) bool {
	drawn := a.drawnConversations()
	return func(id string) bool {
		_, known := a.fleet.agents[id]
		_, tile := a.boardDMs[id]
		_, retry := a.retrying[id]
		tiled := tile && a.board.Up && a.board.Tiled
		return known && !drawn(id) && !tiled && !retry
	}
}

// unseenTold tells the inbox what tokensUnseen is now. Update's seam, so every
// message that moves a pane, the board, a retry or the fleet is followed by it.
func (a App) unseenTold() App {
	if a.in != nil {
		a.in.setUnseen(a.tokensUnseen())
	}
	return a
}

const (
	// minPreviewRows is how many rows a board tile shows of an answer being
	// written: a preview of the sentence rather than of the message, which a tile
	// has no room for.
	minPreviewRows = 3

	// previewSlack is how many rows of text a tile keeps beyond the drawn ones.
	// The tail is cut by byte to bound the work, so the slack is what absorbs the
	// cut: a word split at the front, or a multi-byte rune, is dropped by the
	// wrap rather than drawn as a fragment on the top row.
	previewSlack = 2
)

// previewChars is how many characters of a board tile's preview are retained at
// width w: its rows and the slack.
func previewChars(w int) int {
	return max(w, minBlockWidth) * (minPreviewRows + previewSlack)
}

// partial is the block being written, and the rows it draws.
//
// text is the open text: since the last finished block of a pane that reads them,
// since the last finished line of one that does not. fin is the blocks finished
// above it (partialchunks.go), nil for a pane that reads nothing; lines and srcs
// are the finished lines' rows and sources, wrapped once (settle). rows is what a
// pane draws, laid out when the text or the width changes and never per frame.
// synced is whether this pane has heard every token of the block since its message
// began; only then are finished blocks read out of it. raw is a board tile's: the
// newest minPreviewRows rows of plain text, in view.
//
// Its methods take value receivers and return a new partial, like everything else a
// DM holds; fin, lines and srcs are shared by every copy and never written.
type partial struct {
	text  string
	view  string
	width int
	col   int // the cell column text's last line has reached, for its next tab

	fin    *blocks
	lines  chunked[string]
	srcs   chunked[string]
	rows   previewRows
	prosed bool // laid out as prose since the message began
	scan   render.Splitter
	synced bool
	raw    bool
}

// add appends the tokens that just arrived, reads any block or line they
// finished, and lays out what changed.
func (p partial) add(s string) partial {
	// Expanded from the column the line had reached, kept here because a cut can
	// take the line's start away (bytes an inbox fold drops are not counted).
	s, p.col = render.ExpandTabsAt(p.col, s)
	p.text += s
	if p.raw {
		// Bytes rather than runes: this is a bound on work, and a multi-byte rune
		// cut in half at the front is dropped by the wrap - what the slack is for.
		p.text = tail(p.text, previewChars(p.width))
		return p.wrapped()
	}
	if p.formats() {
		p = p.cut()
	}
	if !p.formats() {
		p = p.settle()
	}
	return p.laidOut()
}

// tail is the last n bytes of s.
func tail(s string, n int) string { return s[max(len(s)-n, 0):] }

// cleared is the preview after the block it was previewing has landed.
func (p partial) cleared() partial {
	p.text, p.view, p.col, p.fin, p.scan = "", "", 0, nil, render.Splitter{}
	p.lines, p.srcs, p.rows, p.prosed = chunked[string]{}, chunked[string]{}, previewRows{}, false
	return p
}

// sized lays the preview out again for a new pane width, and returns the receiver
// untouched when the width has not moved. A finished block is rendered again at the
// new width, and a finished line wrapped again, as the transcript's are.
func (p partial) sized(w int) partial {
	if w == p.width {
		return p
	}
	p.width = w
	if p.raw {
		return p.wrapped()
	}
	if done := p.fin.chunks(); len(done) > 0 {
		redone := make([]chunk, 0, len(done))
		for _, c := range done {
			if rows := p.draw(c.src); len(rows) > 0 {
				redone = append(redone, chunk{src: c.src, rows: rows})
			}
		}
		p.fin = finishedBlocks(redone)
	}
	if p.formats() {
		// The open block is still being read for blocks, from where the splitter is.
		return p.laidOut()
	}
	srcs, text := p.srcs, p.text
	p.lines, p.srcs = chunked[string]{}, chunked[string]{}
	p.text = strings.Join(srcs.slice(0, srcs.len()), "") + text
	return p.settle().laidOut()
}

// wrapped lays a board tile's preview out and keeps its newest rows.
//
// The floor is the same one previewChars applies, so the width the tail is cut
// to and the width it is laid out at cannot drift - and a tile that has not been
// sized yet wraps at the floor rather than at zero, which is what render.Markdown
// does one package over and for the same reason.
func (p partial) wrapped() partial {
	if p.text == "" {
		p.view = ""
		return p
	}
	// ToValidUTF8 drops the rune the byte-wise cut in add may have halved.
	lines := strings.Split(ansi.Wrap(strings.ToValidUTF8(p.text, ""), max(p.width, minBlockWidth), ""), "\n")
	if len(lines) > minPreviewRows {
		lines = lines[len(lines)-minPreviewRows:]
	}
	p.view = strings.Join(lines, "\n")
	return p
}

// shown reports whether the preview draws anything.
func (p partial) shown() bool { return p.view != "" || p.rows.len() > 0 }

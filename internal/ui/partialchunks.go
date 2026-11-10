package ui

// A preview's finished blocks: cut out of the open text as they complete, each
// rendered once through renderMarkdown, the seam a landed block goes through too.
// render.Splitter says where a block ends; the source is handed over untrimmed, as
// kindBlock hands a landed one.
//
// Synced is the rule that keeps a fragment from being read as prose. A pane reads
// blocks only if it has heard every token of the one it is in, so a pane that
// missed one cannot be cut inside a fence it never saw open. A message start
// syncs it; losing a token unsyncs it - the one principle, applied where the token
// is lost:
//
//   - App.wants refuses it (the pane is not drawn: observe);
//   - the daemon or the window's inbox dropped it (rpc.Frame.Lost);
//   - the record has a gap, or the connection was replaced (unsyncedAll);
//   - Leave cleared the open text it would have to start from.
//
// An unsynced pane previews the block as plain text to its end, as before.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// chunk is one finished block: the source it was rendered from, kept for a width
// change, and the rows it drew.
type chunk struct {
	src  string
	rows []string
}

// blocks are an answer's finished blocks and their rows laid one under another,
// with how many blank rows end them. Immutable and shared by every copy of the DM:
// a cut appends to a new one, and a pane that reads nothing holds none.
type blocks struct {
	done     []chunk
	stack    chunked[string]
	trailing int
}

// chunks is nil-safe: a pane that has finished nothing holds no blocks.
func (b *blocks) chunks() []chunk {
	if b == nil {
		return nil
	}
	return b.done
}

// with is b and one more finished block under it, laid as render.Stack lays it:
// the cost of the block's own rows, never of the answer above it.
func (b *blocks) with(c chunk) *blocks {
	var prev blocks
	if b != nil {
		prev = *b
	}
	n := prev.stack.count()
	gap := gapUnder(prev.stack.slice(n-2, n))
	next := &blocks{
		done:  append(prev.done[:len(prev.done):len(prev.done)], c),
		stack: prev.stack.append(append(gap, c.rows...)...),
	}
	if blank := len(c.rows) - len(trimmedBlank(c.rows)); blank < len(c.rows) {
		next.trailing = blank
	} else {
		next.trailing = prev.trailing + len(gap) + blank
	}
	return next
}

// finishedBlocks is every block of done, laid one under another.
func finishedBlocks(done []chunk) *blocks {
	var b *blocks
	for _, c := range done {
		b = b.with(c)
	}
	return b
}

// formats reports whether the open text is still being read for finished blocks.
func (p partial) formats() bool { return p.synced && !p.raw && !p.scan.Frozen() }

// prose reports whether the preview is drawn the way the landed block will be:
// inside the document margin, under the finished blocks. One that never formatted
// anything is drawn as it was before they existed, and one that has drawn prose
// keeps it, so a lost token re-lays no row (laidOut latches it).
func (p partial) prose() bool { return !p.raw && (p.synced || p.fin != nil || p.prosed) }

// superseded is the preview once ev has ended its block: the block landed, the turn
// ended, or the next message began. Only a message start syncs a pane, since every
// token of a block follows one. A subagent's block landing between two of the
// agent's tokens ends none of the agent's: the answer stays, read no further for
// blocks.
func (p partial) superseded(ev core.Event) partial {
	if ev.Subagent != nil {
		return p.unsynced()
	}
	p = p.cleared()
	if ev.Kind == core.KindMessageStart {
		p.synced = true
	}
	return p
}

// unsynced is the partial of a block this pane has lost some of.
func (p partial) unsynced() partial {
	p.synced = false
	return p
}

// cut reads the open text for finished blocks and renders each one.
func (p partial) cut() partial {
	for {
		at, scan := p.scan.Next(p.text)
		p.scan = scan
		if at == 0 {
			return p
		}
		if rows := p.draw(p.text[:at]); len(rows) > 0 {
			p.fin = p.fin.with(chunk{src: p.text[:at], rows: rows})
		}
		p.text = p.text[at:]
	}
}

// draw is a block's rows at the pane's width.
func (p partial) draw(src string) []string {
	out := renderMarkdown(src, max(p.width, minBlockWidth))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// settle wraps the open text's finished lines once, for a pane not reading it for
// blocks, so a token lays out only the line it lands on. Both layouts are per
// line (render.Prose, ansi.Wrap), so the rows of a finished line never change.
// A line past render.MaxChunk keeps its newest bytes, which bounds the work.
func (p partial) settle() partial {
	if at := strings.LastIndexByte(p.text, '\n'); at >= 0 {
		done := p.text[:at+1]
		p.text = p.text[at+1:]
		p.srcs = p.srcs.append(done)
		for _, line := range strings.Split(done[:at], "\n") {
			p.lines = p.lines.append(p.lineRows(line)...)
		}
	}
	if len(p.text) > render.MaxChunk {
		p.text = p.text[len(p.text)-render.MaxChunk:]
	}
	return p
}

// lineRows is one finished line's rows. Blank lines before the first worded one
// draw nothing in prose, as the open text's head draws none (openRows).
func (p partial) lineRows(line string) []string {
	width := max(p.width, minBlockWidth)
	switch {
	case !p.prose():
		return strings.Split(ansi.Wrap(strings.ToValidUTF8(line, ""), width, ""), "\n")
	case p.lines.count() == 0 && strings.TrimSpace(line) == "":
		return nil
	}
	return strings.Split(render.Prose(strings.ToValidUTF8(line, ""), width), "\n")
}

// unsynced tells one conversation, if the model holds it, that tokens of its block
// are gone. Nothing is written for one already raw, so a stalled fleet pays once.
func (a App) unsynced(id string) App {
	if dm, ok := a.dms[id]; ok && dm.partial.synced {
		moved := *dm
		moved.partial = moved.partial.unsynced()
		return a.withDM(id, moved)
	}
	return a
}

// unsyncedAll is the same for every conversation: a gap in the record, a
// connection replaced.
func (a App) unsyncedAll() App {
	next := make(map[string]*DM, len(a.dms))
	for id, dm := range a.dms {
		if dm.partial.synced {
			moved := *dm
			moved.partial = moved.partial.unsynced()
			dm = &moved
		}
		next[id] = dm
	}
	a.dms = next
	return a
}

// applied folds a batch of frames. A frame marked Lost - tokens went missing
// before it - tells its pane first, so a message start earlier in the batch cannot
// sync it again ahead of the loss.
func (a App) applied(frames []rpc.Frame) App {
	for _, f := range frames {
		if f.Lost {
			a = a.unsynced(f.SessionID)
		}
		a = a.apply(f)
	}
	return a
}

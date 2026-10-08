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
	"slices"
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

// blocks are an answer's finished blocks and their rows laid one under another.
// Immutable and shared by every copy of the DM: a cut or a width change makes a
// new one, and a pane that reads nothing holds none.
type blocks struct {
	done  []chunk
	stack []string
}

// chunks and rows are nil-safe: a pane that has finished nothing holds no blocks.
func (b *blocks) chunks() []chunk {
	if b == nil {
		return nil
	}
	return b.done
}

func (b *blocks) rows() []string {
	if b == nil {
		return nil
	}
	return b.stack
}

// finishedBlocks is the newest of done whose rows reach want, the slack being what
// absorbs a block that is partly above the rows a pane can draw.
func finishedBlocks(done []chunk, want int) *blocks {
	rows, keep := 0, len(done)
	for keep > 0 && rows < want {
		keep--
		if rows > 0 {
			rows++ // the blank row between it and the one under it
		}
		rows += len(done[keep].rows)
	}
	b := &blocks{done: slices.Clone(done[keep:])}
	for _, c := range b.done {
		b.stack = render.Stack(b.stack, c.rows)
	}
	return b
}

// wanted is how many rows of finished blocks are worth keeping.
func (p partial) wanted() int { return max(p.cap, minPreviewRows) + previewSlack }

// formats reports whether the open text is still being read for finished blocks.
func (p partial) formats() bool { return p.synced && !p.raw && !p.scan.Frozen() }

// prose reports whether the preview is drawn the way the landed block will be:
// inside the document margin, under the finished blocks. One that never formatted
// anything is drawn as it was before they existed.
func (p partial) prose() bool { return !p.raw && (p.synced || p.fin != nil) }

// superseded is the preview once ev has ended its block: the block landed, the turn
// ended, or the next message began. Only a message start syncs a pane, since every
// token of a block follows one. A subagent's block landing between two of the
// agent's tokens ends none of the agent's, so it unsyncs.
func (p partial) superseded(ev core.Event) partial {
	p = p.cleared()
	switch {
	case ev.Kind == core.KindMessageStart:
		p.synced = true
	case ev.Subagent != nil:
		p.synced = false
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
			done := p.fin.chunks()
			p.fin = finishedBlocks(append(done[:len(done):len(done)], chunk{src: p.text[:at], rows: rows}), p.wanted())
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

// proseView is the preview of a pane that reads blocks: the newest rows of the
// finished blocks and, under them, the open one as plain text laid out the way
// glamour will lay it out, so the block that finishes keeps its column and breaks.
func (p partial) proseView() partial {
	rows := p.fin.rows()
	rows = rows[max(len(rows)-max(p.cap, 2), 0):]
	if open := p.openRows(); len(open) > 0 {
		rows = render.Stack(rows, open)
	} else {
		// A rule or a nested list ends in a blank row, which would sit above the
		// working line's own gap.
		for len(rows) > 0 && strings.TrimSpace(ansi.Strip(rows[len(rows)-1])) == "" {
			rows = rows[:len(rows)-1]
		}
	}
	if keep := max(p.cap, 0); len(rows) > keep {
		rows = rows[len(rows)-keep:]
	}
	p.view = strings.Join(rows, "\n")
	return p
}

// openRows is the open block as the rows it draws: its last previewChars bytes,
// which is all a pane can show, without the blank lines a cut leaves at its head.
func (p partial) openRows() []string {
	text := strings.ToValidUTF8(tail(p.text, previewChars(p.width, p.cap)), "")
	for {
		line, rest, found := strings.Cut(text, "\n")
		if !found || strings.TrimSpace(line) != "" {
			break
		}
		text = rest
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return strings.Split(render.Prose(text, max(p.width, minBlockWidth)), "\n")
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

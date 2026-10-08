package ui

// A preview's finished blocks: read out of the open text as they complete,
// rendered once each, and laid out above the block still being written.
//
// render.Splitter says where a block ends - a pure function of the text - and the
// rows come from renderMarkdown, the seam every markdown block a pane draws goes
// through, so a finished block is drawn by the very renderer the landed block is.
// The text is handed over untrimmed, as kindBlock hands a landed block over: a
// trim on one side is a blank row on the other.
//
// # Synced
//
// A preview may read blocks only if it has heard every token of the one it is
// previewing. A pane that starts accumulating halfway has no way to know a line
// is inside a code block, and would format a fragment as prose. So the splitter
// runs only while synced, and a pane that is not previews its block as plain text
// to the end, as it always did.
//
// Synced is set where a block is known to begin - a message start - and cleared
// where tokens can have been lost: leaving a
// conversation, coming back on screen (App.wants drops tokens, not message starts,
// for a pane that is away), and the inbox's fold trim, which keeps only the newest
// bytes of what a stalled draw loop left behind.

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

// formats reports whether the open text is still being read for finished blocks.
func (p partial) formats() bool { return p.synced && !p.raw && !p.scan.Frozen() }

// prose reports whether the preview is drawn the way the landed block will be:
// inside the document margin, under the finished blocks. A preview that never
// formatted anything is drawn as it was before they existed.
func (p partial) prose() bool { return !p.raw && (p.synced || len(p.done) > 0) }

// superseded is the preview once ev has ended the block it was previewing: the
// block landed, the turn ended, or the next message began. Only the last is where a
// pane may start reading - every token of a block follows its message's start. A
// landing leaves the pane as it was, except for a block a subagent wrote landing
// between two of the agent's tokens, which ends no block of the agent's and leaves
// the rest of this one unreadable.
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

// asRaw is the partial of a pane that never formats.
func (p partial) asRaw() partial {
	p.raw = true
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
		p = p.finished(p.text[:at])
		p.text = p.text[at:]
	}
}

// finished adds the block just cut, rendered at the pane's width.
func (p partial) finished(src string) partial {
	rows := p.draw(src)
	if len(rows) == 0 {
		return p
	}
	p.done = append(p.done[:len(p.done):len(p.done)], chunk{src: src, rows: rows})
	p.stack = render.Stack(p.stack, rows)
	return p.pruned()
}

// draw is a block's rows at the pane's width.
func (p partial) draw(src string) []string {
	out := renderMarkdown(src, max(p.width, minBlockWidth))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// pruned drops the blocks the view can no longer reach: those older than the
// newest cap+previewSlack rows, the slack being what absorbs a block that is
// partly above them.
func (p partial) pruned() partial {
	want := max(p.cap, minPreviewRows) + previewSlack
	rows, keep := 0, len(p.done)
	for keep > 0 && rows < want {
		keep--
		if rows > 0 {
			rows++ // the blank row between it and the one under it
		}
		rows += len(p.done[keep].rows)
	}
	if keep == 0 {
		return p
	}
	p.done = slices.Clone(p.done[keep:])
	return p.stacked()
}

// stacked lays the finished blocks' rows one under the other.
func (p partial) stacked() partial {
	p.stack = nil
	for _, c := range p.done {
		p.stack = render.Stack(p.stack, c.rows)
	}
	return p
}

// rerendered draws the finished blocks again, for a pane whose width moved.
func (p partial) rerendered() partial {
	if len(p.done) == 0 {
		return p
	}
	redone := make([]chunk, 0, len(p.done))
	for _, c := range p.done {
		if rows := p.draw(c.src); len(rows) > 0 {
			redone = append(redone, chunk{src: c.src, rows: rows})
		}
	}
	p.done = redone
	return p.stacked().pruned()
}

// proseView is the preview of a pane that reads blocks: the newest rows of the
// finished blocks and, under them, the open one as plain text laid out the way
// glamour will lay it out, so the block that finishes keeps its column and its
// breaks.
func (p partial) proseView() partial {
	open := p.openRows()
	rows := p.stack[max(len(p.stack)-max(p.cap, 2), 0):]
	if len(open) > 0 {
		rows = render.Stack(rows, open)
	} else {
		rows = trimBlankRows(rows)
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

// trimBlankRows drops the blank rows a block ends in: the one glamour draws under
// a rule or a nested list would otherwise sit above the working line's own gap.
func trimBlankRows(rows []string) []string {
	for len(rows) > 0 && strings.TrimSpace(ansi.Strip(rows[len(rows)-1])) == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

// unsynced is the conversation after tokens may have been lost from it. A
// finished block already drawn is still right; the rest of this one is raw.
func (d DM) unsynced() DM {
	d.partial = d.partial.unsynced()
	return d
}

// afterFoldTrim tells the conversation frame i is for, before it hears it, that the
// frame is a fold the inbox kept only the newest bytes of (foldChars). Nothing is
// written for one already raw, so a stalled fleet pays once.
func (a App) afterFoldTrim(m streamMsg, i int, f rpc.Frame) App {
	if _, lost := m.trimmed[i]; !lost {
		return a
	}
	if dm, ok := a.dms[f.SessionID]; ok && dm.partial.synced {
		return a.withDM(f.SessionID, dm.unsynced())
	}
	return a
}

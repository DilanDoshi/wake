package ui

// The scrollback: every rendered line of a conversation, and the window of it
// that is on screen. dm.go owns what goes in; dm_blocks.go owns what one event
// looks like.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// transcript is the conversation as lines, plus where the reader is in it.
//
// It is what the DM uses instead of bubbles' viewport, which cannot be fed
// incrementally: viewport.SetContent takes the whole content as a single
// string, so one more event costs a rebuild of the entire transcript - rejoined
// into one string, re-split into lines, and rescanned for the longest one.
// Measured at 4,000 events that was 11.3ms of an 11.5ms Append and it grew
// without bound, which made the DM monotonically slower the longer a session
// ran. Here, appending costs the new block and drawing costs the height of the
// pane; neither knows how long the conversation is.
//
// Its methods take value receivers and return a new transcript, so a caller
// holding an older copy keeps the scrollback and scroll position it had.
type transcript struct {
	// lines is every rendered line, wrapped for width. Chunked because it is
	// appended to on every event and copied on every append - see chunked.
	lines chunked[string]

	// prefix is a fixed boundary before lines. The room uses it after reclaiming
	// history; it moves with the oldest retained line without entering the
	// append-only sequence or the event cap.
	prefix string

	// Where each tool call sits. tools answers a click - which call is on the
	// line under the pointer - and heads answers a result arriving, which has
	// to recolour the ⏺ of the call it settles without re-rendering anything.
	//
	// Both are rebuilt wholesale by replace, because a re-wrap renumbers every
	// line. add writes a unique tool_use key in place; front trimming copies only
	// retained entries, so an older transcript keeps the map it had while the
	// current one releases ids below its first line.
	tools map[int]string
	heads map[string]int

	// Where each folded tool run's rollup line sits. runs answers a click on a
	// summary - which run to open - and runHeads answers a run growing, which
	// rewrites its one line in place. The pair mirrors tools/heads for the same
	// reasons and under the same write rule; a run's key is its first use's id,
	// as unique and unrepeated as a tool_use id. See rollup.go.
	runs     map[int]string
	runHeads map[string]int

	// scroll is the index of the top line on screen. It is held rather than
	// derived because it is the reader's position: an event arriving must not
	// move it, which is the whole reason Append samples atBottom first.
	scroll int

	width  int
	height int
}

// sized returns the transcript laid out for a pane of w by h. Both are the
// measured box, not the DM's requested size - the DM applies its floors first.
func (t transcript) sized(w, h int) transcript {
	t.width, t.height = w, h
	return t
}

// add appends one block, with the blank row that opens a new turn.
func (t transcript) add(b block) transcript {
	t, _ = t.addMeasured(b)
	return t
}

// addMeasured is add plus the number of transcript rows it appended. The room
// stores that result beside the event so front reclamation never renders the
// event again merely to rediscover its height.
func (t transcript) addMeasured(b block) (transcript, int) {
	added := blockLines(b, t.lines.count() == 0 && t.prefix == "")
	t.mark(b, t.lines.len(), added)
	t.lines = t.lines.append(added...)
	return t, len(added)
}

// replace swaps in a whole transcript, re-wrapped for a new width. The lines
// are gathered before they are stored so a re-wrap costs one bulk append
// rather than one per block.
func (t transcript) replace(blocks []block) transcript {
	return t.replaceFrom(blocks, 0, t.prefix)
}

// replaceFrom swaps in a transcript whose first rendered line has the given
// absolute index. A room history merge uses the base to keep retained live
// lines at the indices a viewport and selection already hold.
func (t transcript) replaceFrom(blocks []block, base int, prefix string) transcript {
	t.tools, t.heads, t.runs, t.runHeads = nil, nil, nil, nil
	lines := make([]string, 0, len(blocks)*2)
	for _, b := range blocks {
		added := b.laidOut
		if added == nil {
			added = blockLines(b, len(lines) == 0 && prefix == "")
		}
		t.mark(b, base+len(lines), added)
		lines = append(lines, added...)
	}
	t.lines = chunked[string]{base: base, n: base}.append(lines...)
	t.prefix = prefix
	return t
}

// trimBefore releases rendered lines and click-map entries below at. It copies
// every map it changes, preserving transcript's immutable value contract.
func (t transcript) trimBefore(at int) transcript {
	t.lines = t.lines.trimBefore(at)
	t.tools = keptLineMap(t.tools, at)
	t.runs = keptLineMap(t.runs, at)
	t.heads = keptHeadMap(t.heads, at)
	t.runHeads = keptHeadMap(t.runHeads, at)
	return t
}

func keptLineMap(held map[int]string, at int) map[int]string {
	if held == nil {
		return nil
	}
	kept := make(map[int]string, len(held))
	for line, id := range held {
		if line >= at {
			kept[line] = id
		}
	}
	return kept
}

func keptHeadMap(held map[string]int, at int) map[string]int {
	if held == nil {
		return nil
	}
	kept := make(map[string]int, len(held))
	for id, line := range held {
		if line >= at {
			kept[id] = line
		}
	}
	return kept
}

// clickableRows bounds how much of a result block is a click target.
//
// A folded result is a few rows and all of them should open it - that is what
// Claude Code's "click to expand collapsed tool results" means. An *expanded*
// one is the whole of a command's output, and marking every row of it would
// put a map entry on every line in the conversation the moment ⌃E is pressed.
// Six covers a fold whole and leaves the rest of an opened block to select.
const clickableRows = 6

// mark records where a tool block landed. A header contributes only its ⏺ row,
// so a diff drawn under it still selects; a rollup contributes its one line.
func (t *transcript) mark(b block, at int, added []string) {
	if b.tool == "" && b.run == "" {
		return
	}
	if t.tools == nil {
		t.tools, t.heads = map[int]string{}, map[string]int{}
		t.runs, t.runHeads = map[int]string{}, map[string]int{}
	}
	// blockLines may open with the blank row that separates turns, which
	// belongs to neither block.
	first := at
	if len(added) > 0 && added[0] == "" {
		first++
	}
	switch {
	case b.run != "":
		t.runs[first], t.runHeads[b.run] = b.run, first
	case b.head:
		t.tools[first], t.heads[b.tool] = b.tool, first
	default:
		for i := first; i < min(at+len(added), first+clickableRows); i++ {
			t.tools[i] = b.tool
		}
	}
}

// toolAt is the call drawn on a line, and "" for a line that is not one.
func (t transcript) toolAt(line int) string { return t.tools[line] }

// runAt is the folded run whose rollup is drawn on a line, and "" for a line
// that is not one.
func (t transcript) runAt(line int) string { return t.runs[line] }

// runHead is the row a run's rollup was drawn on, and -1 for a run this
// transcript has not drawn - which is a run that is expanded or off screen.
func (t transcript) runHead(key string) int {
	if at, ok := t.runHeads[key]; ok {
		return at
	}
	return -1
}

// restyle rewrites one rendered line in place. It is how a tool call settles
// from running to finished, and how a folded run's count grows: the one line
// changes and nothing else moves, at the cost of one chunk rather than a
// re-render of the conversation.
func (t transcript) restyle(line, width int, s string) transcript {
	_, isTool := t.tools[line]
	_, isRun := t.runs[line]
	if !isTool && !isRun {
		return t
	}
	t.lines = t.lines.replace(line, ansi.Truncate(s, width, ""))
	return t
}

// headLine is the row a call's ⏺ was drawn on, and false for a call this
// transcript has not drawn.
func (t transcript) headLine(id string) (int, bool) {
	at, ok := t.heads[id]
	return at, ok
}

// blockLines is one block as transcript lines: its own, and the blank row that
// separates it from the turn before it.
//
// The first block in a transcript gets no separator, and neither does a tool
// result - it belongs directly under the ⏺ call it answers, and a blank row
// between the two halves reads as two unrelated things.
func blockLines(b block, first bool) []string {
	lines := strings.Split(b.text, "\n")
	if first || b.joined {
		return lines
	}
	return append([]string{""}, lines...)
}

// bottom is the scroll position showing the last line.
func (t transcript) first() int {
	if t.prefix != "" {
		return t.lines.first() - 1
	}
	return t.lines.first()
}

func (t transcript) bottom() int { return max(t.first(), t.lines.len()-t.height) }

// atBottom reports whether the newest line is on screen - which is to say,
// whether the reader is following the conversation rather than reading back.
func (t transcript) atBottom() bool { return t.scroll >= t.bottom() }

// toBottom returns to the newest line.
func (t transcript) toBottom() transcript {
	t.scroll = t.bottom()
	return t
}

// scrolledUp moves the reader n lines back through the conversation.
func (t transcript) scrolledUp(n int) transcript {
	t.scroll = min(max(t.scroll-n, t.first()), t.bottom())
	return t
}

// view draws the window, measuring exactly width by height.
//
// A line wider than the pane is cut rather than wrapped: lipgloss's Width
// wraps, and one wrapped line would push everything below it off the bottom of
// the pane. The cut carries no ellipsis and there is no horizontal scroll to
// reach what it removed - deferred.md's I2 owns that, and it belongs to the
// renderers that produce the over-wide line rather than to the pane that has
// to fit it.
// A non-zero selection marks the cells a drag has taken. It is resolved for
// this transcript by the caller - App decides which pane owns it - so nothing
// here knows about panes or ids.
func (t transcript) view(m marked) string {
	top := min(max(t.scroll, t.first()), t.bottom())
	first := top
	lines := make([]string, 0, t.height)
	if t.prefix != "" && top == t.lines.first()-1 {
		lines = append(lines, t.prefix)
		top++
	}
	lines = append(lines, t.lines.slice(top, top+t.height-len(lines))...)
	for i, l := range lines {
		l = t.visibleLine(l)
		if c0, c1, ok := m.covers(first + i); ok {
			if c1 == lineEnd {
				c1 = t.width
			}
			l = highlighted(l, c0, c1)
		}
		lines[i] = l
	}
	return lipgloss.NewStyle().
		Width(t.width).
		Height(t.height).
		MaxWidth(t.width).
		MaxHeight(t.height).
		Render(strings.Join(lines, "\n"))
}

// visibleLine is the exact pre-highlight representation both View and clipboard
// extraction expose. Truncate before selectedText applies its ANSI-aware cell
// slice, so an intermediate lineEnd cannot copy cells outside the pane.
func (t transcript) visibleLine(line string) string {
	return ansi.Truncate(line, t.width, "")
}

// selectionLines returns the rendered lines a selection may copy, including
// the room's visible fixed prefix when the selection crosses it.
func (t transcript) selectionLines(m marked) ([]string, int) {
	first := max(m.from.line, t.first())
	lines := make([]string, 0, m.to.line-first+1)
	marker := t.lines.first() - 1
	if t.prefix != "" && first <= marker && m.to.line >= marker {
		first = marker
		lines = append(lines, t.visibleLine(t.prefix))
	}
	from := max(m.from.line, t.lines.first())
	for _, line := range t.lines.slice(from, m.to.line+1) {
		lines = append(lines, t.visibleLine(line))
	}
	return lines, first
}

// highlighted restyles the cells in [c0, c1) of one rendered line.
//
// The selected part is stripped before it is restyled rather than wrapped in a
// background, and that is the one visible compromise in this feature: glamour
// ends every span with an SGR reset, and a background applied around a string
// containing one ends at that reset - so the highlight would break into stripes
// mid-word. Stripping costs the syntax colour inside a selection, which is what
// a terminal's own selection costs too.
func highlighted(line string, c0, c1 int) string {
	w := ansi.StringWidth(line)
	c0, c1 = min(max(c0, 0), w), min(max(c1, 0), w)
	if c1 <= c0 {
		return line
	}
	return ansi.Cut(line, 0, c0) +
		SelectionStyle.Render(ansi.Strip(ansi.Cut(line, c0, c1))) +
		ansi.Cut(line, c1, w)
}

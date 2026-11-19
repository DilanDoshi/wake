package ui

// The room folds an agent's repeated "↻ resumed" lines into one with a count
// (decisions.md, 2026-11-18). The daemon sends a real `continue` per account flip and
// the limit stops the agent again, so only the room's drawing folds: a resume of an
// agent whose run is open is not a line at all, and the run's first line is redrawn in
// place as `↻ name resumed after the usage limit ×N`.
//
// A run ends at any other line of the agent's thread (focusAdmits), so an answered
// resume is followed by a new line. Not ending it: the next resume of the same kind,
// and a turn-end marker the API failed (the first stays drawn, the rest fold); a
// marker it did not fail ends the run and stays. The count is Room.counts by the
// head's id because roomLine takes no field: room_retention_test.go budgets its
// copies. One pure step, run per change by appendLine and over a restore by foldHist.

import (
	"fmt"
	"maps"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// resumeCountFormat is the count a folded line ends in; it is drawn last so a
// narrow pane cuts the words and keeps the figure, which is the news.
const resumeCountFormat = " ×%d"

// noticeFailedTurn is the room's stamp on a turn-end marker whose turn the API
// failed, read off the fleet's per-agent bit (turnoutcome.go) as the line is
// appended. UI-only, like noticeTakenBack.
const noticeFailedTurn core.Notice = "failed_turn"

// resumeRun is one agent's open run: the said index of the line that draws it,
// which kind its resumes are and whether a failed turn's marker is already drawn
// under it.
type resumeRun struct {
	head        int
	reconnected bool
	marked      bool
}

// resumeRuns is every open run by agent id. Copied on write, like the Room that
// holds it.
type resumeRuns map[string]resumeRun

type resumeAction int

const (
	resumeNothing  resumeAction = iota // the line is drawn as any other
	resumeHeads                        // a resume that opens a run
	resumeFolds                        // a resume that joins one: the head counts it
	resumeSwallows                     // a failed turn's marker the run already drew
)

// resumeStep is what one line did to the runs: the action, and for a fold the
// said index of the head that now stands for it.
type resumeStep struct {
	action resumeAction
	head   int
}

// hides reports a line that is not drawn because a counted line stands for it.
func (s resumeStep) hides() bool { return s.action == resumeFolds || s.action == resumeSwallows }

func isResume(ev core.Event) bool {
	return ev.Kind == core.KindUserText && ev.Notice == core.NoticeAutoResumed
}

func failedMarker(ev core.Event) bool {
	return ev.Kind == core.KindTurnEnd && ev.Notice == noticeFailedTurn
}

// stamped is l with its marker stamped if its turn failed.
func stamped(l roomLine) roomLine {
	if l.ev.Kind == core.KindTurnEnd && l.by.failed {
		l.ev.Notice = noticeFailedTurn
	}
	return l
}

// step is the whole rule: what l, which would sit at index at of a history whose
// oldest line is first, does to the open runs. A head older than first has been
// reclaimed and cannot be folded into.
func (s resumeRuns) step(l roomLine, at, first int) (resumeRuns, resumeStep) {
	id := l.ev.SessionID
	switch run, open := s[id]; {
	case isResume(l.ev):
		reconnect := reconnected(l.ev.MessageID)
		if open && run.head >= first && run.reconnected == reconnect {
			return s, resumeStep{action: resumeFolds, head: run.head}
		}
		return s.with(id, resumeRun{head: at, reconnected: reconnect}), resumeStep{action: resumeHeads}
	case failedMarker(l.ev) && open && run.head >= first:
		if run.marked {
			return s, resumeStep{action: resumeSwallows}
		}
		run.marked = true
		return s.with(id, run), resumeStep{}
	case failedMarker(l.ev):
		return s, resumeStep{}
	default:
		return s.endedBy(l), resumeStep{}
	}
}

// with is s holding run for id.
func (s resumeRuns) with(id string, run resumeRun) resumeRuns {
	next := maps.Clone(s)
	if next == nil {
		next = resumeRuns{}
	}
	next[id] = run
	return next
}

// endedBy is s without the runs of every thread l belongs to - the agent's own
// line, a message to it, a broadcast - and s itself when it ends none.
func (s resumeRuns) endedBy(l roomLine) resumeRuns {
	var ended []string
	for id := range s {
		if focusAdmits(l, id) {
			ended = append(ended, id)
		}
	}
	if len(ended) == 0 {
		return s
	}
	next := maps.Clone(s)
	for _, id := range ended {
		delete(next, id)
	}
	return next
}

// foldHist folds a restored history: the lines a run's later resumes and
// swallowed markers stand for are dropped, in place - the slice is the caller's
// own copy, as renderAll writes rows into one - and the heads' counts are
// returned by line id. What Room.appendLine would have made of them one by one.
func foldHist(lines []roomLine) ([]roomLine, map[uint64]int) {
	var runs resumeRuns
	var counts map[uint64]int
	kept := lines[:0]
	for _, l := range lines {
		var st resumeStep
		runs, st = runs.step(l, len(kept), 0)
		switch st.action {
		case resumeFolds:
			id := kept[st.head].id
			if counts == nil {
				counts = map[uint64]int{}
			}
			counts[id] = max(counts[id], 1) + 1
		case resumeSwallows:
		default:
			kept = append(kept, l)
		}
	}
	return kept, counts
}

// openRuns is the runs still open after lines, the first of which sits at index
// at0 of its history: what the live part of a merged history goes on folding into.
func openRuns(lines []roomLine, at0 int) resumeRuns {
	var runs resumeRuns
	for i, l := range lines {
		runs, _ = runs.step(l, at0+i, at0)
	}
	return runs
}

// withCount is counts holding n for id, copied on write.
func withCount(counts map[uint64]int, id uint64, n int) map[uint64]int {
	next := maps.Clone(counts)
	if next == nil {
		next = map[uint64]int{}
	}
	next[id] = n
	return next
}

// unionCounts is a and b together, nil when both are empty.
func unionCounts(a, b map[uint64]int) map[uint64]int {
	if len(a)+len(b) == 0 {
		return nil
	}
	out := maps.Clone(a)
	if out == nil {
		out = make(map[uint64]int, len(b))
	}
	maps.Copy(out, b)
	return out
}

// drawn is the block for one line: roomBlock's, except a resume that stands for
// several, which draws once with the count.
func (r Room) drawn(l roomLine, expanded bool) block {
	if n := r.counts[l.id]; n > 1 {
		return block{text: repeatedResume(l.ev, l.by, n, r.blockWidth())}
	}
	return renderRoomBlock(l.ev, l.by, l.recipient, r.blockWidth(), expanded)
}

// repeatedResume is the resume line with its count, cut to width from the
// words so the count stays.
func repeatedResume(ev core.Event, a Agent, n, width int) string {
	count := fmt.Sprintf(resumeCountFormat, n)
	words := ansi.Truncate(autoResumedText(ev, a), max(width-ansi.StringWidth(count), 0), ellipsis)
	return mutedLine(words+count, width)
}

// folded is the room after a line the runs folded away: it is not appended, so
// nothing is spent but the runs, and a fold redraws its head.
func (r Room) folded(runs resumeRuns, st resumeStep) Room {
	r.lineMoves, r.runs = nil, runs
	if st.action == resumeFolds {
		r = r.countedHead(st.head)
	}
	return r
}

// countedHead counts one more resume on the run's head line and redraws its row.
// The row is the one it already had - a resume is one muted line however wide the
// pane - so nothing below it moves and no reader's place needs translating.
func (r Room) countedHead(idx int) Room {
	head := r.said.at(idx)
	r.counts = withCount(r.counts, head.id, max(r.counts[head.id], 1)+1)
	if head.rows == 0 { // a focus hides it; the count waits for it to be shown
		return r
	}
	rows := blockLines(r.drawn(head, false), false)
	r.tr = r.tr.rewritten(r.rowOf(idx), rows[max(len(rows)-head.rows, 0):])
	return r
}

// rowOf is the absolute transcript row where the line at said index idx begins.
func (r Room) rowOf(idx int) int {
	at := r.tr.lines.first()
	if !r.reclaimed {
		at += len(blockLines(roomBanner(r.blockWidth()), true))
	}
	for i := r.said.first(); i < idx; i++ {
		at += r.said.at(i).rows
	}
	return at
}

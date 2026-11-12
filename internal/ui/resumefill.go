package ui

// The resume picker opens on the directory listing and its rows fill in as their
// transcripts are read (Sessions.Resumable), so a machine with gigabytes of
// transcripts opens it at once instead of after reading all of them.
//
// What the listing knows is each session's id, project and age - which is also
// its place in the list, so a fill never moves a row. What it does not know is
// the directory, the name and the last prompt: a stranger's row draws
// `reading…` for those until its fill lands. A parked session's row needs
// none of them (the park book holds its directory and name), so it is complete
// from the first frame.
//
// A row still being read cannot be verified, so ↵ over one holds - the picker
// stays up, nothing is sent - rather than ask the daemon to resume a session
// whose directory nobody has proved. It is a moment, and the notice says so.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
)

// resumeFillBatch bounds the rows one message carries, so a fast walk is a few
// redraws rather than one per session and a slow one is not held for a full
// batch.
const resumeFillBatch = 64

// resumeStillReading is ↵ over a row whose transcript is still being read.
const resumeStillReading = "%s is still being read - ↵ again in a moment"

// resumeFilledMsg is rows whose transcripts have been read, folded by App.Update.
// done says nothing more will come; otherwise filled is where the next rows are.
type resumeFilledMsg struct {
	seq    uint64
	rows   []DiskSession
	filled <-chan DiskSession
	done   bool
}

// fillCmd waits on the walk's own goroutines for the next rows - at least one,
// then whatever else is already there - and hands them over. One command per
// message: the model re-issues it until the channel closes, so nothing polls.
func fillCmd(filled <-chan DiskSession, seq uint64) tea.Cmd {
	if filled == nil {
		return nil
	}
	return func() tea.Msg {
		first, ok := <-filled
		if !ok {
			return resumeFilledMsg{seq: seq, done: true}
		}
		batch := []DiskSession{first}
		for len(batch) < resumeFillBatch {
			select {
			case d, ok := <-filled:
				if !ok {
					return resumeFilledMsg{seq: seq, rows: batch, done: true}
				}
				batch = append(batch, d)
			default:
				return resumeFilledMsg{seq: seq, rows: batch, filled: filled}
			}
		}
		return resumeFilledMsg{seq: seq, rows: batch, filled: filled}
	}
}

// resumeFilled folds rows that have been read into the picker that asked for
// them. A picker that has closed, or been replaced by a later /resume, takes
// nothing and asks for nothing more: closing it ended the reads.
func (a App) resumeFilled(m resumeFilledMsg) (App, tea.Cmd) {
	if !a.resumePicker.Open() || m.seq != a.resumePicker.Seq {
		return a, nil
	}
	a.resumePicker = a.resumePicker.filledWith(m.rows)
	if m.done {
		return a, nil
	}
	return a, fillCmd(m.filled, m.seq)
}

// filledWith is the picker with the rows these sessions describe completed. A
// row is matched by id and project, since one id can sit under two projects and
// the picker holds the newer. The cursor stays on the session it was on: a fill
// can make a row match the query, which moves the index of everything below it.
func (p ResumePicker) filledWith(read []DiskSession) ResumePicker {
	type key struct{ id, slug string }
	byKey := make(map[key]DiskSession, len(read))
	for _, d := range read {
		byKey[key{d.ID, d.Slug}] = d
	}
	var cursored string
	if f := p.filtered(); p.Cursor >= 0 && p.Cursor < len(f) {
		cursored = f[p.Cursor].ID
	}
	rows := make([]resumeRow, len(p.Rows))
	for i, r := range p.Rows {
		if d, ok := byKey[key{r.ID, r.slug}]; ok && r.Reading {
			r.Dir, r.Preview, r.Title, r.Resumable, r.Reading = d.Dir, d.Preview, d.Title, d.Dir != "", false
		}
		rows[i] = r
	}
	p.Rows = rows
	f := p.filtered()
	for i, r := range f {
		if r.ID == cursored {
			p.Cursor = i
			break
		}
	}
	p.Cursor = clamp(p.Cursor, 0, max(len(f)-1, 0))
	return p
}

// unread is how many rows are still waiting on their transcript.
func (p ResumePicker) unread() int {
	n := 0
	for _, r := range p.Rows {
		if r.Reading {
			n++
		}
	}
	return n
}

// holdsForReading says ↵ must wait, and why, when any chosen row is unread.
func holdsForReading(chosen []resumeRow) bool {
	for _, r := range chosen {
		if r.Reading {
			notice.Report(resumeStillReading, r.label())
			return true
		}
	}
	return false
}

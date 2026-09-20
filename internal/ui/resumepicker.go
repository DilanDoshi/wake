package ui

// The resume picker: the list a bare `/resume` opens over the composer, merging
// parked sessions and on-disk conversations into one recency-sorted set, each
// row resuming its session **in place** on ↵.
//
// It is a bespoke type rather than the Picker (a single-value config chooser)
// for RewindPicker's reason: it holds many rows with several fields, and the
// room's copy is multi-select. Its keys are intercepted above App.key's switch
// like pickerKey/rewindKey, so it adds no legendEntries entry; the menu
// advertises them on itself.
//
// Room = multi-select (␣ toggles, ↵ resumes the checked set), DM = single-select
// (↵ resumes the cursor row) — decided by App.focus at open time, carried as
// Multi. A row's source (parked vs on-disk stranger) is read from Parked: it
// chooses FrameWake or FrameResume on confirm, and shows as an @name or a short
// id in the row. See resume.go for how the rows are gathered and confirmResume
// wired, and resumepicker.go's confirm for the mixed-batch write.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// resumePickerCap bounds the rows a bare `/resume` draws, adoptRows' own reason:
// a machine has hundreds of transcripts and a pane cannot hold them, and the
// listing is ordered by recency, so the newest are the ones somebody means. The
// rest are reached by name (`/resume @who`) or by `/adopt`.
const resumePickerCap = 10

// noDirNote is the directory column for a row with no provable directory - shown
// but the daemon refuses it on confirm, /adopt's "this side decides nothing".
const noDirNote = "(no directory — open it where it lives)"

// resumingN is the ask for more than one row; the single case reuses resume.go's
// resumeAsked, and neither says "parked" because the picker resumes disk rows too.
const resumingN = "bringing %d sessions back…"

// resumeRow is one resumable conversation: a parked session or an on-disk
// stranger, told apart by Parked (which chooses the frame) and by whether it
// carries a Name.
type resumeRow struct {
	ID        string
	Name      string // "@name" without the prefix for a parked row; "" for a disk stranger
	Dir       string
	Label     string // branch, where known
	Preview   string // first-prompt snippet, for a disk row
	Age       string // coarse relative time
	Parked    bool   // FrameWake vs FrameResume on confirm
	Resumable bool   // false when Dir == "" (shown, but the daemon refuses)
}

// label is how a row names itself in a notice: the @name for a parked session,
// the short id for a stranger the operator has only ever seen by id.
func (r resumeRow) label() string {
	if r.Name != "" {
		return agentPrefix + r.Name
	}
	return shortSource(r.ID)
}

// ResumePicker is the open picker. Multi is the room's; a DM's is single-select.
type ResumePicker struct {
	Rows     []resumeRow
	More     int // how many older rows the cap left out
	Multi    bool
	Cursor   int
	Selected map[int]bool
}

// Open reports whether there is a picker at all. Empty is closed - a machine
// with nothing to resume takes the notice path rather than an empty menu.
func (p ResumePicker) Open() bool { return len(p.Rows) > 0 }

// openResume puts the picker up over the composer that opened it.
func (a App) openResume(rows []resumeRow, more int, multi bool) App {
	a.resumePicker = ResumePicker{Rows: rows, More: more, Multi: multi, Selected: map[int]bool{}}
	return a.clearDraft()
}

// closeResume takes it down with nothing sent.
func (a App) closeResume() App {
	a.resumePicker = ResumePicker{}
	return a
}

// resumePickerKey is ↑↓ to move, ␣ to toggle (room only), ↵ to resume and esc to
// cancel - claude's own list keys, plus the checkbox. Read above App.key's switch
// like pickerKey, so it adds no legend entry; a key it declines dismisses the
// picker in App.update, the pickers' shared rule.
func (a App) resumePickerKey(m tea.KeyMsg) (App, tea.Cmd, bool) {
	if !a.resumePicker.Open() {
		return a, nil, false
	}
	// ␣ toggles the cursored row, but only in the room's multi-select picker; a
	// single-select DM lets it fall through like every other picker, which is
	// what dismisses one on a key it does not own.
	if a.resumePicker.Multi && isSpace(m) {
		return a.toggleResume(), nil, true
	}
	switch m.Type {
	case tea.KeyUp:
		return a.moveResume(-1), nil, true
	case tea.KeyDown:
		return a.moveResume(1), nil, true
	case tea.KeyEsc:
		return a.closeResume(), nil, true
	case tea.KeyEnter:
		next, cmd := a.confirmResume()
		return next, cmd, true
	}
	return a, nil, false
}

// isSpace is space however the terminal delivers it - some send tea.KeySpace,
// some a KeyRunes carrying ' ' - so the toggle works either way.
func isSpace(m tea.KeyMsg) bool {
	return m.Type == tea.KeySpace || (m.Type == tea.KeyRunes && len(m.Runes) == 1 && m.Runes[0] == ' ')
}

// moveResume walks the list without wrapping, movePicker's reason: a cursor that
// wraps makes the ends of the list indistinguishable at a glance.
func (a App) moveResume(by int) App {
	p := a.resumePicker
	p.Cursor = clamp(p.Cursor+by, 0, max(len(p.Rows)-1, 0))
	a.resumePicker = p
	return a
}

// toggleResume flips the cursored row's checkbox. A copied map, App's
// immutability rule.
func (a App) toggleResume() App {
	p := a.resumePicker
	if p.Cursor < 0 || p.Cursor >= len(p.Rows) {
		return a
	}
	sel := make(map[int]bool, len(p.Selected)+1)
	for k, v := range p.Selected {
		sel[k] = v
	}
	sel[p.Cursor] = !sel[p.Cursor]
	p.Selected = sel
	a.resumePicker = p
	return a
}

// chosen is the rows ↵ resumes: the checked set in a multi picker, or the cursor
// row - which is also the multi fallback when nothing is checked, so ↵ on a row
// nobody ticked still resumes it, single-select's own behaviour.
func (p ResumePicker) chosen() []resumeRow {
	if p.Multi {
		out := make([]resumeRow, 0, len(p.Selected))
		for i := range p.Rows {
			if p.Selected[i] {
				out = append(out, p.Rows[i])
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if p.Cursor >= 0 && p.Cursor < len(p.Rows) {
		return []resumeRow{p.Rows[p.Cursor]}
	}
	return nil
}

// resumeFrames is the mixed batch: a parked row wakes (FrameWake, unparkRecord),
// an on-disk row resumes in place (FrameResume). One write for the set, /resume
// all's rule - rpc's write lock is process-wide.
func resumeFrames(rows []resumeRow) []rpc.Frame {
	out := make([]rpc.Frame, 0, len(rows))
	for _, r := range rows {
		kind := rpc.FrameResume
		if r.Parked {
			kind = rpc.FrameWake
		}
		out = append(out, rpc.Frame{Kind: kind, SessionID: r.ID})
	}
	return out
}

// confirmResume writes one wake or resume per chosen row and remembers the ids,
// so wakeArrived says each is back when its report shows it running.
func (a App) confirmResume() (App, tea.Cmd) {
	chosen := a.resumePicker.chosen()
	a = a.closeResume()
	if len(chosen) == 0 {
		return a, nil
	}
	a = a.clearDraft()
	ids := make([]string, 0, len(chosen))
	for _, r := range chosen {
		ids = append(ids, r.ID)
	}
	if len(chosen) == 1 {
		notice.Report(resumeAsked, "", chosen[0].label())
	} else {
		notice.Report(resumingN, len(chosen))
	}
	return a.awaitingWake(ids...), a.write(resumeFailed, resumeFrames(chosen)...)
}

// View draws it through the same rows a card and the other pickers draw. The
// keys are advertised on the menu itself (the completion menu's own reason), so
// the picker earns no legend entry for them.
func (p ResumePicker) View(width int) string {
	if !p.Open() {
		return ""
	}
	rows := make([]string, 0, len(p.Rows)+3)
	rows = append(rows, detailRow(p.header(), width))
	for i := range p.Rows {
		rows = append(rows, optionRow(p.rowLabel(i), width, i == p.Cursor, false, AccentStyle))
	}
	if p.More > 0 {
		rows = append(rows, detailRow(fmt.Sprintf("… %d more, older", p.More), width))
	}
	rows = append(rows, detailRow(p.keyHint(), width))
	return strings.Join(rows, "\n")
}

// header counts the two sources, so the menu says what it is a list of.
func (p ResumePicker) header() string {
	parked, disk := 0, 0
	for _, r := range p.Rows {
		if r.Parked {
			parked++
		} else {
			disk++
		}
	}
	return fmt.Sprintf("resume · %d parked · %d on disk", parked, disk)
}

// keyHint is the key line the menu advertises, and it names ␣ only where it does
// something - the room's multi-select.
func (p ResumePicker) keyHint() string {
	if p.Multi {
		return "␣ select · ↵ resume · esc cancel"
	}
	return "↑↓ move · ↵ resume · esc cancel"
}

// rowLabel is one row as the operator reads it: the checkbox (room only), then
// the @name or short id, the directory (or the no-dir note), the branch, a
// preview snippet and a coarse age - joined by · so a whitespace-collapsing
// optionRow keeps them apart.
func (p ResumePicker) rowLabel(i int) string {
	r := p.Rows[i]
	fields := []string{r.label()}
	if r.Dir != "" {
		fields = append(fields, r.Dir)
	} else {
		fields = append(fields, noDirNote)
	}
	if r.Label != "" {
		fields = append(fields, r.Label)
	}
	if r.Preview != "" {
		fields = append(fields, "\""+r.Preview+"\"")
	}
	if r.Age != "" {
		fields = append(fields, r.Age)
	}
	body := strings.Join(fields, " · ")
	if p.Multi {
		box := "[ ] "
		if p.Selected[i] {
			box = "[x] "
		}
		return box + body
	}
	return body
}

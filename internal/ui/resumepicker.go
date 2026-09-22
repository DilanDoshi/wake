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
// # It is a search box, not a short list
//
// A heavy user has hundreds of on-disk conversations, so the picker holds the
// whole recency-sorted set and **type-to-search** narrows it, the way Claude
// Code's own resume picker does: printable keys build a query, the list filters
// live to what matches (name, directory, branch, preview or id), and a window of
// the matches is drawn around the cursor with a `n of m` count. It is a modal
// search box - routed above the card and the other pickers, it claims every key
// while it is up, so typing filters rather than dismissing and a stray key does
// not lose the query - and ⎋ cancels, ↵ resumes, ⌃C parks (its first press stays
// visible for the kill switch's invariant).
//
// Room = multi-select (⇥ toggles, ↵ resumes the checked set), DM = single-select
// (↵ resumes the cursor row) - decided by App.focus at open time, carried as
// Multi. ⇥ rather than ␣ toggles, because ␣ is a search character now. A row's
// source (parked vs on-disk stranger) is read from Parked: it chooses FrameWake
// or FrameResume on confirm, and shows as an @name or a short id in the row. See
// resume.go for how the rows are gathered and confirmResume wired.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// resumeWindow is how many rows are drawn at once. The picker holds every match
// but the pane over the composer is a handful of rows tall, so the display is a
// window around the cursor and the count header says where in the set it is.
const resumeWindow = 8

// resumePickerMax bounds the rows the picker holds - not what it draws (that is
// resumeWindow), but what search can reach. A heavy user has hundreds of
// sessions and search wants all of them; the bound is a memory sanity limit far
// above any real machine's count, and the recency sort means what it drops is
// the oldest, still reachable by `/resume @who` or `/adopt`.
const resumePickerMax = 500

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

// matches reports whether a row satisfies a lower-cased query: every whitespace-
// separated term must appear somewhere in the row's own text. AND rather than OR
// so a second word narrows, which is what a search box is for.
func (r resumeRow) matches(query string) bool {
	hay := strings.ToLower(strings.Join([]string{r.label(), r.Name, r.Dir, r.Label, r.Preview, r.ID}, " "))
	for _, term := range strings.Fields(query) {
		if !strings.Contains(hay, term) {
			return false
		}
	}
	return true
}

// ResumePicker is the open picker. Multi is the room's; a DM's is single-select.
//
// Selected is keyed by row **id**, not by row index, so a checked row survives
// the list being re-filtered as the query changes - an index would point at a
// different row the moment a term narrowed the set.
type ResumePicker struct {
	Rows     []resumeRow
	More     int // how many older rows the cap left out
	Multi    bool
	Query    string
	Cursor   int // index into the filtered set
	Selected map[string]bool
}

// Open reports whether there is a picker at all. Empty is closed - a machine
// with nothing to resume takes the notice path rather than an empty menu.
func (p ResumePicker) Open() bool { return len(p.Rows) > 0 }

// filtered is the rows the query admits, in the held recency order. An empty
// query is every row.
func (p ResumePicker) filtered() []resumeRow {
	q := strings.ToLower(strings.TrimSpace(p.Query))
	if q == "" {
		return p.Rows
	}
	out := make([]resumeRow, 0, len(p.Rows))
	for _, r := range p.Rows {
		if r.matches(q) {
			out = append(out, r)
		}
	}
	return out
}

// openResume puts the picker up over the composer that opened it.
func (a App) openResume(rows []resumeRow, more int, multi bool) App {
	a.resumePicker = ResumePicker{Rows: rows, More: more, Multi: multi, Selected: map[string]bool{}}
	return a.clearDraft()
}

// closeResume takes it down with nothing sent.
func (a App) closeResume() App {
	a.resumePicker = ResumePicker{}
	return a
}

// resumePickerKey drives the search box: printable keys build the query, ⌫ edits
// it, ↑↓ walk the matches, ⇥ toggles a row (room only), ↵ resumes and ⎋ cancels.
// Read above App.key's switch like pickerKey, so it adds no legend entry and it
// captures typing before the composer sees it - which is what makes it a search
// box rather than a list a keystroke dismisses.
func (a App) resumePickerKey(m tea.KeyMsg) (App, tea.Cmd, bool) {
	if !a.resumePicker.Open() {
		return a, nil, false
	}
	switch m.Type {
	case tea.KeyCtrlC:
		// ⌃C keeps its meaning - close the picker and park the focused agent - so
		// its first press stays visible. The emergency ⌃C⌃C kill switch
		// (cmd/wake/killswitch.go) arms on the tty byte no matter what the picker
		// does, and relies on the first press doing something so a second is intent
		// rather than the reflex that follows a press that appeared to do nothing.
		// Swallowing it with the rest would turn a natural retry into an unmeant
		// emergency exit that leaves the fleet running. park returns a tea.Model
		// (always this App), asserted back so the picker's own return type holds.
		model, cmd, _ := a.closeResume().park()
		return model.(App), cmd, true
	case tea.KeyUp:
		return a.moveResume(-1), nil, true
	case tea.KeyDown:
		return a.moveResume(1), nil, true
	case tea.KeyEsc:
		return a.closeResume(), nil, true
	case tea.KeyEnter:
		next, cmd := a.confirmResume()
		return next, cmd, true
	case tea.KeyTab:
		// ⇥ toggles a row's checkbox in the room's multi-select picker; in a
		// single-select DM it is a no-op the picker still swallows, so ⇥ does not
		// leave the picker to move the keys to another pane mid-search.
		if a.resumePicker.Multi {
			return a.toggleResume(), nil, true
		}
		return a, nil, true
	case tea.KeyBackspace:
		return a.backspaceQuery(), nil, true
	case tea.KeySpace:
		return a.appendQuery(" "), nil, true
	case tea.KeyRunes:
		return a.appendQuery(string(m.Runes)), nil, true
	}
	// Every other key is swallowed while the picker is up, so it is a proper
	// search box: an arrow or a chord does not dismiss it mid-query the way an
	// unowned key dismisses the config and rewind pickers, which have no typed
	// state to lose. ⎋ cancels, ↵ resumes, and ⌃C parks (its own case above); no
	// other key leaves it.
	return a, nil, true
}

// appendQuery adds to the search and re-homes the cursor, since the match set
// has changed under it. A copied picker, App's immutability rule.
func (a App) appendQuery(s string) App {
	p := a.resumePicker
	p.Query += s
	p.Cursor = 0
	a.resumePicker = p
	return a
}

// backspaceQuery drops the query's last rune. Runes, not bytes, so a multi-byte
// character deletes as one keypress.
func (a App) backspaceQuery() App {
	p := a.resumePicker
	if r := []rune(p.Query); len(r) > 0 {
		p.Query = string(r[:len(r)-1])
	}
	p.Cursor = 0
	a.resumePicker = p
	return a
}

// moveResume walks the matches without wrapping, movePicker's reason: a cursor
// that wraps makes the ends of the list indistinguishable at a glance.
func (a App) moveResume(by int) App {
	p := a.resumePicker
	p.Cursor = clamp(p.Cursor+by, 0, max(len(p.filtered())-1, 0))
	a.resumePicker = p
	return a
}

// toggleResume flips the cursored match's checkbox, keyed by its id so the tick
// survives a later filter. A copied map, App's immutability rule.
func (a App) toggleResume() App {
	p := a.resumePicker
	f := p.filtered()
	if p.Cursor < 0 || p.Cursor >= len(f) {
		return a
	}
	id := f[p.Cursor].ID
	sel := make(map[string]bool, len(p.Selected)+1)
	for k, v := range p.Selected {
		sel[k] = v
	}
	sel[id] = !sel[id]
	p.Selected = sel
	a.resumePicker = p
	return a
}

// chosen is the rows ↵ resumes: the checked set in a multi picker - taken from
// the whole held set, so a tick made before a filter still counts - or the
// cursored match, which is also the multi fallback when nothing is checked.
func (p ResumePicker) chosen() []resumeRow {
	if p.Multi {
		out := make([]resumeRow, 0, len(p.Selected))
		for _, r := range p.Rows {
			if p.Selected[r.ID] {
				out = append(out, r)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	f := p.filtered()
	if p.Cursor >= 0 && p.Cursor < len(f) {
		return []resumeRow{f[p.Cursor]}
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

// View draws it through the same rows a card and the other pickers draw: a count
// header, the search line, a window of the matches around the cursor, and the
// key hint. The keys are advertised on the menu itself (the completion menu's
// own reason), so the picker earns no legend entry for them.
func (p ResumePicker) View(width int) string {
	if !p.Open() {
		return ""
	}
	f := p.filtered()
	rows := make([]string, 0, resumeWindow+4)
	rows = append(rows, detailRow(p.header(f), width))
	rows = append(rows, detailRow(p.searchLine(), width))
	if len(f) == 0 {
		rows = append(rows, detailRow("no session matches — ⌫ to widen the search", width))
	}
	start, end := p.window(len(f))
	for i := start; i < end; i++ {
		rows = append(rows, optionRow(p.rowLabel(f[i]), width, i == p.Cursor, false, AccentStyle))
	}
	if p.More > 0 {
		rows = append(rows, detailRow(fmt.Sprintf("… %d more, older", p.More), width))
	}
	rows = append(rows, detailRow(p.keyHint(), width))
	return strings.Join(rows, "\n")
}

// window is the slice of the match set the pane draws, kept around the cursor so
// walking off the visible end pages the list rather than losing the cursor.
func (p ResumePicker) window(n int) (start, end int) {
	if n <= resumeWindow {
		return 0, n
	}
	start = clamp(p.Cursor-resumeWindow/2, 0, n-resumeWindow)
	return start, start + resumeWindow
}

// header is the Claude-style count: which match the cursor is on, of how many.
// When a query admits nothing it says so against the whole set instead.
func (p ResumePicker) header(f []resumeRow) string {
	if len(f) == 0 {
		return fmt.Sprintf("resume session · no match of %d", len(p.Rows))
	}
	return fmt.Sprintf("resume session · %d of %d", p.Cursor+1, len(f))
}

// searchLine shows the query being typed, so it is clear where the keys are
// going; empty, it is the box's own prompt.
func (p ResumePicker) searchLine() string {
	if p.Query == "" {
		return "› search…"
	}
	return "› " + p.Query
}

// keyHint is the key line the menu advertises, and it names ⇥ only where it does
// something - the room's multi-select.
func (p ResumePicker) keyHint() string {
	if p.Multi {
		return "↑↓ move · type to search · ⇥ select · ↵ resume · esc cancel"
	}
	return "↑↓ move · type to search · ↵ resume · esc cancel"
}

// rowLabel is one match as the operator reads it: the checkbox (room only), then
// the @name or short id, a coarse age, the directory (or the no-dir note), the
// branch and a preview snippet - joined by · so a whitespace-collapsing
// optionRow keeps them apart.
func (p ResumePicker) rowLabel(r resumeRow) string {
	fields := []string{r.label()}
	if r.Age != "" {
		fields = append(fields, r.Age)
	}
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
	body := strings.Join(fields, " · ")
	if p.Multi {
		box := "[ ] "
		if p.Selected[r.ID] {
			box = "[x] "
		}
		return box + body
	}
	return body
}

package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The picker opens on the directory listing and fills in as transcripts are
// read. These drive it the way Bubble Tea does - a command, its message, the
// model folding it - with a seam whose stream the test holds, so "before any
// transcript is read" is a state a test can stand in.

const (
	newestID = "cccccccc-3333-4333-8333-333333333333"
	middleID = "dddddddd-4444-4444-8444-444444444444"
	oldestID = "eeeeeeee-5555-4555-8555-555555555555"
)

// streamedApp is a room over a live and a parked session, and a seam that lists
// these sessions as still being read and completes them on the returned channel.
func streamedApp(t *testing.T, listed ...DiskSession) (App, chan DiskSession, *[]context.Context) {
	t.Helper()
	st := rpc.Status{
		Running:  true,
		Sessions: []rpc.SessionStatus{{ID: "live1", Name: "alex", State: rpc.StateIdle}},
		Parked:   []rpc.SessionStatus{{ID: "parked1", Name: "iris", State: rpc.StateParked}},
	}
	filled := make(chan DiskSession, len(listed))
	walks := new([]context.Context)
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	return a.WithSessions(fakeSessions{listed: listed, filled: filled, walks: walks}), filled, walks
}

// stranger is a listed session whose transcript has not been read.
func stranger(id string, age time.Duration) DiskSession {
	return DiskSession{ID: id, Slug: "-proj-" + id[:4], Modified: time.Now().Add(-age), Reading: true}
}

// readOf is that session once its transcript has been.
func readOf(d DiskSession, dir, title, preview string) DiskSession {
	d.Dir, d.Title, d.Preview, d.Reading = dir, title, preview, false
	return d
}

// openStreamed runs /resume to the picker opening, returning the model and the
// command that waits on the fills.
func openStreamed(t *testing.T, a App) (App, tea.Cmd) {
	t.Helper()
	next, cmd := a.resume("")
	if cmd == nil {
		t.Fatal("bare /resume did not kick the walk")
	}
	m, pump := next.Update(cmd())
	return m.(App).withSize(200, 40).applyGeometry(), pump
}

func pickerRow(t *testing.T, a App, id string) resumeRow {
	t.Helper()
	for _, r := range a.resumePicker.Rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("the picker holds no row for %s: %+v", id, a.resumePicker.Rows)
	return resumeRow{}
}

// foldFills runs the pump once and folds what it brings.
func foldFills(t *testing.T, a App, pump tea.Cmd) (App, tea.Cmd) {
	t.Helper()
	if pump == nil {
		t.Fatal("nothing is waiting on the fills")
	}
	m, next := a.Update(pump())
	return m.(App), next
}

func TestTheResumePickerOpensOnTheListingBeforeAnyTranscriptIsRead(t *testing.T) {
	a, _, _ := streamedApp(t,
		stranger(newestID, time.Minute), stranger(middleID, time.Hour),
		DiskSession{ID: "parked1", Slug: "-p", Modified: time.Now().Add(-2 * time.Hour)})
	got, pump := openStreamed(t, a)
	if !got.resumePicker.Open() {
		t.Fatal("the picker did not open on the listing")
	}
	if pump == nil {
		t.Error("nothing waits on the transcripts being read")
	}
	if r := pickerRow(t, got, newestID); !r.Reading || r.Dir != "" || r.Resumable {
		t.Errorf("a stranger's row is %+v, want it still being read, with no directory", r)
	}
	if r := pickerRow(t, got, "parked1"); r.Reading || !r.Parked {
		t.Errorf("the parked row is %+v, want it complete: the park book has all it needs", r)
	}
	frame := stripANSI(got.View())
	for _, want := range []string{"reading 2…", "reading…", "@iris"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the frame does not draw %q:\n%s", want, frame)
		}
	}
	if d := pickerRow(t, got, newestID).details(); strings.Contains(d, noDirNote) {
		t.Errorf("a session being read says it has no directory: %q", d)
	}
}

func TestARowFillsInPlaceWhenItsTranscriptIsRead(t *testing.T) {
	newest, middle := stranger(newestID, time.Minute), stranger(middleID, time.Hour)
	a, fills, _ := streamedApp(t, newest, middle)
	got, pump := openStreamed(t, a)
	before := pickerIDs(got.resumePicker.Rows)

	fills <- readOf(middle, "/work/gmail", "gmail helper", "fix the parser")
	got, pump = foldFills(t, got, pump)
	if pump == nil {
		t.Error("the picker stopped waiting with a transcript still unread")
	}
	r := pickerRow(t, got, middleID)
	if r.Reading || r.Dir != "/work/gmail" || r.Title != "gmail helper" || r.Preview != "fix the parser" || !r.Resumable {
		t.Errorf("the filled row is %+v", r)
	}
	if !pickerRow(t, got, newestID).Reading {
		t.Error("a row whose transcript was not read stopped reading")
	}
	if after := pickerIDs(got.resumePicker.Rows); !equalStrings(before, after) {
		t.Errorf("a fill reordered the rows: %v then %v", before, after)
	}
	frame := stripANSI(got.withSize(200, 40).applyGeometry().View())
	if !strings.Contains(frame, "gmail helper") || !strings.Contains(frame, "reading 1…") {
		t.Errorf("the frame does not show the filled row and the one left:\n%s", frame)
	}
}

func TestTheHeaderStopsSayingReadingWhenEveryRowIsRead(t *testing.T) {
	only := stranger(newestID, time.Minute)
	a, fills, _ := streamedApp(t, only)
	got, pump := openStreamed(t, a)
	fills <- readOf(only, "/work", "named", "")
	close(fills)
	for pump != nil {
		got, pump = foldFills(t, got, pump)
	}
	if frame := stripANSI(got.withSize(200, 40).applyGeometry().View()); strings.Contains(frame, "reading") {
		t.Errorf("the frame still says it is reading once the walk is done:\n%s", frame)
	}
}

func TestEnterOverARowStillBeingReadHoldsWithANotice(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)
	a, _, _ := streamedApp(t, stranger(newestID, time.Minute))
	got, _ := openStreamed(t, a)

	next, cmd := pressKey(got, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("↵ over a row still being read wrote something: %v", cmd)
	}
	if !next.resumePicker.Open() {
		t.Error("↵ over a row still being read closed the picker")
	}
	if notice.Count(fmt.Sprintf(resumeStillReading, shortSource(newestID))) != 1 {
		t.Error("↵ held without saying why")
	}
}

func TestEnterOverARowOnceItIsReadResumesIt(t *testing.T) {
	only := stranger(newestID, time.Minute)
	a, fills, walks := streamedApp(t, only)
	got, pump := openStreamed(t, a)
	fills <- readOf(only, "/work", "named", "")
	got, _ = foldFills(t, got, pump)

	next, cmd := pressKey(got, tea.KeyMsg{Type: tea.KeyEnter})
	if f := sentFrame(t, next, cmd); f.Kind != rpc.FrameResume || f.SessionID != newestID {
		t.Errorf("↵ wrote %+v, want a FrameResume for %s", f, newestID)
	}
	if next.resumePicker.Open() {
		t.Error("the picker is still open after ↵ resumed")
	}
	if (*walks)[0].Err() == nil {
		t.Error("resuming left the walk's reads running")
	}
}

// A parked session is complete from the first frame, so ↵ over it never waits
// on a transcript - the case a /resume to bring one agent back is.
func TestAParkedRowIsNeverHeldForReading(t *testing.T) {
	a, _, _ := streamedApp(t,
		DiskSession{ID: "parked1", Slug: "-p", Modified: time.Now().Add(-time.Minute)},
		stranger(newestID, time.Hour))
	got, _ := openStreamed(t, a)
	next, cmd := pressKey(got, tea.KeyMsg{Type: tea.KeyEnter})
	if f := sentFrame(t, next, cmd); f.Kind != rpc.FrameWake || f.SessionID != "parked1" {
		t.Errorf("↵ over the parked row wrote %+v, want a FrameWake for it while others are still being read", f)
	}
}

// In the room's multi-select, one unread row among the checked holds all of them:
// a set is sent whole or not at all.
func TestARoomPickWithOneRowStillBeingReadSendsNone(t *testing.T) {
	read, unread := stranger(newestID, time.Minute), stranger(middleID, time.Hour)
	a, fills, _ := streamedApp(t, read, unread)
	got, pump := openStreamed(t, a)
	fills <- readOf(read, "/work", "named", "")
	got, _ = foldFills(t, got, pump)
	got, _ = pressKey(got, tea.KeyMsg{Type: tea.KeyTab}) // ticks the newest, now read
	got, _ = pressKey(got, tea.KeyMsg{Type: tea.KeyDown})
	got, _ = pressKey(got, tea.KeyMsg{Type: tea.KeyTab}) // and the one still unread
	next, cmd := pressKey(got, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || !next.resumePicker.Open() {
		t.Errorf("a set holding an unread row was sent (cmd %v) or the picker closed", cmd)
	}
}

// A fill can make a row match the search, which moves the index of everything
// below it. The cursor stays on the session it was on.
func TestAFillNeverMovesTheCursorOffItsSession(t *testing.T) {
	newer, older := stranger(newestID, time.Minute), stranger(oldestID, time.Hour)
	a, fills, _ := streamedApp(t, newer, older)
	got, pump := openStreamed(t, a)
	for _, r := range "gmail" {
		got, _ = pressKey(got, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if n := len(got.resumePicker.filtered()); n != 0 {
		t.Fatalf("%d rows match a search no unread row can satisfy", n)
	}

	fills <- readOf(older, "/work/gmail", "gmail helper", "")
	got, pump = foldFills(t, got, pump)
	if f := got.resumePicker.filtered(); len(f) != 1 || f[0].ID != oldestID {
		t.Fatalf("after the older row was read the matches are %+v", f)
	}
	fills <- readOf(newer, "/work/gmail", "gmail again", "")
	got, _ = foldFills(t, got, pump)
	f := got.resumePicker.filtered()
	if len(f) != 2 || f[0].ID != newestID {
		t.Fatalf("the matches are %+v, want the newer row first", f)
	}
	if got.resumePicker.Cursor != 1 || f[got.resumePicker.Cursor].ID != oldestID {
		t.Errorf("the cursor is on %s (index %d), want it still on %s", f[got.resumePicker.Cursor].ID, got.resumePicker.Cursor, oldestID)
	}
}

// One id can sit under two projects and the picker holds the newer; the older
// copy's fill is not that row's.
func TestTheOlderCopyOfASessionDoesNotFillTheRowTheNewerHolds(t *testing.T) {
	newerCopy := DiskSession{ID: newestID, Slug: "-proj-new", Modified: time.Now().Add(-time.Minute), Reading: true}
	olderCopy := DiskSession{ID: newestID, Slug: "-proj-old", Modified: time.Now().Add(-time.Hour), Reading: true}
	a, fills, _ := streamedApp(t, newerCopy, olderCopy)
	got, pump := openStreamed(t, a)

	fills <- readOf(olderCopy, "/old/dir", "old", "")
	got, pump = foldFills(t, got, pump)
	if r := pickerRow(t, got, newestID); !r.Reading || r.Dir != "" {
		t.Fatalf("the older copy filled the newer copy's row: %+v", r)
	}
	fills <- readOf(newerCopy, "/new/dir", "new", "")
	got, _ = foldFills(t, got, pump)
	if r := pickerRow(t, got, newestID); r.Reading || r.Dir != "/new/dir" {
		t.Errorf("the row is %+v, want the newer copy's directory", r)
	}
}

// What the operator ends up with is what a walk that finished first would have
// shown: the same rows, in the same order, field for field.
func TestAFilledPickerIsTheSameRowsAWholeWalkWouldHaveGiven(t *testing.T) {
	listed := []DiskSession{stranger(newestID, time.Minute), stranger(middleID, time.Hour), stranger(oldestID, 3*time.Hour)}
	complete := make([]DiskSession, len(listed))
	for i, d := range listed {
		complete[i] = readOf(d, fmt.Sprintf("/work/%d", i), fmt.Sprintf("name %d", i), fmt.Sprintf("prompt %d", i))
	}
	whole := parkedFleetApp(t, complete...)
	wantRows, _ := whole.resumeRowsFrom(complete)

	a, fills, _ := streamedApp(t, listed...)
	got, pump := openStreamed(t, a)
	for _, d := range complete {
		fills <- d
	}
	close(fills)
	for pump != nil {
		got, pump = foldFills(t, got, pump)
	}
	if fmt.Sprintf("%+v", got.resumePicker.Rows) != fmt.Sprintf("%+v", wantRows) {
		t.Errorf("the filled picker holds\n%+v\na whole walk gives\n%+v", got.resumePicker.Rows, wantRows)
	}
}

func TestAFillForAClosedPickerIsDroppedAndAsksForNothing(t *testing.T) {
	only := stranger(newestID, time.Minute)
	a, fills, _ := streamedApp(t, only)
	got, pump := openStreamed(t, a)
	got, _, _ = got.resumePickerKey(tea.KeyMsg{Type: tea.KeyEsc})
	fills <- readOf(only, "/work", "named", "")
	got, next := foldFills(t, got, pump)
	if got.resumePicker.Open() {
		t.Error("a fill reopened a closed picker")
	}
	if next != nil {
		t.Error("a closed picker kept waiting on the walk")
	}
}

func TestAFillForAnEarlierResumeIsNotTheLaterOnesRow(t *testing.T) {
	only := stranger(newestID, time.Minute)
	a, fills, _ := streamedApp(t, only)
	got, _ := openStreamed(t, a)
	fills <- readOf(only, "/work", "named", "")
	stale := resumeFilledMsg{seq: got.resumePicker.Seq + 1, rows: []DiskSession{readOf(only, "/stale", "stale", "")}}
	m, next := got.Update(stale)
	if r := pickerRow(t, m.(App), newestID); !r.Reading || r.Dir != "" {
		t.Errorf("a fill from another /resume landed: %+v", r)
	}
	if next != nil {
		t.Error("a stale fill asked for more")
	}
}

func TestClosingThePickerEndsTheReadsBehindIt(t *testing.T) {
	a, _, walks := streamedApp(t, stranger(newestID, time.Minute))
	got, _ := openStreamed(t, a)
	if len(*walks) != 1 || (*walks)[0].Err() != nil {
		t.Fatalf("the walk's context is %v before the picker closed", *walks)
	}
	for name, key := range map[string]tea.KeyType{"esc": tea.KeyEsc, "⌃C": tea.KeyCtrlC} {
		a, _, walks := streamedApp(t, stranger(newestID, time.Minute))
		opened, _ := openStreamed(t, a)
		next, _, _ := opened.resumePickerKey(tea.KeyMsg{Type: key})
		if next.resumePicker.Open() || (*walks)[0].Err() == nil {
			t.Errorf("%s left the picker open or its reads running", name)
		}
	}
	_ = got
}

func TestAListingThatReachesATypedDraftIsDroppedAndItsReadsEnded(t *testing.T) {
	a, _, walks := streamedApp(t, stranger(newestID, time.Minute))
	next, cmd := a.resume("")
	typed := next.withDraft("half a thought")
	m, pump := typed.Update(cmd())
	if m.(App).resumePicker.Open() {
		t.Error("the picker opened over a draft the operator had begun")
	}
	if pump != nil {
		t.Error("a dropped listing asked to be filled")
	}
	if (*walks)[0].Err() == nil {
		t.Error("a dropped listing left its reads running")
	}
}

func TestALaterResumeEndsTheEarlierWalk(t *testing.T) {
	a, _, walks := streamedApp(t, stranger(newestID, time.Minute))
	first, firstCmd := a.resume("")
	second, secondCmd := first.resume("")
	firstMsg, secondMsg := firstCmd(), secondCmd()

	m, _ := second.Update(firstMsg)
	if m.(App).resumePicker.Open() {
		t.Error("the earlier walk's listing opened a picker the later /resume replaced")
	}
	if (*walks)[0].Err() == nil {
		t.Error("the earlier walk's reads are still running")
	}
	m, _ = m.(App).Update(secondMsg)
	if !m.(App).resumePicker.Open() || (*walks)[1].Err() != nil {
		t.Error("the later walk did not open its picker, or its reads were ended")
	}
}

// Typing searches what has arrived and what the listing always knew: the id.
func TestTheSearchBoxWorksWhileTheTranscriptsAreStillBeingRead(t *testing.T) {
	a, _, _ := streamedApp(t, stranger(newestID, time.Minute), stranger(middleID, time.Hour))
	got, _ := openStreamed(t, a)
	for _, r := range "dddd" {
		got, _ = pressKey(got, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if f := got.resumePicker.filtered(); len(f) != 1 || f[0].ID != middleID {
		t.Errorf("searching by id while reading matched %+v", f)
	}
}

func pickerIDs(rows []resumeRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

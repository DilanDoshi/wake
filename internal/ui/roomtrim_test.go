package ui

// The room trims itself: when it holds roomRetentionEvents messages it drops the
// oldest roomTrimEvents in one step, so it swings between the two and never
// holds the cap (owner's 2026-11-23 ruling, decisions.md).

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
)

// The ruling's two numbers. Everything else derives from them; a change here is
// the owner's to make, not a test's to follow.
func TestTheRoomsCapAndChunkAreTheOwnersRuling(t *testing.T) {
	if roomRetentionEvents != 3_000 || roomTrimEvents != 1_000 {
		t.Errorf("the room trims %d at %d, the owner's ruling is 1,000 at 3,000", roomTrimEvents, roomRetentionEvents)
	}
}

// What a count of n messages sheds: nothing under the cap, then whole chunks
// until it is under again.
func TestTheTrimIsWholeChunksUntilUnderTheCap(t *testing.T) {
	for _, c := range []struct{ held, want int }{
		{0, 0},
		{roomRetentionEvents - 1, 0},
		{roomRetentionEvents, roomTrimEvents},
		{roomRetentionEvents + 1, roomTrimEvents},
		{roomRetentionEvents + roomTrimEvents - 1, roomTrimEvents},
		{roomRetentionEvents + roomTrimEvents, 2 * roomTrimEvents},
		{10_000, 8_000},
	} {
		if got := roomTrimCount(c.held); got != c.want {
			t.Errorf("a room holding %d sheds %d, want %d", c.held, got, c.want)
		}
	}
	for n := 0; n <= 4*roomRetentionEvents; n++ {
		left := n - roomTrimCount(n)
		if left >= roomRetentionEvents {
			t.Fatalf("%d messages left %d, not under the cap", n, left)
		}
		if n >= roomRetentionEvents && left < roomRetentionEvents-roomTrimEvents {
			t.Fatalf("%d messages left %d, more than one chunk too few", n, left)
		}
		if roomTrimCount(n)%roomTrimEvents != 0 {
			t.Fatalf("%d messages shed %d, not whole chunks", n, roomTrimCount(n))
		}
	}
}

// The newest events stay and the oldest chunk goes, all at once, behind one
// marker: below the cap nothing is touched, and the message that reaches it is
// the one that trims.
func TestTheRoomDropsItsOldestThousandAtThree(t *testing.T) {
	r := retainedRoom(roomRetentionEvents - 1)
	if got := len(roomEvents(r)); got != roomRetentionEvents-1 || r.reclaimed || r.trimmed != 0 {
		t.Fatalf("a room one short of the cap holds %d (reclaimed %v, %d trimmed), want it all untouched", got, r.reclaimed, r.trimmed)
	}
	r = r.Append(retainedRoomEvent(roomRetentionEvents-1), roomScaleAgent)
	got := roomEvents(r)
	if want := roomRetentionEvents - roomTrimEvents; len(got) != want {
		t.Fatalf("the room holds %d after reaching the cap, want %d", len(got), want)
	}
	if oldest, want := got[0].ev.MessageID, fmt.Sprintf("event-%05d", roomTrimEvents); oldest != want {
		t.Errorf("the oldest retained event is %q, want %q: the oldest chunk goes, not the newest", oldest, want)
	}
	if newest, want := got[len(got)-1].ev.MessageID, fmt.Sprintf("event-%05d", roomRetentionEvents-1); newest != want {
		t.Errorf("the newest retained event is %q, want %q", newest, want)
	}
	if r.trimmed != roomTrimEvents {
		t.Errorf("the room owes %d trimmed messages, want %d", r.trimmed, roomTrimEvents)
	}
	top := stripANSI(r.ScrollUp(1<<30).View(80, 12))
	if n := strings.Count(top, wantReclaimedLine); n != 1 {
		t.Errorf("the oldest view holds %d reclaim markers, want one:\n%s", n, top)
	}
	assertSaidMatchesTr(t, r, "after the trim")
}

// It swings between the two figures and never settles on the cap, however long
// it runs, and each trim is exactly one chunk.
func TestTheRoomSwingsBetweenTwoThousandAndTheCapForever(t *testing.T) {
	r := NewRoom().SetSize(80, 12)
	floor := roomRetentionEvents - roomTrimEvents
	for i := 1; i <= 3*roomRetentionEvents+17; i++ {
		before := r.said.count()
		r = r.Append(retainedRoomEvent(i), roomScaleAgent)
		held := r.said.count()
		want := i
		if i >= roomRetentionEvents {
			want = floor + (i-floor)%roomTrimEvents
		}
		if held != want {
			t.Fatalf("after %d messages the room holds %d, want %d", i, held, want)
		}
		if held >= roomRetentionEvents {
			t.Fatalf("after %d messages the room holds the cap itself: %d", i, held)
		}
		if shed := before + 1 - held; shed != 0 && shed != roomTrimEvents {
			t.Fatalf("message %d shed %d, want none or one chunk of %d", i, shed, roomTrimEvents)
		}
	}
}

// A line the focus hides is a message the room holds: it counts toward the cap
// and goes in the chunk, so a narrowed room never holds more than a whole one.
func TestHiddenLinesCountTowardTheTrim(t *testing.T) {
	const john, iris = "john-id", "iris-id"
	r := NewRoom().SetSize(80, 12).WithFocus(john, "john")
	for i := range roomRetentionEvents {
		r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: iris, Text: fmt.Sprintf("iris %d", i)}, Agent{ID: iris, Name: "iris"})
	}
	if got, want := r.said.count(), roomRetentionEvents-roomTrimEvents; got != want {
		t.Fatalf("a room showing none of %d lines holds %d, want %d", roomRetentionEvents, got, want)
	}
	assertSaidMatchesTr(t, r, "hidden lines trimmed")
}

// A restore, or a history that lands late among live lines, sheds whole chunks
// by the same rule, so it never leaves more than the live room would hold.
func TestALateHistoryMergeSharesTheTrim(t *testing.T) {
	const restored = 400
	base := time.Unix(1_700_000_000, 0)
	var earlier []roomLine
	for i := range restored {
		earlier = append(earlier, restoredBroadcast(fmt.Sprintf("broadcast-%03d", i), base.Add(time.Duration(i)*10*time.Second))...)
	}
	live := roomRetentionEvents - 100
	r := retainedRoom(live).Before(earlier)
	if want := live + restored - roomTrimEvents; len(roomEvents(r)) != want {
		t.Fatalf("%d live and %d restored hold %d, want %d (one chunk shed)", live, restored, len(roomEvents(r)), want)
	}
	if r.trimmed != 0 {
		t.Errorf("a merge owes %d trimmed messages: it names none, the marker draws", r.trimmed)
	}
	if got := roomEvents(r)[0].ev.MessageID; got != fmt.Sprintf("event-%05d", roomTrimEvents-restored) {
		t.Errorf("the oldest retained event is %q: the restored history, being oldest, goes first", got)
	}

	under := retainedRoom(roomRetentionEvents - restored - 1).Before(earlier)
	if got, want := len(roomEvents(under)), roomRetentionEvents-restored-1+restored; got != want {
		t.Errorf("a merge that stays under the cap holds %d, want all %d: nothing is shed", got, want)
	}
}

// The risk a chunk of 1,000 adds to a one-event reclaim: a run of an agent's
// resumes whose head goes in the chunk. The next resume heads a new line - it
// cannot fold into one that is gone.
func TestAThousandDropAcrossAnOpenRunHeadStartsANewRun(t *testing.T) {
	r := foldRoom().Append(foldResume(foldSydney, false), foldSydney)
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	if got := rowsWith(r, "resumed after"); len(got) != 1 || !strings.Contains(got[0], "×2") {
		t.Fatalf("the fixture's run is %q, want one line ×2", got)
	}
	for i := 0; r.said.first() == 0; i++ {
		r = r.Append(foldProse(foldJohn, fmt.Sprintf("john %d", i)), foldJohn)
	}
	if len(r.counts) != 0 {
		t.Errorf("the head left with its chunk but its count stayed: %v", r.counts)
	}
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	got := rowsWith(r, "resumed after")
	if len(got) != 1 || strings.Contains(got[0], "×") {
		t.Errorf("a resume after its head went should draw one plain line, drew %q", got)
	}
	assertSaidMatchesTr(t, r, "after the run's head left")
}

// ...and a run whose head stays keeps folding into it after the chunk goes, the
// head row redrawn in place - the same rows a full re-render draws.
func TestAThousandDropLeavesARunWhoseHeadStaysFolding(t *testing.T) {
	r := foldRoom()
	for i := range roomRetentionEvents - 600 {
		r = r.Append(foldProse(foldJohn, fmt.Sprintf("john %d", i)), foldJohn)
	}
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	for i := 0; r.said.first() == 0; i++ {
		r = r.Append(foldProse(foldJohn, fmt.Sprintf("later %d", i)), foldJohn)
	}
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	got := rowsWith(r, "resumed after")
	if len(got) != 1 || !strings.HasSuffix(got[0], "×3") {
		t.Fatalf("the surviving run should draw one line ×3, drew %q", got)
	}
	rows := foldedRows(r)
	if resized := foldedRows(r.SetSize(r.width+1, r.height).SetSize(r.width, r.height)); !slices.Equal(rows, resized) {
		t.Errorf("the redrawn head differs from a full render")
	}
	assertSaidMatchesTr(t, r, "after the chunk, with a surviving run")
}

// A reader inside what stays keeps exactly the rows they were reading; one inside
// the chunk is put at the oldest line left. Neither follows the newest line.
func TestATrimKeepsAReadersPlaceWhereItCan(t *testing.T) {
	r := retainedRoom(roomRetentionEvents - 1)
	deep := r.ScrollUp(40) // a few messages up: well inside what stays
	atRow, text := deep.tr.scroll, stripANSI(deep.tr.rows(deep.tr.scroll, deep.tr.scroll+1)[0])
	deep = deep.Append(retainedRoomEvent(roomRetentionEvents-1), roomScaleAgent)
	if deep.tr.scroll != atRow {
		t.Errorf("a reader inside what stays moved from line %d to %d", atRow, deep.tr.scroll)
	}
	if got := stripANSI(deep.tr.rows(deep.tr.scroll, deep.tr.scroll+1)[0]); got != text {
		t.Errorf("the reader's top row changed from %q to %q", text, got)
	}

	top := r.ScrollUp(1 << 30)
	top = top.Append(retainedRoomEvent(roomRetentionEvents-1), roomScaleAgent)
	if top.tr.scroll != top.tr.first() {
		t.Errorf("a reader inside the dropped chunk sits at line %d, want the oldest retained %d", top.tr.scroll, top.tr.first())
	}
}

// The notice is the reminder's plumbing: said when the room holds the keys, once
// per trim, and an ordinary one that times out.
func TestATrimIsAnnouncedOnceInATimedNotice(t *testing.T) {
	ticks := recordNoticeTicks(t)
	a := roomWithHistoryAsked(t)
	notice.Reset()
	fill := func(a App, n int) App {
		for range n {
			a = a.withRoom(a.room.appendUser(core.Event{Kind: core.KindUserText, Text: "@sydney hi"}, "s1", ""))
		}
		return a
	}
	want := roomTrimNotice(roomTrimEvents)
	if !strings.Contains(want, "1,000") || !strings.Contains(want, "agents untouched") {
		t.Fatalf("the notice %q does not name the count and say agents are untouched", want)
	}

	a = fill(a, roomRetentionEvents-1)
	if got := noticeNow(); strings.Contains(got, "cleared the oldest") {
		t.Fatalf("announced a trim before one: %q", got)
	}
	a = fill(a, 1)
	if got := noticeNow(); got != want {
		t.Fatalf("at the cap the notice reads %q, want %q", got, want)
	}

	var m tea.Model = a
	m, _ = m.Update(nudge{})
	if len(*ticks) == 0 {
		t.Fatal("the notice armed no linger, so it would stay up")
	}
	m, _ = m.Update((*ticks)[len(*ticks)-1].msg)
	if row := m.(App).noticeLine(); row != "" {
		t.Errorf("the notice outlived its linger: %q", row)
	}
	a = m.(App)

	notice.Reset()
	a = fill(a, roomTrimEvents-1)
	if noticeNow() != "" {
		t.Errorf("announced again before the next trim: %q", noticeNow())
	}
	fill(a, 1)
	if got := noticeNow(); got != want {
		t.Errorf("the next trim said %q, want %q", got, want)
	}
}

// A trim while a conversation holds the keys is owed, and counted: back in the
// room it is said once, for every thousand that went meanwhile.
func TestTrimsOwedWhileAConversationHoldsTheKeysAreSaidTogether(t *testing.T) {
	a := roomWithHistoryAsked(t)
	notice.Reset()
	a.focus = "s1"
	for range roomRetentionEvents + roomTrimEvents {
		a = a.withRoom(a.room.appendUser(core.Event{Kind: core.KindUserText, Text: "@sydney hi"}, "s1", ""))
	}
	if got := a.room.trimmed; got != 2*roomTrimEvents {
		t.Fatalf("the room owes %d trimmed messages, want %d", got, 2*roomTrimEvents)
	}
	if strings.Contains(noticeNow(), "cleared the oldest") {
		t.Fatalf("announced while a conversation held the keys: %q", noticeNow())
	}
	a = a.showRoom()
	if got, want := noticeNow(), roomTrimNotice(2*roomTrimEvents); got != want {
		t.Errorf("back in the room the notice reads %q, want %q", got, want)
	}
	if a.room.trimmed != 0 {
		t.Errorf("the room still owes %d after saying it", a.room.trimmed)
	}
}

// /clear is the window's: what was owed goes with the lines it was owed for.
func TestClearingForgetsTheTrimsOwed(t *testing.T) {
	r := retainedRoom(roomRetentionEvents)
	if r.trimmed == 0 {
		t.Fatal("the fixture owes nothing")
	}
	if got := r.cleared(time.Unix(1, 0)).trimmed; got != 0 {
		t.Errorf("a cleared room still owes %d trimmed messages", got)
	}
}

// Production mutation caught: laying the retained room out again on a trim (a
// rebuild through renderRoom) instead of dropping the chunk's rows.
func TestATrimRendersNothingAgain(t *testing.T) {
	r := retainedRoom(fullRoom)
	renders := countRoomRenders(t, func() { r = r.Append(retainedRoomEvent(fullRoom), roomScaleAgent) })
	if !r.reclaimed {
		t.Fatal("the append did not trim, so this proves nothing")
	}
	if renders != 0 {
		t.Errorf("a trim laid the room out %d times, want none: it drops rows, it does not re-wrap", renders)
	}
}

// Production mutation caught: a trim that copies or rebuilds the retained window
// allocates by the window; this one allocates by the chunk it drops.
func TestATrimAllocatesPerDroppedEventNotPerWindow(t *testing.T) {
	original := renderRoomBlock
	renderRoomBlock = func(ev core.Event, a, to Agent, width int, expanded bool) block {
		return block{text: "assistant markdown"}
	}
	t.Cleanup(func() { renderRoomBlock = original })
	r := cappedAssistantRoom("**retained markdown**")
	bytes := roomAllocatedBytes(func() {
		sinkRoom = r.Append(core.Event{Kind: core.KindAssistantText, Text: "**retained markdown**"}, roomScaleAgent)
	})
	perDropped := bytes / roomTrimEvents
	t.Logf("a trim allocated %d bytes (%d per dropped event)", bytes, perDropped)
	if perDropped > roomEventBudget {
		t.Errorf("a trim allocated %d bytes per dropped event, want at most %d", perDropped, roomEventBudget)
	}
	if len(roomEvents(sinkRoom)) != roomRetentionEvents-roomTrimEvents {
		t.Fatalf("the append did not trim: the room holds %d", len(roomEvents(sinkRoom)))
	}
}

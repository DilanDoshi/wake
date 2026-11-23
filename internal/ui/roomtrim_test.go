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

// restoredTurn is a broadcast and the n-1 lines one session says in answer to it:
// n restored lines, all of them public, from at on.
func restoredTurn(tag string, n int, at time.Time) []roomLine {
	lines := restoredBroadcast(tag+" asked", at)
	for i := 1; i < n; i++ {
		lines = append(lines, roomLine{
			ev: core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: fmt.Sprintf("%s said %04d", tag, i), At: at.Add(time.Duration(i+1) * time.Second)},
			by: roomScaleAgent,
		})
	}
	return lines
}

// A restore, or a history that lands late among live lines, sheds by the same
// rule as the live room - whole chunks until under the cap - but only ever of the
// history: the live lines were all seen, and a restore must not cost one.
func TestALateHistoryMergeShedsOnlyHistory(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	broadcasts := func(n int) []roomLine {
		var lines []roomLine
		for i := range n {
			lines = append(lines, restoredBroadcast(fmt.Sprintf("broadcast-%03d", i), base.Add(time.Duration(i)*10*time.Second))...)
		}
		return lines
	}
	for _, c := range []struct {
		name         string
		live         int
		earlier      []roomLine
		held         int  // what the room holds after
		firstIsLive  bool // whether the oldest retained line is live event 0
		wantReclaims bool
	}{
		{"history goes first, live stays", roomRetentionEvents - 100, broadcasts(400), roomRetentionEvents - 100, true, true},
		{"a handful at the edge costs no live message", fullRoom, broadcasts(5), fullRoom, true, true},
		{"under the cap nothing goes", 2_000, broadcasts(400), 2_400, false, false},
		{"a chunk of a large history goes", 2_000, restoredTurn("big", 1_101, base), 2_000 + 1_101 - roomTrimEvents, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := retainedRoom(c.live).Before(c.earlier)
			got := roomEvents(r)
			if len(got) != c.held {
				t.Fatalf("%d live and a restore hold %d, want %d", c.live, len(got), c.held)
			}
			if live0 := got[0].ev.MessageID == "event-00000"; live0 != c.firstIsLive {
				t.Errorf("the oldest retained line is %q: live event 0 retained = %v, want %v", got[0].ev.MessageID, live0, c.firstIsLive)
			}
			if kept := slices.IndexFunc(got, func(l roomLine) bool { return l.ev.MessageID == fmt.Sprintf("event-%05d", c.live-1) }); kept != len(got)-1 {
				t.Errorf("the newest live event is not the last line: a live message went")
			}
			if live := countLive(got); live != c.live {
				t.Errorf("%d live lines retained, want all %d", live, c.live)
			}
			if r.reclaimed != c.wantReclaims || r.trimmed != 0 {
				t.Errorf("reclaimed %v with %d owed, want %v and none: the line draws, no notice says it", r.reclaimed, r.trimmed, c.wantReclaims)
			}
			if r.said.count() >= roomRetentionEvents {
				t.Errorf("the room holds %d after a restore, at the cap", r.said.count())
			}
		})
	}
}

// countLive is how many of lines are the live events retainedRoom appended.
func countLive(lines []roomLine) int {
	return len(slices.DeleteFunc(slices.Clone(lines), func(l roomLine) bool { return !strings.HasPrefix(l.ev.MessageID, "event-") }))
}

// What a live trim let go of stays gone: Before re-derives history from every
// restored transcript, and the trim leaves those alone, so without a watermark
// the next restore brings the trimmed lines back above a live tail that has lost
// the messages between.
func TestTrimmedHistoryDoesNotComeBackWithALaterRestore(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	var seed []roomLine
	for i := range 100 {
		seed = append(seed, restoredBroadcast(fmt.Sprintf("seed-%03d", i), base.Add(time.Duration(i)*10*time.Second))...)
	}
	r := NewRoom().SetSize(80, 12).Before(seed)
	live := roomRetentionEvents + 100
	for i := range live {
		r = r.Append(retainedRoomEvent(i), roomScaleAgent)
	}
	if !r.reclaimed || countLive(roomEvents(r)) != r.said.count() {
		t.Fatalf("the fixture's trim did not consume the restored history: %d held", r.said.count())
	}
	held := r.said.count()

	r = r.Before(restoredBroadcast("late", base.Add(time.Hour)))
	got := roomEvents(r)
	if want := held + 1; len(got) != want {
		t.Fatalf("a late restore of one line leaves %d, want %d: only that line comes", len(got), want)
	}
	if i := slices.IndexFunc(got, func(l roomLine) bool { return strings.HasPrefix(l.ev.Text, "seed-") }); i >= 0 {
		t.Errorf("a line the trim dropped came back: %q", got[i].ev.Text)
	}
	if got[0].ev.Text != "late" {
		t.Errorf("the oldest line is %q, want the late restore", got[0].ev.Text)
	}
	// A restore older than what the trim let go of stays out too.
	if before := r.Before(restoredBroadcast("older", base)); len(roomEvents(before)) != len(got) {
		t.Errorf("a restore older than what the trim dropped came back: %d lines, want %d", len(roomEvents(before)), len(got))
	}
}

// Production mutation caught: leaving r.hist standing after a trim made the next
// restore take that many live lines for history and drop them.
func TestALiveTrimThatConsumesRestoredHistoryLeavesTheNextRestoreNoneOfItsLiveLines(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	r := NewRoom().SetSize(80, 12).Before(restoredBroadcast("early", base))
	for i := range roomRetentionEvents + 100 {
		r = r.Append(retainedRoomEvent(i), roomScaleAgent)
	}
	live := countLive(roomEvents(r))
	r = r.Before(restoredBroadcast("late", base.Add(time.Hour)))
	if got := countLive(roomEvents(r)); got != live {
		t.Errorf("a restore after a trim left %d live lines of the %d the room held", got, live)
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

// Every way the room gets the keys back says what it owes: ⌃W closes a
// conversation without going through refocus.
func TestTrimsOwedAreSaidWhenAConversationIsClosed(t *testing.T) {
	a := roomWithHistoryAsked(t)
	notice.Reset()
	a.focus = "s1"
	for range roomRetentionEvents {
		a = a.withRoom(a.room.appendUser(core.Event{Kind: core.KindUserText, Text: "@sydney hi"}, "s1", ""))
	}
	if strings.Contains(noticeNow(), "cleared the oldest") {
		t.Fatalf("announced while a conversation held the keys: %q", noticeNow())
	}
	a = a.closeDM()
	if got, want := noticeNow(), roomTrimNotice(roomTrimEvents); got != want {
		t.Errorf("closing the conversation said %q, want %q", got, want)
	}
}

// Production mutation caught: dropping the outer prune leaves a counted resume
// line's count behind for a head a merge dropped with its chunk of history.
func TestAMergeThatDropsARestoredRunsHeadForgetsItsCount(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	earlier := []roomLine{
		{ev: resumeEvent("s1", base), by: foldSydney},
		{ev: resumeEvent("s1", base.Add(time.Minute)), by: foldSydney},
		{ev: resumeEvent("s1", base.Add(2*time.Minute)), by: foldSydney},
	}
	earlier = append(earlier, restoredTurn("big", 1_101, base.Add(time.Hour))...)
	r := retainedRoom(2_000).Before(earlier)
	if len(r.counts) != 0 {
		t.Errorf("the run's head went with the oldest chunk but %d counts stayed: %v", len(r.counts), r.counts)
	}
	if got := rowsWith(r, "resumed after"); len(got) != 0 {
		t.Errorf("the dropped run is still drawn: %q", got)
	}
}

// Production mutation caught: dropping the prune leaves an opened reply's entry
// behind when the merge that evicts it re-derives the history's ids.
func TestAMergeThatDropsAnOpenedRestoredReplyForgetsItsOpen(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	turn := restoredTurn("big", 1_101, base)
	turn[2].ev.Text = longRoomReply("OPENED")
	r := retainedRoom(1_800).Before(turn) // 2,901: nothing goes yet
	line, id := expandableLine(t, r)
	r, _ = r.toggleLine(line)
	if !r.expanded[id] {
		t.Fatal("the click did not open the restored reply")
	}
	r = r.Before(restoredTurn("more", 200, base.Add(time.Hour))) // 3,100: the oldest chunk goes
	if _, stale := r.expanded[id]; stale || len(r.expanded) != 0 {
		t.Errorf("the open outlived the reply it opened: %v", r.expanded)
	}
}

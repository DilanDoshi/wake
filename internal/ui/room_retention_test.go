package ui

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	wantRoomRetention = 20_000
	wantReclaimedLine = "… older room history reclaimed"
)

func retainedRoom(n int) Room {
	r := NewRoom().SetSize(80, 12)
	for i := range n {
		r = r.Append(retainedRoomEvent(i), roomScaleAgent)
	}
	return r
}

func retainedRoomEvent(i int) core.Event {
	return core.Event{Kind: core.KindTurnEnd, MessageID: fmt.Sprintf("event-%05d", i)}
}

func roomEvents(r Room) []roomLine { return r.said.slice(0, r.said.len()) }

// Production mutation caught: removing the combined-cap reclamation from
// Room.Append leaves cap+1 events and no boundary marker.
func TestRoomRetentionKeepsTheNewestEventsAndOneMarker(t *testing.T) {
	r := retainedRoom(wantRoomRetention + 1)
	got := roomEvents(r)
	if len(got) != wantRoomRetention {
		t.Errorf("room retains %d events after cap+1, want %d", len(got), wantRoomRetention)
	}
	if len(got) > 0 && got[0].ev.MessageID != "event-00001" {
		t.Errorf("oldest retained event = %q, want event-00001", got[0].ev.MessageID)
	}
	if len(got) > 0 && got[len(got)-1].ev.MessageID != "event-20000" {
		t.Errorf("newest retained event = %q, want event-20000", got[len(got)-1].ev.MessageID)
	}

	top := ansi.Strip(r.ScrollUp(1<<30).View(80, 12))
	if n := strings.Count(top, wantReclaimedLine); n != 1 {
		t.Errorf("oldest room view contains %d reclamation markers, want exactly one:\n%s", n, top)
	}
}

// Production mutation caught: treating the marker as an appended event, or
// trimming only once, either grows the room past the cap or accumulates marks.
func TestRepeatedRoomAppendsStayCappedWithoutAccumulatingMarkers(t *testing.T) {
	r := retainedRoom(wantRoomRetention + 2*chunkSize + 7)
	if got := len(roomEvents(r)); got != wantRoomRetention {
		t.Errorf("room retains %d events after repeated reclamation, want %d", got, wantRoomRetention)
	}
	all := ansi.Strip(r.SetSize(80, r.tr.lines.len()+20).ScrollUp(1<<30).View(80, r.tr.lines.len()+20))
	if n := strings.Count(all, wantReclaimedLine); n != 1 {
		t.Errorf("full retained room contains %d reclamation markers, want exactly one", n)
	}
}

// Production mutation caught: capping history and live events separately, or
// sorting the live tail by restored timestamps, changes this exact order.
func TestLateHistoryAndLiveRoomEventsShareOneCapWithoutReorderingLive(t *testing.T) {
	r := retainedRoom(wantRoomRetention - 1)
	base := time.Unix(1_700_000_000, 0)
	earlier := []roomLine{
		{ev: core.Event{Kind: core.KindUserText, SessionID: "s1", MessageID: "history-0", Text: "first broadcast", At: base}, by: roomScaleAgent},
		{ev: core.Event{Kind: core.KindUserText, SessionID: "s2", MessageID: "history-0-copy", Text: "first broadcast", At: base.Add(time.Second)}, by: roomScaleAgent},
		{ev: core.Event{Kind: core.KindUserText, SessionID: "s1", MessageID: "history-1", Text: "second broadcast", At: base.Add(10 * time.Second)}, by: roomScaleAgent},
		{ev: core.Event{Kind: core.KindUserText, SessionID: "s2", MessageID: "history-1-copy", Text: "second broadcast", At: base.Add(11 * time.Second)}, by: roomScaleAgent},
	}
	r = r.Before(earlier)

	got := roomEvents(r)
	if len(got) != wantRoomRetention {
		t.Fatalf("combined room retains %d events, want %d", len(got), wantRoomRetention)
	}
	if got[0].ev.MessageID != "history-1" {
		t.Errorf("oldest retained event = %q, want history-1 after history-0 is reclaimed", got[0].ev.MessageID)
	}
	for i, line := range got[1:] {
		if want := fmt.Sprintf("event-%05d", i); line.ev.MessageID != want {
			t.Fatalf("live event %d = %q, want %q; restored timestamps must not reorder the live tail", i, line.ev.MessageID, want)
		}
	}
}

// Production mutation caught: retaining the old 400-event history-only cap
// violates the one combined history/live retention policy.
func TestRestoredRoomHistoryHasNoSeparateDisplayCap(t *testing.T) {
	const broadcasts = 401
	base := time.Unix(1_700_000_000, 0)
	earlier := make([]roomLine, 0, 2*broadcasts)
	for i := range broadcasts {
		text := fmt.Sprintf("broadcast-%03d", i)
		at := base.Add(time.Duration(i) * 10 * time.Second)
		earlier = append(earlier,
			roomLine{ev: core.Event{Kind: core.KindUserText, SessionID: "s1", Text: text, At: at}},
			roomLine{ev: core.Event{Kind: core.KindUserText, SessionID: "s2", Text: text, At: at.Add(time.Second)}},
		)
	}

	r := NewRoom().Before(earlier)
	if got := len(roomEvents(r)); got != broadcasts {
		t.Errorf("restored room retains %d events below the combined cap, want %d; history must not have its own display cap", got, broadcasts)
	}
}

func restoredBroadcast(text string, at time.Time) []roomLine {
	return []roomLine{
		{ev: core.Event{Kind: core.KindUserText, SessionID: "s1", Text: text, At: at}},
		{ev: core.Event{Kind: core.KindUserText, SessionID: "s2", Text: text, At: at.Add(time.Second)}},
	}
}

func roomTextLine(r Room, text string) int {
	for line := r.tr.lines.first(); line < r.tr.lines.len(); line++ {
		if strings.Contains(ansi.Strip(r.tr.lines.at(line)), text) {
			return line
		}
	}
	return -1
}

// Production mutation caught: anchoring only the live tail during Before
// leaves a restored-history viewport on the line number an insertion reuses.
func TestLateHistoryInsertionKeepsTheRestoredTopLine(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	r := NewRoom().SetSize(80, 8)
	earlier := append(restoredBroadcast("history A", base), restoredBroadcast("history C", base.Add(20*time.Second))...)
	earlier = append(earlier, restoredBroadcast("history D", base.Add(30*time.Second))...)
	r = r.Before(earlier)
	for i := range 10 {
		r = r.Append(retainedRoomEvent(i), roomScaleAgent)
	}
	cLine := roomTextLine(r, "history C")
	if cLine < 0 {
		t.Fatal("fixture did not render history C")
	}
	r.tr.scroll = cLine
	want := ansi.Strip(r.tr.lines.at(cLine))

	r = r.Before(restoredBroadcast("history B", base.Add(10*time.Second)))
	if got := ansi.Strip(r.tr.lines.at(r.tr.scroll)); got != want {
		t.Errorf("late insertion moved the viewport from %q to %q", want, got)
	}
}

// Production mutation caught: leaving absolute selection indices unchanged
// after a history insertion silently changes selected C into inserted B.
func TestLateHistoryInsertionKeepsTheRestoredSelectionContent(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	a := NewRoomApp(nil, Stream{}, nil)
	a.room = NewRoom().SetSize(80, 8).Before(append(restoredBroadcast("history A", base), restoredBroadcast("history C", base.Add(20*time.Second))...))
	cLine := roomTextLine(a.room, "history C")
	line := ansi.Strip(a.room.tr.lines.at(cLine))
	a.sel = selection{pane: "", anchor: point{line: cLine, col: 0}, head: point{line: cLine, col: ansi.StringWidth(line) - 1}}
	want := selectedText(a.room.tr.lines.slice(cLine, cLine+1), cLine, a.sel.marked())

	a = a.withRoom(a.room.Before(restoredBroadcast("history B", base.Add(10*time.Second))))
	m := a.sel.marked()
	got := selectedText(a.room.tr.lines.slice(m.from.line, m.to.line+1), m.from.line, m)
	if got != want {
		t.Errorf("late insertion changed selected history from %q to %q", want, got)
	}
}

// Production mutation caught: an evicted restored block can be replaced at
// the same absolute line number, so bounds checks alone retain a stale anchor.
func TestLateHistoryEvictionClampsViewportAndClearsSelection(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	r := NewRoom().SetSize(80, 8).Before(restoredBroadcast("history A", base))
	for i := range wantRoomRetention - 1 {
		r = r.Append(retainedRoomEvent(i), roomScaleAgent)
	}
	aLine := roomTextLine(r, "history A")
	r.tr.scroll = aLine
	a := NewRoomApp(nil, Stream{}, nil)
	a.room = r
	a.sel = selection{pane: "", anchor: point{line: aLine, col: 0}, head: point{line: aLine, col: 3}}

	a = a.withRoom(a.room.Before(restoredBroadcast("history B", base.Add(10*time.Second))))
	if a.room.tr.scroll != a.room.tr.first() {
		t.Errorf("evicted history viewport stayed at %d, want boundary %d", a.room.tr.scroll, a.room.tr.first())
	}
	if a.sel != (selection{}) {
		t.Errorf("selection on evicted history survived at %+v", a.sel)
	}
	if top := ansi.Strip(a.room.View(80, 8)); !strings.Contains(top, wantReclaimedLine) {
		t.Errorf("evicted history viewport did not clamp to the reclamation marker:\n%s", top)
	}
}

// Production mutation caught: resetting scroll after a front trim yanks both
// following readers and readers whose retained line still exists.
func TestRoomRetentionPreservesFollowingAndRetainedScrollPositions(t *testing.T) {
	following := retainedRoom(wantRoomRetention)
	following = following.Append(retainedRoomEvent(wantRoomRetention), roomScaleAgent)
	if following.tr.scroll != following.tr.bottom() {
		t.Errorf("following reader is at %d of %d after reclamation", following.tr.scroll, following.tr.bottom())
	}

	scrolled := retainedRoom(wantRoomRetention).ScrollUp(100)
	before := scrolled.tr.scroll
	scrolled = scrolled.Append(retainedRoomEvent(wantRoomRetention), roomScaleAgent)
	if scrolled.tr.scroll != before {
		t.Errorf("retained scroll position moved %d -> %d during reclamation", before, scrolled.tr.scroll)
	}
}

// Production mutation caught: leaving a viewport before the retained prefix
// produces an empty/stale window instead of clamping it to the boundary.
func TestRoomRetentionClampsAnEvictedViewportToTheOldestRetainedLine(t *testing.T) {
	r := retainedRoom(wantRoomRetention).ScrollUp(1 << 30)
	before := r.tr.scroll
	r = r.Append(retainedRoomEvent(wantRoomRetention), roomScaleAgent)
	if r.tr.scroll <= before {
		t.Errorf("evicted viewport stayed at %d after reclamation; want it clamped past that line", r.tr.scroll)
	}
	if top := ansi.Strip(r.View(80, 12)); !strings.Contains(top, wantReclaimedLine) {
		t.Errorf("clamped viewport does not begin at the reclamation boundary:\n%s", top)
	}
}

// Production mutation caught: renumbering retained transcript lines on trim
// invalidates a selection that still points at retained content.
func TestRoomRetentionKeepsARetainedSelectionAnchored(t *testing.T) {
	a := NewRoomApp(nil, Stream{}, nil)
	a.room = retainedRoom(wantRoomRetention)
	line := a.room.tr.lines.len() - 10
	a.sel = selection{pane: "", anchor: point{line: line, col: 0}, head: point{line: line, col: 2}}
	want := a.sel

	a = a.appendEvent(retainedRoomEvent(wantRoomRetention))
	if a.sel != want {
		t.Errorf("retained selection moved or cleared: got %+v, want %+v", a.sel, want)
	}
}

// Production mutation caught: rebuilding a late Before merge from line zero
// renumbers a retained live selection even when the live order is unchanged.
func TestLateHistoryKeepsARetainedLiveSelectionAnchored(t *testing.T) {
	a := NewRoomApp(nil, Stream{}, nil)
	a.room = retainedRoom(wantRoomRetention - 1)
	line := a.room.tr.lines.len() - 10
	a.sel = selection{pane: "", anchor: point{line: line, col: 0}, head: point{line: line, col: 2}}
	want := a.sel
	base := time.Unix(1_700_000_000, 0)
	earlier := []roomLine{
		{ev: core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "first broadcast", At: base}, by: roomScaleAgent},
		{ev: core.Event{Kind: core.KindUserText, SessionID: "s2", Text: "first broadcast", At: base.Add(time.Second)}, by: roomScaleAgent},
		{ev: core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "second broadcast", At: base.Add(10 * time.Second)}, by: roomScaleAgent},
		{ev: core.Event{Kind: core.KindUserText, SessionID: "s2", Text: "second broadcast", At: base.Add(11 * time.Second)}, by: roomScaleAgent},
	}

	a = a.withRoom(a.room.Before(earlier))
	if a.sel != want {
		t.Errorf("late history moved or cleared retained live selection: got %+v, want %+v", a.sel, want)
	}
}

// Production mutation caught: tying the displayed broadcast block to the
// chronologically earliest physical transcript copy loses every anchor when a
// still-earlier copy arrives after the cluster is already visible.
func TestReverseTimestampBroadcastRepliesKeepViewportAndMultiRowSelectionOffsets(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	text := "@all " + strings.Repeat("reverse timestamp broadcast stays anchored ", 6)
	copyAt := func(id string, at time.Time) []roomLine {
		return []roomLine{{ev: core.Event{Kind: core.KindUserText, SessionID: id, Text: text, At: at}}}
	}

	r := NewRoom().SetSize(36, 6)
	r = r.Before(copyAt("s2", base.Add(80*time.Millisecond)))
	r = r.Before(copyAt("s3", base.Add(120*time.Millisecond)))
	for i := range 12 {
		r = r.Append(retainedRoomEvent(i), roomScaleAgent)
	}
	held := roomEvents(r)
	if len(held) == 0 || held[0].ev.Text != text {
		t.Fatal("fixture did not make the reverse-order broadcast public")
	}
	before := r.roomSpans(held)[held[0].id]
	if before.rows < 4 {
		t.Fatalf("fixture rendered the broadcast in %d rows, want at least 4 to exercise row offsets", before.rows)
	}
	r.tr.scroll = before.first + 1
	wantTop := ansi.Strip(r.tr.lines.at(r.tr.scroll))

	a := NewRoomApp(nil, Stream{}, nil)
	a.room = r
	a.sel = selection{
		pane:   "",
		anchor: point{line: before.first + 1, col: 1},
		head:   point{line: before.first + 2, col: 5},
	}
	a.selecting = true

	a = a.withRoom(a.room.Before(copyAt("s1", base)))
	afterLines := roomEvents(a.room)
	if len(afterLines) == 0 || afterLines[0].ev.Text != text {
		t.Fatal("late earliest copy changed the visible logical broadcast")
	}
	after := a.room.roomSpans(afterLines)[afterLines[0].id]
	if a.room.tr.scroll != after.first+1 {
		t.Errorf("late earliest copy moved viewport row offset to %d, want %d", a.room.tr.scroll, after.first+1)
	}
	if got := ansi.Strip(a.room.tr.lines.at(a.room.tr.scroll)); got != wantTop {
		t.Errorf("late earliest copy changed viewport content from %q to %q", wantTop, got)
	}
	if !a.selecting || a.sel == (selection{}) {
		t.Fatal("late earliest copy cleared the active multi-row broadcast selection")
	}
	if a.sel.anchor.line != after.first+1 || a.sel.head.line != after.first+2 {
		t.Errorf("multi-row selection offsets became [%d,%d], want [%d,%d]", a.sel.anchor.line, a.sel.head.line, after.first+1, after.first+2)
	}
}

// Production mutation caught: checking only the old identities inside a new
// public cluster transfers the displayed block's identity when another still-
// retained member of the old cluster is repartitioned into a private singleton.
func TestPublicPlusPrivateBroadcastSplitDoesNotTransferViewportOrSelectionIdentity(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	text := "@all " + strings.Repeat("a repartitioned broadcast is a different logical send ", 5)
	copyAt := func(id string, at time.Time) []roomLine {
		return []roomLine{{ev: core.Event{Kind: core.KindUserText, SessionID: id, Text: text, At: at}}}
	}

	r := NewRoom().SetSize(36, 6)
	r = r.Before(copyAt("s1", base.Add(4*time.Second)))
	r = r.Before(copyAt("s2", base.Add(8*time.Second)))
	for i := range 12 {
		r = r.Append(retainedRoomEvent(i), roomScaleAgent)
	}
	held := roomEvents(r)
	if len(held) == 0 || held[0].ev.Text != text {
		t.Fatal("fixture did not make the t+4/t+8 broadcast public")
	}
	before := r.roomSpans(held)[held[0].id]
	if before.rows < 4 {
		t.Fatalf("fixture rendered the broadcast in %d rows, want at least 4 to exercise row identity", before.rows)
	}
	r.tr.scroll = before.first + 1

	a := NewRoomApp(nil, Stream{}, nil)
	a.room = r
	a.sel = selection{
		pane:   "",
		anchor: point{line: before.first + 1, col: 1},
		head:   point{line: before.first + 2, col: 5},
	}
	a.selecting = true

	a = a.withRoom(a.room.Before(copyAt("s3", base)))
	after := roomEvents(a.room)
	if len(after) == 0 || after[0].ev.Text != text {
		t.Fatal("late t copy removed the newly proved t/t+4 broadcast")
	}
	if a.room.tr.scroll != a.room.tr.first() {
		t.Errorf("repartitioned broadcast inherited the old viewport at %d, want clamp to %d", a.room.tr.scroll, a.room.tr.first())
	}
	if a.sel != (selection{}) || a.selecting {
		t.Fatalf("repartitioned broadcast inherited the old selection: %+v, selecting=%v", a.sel, a.selecting)
	}
}

// Production mutation caught: raw-front pruning that lands immediately before
// a proved cluster must leave that whole cluster and its displayed identity
// alone while still enforcing the 1,600-event bound.
func TestBroadcastIdentitySurvivesRawBackstopPruningWhenClusterIsWhollyRetained(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	text := "@all " + strings.Repeat("raw backstop keeps this logical broadcast anchored ", 5)
	copyAt := func(id string, at time.Time) []roomLine {
		return []roomLine{{ev: core.Event{Kind: core.KindUserText, SessionID: id, Text: text, At: at}}}
	}

	r := NewRoom().SetSize(36, 6)
	r = r.Before(copyAt("s1", base))
	r = r.Before(copyAt("s2", base.Add(80*time.Millisecond)))
	r = r.Before(copyAt("s3", base.Add(120*time.Millisecond)))
	for i := range 12 {
		r = r.Append(retainedRoomEvent(i), roomScaleAgent)
	}
	held := roomEvents(r)
	if len(held) == 0 || held[0].ev.Text != text {
		t.Fatal("fixture did not make the three-copy broadcast public")
	}
	before := r.roomSpans(held)[held[0].id]
	if before.rows < 4 {
		t.Fatalf("fixture rendered the broadcast in %d rows, want at least 4 to exercise row offsets", before.rows)
	}
	r.tr.scroll = before.first + 1

	a := NewRoomApp(nil, Stream{}, nil)
	a.room = r
	a.sel = selection{
		pane:   "",
		anchor: point{line: before.first + 1, col: 1},
		head:   point{line: before.first + 2, col: 5},
	}
	a.selecting = true

	noise := make([]roomLine, roomRawEvents-2)
	noise[0] = roomLine{ev: core.Event{
		Kind:      core.KindAssistantText,
		SessionID: "private-noise",
		Text:      "older private noise",
		At:        base.Add(-time.Second),
	}}
	for i := 1; i < len(noise); i++ {
		noise[i] = roomLine{ev: core.Event{
			Kind:      core.KindAssistantText,
			SessionID: "private-noise",
			Text:      fmt.Sprintf("private-%04d", i),
			At:        base.Add(time.Duration(i) * time.Second),
		}}
	}
	a = a.withRoom(a.room.Before(noise))
	if len(a.room.raw) != roomRawEvents || a.room.raw[0].ev.SessionID != "s1" {
		t.Fatalf("raw pruning disturbed the wholly retained cluster; raw len=%d first=%q", len(a.room.raw), a.room.raw[0].ev.SessionID)
	}
	afterLines := roomEvents(a.room)
	if len(afterLines) == 0 || afterLines[0].ev.Text != text {
		t.Fatal("raw pruning removed the wholly retained logical broadcast")
	}
	after := a.room.roomSpans(afterLines)[afterLines[0].id]
	if a.room.tr.scroll != after.first+1 {
		t.Errorf("raw pruning moved viewport row offset to %d, want %d", a.room.tr.scroll, after.first+1)
	}
	if !a.selecting || a.sel == (selection{}) {
		t.Fatal("raw pruning cleared the active multi-row broadcast selection")
	}
	if a.sel.anchor.line != after.first+1 || a.sel.head.line != after.first+2 {
		t.Errorf("raw pruning changed selection offsets to [%d,%d], want [%d,%d]", a.sel.anchor.line, a.sel.head.line, after.first+1, after.first+2)
	}
}

// Safety guard: a stable identity belongs only to a cluster broadcastIndex has
// proved public. A later same-session copy makes the run ambiguous and must
// still remove it rather than preserving the previously displayed line.
func TestStableBroadcastIdentityDoesNotKeepAnAmbiguousRepeatedSendPublic(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	text := "same words twice"
	copyAt := func(id string, at time.Time) []roomLine {
		return []roomLine{{ev: core.Event{Kind: core.KindUserText, SessionID: id, Text: text, At: at}}}
	}

	r := NewRoom().Before(copyAt("s1", base))
	r = r.Before(copyAt("s2", base.Add(80*time.Millisecond)))
	if got := texts(r); len(got) != 1 || got[0] != text {
		t.Fatalf("fixture did not first display the proved broadcast: %v", got)
	}
	r = r.Before(copyAt("s1", base.Add(120*time.Millisecond)))
	if got := texts(r); len(got) != 0 {
		t.Errorf("stable identity kept an ambiguous repeated send public: %v", got)
	}
}

// Production mutation caught: retaining selection endpoints inside reclaimed
// content lets copy/highlight address lines the current room no longer owns.
func TestRoomRetentionClearsOnlyASelectionThatWasEvicted(t *testing.T) {
	a := NewRoomApp(nil, Stream{}, nil)
	a.room = retainedRoom(wantRoomRetention)
	a.sel = selection{pane: "", anchor: point{line: 0, col: 0}, head: point{line: 0, col: 2}}

	a = a.appendEvent(retainedRoomEvent(wantRoomRetention))
	if a.sel != (selection{}) {
		t.Errorf("selection into reclaimed content survived: %+v", a.sel)
	}
}

// Production mutation caught: omitting Append's trim translation leaves the
// final evicted row selected because the first marker reuses exactly cut-1.
// The stale highlight then copies the marker even though the drag selected
// content that no longer exists.
func TestFirstReclamationDoesNotAliasTheLastEvictedRowSelectionToTheMarker(t *testing.T) {
	forceColour(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "")
	a := NewRoomApp(nil, Stream{}, nil)
	a.room = retainedRoom(wantRoomRetention)
	held := roomEvents(a.room)
	oldest := a.room.roomSpans(held)[held[0].id]
	lastEvictedRow := oldest.first + oldest.rows - 1
	a.room.tr.scroll = lastEvictedRow
	a.sel = selection{pane: "", anchor: point{line: lastEvictedRow, col: 0}, head: point{line: lastEvictedRow, col: 2}}
	a.selecting = true

	a = a.appendEvent(retainedRoomEvent(wantRoomRetention))
	if marker := a.room.tr.first(); marker != lastEvictedRow {
		t.Fatalf("fixture marker landed at %d, want the old last-evicted row %d", marker, lastEvictedRow)
	}
	if a.sel != (selection{}) || a.selecting {
		t.Fatalf("selection on the final evicted row became the marker selection: %+v, selecting=%v", a.sel, a.selecting)
	}
	selected := a.room.tr.view(a.selectionIn(""))
	plain := a.room.tr.view(marked{})
	if selected != plain {
		t.Error("the first reclamation marker inherited the evicted row's highlight")
	}
	if _, cmd := a.endSelection(); cmd != nil {
		t.Error("releasing the cleared evicted-row selection copied the marker")
	}
}

// Production mutation caught: treating selection{} as absent while the mouse
// is still down skips trim translation for the real anchor at line 0, column 0.
// Motion after rollover then aliases reclaimed content and copies the marker.
func TestRoomRolloverClearsAZeroValuedLiveMouseAnchorBeforeMotionAndRelease(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "")
	a := newRoomApp(t).withSize(80, 24)
	a.layout.ShowGroups, a.layout.ShowRoster = false, false
	a = a.resizePanes()
	a.room = retainedRoom(wantRoomRetention).
		SetSize(a.regions().Room(), a.paneHeight()).
		ScrollUp(1 << 30)

	a, _ = a.mouse(pressAt(0, 0))
	if !a.selecting || a.sel != (selection{}) {
		t.Fatalf("mouse-down at transcript (0,0) produced sel=%+v selecting=%v; fixture did not reach the zero-valued live anchor", a.sel, a.selecting)
	}

	a = a.appendEvent(retainedRoomEvent(wantRoomRetention))
	if a.selecting || a.sel != (selection{}) {
		t.Errorf("rollover left the reclaimed zero-valued anchor live: sel=%+v selecting=%v", a.sel, a.selecting)
	}
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 20, Y: 0})
	a, cmd := a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: 20, Y: 0})
	if cmd != nil {
		msg := cmd().(copiedMsg)
		t.Errorf("release after the invalid anchor was reclaimed copied %q", msg.seq)
	}
	if got := a.selectionIn(""); got != (marked{}) {
		t.Errorf("release after rollover left a highlight at %+v", got)
	}
}

// Production mutation caught: a later trim must translate the old fixed marker
// to the new fixed marker. Treating it as an ordinary row clears the active
// highlight and prevents the release from copying the marker.
func TestMarkerSelectionMovesWithTheMarkerAcrossRepeatedReclamation(t *testing.T) {
	forceColour(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "")
	a := NewRoomApp(nil, Stream{}, nil)
	a.room = retainedRoom(wantRoomRetention + 1)
	marker := a.room.tr.first()
	a.room.tr.scroll = marker
	a.sel = selection{
		pane:   "",
		anchor: point{line: marker, col: 0},
		head:   point{line: marker, col: ansi.StringWidth(wantReclaimedLine) - 1},
	}
	a.selecting = true

	for i := range 3 {
		oldMarker := a.room.tr.first()
		a = a.appendEvent(retainedRoomEvent(wantRoomRetention + 1 + i))
		marker = a.room.tr.first()
		if marker <= oldMarker {
			t.Fatalf("reclamation %d did not advance the marker: %d -> %d", i+1, oldMarker, marker)
		}
		if !a.selecting || a.sel == (selection{}) {
			t.Fatalf("reclamation %d cleared the active marker selection", i+1)
		}
		if a.sel.anchor.line != marker || a.sel.head.line != marker {
			t.Fatalf("reclamation %d left marker selection at [%d,%d], want %d", i+1, a.sel.anchor.line, a.sel.head.line, marker)
		}
	}

	selectedRow := strings.Split(a.room.tr.view(a.selectionIn("")), "\n")[0]
	plainRow := strings.Split(a.room.tr.view(marked{}), "\n")[0]
	if selectedRow == plainRow || ansi.Strip(selectedRow) != ansi.Strip(plainRow) {
		t.Error("translated marker selection is not highlighted on the current marker")
	}
	_, cmd := a.endSelection()
	if cmd == nil {
		t.Fatal("releasing the translated marker selection produced no clipboard command")
	}
	msg := cmd().(copiedMsg)
	if want := clipboardSequence(wantReclaimedLine, ""); msg.seq != want {
		t.Errorf("translated marker clipboard sequence = %q, want %q", msg.seq, want)
	}
}

// Production mutation caught: endSelection used only transcript.lines, so the
// visible prefix at lines.first()-1 produced no clipboard command.
func TestTheReclamationMarkerCanBeCopiedByItself(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "")
	a := NewRoomApp(nil, Stream{}, nil)
	a.room = retainedRoom(wantRoomRetention + 1)
	marker := a.room.tr.first()
	a.sel = selection{pane: "", anchor: point{line: marker, col: 0}, head: point{line: marker, col: ansi.StringWidth(wantReclaimedLine) - 1}}
	a.selecting = true

	_, cmd := a.endSelection()
	if cmd == nil {
		t.Fatal("dragging the visible reclamation marker produced no clipboard command")
	}
	msg := cmd().(copiedMsg)
	if want := clipboardSequence(wantReclaimedLine, ""); msg.seq != want {
		t.Errorf("marker clipboard sequence = %q, want %q", msg.seq, want)
	}
}

// Production mutation caught: clipboard extraction from raw transcript strings
// disagrees with the 24-column representation view highlights. The marker-only
// case guards the boundary; crossing into content catches the six unseen cells.
func TestNarrowReclamationMarkerClipboardMatchesTheVisibleHighlight(t *testing.T) {
	forceColour(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "")
	const width = 24
	base := NewRoomApp(nil, Stream{}, nil)
	base.room = retainedRoom(wantRoomRetention+1).SetSize(width, 12)
	marker := base.room.tr.first()
	base.room.tr.scroll = marker
	content := base.room.tr.lines.first()
	for content < base.room.tr.lines.len() && ansi.StringWidth(ansi.Strip(base.room.tr.lines.at(content))) == 0 {
		content++
	}
	if content >= base.room.tr.lines.len() {
		t.Fatal("fixture has no retained content after the marker")
	}

	for _, tc := range []struct {
		name string
		head point
		want string
	}{
		{
			name: "marker only",
			head: point{line: marker, col: width - 1},
			want: "… older room history rec",
		},
		{
			name: "marker into content",
			head: point{line: content, col: 2},
			want: func() string {
				lines := []string{"… older room history rec"}
				for line := base.room.tr.lines.first(); line < content; line++ {
					visible := ansi.Truncate(base.room.tr.lines.at(line), width, "")
					lines = append(lines, strings.TrimRight(ansi.Strip(visible), " "))
				}
				visible := ansi.Truncate(base.room.tr.lines.at(content), width, "")
				lines = append(lines, strings.TrimRight(ansi.Strip(ansi.Cut(visible, 0, 3)), " "))
				return strings.Join(lines, "\n")
			}(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := base
			a.sel = selection{pane: "", anchor: point{line: marker, col: 0}, head: tc.head}
			a.selecting = true
			selectedRow := strings.Split(a.room.tr.view(a.selectionIn("")), "\n")[0]
			plainRow := strings.Split(a.room.tr.view(marked{}), "\n")[0]
			if selectedRow == plainRow || ansi.Strip(selectedRow) != ansi.Strip(plainRow) {
				t.Error("the narrow marker selection is not highlighted on exactly the visible cells")
			}
			_, cmd := a.endSelection()
			if cmd == nil {
				t.Fatal("releasing the narrow marker selection produced no clipboard command")
			}
			msg := cmd().(copiedMsg)
			if want := clipboardSequence(tc.want, ""); msg.seq != want {
				t.Errorf("narrow clipboard sequence = %q, want %q for visible text %q", msg.seq, want, tc.want)
			}
		})
	}
}

// Production mutation caught: truncating only the fixed marker still lets an
// overwide retained intermediate line copy cells the pane never displayed.
// The same-line subtest protects ordinary narrower-than-width ANSI cell slices.
func TestClipboardUsesVisibleWidthForOverwideRetainedContent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "")
	const visible = "012345678901234567890123"
	base := NewRoomApp(nil, Stream{}, nil)
	base.room.tr = transcript{
		width:  24,
		height: 3,
		lines:  chunked[string]{}.append("\x1b[31m"+visible+"UNSEEN\x1b[0m", "tail"),
	}

	for _, tc := range []struct {
		name string
		sel  selection
		want string
	}{
		{
			name: "across lines",
			sel:  selection{pane: "", anchor: point{line: 0, col: 0}, head: point{line: 1, col: 3}},
			want: visible + "\ntail",
		},
		{
			name: "narrow ANSI cell slice",
			sel:  selection{pane: "", anchor: point{line: 0, col: 4}, head: point{line: 0, col: 7}},
			want: "4567",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := base
			a.sel, a.selecting = tc.sel, true
			_, cmd := a.endSelection()
			if cmd == nil {
				t.Fatal("overwide retained-content selection produced no clipboard command")
			}
			msg := cmd().(copiedMsg)
			if want := clipboardSequence(tc.want, ""); msg.seq != want {
				t.Errorf("clipboard sequence = %q, want %q for %q", msg.seq, want, tc.want)
			}
		})
	}
}

// Production mutation caught: omitting the prefix from the extracted slice
// shifts every selected content line up by one when the drag crosses it.
func TestCopyAcrossTheReclamationMarkerKeepsMarkerAndContentAligned(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "")
	a := NewRoomApp(nil, Stream{}, nil)
	a.room = retainedRoom(wantRoomRetention + 1)
	marker := a.room.tr.first()
	last := a.room.tr.lines.first()
	for last < a.room.tr.lines.len() && ansi.StringWidth(ansi.Strip(a.room.tr.lines.at(last))) == 0 {
		last++
	}
	plain := []string{wantReclaimedLine}
	for line := a.room.tr.lines.first(); line <= last; line++ {
		plain = append(plain, strings.TrimRight(ansi.Strip(a.room.tr.lines.at(line)), " "))
	}
	wantText := strings.Join(plain, "\n")
	a.sel = selection{pane: "", anchor: point{line: marker, col: 0}, head: point{line: last, col: ansi.StringWidth(ansi.Strip(a.room.tr.lines.at(last))) - 1}}
	a.selecting = true

	_, cmd := a.endSelection()
	if cmd == nil {
		t.Fatal("drag across marker and retained content produced no clipboard command")
	}
	msg := cmd().(copiedMsg)
	if want := clipboardSequence(wantText, ""); msg.seq != want {
		t.Errorf("marker+content clipboard sequence = %q, want %q", msg.seq, want)
	}
}

// Production mutation caught: validating a translated selection against
// lines.first instead of the visible transcript first clears the marker while
// a late history reply lands between mouse press and release.
func TestLateHistoryKeepsAnActiveMarkerSelectionHighlightAndClipboard(t *testing.T) {
	forceColour(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "")
	base := NewRoomApp(nil, Stream{}, nil)
	base.room = retainedRoom(wantRoomRetention + 1)
	marker := base.room.tr.first()
	base.room.tr.scroll = marker
	last := base.room.tr.lines.first()
	for last < base.room.tr.lines.len() && ansi.StringWidth(ansi.Strip(base.room.tr.lines.at(last))) == 0 {
		last++
	}
	plain := []string{wantReclaimedLine}
	for line := base.room.tr.lines.first(); line <= last; line++ {
		plain = append(plain, strings.TrimRight(ansi.Strip(base.room.tr.lines.at(line)), " "))
	}

	for _, tc := range []struct {
		name string
		head point
		want string
	}{
		{"marker only", point{line: marker, col: ansi.StringWidth(wantReclaimedLine) - 1}, wantReclaimedLine},
		{"marker and content", point{line: last, col: ansi.StringWidth(ansi.Strip(base.room.tr.lines.at(last))) - 1}, strings.Join(plain, "\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := base
			a.sel = selection{pane: "", anchor: point{line: marker, col: 0}, head: tc.head}
			a.selecting = true
			a = a.withRoom(a.room.Before(restoredBroadcast("late history", time.Unix(1_700_000_000, 0))))

			if !a.selecting || a.sel == (selection{}) {
				t.Fatalf("late history cleared the active %s selection", tc.name)
			}
			if _, _, ok := a.sel.marked().covers(a.room.tr.first()); !ok {
				t.Fatalf("translated %s selection no longer covers the visible marker", tc.name)
			}
			selectedRow := strings.Split(a.room.tr.view(a.selectionIn("")), "\n")[0]
			plainRow := strings.Split(a.room.tr.view(marked{}), "\n")[0]
			if selectedRow == plainRow || ansi.Strip(selectedRow) != ansi.Strip(plainRow) {
				t.Errorf("translated %s selection is not highlighted on the marker", tc.name)
			}

			_, cmd := a.endSelection()
			if cmd == nil {
				t.Fatalf("releasing translated %s selection produced no clipboard command", tc.name)
			}
			msg := cmd().(copiedMsg)
			if want := clipboardSequence(tc.want, ""); msg.seq != want {
				t.Errorf("translated %s clipboard sequence = %q, want %q", tc.name, msg.seq, want)
			}
		})
	}
}

// Production mutation caught: trimming through a shared chunk or transcript
// map mutates the older Room value that immutable value receivers promise.
func TestRoomRetentionDoesNotMutateAnOlderRoomCopy(t *testing.T) {
	before := retainedRoom(wantRoomRetention)
	oldest := roomEvents(before)[0].ev.MessageID
	oldFirstLine := before.tr.lines.at(0)

	after := before.Append(retainedRoomEvent(wantRoomRetention), roomScaleAgent)
	if got := len(roomEvents(before)); got != wantRoomRetention {
		t.Errorf("older Room copy changed length to %d", got)
	}
	if got := roomEvents(before)[0].ev.MessageID; got != oldest {
		t.Errorf("older Room copy changed its first event from %q to %q", oldest, got)
	}
	if got := before.tr.lines.at(0); got != oldFirstLine {
		t.Errorf("older transcript copy changed its first rendered line from %q to %q", oldFirstLine, got)
	}
	if got := roomEvents(after)[0].ev.MessageID; got == oldest {
		t.Errorf("derived Room still retains evicted event %q", oldest)
	}
}

// Production mutation caught: reslicing a chunk or leaving click maps shared
// keeps pointers, strings, and map entries for reclaimed content reachable.
func TestRoomRetentionReleasesEvictedValuesAndClickMaps(t *testing.T) {
	tool := &core.ToolCall{ID: "evicted-tool", Name: "Read"}
	commands := commandSet{names: []string{"evicted-command"}}
	agent := roomScaleAgent
	agent.advertised = &commands
	r := NewRoom().SetSize(80, 12)
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: "evicted rendered string", Tool: tool, MessageID: "oldest"}, agent)
	for i := 1; i < wantRoomRetention; i++ {
		r = r.Append(retainedRoomEvent(i), roomScaleAgent)
	}
	oldLine := r.tr.lines.first() + len(blockLines(roomBanner(r.blockWidth()), true))
	r.tr.tools = map[int]string{oldLine: "evicted-tool", r.tr.lines.len() - 1: "kept-tool"}
	r.tr.heads = map[string]int{"evicted-tool": oldLine, "kept-tool": r.tr.lines.len() - 1}
	r.tr.runs = map[int]string{oldLine: "evicted-run", r.tr.lines.len() - 1: "kept-run"}
	r.tr.runHeads = map[string]int{"evicted-run": oldLine, "kept-run": r.tr.lines.len() - 1}

	r = r.Append(retainedRoomEvent(wantRoomRetention), roomScaleAgent)
	for _, chunk := range r.said.chunks {
		for _, line := range chunk {
			if line.ev.Tool == tool || line.by.advertised == &commands || strings.Contains(line.ev.Text, "evicted rendered string") {
				t.Fatal("current Room still reaches pointer-bearing values from the evicted event")
			}
		}
	}
	for _, line := range r.tr.lines.chunks {
		for _, text := range line {
			if strings.Contains(text, "evicted rendered string") {
				t.Fatal("current transcript still reaches an evicted rendered string")
			}
		}
	}
	if _, ok := r.tr.heads["evicted-tool"]; ok || r.tr.tools[oldLine] == "evicted-tool" {
		t.Error("current transcript still reaches evicted tool click-map entries")
	}
	if _, ok := r.tr.runHeads["evicted-run"]; ok || r.tr.runs[oldLine] == "evicted-run" {
		t.Error("current transcript still reaches evicted run click-map entries")
	}
	if r.tr.heads["kept-tool"] == 0 || r.tr.runHeads["kept-run"] == 0 {
		t.Error("reclamation removed retained click-map entries")
	}
}

// Production mutation caught: pruning only clusters broadcastIndex accepts as
// public can split an ambiguous same-session run and let its tail be rejudged
// under a different anchor after rollover.
func TestRoomRawBackstopDoesNotSplitARefusedRepeatedSessionRun(t *testing.T) {
	earlier := []roomLine{
		{ev: core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "ambiguous", MessageID: "run-0", At: base}},
		{ev: core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "ambiguous", MessageID: "run-1", At: base.Add(time.Second)}},
		{ev: core.Event{Kind: core.KindUserText, SessionID: "s2", Text: "ambiguous", MessageID: "run-2", At: base.Add(2 * time.Second)}},
	}
	for i := range roomRawEvents - 2 {
		earlier = append(earlier, roomLine{ev: core.Event{
			Kind:      core.KindTurnEnd,
			MessageID: fmt.Sprintf("tail-%d", i),
			At:        base.Add(time.Duration(i+10) * time.Second),
		}})
	}

	r := NewRoom().Before(earlier)
	if len(r.raw) > roomRawEvents {
		t.Fatalf("Room.raw retained %d events, want at most %d", len(r.raw), roomRawEvents)
	}
	if len(r.raw) == 0 {
		t.Fatal("raw pruning removed the retained tail with the refused run")
	}
	if r.raw[0].ev.MessageID != "tail-0" {
		t.Fatalf("raw pruning left part of a refused run at the boundary: first=%q", r.raw[0].ev.MessageID)
	}
}

// Production mutation caught: advancing through one split run can put the cut
// inside an interleaved run of another text; stopping after one pass leaves the
// second run vulnerable to re-anchoring.
func TestRoomRawBackstopClosesOverInterleavedAnchorRuns(t *testing.T) {
	earlier := []roomLine{
		{ev: core.Event{Kind: core.KindUserText, SessionID: "a1", Text: "alpha", MessageID: "alpha-0", At: base}},
		{ev: core.Event{Kind: core.KindUserText, SessionID: "b1", Text: "beta", MessageID: "beta-0", At: base.Add(time.Second)}},
		{ev: core.Event{Kind: core.KindUserText, SessionID: "a2", Text: "alpha", MessageID: "alpha-1", At: base.Add(2 * time.Second)}},
		{ev: core.Event{Kind: core.KindUserText, SessionID: "b2", Text: "beta", MessageID: "beta-1", At: base.Add(3 * time.Second)}},
	}
	for i := range roomRawEvents - 3 {
		earlier = append(earlier, roomLine{ev: core.Event{
			Kind:      core.KindTurnEnd,
			MessageID: fmt.Sprintf("tail-%d", i),
			At:        base.Add(time.Duration(i+10) * time.Second),
		}})
	}

	r := NewRoom().Before(earlier)
	if len(r.raw) > roomRawEvents {
		t.Fatalf("Room.raw retained %d events, want at most %d", len(r.raw), roomRawEvents)
	}
	if len(r.raw) == 0 {
		t.Fatal("raw pruning removed the retained tail after interleaved runs")
	}
	if r.raw[0].ev.MessageID != "tail-0" {
		t.Fatalf("raw pruning stopped inside an interleaved run: first=%q", r.raw[0].ev.MessageID)
	}
}

// Production mutation caught: reslicing Room.raw keeps its old backing array
// and therefore references to restored core.Event/Agent values before the cap.
func TestRoomRawBackstopOwnsOnlyItsRetainedTail(t *testing.T) {
	tool := &core.ToolCall{ID: "evicted-raw-tool", Name: "Read"}
	commands := commandSet{names: []string{"evicted-raw-command"}}
	agent := roomScaleAgent
	agent.advertised = &commands
	earlier := make([]roomLine, roomRawEvents+1)
	for i := range earlier {
		earlier[i] = roomLine{ev: core.Event{Kind: core.KindTurnEnd, MessageID: fmt.Sprintf("raw-%d", i), At: time.Unix(int64(i+1), 0)}}
	}
	earlier[0].ev.Tool = tool
	earlier[0].ev.Text = "evicted raw payload"
	earlier[0].by = agent
	r := NewRoom().Before(earlier)
	if len(r.raw) != roomRawEvents {
		t.Fatalf("Room.raw retains %d events, want %d", len(r.raw), roomRawEvents)
	}
	if cap(r.raw) != len(r.raw) {
		t.Errorf("Room.raw capacity is %d for %d retained events; hidden backing slots can retain evicted values", cap(r.raw), len(r.raw))
	}
	if r.raw[0].ev.MessageID != "raw-1" {
		t.Errorf("oldest raw event = %q, want raw-1", r.raw[0].ev.MessageID)
	}
	for _, line := range r.raw {
		if line.ev.Tool == tool || line.by.advertised == &commands || line.ev.Text == "evicted raw payload" {
			t.Fatal("current Room.raw still reaches pointer-bearing values from an evicted event")
		}
	}
}

func cappedAssistantRoom(text string) Room {
	r := NewRoom()
	lines := make([]roomLine, wantRoomRetention)
	for i := range lines {
		lines[i] = roomLine{
			ev: core.Event{Kind: core.KindAssistantText, Text: text},
			by: roomScaleAgent,
			id: uint64(i + 1),
		}
	}
	r.said = r.said.append(lines...)
	r.nextLineID = wantRoomRetention
	return r.SetSize(80, 12)
}

func roomAllocatedBytes(f func()) uint64 {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// Production mutation caught: retaining a parallel per-layout structure or
// rebuilding said repeatedly makes the metadata fix allocate without bound at
// the 20,000-event cap. The renderer is fenced to isolate Room's own storage.
func TestRoomBeforeMetadataAllocationStaysBoundedAtTheCap(t *testing.T) {
	original := renderRoomBlock
	renderRoomBlock = func(ev core.Event, a Agent, width int) block {
		return block{text: "assistant markdown"}
	}
	t.Cleanup(func() { renderRoomBlock = original })
	r := cappedAssistantRoom("**retained markdown**")
	before := roomAllocatedBytes(func() {
		sinkRoom = r.Before([]roomLine{{ev: core.Event{
			Kind:      core.KindUserText,
			SessionID: "private",
			Text:      "private history that stays out of the room",
			At:        time.Unix(1, 0),
		}}})
	})
	perEvent := before / wantRoomRetention
	t.Logf("Before allocated %d bytes at the cap (%d bytes per retained assistant event)", before, perEvent)
	if perEvent > 6*1024 {
		t.Errorf("Before allocated %d bytes per retained assistant event, want at most 6 KiB", perEvent)
	}
}

// Production mutation caught: making roomSpans call renderRoomBlock to recover
// heights turns one Before into three full-window renders. Markdown is kept in
// the events while the rendering seam returns a cheap deterministic block so a
// 20,000-event control-flow assertion does not allocate gigabytes in glamour.
func TestRoomBeforeRendersEachRetainedAssistantExactlyOnce(t *testing.T) {
	original := renderRoomBlock
	renders := 0
	renderRoomBlock = func(ev core.Event, a Agent, width int) block {
		if ev.Kind != core.KindAssistantText || ev.Text != "**retained markdown**" {
			t.Fatalf("unexpected event reached room rendering: kind=%v text=%q", ev.Kind, ev.Text)
		}
		renders++
		return block{text: "assistant markdown"}
	}
	t.Cleanup(func() { renderRoomBlock = original })
	r := cappedAssistantRoom("**retained markdown**")
	renders = 0
	r = r.Before([]roomLine{{ev: core.Event{
		Kind:      core.KindUserText,
		SessionID: "private",
		Text:      "private history that stays out of the room",
		At:        time.Unix(1, 0),
	}}})
	if got := len(roomEvents(r)); got != wantRoomRetention {
		t.Fatalf("Before retained %d room events, want the cap of %d", got, wantRoomRetention)
	}
	if renders != wantRoomRetention {
		t.Errorf("Before rendered %d retained assistant events, want exactly one pass of %d", renders, wantRoomRetention)
	}
}

// Production mutation caught: leaving cached rows untouched on a width change
// makes the next history merge translate viewport/selection offsets using the
// previous wrapping. The transcript is the independent laid-out row count.
func TestRoomResizeRefreshesCachedAssistantRowCounts(t *testing.T) {
	text := strings.Repeat("wrapped assistant markdown words ", 18)
	wide := NewRoom().SetSize(120, 12).Append(core.Event{Kind: core.KindAssistantText, Text: text}, roomScaleAgent)
	wideRows := roomEvents(wide)[0].rows
	narrow := wide.SetSize(40, 12)
	narrowRows := roomEvents(narrow)[0].rows
	if narrowRows == wideRows {
		t.Fatalf("assistant row count stayed %d across 120 -> 40 columns; fixture did not rewrap", narrowRows)
	}
	bannerRows := len(blockLines(roomBanner(narrow.blockWidth()), true))
	want := narrow.tr.lines.count() - bannerRows
	if narrowRows != want {
		t.Errorf("cached assistant rows after resize = %d, transcript laid out %d", narrowRows, want)
	}
}

// Production mutation caught: rebuilding all retained blocks on each trim
// makes allocation per append grow with the 20,000-event retained window.
func TestRoomAppendAllocationStaysBoundedAtTheRetentionCap(t *testing.T) {
	short := roomBytesPerAppend(retainedRoom(scaleShort))
	atCap := roomBytesPerAppend(retainedRoom(wantRoomRetention))
	t.Logf("one append allocates %d bytes at %d events and %d at the %d-event cap", short, scaleShort, atCap, wantRoomRetention)
	if ratio := float64(atCap) / float64(short); ratio > 4 {
		t.Errorf("one append allocates %.1fx more at the retention cap; want at most 4x", ratio)
	}
}

func BenchmarkRoomAppendAtRetentionCap(b *testing.B) {
	r := retainedRoom(wantRoomRetention)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		r = r.Append(retainedRoomEvent(wantRoomRetention+i), roomScaleAgent)
	}
	sinkRoom = r
}

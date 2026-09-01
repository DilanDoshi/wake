package ui

// Expanding a room response in place: the per-line click and the expand-all ⌃E,
// what each acts on, what it refuses, and the scroll rule that tells them apart.
//
// The room folds a long reply into a two-line pointer so thirty agents do not
// bury each other. These tests hold that a reader can ask for one - or all - in
// full without leaving the group chat, and that the ask is a toggle: a room full
// of expanded turns is the wall of text the fold exists to prevent.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// roomFoldMark is the glyph a collapsed reply's pointer opens with (see
// collapsedFormat). Used where a test reads the whole app frame - which includes
// the composer legend - because that legend draws `⌃D open DM` too once the pane
// is wide enough, so openDMHint alone no longer means "a reply is folded".
const roomFoldMark = "⤷"

// roomShown is the room drawn at a size and stripped of styling, so a test can
// name a row it should or should not carry.
func roomShown(r Room, w, h int) string { return ansi.Strip(r.View(w, h)) }

// expandableLine is an absolute transcript line inside the room's first
// collapsible reply, and the id of that block. It reads the room's own spans, so
// the line it returns is the one a click on that reply would resolve to.
func expandableLine(t *testing.T, r Room) (int, uint64) {
	t.Helper()
	lines := r.said.slice(r.said.first(), r.said.len())
	spans := r.roomSpans(lines)
	for _, l := range lines {
		if roomCollapsible(l.ev, r.blockWidth()) {
			return spans[l.id].first, l.id
		}
	}
	t.Fatal("the room has no collapsible reply for this test to expand")
	return 0, 0
}

// The full render is what agentSaid already computed and then threw away; expand
// keeps it. A short reply is whole either way, so the flag is a no-op there - the
// pointer only exists past the cap.
func TestExpandedDrawsTheWholeReplyAndDropsThePointer(t *testing.T) {
	ev := core.Event{Kind: core.KindAssistantText, Text: longRoomReply("THE_TAIL")}

	collapsed := roomBlock(ev, Agent{Name: "sydney"}, roomWidth, false)
	if !strings.Contains(collapsed.text, openDMHint) || strings.Contains(collapsed.text, "THE_TAIL") {
		t.Fatalf("this reply is not collapsed at false, so the test proves nothing:\n%s", collapsed.text)
	}

	expanded := roomBlock(ev, Agent{Name: "sydney"}, roomWidth, true)
	if strings.Contains(expanded.text, openDMHint) {
		t.Errorf("an expanded reply still carries the pointer:\n%s", expanded.text)
	}
	if !strings.Contains(expanded.text, "THE_TAIL") {
		t.Errorf("an expanded reply does not show its own end:\n%s", expanded.text)
	}
}

// The room interleaves many speakers, so an expanded block still has to say
// whose it is. The head is the same one the pointer carried, so attribution is
// never the thing expansion drops.
func TestAnExpandedReplyStillNamesWhoSaidIt(t *testing.T) {
	ev := core.Event{Kind: core.KindAssistantText, Text: longRoomReply("THE_TAIL")}
	b := roomBlock(ev, Agent{Name: "sydney", Label: "auth-fix"}, roomWidth, true)
	if !strings.Contains(b.text, "sydney") || !strings.Contains(b.text, "auth-fix") {
		t.Errorf("an expanded reply in a room of many speakers does not name its own:\n%s", b.text)
	}
}

// roomCollapsible is the gate both gestures share, so it has to agree with
// agentSaid about which lines draw as a pointer. Only an agent's over-cap reply
// does; your own turn wraps whole and a marker is one line.
func TestRoomCollapsibleMatchesWhatDrawsAsAPointer(t *testing.T) {
	cases := []struct {
		name string
		ev   core.Event
		want bool
	}{
		{"a long agent reply", core.Event{Kind: core.KindAssistantText, Text: longRoomReply("t")}, true},
		{"a short agent reply", core.Event{Kind: core.KindAssistantText, Text: "tests pass"}, false},
		{"your own long turn", core.Event{Kind: core.KindUserText, Text: longRoomReply("t")}, false},
		{"a silent turn", core.Event{Kind: core.KindTurnEnd}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := roomCollapsible(c.ev, roomWidth)
			hasPointer := strings.Contains(roomBlock(c.ev, Agent{Name: "sydney"}, roomWidth, false).text, openDMHint)
			if got != c.want {
				t.Errorf("roomCollapsible = %v, want %v", got, c.want)
			}
			if got != hasPointer {
				t.Errorf("roomCollapsible = %v but the block %s a pointer: the gate disagrees with what draws", got, map[bool]string{true: "carries", false: "has no"}[hasPointer])
			}
		})
	}
}

// ⌃E is expand-everything, and a second press is hide-everything. Two long
// replies both open; a short one is unaffected because there was nothing folded.
func TestToggleExpandAllOpensThenClosesEveryCollapsedReply(t *testing.T) {
	r := NewRoom().SetSize(80, 200)
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: longRoomReply("TAIL_ONE")}, Agent{Name: "sydney"})
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: "short and whole"}, Agent{Name: "john"})
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: longRoomReply("TAIL_TWO")}, Agent{Name: "iris"})
	if c := strings.Count(roomShown(r, 80, 200), openDMHint); c != 2 {
		t.Fatalf("want two collapsed replies to begin with, got %d pointers", c)
	}

	r = r.toggleExpandAll()

	out := roomShown(r, 80, 200)
	for _, tail := range []string{"TAIL_ONE", "TAIL_TWO"} {
		if !strings.Contains(out, tail) {
			t.Errorf("⌃E did not expand every collapsed reply (%s missing):\n%s", tail, out)
		}
	}
	if strings.Contains(out, openDMHint) {
		t.Errorf("a pointer survived expand-all:\n%s", out)
	}
	if !strings.Contains(out, "short and whole") {
		t.Errorf("expand-all lost a short reply that was never folded:\n%s", out)
	}

	r = r.toggleExpandAll()
	if out := roomShown(r, 80, 200); strings.Contains(out, "TAIL_ONE") || strings.Contains(out, "TAIL_TWO") {
		t.Errorf("a second ⌃E did not hide everything:\n%s", out)
	}
}

// The click is per-line: it opens the reply it landed on and leaves the rest
// folded, which is the property that keeps the room from becoming the DM.
func TestToggleLineOpensOneReplyAndLeavesTheOthersFolded(t *testing.T) {
	r := NewRoom().SetSize(80, 200)
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: longRoomReply("TAIL_ONE")}, Agent{Name: "sydney"})
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: longRoomReply("TAIL_TWO")}, Agent{Name: "iris"})
	line, _ := expandableLine(t, r)

	next, hit := r.toggleLine(line)
	if !hit {
		t.Fatal("a click on a collapsed reply reported no hit")
	}

	out := roomShown(next, 80, 200)
	if !strings.Contains(out, "TAIL_ONE") {
		t.Errorf("the clicked reply did not expand:\n%s", out)
	}
	if strings.Contains(out, "TAIL_TWO") {
		t.Errorf("expanding one reply expanded another:\n%s", out)
	}
	if c := strings.Count(out, openDMHint); c != 1 {
		t.Errorf("want one reply still folded, got %d pointers:\n%s", c, out)
	}

	if back, _ := next.toggleLine(line); strings.Contains(roomShown(back, 80, 200), "TAIL_ONE") {
		t.Errorf("a second click did not re-fold the reply:\n%s", roomShown(back, 80, 200))
	}
}

// The click refuses everything that has no pointer to open: a short reply, your
// own turn, a marker, and a line off the end of the content. A refusal is how
// clickedTool knows to leave the pane alone.
func TestToggleLineRefusesWhatIsNotAFoldedReply(t *testing.T) {
	r := NewRoom().SetSize(80, 40)
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: "tests pass"}, Agent{Name: "sydney"})
	r = r.Append(core.Event{Kind: core.KindUserText, Text: "who is stuck?"}, Agent{})
	r = r.Append(core.Event{Kind: core.KindTurnEnd}, Agent{Name: "john"})

	lines := r.said.slice(r.said.first(), r.said.len())
	spans := r.roomSpans(lines)
	for _, l := range lines {
		if _, hit := r.toggleLine(spans[l.id].first); hit {
			t.Errorf("toggleLine opened a %v, which draws no pointer", l.ev.Kind)
		}
	}
	if _, hit := r.toggleLine(r.tr.lines.len() + 5); hit {
		t.Error("toggleLine opened something off the end of the content")
	}
}

// The click keeps the reader where they are: expanding renumbers only the lines
// below it, so a scroll offset above the click still points at what was being
// read. This is the openTool rule, and the whole reason the click is not ⌃E.
func TestToggleLineKeepsAScrolledReaderInPlace(t *testing.T) {
	r := NewRoom().SetSize(80, 8)
	for range 6 {
		r = r.Append(core.Event{Kind: core.KindAssistantText, Text: longRoomReply("tail")}, Agent{Name: "sydney"})
	}
	r = r.ScrollUp(1000) // to the top
	if r.tr.atBottom() {
		t.Fatal("the reader is at the bottom, so this test cannot see a scroll be preserved")
	}
	top := r.tr.scroll
	line, _ := expandableLine(t, r)

	next, hit := r.toggleLine(line)
	if !hit {
		t.Fatal("no collapsible reply was found at the top")
	}
	if next.tr.atBottom() {
		t.Errorf("the click threw the reader to the newest line; a click to read a reply must leave them where they were")
	}
	if next.tr.scroll != top {
		t.Errorf("the scroll moved from %d to %d: expanding a reply below the reader must not move them", top, next.tr.scroll)
	}
}

// Collapsing a reply you have scrolled deep into keeps it in view rather than
// throwing you to the oldest line. The removed rows leave the old offset with no
// line to translate to, and the fallback must anchor to the block's own new
// position, not the transcript start.
func TestCollapsingFromDeepInsideStaysWithTheReply(t *testing.T) {
	r := NewRoom().SetSize(80, 10)
	for i := range 30 {
		r = r.Append(core.Event{Kind: core.KindAssistantText, Text: fmt.Sprintf("filler %d", i)}, Agent{Name: "john"})
	}
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: longRoomReply("DEEP_OPENING")}, Agent{Name: "sydney"})
	line, id := expandableLine(t, r)

	r, _ = r.toggleLine(line) // expand it to many rows
	blk := r.roomSpans(r.said.slice(r.said.first(), r.said.len()))[id]
	if blk.rows <= roomCollapseLines+4 {
		t.Fatalf("the reply did not expand to enough rows to scroll into (%d)", blk.rows)
	}
	r.tr.scroll = blk.first + blk.rows/2 // deep inside the expanded reply
	if r.tr.atBottom() {
		t.Fatal("the fixture is at the bottom; it cannot exercise a deep-inside collapse")
	}

	r, _ = r.toggleLine(line) // collapse it from that deep offset

	if r.tr.scroll == r.tr.first() {
		t.Errorf("collapsing from deep inside jumped the reader to the oldest line (scroll=%d)", r.tr.scroll)
	}
	// The reply is folded now, so what should be in view is its collapsed
	// preview - its opening and its pointer - not the first filler at the top.
	out := roomShown(r, 80, 10)
	if !strings.Contains(out, "OPENING of the reply.") || !strings.Contains(out, openDMHint) {
		t.Errorf("the folded reply is not in view after collapsing it from deep inside:\n%s", out)
	}
	if strings.Contains(out, "filler 0") {
		t.Errorf("folding the reply scrolled all the way back to the first filler:\n%s", out)
	}
}

// The expand set is bounded by what the room retains, not by every reply ever
// clicked: an opened reply that ages out of the 20,000-event cap leaves no entry
// behind, and pruning it does not mutate an older Room that still holds it.
func TestExpandSetIsPrunedWhenAnOpenedReplyIsReclaimed(t *testing.T) {
	r := NewRoom().SetSize(80, 12)
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: longRoomReply("OLD")}, Agent{Name: "sydney"})
	line, id := expandableLine(t, r)
	r, _ = r.toggleLine(line)
	if !r.expanded[id] {
		t.Fatal("the click did not record the reply as expanded")
	}
	held := r // an older Room, sharing the expand set until a prune copies it

	for i := range wantRoomRetention + 5 {
		r = r.Append(retainedRoomEvent(i), roomScaleAgent)
	}

	if _, ok := r.expanded[id]; ok {
		t.Errorf("the expand set still holds a reclaimed block's id, so it grows without bound")
	}
	if len(r.expanded) != 0 {
		t.Errorf("the expand set kept %d entries after its only opened reply aged out", len(r.expanded))
	}
	if !held.expanded[id] {
		t.Errorf("pruning on reclaim mutated an older Room's expand set")
	}
}

// ⌃E returns the reader to the newest line, the way the DM's ⌃E does: it
// re-renders everything, so the offset a scroll held points at lines that have
// renumbered.
func TestToggleExpandAllReturnsToTheNewestLine(t *testing.T) {
	r := NewRoom().SetSize(80, 8)
	for range 6 {
		r = r.Append(core.Event{Kind: core.KindAssistantText, Text: longRoomReply("tail")}, Agent{Name: "sydney"})
	}
	r = r.ScrollUp(1000)
	if r.tr.atBottom() {
		t.Fatal("the reader is at the bottom, so this test proves nothing")
	}

	r = r.toggleExpandAll()

	if !r.tr.atBottom() {
		t.Errorf("expand-all left the reader at a stale offset pointing at renumbered lines")
	}
}

// Expand-all is a standing choice, so a long reply arriving while it is on lands
// expanded rather than folded - ⌃E's "everything" keeps meaning everything.
func TestALongReplyArrivingUnderExpandAllLandsExpanded(t *testing.T) {
	r := NewRoom().SetSize(80, 200).toggleExpandAll()

	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: longRoomReply("FRESH_TAIL")}, Agent{Name: "sydney"})

	out := roomShown(r, 80, 200)
	if !strings.Contains(out, "FRESH_TAIL") || strings.Contains(out, openDMHint) {
		t.Errorf("a reply arriving under expand-all was folded anyway:\n%s", out)
	}
}

// Both toggles return a new Room and mutate no copy: the expand set is copied on
// write, so a handed-out Room cannot have its opens changed underneath it.
func TestExpandingARoomIsImmutable(t *testing.T) {
	base := NewRoom().SetSize(80, 200)
	base = base.Append(core.Event{Kind: core.KindAssistantText, Text: longRoomReply("TAIL")}, Agent{Name: "sydney"})
	line, _ := expandableLine(t, base)

	next, _ := base.toggleLine(line)
	if strings.Contains(roomShown(base, 80, 200), "TAIL") {
		t.Errorf("toggleLine expanded the original room, not a copy:\n%s", roomShown(base, 80, 200))
	}
	if !strings.Contains(roomShown(next, 80, 200), "TAIL") {
		t.Errorf("the returned room is not expanded:\n%s", roomShown(next, 80, 200))
	}

	all := base.toggleExpandAll()
	if strings.Contains(roomShown(base, 80, 200), "TAIL") {
		t.Errorf("toggleExpandAll expanded the original room, not a copy:\n%s", roomShown(base, 80, 200))
	}
	if !strings.Contains(roomShown(all, 80, 200), "TAIL") {
		t.Errorf("the room returned by expand-all is not expanded:\n%s", roomShown(all, 80, 200))
	}
}

// Both toggles resolve absolute lines against a room that has reclaimed its
// oldest history, where the transcript's first line is nonzero and a reclaim
// prefix sits above it. This is the base/prefix path Before uses, exercised
// through the expand toggles rather than only through a history merge.
func TestExpandingWorksAfterTheRoomHasReclaimed(t *testing.T) {
	r := NewRoom().SetSize(80, 12)
	for i := range wantRoomRetention + 5 {
		r = r.Append(retainedRoomEvent(i), roomScaleAgent)
	}
	if !r.reclaimed {
		t.Fatal("the room did not reclaim; this test needs a reclaimed room to prove anything")
	}
	r = r.Append(core.Event{Kind: core.KindAssistantText, Text: longRoomReply("RECLAIM_TAIL")}, Agent{Name: "sydney"})
	if !strings.Contains(roomShown(r, 80, 200), openDMHint) {
		t.Fatalf("the reply is not folded in the reclaimed room:\n%s", roomShown(r, 80, 200))
	}

	if out := roomShown(r.toggleExpandAll(), 80, 200); !strings.Contains(out, "RECLAIM_TAIL") {
		t.Errorf("expand-all did not expand the reply in a reclaimed room:\n%s", out)
	}

	line, _ := expandableLine(t, r)
	one, hit := r.toggleLine(line)
	if !hit {
		t.Fatal("toggleLine found no collapsible reply in the reclaimed room")
	}
	if out := roomShown(one, 80, 200); !strings.Contains(out, "RECLAIM_TAIL") {
		t.Errorf("toggleLine did not expand the reply against a reclaimed room's absolute lines:\n%s", out)
	}
}

// The mouse half, end to end: a click on a collapsed reply in the room expands
// that one. The room holds the keys already, so this is the deliberate click and
// not a focus change.
func TestClickingACollapsedRoomReplyExpandsIt(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	if a.focus != "" {
		t.Fatalf("focus is %q, want the room to hold the keys", a.focus)
	}
	a = a.applyFrame(rpc.Frame{
		Kind: rpc.FrameEvent, SessionID: "s1",
		Event: &core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: longRoomReply("CLICK_TAIL")},
	})
	// The fold is checked by its ⤷ marker rather than by openDMHint: with the
	// left sidebar hidden the room pane is wide enough that its own legend now
	// draws `⌃D open DM`, so openDMHint no longer means "a reply is folded".
	if !strings.Contains(shown(a), roomFoldMark) {
		t.Fatalf("the reply is not folded to begin with:\n%s", shown(a))
	}

	a = fullClick(a, midOf(a.regions(), 0), rowOf(t, a, roomFoldMark))

	if !strings.Contains(shown(a), "CLICK_TAIL") {
		t.Errorf("a click on the folded reply did not expand it:\n%s", shown(a))
	}
	if strings.Contains(shown(a), roomFoldMark) {
		t.Errorf("the pointer survived the click:\n%s", shown(a))
	}
}

// A click that moves the keys to the room only focuses it - opening the reply
// under the pointer is a second click, once the room already holds the keys.
// This is clickedTool's refocused rule, which the room shares with a DM.
func TestAClickThatFocusesTheRoomDoesNotExpand(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a = pick(a, "s1").openRight("s1", "sydney").applyGeometry() // s1 holds the keys
	if a.focus != "s1" {
		t.Fatalf("focus is %q, want s1 to hold the keys", a.focus)
	}
	a = a.applyFrame(rpc.Frame{
		Kind: rpc.FrameEvent, SessionID: "s1",
		Event: &core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: longRoomReply("NOEXPAND_TAIL")},
	})

	a = fullClick(a, midOf(a.regions(), 0), rowOf(t, a, openDMHint))

	if a.focus != "" {
		t.Fatalf("the click did not focus the room (focus=%q)", a.focus)
	}
	// The room's pointer is still up, which is the proof it did not expand: only
	// a folded reply draws openDMHint, and s1's own DM - which shows this reply
	// in full - never draws it. So its presence is the room staying folded, not
	// the DM's copy of the same text.
	if !strings.Contains(shown(a), openDMHint) {
		t.Errorf("a click that only focused the room also expanded the folded reply:\n%s", shown(a))
	}
}

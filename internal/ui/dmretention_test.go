package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// A conversation keeps its newest dmRetentionEvents and reclaims the rest, so
// what a width change re-wraps is bounded - and what it keeps is exactly what a
// re-wrap would draw.

// pastTheCap is a conversation laid out at 80x30 that has taken more events
// than it keeps: prose turns with folded tool runs between them, and early on
// more absences than maxLastReadRules keeps anchors for, so the scrollback it
// reclaims holds rules a re-wrap would not draw. Long enough that every absence
// is in what it reclaims: a stale rule left in what it keeps is lastread.go's
// own documented surplus, not the reclaim's.
func pastTheCap(t testing.TB) DM {
	t.Helper()
	d := NewDM("s1", "alex").SetSize(80, 30)
	for i := 0; d.events.len() < dmRetentionEvents+5*chunkSize; i++ {
		if i%17 == 0 && i > 0 && i < 120 {
			d = d.Leave() // an absence: the next event draws a last-read rule
		}
		id := fmt.Sprintf("t%d", i)
		d = d.Append(prose(fmt.Sprintf("Turn %d. The handler reads the config before the context is checked.", i)))
		d = d.Append(bashCall(id)).Append(result(id, "ok", false))
		d = d.Append(readCall(id+"r", "main.go")).Append(result(id+"r", "12 lines", false))
	}
	if !d.reclaimed() {
		t.Fatalf("%d events and nothing reclaimed: every test on this conversation asserts nothing", d.events.len())
	}
	return d
}

// linesOf is a transcript's retained lines as text.
func linesOf(tr transcript) []string {
	return tr.lines.slice(tr.lines.first(), tr.lines.len())
}

func TestAConversationKeepsItsNewestEventsAndReclaimsTheRest(t *testing.T) {
	d := pastTheCap(t)
	if n := d.events.count(); n <= dmRetentionEvents-chunkSize || n >= dmRetentionEvents+chunkSize {
		t.Errorf("the conversation holds %d events, want within a chunk of %d", n, dmRetentionEvents)
	}
	if !strings.Contains(stripANSI(d.tr.prefix), dmReclaimedHistory) {
		t.Errorf("the scrollback has no reclaimed line above it: %q", stripANSI(d.tr.prefix))
	}
	text := stripANSI(strings.Join(linesOf(d.tr), "\n"))
	if strings.Contains(text, "Turn 0.") {
		t.Errorf("the oldest turn is still in the scrollback")
	}
	if first := d.events.at(d.events.first()); !strings.Contains(text, first.Text) {
		t.Errorf("the oldest kept event %q is not drawn", first.Text)
	}
}

// The incremental reclaim cuts where a re-wrap would begin: the lines kept are
// the lines a re-wrap of the kept events draws, rule for rule. The absences put
// rules in the scrollback that the cap has stopped anchoring, which a re-wrap
// would drop - the reclaim has to cut past them rather than short.
func TestAReclaimKeepsTheLinesAReWrapDraws(t *testing.T) {
	d := pastTheCap(t)
	got, want := linesOf(d.tr), linesOf(d.rewrapped())
	if !slices.Equal(got, want) {
		at := 0
		for at < min(len(got), len(want)) && got[at] == want[at] {
			at++
		}
		t.Fatalf("after reclaiming, the scrollback (%d lines) differs from a re-wrap of what it kept (%d lines) at line %d:\n got %q\nwant %q",
			len(got), len(want), at, got[min(at, len(got)-1)], want[min(at, len(want)-1)])
	}
}

// A reclaim cuts only where a block starts: never inside a folded run, never
// between a call and the result under it.
func TestAReclaimNeverCutsIntoAToolRun(t *testing.T) {
	d := pastTheCap(t)
	if first := d.events.at(d.events.first()); d.isToolBlock(first) {
		t.Errorf("the oldest kept event is a tool block (%v): the reclaim cut into a run", first.Kind)
	}
}

// The last-read anchors are event indices; the ones the reclaim took the events
// of go, and the rest still sit above an event the conversation holds.
func TestAReclaimDropsTheMarksItTookTheEventsOf(t *testing.T) {
	d := pastTheCap(t)
	for _, m := range d.marks {
		if m < d.events.first() {
			t.Errorf("mark %d is below the oldest kept event %d", m, d.events.first())
		}
	}
}

// History read off disk goes under what a conversation holds. Once the oldest
// of that has been reclaimed there is nowhere honest to put it, so it is left
// out rather than drawn above a gap.
func TestHistoryAfterAReclaimIsLeftOut(t *testing.T) {
	d := pastTheCap(t)
	before := d.events.count()
	if got := d.Before([]core.Event{prose("an old turn read off disk")}).events.count(); got != before {
		t.Errorf("history after a reclaim changed the conversation from %d to %d events", before, got)
	}
}

// The reclaimed line belongs to the conversation, not to a subagent opened in
// its pane, and comes back with the conversation.
func TestTheReclaimedLineIsTheConversationsOwn(t *testing.T) {
	d := pastTheCap(t).Viewing("d1")
	if d.tr.prefix != "" {
		t.Errorf("a subagent's transcript is drawn under the conversation's reclaimed line")
	}
	if d = d.Viewing(""); !strings.Contains(stripANSI(d.tr.prefix), dmReclaimedHistory) {
		t.Errorf("back in the conversation, the reclaimed line is gone")
	}
}

// A highlight on lines a reclaim took has nothing left to point at.
func TestASelectionOnReclaimedLinesIsCleared(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30)
	for i := range 40 {
		d = d.Append(prose(fmt.Sprintf("Turn %d.", i)))
	}
	a := App{dms: map[string]*DM{}}.withDM("s1", d)
	first := d.tr.lines.first()
	a.sel = selection{pane: "s1", anchor: point{line: first}, head: point{line: first + 1, col: 3}}
	reclaimed := d
	reclaimed.tr = reclaimed.tr.trimBefore(first + 2)
	if a = a.withDM("s1", reclaimed); !a.sel.empty() {
		t.Errorf("a selection on reclaimed lines survived")
	}
}

// BenchmarkReWrapAtTheRetentionCap records what one width change costs a
// conversation holding all it keeps: the figure dmRetentionEvents is set by. A
// record, not a gate - it is timing.
//
// Run: go test ./internal/ui -run XXX -bench ReWrapAtTheRetentionCap -benchtime 3x
func BenchmarkReWrapAtTheRetentionCap(b *testing.B) {
	d := benchTranscript(dmRetentionEvents, 100, 40)
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		d = d.SetSize(100+i%2, 40)
	}
}

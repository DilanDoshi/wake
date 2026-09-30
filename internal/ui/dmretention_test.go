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
	// Every reclaim here cuts exactly: they render what they reclaim, a chunk
	// each, where one that fell back would lay out all it kept.
	n := renderedDuring(t, func() { d = buildPastTheCap(d) })
	if n >= dmRetentionEvents {
		t.Fatalf("building the conversation rendered %d events: its reclaims fell back to laying out all they kept", n)
	}
	if !d.reclaimed() {
		t.Fatalf("%d events and nothing reclaimed: every test on this conversation asserts nothing", d.events.len())
	}
	return d
}

// buildPastTheCap appends pastTheCap's turns to d.
func buildPastTheCap(d DM) DM {
	for i := 0; d.events.len() < dmRetentionEvents+5*chunkSize; i++ {
		if i%17 == 0 && i > 0 && i < 120 {
			d = d.Leave() // an absence: the next event draws a last-read rule
		}
		id := fmt.Sprintf("t%d", i)
		d = d.Append(prose(fmt.Sprintf("Turn %d. The handler reads the config before the context is checked.", i)))
		d = d.Append(bashCall(id)).Append(result(id, "ok", false))
		d = d.Append(readCall(id+"r", "main.go")).Append(result(id+"r", "12 lines", false))
	}
	return d
}

// renderedDuring counts the events f sends back through glamour by way of the
// renderTranscript seam: a reclaim that cut exactly renders only what it
// reclaims, and one that fell back lays out everything it kept.
func renderedDuring(t testing.TB, f func()) int {
	t.Helper()
	was, n := renderTranscript, 0
	renderTranscript = func(d DM) []block { n += d.events.count(); return was(d) }
	defer func() { renderTranscript = was }()
	f()
	return n
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

// The edge of a reclaim is where rules pile up: absences at the seven events
// before the cut leave four rules the cap no longer anchors, two it does in what
// is reclaimed, and one on the cut itself, which the kept scrollback keeps. A
// count of rules cannot tell those apart there; the cut has to walk them.
func TestAReclaimCutsExactlyAmongRulesAtItsEdge(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30)
	for i := 0; i < dmRetentionEvents+chunkSize; i++ {
		if i >= chunkSize-6 && i <= chunkSize {
			d = d.Leave()
		}
		d = d.Append(prose(fmt.Sprintf("Turn %d.", i)))
	}
	if d.events.first() != chunkSize {
		t.Fatalf("the reclaim cut at event %d, want %d: the rules are not at its edge", d.events.first(), chunkSize)
	}
	got, want := linesOf(d.tr), linesOf(d.rewrapped())
	if !slices.Equal(got, want) {
		t.Fatalf("the kept scrollback (%d lines) is not a re-wrap of it (%d lines):\n got %q\nwant %q",
			len(got), len(want), got[:min(4, len(got))], want[:min(4, len(want))])
	}
}

// A reader scrolled back through a reclaimed conversation who opens a folded run
// stays where they were: the re-wrap keeps the scrollback's numbering, which the
// reader's place is held in.
func TestOpeningARunScrolledBackAfterAReclaimKeepsThePlace(t *testing.T) {
	d := pastTheCap(t).ScrollUp(200)
	line := -1
	for at := range d.tr.runs {
		if at >= d.tr.scroll && at < d.tr.scroll+d.tr.height {
			line = at
			break
		}
	}
	if line < 0 {
		t.Fatal("no folded run on screen to open")
	}
	opened, hit := d.openRun(line)
	if !hit {
		t.Fatalf("line %d did not open a run", line)
	}
	if opened.tr.scroll != d.tr.scroll || opened.tr.atBottom() {
		t.Errorf("opening a run moved the reader from line %d to %d (at bottom: %v)", d.tr.scroll, opened.tr.scroll, opened.tr.atBottom())
	}
}

// A result the fold leaves standing - a failed edit's, drawn joined under its
// call - is part of that call. The cut would land on it here, and has to step
// past it rather than keep a result whose call it reclaimed.
func TestAReclaimNeverOrphansAResult(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30)
	for i := 0; i < dmRetentionEvents+chunkSize+2; i++ {
		switch i {
		case chunkSize - 1:
			d = d.Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
				ID: "e1", Name: "Edit", Display: "auth.go", Diff: &core.ToolDiff{Old: "alpha", New: "ALPHA"},
			}})
		case chunkSize:
			d = d.Append(result("e1", "String to replace not found in file.", true))
		default:
			d = d.Append(prose(fmt.Sprintf("Turn %d.", i)))
		}
	}
	if !d.reclaimed() {
		t.Fatal("nothing was reclaimed: the cut is not being tested")
	}
	if first := d.events.at(d.events.first()); first.Tool != nil {
		t.Errorf("the oldest kept event belongs to a tool call (%v) whose other half was reclaimed", first.Kind)
	}
	if got, want := linesOf(d.tr), linesOf(d.rewrapped()); !slices.Equal(got, want) {
		t.Errorf("the kept scrollback is not a re-wrap of it: %d lines against %d", len(got), len(want))
	}
}

// A conversation keeps its bound while a subagent is open in its pane: its
// events are reclaimed behind the subagent's transcript, which is left alone, and
// coming back draws only what was kept.
func TestAConversationBehindASubagentIsBoundedToo(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30).Viewing("d1")
	behind := linesOf(d.tr)
	for i := 0; i < dmRetentionEvents+2*chunkSize; i++ {
		d = d.Append(prose(fmt.Sprintf("Turn %d.", i)))
	}
	if n := d.events.count(); n >= dmRetentionEvents+chunkSize {
		t.Errorf("behind a subagent the conversation grew to %d events", n)
	}
	if !slices.Equal(linesOf(d.tr), behind) {
		t.Errorf("reclaiming behind the subagent changed the subagent's transcript")
	}
	if d = d.Viewing(""); !strings.Contains(stripANSI(d.tr.prefix), dmReclaimedHistory) {
		t.Errorf("back in the conversation, no reclaimed line")
	}
}

// A highlight on the last reclaimed line is on the line the reclaimed marker
// now holds, and goes with the rest.
func TestASelectionOnTheLineTheMarkerTakesIsCleared(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30)
	for i := 0; i < dmRetentionEvents+chunkSize-1; i++ {
		d = d.Append(prose(fmt.Sprintf("Turn %d.", i)))
	}
	next := d.Append(prose("the event that reclaims"))
	marker := next.tr.lines.first() - 1
	a := App{dms: map[string]*DM{}}.withDM("s1", d)
	a.sel = selection{pane: "s1", anchor: point{line: marker}, head: point{line: marker, col: 4}}
	if a = a.withDM("s1", next); !a.sel.empty() {
		t.Errorf("a selection on line %d, now the reclaimed marker's, survived the reclaim", marker)
	}
}

// A checklist op is stored and draws nothing, so an absence before one and
// another after it put two rules side by side - which no count of rules can
// place. The cut walks the scrollback against a render of what it reclaims.
func TestAReclaimWalksRulesBesideAnEventThatDrawsNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   func(n int) bool // whether an absence-op-absence group goes in at event n
		away func(n int) bool // whether a plain absence goes in before event n
	}{
		{"rules side by side", func(n int) bool { return n >= chunkSize-16 && n < chunkSize-4 && n%4 == 0 }, func(int) bool { return false }},
		{"the newest rule on an op at the edge",
			func(n int) bool { return n == chunkSize-1 },
			func(n int) bool { return n >= chunkSize-40 && n < chunkSize-30 && n%2 == 0 }},
		// The rule on the cut itself, its anchor pushed out of the cap by three
		// absences after it: a re-wrap of the kept events does not draw it.
		{"an unanchored rule on the cut", func(int) bool { return false },
			func(n int) bool { return n >= chunkSize && n <= chunkSize+6 && n%2 == 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDM("s1", "alex").SetSize(80, 30)
			for d.events.len() < dmRetentionEvents+chunkSize-1 {
				n := d.events.len()
				if tc.away(n) {
					d = d.Leave()
				}
				if tc.op(n) {
					d = d.Leave().Append(taskCreateID(fmt.Sprintf("c%d", n), "a step", "doing it")).Leave()
					continue
				}
				d = d.Append(prose(fmt.Sprintf("Turn %d.", n)))
			}
			if n := renderedDuring(t, func() { d = d.Append(prose("the turn that reclaims")) }); n >= dmRetentionEvents {
				t.Errorf("the reclaim rendered %d events: it fell back to laying out all it kept rather than cutting exactly", n)
			}
			if d.events.first() != chunkSize {
				t.Fatalf("the reclaim cut at event %d, want %d: the rules are not at its edge", d.events.first(), chunkSize)
			}
			if got, want := linesOf(d.tr), linesOf(d.rewrapped()); !slices.Equal(got, want) {
				t.Fatalf("the kept scrollback (%d lines) is not a re-wrap of it (%d lines)", len(got), len(want))
			}
		})
	}
}

// A stream of nothing but tool calls has no prose to cut before, and is
// bounded all the same: the cut falls before a call, and what is kept is laid
// out again once, so it is a re-wrap by construction - and the run it cut into
// goes on folding into one line.
func TestAToolOnlyStreamIsBoundedToo(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30).Append(prose("Starting the sweep."))
	for i := 0; d.events.len() < dmRetentionEvents+3*chunkSize; i++ {
		id := fmt.Sprintf("b%d", i)
		d = d.Append(bashCall(id)).Append(result(id, "ok", false))
	}
	if n := d.events.count(); n >= dmRetentionEvents+chunkSize {
		t.Fatalf("a tool-only stream grew to %d events", n)
	}
	d = d.Append(bashCall("last")).Append(result("last", "ok", false))
	if got, want := linesOf(d.tr), linesOf(d.rewrapped()); !slices.Equal(got, want) {
		t.Errorf("after the cut into the run, the scrollback (%d lines) is not a re-wrap of it (%d lines)", len(got), len(want))
	}
}

// When a reclaim has to lay out again - a cut into a run - a reader scrolled back
// keeps their place, as far from the newest line as they were, and a highlight
// on lines that all moved goes.
func TestLayingOutAgainKeepsTheReadersPlace(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30).toggleExpanded().Append(prose("Starting the sweep."))
	for i := 0; d.events.count() < dmRetentionEvents+chunkSize-2; i++ {
		id := fmt.Sprintf("b%d", i)
		d = d.Append(bashCall(id)).Append(result(id, "ok", false))
	}
	d = d.ScrollUp(40)
	top := d.tr.lines.at(d.tr.scroll)
	a := App{dms: map[string]*DM{}}.withDM("s1", d)
	a.sel = selection{pane: "s1", anchor: point{line: d.tr.scroll}, head: point{line: d.tr.scroll + 1, col: 3}}
	for i, first := 0, d.events.first(); d.events.first() == first; i++ {
		id := fmt.Sprintf("n%d", i)
		d = d.Append(bashCall(id)).Append(result(id, "ok", false))
	}
	if d.tr.atBottom() || d.tr.lines.at(d.tr.scroll) != top {
		t.Errorf("laying out again moved the reader: top line %q, was %q (at bottom: %v)",
			stripANSI(d.tr.lines.at(d.tr.scroll)), stripANSI(top), d.tr.atBottom())
	}
	if a = a.withDM("s1", d); !a.sel.empty() {
		t.Errorf("a highlight survived a lay-out that moved every line under it")
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
	for i := 0; i < dmRetentionEvents+chunkSize-1; i++ {
		d = d.Append(prose(fmt.Sprintf("Turn %d.", i)))
	}
	a := App{dms: map[string]*DM{}}.withDM("s1", d)
	first := d.tr.lines.first()
	a.sel = selection{pane: "s1", anchor: point{line: first + 2}, head: point{line: first + 3, col: 3}}
	if a = a.withDM("s1", d.Append(prose("the event that reclaims"))); !a.sel.empty() {
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

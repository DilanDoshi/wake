package ui

import (
	"fmt"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// assertSaidMatchesTr is the subset-render invariant (spec §5): the canonical
// said lines, re-rendered through the SAME focus filter, must sum to exactly the
// rows the transcript holds. A desync between said (canonical, hidden lines at
// rows == 0) and tr (rendered) is the one real risk of the design, so it is
// asserted directly rather than through what happens to be on screen.
func assertSaidMatchesTr(t *testing.T, r Room, where string) {
	t.Helper()
	lines := r.said.slice(r.said.first(), r.said.len())
	want := 0
	for _, b := range renderRoom(r, lines) {
		want += len(b.laidOut)
	}
	if got := r.tr.lines.count(); want != got {
		t.Fatalf("%s: said/tr desync: re-rendered rows=%d, tr.lines.count()=%d (reclaimed=%v, said.count=%d)",
			where, want, got, r.reclaimed, r.said.count())
	}
}

// Reclaim while focused holds the invariant across both boundary kinds: a shown
// oldest line (rows > 0, the cut advances) and a hidden oldest line (rows == 0,
// the cut does not). The hidden branch skips rendering, so the flood is cheap.
func TestReclaimUnderFocusKeepsSaidAndTranscriptInSync(t *testing.T) {
	const john, iris, mgr = "john-id", "iris-id", "mgr-id"
	r := NewRoom().SetSize(80, 12).WithFocus(john, "john", mgr)

	shown := func(i int) {
		r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: john, Text: fmt.Sprintf("john %d", i)}, Agent{ID: john, Name: "john"})
	}
	hidden := func(i int) {
		r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: iris, Text: fmt.Sprintf("iris %d", i)}, Agent{ID: iris, Name: "iris"})
	}

	for i := 0; i < 40; i++ {
		shown(i)
	}
	assertSaidMatchesTr(t, r, "after shown prefix")
	for i := 0; i < roomRetentionEvents+100; i++ {
		hidden(i)
		if i%2500 == 0 {
			assertSaidMatchesTr(t, r, fmt.Sprintf("hidden i=%d reclaimed=%v", i, r.reclaimed))
		}
	}
	if !r.reclaimed {
		t.Fatal("precondition: expected reclaim to have fired past the cap")
	}
	assertSaidMatchesTr(t, r, "after hidden flood")

	// Interleave shown and hidden while over the cap, so reclaim evicts a mix.
	for i := 0; i < 500; i++ {
		if i%3 == 0 {
			shown(1000 + i)
		} else {
			hidden(1000 + i)
		}
		assertSaidMatchesTr(t, r, fmt.Sprintf("interleave i=%d", i))
	}

	if _ = r.View(80, 12); !r.tr.atBottom() {
		t.Fatal("a following reader was not kept at bottom after reclaim")
	}

	// Unfocus re-renders every canonical line; nothing was lost to reclaim's
	// interaction with the hidden entries.
	r2 := r.WithFocus("", "", mgr)
	assertSaidMatchesTr(t, r2, "after unfocus")
	if r2.said.count() != r.said.count() {
		t.Fatalf("unfocus changed the canonical line count: %d -> %d", r.said.count(), r2.said.count())
	}
}

// The same invariant in the unfocused room, so a failure above is attributable
// to the filter rather than to a pre-existing reclaim bug.
func TestReclaimUnfocusedKeepsSaidAndTranscriptInSync(t *testing.T) {
	r := NewRoom().SetSize(80, 12)
	for i := 0; i < roomRetentionEvents+100; i++ {
		r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s", Text: fmt.Sprintf("line %d", i)}, Agent{ID: "s", Name: "s"})
		if i%2500 == 0 {
			assertSaidMatchesTr(t, r, fmt.Sprintf("unfocused i=%d", i))
		}
	}
	assertSaidMatchesTr(t, r, "unfocused final")
}

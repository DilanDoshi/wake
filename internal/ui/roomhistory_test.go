package ui

// The room coming back with what was said, from claude's own transcripts.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// base is a fixed moment, so every case below reads as an ordering rather than
// as arithmetic against a clock.
var base = time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)

func heard(id, text string, at time.Time) core.Event {
	return core.Event{Kind: core.KindAssistantText, SessionID: id, Text: text, At: at}
}

func typed(id, text string, at time.Time) core.Event {
	return core.Event{Kind: core.KindUserText, SessionID: id, Text: text, At: at}
}

// named is the attribution the room uses, standing in for the live fleet.
func named(id string) Agent { return Agent{ID: id, Name: "agent-" + id} }

// A room is one conversation over many transcripts, so the first thing it has
// to get right is which of them happened first.
func TestARoomHistoryBatchIsFoldedInTimeOrder(t *testing.T) {
	lines := roomHistoryLines([]core.Event{
		heard("s2", "second thing", base.Add(2*time.Second)),
		heard("s1", "first thing", base.Add(1*time.Second)),
		heard("s1", "third thing", base.Add(3*time.Second)),
	}, base.Add(time.Hour), named)

	got := make([]string, 0, len(lines))
	for _, l := range lines {
		got = append(got, l.ev.Text)
	}
	want := []string{"first thing", "second thing", "third thing"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the batch folded as %v, want %v: two transcripts merged out of order are a conversation nobody had", got, want)
	}
	for _, l := range lines {
		if l.by.Name == "" {
			t.Errorf("a restored line is attributed to nobody: %q", l.ev.Text)
		}
	}
}

// The room draws what agents said. A line with no time cannot be placed among
// the others, and one at or after the ask may already have arrived live - so
// folding it would draw the same turn twice, which is worse than not drawing
// it at all.
func TestHistoryIsDroppedWhenItCouldAlreadyBeOnScreen(t *testing.T) {
	cutoff := base.Add(10 * time.Second)
	lines := roomHistoryLines([]core.Event{
		heard("s1", "before the ask", cutoff.Add(-time.Second)),
		heard("s1", "at the ask", cutoff),
		heard("s1", "after the ask", cutoff.Add(time.Second)),
		{Kind: core.KindAssistantText, SessionID: "s1", Text: "no time at all"},
	}, cutoff, named)

	if len(lines) != 1 || lines[0].ev.Text != "before the ask" {
		got := make([]string, 0, len(lines))
		for _, l := range lines {
			got = append(got, l.ev.Text)
		}
		t.Errorf("folded %v, want only [before the ask]", got)
	}
}

// restored is the room after each batch has been folded the way the daemon
// sends them: one transcript per call, because sendRoomHistory reads one file
// per ask. The broadcast rule lives in Room.Before for exactly this reason.
func restored(batches ...[]core.Event) Room {
	r := NewRoom().SetSize(80, 24)
	for _, b := range batches {
		r = r.Before(roomHistoryLines(b, base.Add(100*time.Hour), named))
	}
	return r
}

// openerText is the broadcast every fixture below uses to make a turn public.
//
// Prose is restored only inside a turn two transcripts prove was a broadcast,
// so a fixture that wants an agent's words in the room has to say who *else*
// received the message that prompted them. That is the contract rather than
// scaffolding - a test that forgets it is a test asserting the room stays empty.
const openerText = "@all carry on"

// opener is one transcript's copy of that broadcast.
func opener(id string, at time.Time) core.Event { return typed(id, openerText, at) }

// texts is what the room holds, in order.
func texts(r Room) []string {
	out := make([]string, 0, r.said.len())
	for _, l := range r.said.slice(0, r.said.len()) {
		out = append(out, l.ev.Text)
	}
	return out
}

// One broadcast is one thing you said, however many transcripts recorded it -
// and it arrives in one reply per transcript.
func TestABroadcastFoundInTwoTranscriptsIsOneRoomLine(t *testing.T) {
	r := restored(
		[]core.Event{typed("s1", "@all stop what you are doing", base)},
		[]core.Event{typed("s2", "@all stop what you are doing", base.Add(80*time.Millisecond))},
		[]core.Event{typed("s3", "@all stop what you are doing", base.Add(120*time.Millisecond))},
	)

	got := texts(r)
	if len(got) != 1 || got[0] != "@all stop what you are doing" {
		t.Fatalf("a broadcast to three agents came back as %v, want it once", got)
	}
	// Attributed to nobody, which is what the room does with a turn you typed.
	// Under one agent's name it would read as that agent quoting you.
	if by := r.said.slice(0, 1)[0].by; by.Name != "" {
		t.Errorf("the restored broadcast is attributed to %q, want the operator's own turn", by.Name)
	}
}

// A rewound broadcast never reaches this pipeline at all: internal/daemon's
// History() is tree-aware (internal/daemon/history.go) and drops a rewound
// turn before it is ever encoded into the FrameRoomHistoryReply this
// function's caller reads f.Events from - see roomHistoryArrived. So the
// batches below are shaped the way the daemon would actually deliver them
// for two agents that both received a broadcast, had it rewound, and were
// then given a different continuation: the rewound turn's text never
// appears in either batch, the same way it never appears in what History()
// hands back.
//
// This pins that the merge and the broadcast rule - the only things between
// a restored batch and the screen - do not need the rewound turn kept out of
// their own input a second time: given only what the daemon actually sends,
// they show the surviving continuation as one broadcast line and nothing of
// what was rewound away. A daemon-side regression that started handing this
// path an unfiltered transcript is caught in internal/daemon, not here; see
// TestTheRoomHistoryReplyDropsARewoundBroadcast.
func TestARoomRestoreOverAlreadyFilteredHistoryShowsThePostRewindBroadcast(t *testing.T) {
	r := restored(
		[]core.Event{
			opener("s1", base),
			heard("s1", "s1 carrying on", base.Add(time.Second)),
			// Stands in for the daemon's post-rewind reply: s1's transcript
			// was rewound past "@all abort the deploy" and its reply, so
			// History() never hands either back, and neither is written here.
			// What follows is the turn the operator sent after the rewind.
			typed("s1", "@all resume the deploy", base.Add(2*time.Second)),
			heard("s1", "s1 resuming", base.Add(3*time.Second)),
		},
		[]core.Event{
			opener("s2", base.Add(40*time.Millisecond)),
			heard("s2", "s2 carrying on", base.Add(time.Second+40*time.Millisecond)),
			typed("s2", "@all resume the deploy", base.Add(2*time.Second+40*time.Millisecond)),
			heard("s2", "s2 resuming", base.Add(3*time.Second+40*time.Millisecond)),
		},
	)

	got := texts(r)
	want := []string{
		openerText, "s1 carrying on", "s2 carrying on",
		"@all resume the deploy", "s1 resuming", "s2 resuming",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("the room restored %v, want %v", got, want)
	}
	for _, text := range got {
		if strings.Contains(text, "abort the deploy") {
			t.Errorf("the rewound broadcast reached the room: %q", text)
		}
	}

	// Both broadcasts are the operator's own turn, the way
	// TestABroadcastFoundInTwoTranscriptsIsOneRoomLine checks for one.
	for _, l := range r.said.slice(0, r.said.len()) {
		if (l.ev.Text == openerText || l.ev.Text == "@all resume the deploy") && l.by.Name != "" {
			t.Errorf("%q is attributed to %q, want the operator's own turn", l.ev.Text, l.by.Name)
		}
	}
}

// A turn in one transcript is indistinguishable from one typed privately into
// that conversation, and a DM is private. It errs toward silence.
func TestAUserTurnInOneTranscriptDoesNotReachTheRoom(t *testing.T) {
	r := restored(
		[]core.Event{
			opener("s1", base),
			typed("s1", "the thing I told sydney alone", base.Add(time.Second)),
			heard("s1", "understood", base.Add(2*time.Second)),
		},
		[]core.Event{
			opener("s2", base.Add(40*time.Millisecond)),
			heard("s2", "john said something else", base.Add(3*time.Second)),
		},
	)

	got := texts(r)
	for _, text := range got {
		if strings.Contains(text, "told sydney alone") || text == "understood" {
			t.Fatalf("a private turn or the reply to it reached the room: %v", got)
		}
	}
	want := []string{openerText, "john said something else"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the room restored %v, want %v", got, want)
	}
}

// Two private image sends to two agents inside the window are NOT a broadcast,
// even though every image decodes to the one placeholder text. Text multiplicity
// can never be sound proof for an image, so the room errs toward silence: an
// image restored here would leak a private send into the room, and for an
// image-only turn would open it and leak the agent's following prose too.
func TestTwoPrivateImagesAreNotAFalseBroadcast(t *testing.T) {
	r := restored(
		[]core.Event{typed("s1", core.ImagePlaceholder, base)},
		[]core.Event{typed("s2", core.ImagePlaceholder, base.Add(80*time.Millisecond))},
	)

	if got := texts(r); len(got) != 0 {
		t.Errorf("two private image sends within the window were promoted to a broadcast: %v", got)
	}
}

// The same words far enough apart are two people typing, not one broadcast.
func TestTheSameWordsOutsideTheWindowAreNotABroadcast(t *testing.T) {
	r := restored(
		[]core.Event{typed("s1", "run the tests", base)},
		[]core.Event{typed("s2", "run the tests", base.Add(time.Hour))},
	)

	if got := texts(r); len(got) != 0 {
		t.Errorf("two separate turns an hour apart came back as %v, want neither", got)
	}
}

// A fan-out slower than the window is two broadcasts rather than a chain of
// overlapping ones, which is what measuring each run from its own first copy
// buys. The pairwise version this replaced measured every copy against the
// candidate and could make all three a first.
func TestARunOfCopiesIsClusteredFromItsOwnFirst(t *testing.T) {
	r := restored(
		[]core.Event{typed("s1", "again", base)},
		[]core.Event{typed("s2", "again", base.Add(broadcastWindow/2))},
		[]core.Event{typed("s3", "again", base.Add(2*broadcastWindow))},
		[]core.Event{typed("s4", "again", base.Add(2*broadcastWindow+time.Second))},
	)

	if got := texts(r); len(got) != 2 {
		t.Errorf("two fan-outs an interval apart came back as %v, want one line each", got)
	}
}

// Restored history has no display cap of its own: it shares the room's combined
// cap with live events. Room.raw's separate backstop is not reached here.
func TestRoomHistoryBelowTheCombinedCapIsKeptAfterTheMerge(t *testing.T) {
	events := []core.Event{opener("s1", base)}
	const replies = 800
	for i := range replies {
		events = append(events, heard("s1", fmt.Sprintf("line %d", i), base.Add(time.Duration(i+1)*time.Second)))
	}
	r := restored(events, []core.Event{opener("s2", base.Add(40*time.Millisecond))})

	got := texts(r)
	if len(got) != replies+1 {
		t.Fatalf("the merge kept %d lines, want all %d below the combined cap", len(got), replies+1)
	}
	// The newest, which is where a returning reader wants to be.
	if want := fmt.Sprintf("line %d", replies-1); got[len(got)-1] != want {
		t.Errorf("last restored line is %q, want %q", got[len(got)-1], want)
	}
}

// --- Room.Before -----------------------------------------------------------

func TestBeforePutsAHistoryBatchAboveWhatIsAlreadyDrawn(t *testing.T) {
	r := NewRoom().SetSize(80, 20)
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "live line"}, named("s1"))
	r = r.Before(roomHistoryLines([]core.Event{opener("s2", base)}, base.Add(time.Hour), named))
	r = r.Before(roomHistoryLines([]core.Event{
		opener("s1", base.Add(40*time.Millisecond)),
		heard("s1", "older line", base.Add(time.Second)),
	}, base.Add(time.Hour), named))

	view := stripANSI(r.View(80, 20))
	older, live := strings.Index(view, "older line"), strings.Index(view, "live line")
	if older < 0 || live < 0 {
		t.Fatalf("a line went missing:\n%s", view)
	}
	if older > live {
		t.Errorf("the restored line is drawn under the live one:\n%s", view)
	}
}

// A resume brings a session back after the room has been open, and its history
// belongs among the history that is already there rather than under whatever
// was said in the meantime.
func TestASecondBatchMergesIntoTheHistoryRatherThanUnderTheLiveLines(t *testing.T) {
	r := NewRoom().SetSize(80, 20)
	r = r.Before(roomHistoryLines([]core.Event{
		opener("s1", base),
		heard("s1", "oldest", base.Add(time.Second)),
		heard("s1", "newest history", base.Add(10*time.Second)),
	}, base.Add(time.Hour), named))
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "live line"}, named("s1"))

	// The batch that arrives late both makes the first one's turn public and
	// belongs among it, which is the case this test is about.
	r = r.Before(roomHistoryLines([]core.Event{
		opener("s2", base.Add(40*time.Millisecond)),
		heard("s2", "in between", base.Add(5*time.Second)),
	}, base.Add(time.Hour), named))

	view := stripANSI(r.View(80, 20))
	order := []string{"oldest", "in between", "newest history", "live line"}
	at := -1
	for _, want := range order {
		i := strings.Index(view, want)
		if i < 0 {
			t.Fatalf("%q is not on screen:\n%s", want, view)
		}
		if i < at {
			t.Fatalf("the room reads out of order - %q comes too late:\n%s", want, view)
		}
		at = i
	}
}

func TestBeforeWithNothingInItLeavesTheRoomAlone(t *testing.T) {
	r := NewRoom().SetSize(80, 20)
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "live line"}, named("s1"))
	before := stripANSI(r.View(80, 20))

	if got := stripANSI(r.Before(nil).View(80, 20)); got != before {
		t.Errorf("an empty batch changed the room:\n%s", got)
	}
}

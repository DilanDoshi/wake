package ui

// What the restored room may show, and the turn is the unit.
//
// Restoring line by line hid the question and showed the answer: a turn typed
// privately into a conversation pane was dropped by the broadcast rule, and the
// agent's reply to it - the same conversation, in the agent's words - went into
// the group chat anyway. Found by an adversarial review.
//
// So provenance is carried across a whole turn: an agent's prose is restored
// only while the user turn it belongs to is one two transcripts prove was a
// broadcast. Everything else is a conversation Wake cannot prove was public.

import (
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

func TestAReplyToAPrivateTurnDoesNotReachTheRoom(t *testing.T) {
	r := restored(
		[]core.Event{
			typed("s1", "what is the api key for staging", base),
			heard("s1", "it is in the vault under staging/api", base.Add(time.Second)),
		},
		[]core.Event{heard("s2", "john was asked something else", base.Add(2*time.Second))},
	)

	for _, text := range texts(r) {
		if strings.Contains(text, "vault under staging") {
			t.Fatalf("the reply to a turn Wake cannot prove was public reached the room: %v", texts(r))
		}
	}
	if got := texts(r); len(got) != 0 {
		t.Errorf("the room restored %v, want nothing it cannot prove was said in the open", got)
	}
}

func TestAReplyToABroadcastReachesTheRoom(t *testing.T) {
	r := restored(
		[]core.Event{
			typed("s1", "@all what are you on", base),
			heard("s1", "sydney is on the parser", base.Add(time.Second)),
		},
		[]core.Event{
			typed("s2", "@all what are you on", base.Add(60*time.Millisecond)),
			heard("s2", "john is on the tests", base.Add(2*time.Second)),
		},
	)

	got := texts(r)
	want := []string{"@all what are you on", "sydney is on the parser", "john is on the tests"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the room restored %v, want %v: a broadcast is public and so is every answer to it", got, want)
	}
}

// The 400-event tail usually opens in the middle of a conversation, so the
// first lines have no initiator in the window at all. They are dropped, and
// that is where most of a transcript's prose is - keeping them would reopen the
// leak exactly where it is widest.
func TestProseBeforeAnyUserTurnIsNotRestored(t *testing.T) {
	r := restored([]core.Event{
		heard("s1", "carrying on from something", base),
		heard("s1", "and finishing it", base.Add(time.Second)),
	})

	if got := texts(r); len(got) != 0 {
		t.Errorf("the room restored %v, want nothing: no turn in the window says these were public", got)
	}
}

// A turn ends where the next one starts, so a private turn closes a public one.
func TestAPrivateTurnClosesTheBroadcastBeforeIt(t *testing.T) {
	r := restored(
		[]core.Event{
			typed("s1", "@all status", base),
			heard("s1", "sydney is fine", base.Add(time.Second)),
			typed("s1", "now do the private thing", base.Add(2*time.Second)),
			heard("s1", "the private thing is done", base.Add(3*time.Second)),
		},
		[]core.Event{typed("s2", "@all status", base.Add(50*time.Millisecond))},
	)

	got := texts(r)
	for _, text := range got {
		if strings.Contains(text, "private thing") {
			t.Fatalf("a private turn after a broadcast stayed public: %v", got)
		}
	}
	if len(got) != 2 || got[1] != "sydney is fine" {
		t.Errorf("the room restored %v, want the broadcast and the answer to it", got)
	}
}

// The same words typed privately and then broadcast, inside the window.
//
// One broadcast writes once per transcript, so a session appearing **twice** in
// a cluster means two separate sends and there is no way to tell which of them
// was the public one. Counting distinct senders alone made the cluster public,
// which promoted the *private* copy - the earliest - to the line the room draws
// and opened its turn, so the reply to the private message was restored.
//
// Found by an adversarial review, on the pass that added the turn rule.
func TestASessionAppearingTwiceInAClusterProvesNothing(t *testing.T) {
	r := restored(
		[]core.Event{
			typed("s1", "status", base),
			heard("s1", "the private answer", base.Add(time.Second)),
			typed("s1", "status", base.Add(2*time.Second)),
			heard("s1", "the public answer", base.Add(3*time.Second)),
		},
		[]core.Event{typed("s2", "status", base.Add(2*time.Second+40*time.Millisecond))},
	)

	for _, text := range texts(r) {
		if text == "the private answer" {
			t.Fatalf("the reply to a private turn was restored because the same words were broadcast later: %v", texts(r))
		}
	}
	if got := texts(r); len(got) != 0 {
		t.Errorf("the room restored %v; an ambiguous cluster proves nothing and none of it should come back", got)
	}
}

// One broadcast opens a turn in every transcript that received it, so a long
// turn still keeps that opener while it remains within Room.raw's backstop.
func TestALongTurnKeepsTheBroadcastThatOpenedIt(t *testing.T) {
	events := []core.Event{typed("s1", "@all keep going", base)}
	for i := range roomRawEvents / 2 {
		events = append(events, heard("s1", "line", base.Add(time.Duration(i+1)*time.Second)))
	}
	r := restored(events, []core.Event{typed("s2", "@all keep going", base.Add(40*time.Millisecond))})

	if got := texts(r); len(got) == 0 {
		t.Error("a long turn inside the raw backstop restored nothing: the opener that made it public was lost")
	}
}

// Production mutation caught: a plain 1,600-event tail slice evicts t=0 from
// the public t=0/t+4 run, re-anchors at t+4, promotes the private t+8 singleton,
// and restores the private reply that follows it.
func TestRawBackstopRolloverDoesNotReanchorAPrivateTurnIntoAPublicRun(t *testing.T) {
	const (
		text         = "same restored words"
		privateReply = "private reply must stay private"
	)
	earlier := []roomLine{
		{ev: typed("s1", text, base)},
		{ev: typed("s2", text, base.Add(4*time.Second))},
		{ev: typed("s3", text, base.Add(8*time.Second))},
		{ev: heard("s3", privateReply, base.Add(9*time.Second))},
	}
	for i := range roomRawEvents - 3 {
		earlier = append(earlier, roomLine{ev: core.Event{
			Kind:      core.KindTurnEnd,
			SessionID: "private-noise",
			MessageID: "tail",
			At:        base.Add(time.Duration(i+10) * time.Second),
		}})
	}

	r := NewRoom().Before(earlier)
	if len(r.raw) > roomRawEvents {
		t.Fatalf("Room.raw retained %d events, want at most %d", len(r.raw), roomRawEvents)
	}
	for _, got := range texts(r) {
		if got == privateReply {
			t.Fatalf("raw rollover promoted a private reply into the room: %v", texts(r))
		}
	}
}

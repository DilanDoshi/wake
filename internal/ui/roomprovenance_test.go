package ui

// A turn typed in the room carries a uuid the restore can recognise on disk:
// the provenance deferred.md said nothing recorded, in a field the model never
// reads. Recorded accepted and persisted by claude 2.1.285
// (testdata/input/room-stamped-uuid.stdin.jsonl, testdata/transcript/room-stamped-uuid.jsonl).

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
)

func TestARoomMessageIDIsAUUIDTheRestoreRecognises(t *testing.T) {
	send := newRoomSend(true)
	a, b := send.messageID(), send.messageID()
	for _, id := range []string{a, b} {
		u, err := uuid.Parse(id)
		if err != nil {
			t.Fatalf("%q is not a uuid claude will take: %v", id, err)
		}
		if u.Variant() != uuid.RFC4122 {
			t.Errorf("%q has variant %v", id, u.Variant())
		}
		if _, direct, ok := roomSendOf(id); !ok || !direct {
			t.Errorf("%q is not recognised as a direct room send", id)
		}
	}
	if a == b {
		t.Error("two targets of one send got the same uuid; a fork's copy would read as a broadcast")
	}
	sa, _, _ := roomSendOf(a)
	sb, _, _ := roomSendOf(b)
	other, _, _ := roomSendOf(newRoomSend(true).messageID())
	if sa != sb || sa == other {
		t.Errorf("the send a uuid carries is not what groups one send's targets (same send %v, other send %v)", sa == sb, sa == other)
	}
	if _, direct, _ := roomSendOf(newRoomSend(false).messageID()); direct {
		t.Error("an undirected send reads as direct")
	}
	if _, _, ok := roomSendOf(uuid.NewString()); ok {
		t.Error("a random uuid - a DM send's, or claude's own - reads as a room send")
	}
}

// The version this build mints is the one recorded accepted. A CLI that insists
// on another rejects the stdin line and ends the agent (the stdin trap), so a
// change here is a re-recording, not a constant edit.
func TestTheRoomMessageVersionIsTheOneRecordedAccepted(t *testing.T) {
	line, err := os.ReadFile("../../testdata/input/room-stamped-uuid.stdin.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(line, &f); err != nil {
		t.Fatal(err)
	}
	recorded := uuid.MustParse(f.UUID).Version()
	minted := uuid.MustParse(newRoomSend(false).messageID()).Version()
	if minted != recorded {
		t.Errorf("room sends are minted as version %d; claude was recorded accepting version %d", minted, recorded)
	}
}

// The live halves: a room send stamps every target with one send's uuids, and a
// DM send stamps an ordinary one.
func TestARoomSendIsMarkedAndADMSendIsNot(t *testing.T) {
	a := newRoomApp(t).withAgents("john", "sydney").withSize(200, 40)
	m, cmd := typeAndSubmit(a, "@all ship it")
	frames := sentFrames(t, m.(App), cmd)
	if len(frames) != 2 {
		t.Fatalf("a broadcast to two agents wrote %d frames", len(frames))
	}
	s0, direct, ok0 := roomSendOf(frames[0].MessageID)
	s1, _, ok1 := roomSendOf(frames[1].MessageID)
	if !ok0 || !ok1 || s0 != s1 || direct {
		t.Errorf("a broadcast's targets are not one undirected room send: %q %q", frames[0].MessageID, frames[1].MessageID)
	}

	m, cmd = typeAndSubmit(newRoomApp(t).withAgents("john", "sydney").withSize(200, 40), "@john just you")
	if _, direct, ok := roomSendOf(sentFrame(t, m.(App), cmd).MessageID); !ok || !direct {
		t.Error("a lone direct @name is not marked a direct room send")
	}

	dm := idleDM(t)
	m, cmd = typeAndSubmit(dm, "private words")
	if _, _, ok := roomSendOf(sentFrame(t, m.(App), cmd).MessageID); ok {
		t.Error("a DM send is marked as typed in the room")
	}
}

// roomTyped is a turn restored from a transcript record a room send stamped.
func roomTyped(id, text string, at time.Time, send roomSend) core.Event {
	return record(typed(id, text, at), send.messageID())
}

// A room message to one agent comes back, as the room drew it - `@john ...`,
// in john's thread - and so does the reply it opened.
//
// Mutation check: have roomSendOf refuse every id and both are dropped again.
func TestARoomMessageToOneAgentComesBack(t *testing.T) {
	send := newRoomSend(true)
	r := restored([]core.Event{
		roomTyped("s1", "fix the build", base, send),
		heard("s1", "fixed, it was the linker", base.Add(time.Second)),
	})
	got := strings.Join(texts(r), "|")
	if got != "@agent-s1 fix the build|fixed, it was the linker" {
		t.Fatalf("the room restored %q, want the direct message and its reply", got)
	}
	if to := r.said.slice(0, 1)[0].to; to != "s1" {
		t.Errorf("the restored direct message is addressed to %q, want s1's thread", to)
	}
}

// An undirected room send that reached one agent - the manager, as the default
// addressee - comes back as the room drew it: unaddressed.
func TestAnUndirectedRoomSendToOneAgentComesBackUnaddressed(t *testing.T) {
	r := restored([]core.Event{roomTyped("s1", "status please", base, newRoomSend(false))})
	got := r.said.slice(0, r.said.len())
	if len(got) != 1 || got[0].ev.Text != "status please" || got[0].to != "" {
		t.Errorf("an undirected room send came back as %+v, want the text unaddressed", got)
	}
}

// One send in two transcripts is still one broadcast, drawn once - the send the
// uuids carry, not the text, decides it.
func TestARoomSendInTwoTranscriptsIsOneBroadcast(t *testing.T) {
	send := newRoomSend(false)
	r := restored(
		[]core.Event{roomTyped("s1", "@all stop", base, send)},
		[]core.Event{roomTyped("s2", "@all stop", base.Add(80*time.Millisecond), send)},
	)
	got := r.said.slice(0, r.said.len())
	if len(got) != 1 || got[0].ev.Text != "@all stop" || got[0].to != "" {
		t.Errorf("one room send to two agents came back as %v, want it once, unaddressed", texts(r))
	}
}

// The old repeat-sender leak, now decided by provenance: a private DM `status`
// and a room broadcast `status` seconds later. The broadcast comes back once,
// and the private turn and its reply do not.
func TestAPrivateTurnBesideARoomBroadcastOfTheSameWordsStaysPrivate(t *testing.T) {
	send := newRoomSend(false)
	r := restored(
		[]core.Event{
			record(typed("s1", "status", base), uuid.NewString()),
			heard("s1", "private status answer", base.Add(time.Second)),
			roomTyped("s1", "status", base.Add(2*time.Second), send),
			heard("s1", "public status answer", base.Add(3*time.Second)),
		},
		[]core.Event{roomTyped("s2", "status", base.Add(2*time.Second+40*time.Millisecond), send)},
	)
	got := strings.Join(texts(r), "|")
	if got != "status|public status answer" {
		t.Errorf("the room restored %q, want the broadcast once and only its reply", got)
	}
}

// Two room messages of the same words to one agent are two things said.
func TestTwoRoomSendsOfTheSameWordsAreTwoLines(t *testing.T) {
	r := restored([]core.Event{
		roomTyped("s1", "again", base, newRoomSend(true)),
		roomTyped("s1", "again", base.Add(time.Second), newRoomSend(true)),
	})
	if n := len(texts(r)); n != 2 {
		t.Errorf("two room sends came back as %d lines: %v", n, texts(r))
	}
}

// A room send with an image is one record of two blocks - the image first, the
// text last (EncodeUserMessage) - so both events carry its uuid. The text is
// the turn; the image is dropped from a restore the way multiplicity drops it.
func TestAMarkedRoomSendWithAnImageComesBackWithItsText(t *testing.T) {
	id := newRoomSend(true).messageID()
	r := restored([]core.Event{
		record(typed("s1", core.ImagePlaceholder, base), id),
		record(typed("s1", "what is wrong with this chart?", base), id),
		heard("s1", "the y axis is logarithmic", base.Add(time.Second)),
	})
	got := strings.Join(texts(r), "|")
	if got != "@agent-s1 what is wrong with this chart?|the y axis is logarithmic" {
		t.Errorf("a marked send with an image restored %q, want its text and its reply", got)
	}
}

// An image sent to @john with no caption is a record of one block, the image:
// it is the turn, and it opens the reply.
func TestAMarkedImageOnlyRoomSendComesBack(t *testing.T) {
	r := restored([]core.Event{
		record(typed("s1", core.ImagePlaceholder, base), newRoomSend(true).messageID()),
		heard("s1", "that is the staging dashboard", base.Add(time.Second)),
	})
	got := strings.Join(texts(r), "|")
	if got != "@agent-s1 "+core.ImagePlaceholder+"|that is the staging dashboard" {
		t.Errorf("a captionless image sent to one agent restored %q, want the image and its reply", got)
	}
}

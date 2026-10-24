package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// A room send to a team is marked as one, so a restore can address it: the
// live echo is addressed to the members, and a restart must not turn it into a
// broadcast that draws in every thread.
func TestARoomSendToATeamIsMarkedAsOne(t *testing.T) {
	a := teamRoom(t)
	for draft, want := range map[string]bool{"@backend ship it": true, "@sydney ship it": false, "@all ship it": false} {
		m, cmd := typeAndSubmit(a, draft)
		for _, f := range sentFrames(t, m.(App), cmd) {
			if _, _, ok := roomSendOf(f.MessageID); !ok || roomSendTeam(f.MessageID) != want {
				t.Errorf("%q: a target's uuid is marked a team send = %v, want %v", draft, roomSendTeam(f.MessageID), want)
			}
		}
	}
}

func TestARestoredTeamSendIsAddressedToTheMembersThatHoldIt(t *testing.T) {
	send := newRoomSend(false).ForTeam()
	r := restored(
		[]core.Event{roomTyped("s1", "run the migrations", base, send), heard("s1", "sydney migrated", base.Add(time.Second))},
		[]core.Event{roomTyped("s2", "run the migrations", base.Add(80*time.Millisecond), send), heard("s2", "thea migrated", base.Add(time.Second+80*time.Millisecond))},
	)
	var sent []roomLine
	for _, l := range r.said.slice(0, r.said.len()) {
		if l.ev.Kind == core.KindUserText {
			sent = append(sent, l)
		}
	}
	if len(sent) != 1 || sent[0].to != joinIDs([]string{"s1", "s2"}) {
		t.Fatalf("the restored team send is %+v, want one line addressed to its members", sent)
	}
	for _, c := range []struct {
		focus, name string
		seen        bool
	}{
		{joinIDs([]string{"s1", "s2"}), "backend", true},
		{"s1", "agent-s1", true},
		{"s3", "agent-s3", false},
		{joinIDs([]string{"s3", "s4"}), "frontend", false},
	} {
		got := stripANSI(r.WithFocus(c.focus, c.name).View(80, 24))
		if seen := strings.Contains(got, "run the migrations"); seen != c.seen {
			t.Errorf("under @%s the restored team send is drawn = %v, want %v:\n%s", c.name, seen, c.seen, got)
		}
	}
}

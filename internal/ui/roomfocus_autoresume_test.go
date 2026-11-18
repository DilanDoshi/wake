package ui

// The daemon's resume line has no addressee but is no broadcast: a room narrowed
// to an agent, or to a team's channel, draws only its own agents' resumes, and the
// operator's own broadcast, which has no addressee either, still draws.

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// resumeTail is the words a resume line ends in; reconnected is the resume an
// unreachable API called for.
func resumeTail(reconnected bool) string {
	if reconnected {
		return autoReconnectedTail
	}
	return autoResumedTail
}

// daemonResume is the room's line for the daemon resuming session id, as the
// fleet stamps it (the announcement itself names the session only on its frame).
func daemonResume(id, name string, reconnected bool) roomLine {
	flags := rpc.MessageResumed
	if reconnected {
		flags |= rpc.MessageReconnected
	}
	ev := core.Event{
		Kind: core.KindUserText, Notice: core.NoticeAutoResumed, SessionID: id,
		Text: "continue", MessageID: rpc.NewMessageSend(flags).ID(),
	}
	return roomLine{ev: ev, by: Agent{ID: id, Name: name}}
}

func TestFocusAdmitsADaemonResume(t *testing.T) {
	const john, iris, mgr = "john-id", "iris-id", "mgr-id"
	team := joinIDs([]string{john, iris})
	cases := []struct {
		name  string
		focus string
		want  bool
	}{
		{"unfocused", "", true},
		{"its own agent", john, true},
		{"another agent", iris, false},
		{"the manager", mgr, false},
		{"a team holding its agent", team, true},
		{"a team holding another agent", joinIDs([]string{iris, mgr}), false},
	}
	for _, reconnected := range []bool{false, true} {
		line := daemonResume(john, "john", reconnected)
		for _, c := range cases {
			name := c.name
			if reconnected {
				name += ", reconnected"
			}
			t.Run(name, func(t *testing.T) {
				if got := focusAdmits(line, c.focus); got != c.want {
					t.Fatalf("focusAdmits(john's resume, %q) = %v, want %v", c.focus, got, c.want)
				}
			})
		}
	}
}

// The operator's broadcast has no addressee either, and a restored one carries
// the session id of whichever transcript held it: the fix must tell Wake's resume
// from it by what it is, never by an empty `to` or a session id.
func TestFocusStillAdmitsABroadcastBesideAResume(t *testing.T) {
	const john, iris = "john-id", "iris-id"
	broadcast := func(id string) roomLine {
		return roomLine{ev: core.Event{Kind: core.KindUserText, SessionID: id, Text: "ship it"}}
	}
	for _, id := range []string{"", iris, john} {
		if !focusAdmits(broadcast(id), john) {
			t.Errorf("a broadcast carrying session %q is hidden under @john", id)
		}
	}
}

// Live: the room draws a line as it arrives, narrowed already or narrowed after.
func TestANarrowedRoomDrawsOnlyItsOwnAgentsResume(t *testing.T) {
	const alpha, bravo = "a-id", "b-id"
	for _, reconnected := range []bool{false, true} {
		tail := resumeTail(reconnected)
		lines := []roomLine{daemonResume(alpha, "alpha", reconnected), daemonResume(bravo, "bravo", reconnected)}
		for name, r := range map[string]Room{
			"narrowed after":  appendAll(NewRoom().SetSize(80, 24), lines).WithFocus(bravo, "bravo"),
			"narrowed before": appendAll(NewRoom().SetSize(80, 24).WithFocus(bravo, "bravo"), lines),
		} {
			got := r.View(80, 24)
			if !strings.Contains(got, "bravo"+tail) {
				t.Errorf("%s: the room narrowed to bravo dropped bravo's own resume:\n%s", name, got)
			}
			if strings.Contains(got, "alpha"+tail) {
				t.Errorf("%s: the room narrowed to bravo drew alpha's resume:\n%s", name, got)
			}
			if all := r.WithFocus("", "").View(80, 24); !strings.Contains(all, "alpha"+tail) {
				t.Errorf("%s: widening the room lost alpha's resume:\n%s", name, all)
			}
		}
	}
}

// Restored history goes through the same filter, so a reopened room narrowed to
// one agent shows none of the others' resumes.
func TestANarrowedRestoredRoomDrawsOnlyItsOwnAgentsResume(t *testing.T) {
	r := restored(
		[]core.Event{resumeEvent("s1", base)},
		[]core.Event{resumeEvent("s2", base.Add(40*time.Millisecond))},
	)
	got := r.WithFocus("s2", "agent-s2").View(80, 24)
	if !strings.Contains(got, "agent-s2"+autoResumedTail) {
		t.Errorf("the restored room narrowed to s2 dropped its own resume:\n%s", got)
	}
	if strings.Contains(got, "agent-s1"+autoResumedTail) {
		t.Errorf("the restored room narrowed to s2 drew s1's resume:\n%s", got)
	}
}

// A team's channel is the same predicate over its members' ids.
func TestAChannelDrawsOnlyItsMembersResumes(t *testing.T) {
	member, outsider := daemonResume("s1", "sydney", false), daemonResume("s3", "delta", false)
	r := appendAll(NewRoom().SetSize(120, 40).WithChannel("backend", joinIDs([]string{"s1", "s2"})), []roomLine{member, outsider})
	got := r.View(120, 40)
	if !strings.Contains(got, "sydney"+autoResumedTail) {
		t.Errorf("the backend channel dropped its member's resume:\n%s", got)
	}
	if strings.Contains(got, "delta"+autoResumedTail) {
		t.Errorf("the backend channel drew a frontend agent's resume:\n%s", got)
	}
}

// Through the App, as the daemon's frames arrive - the announcement names its
// session only on the frame, so the fleet's stamp is what the narrowing reads -
// with the narrowing a typed @name makes and its clearing.
func TestTypingAtNameHidesOtherAgentsResumeLines(t *testing.T) {
	wire := func(id string, flags byte) rpc.Frame {
		f := resumeFrame(id)
		f.Event.SessionID = ""
		f.Event.MessageID = rpc.NewMessageSend(flags).ID()
		return f
	}
	for _, reconnected := range []bool{false, true} {
		flags, name := rpc.MessageResumed, "usage limit"
		if reconnected {
			flags, name = flags|rpc.MessageReconnected, "connection"
		}
		tail := resumeTail(reconnected)
		frames := []rpc.Frame{wire("s1", flags), wire("s2", flags)}
		base := newRoomApp(t).withSize(200, 40).withAgents("john", "iris")

		after := base
		for _, f := range frames {
			after = after.applyFrame(f)
		}
		after = after.withDraft("@john ")
		before := base.withDraft("@john ")
		for _, f := range frames {
			before = before.applyFrame(f)
		}

		for order, a := range map[string]App{"typed after": after, "typed before": before} {
			view := shown(a)
			if !strings.Contains(view, "john"+tail) {
				t.Errorf("%s, %s: the room narrowed to @john dropped john's resume:\n%s", name, order, view)
			}
			if strings.Contains(view, "iris"+tail) {
				t.Errorf("%s, %s: the room narrowed to @john drew iris's resume:\n%s", name, order, view)
			}
			if cleared, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc}); !strings.Contains(shown(cleared), "iris"+tail) {
				t.Errorf("%s, %s: clearing the draft did not bring iris's resume back", name, order)
			}
		}
	}
}

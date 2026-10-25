package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// withFleetCalled is a report naming the fleet, the way every push names it.
func (a App) withFleetCalled(name string) App {
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, Fleet: name}})
}

// /wake-name asks the daemon, from the room or any conversation, for the name as
// typed, a spaced one hyphenated as /name's is.
func TestWakeNameAsksTheDaemonToRenameTheFleet(t *testing.T) {
	for _, tc := range []struct {
		name, draft, want string
		room              bool
	}{
		{name: "from a conversation", draft: "/wake-name dev", want: "dev"},
		{name: "from the room", draft: "/wake-name dev", want: "dev", room: true},
		{name: "a spaced name", draft: "/wake-name wake  dev", want: "wake-dev"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh(t)
			conn, sent := pipeClient(t)
			a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
			if tc.room {
				a = a.showRoom()
			}

			_, cmd := typeAndSubmit(a, tc.draft)
			go func() { _ = runCmdQuietly(cmd) }()
			f := awaitFrame(t, sent)
			if f.Kind != rpc.FrameRenameFleet || f.Text != tc.want {
				t.Errorf("%q wrote %q %q, want %q %q", tc.draft, f.Kind, f.Text, rpc.FrameRenameFleet, tc.want)
			}
		})
	}
}

// Bare, it says what the fleet is called and asks for nothing.
func TestABareWakeNameSaysWhatTheFleetIsCalled(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(200, 40).withFleetCalled("atlas")
	m, cmd := typeAndSubmit(a, "/wake-name")
	if cmd != nil {
		t.Fatalf("a bare /wake-name was acted on: %+v", sentFrames(t, m.(App), cmd))
	}
	if got := shown(m); !strings.Contains(got, "this fleet is atlas") {
		t.Errorf("a bare /wake-name did not name the fleet:\n%s", got)
	}
}

// Every window says so when the report names another fleet - the asking one
// and any other - and only then: not on the first report, not on an unchanged one.
func TestEveryWindowSaysSoWhenTheFleetIsRenamed(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(200, 40).withFleetCalled("atlas")
	if got := shown(a); strings.Contains(got, "is now") {
		t.Fatalf("the first report announced a rename:\n%s", got)
	}
	a = a.withFleetCalled("dev")
	if got := shown(a); !strings.Contains(got, "fleet atlas is now dev") || !strings.Contains(got, "wake --fleet dev") {
		t.Errorf("a renamed fleet was not announced with its way back:\n%s", got)
	}
	fresh(t)
	if got := shown(a.withFleetCalled("dev")); strings.Contains(got, "is now") {
		t.Errorf("an unchanged report announced a rename again:\n%s", got)
	}
}

// A window opened on a fleet already knows its name from the seed, so a rename
// before the first push is still a rename.
func TestTheSeedNamesTheFleet(t *testing.T) {
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, &rpc.Status{Running: true, Fleet: "atlas"}).withSize(200, 40)
	if got := shown(a.withFleetCalled("dev")); !strings.Contains(got, "fleet atlas is now dev") {
		t.Errorf("a rename after the seed was not announced:\n%s", got)
	}
}

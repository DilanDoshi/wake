package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/notice"
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

// ⌃Q's notice row names the fleet by the name the last report gave it.
func TestCtrlQsNoticeNamesTheFleetItReopens(t *testing.T) {
	for _, tc := range []struct{ fleet, want string }{
		{fleet: "dev", want: "`wake --fleet dev` reopens the room"},
		{fleet: "", want: "`wake` reopens the room"},
	} {
		fresh(t)
		conn, _ := pipeClient(t)
		a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30).withFleetCalled("atlas").withFleetCalled(tc.fleet)
		if _, _, handled := confirmQuit(t, a); !handled {
			t.Fatal("the confirming ⌃Q was not taken by App.key")
		}
		if n, ok := notice.Latest(); !ok || !strings.Contains(n.Text, tc.want) {
			t.Errorf("on fleet %q, ⌃Q said %q, want it to contain %q", tc.fleet, n.Text, tc.want)
		}
	}
}

// A reattach that failed names a way back into this fleet: `wake attach` with no
// fleet looks in the unnamed one.
func TestAFailedReattachNamesTheFleet(t *testing.T) {
	for _, tc := range []struct {
		fleet, session, want string
	}{
		{fleet: "dev", session: "s1", want: "⌃O, then `wake attach s1 --fleet dev`"},
		{fleet: "dev", want: "⌃O, then `wake --fleet dev`"},
		{fleet: "", session: "s1", want: "⌃O, then `wake attach s1`"},
	} {
		fresh(t)
		d := &stubDialer{err: errors.New("refused")}
		a := NewRoomApp(newRecorder(t), Stream{}, &rpc.Status{Running: true, Fleet: tc.fleet}).withSize(200, 40).
			WithOpenDM(tc.session, "alex").WithDialer(d.dial)
		_, cmd := a.hungUp(errors.New("hung up"))
		if cmd == nil {
			t.Fatal("a hang-up with a dialer wired did not try to reattach")
		}
		msg, ok := cmd().(errMsg)
		if !ok || !strings.Contains(msg.Err.Error(), tc.want) {
			t.Errorf("on fleet %q a failed reattach said %v, want it to contain %q", tc.fleet, msg.Err, tc.want)
		}
	}
}

// cmd/wake's exit line reads the name the last report gave, a rename's included.
func TestTheFleetNameFollowsTheReport(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(200, 40).withFleetCalled("atlas").withFleetCalled("dev")
	if got := a.FleetName(); got != "dev" {
		t.Errorf("after a rename to dev the model names its fleet %q", got)
	}
}

// The emergency exit is handed each name the fleet is reported under - the seed's
// and a rename's - since the loop that folds them is what it runs without.
func TestEveryNameTheFleetIsReportedUnderIsHandedOn(t *testing.T) {
	fresh(t)
	var got []string
	a := NewRoomApp(newRecorder(t), Stream{}, &rpc.Status{Running: true, Fleet: "atlas"}).withSize(200, 40).
		WithFleetNamed(func(name string) { got = append(got, name) })
	_ = a.withFleetCalled("atlas").withFleetCalled("dev")
	if want := []string{"atlas", "dev"}; !slices.Equal(got, want) {
		t.Errorf("the names handed on were %q, want %q: the seed's, then the rename, once each", got, want)
	}
}

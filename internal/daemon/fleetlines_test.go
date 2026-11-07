package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A command the daemon puts in a sentence is typed in a shell, where a bare verb
// reaches the unnamed fleet: so each names the fleet that wrote it, as #214's
// way-back lines do, and only a socket set by hand gets the bare verb.

// onEachFleet runs check against a server on a named fleet, the unnamed one and a
// socket set by hand, with the flag every command it names must end in.
func onEachFleet(t *testing.T, check func(t *testing.T, s *server, flag string)) {
	t.Helper()
	t.Setenv("HOME", tempHome(t))
	t.Setenv("WAKE_PROJECTS", t.TempDir())
	root, err := StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	fleetsUnder(t, root, "dev")
	for _, tc := range []struct{ what, socket, flag string }{
		{"a named fleet", filepath.Join(fleetPath(root, "dev"), socketFileName), " --fleet dev"},
		{"the unnamed fleet", filepath.Join(root, socketFileName), " --fleet default"},
		{"a socket set by hand", tempSocket(t), ""},
	} {
		t.Run(tc.what, func(t *testing.T) { check(t, newServer(tc.socket), tc.flag) })
	}
}

// says fails unless why names each command whole, closed by its backtick so a
// bare verb cannot pass as the start of a flagged one.
func says(t *testing.T, door, why, flag string, commands ...string) {
	t.Helper()
	for _, cmd := range commands {
		if want := "`" + cmd + flag + "`"; !strings.Contains(why, want) {
			t.Errorf("%s says %q, want it to name %s", door, why, want)
		}
	}
	if flag != "" && strings.Contains(why, "`wake stop`") {
		t.Errorf("%s says %q: a bare `wake stop` typed for this fleet ends the unnamed one", door, why)
	}
}

// queueClient is a client with a queue and no connection: what the server says
// to it stays readable.
func queueClient() *client {
	return &client{out: make(chan rpc.Frame, clientQueue), closing: make(chan struct{})}
}

// refusalTo is the error the server queued for id, or a failure when it queued none.
func refusalTo(t *testing.T, c *client, id string) string {
	t.Helper()
	for {
		select {
		case f := <-c.out:
			if f.Kind == rpc.FrameError && f.SessionID == id {
				return f.Text
			}
		default:
			t.Fatalf("nothing refused %s", id)
			return ""
		}
	}
}

// atCap is s with its one session admitted under a cap of one.
func atCap(t *testing.T, s *server) {
	t.Helper()
	s.maxAgents.Store(1)
	if why := s.admitLive(liveAgent(idAlpha, "sydney", t.TempDir()), nil, false); why != "" {
		t.Fatalf("the first session under a cap of 1 was refused: %s", why)
	}
}

// A spawn past the cap is told `wake stop` ends the fleet, at both doors: the
// one a spawn frame passes and the last one, which holds two racing spawns.
func TestTheCapRefusalStopsTheFleetThatRefused(t *testing.T) {
	onEachFleet(t, func(t *testing.T, s *server, flag string) {
		atCap(t, s)
		late := liveAgent(idBeta, "alex", t.TempDir())
		says(t, "the last door", s.admitLive(late, nil, false), flag, "wake stop")

		c := queueClient()
		if s.maySpawn(context.Background(), c, rpc.Frame{SessionID: idGamma}) {
			t.Fatal("a spawn past the cap was allowed")
		}
		says(t, "maySpawn", refusalTo(t, c, idGamma), flag, "wake stop")
	})
}

// An import of a session already here offers the verbs that open and branch it,
// on this fleet.
func TestAnImportOfAHeldSessionOffersThisFleetsVerbs(t *testing.T) {
	onEachFleet(t, func(t *testing.T, s *server, flag string) {
		id := testSessionID("e55e")
		if !s.register(liveAgent(id, "alex", t.TempDir())) {
			t.Fatal("could not put the session in the fleet")
		}
		says(t, "an import of a live session", s.heldRefusal(id), flag,
			"wake attach "+short(id), "wake fork "+short(id))
	})
}

// A parked one's refusal leads with /resume, and the fork it warns of is this
// fleet's.
func TestAnImportOfAParkedSessionWarnsOfThisFleetsFork(t *testing.T) {
	onEachFleet(t, func(t *testing.T, s *server, flag string) {
		id := testSessionID("f66f")
		a := liveAgent(id, "alex", t.TempDir())
		a.finish(nil)
		a.markParked()
		if !s.register(a) {
			t.Fatal("could not put the session in the fleet")
		}
		says(t, "an import of a parked session", s.heldRefusal(id), flag, "wake fork "+short(id))
	})
}

// A second import of a source already imported opens the import on this fleet.
func TestASecondImportOpensTheImportOnThisFleet(t *testing.T) {
	onEachFleet(t, func(t *testing.T, s *server, flag string) {
		source, id := testSessionID("a77a"), testSessionID("b88b")
		imported := newAgent(id, "sydney", "label", t.TempDir(), source,
			core.NewSession(core.Config{SessionID: id}), func() {})
		if !s.register(imported) {
			t.Fatal("could not put the import in the fleet")
		}
		says(t, "a second import", s.explainHeldSource(source, errNotExplained).Error(), flag, "wake attach sydney")
	})
}

// After a /wake-name the stop names the fleet's new name: the old one forwards
// only while this daemon lives.
func TestTheCapRefusalFollowsARenamedFleet(t *testing.T) {
	s, _ := namedServer(t, "atlas")
	if err := s.moveFleetTo("dev"); err != nil {
		t.Fatal(err)
	}
	atCap(t, s)
	says(t, "the last door", s.admitLive(liveAgent(idBeta, "alex", t.TempDir()), nil, false), " --fleet dev", "wake stop")
}

// A name somebody typed that a live session already wears is refused with the
// command that opens that session on this fleet, at every door a typed name
// comes through: spawn, fork and import.
func TestATakenNameOpensItsHolderOnThisFleet(t *testing.T) {
	onEachFleet(t, func(t *testing.T, s *server, flag string) {
		if _, err := s.names.claim("alex"); err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()

		spawned, c := testSessionID("c99c"), queueClient()
		s.spawn(ctx, c, rpc.Frame{SessionID: spawned, Text: "alex", Dir: t.TempDir()})
		says(t, "a spawn", refusalTo(t, c, spawned), flag, "wake attach alex")

		parent := testSessionID("d00d")
		p := liveAgent(parent, "sydney", t.TempDir())
		p.finish(nil)
		p.markParked()
		if !s.register(p) {
			t.Fatal("could not put the fork's parent in the fleet")
		}
		forked, c := testSessionID("e11e"), queueClient()
		s.fork(ctx, c, rpc.Frame{SessionID: forked, ParentID: parent, Text: "alex"})
		says(t, "a fork", refusalTo(t, c, forked), flag, "wake attach alex")

		source, real := testSessionID("f22f"), t.TempDir()
		writeTranscript(t, ProjectsDir(), slugOf(real), source, real)
		imported, c := testSessionID("a33a"), queueClient()
		s.importSession(ctx, c, rpc.Frame{SessionID: imported, ParentID: source, Text: "alex"})
		says(t, "an import", refusalTo(t, c, imported), flag, "wake attach alex")
	})
}

// An orphaned parent never reaches forkRefusal from a daemon - only FleetOnDisk,
// a client reading a dead daemon's roster, reports one - so its sentence names no
// command that could be typed at the wrong fleet.
func TestAnOrphanedParentsRefusalNamesNoCommand(t *testing.T) {
	why := forkRefusal(rpc.SessionStatus{ID: idAlpha, Name: "alex", State: rpc.StateOrphaned})
	if strings.Contains(why, "`wake") {
		t.Errorf("the orphaned parent's refusal is %q, which names a `wake` command with no fleet", why)
	}
}

// errNotExplained is a refusal explainHeldSource has to replace.
var errNotExplained = errors.New("a process is still running under the session")

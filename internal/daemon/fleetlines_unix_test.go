//go:build unix

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

// onEachFleet runs check against a server on a named fleet, one renamed by
// /wake-name, the unnamed one and a socket set by hand, with the flag every
// command it names must end in. Only ps is on PATH: a regression past a refusal
// must fail to launch rather than start the operator's claude.
func onEachFleet(t *testing.T, check func(t *testing.T, s *server, flag string)) {
	t.Helper()
	onlyPsOnPath(t)
	t.Setenv("HOME", tempHome(t))
	root, err := StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	fleetsUnder(t, root, "dev", "atlas")
	on := func(socket string) func(*testing.T) *server {
		return func(*testing.T) *server { return newServer(socket) }
	}
	for _, tc := range []struct {
		what, flag string
		server     func(*testing.T) *server
	}{
		{"a named fleet", " --fleet dev", on(filepath.Join(fleetPath(root, "dev"), socketFileName))},
		{"a renamed fleet", " --fleet ops", func(t *testing.T) *server {
			s := newServer(filepath.Join(fleetPath(root, "atlas"), socketFileName))
			if err := s.moveFleetTo("ops"); err != nil {
				t.Fatal(err)
			}
			return s
		}},
		{"the unnamed fleet", " --fleet default", on(filepath.Join(root, socketFileName))},
		{"a socket set by hand", "", on(tempSocket(t))},
	} {
		t.Run(tc.what, func(t *testing.T) {
			t.Setenv("WAKE_PROJECTS", t.TempDir())
			check(t, tc.server(t), tc.flag)
		})
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

		c := newClient(nil)
		if s.maySpawn(context.Background(), c, rpc.Frame{SessionID: idGamma}) {
			t.Fatal("a spawn past the cap was allowed")
		}
		says(t, "maySpawn", firstRefusal(t, c).Text, flag, "wake stop")
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

// A name somebody typed that a live session already wears is refused with the
// command that opens that session on this fleet, at every door a typed name
// comes through: spawn, fork and import.
func TestATakenNameOpensItsHolderOnThisFleet(t *testing.T) {
	onEachFleet(t, func(t *testing.T, s *server, flag string) {
		if _, err := s.names.claim("alex"); err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()

		c := newClient(nil)
		s.spawn(ctx, c, rpc.Frame{SessionID: testSessionID("c99c"), Text: "alex", Dir: t.TempDir()})
		says(t, "a spawn", firstRefusal(t, c).Text, flag, "wake attach alex")

		parent := testSessionID("d00d")
		p := liveAgent(parent, "sydney", t.TempDir())
		p.finish(nil)
		p.markParked()
		if !s.register(p) {
			t.Fatal("could not put the fork's parent in the fleet")
		}
		c = newClient(nil)
		s.fork(ctx, c, rpc.Frame{SessionID: testSessionID("e11e"), ParentID: parent, Text: "alex"})
		says(t, "a fork", firstRefusal(t, c).Text, flag, "wake attach alex")

		source, real := testSessionID("f22f"), t.TempDir()
		writeTranscript(t, ProjectsDir(), slugOf(real), source, real)
		c = newClient(nil)
		s.importSession(ctx, c, rpc.Frame{SessionID: testSessionID("a33a"), ParentID: source, Text: "alex"})
		says(t, "an import", firstRefusal(t, c).Text, flag, "wake attach alex")
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

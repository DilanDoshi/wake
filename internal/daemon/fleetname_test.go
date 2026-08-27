package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A fresh fleet gets the first free name, and the same one every time until
// there are two.
//
// First-free rather than random on purpose: a fleet is a directory somebody
// comes back to by typing its name, so `harbor` being what a fresh Wake is
// called is a thing that can be learned.
func TestAFreshFleetTakesTheFirstFreeName(t *testing.T) {
	root := testRoot(t)

	first, err := nextFleetName(root)
	if err != nil {
		t.Fatalf("nextFleetName: %v", err)
	}
	if first != fleetPool[0] {
		t.Errorf("a fresh machine named its fleet %q, want %q", first, fleetPool[0])
	}

	// Nothing was created, so asking again gives the same answer. A name is
	// claimed by the directory existing, not by having been suggested.
	again, err := nextFleetName(root)
	if err != nil {
		t.Fatalf("nextFleetName: %v", err)
	}
	if again != first {
		t.Errorf("naming twice without creating anything gave %q then %q", first, again)
	}
}

// Names are not reused while a fleet holds them, and the pool running out is
// not a failure.
func TestFleetNamesAreNotReusedAndThePoolRunsPastItsEnd(t *testing.T) {
	root := testRoot(t)
	fleets := filepath.Join(root, fleetsDirName)

	for _, name := range fleetPool {
		if err := os.MkdirAll(filepath.Join(fleets, name), stateDirPerm); err != nil {
			t.Fatal(err)
		}
	}
	got, err := nextFleetName(root)
	if err != nil {
		t.Fatalf("nextFleetName with a full pool: %v", err)
	}
	if want := fleetPool[0] + "-2"; got != want {
		t.Errorf("with every word taken the next fleet is %q, want %q: running out of names must not "+
			"stop somebody starting a fleet", got, want)
	}
	if err := os.MkdirAll(filepath.Join(fleets, got), stateDirPerm); err != nil {
		t.Fatal(err)
	}
	next, err := nextFleetName(root)
	if err != nil {
		t.Fatalf("nextFleetName: %v", err)
	}
	if next == got {
		t.Errorf("nextFleetName gave %q twice", got)
	}
}

// `default` reaches the unnamed fleet and cannot be the name of a fleet.
//
// Every fleet that existed before fleets did is the unnamed one - so without
// this word, the first run of this build leaves somebody's whole Wake reachable
// only by setting $WAKE_SOCKET by hand. Reserving it is what stops
// `--fleet default` becoming ambiguous the moment somebody makes a fleet called
// that.
func TestTheReservedWordReachesTheUnnamedFleetAndCannotBeAFleet(t *testing.T) {
	root := testRoot(t)

	dir, err := fleetDirFor(root, LegacyFleet)
	if err != nil {
		t.Fatalf("fleetDirFor(%s): %v", LegacyFleet, err)
	}
	if dir != root {
		t.Errorf("--fleet %s resolved to %q, want the unnamed fleet at %q", LegacyFleet, dir, root)
	}
	if err := checkFleetName(LegacyFleet); err == nil {
		t.Errorf("a fleet may be called %q, so `--fleet %s` is ambiguous the moment somebody makes one",
			LegacyFleet, LegacyFleet)
	}
}

// The unnamed fleet is listed when it is real and not when it is not.
//
// A listing that always carried it would advertise a fleet with nothing in it,
// which somebody would open, find empty, and wonder what they had lost.
func TestTheUnnamedFleetIsListedOnlyWhenItHasSomethingInIt(t *testing.T) {
	root := testRoot(t)
	if err := os.MkdirAll(root, stateDirPerm); err != nil {
		t.Fatal(err)
	}

	if legacyFleetExists(root) {
		t.Error("an empty ~/.wake counts as a fleet")
	}
	// A park book is enough: it is what /resume reads, so a fleet with one and
	// no socket is a *stopped* fleet rather than no fleet.
	if err := os.WriteFile(filepath.Join(root, parkBookName), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !legacyFleetExists(root) {
		t.Error("a ~/.wake holding a park book does not count as a fleet, so the sessions in it are " +
			"reachable by no word at all")
	}
}

// The pool is names a person can type, and holds no duplicates.
func TestTheFleetPoolIsUsable(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range fleetPool {
		if seen[name] {
			t.Errorf("%q is in the fleet pool twice, so one of the names can never be handed out", name)
		}
		seen[name] = true
		if err := checkFleetName(name); err != nil {
			t.Errorf("the pool offers %q, which the name rule refuses: %v", name, err)
		}
		if strings.Contains(name, "-") {
			t.Errorf("%q carries a dash, which collides with the -2 suffix a full pool appends", name)
		}
	}
	if len(fleetPool) < 8 {
		t.Errorf("the pool holds %d names; too few and a machine hits the numbered fallback for "+
			"ordinary use", len(fleetPool))
	}
}

// A fleet name and a session name must not be drawn from one pool.
//
// A fleet called `alex` beside an agent called `alex` is two different things
// wearing one word, on the surface whose whole job is telling them apart.
func TestFleetNamesAndAgentNamesShareNoWords(t *testing.T) {
	agents := map[string]bool{}
	for _, n := range namePool {
		agents[n] = true
	}
	for _, n := range fleetPool {
		if agents[n] {
			t.Errorf("%q names both a fleet and an agent: the roster and the fleet listing would "+
				"disagree about what that word means", n)
		}
	}
}

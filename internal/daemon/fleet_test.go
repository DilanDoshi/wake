package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// testRoot is a ~/.wake for one test, short enough that a fleet socket under it
// can be bound.
//
// **It does not touch $HOME**, and that is the point rather than a convenience.
// This package's tests start real daemons; those daemons read $HOME on their own
// goroutines; a test that moved it underneath them was a data race the detector
// found intermittently - three fleet tests failing under `make ci` and passing
// alone. fleetDirIn and fleetsIn take a root so nothing has to.
//
// /tmp rather than t.TempDir() because darwin's is ~80 characters before
// `.wake/fleets/<name>/daemon.sock` adds 31 more, past the 103-byte sun_path
// limit checkSocketPath enforces. That is a fact about the test environment and
// not about a real home, where `/.wake/fleets/backend/daemon.sock` adds 33 to
// a short one and clears the limit easily.
func testRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "wkf")
	if err != nil {
		t.Fatalf("temporary state root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, stateDirName)
}

// A fleet name may not reach outside ~/.wake, and this is the guard that says
// so rather than a comment claiming it.
//
// The name becomes a directory, and the directory holds a socket, a roster, a
// lock and a park book naming session ids. A name containing a separator writes
// all of that wherever the caller likes - so `--fleet ../../tmp/x` has to be a
// refusal and not a path.
func TestAFleetNameCannotEscapeTheStateDirectory(t *testing.T) {
	root := testRoot(t)
	for _, name := range []string{
		"../escape",
		"../../tmp/x",
		"a/b",
		"/absolute",
		"..",
		".",
		"~",
		"$HOME",
		"with space",
		"with\nnewline",
		"-leading-dash",
		strings.Repeat("x", maxFleetName+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fleetDirIn(root, name); err == nil {
				t.Errorf("fleet name %q was accepted: a name is one path segment, and this one is a "+
					"way to write a socket and a park book outside ~/%s", name, stateDirName)
			}
		})
	}
}

// A named fleet is a directory under the fleets root; the default one is not.
//
// The default keeps the path it has always had, so every park book and roster
// already on disk is still the default fleet's afterwards. Moving it would lose
// every fleet on every machine, since all of them are currently that one.
func TestANamedFleetLivesUnderTheFleetsDirectoryAndTheDefaultDoesNot(t *testing.T) {
	root := testRoot(t)

	got, err := fleetDirIn(root, "backend")
	if err != nil {
		t.Fatalf("fleetDirIn(backend): %v", err)
	}
	if want := filepath.Join(root, fleetsDirName, "backend"); got != want {
		t.Errorf("fleetDirIn(backend) = %q, want %q", got, want)
	}

	def, err := fleetDirIn(root, DefaultFleet)
	if err != nil {
		t.Fatalf("fleetDirIn(default): %v", err)
	}
	if def != root {
		t.Errorf("the default fleet is at %q, want %q: it must keep the path it has always had", def, root)
	}
}

// Two fleets share nothing, which is the whole feature.
//
// Asserted on the paths every piece of per-fleet state derives from: the roster,
// park book, lock and log are all `filepath.Dir(socket)` plus a name, so two
// sockets in two directories is isolation by construction rather than by
// enforcement.
func TestTwoFleetsShareNoneOfTheirState(t *testing.T) {
	root := testRoot(t)

	a, err := fleetDirIn(root, "alpha")
	if err != nil {
		t.Fatalf("alpha: %v", err)
	}
	b, err := fleetDirIn(root, "beta")
	if err != nil {
		t.Fatalf("beta: %v", err)
	}
	sockA, sockB := filepath.Join(a, socketFileName), filepath.Join(b, socketFileName)

	for _, pair := range []struct {
		what string
		a, b string
	}{
		{"socket", sockA, sockB},
		{"roster", rosterPath(sockA), rosterPath(sockB)},
		{"park book", parkBookPath(sockA), parkBookPath(sockB)},
		{"lock", lockPath(sockA), lockPath(sockB)},
	} {
		if pair.a == pair.b {
			t.Errorf("two fleets share one %s (%q): they would see each other's agents", pair.what, pair.a)
		}
	}
}

// Fleets lists what is there, skips what is not a fleet, and is not an error
// when there are none.
func TestFleetsListsTheNamedOnes(t *testing.T) {
	root := testRoot(t)

	// None yet, which is an answer rather than a failure.
	got, err := fleetsIn(root)
	if err != nil {
		t.Fatalf("fleetsIn on a machine with none: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("fleetsIn = %v on a fresh machine", got)
	}

	fleets := filepath.Join(root, fleetsDirName)
	for _, name := range []string{"web", "api", "backend"} {
		if err := os.MkdirAll(filepath.Join(fleets, name), stateDirPerm); err != nil {
			t.Fatal(err)
		}
	}
	// A file rather than a directory, and a directory whose name would be
	// refused on the way in: neither is a fleet, and both are reachable because
	// this directory is on disk where somebody can edit it.
	if err := os.WriteFile(filepath.Join(fleets, "notafleet"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(fleets, "-bad"), stateDirPerm); err != nil {
		t.Fatal(err)
	}

	got, err = fleetsIn(root)
	if err != nil {
		t.Fatalf("fleetsIn: %v", err)
	}
	want := []string{"api", "backend", "web"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("fleetsIn = %v, want %v (sorted, directories only, and only names that would be "+
			"accepted on the way in)", got, want)
	}
}

// $WAKE_SOCKET and a fleet name are refused together rather than one winning.
//
// EnsureRunning forks the daemon and dials it, and the two agree by both reading
// $WAKE_SOCKET - so it has to keep winning. A caller that sets it *and* names a
// fleet has said two different things, and picking one silently is how somebody
// looks at the wrong fleet's agents while certain they asked for the other.
func TestASocketAndAFleetTogetherAreRefused(t *testing.T) {
	t.Setenv(SocketEnv, filepath.Join(t.TempDir(), "s.sock"))

	if _, err := FleetSocketPath("backend"); err == nil {
		t.Error("a fleet name was accepted alongside $WAKE_SOCKET, so one of the two was ignored")
	}
}

// Two fleets really are two, driven through real daemons rather than paths.
//
// The path tests above say *why* isolation holds; this says *that* it does. Both
// are here because the first would keep passing if Serve ever grew a global - a
// package-level roster, a shared name registry - and this one would not.
//
// The sockets are built by hand rather than through FleetSocketPath so this test
// needs no $HOME either; what it is about is two daemons on two sockets, and
// where those sockets came from is the path tests' subject.
func TestTwoFleetsRunSideBySideAndCannotSeeEachOther(t *testing.T) {
	fakeClaudeOnPath(t, "")
	root := testRoot(t)

	alpha := filepath.Join(root, fleetsDirName, "alpha", socketFileName)
	beta := filepath.Join(root, fleetsDirName, "beta", socketFileName)
	for _, sock := range []string{alpha, beta} {
		if err := os.MkdirAll(filepath.Dir(sock), stateDirPerm); err != nil {
			t.Fatal(err)
		}
	}

	startDaemonOn(t, alpha)
	startDaemonOn(t, beta)

	a := attach(t, alpha)
	b := attach(t, beta)

	a.spawn(idAlpha, "one")
	a.awaitState(idAlpha, rpc.StateIdle)
	b.spawn(idBeta, "two")
	b.awaitState(idBeta, rpc.StateIdle)

	if got := stateOf(a.status(), idBeta); got != "" {
		t.Errorf("the alpha fleet can see beta's session as %q: two fleets share no roster, so this "+
			"is a global somewhere", got)
	}
	if got := stateOf(b.status(), idAlpha); got != "" {
		t.Errorf("the beta fleet can see alpha's session as %q", got)
	}
	if n := len(live(a.status())); n != 1 {
		t.Errorf("the alpha fleet holds %d live sessions, want its own 1", n)
	}

	// And the same display name is free in both, because the name registry is
	// per daemon. Two fleets fighting over one 64-name pool would be one fleet
	// with extra steps.
	b.spawn(idGamma, "one")
	if got := stateOf(b.status(), idGamma); got == "" {
		t.Error(`the name "one" was refused in the beta fleet because the alpha fleet is using it`)
	}
}

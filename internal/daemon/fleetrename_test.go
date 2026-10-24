package daemon

// /wake-name: a running fleet's rename moves its directory under ~/.wake/fleets
// and leaves a link at the old name for every process still holding a path under
// it (fleetrename.go).

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// fleetsUnder makes named fleets' directories under root's fleets, each holding
// the park book a daemon keeps beside its socket, and returns root.
func fleetsUnder(t *testing.T, root string, names ...string) string {
	t.Helper()
	for _, name := range names {
		dir := fleetPath(root, name)
		if err := os.MkdirAll(dir, stateDirPerm); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, parkBookName), []byte("[]"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// aFleetRoot is fleetsUnder a fresh ~/.wake short enough to bind a socket in.
func aFleetRoot(t *testing.T, names ...string) string {
	t.Helper()
	return fleetsUnder(t, filepath.Join(tempHome(t), stateDirName), names...)
}

func mustMoveFleet(t *testing.T, root, from, to string) {
	t.Helper()
	if err := moveFleet(root, from, to); err != nil {
		t.Fatalf("rename fleet %s to %s: %v", from, to, err)
	}
}

// forwardsTo is where the link at a fleet's old name points, or why it is none.
func forwardsTo(root, name string) (string, error) { return os.Readlink(fleetPath(root, name)) }

func TestRenamingAFleetMovesItsDirectoryAndLeavesAForwardingLink(t *testing.T) {
	root := aFleetRoot(t, "atlas")
	mustMoveFleet(t, root, "atlas", "dev")

	if _, err := os.Stat(filepath.Join(fleetPath(root, "dev"), parkBookName)); err != nil {
		t.Errorf("the park book did not move with the fleet: %v", err)
	}
	if to, err := forwardsTo(root, "atlas"); err != nil || to != "dev" {
		t.Errorf("the old name is %q (%v), want a link to dev", to, err)
	}
	// What an agent's WAKE_SOCKET still names: a path under the old name.
	if _, err := os.Stat(filepath.Join(fleetPath(root, "atlas"), parkBookName)); err != nil {
		t.Errorf("a path under the old name no longer reaches the fleet: %v", err)
	}
	if got, err := fleetsIn(root); err != nil || !slices.Equal(got, []string{"dev"}) {
		t.Errorf("`wake fleets` would list %v (%v), want only dev", got, err)
	}
}

// A refused rename names its reason and moves nothing.
func TestAFleetRenameRefusesWhatIsNotAFreeName(t *testing.T) {
	for _, tc := range []struct{ name, to, says string }{
		{"nothing", "", "not usable"},
		{"the unnamed fleet's word", LegacyFleet, "reserved"},
		{"a path", "../x", "not usable"},
		{"a flag", "-x", "not usable"},
		{"too long", strings.Repeat("a", maxFleetName+1), "limit"},
		{"its own name", "atlas", "already called"},
		{"another fleet", "mesa", "already exists"},
		{"another fleet's old name", "cove", "forwards"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := aFleetRoot(t, "atlas", "mesa")
			// cove is mesa's old name, while mesa's daemon still runs.
			if err := os.Symlink("mesa", fleetPath(root, "cove")); err != nil {
				t.Fatal(err)
			}
			err := moveFleet(root, "atlas", tc.to)
			if err == nil || !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("renaming atlas to %q answered %v, want a refusal saying %q", tc.to, err, tc.says)
			}
			if fi, err := os.Lstat(fleetPath(root, "atlas")); err != nil || !fi.IsDir() {
				t.Errorf("a refused rename moved atlas: %v", err)
			}
			if to, err := forwardsTo(root, "cove"); err != nil || to != "mesa" {
				t.Errorf("a refused rename changed cove's link to %q (%v)", to, err)
			}
		})
	}
}

// The name is a directory a socket is bound under, so its length is the
// socket path's too.
func TestAFleetRenameRefusesANameNoSocketCouldBeBoundUnder(t *testing.T) {
	root := fleetsUnder(t, filepath.Join(t.TempDir(), stateDirName), "atlas")
	long := strings.Repeat("a", maxFleetName)
	if len(filepath.Join(fleetPath(root, long), socketFileName)) <= maxSocketPath {
		t.Skip("this machine's temporary directory is too short to build an unbindable path")
	}
	if err := moveFleet(root, "atlas", long); err == nil || !strings.Contains(err.Error(), "socket path") {
		t.Errorf("a rename past sun_path answered %v, want the socket path's refusal", err)
	}
}

// A name that forwards here is this fleet's own, so a rename may take it back -
// an undo - and every old name still reaches the directory.
func TestARenameMayTakeBackANameThatForwardsHere(t *testing.T) {
	root := aFleetRoot(t, "atlas")
	mustMoveFleet(t, root, "atlas", "dev")
	mustMoveFleet(t, root, "dev", "prod")
	mustMoveFleet(t, root, "prod", "atlas")

	if fi, err := os.Lstat(fleetPath(root, "atlas")); err != nil || !fi.IsDir() {
		t.Fatalf("atlas is not the fleet's directory again: %v", err)
	}
	for _, old := range []string{"dev", "prod"} {
		if _, err := os.Stat(filepath.Join(fleetPath(root, old), parkBookName)); err != nil {
			t.Errorf("a path under %s no longer reaches the fleet: %v", old, err)
		}
	}
}

// A name that forwards to nothing - its fleet gone by hand - is stale, and taken.
func TestARenameTakesANameThatForwardsToNothing(t *testing.T) {
	root := aFleetRoot(t, "atlas")
	if err := os.Symlink("gone", fleetPath(root, "dev")); err != nil {
		t.Fatal(err)
	}
	mustMoveFleet(t, root, "atlas", "dev")
	if fi, err := os.Lstat(fleetPath(root, "dev")); err != nil || !fi.IsDir() {
		t.Errorf("dev is not the fleet's directory: %v", err)
	}
}

// `wake --fleet <old>` lands on the fleet's real name, so neither a window nor a
// daemon it starts ever runs under an alias; a link to nothing forwards nothing.
func TestAnOldNameReachesTheFleetItForwardsTo(t *testing.T) {
	root := aFleetRoot(t, "atlas")
	mustMoveFleet(t, root, "atlas", "dev")
	mustMoveFleet(t, root, "dev", "prod")
	for _, name := range []string{"atlas", "dev", "prod"} {
		if got, err := fleetDirIn(root, name); err != nil || got != fleetPath(root, "prod") {
			t.Errorf("--fleet %s resolved to %q (%v), want prod's directory", name, got, err)
		}
	}

	if err := os.Symlink("gone", fleetPath(root, "stale")); err != nil {
		t.Fatal(err)
	}
	if got, err := fleetDirIn(root, "stale"); err != nil || got != fleetPath(root, "stale") {
		t.Errorf("--fleet stale resolved to %q (%v), want the name as its own", got, err)
	}
	if _, err := os.Lstat(fleetPath(root, "stale")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a link to nothing was left in place: %v", err)
	}
}

// A bare `wake` that drew an old name would open the renamed fleet under it.
func TestABareWakeNeverDrawsANameThatForwards(t *testing.T) {
	root := aFleetRoot(t, fleetPool[1])
	if err := os.Symlink(fleetPool[1], fleetPath(root, fleetPool[0])); err != nil {
		t.Fatal(err)
	}
	if got, err := nextFleetName(root); err != nil || got != fleetPool[2] {
		t.Errorf("a bare wake would be called %q (%v), want %s past the forwarding %s", got, err, fleetPool[2], fleetPool[0])
	}
}

// The daemon's exit takes every old name of its own fleet and nobody else's.
func TestAFleetsExitDropsOnlyTheNamesThatForwardToIt(t *testing.T) {
	root := aFleetRoot(t, "atlas", "mesa")
	mustMoveFleet(t, root, "atlas", "dev")
	mustMoveFleet(t, root, "dev", "prod")
	if err := os.Symlink("mesa", fleetPath(root, "cove")); err != nil {
		t.Fatal(err)
	}

	dropForwards(root, fleetPath(root, "atlas"))
	for _, gone := range []string{"atlas", "dev"} {
		if _, err := os.Lstat(fleetPath(root, gone)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s still forwards after the fleet's daemon ended: %v", gone, err)
		}
	}
	for _, kept := range []string{"prod", "mesa", "cove"} {
		if _, err := os.Stat(fleetPath(root, kept)); err != nil {
			t.Errorf("%s did not survive another fleet's exit: %v", kept, err)
		}
	}
}

// What a report calls the fleet: a named one's name, the unnamed one's word,
// and nothing for a socket set by hand.
func TestTheReportNamesTheFleetBySocket(t *testing.T) {
	root := filepath.Join(string(filepath.Separator)+"h", stateDirName)
	for _, tc := range []struct{ socket, want string }{
		{filepath.Join(root, socketFileName), LegacyFleet},
		{filepath.Join(fleetPath(root, "atlas"), socketFileName), "atlas"},
		{filepath.Join(fleetPath(root, "atlas"), "other.sock"), ""},
		{filepath.Join(root, "elsewhere", "atlas", socketFileName), ""},
		{filepath.Join(string(filepath.Separator)+"tmp", "x", "s"), ""},
	} {
		if got := fleetNameOf(root, tc.socket); got != tc.want {
			t.Errorf("a daemon on %s reports fleet %q, want %q", tc.socket, got, tc.want)
		}
	}
}

// The unnamed fleet has no directory of its own, and a hand-set socket names no
// fleet, so neither has a name to change.
func TestTheUnnamedAndHandSetFleetsKeepTheirSockets(t *testing.T) {
	t.Setenv("HOME", tempHome(t))
	t.Setenv(SocketEnv, "")
	root, err := StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ socket, says string }{
		{filepath.Join(root, socketFileName), "unnamed"},
		{tempSocket(t), SocketEnv},
	} {
		s := newServer(tc.socket)
		if err := s.moveFleetTo("dev"); err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("renaming the fleet on %s answered %v, want a refusal saying %q", tc.socket, err, tc.says)
		}
		if got := s.fleetSocket(); got != tc.socket {
			t.Errorf("a refused rename moved the fleet to %s", got)
		}
	}
	if _, err := os.Lstat(fleetPath(root, "dev")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a refused rename made a fleet called dev: %v", err)
	}
}

// Over a real socket: the report moves to the new name, every path to the old
// one still reaches the daemon, `--fleet` lands on the new one from either, and
// the daemon's exit takes the old name with it.
func TestRenamingARunningFleetKeepsEveryPathToIt(t *testing.T) {
	t.Setenv("HOME", tempHome(t))
	t.Setenv(SocketEnv, "")
	old, err := FleetSocketPath("atlas")
	if err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(filepath.Dir(filepath.Dir(old)), "dev", socketFileName)
	d := startDaemonOn(t, old)
	c := attach(t, old)

	c.send(rpc.Frame{Kind: rpc.FrameRenameFleet, Text: "dev"})
	st := c.await("a report naming the fleet dev", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameStatusPush && f.Status != nil && f.Status.Fleet == "dev"
	}).Status
	if st.Socket != renamed {
		t.Errorf("the report puts the fleet on %s, want %s", st.Socket, renamed)
	}
	// With no manager yet, the one the fleet would start is the new name's.
	if st.ManagerClaude != "manager-dev" {
		t.Errorf("the report names the fleet's manager %q, want manager-dev", st.ManagerClaude)
	}
	// An agent's WAKE_SOCKET, and every window's, still names the old path.
	if got := attach(t, old).status(); got.Fleet != "dev" {
		t.Errorf("the old path reached a daemon calling itself %q, want dev", got.Fleet)
	}
	for _, name := range []string{"atlas", "dev"} {
		if got, err := FleetSocketPath(name); err != nil || got != renamed {
			t.Errorf("--fleet %s is %q (%v), want %s", name, got, err, renamed)
		}
	}
	if got, err := Fleets(); err != nil || !slices.Equal(got, []string{"dev"}) {
		t.Errorf("`wake fleets` lists %v (%v), want only dev", got, err)
	}

	c.send(rpc.Frame{Kind: rpc.FrameRenameFleet, Text: "dev"})
	if f := c.await("the refusal of a rename to the name it has", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError
	}); !strings.Contains(f.Text, "already called") {
		t.Errorf("a rename to the fleet's own name answered %q", f.Text)
	}

	d.stop(t)
	if _, err := os.Lstat(filepath.Dir(old)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("atlas still forwards after its daemon ended: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(renamed)); err != nil {
		t.Errorf("the fleet's own directory went with its daemon: %v", err)
	}
}

// Over a real process: a manager under the fleet's own name follows the fleet,
// told with the one /rename any rename sends.
func TestTheManagerFollowsARenamedFleet(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	t.Setenv("HOME", tempHome(t))
	t.Setenv(SocketEnv, "")
	sock, err := FleetSocketPath("atlas")
	if err != nil {
		t.Fatal(err)
	}
	startDaemonOn(t, sock)
	c := attach(t, sock)

	spawnManager(c, idAlpha)
	if got := askRenames(c, idAlpha); !strings.HasSuffix(got, "name=manager-atlas") {
		t.Fatalf("the manager was started as %q, want --name manager-atlas", got)
	}
	c.send(rpc.Frame{Kind: rpc.FrameRenameFleet, Text: "dev"})
	awaitManagerNamed(c, "manager-dev")
	askRenames(c, idAlpha)
	if got := askRenames(c, idAlpha); !strings.HasPrefix(got, "renames: 1 [manager-dev]") {
		t.Errorf("claude was sent %q, want exactly one /rename manager-dev", got)
	}
}

// The operator's end part wins over the fleet's, and a parked manager has no
// claude to tell: only the name it wakes under moves.
func TestTheManagerFollowsTheFleetOnlyUnderTheFleetsOwnName(t *testing.T) {
	live, _ := aManager(t)
	live.followFleet("manager-harbor", "manager-dev")
	if got := live.wantedClaude(); got != "manager-dev" {
		t.Errorf("a manager under the fleet's name is to be called %q, want manager-dev", got)
	}
	if got := queuedRenames(live); !slices.Equal(got, []string{"/rename manager-dev"}) {
		t.Errorf("following the fleet queued %q, want one [/rename manager-dev]", got)
	}

	named, r := aManager(t)
	mustRename(t, named, r, "ops")
	queuedRenames(named)
	named.followFleet("manager-harbor", "manager-dev")
	if got := named.wantedClaude(); got != "manager-ops" {
		t.Errorf("the operator's manager-ops became %q", got)
	}
	if got := queuedRenames(named); len(got) != 0 {
		t.Errorf("a manager the operator named was sent %q", got)
	}

	parked, _ := aManager(t)
	parked.finish(nil)
	parked.markParked()
	parked.followFleet("manager-harbor", "manager-dev")
	if got := parked.wantedClaude(); got != "manager-dev" {
		t.Errorf("a parked manager will wake as %q, want manager-dev", got)
	}
	if got := queuedRenames(parked); len(got) != 0 {
		t.Errorf("a parked manager was sent %q", got)
	}
}

// A manager parked under the fleet's own name is booked under none, so it wakes
// under whatever the fleet is called by then; a part the operator chose is kept.
func TestAManagerUnderTheFleetsOwnNameIsBookedUnderNone(t *testing.T) {
	s := newServer(tempSocket(t))
	a, r := aManager(t)
	a.claudeWant = s.managerClaudeDefault()
	if got := s.recordOf(a).ClaudeName; got != "" {
		t.Errorf("a manager under the fleet's name was booked as %q, want none", got)
	}
	mustRename(t, a, r, "ops")
	if got := s.recordOf(a).ClaudeName; got != "manager-ops" {
		t.Errorf("a manager the operator named was booked as %q, want manager-ops", got)
	}
}

// With the newest manager record under the fleet's own name, the report names
// the fleet's, not an older record's part.
func TestTheNewestManagerRecordUnderNoNameIsTheFleets(t *testing.T) {
	s := newServer(tempSocket(t))
	for id, rec := range map[string]parkedRecord{
		idAlpha: {Name: core.ManagerName, ClaudeName: "manager-ops", Parked: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
		idBeta:  {Name: core.ManagerName, Parked: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)},
	} {
		rec.ID = id
		if err := s.parked.add(rec); err != nil {
			t.Fatalf("book %s: %v", id, err)
		}
	}
	if got, want := s.managerClaude(), s.managerClaudeDefault(); got != want {
		t.Errorf("the report names the parked manager %q, want the fleet's %q", got, want)
	}
}

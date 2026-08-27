package daemon

// Named fleets: several independent Wakes on one machine, in one directory.
//
// # Why a name and not a directory
//
// The obvious identity for a fleet is the directory it was started in, the way
// `claude` picks a project. It is the wrong one here, and the owner's case is
// the counterexample: *"if I want to develop Wake in this dir, I have multiple
// wakes with different sets of agents in each"*. A directory cannot tell two of
// those apart, and neither can a pid - a fleet outlives the terminal that
// started it, which is what the daemon is *for*, so its identity has to be
// something a person can type again tomorrow.
//
// # Why this is small
//
// Because every piece of per-fleet state is already `filepath.Dir(socket)` plus
// a filename - the roster, the park book, the lock, the log, the manager's MCP
// config. Nothing keys on a global. So a fleet is a **directory with a socket in
// it**, and isolation is a consequence of the layout rather than a feature
// anything has to enforce: two fleets cannot see each other's agents because
// nothing either of them reads is shared.
//
// The default fleet keeps the path it has always had, `~/.wake/daemon.sock`, so
// every existing park book and roster is still the default fleet's afterwards.
// A named one goes under `~/.wake/fleets/<name>/`.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// fleetsDirName holds the named fleets, beside the default fleet's own files
// rather than around them - so the default's socket does not move and a fleet
// cannot be named such that it collides with `daemon.sock` or `parked.json`.
const fleetsDirName = "fleets"

// maxFleetName bounds a name so the socket under it can still be bound.
//
// The real limit is the socket path's, which checkSocketPath already enforces -
// but it fires with a number about `sun_path` for somebody who typed a name,
// and the two errors are about different things. This one is about the name.
const maxFleetName = 32

// fleetName is what a fleet may be called: one path segment, and a
// conservative one.
//
// **This is a security boundary, not tidiness.** The name becomes a directory
// under ~/.wake, so a name containing a separator or `..` writes a socket - and
// a roster, and a park book naming session ids - wherever the caller likes.
// `wake --fleet ../../tmp/x` must be a refusal rather than a path.
//
// Letters, digits, dash and underscore. Leading dash is refused so a name
// cannot be mistaken for a flag by anything that later re-parses one, and an
// empty name is refused because it is `--fleet` with nothing after it, which is
// a typo rather than a request for the default.
var fleetName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*$`)

// DefaultFleet is the fleet a bare `wake` uses: the one that has always been
// there. Named as a constant so a caller can say what it means rather than
// passing "".
const DefaultFleet = ""

// FleetSocketPath returns the socket for one fleet, creating its directory.
//
// $WAKE_SOCKET still wins over everything, and that is not a leftover:
// EnsureRunning forks the daemon and then dials it, so the two processes have
// to agree on a path they may not both derive. A caller that sets it has
// chosen an exact socket and named no fleet; a caller that sets it *and* names
// a fleet is refused rather than having one silently ignored.
func FleetSocketPath(name string) (string, error) {
	if sock := os.Getenv(SocketEnv); sock != "" {
		if name != DefaultFleet {
			return "", fmt.Errorf("both %s and a fleet name (%q) were given, and they name different "+
				"sockets: unset %s, or drop the fleet", SocketEnv, name, SocketEnv)
		}
		if err := checkSocketPath(sock); err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(sock), stateDirPerm); err != nil {
			return "", fmt.Errorf("create %s: %w", filepath.Dir(sock), err)
		}
		return sock, nil
	}

	dir, err := fleetDir(name)
	if err != nil {
		return "", err
	}
	sock := filepath.Join(dir, socketFileName)
	if err := checkSocketPath(sock); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, stateDirPerm); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return sock, nil
}

// fleetDir is where one fleet keeps everything: its socket and every file
// beside it.
func fleetDir(name string) (string, error) {
	root, err := stateRoot()
	if err != nil {
		return "", err
	}
	return fleetDirFor(root, name)
}

// stateRoot is ~/.wake, the directory every fleet lives under.
func stateRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, stateDirName), nil
}

// fleetDirIn is fleetDir against a given root, and it is where the rule
// actually lives.
//
// Split out so the tests can drive it with a temporary root instead of setting
// $HOME. That is not tidiness: this package's tests start real daemons, those
// daemons read $HOME on their own goroutines, and a test that moves it
// underneath them is a data race the detector finds intermittently - which
// showed up as three fleet tests failing under `make ci` and passing alone.
func fleetDirIn(root, name string) (string, error) {
	if name == DefaultFleet {
		return root, nil
	}
	if err := checkFleetName(name); err != nil {
		return "", err
	}
	return filepath.Join(root, fleetsDirName, name), nil
}

// checkFleetName refuses a name that is not one, saying which rule it broke.
//
// Two messages rather than one, because the two failures are different
// mistakes: a name that is too long is a name, and a name with a slash in it is
// somebody addressing the filesystem.
func checkFleetName(name string) error {
	if name == LegacyFleet {
		return fmt.Errorf("%q is reserved: it is the word for the fleet that has no name, the one a "+
			"bare `wake` used to open. A fleet cannot be called that", LegacyFleet)
	}
	if len(name) > maxFleetName {
		return fmt.Errorf("fleet name %q is %d characters and the limit is %d", name, len(name), maxFleetName)
	}
	if !fleetName.MatchString(name) {
		return fmt.Errorf("fleet name %q is not usable: a fleet is a directory under ~/%s, so a name "+
			"is letters, digits, dash and underscore - it may not contain %q, %q or begin with %q",
			name, stateDirName, "/", "..", "-")
	}
	return nil
}

// Fleets is every named fleet on this machine, in name order.
//
// The default fleet is **not** in the list and is not missing from it: it has no
// name, so there is nothing to print in a column of names, and `wake status`
// with no fleet is already how somebody looks at it.
//
// A directory with no socket in it is still a fleet. It is one that has been
// stopped rather than one that never existed - the park book beside it is what
// `/resume` reads - and leaving it out would make a stopped fleet unfindable by
// exactly the person looking for how to bring it back.
func Fleets() ([]string, error) {
	root, err := stateRoot()
	if err != nil {
		return nil, err
	}
	named, err := fleetsIn(root)
	if err != nil {
		return nil, err
	}
	// The unnamed fleet first, and only when it is real. Every fleet that
	// existed before fleets did is that one, so leaving it out of the listing
	// would hide somebody's whole existing Wake behind a word nothing told them.
	if legacyFleetExists(root) {
		return append([]string{LegacyFleet}, named...), nil
	}
	return named, nil
}

// fleetsIn is Fleets against a given root. Split for fleetDirIn's reason.
func fleetsIn(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, fleetsDirName))
	if os.IsNotExist(err) {
		// Nobody has made one. Not an error: it is the answer.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read fleets: %w", err)
	}
	var names []string
	for _, e := range entries {
		// Checked rather than trusted. This directory is on disk and somebody
		// may have made one by hand; a name that would be refused on the way in
		// must not be listed on the way out as though it worked.
		if e.IsDir() && checkFleetName(e.Name()) == nil {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

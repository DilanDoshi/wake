package daemon

// Naming a fleet nobody named: the pool a bare `wake` draws from, and the one
// reserved word that reaches the fleet that has no name.

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// LegacyFleet is the word that reaches the unnamed fleet at ~/.wake.
//
// It exists because bare `wake` stopped opening that fleet and started making a
// new one, and **every fleet that existed before this change is the unnamed
// one**. Without a word for it, the first run of the new build would leave
// somebody's whole fleet - agents, park book, transcripts - reachable only by
// setting $WAKE_SOCKET by hand.
//
// It is reserved rather than ordinary: checkFleetName refuses it as a *name*, so
// `~/.wake/fleets/default/` cannot exist and there is never a question about
// which of the two `--fleet default` means.
const LegacyFleet = "default"

// fleetPool is what a bare `wake` names a fleet.
//
// **Deliberately not namePool**, which is people: a fleet called `alex` beside
// an agent called `alex` is two different things wearing one word, on a surface
// whose whole job is telling you which agent is which. These are places - a
// fleet is somewhere work happens, and it still reads at four of them.
//
// Twenty-four rather than sixty-four because fleets are counted in ones and
// twos where agents are counted in thirties, and a pool exhausted is not a
// failure here: nextFleetName numbers past the end.
var fleetPool = []string{
	"harbor", "atlas", "meridian", "quarry", "foundry", "orchard",
	"summit", "delta", "basin", "ridge", "cove", "prairie",
	"canyon", "fjord", "mesa", "tundra", "savanna", "reef",
	"glacier", "lagoon", "steppe", "bayou", "highland", "estuary",
}

// nextFleetName is a name no fleet under root is using.
//
// First-free rather than random, and the order is the pool's. A fleet is a
// directory somebody comes back to by typing its name, so the same machine
// giving out the same first name is a feature: `harbor` is what a fresh Wake is
// called, every time, until there are two.
//
// Past the pool it numbers - `harbor-2` - rather than failing. Running out of
// fleet names is not a thing that should stop somebody starting one.
func nextFleetName(root string) (string, error) {
	used, err := fleetsIn(root)
	if err != nil {
		return "", err
	}
	taken := func(name string) bool { return slices.Contains(used, name) }

	for _, name := range fleetPool {
		if !taken(name) {
			return name, nil
		}
	}
	// Every word is in use. Number from the first, which keeps the names
	// readable rather than falling back to something nobody can type.
	for n := 2; ; n++ {
		for _, name := range fleetPool {
			numbered := fmt.Sprintf("%s-%d", name, n)
			if !taken(numbered) {
				return numbered, nil
			}
		}
	}
}

// NewFleetSocketPath makes a fleet nobody named and returns its socket and its
// name.
//
// The name is returned rather than only used because it is the **only way
// back**: bare `wake` no longer reopens what you had, so a fleet whose name was
// never shown is one that can be found again only through `wake fleets`. See
// cmd/wake, which prints it.
func NewFleetSocketPath() (sock, name string, err error) {
	root, err := stateRoot()
	if err != nil {
		return "", "", err
	}
	name, err = nextFleetName(root)
	if err != nil {
		return "", "", err
	}
	sock, err = FleetSocketPath(name)
	if err != nil {
		return "", "", err
	}
	return sock, name, nil
}

// fleetDirFor resolves the words a caller may type, including the reserved one.
//
// LegacyFleet is the only word that does not become a directory under
// `fleets/`, which is what makes it the unnamed fleet rather than a fleet
// called "default".
func fleetDirFor(root, name string) (string, error) {
	if name == LegacyFleet {
		return root, nil
	}
	return fleetDirIn(root, name)
}

// legacyFleetExists reports whether the unnamed fleet has anything in it.
//
// Asked so `wake fleets` can list it only when it is real. A machine that has
// never run the old build has an empty ~/.wake, and a listing that always
// carried "default" would be advertising a fleet with nothing in it - which
// somebody would then open, and find empty, and wonder what they had lost.
func legacyFleetExists(root string) bool {
	for _, name := range []string{socketFileName, parkBookName, rosterFileName} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return true
		}
	}
	return false
}

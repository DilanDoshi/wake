package rpc

import (
	"fmt"
	"strings"
)

// A per-agent identity colour, and the fence both sides of the socket apply.
//
// Here rather than in internal/daemon for paths.go's reason: both sides check
// it and internal/ui may not import the daemon, so it reads the set from here -
// for the /color usage line and to hold its hue map to these names. The name ->
// hue mapping is internal/ui's (theme.go), held to this list by a bijection test
// - this package knows the names, not the pixels.
//
// The set is a closed vocabulary, not free RGB: a chosen hue has to stay legible
// on both the light and the dark ground, and an arbitrary hex fights that. The
// values are Wake's own, deliberately bolder than Claude's pastel rainbow set;
// see internal/ui/theme.go.

// ColorNone clears an agent's colour. A word rather than a colour, and it may
// not be one of ColorNames, or clearing and setting would share a spelling.
const ColorNone = "none"

// ColorNames is the closed set /color accepts, canonical (lower-case) form.
var ColorNames = []string{"blue", "green", "indigo", "orange", "red", "violet", "yellow"}

// NormalizeColor canonicalises a requested colour, or says why it is not one.
//
// The empty string and ColorNone both clear - an operator asking for no colour
// and one asking to remove it want the same result. A known name folds to its
// canonical lower-case form, the way normalizeName folds case; anything else is
// refused with the whole set named, so a wrong colour costs one line rather than
// a guess.
func NormalizeColor(requested string) (string, error) {
	folded := strings.ToLower(strings.TrimSpace(requested))
	if folded == "" || folded == ColorNone {
		return "", nil
	}
	for _, name := range ColorNames {
		if folded == name {
			return name, nil
		}
	}
	return "", fmt.Errorf("%q is not a colour; choose one of: %s (or %q to clear)",
		requested, strings.Join(ColorNames, " "), ColorNone)
}

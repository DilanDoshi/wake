package core

// What a session may log about itself.
//
// One knob that matters at fleet scale and nowhere else: when one agent of
// thirty misbehaves there is otherwise no log to turn on for *that* agent, and
// turning it on for all thirty is not a diagnosis. Verified present in claude
// 2.1.233 as `-d, --debug [filter]` - *"Enable debug mode with optional
// category filtering (e.g., \"api,hooks\" or \"!1p,!file\")"* - and
// `--debug-file <path>`, *"Write debug logs to a specific file path (implicitly
// enables debug mode)"*.
//
// The predicate is here and the flags are spelled in argv.go, which is
// spend.go's arrangement and for its reason.
//
// # Why a filter never reaches an argv without a file beside it
//
// **`--debug` alone does nothing observable in the mode every Wake agent runs
// in.** Recorded 2026-08-16 against 2.1.233: a `--print --input-format
// stream-json --output-format stream-json --verbose --debug api` session exits
// 0 with **zero bytes on stderr** and a stdout byte-identical to the same spawn
// without the flag, while `--debug-file` on the same session wrote 17KB. So a
// filter with no file is not a weaker log - it is a flag an operator believes
// they turned on and no log anywhere. Refused rather than emitted;
// daemon/spawnconfig.go holds the pairing, because it is a rule about two
// fields and this file knows one.

import "strings"

const (
	// filterSep is how claude spells a list of categories.
	filterSep = ","

	// filterNegate marks a category to leave out: "!1p,!file".
	filterNegate = "!"

	// maxDebugFilter bounds what reaches an exec argv, for rpc's reason: an
	// unbounded one is refused by the OS at a size nobody chose.
	maxDebugFilter = 200
)

// ValidDebugFilter reports whether a category filter may reach a command line
// as --debug.
//
// Closed in the way ValidBudget is and ValidModel is not: the categories are
// claude's own vocabulary, and while the *set* is not knowable from here the
// *shape* is - a comma-separated list of category words, each optionally
// negated. Two refusals, and both are recorded rules rather than taste.
//
// A link naming nothing is ValidFallbackModel's own shape, produced by a
// trailing comma and reported by no frame afterwards. And a word that is not a
// category word is refused rather than passed through, because unlike a model
// there is no argument that the set will grow names this cannot spell: a
// category is an identifier. A wider set can be argued for against a case
// somebody has.
func ValidDebugFilter(filter string) bool {
	if filter == "" || len(filter) > maxDebugFilter {
		return false
	}
	for _, category := range strings.Split(filter, filterSep) {
		if !validDebugCategory(strings.TrimPrefix(category, filterNegate)) {
			return false
		}
	}
	return true
}

// validDebugCategory reports whether one word is a category: letters, digits,
// dash and underscore, at least one of them. No leading dash, which would be a
// flag claude read rather than a category it filtered on.
func validDebugCategory(category string) bool {
	if category == "" || strings.HasPrefix(category, "-") {
		return false
	}
	for _, r := range category {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

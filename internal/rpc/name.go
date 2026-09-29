package rpc

// The whitespace fold both sides apply to a name: the client to a spaced
// `/name`, `/rename` or `/team`, and the daemon to the title a resumed
// transcript recorded - so a `/rename foo bar` comes back from disk as the
// `foo-bar` Wake held. Here for team.go's reason: internal/ui may not import
// the daemon.

import "strings"

// HyphenateName folds internal whitespace in a requested name into single
// hyphens, so a name typed with spaces becomes the one-word `@`-token Wake
// stores. It only spares the one character the daemon was certain to refuse;
// every other rule stays the daemon's (normalizeName).
func HyphenateName(name string) string {
	return strings.Join(strings.Fields(name), "-")
}

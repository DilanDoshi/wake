package mcp

// The two checks spawn_agent's optional name passes on this surface, and the
// whole of what the tool decides about a name. Everything else - the charset,
// the length, uniqueness, the fleet-wide reserved words - is the daemon's own
// claim()/normalizeName, because only the daemon sees the whole fleet.

import (
	"errors"
	"strings"
)

// optionalName reads spawn_agent's name argument.
//
// Absent is "", which the daemon reads as "pick one from the pool" - every
// spawn before naming existed. A present value that is not a string is a
// malformed call and is refused here rather than coerced to "": a model that
// asked for a named agent and silently got a pooled one believes in a name that
// does not exist, and the daemon would spend a process and money on it. A
// present string is returned unchanged for the daemon to validate.
func optionalName(args map[string]any) (string, error) {
	raw, present := args[nameArg]
	if !present {
		return "", nil
	}
	name, ok := raw.(string)
	if !ok {
		return "", errNonStringName
	}
	return name, nil
}

// errNonStringName is a name field that arrived as something other than a
// string. One value so the message is one thing wherever it is asserted.
var errNonStringName = errors.New(nameArg + ` must be a string like "x", or left out to have one assigned`)

// impersonatesChrome reports whether a manager-requested name would read as the
// operator, Wake, or a system authority.
//
// # Why this check is here and not in the daemon
//
// A name the manager chooses is rendered as identity chrome an operator reads
// as trusted - the roster row and the room's speaker heading - and it is chosen
// by a *model*, which is the authorship hazard FrameLabel and FrameColor are
// refused for. The daemon cannot carry this check, because it cannot tell a
// manager's spawn from a human's `wake new <name>`: both arrive as a FrameSpawn
// with Frame.Text and no role, and a human naming their own agent "admin" is a
// deliberate, trusted choice the daemon must still accept. This surface is the
// one place that knows the requester is the manager, so the manager-scoped
// refusal lives here and the daemon's fleet-wide policy stays whole.
//
// It is a best-effort denylist, not a complete one - an injected manager can
// still reach for "0perator" - and that is the accepted trade for keeping name
// policy off a model without a UI provenance marker (owner's 2026-09-20 call).
// It catches the plain impersonations, which is what a confused operator would
// actually misread. It folds the name the way normalizeName will (lower,
// trimmed) so "System" and " operator " are caught too; the daemon does the
// real normalization, this only matches it for the lookup.
func impersonatesChrome(name string) bool {
	return impersonationNames[strings.ToLower(strings.TrimSpace(name))]
}

// impersonationNames are the display names the manager may not give an agent it
// spawns. Each reads, in the roster or the room, as the operator, Wake itself,
// or a system authority. The fleet-wide reserved words ("manager", "all") are
// not here - the daemon refuses those to every spawn already, so a manager one
// is refused whether or not this set names them.
var impersonationNames = map[string]bool{
	"operator": true,
	"you":      true,
	"human":    true,
	"system":   true,
	"admin":    true,
	"root":     true,
	"sudo":     true,
	"console":  true,
	"daemon":   true,
	"wake":     true,
}

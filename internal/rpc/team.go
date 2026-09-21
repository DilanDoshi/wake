package rpc

// A per-agent team tag, and the fence both sides of the socket apply.
//
// Here rather than in internal/daemon for worktree.go's reason: both sides
// check it and internal/ui may not import the daemon, so it reads the shape
// from here - for the /team usage line and the completion it offers.
//
// A team is the operator's own grouping of the fleet, so unlike a colour it is
// not a closed vocabulary - the operator names it. What is fenced is the shape,
// because the name does two jobs: it heads a roster/board section, and it is
// addressed as `@team`, which the router splits on whitespace (router.go's
// splitWord). So a team name is one lower-case token of the mention charset,
// bounded, with "none" and the empty string clearing - NormalizeColor's rule
// for the same reason, that clearing and setting must not share a spelling.

import (
	"fmt"
	"strings"
)

// FrameTeam sets a session's team tag: the operator's own grouping of the
// fleet, which heads a roster and board section and is addressed as `@team`. It
// is FrameColor's shape one field over - Text (a team name, or TeamNone to
// clear) and SessionID, display and routing only and never an argv word,
// refused for a parked or ended session for FrameRename's reason, and fenced by
// NormalizeTeam on both sides.
//
// Declared here rather than in wire.go beside the other frame kinds because
// wire.go is at the file-size hard max; team.go is where the rest of the team
// wire code lives, so the constant sits with its fence.
const FrameTeam = "team" // client → daemon: set a session's team tag

// TeamNone clears an agent's team. A word rather than a name, and it may not be
// a real team, or clearing and setting one called "none" would collide.
const TeamNone = "none"

// maxTeamName bounds the tag that heads a section and rides in a mention. A
// header is a couple of dozen columns and a mention is one token, so a long one
// serves nobody; the bound is small on purpose, not the OS's own by accident.
const maxTeamName = 32

// NormalizeTeam canonicalises a requested team tag, or says why it is not one.
//
// The empty string and TeamNone both clear. A name folds to lower case, the way
// NormalizeColor folds a colour, so `@Backend` and `@backend` are one team;
// anything with whitespace or a character outside the mention set is refused,
// because a team that cannot be one `@`-token is a team that cannot be reached.
func NormalizeTeam(requested string) (string, error) {
	folded := strings.ToLower(strings.TrimSpace(requested))
	if folded == "" || folded == TeamNone {
		return "", nil
	}
	if len(folded) > maxTeamName {
		return "", fmt.Errorf("a team name is at most %d characters, got %d (or %q to clear)",
			maxTeamName, len(folded), TeamNone)
	}
	for _, r := range folded {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			return "", fmt.Errorf("%q cannot be a team name: it heads a section and is addressed as @%s, "+
				"so letters, digits, dash and underscore only (or %q to clear)", requested, folded, TeamNone)
		}
	}
	return folded, nil
}

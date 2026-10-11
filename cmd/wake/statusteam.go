package main

// `wake status --team <name>`: one team's members. It is how an agent, which has
// `wake` on its PATH and no fleet view of its own, finds its teammates.

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// statusFlags takes `--team <name>` off the arguments, returning the rest and
// the canonical team asked for ("" for none).
//
// The flag is `wake status`'s alone, and on another verb it is refused rather
// than left for the arity check to call an extra word. The value is fenced by
// rpc.NormalizeTeam, so `--team Backend` lists what `/team backend` tagged. That
// fence refuses a name that starts with a dash, so no team is spelled like a flag
// the parsers before this one would strip.
func statusFlags(args []string) (rest []string, team string, err error) {
	rest = make([]string, 0, len(args))
	seen := false
	for i := 0; i < len(args); i++ {
		if args[i] != rpc.TeamFilterFlag {
			rest = append(rest, args[i])
			continue
		}
		if args[0] != cmdStatus {
			return nil, "", fmt.Errorf("%s filters `wake status` and nothing else; /team puts an agent on a team", rpc.TeamFilterFlag)
		}
		if seen {
			return nil, "", fmt.Errorf("%s was given twice: a listing is of one team", rpc.TeamFilterFlag)
		}
		if i+1 >= len(args) {
			return nil, "", fmt.Errorf("%s needs a team name: which team to list", rpc.TeamFilterFlag)
		}
		i++
		if team, err = rpc.NormalizeTeam(args[i]); err != nil {
			return nil, "", err
		}
		if team == "" {
			return nil, "", fmt.Errorf("%s was given %q, which clears a team rather than naming one to list", rpc.TeamFilterFlag, args[i])
		}
		seen = true
	}
	return rest, team, nil
}

// onTeam is the sessions on a team, or all of them when none is asked for.
func onTeam(sessions []rpc.SessionStatus, team string) []rpc.SessionStatus {
	if team == "" {
		return sessions
	}
	return slices.DeleteFunc(slices.Clone(sessions), func(s rpc.SessionStatus) bool { return s.Team != team })
}

// noSessions is the line for a report with no row to print: a fleet with nobody
// in it, or a team nobody is on - which says which teams there are instead.
func noSessions(team string, teams []string) string {
	if team == "" {
		return "No sessions.\n"
	}
	out := fmt.Sprintf("No sessions on team %s.", team)
	if len(teams) > 0 {
		out += " Teams: " + strings.Join(teams, ", ") + "."
	}
	return daemon.OneLine(out) + "\n"
}

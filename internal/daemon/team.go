package daemon

// Setting a session's team tag: the operator's own grouping of the fleet, which
// heads a roster and board section and is addressed as `@team`. Display and
// routing only - it never reaches an argv, because a team is Wake's own idea
// rather than something claude is told.
//
// It is colour's shape, and the reasons are colour's: the same lock, the same
// state verdict, and no registry - a team has no uniqueness to keep, since it
// *is* the set of sessions that share the tag, so two agents may share one
// exactly as two share a colour. The value is fenced by rpc.NormalizeTeam,
// which folds case and treats the empty string or rpc.TeamNone as "clear", so
// the daemon stores a canonical token or nothing.
//
// A parked or ended session is refused for colour's reason: a parked session's
// display halves live in the park book, written once by the park itself, and
// the team rides with them (recordFor) rather than being rewritten out of band.

import (
	"errors"
	"fmt"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// setTeam changes which team one session is grouped under.
//
// The state verdict comes before the value check, relabel's order: a parked
// session reports "parked" whatever team was typed, because the way round is to
// bring it back first, not to fix the spelling.
func (a *agent) setTeam(requested string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if why := renameableStates[a.stateLocked(time.Now())]; why != "" {
		return errors.New(why)
	}
	team, err := rpc.NormalizeTeam(requested)
	if err != nil {
		return err
	}
	// A team is addressed as `@team`, so it may not wear a word the router spends
	// on something else: `@all` broadcasts and `@manager` reaches the service, and
	// a team of either name would be a section nothing could address. reservedNames
	// is the router's own set, so this cannot drift from what routing actually
	// claims. (A team that collides with a live *agent* name is refused too, but
	// that check needs the fleet registry and lands with teamOrder - see deferred.md.)
	if team != "" && reservedNames[team] {
		return fmt.Errorf("%q is a reserved routing word, not a team name", team)
	}
	a.team = team
	return nil
}

// teamSession is the FrameTeam handler. Like colorSession it goes through
// withAgent, where "unknown session" is answered once, and publishes on success
// so the new tag reaches every other window and a `wake status` after this
// daemon dies.
func (s *server) teamSession(c *client, f rpc.Frame) {
	s.withAgent(c, f, func(a *agent) error {
		if err := a.setTeam(f.Text); err != nil {
			return err
		}
		s.published(a)
		return nil
	})
}

// orderTeams is the live teams in creation order, and where s.teamOrder grows.
// Every path that tags a session - /team, and a woken session's restored tag -
// shows up in the report's sessions, so appending here as a new team first
// appears catches them all in one place rather than at each mutation site. The
// result is that order filtered to the teams a live session still wears, so an
// emptied team stops shipping but keeps its rank for when a member returns.
// Bounded by the distinct team names a human types. Under s.mu, which guards
// teamOrder; the sessions are sorted, so a new team's rank is deterministic
// across clients. Called from fleet().
func (s *server) orderTeams(sessions []rpc.SessionStatus) []string {
	present := make(map[string]bool)
	for _, ss := range sessions {
		if ss.Team != "" {
			present[ss.Team] = true
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]bool, len(s.teamOrder))
	for _, t := range s.teamOrder {
		seen[t] = true
	}
	for _, ss := range sessions {
		if ss.Team != "" && !seen[ss.Team] {
			s.teamOrder = append(s.teamOrder, ss.Team)
			seen[ss.Team] = true
		}
	}
	out := make([]string, 0, len(present))
	for _, t := range s.teamOrder {
		if present[t] {
			out = append(out, t)
		}
	}
	return out
}

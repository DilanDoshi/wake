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

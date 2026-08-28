package daemon

// Setting a session's identity colour: the hue its name-tag, status bar and
// roster row are drawn in. Display only - it never reaches an argv, because it
// is Wake's own rather than something claude is told.
//
// # It is relabel's shape, and the reasons are relabel's
//
// The same lock, the same state verdict, and no registry: a colour has no
// uniqueness to keep, so two agents may share one, exactly as two on one branch
// share a label. The value is fenced by rpc.NormalizeColor, which folds case and
// treats the empty string or rpc.ColorNone as "clear" - so the daemon stores a
// canonical name or nothing, never a colour internal/ui has no hue for.
//
// # Why a parked or ended session is refused
//
// renameableStates, shared with rename and label, and for the sharper of their
// two reasons: a parked session's display halves live in the park book, written
// once by the park itself. A colour set while parked would have to rewrite that
// book out of band - which rename refuses rather than making the book's contract
// something other than a park writes. The colour still survives a park, because
// it is captured *into* the record when the session parks (recordFor) and
// restored when it wakes; it is set on a live session and no other kind.

import (
	"errors"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// setColor changes what one session is drawn in.
//
// The state verdict comes before the value check, relabel's order: a parked
// session reports "parked" whatever colour was typed, because the way round is
// to bring it back first, not to fix the spelling.
func (a *agent) setColor(requested string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if why := renameableStates[a.stateLocked(time.Now())]; why != "" {
		return errors.New(why)
	}
	color, err := rpc.NormalizeColor(requested)
	if err != nil {
		return err
	}
	a.color = color
	return nil
}

// colorSession is the FrameColor handler. Like renameSession it goes through
// withAgent, where "unknown session" is answered once, and publishes on success
// so the new colour reaches every other window and a `wake status` after this
// daemon dies.
func (s *server) colorSession(c *client, f rpc.Frame) {
	s.withAgent(c, f, func(a *agent) error {
		if err := a.setColor(f.Text); err != nil {
			return err
		}
		s.published(a)
		return nil
	})
}

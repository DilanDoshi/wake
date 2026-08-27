// Renaming a session, and saying what it is working on: the two halves of
// `sydney <> dev-5748`, which the founding message asks for as *"you can either
// rename or assign a 'task' so they are called like `sydney <> dev-5748` or
// `alex <> ui fixes`"*.
//
// # A rename changes the handle, and that is the ruling
//
// Nothing below the daemon is affected, and that half was already true: a name
// is never an address here. `rpc.Frame` carries `SessionID`, the reaper proves a
// process group by finding that UUID in an argv, and a client resolves
// `wake attach sydney` to an id before it writes anything. So the only place a
// rename can change an *address* is a composer, where `@sydney` is how an
// operator addresses one agent — and it changes that one, deliberately.
//
// The alternative is an alias: `@old` keeps working beside `@new`. It is worse
// for a reason this file cannot mitigate, because the reason is the name pool.
// **A name is released when its session ends and handed to the next one**, so an
// alias outlives the session it named: rename `alex` to `bob`, let `bob` end,
// let a fresh spawn draw `alex` from the pool, and `@alex` now resolves to two
// live agents. `core.Resolve`'s exact match is unambiguous *because* the daemon
// guarantees no two live sessions share a name — the alias breaks that
// guarantee at its root, and it breaks it into a misroute nothing reports. What
// the rename does instead is release the old name and take the new one in one
// locked step, so the old handle resolves to nothing and the composer refuses
// with the fleet listed. A refusal somebody reads beats a delivery somebody
// does not.
//
// The residual hazard is real and is smaller than that: an operator reads the
// roster, types `@alex …`, and the agent is renamed before they press ↵. `@` is
// resolved at submit time, so a message already sent is untouched and the window
// is one draft long — and it is the same window an *ending* has had since Phase
// 1, answered the same way, by refusing rather than guessing.
//
// # Why both verbs are refused for a parked or ended session
//
// **Ended**: the name has already gone back to the pool (`retire` releases it
// before the row leaves `s.agents`, so this state is reachable here), and it may
// belong to a live session by the time the frame arrives. There is nothing to
// rename and no display anybody will read again.
//
// **Parked**: a parked session's two display halves live in `parked.json`, and
// that record is written once — by `completePark`, in `retire`, after the
// process is provably gone. A rename that did not rewrite it would come back
// under the old name after a restart, which is exactly the silently-stale
// display fact the park book was built to avoid; and rewriting it would make
// this the first thing other than a park to write that file, which is a change
// to its contract rather than to a name. So it is refused with the way round in
// the sentence: bring the session back, then rename it. See
// docs/notes/deferred.md.

package daemon

import (
	"errors"
	"fmt"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// renameableStates is the verdict per state a running daemon can report, and
// the reason it is a map rather than two `if`s is docs/notes/decisions.md's
// rung 4: `agent.stateLocked` is the producer, so a seventh state it learns to
// return is a build failure until somebody says what a rename does with it.
// TestEveryStateAgentStateLockedCanReportHasARenameVerdict holds it.
var renameableStates = map[string]string{
	rpc.StateIdle:    "",
	rpc.StateWorking: "",
	rpc.StateBlocked: "",
	rpc.StateSilent:  "",
	rpc.StateParked:  parkedDisplay,
	rpc.StateEnded:   endedDisplay,
}

const (
	// parkedDisplay and endedDisplay are the two refusals, and each names what
	// the operator can do instead rather than only what they cannot.
	parkedDisplay = "this session is parked, and what it is called is written in the park book by the park itself; " +
		"`/resume` brings it back, and it can be renamed then"
	endedDisplay = "this session has ended, so its name has already gone back to the pool and may belong to " +
		"another agent by now"

	// renameManager is the one name that is not display. daemon/manager.go keys
	// the MCP configuration and the scoping prompt off `core.ManagerName`,
	// internal/mcp keeps the manager off its own roster by it, and the room's
	// default addressee is found by it - so a manager under a pooled name is a
	// session holding tools that act on the whole fleet, which `@manager`
	// reaches nothing of and which `list_agents` would offer as an ordinary
	// agent.
	renameManager = "the manager's name is what makes it the manager - the tools it holds and the room's " +
		"default addressee are both keyed on it - so it cannot be called anything else"

	// renameNothing is a rename frame carrying no name. Refused rather than
	// read as claim reads an empty request: there, empty means "pick one from
	// the pool", and renaming an agent somebody named to a random word is the
	// one outcome nobody asked for.
	renameNothing = "a rename needs a name to change to"

	// labelNothing is the same for the other half. It is refused rather than
	// read as "put the label back to the branch", which is a verb nothing has
	// asked for and which would be indistinguishable from a dropped argument.
	labelNothing = "a task label needs something to say"
)

// rename changes what a session is called.
//
// One method under the agent's own lock, because the two halves have to agree:
// the registry decides whether a name is free and the agent is what holds one,
// and a check-then-act across them is a window in which two clients rename two
// sessions into one name. The lock order is agent then registry, which is the
// only order anything in this package takes them in - every other caller of
// nameRegistry holds no agent lock at all.
//
// It is deliberately *not* routed through the agent's input queue the way a
// send or a park is. That queue exists to keep a write to a child's stdin off
// the connection's goroutine; this writes nothing to a process, exactly like
// unpark, and putting it behind an agent that has stopped reading its stdin
// would make renaming a wedged session impossible.
func (a *agent) rename(names *nameRegistry, requested string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if why := renameableStates[a.stateLocked(time.Now())]; why != "" {
		return errors.New(why)
	}
	if a.name == core.ManagerName {
		return errors.New(renameManager)
	}
	to, err := names.rename(a.name, requested)
	if err != nil {
		return err
	}
	a.name = to
	return nil
}

// relabel says what a session is working on.
//
// The same lock and the same state verdict, and no registry: a label is display
// with no uniqueness to keep. Two agents on one branch legitimately carry the
// same label today, because that is what taskLabel derives.
func (a *agent) relabel(requested string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if why := renameableStates[a.stateLocked(time.Now())]; why != "" {
		return errors.New(why)
	}
	label, err := normalizeLabel(requested)
	if err != nil {
		return err
	}
	a.label = label
	return nil
}

// named reports whether this agent answers to one name, under its own lock.
//
// It exists because `name` stopped being written-once when rename shipped, and
// the reader that needed it - managerAgent - had been correct for years by
// holding s.mu, which orders nothing against a write under a.mu.
func (a *agent) named(who string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.name == who
}

// currentModel is what this session runs as, under the agent's own lock.
//
// Beside named and rosterRecord rather than beside the field, because the
// reason it exists is theirs: launch writes it and park reads it from another
// goroutine, and this struct's rule is that every display half is read under
// the lock rather than reasoned about per field.
func (a *agent) currentModel() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.model
}

// currentSpend is the ceiling and the failover chain, under the agent's own
// lock. One reader for both because they are read together everywhere - the
// park record and the fleet report - and two calls would take the lock twice
// for one snapshot.
func (a *agent) currentSpend() (budget, fallback string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.budget, a.fallback
}

// rosterRecord is this agent's on-disk row, read under its own lock.
//
// Under the lock because the two display halves are no longer written once at
// launch: a rename changes them while a fan-out goroutine and a status reply
// are reading them. It takes the pgid rather than asking the session for it so
// that launch's call site says which process it recorded.
func (a *agent) rosterRecord(pgid int) record {
	a.mu.Lock()
	defer a.mu.Unlock()
	return record{ID: a.id, Name: a.name, Label: a.label, PID: pgid, Started: a.started}
}

// renameSession and relabelSession are the two frames' handlers.
//
// Both go through withAgent, which is where "unknown session" is answered once,
// and both publish on success - see published, which is what makes a rename
// visible to the other windows and to a `wake status` run after this daemon
// dies.
func (s *server) renameSession(c *client, f rpc.Frame) {
	s.withAgent(c, f, func(a *agent) error {
		if err := a.rename(s.names, f.Text); err != nil {
			return err
		}
		s.published(a)
		return nil
	})
}

func (s *server) relabelSession(c *client, f rpc.Frame) {
	s.withAgent(c, f, func(a *agent) error {
		if err := a.relabel(f.Text); err != nil {
			return err
		}
		s.published(a)
		return nil
	})
}

// published makes a display change visible everywhere a display fact is kept.
//
// The roster, because `wake status` on a machine whose daemon died reads it and
// would otherwise print the name the session was born with. And a status push,
// for launch's reason: another window has asked nothing and would otherwise not
// see the new name until watchLiveness noticed some *other* change - which is
// on the 30 s clamp, and a room where an agent you just renamed keeps its old
// handle for half a minute is a room whose `@` you cannot trust.
func (s *server) published(a *agent) {
	s.record(a, a.sess.Pgid())
	s.broadcast(s.statusPush())
}

// rename releases one name and takes another, in one locked step.
//
// Atomic because the two halves are the same decision: a release followed by a
// claim is a window in which another spawn takes the name being vacated, and a
// claim followed by a release is a window in which one session holds two. The
// registry is the only thing that can see both, exactly as it is the only thing
// that can answer "is this name free" at all.
//
// Every rule a chosen name has to pass is normalizeName's, unchanged and not
// re-stated here: the character set, the length bound that keeps a name from
// containing a UUID, the hex rule that stops a name shadowing a printed short
// id, and the reserved words the router spends. A rename is a name somebody
// chose, so it is held to exactly what `wake new <name>` is held to.
//
// Renaming to the name it already has is a no-op rather than a collision with
// itself, and that arm has to come before the held check for the obvious reason.
func (r *nameRegistry) rename(from, requested string) (string, error) {
	to, err := normalizeName(requested)
	if err != nil {
		return "", err
	}
	if to == "" {
		return "", errors.New(renameNothing)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if to == from {
		return to, nil
	}
	if _, held := r.taken[to]; held {
		return "", fmt.Errorf("a live session is already called %q; choose another name", to)
	}
	delete(r.taken, from)
	r.taken[to] = struct{}{}
	return to, nil
}

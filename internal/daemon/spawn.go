// Starting a session, fanning its events out to every client, and retiring
// it when it ends.
//
// The watchdog that turns agent.go's liveness policy into something a client
// hears about without asking used to be here too, and moved to watchdog.go when
// the probe it runs grew a schedule of its own. This file was 7 lines from the
// 800-line hard max at the time, which is the guard doing the job the rule
// cannot: naming the file that is about to cross rather than the one that has.

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// spawnPermissionMode is what every agent this daemon starts is spawned with.
//
// Stated here rather than left to core.NewSession's default, which is the same
// value. A fleet-wide behavioural decision - whether 20 agents ask before
// touching a repo - inherited from a constructor is a decision nobody can find
// when they come looking for it. Named at the spawn site, it is where anyone
// changing it would look.
//
// "auto" is the product intent per spec §5, and it is the mode every session
// *starts* in - rpc.FrameMode can move one afterwards, and a mode moved that way
// does not survive a park (permission-mode-findings.md §8), so a woken session
// comes back here. The spelling comes from core so the tree holds one.
const spawnPermissionMode = core.PermissionModeAuto

// spawn starts one session, names it, and puts it in the roster.
//
// The name is claimed before anything is started and released on every path
// that does not end with a running agent. Frame.Text carries what the client
// asked for - a name a person typed after `wake new`, or nothing at all, which
// is what bare `wake` sends and means "pick one". The daemon decides either
// way, because it is the only process that can see the whole fleet.
func (s *server) spawn(ctx context.Context, c *client, f rpc.Frame) {
	if !s.maySpawn(ctx, c, f) {
		return
	}
	// Before the name is claimed and before anything is started: a value this
	// build does not accept must cost no name, no directory and no process. See
	// spawnconfig.go, which owns the checks and says why they are one function.
	if why := s.configRefusal(f); why != "" {
		c.enqueue(errorFrame(f.SessionID, why))
		return
	}
	// Before the name is claimed, for the reason the checks above are: this
	// is the one that runs a subprocess and touches the filesystem, and it is
	// the one most likely to fail on a real machine. A failure is a refusal
	// rather than a spawn in the shared tree - falling back to the repository
	// would put an agent exactly where the operator asked for it not to be.
	dir, err := sessionDir(f)
	if err != nil {
		c.enqueue(errorFrame(f.SessionID, err.Error()))
		return
	}
	name, err := s.spawnName(f)
	if err != nil {
		// Before the process exists rather than after: a name that is already
		// taken must not cost a claude process that is then torn down, and the
		// client is waiting to be told which of the two things it asked for
		// went wrong.
		c.enqueue(errorFrame(f.SessionID, err.Error()))
		return
	}
	cfg, err := s.configFor(f)
	if err != nil {
		s.names.release(name)
		c.enqueue(errorFrame(f.SessionID, err.Error()))
		return
	}
	// configFor is where the debug log name is claimed, so the release is armed
	// only once that has returned: a second spawn on this id is refused there,
	// and it must not give back the claim the first one is holding.
	claimedDebug := f.DebugFile != ""
	defer func() {
		if claimedDebug {
			s.releaseDebugFile(f.SessionID)
		}
	}()
	cfg.SessionID, cfg.Name, cfg.Dir = f.SessionID, name, dir
	cfg.PermissionMode = spawnPermissionMode
	claimedDebug = false // launch owns the claim from here.
	s.launch(c, cfg, "", nil, nil)
}

// spawnName is the name a spawn gets, and it is the only place Role is read.
//
// A role this build does not recognise is an ordinary agent - the safe,
// existing and overwhelmingly common case, which is why Role is a field rather
// than a second frame kind. Frame.Text is ignored on a manager spawn: there is
// one manager name and the daemon chooses it, so a client asking for another
// one is asking for something that would not be the manager.
//
// **Only spawn reads it.** A fork's name comes from claim, which refuses the
// reserved word, so a fork of the manager is an ordinary agent - which is
// right: it is a second session, and there is one manager.
func (s *server) spawnName(f rpc.Frame) (string, error) {
	if f.Role == rpc.RoleManager {
		name, err := s.names.claimManager()
		if errors.Is(err, errManagerNameHeld) {
			return "", errors.New(s.managerRefusal())
		}
		return name, err
	}
	return s.names.claim(f.Text)
}

// fork starts a session that inherits another's conversation.
//
// It differs from spawn in exactly three places and shares everything else, on
// purpose: from the moment it starts, a fork is an ordinary Wake session.
//
//  1. It refuses a parent whose state is not one the recordings cover, and says
//     when it could be forked instead - see forkRefusal.
//  2. Its Config carries ForkFrom, which is what turns core's identity flags
//     into the fork triple.
//  3. It runs in the **parent's** directory and ignores f.Dir. That is not
//     tidiness: claude derives the project slug from the working directory, and
//     the transcript being forked lives under the parent's slug. Resuming or
//     forking from a different working directory is completely unrecorded
//     (2026-08-10 findings §12), so the parent's own directory is the only one
//     with evidence behind it.
//
// maySpawn is unchanged and is called first, so the fork's own id gets the
// same treatment a spawn's does - a UUID, not already held, not while the
// daemon is shutting down.
//
// **Every refusal here carries f.SessionID, the fork's own id, never the
// parent's.** The client is waiting on that id: cmd/wake's awaitSpawn matches
// an error frame by it, and the TUI clears its pending fork by it. A refusal
// addressed to the parent leaves both waiting forever, and neither wait has a
// deadline by design.
func (s *server) fork(ctx context.Context, c *client, f rpc.Frame) {
	if !s.maySpawn(ctx, c, f) {
		return
	}
	parent, err := s.forkSource(f.ParentID)
	if err != nil {
		c.enqueue(errorFrame(f.SessionID, err.Error()))
		return
	}
	name, err := s.names.claim(f.Text)
	if err != nil {
		c.enqueue(errorFrame(f.SessionID, err.Error()))
		return
	}
	s.launch(c, core.Config{
		SessionID:      f.SessionID,
		ForkFrom:       parent.ID,
		Name:           name,
		Dir:            parent.Dir,
		PermissionMode: spawnPermissionMode,
	}, parent.ID, nil, nil)
}

// launch starts one configured session and wires it into the fleet.
//
// Extracted rather than copied, because the half that would drift is the half
// that matters: every path out of here that does not end with a running agent
// releases the name, and a second copy of that is how a fleet runs out of
// names it is not using. CLAUDE.md's rule is that a change either extends the
// existing code or replaces it - never a second version beside it.
//
// The caller has already claimed cfg.Name and has already been through
// maySpawn.
//
// replaces is the parked agent this launch is bringing back, or nil for a
// session that is starting fresh. It changes two things and both are about
// ownership. The name is already claimed by the parked session, so a failure
// here must not release it - the session is still in the fleet under it. And
// the id is already in s.agents, so register would refuse: a wake replaces
// that row rather than taking a free one.
//
// **A failed wake therefore leaves the session parked**, which is what the two
// `replaces == nil` guards below buy: the row stays, the name stays, and the
// operator can try again.
//
// # The row is taken before the process is started, and that ordering is the
// whole safety of waking
//
// Each client connection is dispatched on its own goroutine, so two FrameWake
// frames for one id can be inside unpark at the same time - two `wake` windows
// on one daemon, which is this product's own premise at 15-30 sessions. Started
// first, the loser of the race for the row has **already spawned a second
// claude on the id**, and resumeSafe provably could not have seen the winner's:
// it may not have existed when the loser looked. Two live processes on one id
// are both accepted, both answer correctly from their own history, neither is
// told about the other, and the transcript branches in place with
// last-writer-wins and no error on any wire (2026-08-09 findings §5). There is
// nothing to detect afterwards, so the only defence is to never reach it.
//
// Taking the row first turns that into a refusal with nothing started, which
// costs one undo: withdraw puts the row back when the process will not start.
// TestLaunchTakesTheRowBeforeItStartsAProcess is where the ordering lives,
// because "this statement runs before that one" is a static property and the
// race that violates it has an exec inside its window.
// launch reports whether a process is running. Wake uses the answer to commit
// durable record deletion or release process-local reservation ownership.
func (s *server) launch(c *client, cfg core.Config, parent string, replaces *agent, outcome func(bool)) bool {
	settle := func(success bool) {
		if outcome == nil {
			return
		}
		callback := outcome
		outcome = nil
		callback(success)
	}
	launched := false
	releaseDebug := cfg.DebugFile != "" && s.ownsDebugFile(cfg.SessionID, cfg.DebugFile)
	defer func() {
		if releaseDebug && !launched {
			s.releaseDebugFile(cfg.SessionID)
		}
	}()
	// Every session this daemon starts comes through here - spawn, fork,
	// import and wake - which is why the check is here and not at any of them.
	// It was at the spawn *frame* only, and a wake builds its Config from
	// parked.json instead: a file on disk that parkbook.go's own header says
	// somebody may have edited by hand. A row edited there put an arbitrary
	// string on a live process's command line with the frame check untouched.
	//
	// core/argv.go deliberately does not ask this. Its path is held to
	// emptiness tests on Config fields by argvguard_test.go, because a richer
	// question there is a flag that can silently stop being emitted - so the
	// last place that can refuse is the last place *before* the argv.
	if why := s.configRefusal(rpc.Frame{Effort: cfg.Effort, MaxBudgetUSD: cfg.MaxBudgetUSD, FallbackModel: cfg.FallbackModel}); why != "" {
		if replaces == nil {
			s.names.release(cfg.Name)
		}
		settle(false)
		c.enqueue(errorFrame(cfg.SessionID, "refusing to start a session: "+why))
		return false
	}
	// Same door, same reason, for the paths: maySpawn checks the spawn *frame*,
	// which reaches no wake, fork or import - and these become cmd.Dir and argv
	// words, so this is the last place that can refuse one. See launchRefusal.
	if why := s.launchRefusal(cfg); why != "" {
		if replaces == nil {
			s.names.release(cfg.Name)
		}
		settle(false)
		c.enqueue(errorFrame(cfg.SessionID, why))
		return false
	}
	// Before anything is taken or started, because it is the one step that can
	// fail with nothing to undo. It is here rather than at the three call sites
	// so that a **wake** gets it too: the manager's configuration is derived
	// from its name, and unpark builds a Config from the row it already holds -
	// see manager.go for why a version carried on the spawn frame would come
	// back from a park with no tools and nothing saying so.
	cfg, err := s.managerConfig(cfg)
	if err != nil {
		if replaces == nil {
			s.names.release(cfg.Name)
		}
		settle(false)
		c.enqueue(errorFrame(cfg.SessionID, err.Error()))
		return false
	}
	// Every agent runs under the durable supervisor, so this daemon keeps an
	// off-disk handle to its whole process group that outlives the daemon. A
	// platform without one returns an empty launcher and the agent runs directly.
	// A failed resolve is not fatal - an unsupervised agent still works, it is
	// only harder to reclaim after a crash - the same trade s.record makes for a
	// failed roster write.
	if launcher, lerr := newAgentLauncher(); lerr != nil {
		logf("wake: session %s could not resolve a supervisor, starting it directly: %v", cfg.SessionID, lerr)
	} else {
		cfg.Launcher = launcher
	}
	// Deliberately not derived from the daemon's own context. Cancelling is
	// a SIGKILL to the agent's whole process group, so deriving it would
	// turn every daemon shutdown into a fleet-wide kill mid-tool - the one
	// thing spec §5 says Wake is not entitled to do. Shutdown stops
	// sessions gently and reaches for this only for what is left after the
	// grace.
	actx, cancel := context.WithCancel(context.Background())
	sess := core.NewSession(cfg)
	a := newAgent(cfg.SessionID, cfg.Name, labelFor(cfg.Dir, replaces), cfg.Dir, parent, sess, cancel)
	// Set before the agent is published, so park can write down what it ran at.
	a.effort = cfg.Effort
	a.model = cfg.Model
	a.budget = cfg.MaxBudgetUSD
	a.fallback = cfg.FallbackModel
	a.color = cfg.Color // display only, empty on a fresh spawn; set for the wake paths

	// Read before admit takes it, so a launch that fails can put it back. See
	// withdraw.
	ending := s.endingFor(cfg.SessionID)
	if why := s.admitLive(a, replaces, cfg.ResumeFrom != ""); why != "" {
		// Whether the id was claimed between the check and here or the fleet
		// filled to the cap, the refusal lands before anything was started -
		// so there is no process to stop, which is the point of the ordering.
		cancel()
		if replaces == nil {
			s.names.release(cfg.Name)
		}
		settle(false)
		c.enqueue(errorFrame(cfg.SessionID, why))
		return false
	}
	// Supervised, the record goes down through the ownership callback before the
	// supervisor is released, closing the fork-to-record window BUG-16 set out to.
	// It never rejects (a roster write is non-fatal). The direct path has no
	// release to precede and core refuses a callback without a supervisor, so below.
	supervised := cfg.Launcher.Active()
	var onProcess func(context.Context, int) error
	if supervised {
		onProcess = func(_ context.Context, pgid int) error {
			s.record(a, pgid)
			return nil
		}
	}
	if err := sess.StartObserved(actx, onProcess); err != nil {
		s.withdraw(a, replaces, ending)
		cancel()
		if replaces == nil {
			s.names.release(cfg.Name)
		}
		settle(false)
		c.enqueue(errorFrame(cfg.SessionID, err.Error()))
		return false
	}
	launched = true
	if !supervised {
		// The direct path's record, as close behind the start as the path allows.
		s.record(a, sess.Pgid())
	}
	settle(true)
	s.start(a.serveInput)
	s.start(func() { s.fanOut(a) })
	c.enqueue(s.statusReply())
	// Announced to everybody, not only to the client that asked. The reply
	// above answers *this* client; a room open in another terminal has asked
	// nothing and would otherwise not see the new agent until watchLiveness
	// noticed its state was unreported - which lands on the 30s clamp. A group
	// chat where a new member appears half a minute late is not one.
	// Event-driven, so nothing is added to any timer.
	s.broadcast(s.statusPush())
	return true
}

// forkSource is the session a fork may be taken from, or why it may not be.
//
// It reads s.fleet(), which is the one place liveness is decided and which
// carries the recent endings as well as the live map - so an ended parent is
// forkable, which is what the 2026-08-09 spike recorded throughout. A parent
// this daemon has never held is refused rather than resumed off disk: Wake
// would not know which directory it ran in, and forking from a different
// working directory is unrecorded. Discovery across ~/.claude/projects belongs
// to session importing, which owns the picker.
func (s *server) forkSource(parentID string) (rpc.SessionStatus, error) {
	if parentID == "" {
		return rpc.SessionStatus{}, errors.New("a fork needs a session to fork from")
	}
	for _, p := range s.fleet().Sessions {
		if p.ID != parentID {
			continue
		}
		if why := forkRefusal(p); why != "" {
			return rpc.SessionStatus{}, errors.New(why)
		}
		if p.Dir == "" {
			return rpc.SessionStatus{}, fmt.Errorf("this daemon does not know where session %s ran, and a fork has to run there: "+
				"claude locates a transcript by the directory it was started in", parentID)
		}
		return p, nil
	}
	return rpc.SessionStatus{}, fmt.Errorf("this daemon is not holding session %s, so there is nothing to fork", parentID)
}

// forkRefusal is why this session cannot be forked right now, and when it can.
//
// The scope is exactly as wide as the recording and no wider. What was
// recorded is a fork taken from a parent that was **idle** - its turn
// finished, its result arrived, its transcript flushed - and from parents whose
// process had already exited. A **parked** parent is that second case and not a
// third: park closes stdin and lets the in-flight turn finish, so by the time
// the state is reported the process is gone and the transcript is flushed,
// which is the state 2026-08-09 findings §10 says *every* recorded fork
// resumed. Forking a parent that is mid-turn, mid-tool or
// blocked on a permission ask is a **different recording and it was not made**
// (2026-08-10 findings §12): what such a fork inherits, and whether the
// parent's flush can race the fork's read, are both unknown.
//
// So the unrecorded states are refused rather than guessed at - and refused
// with the *next step* in the sentence, because "no" on its own leaves an
// operator with a key that does nothing and no idea when it would.
//
// It is a refusal and not a lock. A parent that starts a turn a millisecond
// after this returns is the other §12 item - a parent working while a fork
// runs - which Wake cannot prevent, because the operator owns the parent's
// composer.
//
// The mitigation is therefore a sentence rather than a guard, and it now
// exists: internal/ui's forkOpened says, on every confirmed fork, that the fork
// carries the parent's conversation *as of that moment* and that nothing the
// parent does next reaches it. This comment claimed that in the present tense
// before anything said it, and then said so in the honest tense for one task;
// both spellings are recorded in docs/notes/deferred.md, because a doc claiming
// a behaviour that does not exist is the legend rule read backwards and this
// one made the claim twice.
func forkRefusal(p rpc.SessionStatus) string {
	who := p.Name
	if who == "" {
		who = p.ID
	}
	// The manager is refused in every state, above the switch, because the
	// reason is not about the recording. A fork gets a *pooled* name -
	// names.claim refuses the reserved word, correctly, since there is one
	// manager - so what a fork of this parent produces is a claude session
	// holding the manager's whole conversation, its appended scope among it,
	// with no MCP config and no scope of its own. Every tool call it makes
	// fails, and nothing anywhere says why.
	//
	// Reading Name for a verdict rather than only for the sentence is new here
	// and it is deliberate: forkRefusalMayRead already allows this field, and
	// the name is the daemon's own - names.go refuses core.ManagerName to every
	// ordinary spawn, so it cannot catch somebody else. It is the same
	// discriminator manager.go and internal/mcp key on.
	if p.Name == core.ManagerName {
		return "the " + core.ManagerName + " cannot be forked: a fork gets an ordinary name, so it would " +
			"be a session holding this conversation with none of the tools it is about. Ask @" +
			core.ManagerName + " itself, or fork one of the agents it reported."
	}
	switch p.State {
	case rpc.StateIdle, rpc.StateEnded, rpc.StateParked:
		return ""
	case rpc.StateWorking:
		return who + " is in the middle of a turn. Fork it when the turn ends, or stop the turn first."
	case rpc.StateBlocked:
		return who + " is waiting on a permission request. Answer or deny it, then fork."
	case rpc.StateSilent:
		return who + " has gone silent with a turn still open. Fork it when it comes back, or stop it first."
	case rpc.StateOrphaned:
		return who + " has no daemon holding it: check `wake status`."
	default:
		return "nothing is known about the state of " + who + ", so it cannot be forked"
	}
}

// spawnDir is where an agent runs.
//
// The client's directory when it has one, because there is one daemon per
// machine and a user's work is spread across repositories: an agent started
// from repo B and run in repo A edits the wrong tree, silently, and claude
// persists the transcript under the wrong path so park and wake inherit the
// mistake. cwdOrHome is the fallback and it is the daemon's own directory -
// which is whichever directory the client that happened to fork it was in.
func spawnDir(f rpc.Frame) string {
	if f.Dir != "" {
		return f.Dir
	}
	return cwdOrHome()
}

// record writes the agent down so a later daemon can find it. See rosterRecord.
func (s *server) record(a *agent, pgid int) {
	if err := s.roster.add(a.rosterRecord(pgid)); err != nil {
		// Not fatal to the session: an unrecorded agent still works, it is
		// only unfindable if this daemon dies. Said out loud for the same
		// reason.
		logf("wake: session %s is not recorded on disk, so a crash would orphan it: %v", a.id, err)
	}
}

// labelFor is what a launched session is working on.
//
// A woken session keeps the label it parked with; only a fresh one derives it.
// That is the same rule as its id, its name and its directory - a wake is not a
// new session - and it is what rpc.FrameWake's own doc comment promises.
// Re-deriving re-reads .git/HEAD, so a checkout while the session was parked
// would silently relabel a conversation nobody moved, on the surface an
// operator scans thirty rows of.
//
// replaces.label is read without a lock: unpark took a.mu immediately above via
// isParked, and a parked session cannot be renamed. See rename_test.go's list.
func labelFor(dir string, replaces *agent) string {
	if replaces != nil {
		return replaces.label
	}
	return taskLabel(dir)
}

// admit puts one agent in the fleet: a fresh session only if its id is free, a
// woken one only in place of the exact parked agent it came from.
func (s *server) admit(a, replaces *agent, wake bool) bool {
	if replaces != nil {
		return s.replaceParked(a, replaces)
	}
	// **A parked id is not a free id**, and nothing else refuses this any more.
	// The park book used to be restored into s.agents, so register's own check
	// covered it; now the fleet holds nothing for a parked session and a spawn
	// under its id would put a second process on that transcript - which
	// branches it with last-writer-wins and no error on any wire, the one
	// collision this project has no way to detect afterwards.
	//
	// A resume never refuses itself here: unparkRecord takes the record out of
	// the book before it launches.
	if _, parked := s.parked.record(a.id); parked && !wake {
		return false
	}
	return s.register(a)
}

// admitRefusal is what the loser of a race for one id is told, and the three
// answers are different events rather than one sentence with a variable in it.
//
// A spawn's id was supposed to be free and was not. A **wake's** session
// obviously already exists - that is what makes it a wake - so "already exists"
// says nothing about what happened, which was that something else brought this
// one back first and there is nothing left to do. The old text was the spawn
// sentence on both paths, which `deferred.md` named as part of this fix.
//
// The third is a spawn under a **parked** id, which reads as "already exists"
// only if you know the park book exists. It names the command that does what
// the caller was probably trying to do, because nothing else on this path will.
func (s *server) admitRefusal(sessionID string, replaces *agent, wake bool) string {
	if s.refusesAdmission() {
		return "the daemon is shutting down"
	}
	if replaces != nil || wake {
		return "session " + sessionID + " was already brought back by something else, so this wake has nothing to do"
	}
	if _, parked := s.parked.record(sessionID); parked {
		return "session " + sessionID + " is parked, and starting one under its id would put two " +
			"processes on its transcript; /resume brings it back instead"
	}
	return "session " + sessionID + " already exists"
}

// withdraw undoes admit for a launch whose process would not start.
//
// The row is taken before the process exists, so this is what keeps a failed
// launch from leaving a session in the fleet with nothing behind it: no
// process, no fan-out goroutine, and therefore no retire - a row that reports
// as a live agent forever and that `wake stop` counts.
//
// A wake goes back to the exact agent it replaced, which is what makes **a
// failed wake leave the session parked** and something the operator can try
// again. A fresh spawn's id goes back to being free.
//
// ending is what admit dropped on the way in. register forgets any remembered
// ending for the id so a respawned session is not reported alive *and* dead in
// one report; once the launch has failed the id is not alive, so the ending is
// true again and putting it back is the only way `wake status` keeps its
// account of how that session died.
//
// The pointer check is replaceParked's, for replaceParked's reason: whoever is
// in the row now may not be the agent this launch put there, and a rollback
// that writes over somebody else's row is the defect it exists to prevent.
func (s *server) withdraw(a, replaces *agent, ending *rpc.SessionStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agents[a.id] != a {
		return
	}
	// A supervised start records before it releases the supervisor, so a start
	// that then fails has a durable record to undo - else the next daemon's
	// reaper would KillGroup a pgid this start never kept. Before the replaces
	// branch because the id is this failed launch's either way; a no-op when
	// nothing was recorded (the direct path records only on success). A failure
	// *before* release can still race its record in past this remove - core must
	// not join the callback goroutine there - and reapRecord then clears the
	// stray by argv rather than mis-killing: retire's pgid-reuse hazard, earlier.
	if rerr := s.roster.remove(a.id); rerr != nil {
		logf("wake: could not undo the roster record for the failed start of %s: %v", a.id, rerr)
	}
	if replaces != nil {
		s.agents[a.id] = replaces
		return
	}
	delete(s.agents, a.id)
	if ending != nil {
		s.rememberLocked(*ending)
	}
}

// refusesAdmission reports whether quit is committed or shutdown has already
// snapshotted the fleet, after either of which nothing more may enter.
func (s *server) refusesAdmission() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quitting || s.taken
}

// endingFor is the remembered ending for one id, or nil when there is none.
func (s *server) endingFor(id string) *rpc.SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.recent {
		if r.ID == id {
			ended := r
			return &ended
		}
	}
	return nil
}

// replaceParked swaps a woken agent in for the parked one it came from.
//
// Pointer identity rather than a state check, and that is what keeps it atomic:
// asking the old agent whether it is still parked would mean taking its lock
// under s.mu, which nothing in this package does, and the answer would be stale
// the moment the lock was released. `was` is the exact agent unpark inspected,
// so this either replaces that one or refuses.
//
// forgetLocked for register's reason: a woken session must not be reported
// alive and ended in one report.
func (s *server) replaceParked(a, was *agent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.quitting || s.taken || s.agents[a.id] != was {
		return false
	}
	s.agents[a.id] = a
	s.forgetLocked(a.id)
	return true
}

// register puts the agent in the map and drops any memory of it having ended
// before, so a respawned id is not reported alive and dead at once.
//
// One lock for both halves, because they are one fact. Taken separately, a
// concurrent retire's remember lands between them and leaves the id reported
// alive *and* ended in the same status reply - which is precisely the state
// TestASessionThatComesBackReplacesItsOwnEnding exists to forbid, reachable
// through the code that was written to prevent it.
func (s *server) register(a *agent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Once shutdown has taken the fleet, nothing more is admitted: this map is
	// one nothing reads again, and an agent started into it is a process the
	// grace, the kill and the roster clear all walk past.
	if s.quitting || s.taken {
		return false
	}
	if _, held := s.agents[a.id]; held {
		return false
	}
	s.agents[a.id] = a
	s.forgetLocked(a.id)
	return true
}

// fanOut carries one session's events to every attached client, so a
// reattaching TUI joins the same stream.
//
// The frame is addressed by the id Wake spawned under, not by the id on the
// event. Those differ after a /clear, which mints a new session id mid-process
// - and a client keyed on the frame would watch its session vanish and a
// stranger appear. The event keeps whatever core stamped it with, so nothing
// is lost: re-keying the roster is the registry's job (Phase 3) and this is
// the id every client already holds.
func (s *server) fanOut(a *agent) {
	defer s.retire(a)
	for ev := range a.sess.Events() {
		// An effort probe's reply is consumed here, before observe and before
		// the broadcast, so it never touches this agent's state and never
		// reaches a client. The one push it earns carries the confirmed level.
		if suppress, publish := a.absorbProbe(ev); suppress {
			if publish {
				s.broadcast(s.statusPush())
			}
			continue
		}
		a.observe(ev)
		// One Event, one pointer, shared by every client's copy of the
		// frame. Nothing mutates an Event after the airlock decodes it, and
		// at 30 sessions a copy per client per event is the difference
		// between fan-out being free and being the bottleneck.
		s.broadcast(rpc.Frame{Kind: rpc.FrameEvent, SessionID: a.id, Event: &ev})

		// An ask, an answer and a turn end each change what the roster draws
		// and what ⇧⇥ can find. Left to watchLiveness, that took up to one
		// 30-second tick, so an agent blocked on a permission request read as
		// idle and "next blocked" said nothing was waiting on anybody.
		//
		// After the event, never before it: the report names the ask by id and
		// the event is what carries what it is asking.
		//
		// changed() records the state it announces, so the watchdog does not
		// repeat this, and it broadcasts on a transition rather than per event.
		if a.changed() {
			s.broadcast(s.statusPush())
		}

		// The session's init arrives before any input, so it is where the
		// startup effort probe belongs - the one place Wake can read a level it
		// never chose. Fires once; the /effort re-probe is in apply.
		if a.firstInit(ev) {
			a.probeEffort()
		}
	}
}

// retire records how a session ended and takes it out of the roster.
//
// Err() is read before anything else: core writes it as the events channel
// closes, and it is the only account of a session that died before it wrote a
// single frame.
//
// Leaving the live map and entering the recent endings are **one locked step**,
// and that is the mirror of register rather than a tidy-up. Taken separately -
// which is what this did, with a roster file write in between - the id is in
// neither place for the width of that write, and a respawn landing there finds
// nothing to reconcile: register succeeds because the id is absent, forgetLocked
// clears nothing because the ending is not recorded yet, and then remember puts
// it back. fleet() reads both halves under one lock, so the result is atomically
// observable - one session reported alive *and* ended in the same report, which
// is exactly what TestASessionThatComesBackReplacesItsOwnEnding forbids.
// register was fixed for this last round; the state simply walked in the other
// door.
//
// The snapshot is taken before s.mu, because it takes the agent's own lock and
// s.mu is never held across one. a.mu → names.mu is the one nesting, in rename.
func (s *server) retire(a *agent) {
	err := a.sess.Err()
	a.finish(err)
	a.cancel()
	s.releaseDebugFile(a.id)

	// A park keeps the session and loses only the process, so it leaves before
	// any of the below: the name stays claimed, the id stays in s.agents, and
	// nothing enters s.recent. See completePark.
	if a.parkRequested() {
		s.completePark(a, err)
		return
	}

	// The leader is reaped by now - core's finish returned from Wait before it
	// closed Events, which is what ended the range that got us here - but the
	// group outlives it: anything the agent spawned that stayed in the group, a
	// `npm run dev &`, is still running, and both of core's kill paths are
	// failure paths a clean exit never took. So the pool sweeps the group here,
	// on the terminal non-park end alone: a parked session's children are its
	// woken world and must survive, which is why this sits past the parkRequested
	// return and never in completePark - and why core cannot do it, since only
	// the pool tells a park's clean exit from an ordinary one. An empty group is
	// os.ErrProcessDone, the outcome asked for.
	//
	// pgid reuse since the leader was reaped is the crash reaper's own accepted
	// hazard, bounded by KillGroup's refusal of pgid <= 1 and of Wake's own group.
	if serr := core.KillGroup(a.sess.Pgid()); serr != nil && !errors.Is(serr, os.ErrProcessDone) {
		logf("wake: session %s left a process group that could not be swept: %v", a.id, serr)
	}

	ended := a.snapshot()
	// The name goes back to the pool here rather than when the ending is
	// forgotten: spec §5 says a name is free once its session ends, and the
	// remembered ending keeps the name only so a status report can still say
	// which agent it was. A live session therefore wins its name against a
	// remembered one, which is the same rule matchSession applies to an id
	// prefix.
	s.names.release(a.name)
	s.mu.Lock()
	delete(s.agents, a.id)
	// Remembered before it is announced. The announcement goes through the
	// same fan-out as every other frame and is just as droppable, so the
	// record is what makes the ending recoverable by a client that missed it.
	s.rememberLocked(ended)
	s.mu.Unlock()

	if rerr := s.roster.remove(a.id); rerr != nil {
		logf("wake: session %s could not be removed from the roster: %v", a.id, rerr)
	}
	if err != nil {
		// Reported as an ending, never as a crash. A clean exit 0 becomes
		// an error whenever something the agent spawned held stderr past
		// core's bound, and an interrupted session exits 1 saying nothing.
		logf("wake: session %s ended: %v", a.id, err)
	}
	s.broadcast(s.statusPush())
	s.reconsiderEmptyExit()
}

// cwdOrHome is the fallback for where a spawned agent runs, used only when the
// client did not say.
//
// It is the daemon's working directory, which is the directory the client that
// first started it was in - not necessarily the one the client asking for this
// session is in. rpc.Frame.Dir is how a client says, and spawnDir prefers it;
// this is what is left when a caller has nothing to offer, and it is a footgun
// rather than an answer.
func cwdOrHome() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	home, _ := os.UserHomeDir()
	return home
}

// sortSessions puts a status report in a stable order, so a client diffing two
// replies sees what changed rather than what moved.
func sortSessions(sessions []rpc.SessionStatus) {
	slices.SortFunc(sessions, func(a, b rpc.SessionStatus) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})
}

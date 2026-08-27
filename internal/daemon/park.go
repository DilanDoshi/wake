// Parking a session, and bringing one back.
//
// The daemon's other four endings are one-way, and that is why ⌃C meant detach
// for the whole of Phase 1 - a stray keystroke that ended a session would
// destroy an hour of context with no route back. A park stops the process,
// keeps the transcript on disk, and keeps the row that says which id to hand
// `--resume`. Waking one is the other half and it is the same row read
// forwards. That is what spent the key: ⌃C parks now, ⌃O detaches, and ⌃Q parks
// the fleet on the way out - see internal/ui/park.go.
//
// The asymmetry between the two halves is the whole subject of resumeSafe.
// Parking is a write to a process this daemon owns; waking starts a *second*
// process on an id whose first one may not be gone, and nothing on claude's
// wire reports that collision.

package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// beginPark records that the stop about to happen is a park. Called
// immediately before the stop it describes - on the agent's own input goroutine
// for a FramePark, and on Serve's for a fleet-wide ⌃Q - so nothing can observe a
// stop that has not yet been labelled.
//
// **A session that is already parked is left alone**, which is what keeps
// markParked's invariant true: *"asked to park" and "parked" are never both
// true, and nothing has to decide which it is looking at.* ⌃Q is the caller that
// would otherwise break it — `shutdown` labels every agent it took, and that set
// includes sessions parked earlier in this daemon's life and rows restored from
// the park book, whose retire has already run and will never run again. The
// refusal is here rather than at the call site because both fields are under
// this lock, so it is atomic rather than a check-then-act, and because an
// invariant belongs with the field that states it.
func (a *agent) beginPark() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.parked {
		return
	}
	a.parking = true
}

// parkRequested reports whether the ending now happening is a park. It is the
// only thing that tells retire the two apart: a park and a stop both arrive as
// the events channel closing, and nothing on the wire or in core distinguishes
// them.
func (a *agent) parkRequested() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.parking
}

// markParked is the transition retire makes once the process has actually
// gone. It clears parking in the same step, so "asked to park" and "parked"
// are never both true and nothing has to decide which it is looking at.
func (a *agent) markParked() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.parking, a.parked, a.wakeable, a.parkDurable = false, true, false, false
	a.parkGeneration = parkedRecord{}
}

// markWakeable publishes that the parked generation is fully finalized.
// completePark calls it only after roster removal and the park-book write have
// returned, so observing this under the same lock proves no old-generation
// storage mutation remains to race a successor. durable says whether that
// write succeeded; a failed publication is wakeable in this daemon but still
// needs the daemon to preserve its only in-memory route back.
func (a *agent) markWakeable(rec parkedRecord, durable bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.wakeable = a.parked && rec.ID == a.id
	a.parkDurable = a.wakeable && durable
	if a.wakeable {
		a.parkGeneration = rec
	}
}

// markParkDurable records that an eventful pending-write retry published
// the currently held generation. reconsiderEmptyExit holds admission while it
// calls this, so a wake cannot replace this parked row between the durable
// write and the in-memory publication.
func (a *agent) markParkDurable(rec parkedRecord) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.parked && a.wakeable && a.parkGeneration == rec {
		a.parkDurable = true
	}
}

// bookable reports whether this session ended as a park, which is the only
// thing a park book entry may be written for.
//
// **Both park flags, and the pair is what makes it total over the window
// shutdown reads it in.** `parked` is the settled answer - markParked has run,
// in retire, after core's Wait returned. `parking` is the same answer one
// instant earlier: the process has gone and retire is on its way to
// completePark. shutdown's wait returns on `ended`, which finish sets *before*
// either, so that window is not an edge case here - it is the ordinary path. A
// check reading `parked` alone would drop exactly the sessions whose retire had
// not quite finished, silently, and more of them the busier the machine.
//
// `ended` is the floor and is not implied by the other two: beginPark labels a
// stop that is still in flight, so an agent that never ended is `parking` and
// must not be written down as a park that completed.
//
// A killed session answers false through these same flags rather than through a
// second check - kill clears `parking` and markParked is never reached
// afterwards - so what a --resume of a transcript a SIGKILL cut mid-turn loads
// stays unrecorded and unoffered.
func (a *agent) bookable() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ended && (a.parking || a.parked)
}

// isParked reports whether this session's process has gone, been kept, and
// finished every old-generation storage mutation: the one state a wake may
// act on.
//
// It reads `wakeable` and not merely `parked`. A park that has been asked for
// but has not completed still has a process; after markParked the process is
// gone but completePark may still be removing its roster row or writing its
// park record. Starting a successor in either window lets those delayed writes
// erase the successor's ownership. markWakeable closes both windows after the
// storage calls return.
//
// It is not stateLocked's arm read twice. stateLocked answers "what does a
// client see", and puts parked above ended because a parked agent is also an
// ended one; this answers "is there a session here to bring back", which the
// word on a report cannot be trusted for once anything else starts producing
// it.
func (a *agent) isParked() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.wakeable
}

// needsSupervision is the lifecycle predicate for empty daemon exit. It is
// deliberately not the process-cap predicate: a finalizing park has no live
// process but still owes storage work, and a finalized park whose durable
// write failed still needs this daemon to retain its only in-memory wake row.
// Ordinary ended rows and finalized, durably recorded parks owe nothing.
func (a *agent) needsSupervision() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.ended || a.parking {
		return true
	}
	return a.parked && (!a.wakeable || !a.parkDurable)
}

// needsSupervision reports whether any agent still needs this daemon even when
// it has no live process. Kept separate from liveCount: that is the process-cap
// predicate, while this includes park finalization and an in-memory park whose
// durable publication failed.
func (s *server) needsSupervision() bool {
	s.mu.Lock()
	agents := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	s.mu.Unlock()
	for _, a := range agents {
		if a.needsSupervision() {
			return true
		}
	}
	return s.parked.hasPending()
}

// completePark keeps a session in the fleet with no process behind it.
//
// Everything it does *not* do is the design. It does not release the name -
// spec §5 frees a name when a session *ends*, and a parked one has not, so a
// later spawn must not be handed it and then be un-nameable when its owner
// comes back. It does not put the session in s.recent, because that is the
// list of endings a client might have missed and this is not one. It does not
// take the id out of s.agents, so `holds` still refuses a respawn under it and
// a wake has one authoritative row to replace.
//
// It does remove the roster record, and that is not tidiness: roster.go is
// "the minimum needed to find a process again", the reaper reads it and
// SIGKILLs what it can verify, and a parked session has no process. An entry
// left there points at a pgid that is gone and may since have been recycled.
//
// err is how the process ended, and it is carried rather than judged. A clean
// exit on EOF is the ordinary shape; core's WaitDelay turns exit 0 into an
// error whenever something the agent spawned held stderr past the bound, and
// an interrupted session exits 1 saying nothing. None of those says anything
// about the transcript, which is what --resume reads - so a park is not
// refused for any of them. The one ending that does refuse is a kill, and it
// refuses through a.parking rather than through this error. See agent.kill.
func (s *server) completePark(a *agent, err error) {
	a.markParked()
	if rerr := s.roster.remove(a.id); rerr != nil {
		logf("wake: session %s could not be removed from the roster: %v", a.id, rerr)
	}
	// And into the book the *next* daemon reads. Written here rather than when
	// the park was asked for, which is what makes the entry mean something: this
	// runs in retire, after core's Wait has returned, so the process Wake
	// started is provably gone by the time anything is recorded. A park that
	// never completed - a kill arriving first - writes nothing.
	rec := recordFor(a)
	perr := s.parked.add(rec)
	if perr != nil {
		// Not fatal to the park: the session is parked in this daemon's fleet
		// either way, and what is lost is only the offer a *later* daemon
		// would have made. Said out loud for the same reason record's failure
		// is.
		logf("wake: session %s is parked but is not written down, so a restart would lose the offer to bring it back: %v", a.id, perr)
		s.warnUnbooked(a.id)
	}
	// Publish wakeability only after every old-generation storage call has
	// returned. parked is already the visible state, but a successor cannot
	// replace this row until the old finalizer has no roster or book mutation
	// left that could erase the successor's ownership or newer generation.
	a.markWakeable(rec, perr == nil)
	// A repaired retry may have published this exact record after add returned
	// but before markWakeable installed its generation. A retry after this
	// point acknowledges through reconsiderEmptyExit; this closes the earlier
	// half without trusting only the session id.
	if s.parked.isDurable(rec) {
		a.markParkDurable(rec)
	}
	if err != nil {
		logf("wake: session %s parked, having ended with: %v", a.id, err)
	}
	s.broadcast(s.statusPush())
	s.reconsiderEmptyExit()
}

// recordFor is one agent's park book row.
//
// One constructor because there are two callers - completePark, and bookParked's
// loop over a fleet - and a record built twice is a record that drifts. It did
// not before only because the two literals were kept identical by hand, which is
// the arrangement that stops working the first time a field is added to one.
//
// It takes the lock itself rather than composing the locked accessors, and that
// is one lock for one row rather than three for the same row. Both callers had
// standing excuses in unlockedReadsOfTheDisplayHalves and neither needs one now:
// a row assembled across three acquisitions is a row whose fields can be from
// three different moments, which is what an excuse about ordering was quietly
// permitting.
func recordFor(a *agent) parkedRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	return parkedRecord{
		ID: a.id, Name: a.name, Label: a.label, Dir: a.dir,
		Effort: a.effort, Model: a.model,
		MaxBudgetUSD: a.budget, FallbackModel: a.fallback,
		Parked: time.Now(),
	}
}

// warnUnbooked tells every attached client that a session parked but could not
// be written down, so the promise ⌃C and ⌃Q make - "parked, /resume brings it
// back" - reaches the operator as the exception it is rather than only a daemon
// log nobody attached is reading. The record's failure is already logged beside
// each call; this is the same fact on the surface somebody is watching.
//
// Tier-1: from bookParked it may reach a client a beat before closeClients tears
// the connection down, so rendering it through that teardown is the ⌃Q task's
// half - see docs/notes/deferred.md. One helper because both write-down sites
// broadcast the same sentence, which is recordFor's reason a field over.
func (s *server) warnUnbooked(id string) {
	s.broadcast(errorFrame(id, "session "+id+" is parked but could not be written down, so a restart cannot bring it back"))
}

// bookParked writes down every session this shutdown actually parked.
//
// It is deliberately not "every agent that was asked to park". Two of them are
// excluded and each exclusion is a recording that does not exist:
//
//   - a session that had to be **killed**, because it did not end within the
//     grace. kill() clears parking, so it cannot be bookable. What a --resume
//     of a transcript a SIGKILL cut mid-turn loads is unrecorded, and this
//     project refuses unrecorded behaviour rather than designing around it.
//   - a session that had already lost its process before shutdown began -
//     unreachableNow - which is killed rather than stopped for the same
//     reason and is excluded by the same flag.
//
// Neither is dropped quietly: shutdown names each of them in the log as it
// decides, and the count below is what reconciles against the fleet.
//
// **What this adds over completePark is not content but ordering.**
// completePark runs on the fan-out goroutine, whenever that agent's retire
// gets there, and may already have written the same record; add() is
// mutex-guarded and the file is replaced whole through a rename, so a second
// write of the same entry costs nothing. This one runs on Serve's own
// goroutine, single-threaded, and finishes before closeClients - which is the
// edge a client turns into "the daemon is gone, start a new one". Both write
// the same set and only one of them is early enough.
//
// **A session already in the book is left exactly as it is**, and the field
// that makes that a decision rather than an optimisation is the timestamp.
// Parked records when the session actually stopped, so re-stamping one that
// parked yesterday with now would claim it parked on the way out. The entry
// being there is the whole of what the ordering needs.
//
// Nothing here waits, and that is deliberate: a park that will not complete is
// already bounded by the grace above and has ended as a kill by the time this
// runs, and a book that cannot be written is reported per session with the rest
// still written - one failed write must not cost the other nineteen.
func (s *server) bookParked(agents []*agent) {
	held := s.parked.protectedIDs()

	booked, refused, lost := 0, 0, 0
	for _, a := range agents {
		switch {
		case !a.bookable():
			refused++
		case held[a.id]:
			booked++
		default:
			if err := s.parked.add(recordFor(a)); err != nil {
				logf("wake: session %s is parked but is not written down, so the next start will not offer it back: %v", a.id, err)
				s.warnUnbooked(a.id)
				lost++
				continue
			}
			booked++
		}
	}
	// "could not be parked" and nothing about how they ended, deliberately. The
	// ordinary member of `refused` is a session killed at the end of the grace
	// whose process has not been reaped yet, so `ended` is still false at the
	// moment this prints - and a confident wrong sentence on the surface an
	// operator reads to diagnose a shutdown is how the next one is
	// mis-diagnosed. Each of them is named by the loop above that decided it.
	logf("wake: parked %d sessions on the way out; %d could not be parked; %d are parked and could not be written down",
		booked, refused, lost)
}

// resumeSafe reports whether this daemon may start a process on a session id,
// or why it may not.
//
// It is a refusal and not a lock, exactly like forkRefusal: nothing stops a
// process appearing a millisecond after it returns. What makes that tolerable
// is that Wake mints these ids and one daemon per socket serves them, so the
// realistic collision is a stray `claude --resume` or an orphan a crashed
// daemon left - both of which this sees.
//
// "I could not check" is a refusal. The failure it prevents has no symptom on
// any wire: two processes under one id each answer correctly from their own
// history, the file branches in place, and whoever resumes it next silently
// does not have half of it (2026-08-09 findings §5).
func (s *server) resumeSafe(id string) error {
	if !mintedByWake(id) {
		return fmt.Errorf("%q is not an id Wake minted, so nothing recorded under it can be matched to a process", id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	in, err := idsInUse(ctx, []string{id})
	if err != nil {
		return fmt.Errorf("could not check whether anything is still running under session %s, and "+
			"resuming an id a second process holds branches the transcript with no error anywhere: %w", id, err)
	}
	if in[id] {
		return fmt.Errorf("a process is still running under session %s, so resuming it would put two "+
			"processes on one transcript and silently lose one of them; close it there first", id)
	}
	return nil
}

// unpark brings a parked session back.
//
// Named unpark rather than wake because `wake` is the binary's name everywhere
// else in this tree, and a method called wake on the server is one grep away
// from being unreadable.
//
// It is not routed through the agent's input queue the way park is, and that
// is the difference between them: park is a write to a running process's
// stdin, and this one has no process to write to.
// It has two doors, and they are the two ways a session gets parked. An agent
// this daemon parked with ⌃C is still in s.agents holding its name; one a
// previous daemon left behind exists only as a park book record, because
// nothing is restored at startup any more. The second is resolved from the
// book, which is also where the name has to be claimed rather than kept.
func (s *server) unpark(ctx context.Context, c *client, f rpc.Frame) {
	if s.stopping(ctx) {
		c.enqueue(errorFrame(f.SessionID, "the daemon is shutting down"))
		return
	}
	a, ok := s.agent(f.SessionID)
	if !ok {
		s.unparkRecord(c, f.SessionID)
		return
	}
	if !a.isParked() {
		c.enqueue(errorFrame(f.SessionID, "session "+f.SessionID+" is not parked, so there is nothing to bring back"))
		return
	}
	if a.dir == "" {
		// forkSource's refusal, for forkSource's reason, arriving through the
		// door the park book opened. Every agent this daemon *started* has a
		// directory - spawnDir falls back to the daemon's own - so before there
		// was a file on disk this could not happen. A record can now come from a
		// book an older build wrote or somebody edited, and launching on it
		// would resume in whatever directory the terminal that forked this
		// daemon happened to be in. claude locates a transcript by the working
		// directory it was started in, so that opens an empty session under the
		// right id and nothing says the history is missing (2026-08-10 findings
		// §12). The row stays parked and stays listed, because the id is still
		// true and it is the only thing that reaches the transcript.
		c.enqueue(errorFrame(f.SessionID, "this daemon does not know where session "+f.SessionID+
			" ran, and a wake has to run there: claude locates a transcript by the directory it was started in"))
		return
	}
	if err := s.resumeSafe(a.id); err != nil {
		c.enqueue(errorFrame(f.SessionID, err.Error()))
		return
	}
	rec, reserved, err := s.parked.reserve(a.id)
	if errors.Is(err, errParkReservationHeld) {
		c.enqueue(errorFrame(a.id, "session "+a.id+" is already being brought back by something else"))
		return
	}
	if err != nil {
		c.enqueue(errorFrame(a.id, "could not reserve the parked session for wake: "+err.Error()))
		return
	}
	// The parent edge travels with it. Nothing on claude's wire says a session
	// was forked or from what, so this is Wake's own memory and it is the only
	// copy - a wake that dropped it would lose the fork's ancestry silently.
	budget, fallback := a.currentSpend()
	s.launch(c, core.Config{
		SessionID:      a.id,
		ResumeFrom:     a.id,
		Name:           a.name,
		Dir:            a.dir,
		PermissionMode: spawnPermissionMode,
		// Sanitised rather than passed: a session set to a level only
		// `/effort` takes is holding one `--effort` refuses, and launch is the
		// one door and refuses rather than dropping. See argvEffort.
		Effort: argvEffort(a.currentEffort(), a.id),
		// Not sanitised, because there is nothing to sanitise against: any
		// non-empty model may go on a command line. See rpc.Frame.Model.
		Model: a.currentModel(),
		// Carried as written, for Model's reason: both were checked before this
		// session was started, and there is no runtime command that could have
		// moved either since.
		MaxBudgetUSD:  budget,
		FallbackModel: fallback,
	}, a.parent, a, s.parkLaunchOutcome(rec, reserved))
}

// unparkRecord brings back a session this daemon never held: a park book entry
// left by a previous one.
//
// Every check unpark makes against a live agent has to be made again here
// against the record, because a record is a file somebody may have edited and
// an agent is not. The order is the one thing that is not obvious: resumeSafe
// runs before reservation, and exact-generation reservation runs before name
// allocation. Only the reservation owner may consume a name, especially the
// one reserved manager name whose value also selects tools and scope.
func (s *server) unparkRecord(c *client, id string) {
	rec, ok, recordErr := s.parked.wakeRecord(id)
	if errors.Is(recordErr, errParkReservationHeld) {
		c.enqueue(errorFrame(id, "session "+id+" is already being brought back by something else"))
		return
	}
	if !ok {
		c.enqueue(errorFrame(id, "unknown session "+id))
		return
	}
	if !mintedByWake(rec.ID) {
		c.enqueue(errorFrame(id, fmt.Sprintf("%q is not an id Wake minted, so nothing can be resumed under it", rec.ID)))
		return
	}
	if rec.Dir == "" {
		// unpark's refusal for the same reason, arriving through the book
		// instead of through an agent: claude locates a transcript by the
		// directory it was started in, so a wake without one opens an empty
		// session under the right id and nothing says the history is missing.
		c.enqueue(errorFrame(id, "this daemon does not know where session "+id+
			" ran, and a wake has to run there: claude locates a transcript by the directory it was started in"))
		return
	}
	if err := s.resumeSafe(rec.ID); err != nil {
		c.enqueue(errorFrame(id, err.Error()))
		return
	}
	reserved, err := s.parked.reserveExact(rec)
	if errors.Is(err, errParkReservationHeld) {
		c.enqueue(errorFrame(id, "session "+id+" is already being brought back by something else"))
		return
	}
	if err != nil {
		c.enqueue(errorFrame(id, "could not reserve the parked session for wake: "+err.Error()))
		return
	}
	if !reserved {
		c.enqueue(errorFrame(id, "parked session "+id+" changed while it was being checked; try bringing it back again"))
		return
	}
	name, err := s.restoredName(rec.Name)
	if err != nil {
		if rec.Name == core.ManagerName {
			s.parked.release(rec.ID)
			c.enqueue(errorFrame(id, "cannot bring back @"+core.ManagerName+" while another manager owns that name"))
			return
		}
		// A display name is never allowed to be the reason somebody cannot
		// start an agent (names.go), and it is not allowed to be the reason one
		// cannot come back either. The name may be held by a session started
		// since the park, which is now ordinary rather than exotic: nothing
		// claims a parked name at startup any more.
		logf("wake: parked session %s cannot have the name %q back (%v), so it is coming back under another", rec.ID, rec.Name, err)
		if name, err = s.names.claim(""); err != nil {
			s.parked.release(rec.ID)
			c.enqueue(errorFrame(id, "no name is free for session "+id+": "+err.Error()))
			return
		}
	}
	// No parent: parkbook.go's header rules that ancestry is not persisted, so
	// a fork resumed from a book draws no `forked from` line.
	// Two narrowings, and they are different questions. bookEffort asks what
	// the *record* may claim - the command's set, so a real `/effort
	// ultracode` is not erased by a daemon restart - and argvEffort asks what
	// may go on a command line, which is narrower. The model has no set to
	// check against, so it is carried as written; see parkedRecord.Model.
	effort := bookEffort(rec)
	// Narrowed for bookEffort's reason and not carried as written: an earlier
	// version of this line trusted the row because the spawn path had checked
	// it, which is only true of rows this build wrote. See bookSpend.
	budget, chain := bookSpend(rec)
	s.launch(c, core.Config{
		SessionID:      rec.ID,
		ResumeFrom:     rec.ID,
		Name:           name,
		Dir:            rec.Dir,
		PermissionMode: spawnPermissionMode,
		Effort:         argvEffort(effort, rec.ID),
		Model:          rec.Model,
		MaxBudgetUSD:   budget,
		FallbackModel:  chain,
	}, "", nil, s.parkLaunchOutcome(rec, true))
}

func (s *server) parkLaunchOutcome(rec parkedRecord, reserved bool) func(bool) {
	if !reserved {
		return nil
	}
	return func(launched bool) {
		s.settleParkReservation(rec, true, launched)
		if launched || !s.parked.isDurable(rec) {
			return
		}
		if a, held := s.agent(rec.ID); held {
			a.markParkDurable(rec)
		}
	}
}

func (s *server) settleParkReservation(rec parkedRecord, reserved, launched bool) {
	if !reserved {
		return
	}
	if launched {
		if err := s.parked.commit(rec); err != nil {
			logf("wake: session %s is running but could not be deleted from the durable park book: %v", rec.ID, err)
		}
		return
	}
	s.parked.release(rec.ID)
}

// bookEffort is the effort a record may put on a command line: the level it
// stored, or none at all.
//
// A park book is a file on disk. A row that has been edited or corrupted must
// not put an arbitrary string on a command line - and must still come back,
// because refusing to wake a session over a bad decoration is worse than waking
// it without one. launch refuses an invalid level outright, so the dropping has
// to happen in front of it rather than inside it.
func bookEffort(rec parkedRecord) string {
	if rec.Effort == "" || core.ValidEffortCommand(rec.Effort) {
		return rec.Effort
	}
	logf("wake: parked session %s names effort %q, which is not a level, so it is coming back without one", rec.ID, rec.Effort)
	return ""
}

// bookSpend is the cap and the chain a record may put on a command line: the
// ones it stored, or nothing.
//
// bookEffort's rule, applied to the two fields this branch added, and applied
// because a review caught them without it: a wake builds its Config from
// parked.json, so these had reached an argv unchecked since a spawn frame is the
// only place the values were tested. The same finding was made against effort
// one release earlier, which is why the answer is already written here.
//
// **The direction this fails in costs something, unlike effort's.** A dropped
// level is a decoration; a dropped cap is a session coming back **uncapped** -
// what every session did before the flag existed, but not what the operator
// asked for. It is still the better trade than refusing the wake, because
// refusing costs the whole session over an edited decoration and the operator
// has no other way back to it. So the log names the consequence rather than only
// the field.
//
// Each field is judged alone: one corrupt value must not take the other with it.
func bookSpend(rec parkedRecord) (budget, chain string) {
	budget, chain = rec.MaxBudgetUSD, rec.FallbackModel
	if budget != "" && !core.ValidBudget(budget) {
		logf("wake: parked session %s names a spend ceiling of %q, which is not an amount, so it is coming back **uncapped**", rec.ID, budget)
		budget = ""
	}
	if chain != "" && !core.ValidFallbackModel(chain) {
		logf("wake: parked session %s names a failover chain of %q, which names a model with no name, so it is coming back with no chain", rec.ID, chain)
		chain = ""
	}
	return budget, chain
}

// restoredName gives a parked session its name back, including the one name a
// client may not ask for.
//
// The manager is the case this exists for, and it is not an edge: ⌃Q parks
// **every** session, so a fleet parked on the way out always has a manager
// record in the book. claim goes through normalizeName, which refuses the
// reserved words - so without this branch the manager comes back under a pooled
// name, addressed by nobody, holding none of the configuration manager.go keys
// on that name, and reported by `wake status` as an ordinary agent. The failure
// is silent and the log line about it is the daemon's, which nobody is reading.
//
// It is the *record* that decides, and the record was written by a daemon that
// had named that session itself: the reserved name is unreachable from a client
// at every door, so an entry carrying it came from a manager or from a book
// somebody edited by hand - and the second of those gets a manager, which is
// the same session it says it is.
func (s *server) restoredName(recorded string) (string, error) {
	if recorded == core.ManagerName {
		return s.names.claimManager()
	}
	return s.names.claim(recorded)
}

// The row is taken before the process is started, and what that buys.
//
// This is the fix `docs/notes/deferred.md` made a precondition of the task that
// binds a key to park: *"binding a key to wake is the change that makes this
// reachable, and the row-before-process fix lands there or the key does not."*
//
// The hazard is two wakes of **one** id, which two client connections can
// produce because each is dispatched on its own goroutine. With the process
// started first, the loser of the race for the row has **already spawned a
// second `claude` on the id** - and `resumeSafe` provably could not have seen
// the winner's process, because it may not have existed when the loser looked.
// Two live processes on one id branch the transcript in place with
// last-writer-wins and **no error on any wire** (2026-08-09 findings §5), so
// there is nothing to detect afterwards. Taking the row first turns that into a
// refusal with nothing started.

package daemon

import (
	"go/ast"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// noClaudeAnywhere leaves PATH with nothing on it, so `claude` cannot be
// resolved and core.Session.Start fails at exec.
//
// That is the discriminator this file is built on: with no claude anywhere, a
// launch that reaches Start *cannot* come back with anything but an exec
// failure. So a refusal that names the row is proof the process was never
// attempted - which is the property, stated as something a test can see rather
// than as a line number.
func noClaudeAnywhere(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// firstRefusal is the one error frame a client was handed, and nothing else.
func firstRefusal(t *testing.T, c *client) rpc.Frame {
	t.Helper()
	select {
	case f := <-c.out:
		if f.Kind != rpc.FrameError {
			t.Fatalf("the first frame the client got is a %s, want an error: %+v", f.Kind, f)
		}
		return f
	default:
		t.Fatal("the client was told nothing at all: a launch that does not start a session owes the " +
			"client that asked for it a reason, or it waits forever with no deadline by design")
		return rpc.Frame{}
	}
}

// parkedRow is a session in the fleet with no process behind it: what ⌃C leaves
// behind, which is the one kind of parked row a daemon still holds.
func parkedRow(t *testing.T, s *server, id, name string) *agent {
	t.Helper()
	a := parkedAgentRow(parkedRecord{ID: id, Name: name, Label: "dev", Dir: t.TempDir()}, name)
	if _, err := s.names.claim(name); err != nil {
		t.Fatalf("claim %q: %v", name, err)
	}
	if !s.register(a) {
		t.Fatalf("session %s could not be put in the fleet, so there is no row for this test to race for", id)
	}
	return a
}

// The loser of two wakes of one id is refused **before** a second claude
// exists, and that is the whole of the fix.
//
// It is driven through launch directly rather than through two connections,
// because the window this closes is between `s.agent()` and the row being
// taken and no test can sit inside it. What a test *can* do is hand launch the
// exact state the loser reaches: a `replaces` that is no longer the row,
// because the winner already swapped its own agent in. Everything below that
// point is the same code the loser runs.
//
// The assertion is on **which refusal** comes back. With no claude on PATH,
// Start cannot succeed - so a refusal naming the row proves admit ran first,
// and an exec failure proves it did not. That is a property of what the daemon
// did rather than of where a line sits, and the static guard below closes the
// same property from the other side.
func TestAWakeThatLosesTheRowForItsIdStartsNoProcess(t *testing.T) {
	noClaudeAnywhere(t)
	s := newServer(tempSocket(t))
	c := newClient(nil)

	// The row as unpark read it, and then the winner's agent in its place.
	was := parkedRow(t, s, idAlpha, "alex")
	winner := newAgent(idAlpha, "alex", "dev", was.dir, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	if !s.replaceParked(winner, was) {
		t.Fatal("the winner could not take the row it is holding for this test, so the launch below is " +
			"not racing anything")
	}

	s.launch(c, core.Config{SessionID: idAlpha, ResumeFrom: idAlpha, Name: "alex", Dir: was.dir}, "", was, nil)

	why := firstRefusal(t, c).Text
	if strings.Contains(why, "executable file not found") || strings.Contains(why, "exec") {
		t.Errorf("the losing wake was refused with %q, which is an exec failure - so it started a "+
			"process before it found out it had lost the row. That second claude carries the same "+
			"--resume as the winner's, both are accepted, both answer from their own history, and the "+
			"transcript branches in place with last-writer-wins and no error on any wire", why)
	}
	if !strings.Contains(why, idAlpha) {
		t.Errorf("the losing wake was refused with %q, which does not name the session: the operator "+
			"pressed a key and has to be told which conversation it was about", why)
	}
	if strings.Contains(why, "already exists") {
		t.Errorf("the losing wake was refused with %q, which is the *spawn* sentence. A wake's session "+
			"obviously already exists - that is what makes it a wake - so this says nothing about what "+
			"happened, which was that something else brought it back first", why)
	}

	// The winner keeps the row, and the name stays claimed. A release here
	// hands @alex to whoever spawns next while the real alex is running under
	// it.
	if got := s.agents[idAlpha]; got != winner {
		t.Errorf("the losing wake left %p in the row and the winner is %p: a loser that takes the row "+
			"anyway puts the fleet's only record of this session on an agent with no process", got, winner)
	}
	if _, err := s.names.claim("alex"); err == nil {
		t.Error("the losing wake released the name, so a spawn can now be handed @alex while the " +
			"session that owns it is running under it")
	}
}

// replaceParked replaces the row it read and refuses every other, and this is
// the check the whole fix rests on.
//
// It is asserted directly because the mutation that gutted it - `s.agents[a.id]
// != was` replaced by `false`, *accept whatever is in the row* - survived the
// entire suite when it was found, for the plain reason that nothing called this
// function with a row it should refuse. A guard nothing exercises negatively is
// a guard whose negative answer has never been seen.
//
// Pointer identity rather than a state check is what makes it atomic: asking
// the old agent whether it is still parked would mean taking its lock under
// s.mu, and the answer would be stale the moment the lock was released.
func TestReplaceParkedTakesTheRowItReadAndRefusesAnyOther(t *testing.T) {
	s := newServer(tempSocket(t))
	was := parkedRow(t, s, idAlpha, "alex")
	stranger := newAgent(idAlpha, "alex", "dev", was.dir, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	woken := newAgent(idAlpha, "alex", "dev", was.dir, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	if s.replaceParked(woken, stranger) {
		t.Fatalf("a wake replaced the row while holding an agent that is not in it. `was` is the exact "+
			"agent unpark inspected, so anything else means somebody has already taken this id - and "+
			"letting it through is a second process on session %s", idAlpha)
	}
	if got := s.agents[idAlpha]; got != was {
		t.Errorf("the refused replace changed the row to %p, want the %p it found: a refusal that "+
			"writes is not a refusal", got, was)
	}
	if !s.replaceParked(woken, was) {
		t.Fatal("a wake holding the row it read could not take it, so the guard above refuses everything " +
			"and no session can ever come back")
	}
	if got := s.agents[idAlpha]; got != woken {
		t.Errorf("the row is %p after a replace that reported success, want the woken agent %p", got, woken)
	}
}

// A launch that cannot start its process puts the row back the way it found it.
//
// The row is taken first now, so it is this function's job to undo that -
// otherwise a spawn that fails at exec leaves the fleet holding a session with
// no process, no fan-out goroutine and therefore no ending: a row that reports
// idle forever and can never be stopped, which is worse than the failed spawn
// it came from.
//
// The remembered ending is the second half and it is restored for the same
// reason: register drops it so a respawned id is not reported alive *and* dead,
// and once the launch has failed the id is not alive, so the ending is true
// again. Without this a failed respawn of an ended session silently deletes
// what `wake status` knew about how it ended.
func TestALaunchThatCannotStartPutsTheRowBackAndKeepsTheEnding(t *testing.T) {
	noClaudeAnywhere(t)
	s := newServer(tempSocket(t))
	c := newClient(nil)

	s.mu.Lock()
	s.rememberLocked(rpc.SessionStatus{ID: idAlpha, Name: "alex", State: rpc.StateEnded, Error: "exit status 2"})
	s.mu.Unlock()

	name, err := s.names.claim("alex")
	if err != nil {
		t.Fatalf("claim alex: %v", err)
	}
	s.launch(c, core.Config{SessionID: idAlpha, Name: name, Dir: t.TempDir()}, "", nil, nil)

	if _, still := s.agents[idAlpha]; still {
		t.Errorf("a spawn that could not start left session %s in the fleet. Nothing fans out for it, so "+
			"nothing will ever retire it: it reports as a live agent forever and `wake stop` counts it", idAlpha)
	}
	if _, err := s.names.claim("alex"); err != nil {
		t.Error("a spawn that never produced an agent kept its name claimed, so the pool leaks a name " +
			"per failed spawn and nothing can be called alex again")
	}
	s.names.release("alex")

	if got := sessionRow(s.fleet(), idAlpha); got.Error != "exit status 2" {
		t.Errorf("after a failed spawn on an id that had ended, `wake status` reports %+v: taking the row "+
			"drops the remembered ending so a session is never alive and dead at once, and a launch that "+
			"then fails has to put it back or the only account of how that session died is gone", got)
	}
}

// The ordering itself, read out of launch's own statement list.
//
// This is rung 6 and it is the same move TestTheParkBookIsWrittenEarlyAndForgottenLate
// makes: the test above observes a *consequence* at an edge, and the property is
// "this statement runs before that one", which the code declares as statement
// order. Put Start back above admit and the behavioural test still fails - but
// only because PATH was emptied for it. On a machine with a claude the loser
// spawns one, wins nothing, and no fixture anywhere sees it.
func TestLaunchTakesTheRowBeforeItStartsAProcess(t *testing.T) {
	body := functionBody(t, "spawn.go", "launch")

	const earlier, later = "s.admitLive(", "sess.StartObserved("
	earlierAt, laterAt := -1, -1
	for i, stmt := range body.List {
		text := stmtText(t, stmt)
		if earlierAt < 0 && strings.Contains(text, earlier) {
			earlierAt = i
		}
		if laterAt < 0 && strings.Contains(text, later) {
			laterAt = i
		}
	}
	switch {
	case earlierAt < 0 || laterAt < 0:
		t.Fatalf("launch has no top-level statement containing %q (found at %d) or %q (found at %d), so "+
			"this orders nothing and the test asserts nothing. Either the work moved out of launch - in "+
			"which case this test has to follow it - or it is gone:\n%s",
			earlier, earlierAt, later, laterAt, stmtText(t, body))
	case earlierAt > laterAt:
		t.Errorf("launch runs %q at statement %d and %q at statement %d, so the process is started before "+
			"the row is taken. The loser of two wakes of one id then has a second claude running under it "+
			"already, resumeSafe could not have seen the winner's, and two live processes on one id "+
			"branch the transcript with last-writer-wins and no error anywhere", earlier, earlierAt, later, laterAt)
	}

	// An index is a happens-before only while the statement runs on this
	// goroutine. `go s.admit(...)` keeps the line exactly where it is and
	// destroys the property outright - and so does `s.start(func() { s.admit(...) })`,
	// which is this package's *own* goroutine helper and is the spelling
	// somebody here would actually reach for. A guard naming only the two
	// keywords misses it, which is the same hole one call deeper.
	ast.Inspect(body, func(n ast.Node) bool {
		var keyword string
		switch stmt := n.(type) {
		case *ast.GoStmt:
			keyword = "go"
		case *ast.DeferStmt:
			keyword = "defer"
		case *ast.ExprStmt:
			call, ok := stmt.X.(*ast.CallExpr)
			if !ok || !strings.Contains(stmtText(t, call.Fun), "s.start") {
				return true
			}
			keyword = "s.start"
		default:
			return true
		}
		text := stmtText(t, n)
		for _, anchor := range []string{earlier, later} {
			if strings.Contains(text, anchor) {
				t.Errorf("launch runs %q under a `%s`: `%s`. Its position in the statement list is then "+
					"no longer when it happens, and the row can be taken after the process exists",
					anchor, keyword, text)
			}
		}
		return true
	})
}

// A spawn that wins the map race after takeAgents must be refused, because the
// map it would enter is one shutdown no longer reads: its process would
// outlive the grace, the kill and the roster clear - the leaked claude
// deferred.md's entry describes, with the fix it prescribes (a flag written
// under s.mu in takeAgents, read by register and replaceParked).
//
// Driven through launch with the fleet already taken, which is the exact state
// the loser of the race reaches: maySpawn's stopping check passed before the
// quit landed, and everything after it is the same code. With no claude on
// PATH the refusals are distinguishable - an exec failure would mean the spawn
// was admitted and only failed to exec, which on a machine with a claude is
// the leak.
func TestAdmissionIsRefusedOnceTheFleetIsTaken(t *testing.T) {
	noClaudeAnywhere(t)
	s := newServer(tempSocket(t))
	c := newClient(nil)

	_ = s.takeAgents()

	name, err := s.names.claim("alex")
	if err != nil {
		t.Fatalf("claim alex: %v", err)
	}
	s.launch(c, core.Config{SessionID: idAlpha, Name: name, Dir: t.TempDir()}, "", nil, nil)

	why := firstRefusal(t, c).Text
	if !strings.Contains(why, "shutting down") {
		t.Errorf("a launch after takeAgents was refused with %q, want the shutdown refusal: anything else "+
			"means it was admitted into a map nothing reads again", why)
	}
	if _, still := s.agents[idAlpha]; still {
		t.Errorf("a launch refused during shutdown left session %s in the fleet", idAlpha)
	}
	if _, err := s.names.claim("alex"); err != nil {
		t.Error("a launch refused during shutdown kept its name claimed")
	}
}

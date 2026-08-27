package main

// The bare-`wake` branch held from the side an example cannot reach: which
// states count as a fleet, over the whole space a report can actually carry
// rather than the two a fixture happens to hold, and whether the offer is said
// by the one path that opens a room.
//
// Split off openroom_test.go by subject, the way forkguard_test.go is split off
// fork_test.go. The tests there drive a daemon and prove the branch is wired to
// something; these prove it is *total*.
//
// # What this does not close, stated here rather than left for a reviewer
//
// The unit is `hasFleet`, not the decision. A second rule in `openRoom` itself -
// `if !hasFleet(st) || len(st.Sessions) > 20` - reads no field of a session, is
// invisible to every check below, and would be killed only by the fixture that
// happens to cross it. That is rung 5's "the narrowing moves to the caller", and
// it is not closed here because `openRoom` is two statements whose whole subject
// is the branch: the argv guard scopes to a call graph because `buildArgs`
// reaches a dozen helpers, and this one reaches one. **If `openRoom` grows a
// second decision, this boundary is the thing to revisit.**

import (
	"go/ast"
	"path/filepath"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// fleetStates is the verdict per state: does a session in it mean there is a
// room to reopen.
//
// **The domain here is wider than any other guard in this package**, and that is
// the finding rather than a detail. Every other reader of a state - forkParent,
// liveSession - sits behind resolveSession, which refuses a report whose Running
// is false, so rpc.StateOrphaned cannot reach them. hasFleet is handed
// daemon.Status's answer with no such filter, and daemon.Status returns
// daemon.FleetOnDisk **when the dial fails**. So this is the one call site where both
// producers can be heard from, and the table has to cover both.
//
// The verdicts and their reasons are in hasFleet's own comment; the one worth
// naming here is the orphan, because it is the cell that differs from "is this
// row interesting". Serve runs reapOrphans before restoreParked and before it
// accepts anything, so the daemon connect() forks ends exactly those processes
// on the way up: counting them opens the room on rows that are being killed as
// it draws. That reaping is gated on `lock.exclusive` and the cell survives the
// exception — see hasFleet, where the second reason is written down.
//
// Hand-written and checked against a derived set rather than being the derived
// set, so an eighth state is a build failure until somebody rules on it.
var fleetStates = map[string]bool{
	rpc.StateIdle:     true,
	rpc.StateWorking:  true,
	rpc.StateBlocked:  true,
	rpc.StateSilent:   true,
	rpc.StateParked:   true,
	rpc.StateEnded:    false,
	rpc.StateOrphaned: false,
}

// unreachableAtTheFrontDoor is the state rpc declares that neither producer can
// put in front of hasFleet, with the producer that is the reason.
//
// **Empty, and that is the assertion rather than a gap**: between them
// agent.stateLocked and daemon.FleetOnDisk write every state rpc declares, so every
// one of them has a verdict above. A state that stops being produced moves here
// and carries its reason, which is what makes deleting the code that handles it
// a decision rather than a guess.
var unreachableAtTheFrontDoor = map[string]string{}

// The states bare `wake` can be handed are the ones the two producers write, and
// every one of them has a verdict.
//
// Asserted in both directions, so this is not a comment: a state either producer
// starts writing must gain a verdict, and a state that stops being written must
// move to the excused map.
func TestTheStatesTheFrontDoorCanBeHandedAreTheOnesEitherProducerWrites(t *testing.T) {
	declared := sessionStateConstants(t)
	reachable := statesAnyStatusCanCarry(t)

	for name, state := range declared {
		_, decided := fleetStates[state]
		why, excused := unreachableAtTheFrontDoor[state]
		switch {
		case decided && excused:
			t.Errorf("rpc.%s = %q is both decided here and excused as unreachable (%s): one of the two is wrong", name, state, why)
		case !decided && !excused:
			t.Errorf("rpc.%s = %q is a state daemon.Status can put in front of bare `wake`, and "+
				"nothing here says whether it counts as a fleet or why it cannot arrive", name, state)
		case decided && !reachable[state]:
			t.Errorf("rpc.%s = %q has a verdict here, but neither agent.stateLocked nor "+
				"daemon.FleetOnDisk writes it - so the cell asserts something about an input no "+
				"report can carry. That reads as coverage and is worse than no cell at all", name, state)
		case excused && reachable[state]:
			t.Errorf("rpc.%s = %q is excused here as unreachable (%s), but a producer writes it now, "+
				"so bare `wake` needs a decision about it", name, state, why)
		}
	}
	for state := range reachable {
		if _, decided := fleetStates[state]; !decided {
			t.Errorf("a status report can carry %q and nothing here says whether bare `wake` calls it a fleet", state)
		}
	}
}

// And hasFleet agrees with every cell.
//
// The mutant this exists for is the one the two branch tests cannot see: each of
// them drives a single state, so `s.State != rpc.StateEnded && s.State !=
// rpc.StateParked` - *a fleet means something is running* - keeps them both
// green while sending every ⌃Q restore down the first-run path.
func TestHasFleetAnswersEveryStateAReportCanCarry(t *testing.T) {
	for state, want := range fleetStates {
		t.Run(state, func(t *testing.T) {
			st := rpc.Status{Sessions: []rpc.SessionStatus{{ID: idAlpha, Name: "alex", State: state}}}
			if got := hasFleet(st); got != want {
				t.Errorf("hasFleet on a fleet of one %s session = %v, want %v", state, got, want)
			}
		})
	}

	// The floor: an empty report is not a fleet, whatever the table says. Bare
	// `wake` on a machine that has never run one is the case a new user hits
	// exactly once, and a hasFleet that returned true unconditionally would pass
	// every positive cell above.
	if hasFleet(rpc.Status{Running: true}) {
		t.Error("hasFleet says an empty fleet is a fleet, so first run opens an empty room and " +
			"produces no agent at all")
	}
}

// hasFleetMayRead is the one field the decision is a function of.
var hasFleetMayRead = map[string]bool{"State": true}

// hasFleetMayReadOfTheReport is the one field of the report itself.
var hasFleetMayReadOfTheReport = map[string]bool{"Sessions": true}

// hasFleet decides from the state and from nothing else, proved about the
// function rather than sampled over values of its fields.
//
// # Why the table above cannot do this job
//
// It walks every state a report can carry, which closes narrowings on a member
// of a closed set. A field's **value** space is neither declared nor closed, so
// no finite sample closes it. Both of these survive the table with the whole
// package green:
//
//	if s.State != rpc.StateEnded && s.QuietMS < 60_000 { return true }
//	if s.State != rpc.StateEnded && s.Error == "" { return true }
//
// The first is a sentence somebody writes - *"a session nobody has heard from in
// an hour is not a room worth reopening"* - and it is wrong here for a reason
// worth stating: a parked fleet's rows are quiet by definition, and the longer
// they have been parked the more certainly this drops them. Adding a QuietMS
// fixture answers that mutant and not the class. The closing move is to deny the
// function the field.
//
// **st.Running is deliberately not readable either.** It is the tempting one -
// "a fleet means a daemon" is exactly the trap this branch exists to avoid - and
// a report from daemon.FleetOnDisk carries Running false with the whole parked fleet
// on it.
func TestHasFleetReadsNothingButTheState(t *testing.T) {
	fn := funcDecl(t, "openroom.go", "hasFleet")

	assertReadsOnly(t, fn, rangeValueName(t, fn), hasFleetMayRead, notReturnable,
		"Whether there is a room to reopen is a function of the state alone: any other field is a "+
			"second rule about which sessions count, and the one it will be written about - how long "+
			"a session has been quiet - drops precisely the parked fleet this branch exists for")

	assertReadsOnly(t, fn, paramName(t, fn, 0), hasFleetMayReadOfTheReport, notReturnable,
		"st.Running is what this branch must not be: a fleet parked by ⌃Q has no daemon, so its "+
			"report carries Running false and every session in it")
}

// The branch is taken on the report fleetToReopen assembles, and on nothing
// else.
//
// TestBareWakeFindsTheParkedFleetWhileTheDaemonIsStillShuttingDown is about
// fleetToReopen, because the rest of openRoom opens a terminal — so what nothing
// behavioural can see is openRoom going back to asking `daemon.Status` itself.
// That is not a hypothetical shape: it is the shape this task shipped, and the
// error it discards is the one ⌃Q produces.
func TestTheFrontDoorBranchesOnTheReportThatKnowsAboutTheDisk(t *testing.T) {
	fn := funcDecl(t, "openroom.go", "openRoom")

	if got := argumentTo(t, fn, "reopensRoom", 0); !callsFunction(got, "fleetToReopen") {
		t.Errorf("openRoom branches on %s. A daemon in graceful shutdown answers daemon.Status with "+
			"an error and a zero report, and a zero report has no sessions in it - so asking it "+
			"directly spawns a fresh agent beside a fleet ⌃Q just parked", exprText(got))
	}
	// Spelled with the package qualifier, because that is what the scan sees: a
	// selector renders as `daemon.Status`, and a bare "Status" here would match
	// nothing and report the strongest possible pass for the weakest possible
	// reason. Written as "Status" in the first draft of this guard and caught by
	// reading it rather than by running it, which is the argument for the
	// floors every other scan in this package carries.
	if callsFunctionNamed(fn, statusCall) {
		t.Errorf("openRoom calls %s itself. The question it has is not `what is running` but `is "+
			"there a room to reopen`, and those differ exactly when the daemon is there and will not "+
			"answer - which is the state ⌃Q leaves and the one this branch exists for", statusCall)
	}
}

// statusCall is daemon.Status as the AST scan renders it. Named so the guard
// above and the floor below cannot disagree about the spelling.
const statusCall = "daemon.Status"

// And the floor that stops the check above being vacuous: fleetToReopen has to
// be calling the thing openRoom is forbidden to call.
//
// Without it, a rename of daemon.Status - or a typo in the constant - leaves the
// guard matching nothing and passing, which is the failure it exists to prevent
// arriving one level up.
func TestTheGuardOnTheFrontDoorIsLookingForACallThatExists(t *testing.T) {
	if fn := funcDecl(t, "openroom.go", "fleetToReopen"); !callsFunctionNamed(fn, statusCall) {
		t.Fatalf("fleetToReopen does not call %s, so the check that openRoom must not call it is "+
			"matching nothing and asserting nothing", statusCall)
	}
}

// Bare `wake` runs the room's model, and the wire from the branch to it is the
// half no test in this package can watch.
//
// # Why this is not a "calls the right function" check for its own sake
//
// Because the wrong one **compiles and very nearly works**. WithOpenDM("")
// returns the model untouched, so `converse(socket, rpc.SessionStatus{}, …)`
// here would open the same room with the same seed and differ in one invisible
// thing: its dialer would be redial on an empty session id, which asks
// liveSession about an id nothing holds and refuses every reattach. The symptom
// is a hang-up that never comes back - after a window drag, on the one path
// that has no session to reattach to - and it is not reachable from any test
// that does not own a terminal.
func TestTheRoomBareWakeOpensIsTheOneWithTheRoomsWayBack(t *testing.T) {
	// First run reaches it too, which is what makes the rest of this test a
	// statement about bare `wake` rather than about one of its two branches. It
	// used to call `attach`, which spawns and then opens that agent's
	// conversation beside the room - so on the only path a new user takes, the
	// surface they typed `wake` to see was the narrower half of a split, and
	// below dmTakeoverColumns not drawn at all. The screen test holds what is on
	// screen; this holds the wire, because `attach` still compiles here and
	// differs in a model rather than in an error.
	front := funcDecl(t, "openroom.go", "openRoom")
	if callsFunctionNamed(front, "attach") {
		t.Error("openRoom calls attach, so first run opens the agent it just spawned and the room " +
			"gets what is left. Bare `wake` is a request about the fleet: the session is a roster " +
			"row, and the pane it would take is one nobody asked for")
	}

	only := funcDecl(t, "openroom.go", "conversationOnly")
	if !callsFunctionNamed(only, "converseRoom") {
		t.Error("conversationOnly does not run the room's model. `converse` builds one with a DM " +
			"and a session dialer, and bare `wake` has neither a target nor an id to reattach to")
	}

	room := funcDecl(t, "attach.go", "converseRoom")
	if got := argumentTo(t, room, "converseModel", 1); !callsFunction(got, "conversationRoom") {
		t.Errorf("converseRoom runs %s. The model is where the two flows differ and the exit line is "+
			"where they must not: converseModel exists so ⌃Q's sentence is written once",
			exprText(got))
	}
}

// callsFunctionNamed reports whether a function calls another by that name.
func callsFunctionNamed(fn *ast.FuncDecl, name string) bool {
	called := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && exprText(call.Fun) == name {
			called = true
		}
		return !called
	})
	return called
}

// rangeValueName is the value a function ranges over, failing rather than
// guessing when there is not exactly one such loop - a scan that matched nothing
// must not read as a scan that found nothing wrong.
func rangeValueName(t *testing.T, fn *ast.FuncDecl) string {
	t.Helper()

	var names []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		rng, ok := n.(*ast.RangeStmt)
		if !ok || rng.Value == nil {
			return true
		}
		if id, ok := rng.Value.(*ast.Ident); ok && id.Name != "_" {
			names = append(names, id.Name)
		}
		return true
	})
	if len(names) != 1 {
		t.Fatalf("%s ranges with %d named values, and this guard is written for the single row it "+
			"judges: a second loop is a place for the check to move to where the scan cannot see it",
			fn.Name.Name, len(names))
	}
	return names[0]
}

// statesAnyStatusCanCarry is the reachable half, and it is the union of the two
// producers because daemon.Status has two answers.
//
// agent.stateLocked writes every row of a report from a running daemon;
// daemon.FleetOnDisk writes every row of the one returned when the dial fails, which
// is the answer a fleet parked by ⌃Q produces and the whole reason this branch
// is not "is a daemon running".
func statesAnyStatusCanCarry(t *testing.T) map[string]bool {
	t.Helper()

	out := statesARunningDaemonReports(t)
	for state := range statesADeadDaemonReports(t) {
		out[state] = true
	}
	return out
}

// statesADeadDaemonReports is the states daemon.FleetOnDisk writes.
func statesADeadDaemonReports(t *testing.T) map[string]bool {
	t.Helper()

	byName := sessionStateConstants(t)
	fn := funcDeclIn(t, filepath.Join("..", "..", "internal", "daemon", "daemon.go"), "FleetOnDisk")

	out := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "rpc" {
			return true
		}
		if value, declared := byName[sel.Sel.Name]; declared {
			out[value] = true
		}
		return true
	})
	if len(out) == 0 {
		t.Fatalf("daemon.FleetOnDisk names no rpc.State… constant: the scan is broken, and every " +
			"reachability claim resting on it is asserting nothing")
	}
	return out
}

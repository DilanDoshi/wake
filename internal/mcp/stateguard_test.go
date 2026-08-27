package mcp

// Which sessions the manager is offered, held over the whole state space a
// report can carry rather than over the two a fixture happens to hold.
//
// # Why this file exists, and why it is the one that mattered most
//
// A whole-branch review added a new member to rpc's state set, made
// agent.stateLocked return it, and counted which guards demanded a verdict.
// **Six did, across four packages** - the attention rank, ⌃F's arrival rule,
// ⌃C's park rule, `wake fork`'s parent rule, bare `wake`'s front door and the
// daemon's fork refusal. `go test ./internal/mcp` stayed green.
//
// Three things made that the gap worth closing rather than a symmetric one:
//
//  1. **It was the only state filter in the tree written the unsafe way round.**
//     Every other one is an allow-list, so an unknown state falls to a default
//     that refuses or to a table that fails the build. This was a **deny-list**:
//     a state nobody had ruled on was *included*, and offered to the manager as
//     an agent to act on.
//  2. **The branch it shipped on is what made it stale-able.** rpc.StateParked
//     was added and this exact line was hand-edited to exclude it, with eight
//     lines of comment reaching the right answer - in the one place nothing
//     would have demanded it. The next state gets no such prompt.
//  3. **The reader is a model, unsupervised.** Every other filter guards a
//     human's keystroke. This one decides what list_agents shows a manager
//     session, and the acting tools are what it does next. A row with no
//     process behind it that a manager "most confidently addresses" is a send
//     that goes nowhere, silently, in a loop nobody is watching.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// agentStates is the verdict per state: is a session in it one the manager is
// offered as something to act on.
//
// **StateParked is the ruling this table exists to keep**, restated
// deliberately rather than inherited. A parked session is excluded, and it is
// the exclusion that needs an argument because it is the one that does not look
// like the others: park is *designed* to keep the name, the label and the
// directory, so a parked row reads exactly like a live one from every angle a
// model can see. What it has not got is a process. Two consequences decide it:
// a send to it is a frame the daemon refuses, so offering it costs the manager
// a turn and a refusal it cannot have predicted from the row; and `/resume` is
// a **human's** verb - `internal/mcp` exposes no wake tool at all, deliberately,
// because waking starts a second process on an id whose first one may not be
// gone and `resumeSafe` is what stands between that and a silently branched
// transcript. A manager offered a parked agent has no correct move available.
//
// agent_status still answers for it. That is the split ended already has and it
// is the whole reason exclusion here is cheap: this list is what a manager
// *chooses from*, not what it may ask about, so nothing is hidden - it is only
// kept out of the roster a recipient is picked from.
//
// StateEnded is the same rule one state further on and needs no argument.
// StateOrphaned is a process this daemon lost track of, which the next daemon's
// reaper kills on its way up; offering it is offering a corpse.
//
// Blocked is *included*, and it is the include that is not obvious. A blocked
// agent has a live process and is stopped dead waiting for a human - which is
// exactly the row a manager is most useful for noticing. Hiding it would make
// the surface whose job is awareness quietly blind in the one state where
// somebody needs to act.
//
// Hand-written and checked against a derived set rather than being the derived
// set, so an eighth state is a build failure until somebody rules on it.
var agentStates = map[string]bool{
	rpc.StateIdle:     true,
	rpc.StateWorking:  true,
	rpc.StateBlocked:  true,
	rpc.StateSilent:   true,
	rpc.StateParked:   false,
	rpc.StateEnded:    false,
	rpc.StateOrphaned: false,
}

// unreachableInAFleetReport is the state rpc declares that no producer can put
// in front of liveSessions, with the producer that is the reason.
//
// **Empty, and that is the assertion rather than a gap**: between them
// agent.stateLocked and daemon.FleetOnDisk write every state rpc declares, so
// every one has a verdict above. A state that stops being produced moves here
// and carries its reason, which is what makes deleting the code that handles it
// a decision rather than a guess.
var unreachableInAFleetReport = map[string]string{}

// The states the manager's roster can be handed are the ones the two producers
// write, and every one of them has a verdict.
//
// **The domain is the union of both producers**, not the running one alone, for
// the reason bare `wake`'s front door gives: Fleet.List is an interface, and the
// implementation this package is written for is daemon.Status, which has two
// answers - the live fleet from daemon.fleet(), and daemon.FleetOnDisk when the
// dial fails. The shipped filter already excluded StateOrphaned, which only
// FleetOnDisk writes, so the wider domain is what the code was written against
// even though nothing said so.
//
// Asserted in both directions, so this is not a comment: a state either producer
// starts writing must gain a verdict, and a state that stops being written must
// move to the excused map.
func TestTheStatesTheManagersRosterCanBeHandedAreTheOnesEitherProducerWrites(t *testing.T) {
	declared := sessionStateConstants(t)
	reachable := statesAnyStatusCanCarry(t)

	for name, state := range declared {
		_, decided := agentStates[state]
		why, excused := unreachableInAFleetReport[state]
		switch {
		case decided && excused:
			t.Errorf("rpc.%s = %q is both decided here and excused as unreachable (%s): one of the two is wrong", name, state, why)
		case !decided && !excused:
			t.Errorf("rpc.%s = %q is a state a fleet report can carry, and nothing here says whether "+
				"list_agents offers a session in it to the manager or why it cannot arrive. This is the "+
				"one filter whose reader is a model rather than a person, so an unruled state is a row "+
				"something addresses without being asked to think about it", name, state)
		case decided && !reachable[state]:
			t.Errorf("rpc.%s = %q has a verdict here, but neither agent.stateLocked nor "+
				"daemon.FleetOnDisk writes it - so the cell asserts something about an input no report "+
				"can carry, which reads as coverage", name, state)
		case excused && reachable[state]:
			t.Errorf("rpc.%s = %q is excused here as unreachable (%s), but a producer writes it now, "+
				"so the manager's roster needs a decision about it", name, state, why)
		}
	}
	for state := range reachable {
		if _, decided := agentStates[state]; !decided {
			t.Errorf("a fleet report can carry %q and nothing here says whether the manager is offered it", state)
		}
	}
}

// And liveSessions agrees with every cell.
//
// One state at a time, because the mutant that matters is a narrowing: a filter
// that keeps ended out and lets parked through passes every example test in this
// package, since none of them holds both.
func TestListAgentsAnswersEveryStateAReportCanCarry(t *testing.T) {
	for state, want := range agentStates {
		t.Run(state, func(t *testing.T) {
			st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
				{ID: idPeter, Name: "peter", Label: "api-v2", State: state},
			}}
			got := len(liveSessions(st)) == 1
			if got != want {
				t.Errorf("liveSessions over a fleet of one %q session offers it = %v, want %v", state, got, want)
			}
		})
	}
}

// A state no producer writes **yet** is not offered, and this is the assertion
// the whole file is for.
//
// It is the difference between an allow-list and a deny-list, stated as a
// behaviour rather than as a shape: the filter is handed a value that is not in
// rpc's constant block at all - which is precisely what the next state looks
// like from here, on the commit before somebody adds it to the table above - and
// must leave it out.
//
// **The table cannot make this assertion**, because a table only ever covers the
// states somebody wrote down. The totality test above turns a new state into a
// build failure, and this one decides what happens in the window before anybody
// runs it: the row is withheld from a model rather than offered to it. Withheld
// is the recoverable direction - a manager that cannot see an agent asks about
// one it can, and a human still sees every row in the room - while offered is a
// send into a session with no process behind it, which fails silently and in a
// loop.
//
// This is the mutant the review found alive: `if s.State == rpc.StateEnded ||
// s.State == rpc.StateOrphaned || s.State == rpc.StateParked { continue }` is
// green against every other test in this package and fails here.
func TestAStateNoProducerWritesYetIsNotOfferedToTheManager(t *testing.T) {
	const invented = "draining"

	if _, declared := sessionStateConstants(t)[stateConstantFor(t, invented)]; declared {
		t.Fatalf("%q is a state rpc declares now, so this test is no longer about an unruled one: "+
			"give it a verdict in agentStates and invent a different value here", invented)
	}

	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idPeter, Name: "peter", Label: "api-v2", State: rpc.StateIdle},
		{ID: idJohn, Name: "john", Label: "api-v2", State: invented},
	}}
	live := liveSessions(st)

	for _, s := range live {
		if s.State == invented {
			t.Errorf("liveSessions offered the manager a session in state %q, which nothing in this "+
				"tree has ruled on. An unknown state defaulting to *included* is how a row with no "+
				"process behind it reaches a model as an agent to send to: the send goes nowhere, "+
				"there is no error on any wire, and nobody is watching", invented)
		}
	}
	if len(live) != 1 {
		t.Fatalf("liveSessions dropped the idle session too (%d rows), so this test is asserting "+
			"nothing about the unruled one", len(live))
	}
}

// stateConstantFor is the rpc constant name declaring a value, or "" - so the
// guard above can say "rpc does not declare this" without hard-coding a name
// that would go stale the moment somebody adds one.
func stateConstantFor(t *testing.T, value string) string {
	t.Helper()

	for name, declared := range sessionStateConstants(t) {
		if declared == value {
			return name
		}
	}
	return ""
}

// --- the derived domain -------------------------------------------------------
//
// Re-spelled here rather than shared, which is the same trade cmd/wake and
// internal/ui already make with these three functions. A test helper exported
// across packages would be a fifth thing to keep in step, and the scans are
// short; what must not drift is the *producer* they read, and each of them
// fails loudly if it finds nothing.

// sessionStateConstants reads every `State… = "…"` constant rpc declares.
//
// Globbed rather than pointed at lifecycle.go, which is where the states live
// today, because a state constant added to wire.go would otherwise leave the
// count right and the scan blind.
func sessionStateConstants(t *testing.T) map[string]string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("..", "rpc", "*.go"))
	if err != nil {
		t.Fatalf("glob the rpc package: %v", err)
	}
	out := map[string]string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		for name, value := range stringConstants(t, file, "State") {
			out[name] = value
		}
	}
	if len(out) == 0 {
		t.Fatalf("no State… constants found across %v: the scan is broken and the test over it is asserting nothing", files)
	}
	return out
}

// statesAnyStatusCanCarry is the reachable half, and it is the union of the two
// producers because daemon.Status has two answers.
func statesAnyStatusCanCarry(t *testing.T) map[string]bool {
	t.Helper()

	out := statesNamedIn(t, filepath.Join("..", "daemon", "agent.go"), "stateLocked")
	for state := range statesNamedIn(t, filepath.Join("..", "daemon", "daemon.go"), "FleetOnDisk") {
		out[state] = true
	}
	return out
}

// statesNamedIn is every rpc.State… constant one producer names, resolved back
// to its value through the constant scan - so a state referred to by a name rpc
// does not declare is a build failure in Go before it is one here.
func statesNamedIn(t *testing.T, file, fn string) map[string]bool {
	t.Helper()

	byName := sessionStateConstants(t)
	out := map[string]bool{}
	ast.Inspect(funcDeclIn(t, file, fn).Body, func(n ast.Node) bool {
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
		t.Fatalf("%s names no rpc.State… constant: the scan is broken, and every reachability claim "+
			"resting on it is asserting nothing", fn)
	}
	return out
}

// funcDeclIn finds one function - method or not - in one file, failing rather
// than returning nil: a scan that found nothing must not read as a scan that
// found nothing wrong.
func funcDeclIn(t *testing.T, file, name string) *ast.FuncDecl {
	t.Helper()

	var found *ast.FuncDecl
	for _, decl := range parseGoFile(t, file).Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name || fn.Body == nil {
			continue
		}
		if found != nil {
			t.Fatalf("%s declares more than one %s, so this scan cannot say which one produces the "+
				"states it is reading", file, name)
		}
		found = fn
	}
	if found == nil {
		t.Fatalf("no func %s in %s: it was renamed or moved, and the test over it is asserting nothing", name, file)
	}
	return found
}

// stringConstants returns the `Name = "value"` constants in one file whose
// names start with a prefix.
func stringConstants(t *testing.T, file, prefix string) map[string]string {
	t.Helper()

	out := map[string]string{}
	for _, decl := range parseGoFile(t, file).Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			lit, ok := vs.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || !strings.HasPrefix(vs.Names[0].Name, prefix) {
				continue
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("unquote %s in %s: %v", vs.Names[0].Name, file, err)
			}
			out[vs.Names[0].Name] = value
		}
	}
	return out
}

// parseGoFile parses one file outside this package. parsePackage covers this
// package's own files and is deliberately not widened: it enumerates a
// directory, and these three read named files in two others.
func parseGoFile(t *testing.T, file string) *ast.File {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}
	return f
}

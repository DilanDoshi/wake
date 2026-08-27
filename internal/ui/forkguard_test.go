// The ⌃F guards held from the side an example cannot reach: which reported
// states the fork's own conversation opens in, over the whole domain a running
// daemon can produce rather than the one state a fixture happens to carry, and
// what the decision is allowed to read, proved about the function rather than
// about one run of it.
//
// Split off fork_test.go by subject, the same way cmd/wake/forkguard_test.go is
// split off cmd/wake/fork_test.go - and the scans below are that file's,
// re-spelled here because a test helper cannot cross a package boundary. If one
// of the two is ever fixed, fix both: they read the same two producers.
//
// Why this file exists at all is docs/notes/decisions.md's ladder. fork_test.go
// drives a fork arriving idle, which kills the mutant that deletes the arm and
// says nothing about the narrowing that is what the next edit actually looks
// like - `s.State != rpc.StateIdle`, which loses exactly the fork that was
// already working by the time the first report naming it arrived. Fan-out
// starts before the confirmation is enqueued, so that report is reachable.

package ui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// forkArrivalStates is what ⌃F does when the report that first names the fork
// carries each state a running daemon can report: true is "the fork's own
// conversation opens", false is "it does not".
//
// Ended is the one false, and it is a decision rather than an oversight. A
// conversation whose process is already gone is the empty pane with a
// working-looking header that cmd/wake.reattach exists to prevent, and a fork
// that died on startup produces a FrameError addressed to its own id, which is
// what clears the wait. What is left over - a fork that started and exited
// cleanly before any report named it - is recorded in docs/notes/deferred.md.
//
// A hand-written decision checked against a derived set rather than the derived
// set itself, so a sixth reachable state is a build failure until somebody says
// what ⌃F does with it.
var forkArrivalStates = map[string]bool{
	rpc.StateIdle:    true,
	rpc.StateWorking: true,
	rpc.StateBlocked: true,
	rpc.StateSilent:  true,
	rpc.StateEnded:   false,
	// Parked joins ended, and for ended's reason rather than by analogy: a pane
	// with no process behind it is the empty conversation with a
	// working-looking header cmd/wake.reattach exists to prevent.
	//
	// **Revisited 2026-08-11 by park/wake Task 6, which built `/resume`, and
	// left false.** The cell asked to be looked at again the day a parked pane
	// stopped being a dead end, and that day arrived: a bare `/resume` typed
	// into one brings the session back. What did not change is what the pane
	// holds - a fork the first report already calls parked has emitted no
	// events, so opening it is an empty transcript under a live-looking header,
	// and the operator pressed ⌃F for a conversation rather than for one they
	// have to revive. isTheAwaitedStart's header carries the full argument.
	rpc.StateParked: false,
}

// unreachableInAForkReport is the state rpc declares that no report reaching
// this model can carry, with the producer that is the reason.
//
// StateOrphaned is written in exactly one place, daemon.FleetOnDisk, which is
// what daemon.Status returns when the dial fails - a report assembled by the
// *client* from the on-disk roster, never a frame off a socket. App.applyStatus
// only ever sees a FrameStatusPush or a FrameStatusReply, both of which come
// from daemon.fleet(), whose rows come from agent.stateLocked.
var unreachableInAForkReport = map[string]string{
	rpc.StateOrphaned: "daemon.FleetOnDisk, which assembles a report for a daemon that is not there and never crosses the socket",
}

// The reachable domain, derived from the producer rather than from the type,
// and asserted in both directions.
//
// This is rung 4 of docs/notes/decisions.md's ladder, which Phase 3 Task 3 was
// burned by one package over: an AST scan over rpc's *constant block* is a
// superset of what can arrive, and a verdict over an input that cannot occur
// reads as coverage while pinning whatever code handles it.
func TestTheStatesAForkCanArriveInAreTheOnesARunningDaemonProduces(t *testing.T) {
	declared := sessionStateConstants(t)
	reachable := statesARunningDaemonReports(t)

	for name, state := range declared {
		_, decided := forkArrivalStates[state]
		why, excused := unreachableInAForkReport[state]
		switch {
		case decided && excused:
			t.Errorf("rpc.%s = %q is both decided here and excused as unreachable (%s): one of the two is wrong", name, state, why)
		case !decided && !excused:
			t.Errorf("rpc.%s = %q is a state a session can be reported in, and nothing here says whether "+
				"⌃F opens the fork's conversation on it or why it cannot arrive", name, state)
		case decided && !reachable[state]:
			t.Errorf("rpc.%s = %q has a verdict here, but agent.stateLocked never returns it - so the cell "+
				"asserts something about an input no daemon can produce, which reads as coverage", name, state)
		case excused && reachable[state]:
			t.Errorf("rpc.%s = %q is excused here as unreachable (%s), but agent.stateLocked returns it now",
				name, state, why)
		}
	}
	for state := range reachable {
		if _, decided := forkArrivalStates[state]; !decided {
			t.Errorf("agent.stateLocked can report %q and nothing here says what ⌃F does with it", state)
		}
	}
}

// The behaviour asserted per member, over every state the fork's first report
// can carry rather than the idle one fork_test.go happens to drive.
//
// The narrowing this kills and an example cannot: an arm that opens the pane
// only for an idle fork. A fork that is already working when the daemon first
// reports it then never opens, and pendingFork stays set - so the next session
// to be given that id steals the pane, which is the failure the id is carried
// for in the first place.
func TestTheForkOpensInEveryStateItsFirstReportCanCarry(t *testing.T) {
	for state, opens := range forkArrivalStates {
		t.Run(state, func(t *testing.T) {
			fresh(t)
			conn, sent := pipeClient(t)
			a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

			m, cmd, _ := a.key(tea.KeyMsg{Type: tea.KeyCtrlF})
			a = m.(App)
			go func() { _ = runCmdQuietly(cmd) }()
			forkID := awaitFrame(t, sent).SessionID

			a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
				Running: true,
				Sessions: []rpc.SessionStatus{
					{ID: "s1", Name: "alex", State: rpc.StateIdle},
					{ID: forkID, Name: "sydney", State: state, ParentID: "s1"},
				},
			}})

			// "On every confirmed fork" is the artefacts' wording, so it is
			// asserted for every state a confirmation can arrive in rather
			// than for the idle one an example happens to drive. Reporting the
			// sentence only for an idle fork is a one-word narrowing that
			// nothing else in this package can see.
			if opens && !strings.Contains(shown(a), SnapshotNotice("sydney", "alex")) {
				t.Errorf("a fork first reported %q opened without saying it is a snapshot:\n%s", state, shown(a))
			}

			switch {
			case opens && a.focus != forkID:
				t.Errorf("a fork first reported %q left the pane on %q: the daemon has started it, so this is a conversation the operator asked for and cannot reach", state, a.focus)
			case opens && len(a.pendingStarts) != 0:
				t.Errorf("a fork first reported %q arrived and %d forks are still pending: the next session given that id would steal the pane", state, len(a.pendingStarts))
			case !opens && a.focus == forkID:
				t.Errorf("a fork first reported %q opened a pane anyway: %q is the one state whose conversation has nothing behind it", state, state)
			}
		})
	}
}

// And the value dimension, which no finite table closes: the decision is the
// id and the state, and nothing else.
//
// Rung 3. The enumeration above closes narrowing on a *member* of a closed
// declared domain; it says nothing about `s.QuietMS > 100_000` or `s.Name == ""`
// or a helper one call down, each of which is a sentence somebody writes and
// each of which every fixture walks straight past. A contract of the form "the
// answer depends on X alone" is a statement about what the code may *read*, and
// reading is static.
func TestTheDecisionThatAStartHasArrivedReadsNothingButTheIdAndTheState(t *testing.T) {
	const (
		file  = "starts.go"
		fn    = "isTheAwaitedStart"
		param = "s"
	)
	allowed := map[string]bool{"ID": true, "State": true}

	decl := funcDeclIn(t, file, fn)
	seen := map[string]bool{}
	mentions := 0

	ast.Inspect(decl.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if ok {
			if id, isIdent := sel.X.(*ast.Ident); isIdent && id.Name == param {
				mentions++
				seen[sel.Sel.Name] = true
				if !allowed[sel.Sel.Name] {
					t.Errorf("%s reads %s.%s: whether a report names the fork this client is waiting for "+
						"is the id and the state alone, and any other field is a narrowing no table of "+
						"values can close", fn, param, sel.Sel.Name)
				}
				return false
			}
		}
		id, ok := n.(*ast.Ident)
		if ok && id.Name == param {
			mentions++
			t.Errorf("%s mentions %s outside a selector: passing the whole row to a helper moves the "+
				"comparison one call down, where a check over selectors alone cannot see it", fn, param)
		}
		return true
	})

	// The vacuity floor, which is this project's own named failure: a renamed
	// function, a renamed parameter or a broken parse all yield zero matches,
	// and "reads nothing" would then be the strongest possible pass for the
	// weakest possible reason.
	if mentions == 0 {
		t.Fatalf("%s never mentions %s: the scan is broken and this test is asserting nothing", fn, param)
	}
	for field := range allowed {
		if !seen[field] {
			t.Errorf("%s never reads %s.%s: the scan found reads and not that one, so either the "+
				"decision has stopped depending on it or this list has", fn, param, field)
		}
	}
}

// The id ⌃F waits on is the id it minted, proved about the function rather than
// about one run of it.
//
// Rung 3 again, on the other half. The behavioural tests read the id **off the
// frame** and the fixture's parent is `"s1"`, so a total substitution
// (`awaitingStart(agent.ID)`) dies three times over. The mutant that lives is the
// subset - mint into a local, send the local, and wait on the *parent's* id for
// some class of parents - because every fixture supplies a parent outside that
// class and keeps the good behaviour. `cmd/wake`'s `mintedOnce` was written for
// exactly this mutant one commit before ⌃F existed; this is that guard, ported.
//
// What a mismatch costs is why it is worth a static assertion: the wait has no
// deadline by design, so waiting on the wrong id is not a failure. It is a fork
// that runs, and a pane that never opens, forever.
func TestTheIdCtrlFWaitsOnIsTheOneItMintedAndNeverTheParents(t *testing.T) {
	fn := funcDeclIn(t, "fork.go", "fork")
	minted := mintedOnceIn(t, fn, "uuid.NewString")

	if waited := soleArgumentTo(t, fn, "a.awaitingStart", 0); exprName(waited) != minted {
		t.Errorf("⌃F waits on %s rather than on %s. startArrived matches the daemon's report against "+
			"that set and nothing times out, so a mismatch here is a fork that runs and a pane that "+
			"never opens", exprName(waited), minted)
	}

	frame := soleCompositeLit(t, fn, "rpc.Frame")
	if sent := fieldIn(t, frame, "SessionID"); exprName(sent) != minted {
		t.Errorf("the frame is written with %s rather than with %s, so the daemon runs the fork under "+
			"an id nothing is waiting on", exprName(sent), minted)
	}
	if parent := fieldIn(t, frame, "ParentID"); exprName(parent) == minted {
		t.Errorf("the frame names %s as both the fork and its parent, which asks the daemon to fork a "+
			"session into itself", minted)
	}
}

// writersOfPendingStarts is every function allowed to change the set ⌃F waits
// on, with what it does. Checked against an AST scan of the whole package in
// both directions.
//
// **Rung 5, and this task is where the ladder needed it.** Rungs 1-4 all ask
// the same question at higher resolution: given an input to this decision, is
// the verdict right, and is the input space the one that can arrive?
// `isTheAwaitedStart` satisfies every one of them - and three defects sat outside
// all four, because none of them is about what the decision *reads*:
//
//   - a second ⌃F **overwrote** the slot, losing the first fork;
//   - a mutant could write the slot a **value** the frame does not carry;
//   - the refusal arm could **clear** it without keying on the id.
//
// All three are about who else writes the state the decision reads. So:
// enumerate the writers, not just the inputs - derived from the code, with a
// verdict required per writer, so a third writer is a build failure until
// somebody says what it means.
var writersOfPendingStarts = map[string]string{
	"awaitingStart": "adds the id the verb just minted - ⌃F's fork or `/new`'s agent. The only place the set grows, so a second press is a second member rather than a replacement",
	"startSettled":  "removes one id, because it arrived or the daemon refused it. Keyed on the id in both callers, so an error about another agent leaves the rest alone",
}

func TestTheOnlyWritersOfThePendingStartSetAreTheOnesWithAVerdictHere(t *testing.T) {
	found := writersOfField(t, "pendingStarts")
	if len(found) == 0 {
		t.Fatal("nothing in this package writes App.pendingStarts: the scan is broken, and every claim about who may change the wait is asserting nothing")
	}
	for fn := range found {
		if _, decided := writersOfPendingStarts[fn]; !decided {
			t.Errorf("%s writes App.pendingStarts and nothing here says what that write means. Every "+
				"defect this guard exists for was a *second* writer of a one-slot state, so a new one "+
				"is a decision rather than an inheritance", fn)
		}
	}
	for fn, why := range writersOfPendingStarts {
		if !found[fn] {
			t.Errorf("%s is listed here as a writer of App.pendingStarts (%s) and writes it no longer: "+
				"a dead entry is what lets the next real writer look accounted for", fn, why)
		}
	}
}

// --- scans ---------------------------------------------------------------

// writersOfField is every function in this package's non-test files that
// assigns to a field of that name, or sets it in a composite literal.
func writersOfField(t *testing.T, field string) map[string]bool {
	t.Helper()

	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	out := map[string]bool{}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		for _, decl := range parseGoFile(t, name).Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if writesField(fn.Body, field) {
				out[fn.Name.Name] = true
			}
		}
	}
	return out
}

// writesField reports whether one body assigns to that field or names it as a
// key in a struct literal. Both, because an initialiser is a write too.
func writesField(body *ast.BlockStmt, field string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == field {
					found = true
				}
			}
		case *ast.KeyValueExpr:
			if id, ok := n.Key.(*ast.Ident); ok && id.Name == field {
				found = true
			}
		}
		return !found
	})
	return found
}

// mintedOnceIn is the name a function assigns from one call, having established
// that it assigns that name exactly once.
//
// The count is the half that matters, and it is `cmd/wake`'s argument: a branch
// substituting another id for some subset of inputs has to write the name a
// second time, and no fixture can see that - it keeps the good behaviour for
// every input the fixture supplies.
func mintedOnceIn(t *testing.T, fn *ast.FuncDecl, mint string) string {
	t.Helper()

	var minted string
	assignments := map[string]int{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
				assignments[id.Name]++
			}
		}
		if len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok || exprName(call.Fun) != mint {
			return true
		}
		id, ok := as.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		if minted != "" {
			t.Fatalf("%s mints more than one id (%s and %s), and this guard is written for the single "+
				"id the daemon is asked for and this client waits on", fn.Name.Name, minted, id.Name)
		}
		minted = id.Name
		return true
	})

	if minted == "" {
		t.Fatalf("%s assigns nothing from %s: the fork's id has to be minted here, and this scan is "+
			"asserting nothing about where it comes from", fn.Name.Name, mint)
	}
	if n := assignments[minted]; n != 1 {
		t.Errorf("%s assigns %s %d times. It has to be written once: a second assignment is where a "+
			"branch substitutes the parent's id for some subset of parents, which no behavioural test "+
			"can see because each drives one fixture", fn.Name.Name, minted, n)
	}
	return minted
}

// soleArgumentTo is argument n of the one call to name, failing when there is
// not exactly one: a guard over "the call" means nothing once there are two.
func soleArgumentTo(t *testing.T, fn *ast.FuncDecl, name string, n int) ast.Expr {
	t.Helper()

	var found []*ast.CallExpr
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && exprName(call.Fun) == name {
			found = append(found, call)
		}
		return true
	})
	if len(found) != 1 {
		t.Fatalf("%s calls %s %d times, and this guard is written for the one call: it was renamed, "+
			"moved, or duplicated, and either way the assertion over it means nothing",
			fn.Name.Name, name, len(found))
	}
	if len(found[0].Args) <= n {
		t.Fatalf("the call to %s takes %d arguments, so there is no argument %d: the signature changed "+
			"under this guard", name, len(found[0].Args), n)
	}
	return found[0].Args[n]
}

// soleCompositeLit is the one literal of that type in a function.
func soleCompositeLit(t *testing.T, fn *ast.FuncDecl, typeName string) *ast.CompositeLit {
	t.Helper()

	var found []*ast.CompositeLit
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if lit, ok := node.(*ast.CompositeLit); ok && exprName(lit.Type) == typeName {
			found = append(found, lit)
		}
		return true
	})
	if len(found) != 1 {
		t.Fatalf("%s builds %d %s literals, and this guard is written for the one frame ⌃F writes",
			fn.Name.Name, len(found), typeName)
	}
	return found[0]
}

// fieldIn is the value one composite literal gives a named field.
func fieldIn(t *testing.T, lit *ast.CompositeLit, field string) ast.Expr {
	t.Helper()
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if id, ok := kv.Key.(*ast.Ident); ok && id.Name == field {
			return kv.Value
		}
	}
	t.Fatalf("the frame sets no %s: this guard is about which id goes where, and one of them is gone", field)
	return nil
}

// exprName renders the identifiers and selectors these guards compare, so a
// failure names what it found rather than a node address.
func exprName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprName(e.X) + "." + e.Sel.Name
	default:
		return fmt.Sprintf("%T", e)
	}
}

// sessionStateConstants is rpc's State… block: constant name to wire value.
func sessionStateConstants(t *testing.T) map[string]string {
	t.Helper()
	out := stringConstants(t, filepath.Join("..", "rpc", "lifecycle.go"), "State")
	if len(out) == 0 {
		t.Fatal("rpc declares no State… constants: the scan is broken, and every claim resting on it asserts nothing")
	}
	return out
}

// statesARunningDaemonReports is what agent.stateLocked can return - the
// producer, which is the authority on what can arrive rather than on what the
// type permits.
func statesARunningDaemonReports(t *testing.T) map[string]bool {
	t.Helper()

	byName := sessionStateConstants(t)
	fn := funcDeclIn(t, filepath.Join("..", "daemon", "agent.go"), "stateLocked")

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
		t.Fatal("agent.stateLocked names no rpc.State… constant: the scan is broken, and every reachability claim resting on it is asserting nothing")
	}
	return out
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

// funcDeclIn finds one function or method in one file, failing rather than
// returning nil: a scan that found nothing must never read as a scan that found
// nothing wrong.
func funcDeclIn(t *testing.T, file, name string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range parseGoFile(t, file).Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name && fn.Body != nil {
			return fn
		}
	}
	t.Fatalf("%s has no function %q: the scan is looking at the wrong thing", file, name)
	return nil
}

func parseGoFile(t *testing.T, file string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	return f
}

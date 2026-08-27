package ui

// What `/adopt` may build, and which id it may wait on.
//
// # Why these are static
//
// Both properties fail **silently**, which is the whole reason a fixture cannot
// hold them.
//
// The daemon addresses every import refusal to the **new** session's id, and
// neither wait in this project has a deadline by design. So a client that
// remembered the *source's* id is not refused - it waits forever, with nothing
// on screen, which is indistinguishable from a daemon that is thinking. A test
// with two ids in it says nothing about a branch that substitutes one for the
// other for *some subset* of sources, which is what the next edit to this
// function looks like: `wake fork` has the same guard for the same reason, and
// its comment says so in the same words.
//
// And `rpc.FrameImport` is the whole of what makes an import an import. The
// frame kinds beside it are one identifier away and each fails quietly rather
// than loudly: a `FrameSpawn` carrying the same minted id starts an ordinary
// empty agent - exit 0, a name, a roster row, an empty transcript under a
// live-looking header - and a `FrameFork` names a *parent in this fleet*, which
// a source id is not, so it is refused for a reason that has nothing to do with
// what went wrong. Neither is visible to a test that only counts frames.
//
// # The rung
//
// These are rung 5 rather than rung 3: the unit is the **path** `/adopt` takes
// rather than one function, because a mint moved into a helper is invisible to
// a check pointed at `adoptAll`. The path is derived by following the calls
// this package declares, from the router's own entry, rather than listed.

import (
	"go/ast"
	"testing"
)

const (
	// adoptFile is where this command lives.
	adoptFile = "adopt.go"

	// adoptEntry is what the router calls, and adoptFold is what folds the
	// answer. Two roots because the command is split across a tea.Cmd: the
	// walk of the disk happens between them.
	adoptEntry = "adopt"
	adoptFold  = "adoptArrived"

	// mintCall is where a session id comes from. Wake originates identity -
	// maySpawn refuses anything that is not a UUID, because the reaper's only
	// proof of a process group is that id in an argv.
	mintCall = "uuid.NewString"

	// waitCall is the set of starts this client is waiting for.
	waitCall = "awaitingStart"

	// importKind is the one frame kind this command may build.
	importKind = "FrameImport"
)

// Every id `/adopt` waits on is one it minted, and never a source's.
func TestTheIdAdoptWaitsOnIsMintedHereAndNeverTheSources(t *testing.T) {
	fn := adoptingFunc(t, waitCall)

	args := argumentsToCall(fn, waitCall)
	if len(args) == 0 {
		t.Fatalf("nothing on the %s path calls %s: either the wait was dropped - in which case an "+
			"adopted conversation never opens - or this scan is looking at the wrong function",
			adoptEntry, waitCall)
	}
	minted := mintedNames(fn)
	if len(minted) == 0 {
		t.Fatalf("nothing on the %s path calls %s, so the id it asks for is one it was given: an "+
			"import is a fork onto a **new** id, and Wake is what originates it", adoptEntry, mintCall)
	}
	for _, arg := range args {
		name, ok := arg.(*ast.Ident)
		if !ok || !minted[name.Name] {
			t.Errorf("%s waits on %q, which is not an id minted by %s on this path. The daemon "+
				"addresses every import refusal to the *new* session's id and this wait has no "+
				"deadline by design, so a wait on the source's id is never settled and never "+
				"reported - it is a blank pane forever, which looks exactly like a daemon that is "+
				"thinking", fn.Name.Name, exprName(arg), mintCall)
		}
	}
}

// The frame carries the minted id as its own and the source as its parent, and
// never the other way round.
func TestAdoptsFrameNamesTheMintedIdAsItsOwnAndTheSourceAsItsParent(t *testing.T) {
	fn := adoptingFunc(t, waitCall)
	minted := mintedNames(fn)

	fields := frameFieldsBuiltIn(t, fn)
	if len(fields) == 0 {
		t.Fatalf("%s builds no rpc.Frame literal: the scan is broken and every claim resting on it "+
			"asserts nothing", fn.Name.Name)
	}
	for _, field := range []string{"Kind", "SessionID", "ParentID"} {
		if _, built := fields[field]; !built {
			t.Fatalf("the frame %s builds carries no %s. An import is a source and a destination, and "+
				"a frame missing either is refused for a reason that says nothing about what went "+
				"wrong", fn.Name.Name, field)
		}
	}
	if got := exprName(fields["Kind"]); got != "rpc."+importKind {
		t.Errorf("%s builds a %s frame. rpc.%s is the whole of what makes an import an import: a "+
			"spawn under this id starts an ordinary empty agent with a name, a roster row and a "+
			"live-looking header, and a fork names a parent in this fleet, which a source id is not",
			fn.Name.Name, got, importKind)
	}
	if id, ok := fields["SessionID"].(*ast.Ident); !ok || !minted[id.Name] {
		t.Errorf("the frame's SessionID is %q, which this path did not mint. That id is the one the "+
			"daemon starts a session under, and handing it a source's id asks for a second process "+
			"on a transcript somebody may still have open", exprName(fields["SessionID"]))
	}
	if src, ok := fields["ParentID"].(*ast.Ident); ok && minted[src.Name] {
		t.Errorf("the frame's ParentID is %q, which this path minted: the source is the transcript "+
			"being adopted and it is never a fresh id", exprName(fields["ParentID"]))
	}
}

// The whole set is one write.
//
// App.write's rule, and it is about goroutines rather than tidiness: bubbletea
// runs every tea.Cmd on its own goroutine and rpc's write lock is process-wide
// and held across the socket write, so a `/adopt` built as one command per
// session is N goroutines queueing on one lock for one keystroke. `/resume all`
// already carries this and this is the second verb that can be plural.
func TestAdoptWritesTheWholeSetWithOneCommand(t *testing.T) {
	fn := adoptingFunc(t, waitCall)

	if n := callsCounted(fn, "a.write"); n != 1 {
		t.Errorf("%s calls a.write %d times, want once. Ten adopted sessions built as ten commands "+
			"are ten goroutines queueing on one process-wide write lock for one keystroke", fn.Name.Name, n)
	}
	// **One call site is not one write, and counting them is the mistake this
	// project already made one lane over.** `wake import`'s first guard counted
	// resumeSafe's call sites and a reviewer beat it by wrapping the one call
	// in a predicate; here the equivalent is one `a.write` inside a `for`, which
	// keeps the count at one and is N writes at run time. So the call has to be
	// a statement of the function's own body - the move
	// TestAnImportAsksResumeSafeUnconditionally and
	// TestLaunchTakesTheRowBeforeItStartsAProcess both make.
	if !calledFromTopLevel(fn, "a.write") {
		t.Errorf("%s's call to a.write is nested inside something. One call site inside a loop is "+
			"one write per session, which is the shape this assertion exists to forbid and which a "+
			"count of call sites cannot see", fn.Name.Name)
	}
}

// calledFromTopLevel reports whether a call appears in a statement that is a
// direct member of the function's own body - never inside a loop, a branch or a
// closure.
func calledFromTopLevel(fn *ast.FuncDecl, name string) bool {
	for _, stmt := range fn.Body.List {
		switch stmt.(type) {
		case *ast.ForStmt, *ast.RangeStmt, *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			continue
		}
		found := false
		ast.Inspect(stmt, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && exprName(call.Fun) == name {
				found = true
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// The router never reads this machine on the goroutine that draws.
//
// Discovery walks every transcript under ~/.claude/projects - 428 files on the
// recording machine - and Bubble Tea has one Update goroutine and it renders.
// The behavioural test asserts the seam was untouched while the router ran;
// this asserts the shape that makes that true, because "it happened not to be
// called" and "it cannot be called here" are different claims and only the
// second survives an edit.
func TestAdoptAsksTheMachineFromACommandAndNeverFromTheRouter(t *testing.T) {
	fn := funcDeclIn(t, adoptFile, adoptEntry)

	for _, method := range []string{"Listing", "Resolve"} {
		if hasSelector(fn, method) {
			t.Errorf("%s calls %s itself. That is a walk of every transcript on this machine, on the "+
				"one goroutine that draws every pane - so the room freezes for the length of it",
				adoptEntry, method)
		}
	}
}

// --- scans ---------------------------------------------------------------

// adoptingFunc is the function on `/adopt`'s path that calls the named thing,
// found by following this package's own calls from the two roots rather than by
// naming it - so a mint or a wait moved into a helper stays in scope.
func adoptingFunc(t *testing.T, calls string) *ast.FuncDecl {
	t.Helper()

	var found []*ast.FuncDecl
	for _, name := range adoptPath(t) {
		fn := funcDeclIn(t, adoptFile, name)
		if callsNamed(fn, calls) {
			found = append(found, fn)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d functions on the %s path call %s, want exactly one: two places that mint or wait "+
			"cannot both be the one this guard is about", len(found), adoptEntry, calls)
	}
	return found[0]
}

// adoptPath is every function in adopt.go reachable from the router's entry or
// from the fold, by a call this file declares.
//
// Derived rather than listed, which is the rung: a guard pointed at `adoptAll`
// alone is satisfied by moving the mint one call down, and that is what the
// next change to this command looks like.
func adoptPath(t *testing.T) []string {
	t.Helper()

	declared := map[string]*ast.FuncDecl{}
	for _, decl := range parseGoFile(t, adoptFile).Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			declared[fn.Name.Name] = fn
		}
	}
	// Both roots, by name, before anything is walked. A count of what the walk
	// reached is not enough: with one root renamed the other still pulls in
	// most of this file, so the floor below passes while the guards above are
	// asserting over a path with the router's own entry missing from it.
	for _, root := range []string{adoptEntry, adoptFold} {
		if declared[root] == nil {
			t.Fatalf("%s declares no %s, which is one of the two roots this path is walked from: the "+
				"function was renamed and every guard resting on this scan is now about a smaller "+
				"path than it names", adoptFile, root)
		}
	}
	seen, queue := map[string]bool{}, []string{adoptEntry, adoptFold}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] || declared[name] == nil {
			continue
		}
		seen[name] = true
		ast.Inspect(declared[name].Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			for _, candidate := range []string{exprName(call.Fun), lastSelector(call.Fun)} {
				if declared[candidate] != nil && !seen[candidate] {
					queue = append(queue, candidate)
				}
			}
			return true
		})
	}
	if len(seen) < 2 {
		t.Fatalf("the %s path walked %d functions: the roots were renamed and this scan is looking at "+
			"nothing", adoptEntry, len(seen))
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	return out
}

// mintedNames is every local this function bound from a fresh session id.
func mintedNames(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 || len(as.Lhs) != 1 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok || exprName(call.Fun) != mintCall {
			return true
		}
		if id, ok := as.Lhs[0].(*ast.Ident); ok {
			out[id.Name] = true
		}
		return true
	})
	return out
}

// frameFieldsBuiltIn is the fields of the one rpc.Frame literal a function
// builds, failing rather than guessing when there is more than one.
func frameFieldsBuiltIn(t *testing.T, fn *ast.FuncDecl) map[string]ast.Expr {
	t.Helper()

	var literals []*ast.CompositeLit
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if ok && exprName(lit.Type) == "rpc.Frame" {
			literals = append(literals, lit)
		}
		return true
	})
	if len(literals) > 1 {
		t.Fatalf("%s builds %d rpc.Frame literals: two frames cannot both be the one this guard is "+
			"about, and one of them is unasserted", fn.Name.Name, len(literals))
	}
	out := map[string]ast.Expr{}
	for _, lit := range literals {
		for _, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				out[exprName(kv.Key)] = kv.Value
			}
		}
	}
	return out
}

// argumentsToCall is the first argument of every call to one name.
func argumentsToCall(fn *ast.FuncDecl, name string) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		if exprName(call.Fun) == name || lastSelector(call.Fun) == name {
			out = append(out, call.Args[0])
		}
		return true
	})
	return out
}

// callsCounted is how many times a function calls one name.
func callsCounted(fn *ast.FuncDecl, name string) int {
	n := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && exprName(call.Fun) == name {
			n++
		}
		return true
	})
	return n
}

// callsNamed reports whether a function calls one name, by selector or plain.
func callsNamed(fn *ast.FuncDecl, name string) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if exprName(call.Fun) == name || lastSelector(call.Fun) == name {
			found = true
		}
		return !found
	})
	return found
}

// hasSelector reports whether a function calls a method of that name on
// anything.
func hasSelector(fn *ast.FuncDecl, method string) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == method {
			found = true
		}
		return !found
	})
	return found
}

// lastSelector is the method name of a call like `a.awaitingStart(id)`.
func lastSelector(e ast.Expr) string {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		return sel.Sel.Name
	}
	return ""
}

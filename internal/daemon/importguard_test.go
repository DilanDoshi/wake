// Static guards over the import path.
//
// Three properties, none of which any fixture can see, because the fake claude
// on PATH in this suite honours neither --resume nor --fork-session: it prints
// what it was told to print. So an import that resumed instead of forking, or
// ran in the wrong directory, is **green everywhere** and wrong only against a
// real binary, on somebody's real conversation, with no error on any wire.
//
// That is rung 5's shape - the property belongs to the emitted Config, and the
// mutations that violate it read only allowed fields.

package daemon

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func importSessionBody(t *testing.T) (*ast.FuncDecl, *token.FileSet) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "import.go", nil, 0)
	if err != nil {
		t.Fatalf("parse import.go: %v", err)
	}
	var fn *ast.FuncDecl
	ast.Inspect(f, func(n ast.Node) bool {
		if d, ok := n.(*ast.FuncDecl); ok && d.Name.Name == "importSession" {
			fn = d
		}
		return true
	})
	if fn == nil {
		t.Fatal("importSession was not found in import.go: the scan is broken, not the tree")
	}
	return fn, fset
}

// coreConfigFields reads the field names of the one core.Config literal
// importSession builds.
func coreConfigFields(t *testing.T) map[string]ast.Expr {
	t.Helper()
	fn, _ := importSessionBody(t)
	out := map[string]ast.Expr{}
	found := 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Config" {
			return true
		}
		found++
		for _, e := range lit.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if k, ok := kv.Key.(*ast.Ident); ok {
				out[k.Name] = kv.Value
			}
		}
		return true
	})
	if found != 1 {
		t.Fatalf("importSession builds %d core.Config literals, want exactly 1: the scan cannot say which one launches", found)
	}
	return out
}

// **An import is a fork.** The whole safety argument in import.go's header
// rests on this one field name, and swapping it for ResumeFrom is a two-word
// edit that compiles, passes the entire suite, and puts a second live process on
// a stranger's transcript - branching it in place, last-writer-wins, with no
// error on any wire (2026-08-09 findings §5).
//
// It is checkable only here, because nothing in this suite runs a real claude.
func TestAnImportBuildsAForkAndNeverAResume(t *testing.T) {
	fields := coreConfigFields(t)
	if _, resumes := fields["ResumeFrom"]; resumes {
		t.Errorf("importSession sets ResumeFrom. An import adopts a transcript Wake did not write, and Wake " +
			"cannot prove nothing is holding it: 2026-08-12 findings §5 counted four live claude processes whose " +
			"whole argv is the word `claude`, which core.SessionArgvMarkers cannot match. A resume of one of those " +
			"branches the conversation silently. A fork leaves the source's file byte-identical (2026-08-10 findings §5)")
	}
	fork, forks := fields["ForkFrom"]
	if !forks {
		t.Error("importSession sets neither ForkFrom nor ResumeFrom, so it starts a session that inherits nothing: " +
			"an import that adopts no conversation is a fresh empty agent under a name that says otherwise")
		return
	}
	// **Pinned to the verified source, exactly as Dir is**, and for the same
	// reason: *which transcript is forked* is the same class of fact as *which
	// directory it runs in*. `ForkFrom: f.ParentID` - the client's own
	// unvalidated spelling instead of the id discovery matched - compiles and
	// passes every fixture, because findSession matches on exact string
	// equality so the two values are identical today. core.sameSession's header
	// documents what UUID spelling variance costs when they are not.
	sel, ok := fork.(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("importSession sets ForkFrom to a %T rather than a field of the discovered source", fork)
	}
	if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "src" || sel.Sel.Name != "ID" {
		t.Errorf("importSession sets ForkFrom to %s.%s, want src.ID - the id discovery matched on disk",
			exprName(sel.X), sel.Sel.Name)
	}
}

// The directory is the one **discovery proved**, and it may not come from the
// frame.
//
// claude locates a transcript by the directory the process was started in, so a
// directory a client chose is a guess at the one fact that cannot be guessed at.
// A Dir taken off the frame does not fail: it starts a session that finds no
// transcript, inherits nothing, and looks exactly like a working agent
// (2026-08-10 findings §12).
//
// Asserted as a *shape* rather than a value: the field's expression must be a
// selector on the discovered source, so `Dir: f.Dir`, `Dir: spawnDir(f)` and
// `Dir: cwdOrHome()` are each a build failure.
func TestAnImportRunsWhereDiscoveryProvedAndNeverWhereTheFrameSaid(t *testing.T) {
	fields := coreConfigFields(t)
	dir, ok := fields["Dir"]
	if !ok {
		t.Fatal("importSession's core.Config has no Dir, so the session runs wherever the daemon happens to be: " +
			"claude locates a transcript by the directory it was started in, and that is not it")
	}
	sel, ok := dir.(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("importSession sets Dir to a %T rather than a field of the discovered source", dir)
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok || x.Name != "src" || sel.Sel.Name != "Dir" {
		t.Errorf("importSession sets Dir to %s.%s, want src.Dir - the directory verifiedDir proved. "+
			"Anything else is a guess at the one fact claude gives no way to recover",
			exprName(sel.X), sel.Sel.Name)
	}
}

// resumeSafe is asked **unconditionally**, and that is a different assertion
// from "it is called".
//
// The first version of this guard counted call sites, and a reviewer beat it in
// one line:
//
//	if time.Since(src.Modified) < staleAfter {   // "nothing has written to this
//	    if err := s.resumeSafe(sourceID); ...    //  in a week, so it cannot be open"
//	}
//
// One call, still there, and green across both packages - including the test
// that manufactures a live holder, whose fixture writes the transcript in the
// same second so the gate is true and the check still runs. The narrowing is
// *attractive*: it removes a `ps -Aww` from a path an operator repeats, and
// FoundSession.Modified is right there. It is also exactly backwards - a
// session open in a terminal and idle for a month is the **most** likely import
// target - and it turns recency, which discover.go's own doc calls a hint, into
// an authorisation.
//
// So the property is *statement position*, which lives in the AST and nowhere
// else. This is the same move TestLaunchTakesTheRowBeforeItStartsAProcess and
// TestTheParkBookIsWrittenEarlyAndForgottenLate already make.
//
// Earlier `return`s are fine and are the point: they are the cheaper refusals,
// and reaching this statement at all is what must not be conditional.
func TestAnImportAsksResumeSafeUnconditionally(t *testing.T) {
	fn, fset := importSourceBody(t)

	// Every call anywhere in the function, and every call that is a *direct*
	// statement of the body. The two must be the same set.
	all, top := 0, 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if isResumeSafeCall(n) {
			all++
		}
		return true
	})
	for _, stmt := range fn.Body.List {
		if !callsResumeSafeDirectly(stmt) {
			continue
		}
		top++
	}
	if all != 1 {
		t.Fatalf("importSource calls resumeSafe %d times, want exactly 1. It is the only thing that refuses an "+
			"import whose session is visibly still open, and 2026-08-09 findings §5 says this flow owes that "+
			"refusal: the picker cannot learn from claude whether a session is closed, so the flow must say "+
			"close it there first", all)
	}
	if top != 1 {
		t.Errorf("importSource's call to resumeSafe at line %d is nested inside something rather than being a "+
			"statement of the function body: %d of %d calls are unconditional. A guard that counts call sites "+
			"cannot see `if time.Since(src.Modified) < staleAfter { … }` wrapped around it, which keeps the "+
			"count at one and skips the check for every session older than the gate - which is the set most "+
			"likely to still be open in somebody's terminal",
			fset.Position(fn.Body.Pos()).Line, top, all)
	}
}

func isResumeSafeCall(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "resumeSafe"
}

// callsResumeSafeDirectly reports whether a top-level statement *is* the call,
// in either shape that keeps it unconditional: as an `if` statement's Init
// (`if err := s.resumeSafe(id); err != nil`), or as a plain assignment. Nothing
// deeper counts - a call inside an `if` **body** is the mutation this exists to
// kill.
func callsResumeSafeDirectly(stmt ast.Stmt) bool {
	switch st := stmt.(type) {
	case *ast.IfStmt:
		return st.Init != nil && assignCallsResumeSafe(st.Init)
	case *ast.AssignStmt:
		return assignCallsResumeSafe(st)
	case *ast.ExprStmt:
		return isResumeSafeCall(st.X)
	}
	return false
}

func assignCallsResumeSafe(stmt ast.Stmt) bool {
	as, ok := stmt.(*ast.AssignStmt)
	if !ok {
		return false
	}
	for _, rhs := range as.Rhs {
		if isResumeSafeCall(rhs) {
			return true
		}
	}
	return false
}

func importSourceBody(t *testing.T) (*ast.FuncDecl, *token.FileSet) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "import.go", nil, 0)
	if err != nil {
		t.Fatalf("parse import.go: %v", err)
	}
	var fn *ast.FuncDecl
	ast.Inspect(f, func(n ast.Node) bool {
		if d, ok := n.(*ast.FuncDecl); ok && d.Name.Name == "importSource" {
			fn = d
		}
		return true
	})
	if fn == nil {
		t.Fatal("importSource was not found in import.go: the scan is broken, not the tree")
	}
	return fn, fset
}

func exprName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return "?"
}

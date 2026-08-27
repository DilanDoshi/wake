// What the argv path is allowed to read, and what shape its decisions may take.
//
// The behavioural guard over the identity switch is fork_test.go's cross
// product: ForkFrom x ResumeFrom x SessionID, built twice, asserted per cell.
// This file is the static half, and it exists because a cross product over a
// function's declared input cannot see a narrowing keyed on a *value*. Verified, not
// reasoned - `case s.cfg.ForkFrom != "" && s.cfg.Model != "sonnet"` left the
// whole tree green while turning every fork of a session naming that model into
// an ordinary empty agent under the child's id.
//
// # Rung 5: guard the unit the property belongs to, and the predicate's shape
//
// The first version of this file guarded `identityArgs` and asserted which
// Config *fields* it may read. A reviewer beat it three ways in twenty minutes,
// all three green against the whole tree, two of them producing a punished argv
// shape:
//
//	case s.cfg.ForkFrom != "" && !strings.HasPrefix(s.cfg.ForkFrom, "9")
//	   reads only allowed fields, so a field-set check cannot see it.
//	   Forks of one parent in sixteen become an empty agent - UUIDs are hex.
//
//	in buildArgs: if s.cfg.Model == "sonnet" { identity = identity[2:] }
//	   out of scope entirely. buildArgs is the function that assembles the
//	   argv; identityArgs only hands it a block. This one emits
//	   --fork-session with no --resume, which is the silent punished shape.
//
//	case s.cfg.ForkFrom != "" && forkModelAllowed()   // reads os.Getenv
//	   mentions the receiver nowhere at all, which the old comment claimed
//	   to have defeated. It defeats preferredModel(s), not preferredModel().
//
// So two things changed, and they are the rung:
//
//   - **The unit is the argv path, not one function.** The property is about
//     the emitted argv, so the check covers everything that can decide it -
//     buildArgs, and every function this package declares that is reachable
//     from it by a call. Derived by following calls, never listed.
//   - **The predicate's shape is constrained, not only its inputs.** Every
//     expression the path uses as a truth value must be built from a closed
//     grammar: emptiness tests against a Config field, comparisons between
//     plain identifiers, and calls to path functions whose every argument is a
//     Config field. A HasPrefix, a <, a len, an env read or a comparison
//     against any string but "" is a build failure regardless of which field it
//     reads.
//
// # What this still does not close, stated because a boundary a reviewer
// # discovers is worth less than one the file admits
//
//   - **The identity fields' own value space is still sampled.**
//     identityDomain() offers six spellings. Nothing here proves the switch
//     behaves the same for a seventh - only that it cannot *ask* a question
//     about one, which is what makes the sample sufficient rather than lucky.
//
//   - **Value positions are unconstrained.** A cfg field may be appended to the
//     argv anywhere; that is what buildArgs is for. `append(args, s.cfg.Model)`
//     under an emptiness test is the shipped code, and nothing here separates it
//     from `append(args, s.cfg.Model[:1])` - constructed, and every check in
//     this file stayed green. It is caught, behaviourally, by
//     TestBuildArgsCarriesIdentityAndMode, and the identity block has its own
//     static close below; the other four flags rest on that one behavioural
//     test alone.
//
//     **A field whose value is literally a flag name reaches a punished shape
//     through this hole, and park/wake's arm added a second door to it.**
//     `Config{SessionID: "--fork-session"}` builds `--session-id
//     --fork-session`, which reads as the silent shape; `Config{ResumeFrom:
//     "--session-id"}` now builds `--resume --session-id`, which reads as the
//     loud one. Brute-forced over a domain including every identity flag name
//     as a value: every construction of either shape needs one, so with every
//     identity field a plausible id both shapes are unrepresentable. The close
//     is at the producer and not here - daemon.mintedByWake is a uuid.Parse -
//     and it is the same ruling identityArgs' own header makes about validity
//     belonging to the layer that knows which ids this fleet issued.
//
//   - **It is syntax, not types.** A second type with a `cfg` field, or a
//     package-level function that shadows a name, would be resolved by
//     spelling. This package has neither, and the floors below fail loudly if
//     the resolution stops finding what it expects.
//
// One property arrives for free and is worth naming so it is not deleted as
// incidental: because a Config field may only be compared against "", the raw
// string compare that sameSession replaced - `s.cfg.SessionID == s.cfg.ForkFrom`
// - is now a build failure. docs/notes/decisions.md argues that one at length
// (uuid.Parse reads six spellings as one id); this is the same ruling arriving
// as a shape rather than as a sentence.
package core

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"
)

// argvSeed is where the argv is assembled. Everything else in the path is
// reached from here.
const argvSeed = "buildArgs"

// argvPathFloor is what the path must reach, so a rename or a rewire that
// empties it fails loudly instead of passing over nothing. It is a floor rather
// than the definition: a fourth function added to the path is fine and simply
// inherits the rules.
var argvPathFloor = []string{"buildArgs", "identityArgs", "sameSession"}

// identityFields are the Config fields identityArgs is allowed to read.
// Adding one - park and wake added ResumeFrom - is a deliberate edit here, and
// that is the point: fork_test.go's cross product is exhaustive over
// identityArgs' input *because* the input is these three, and a field read
// quietly makes that claim false without failing anything. Extending this list
// without extending that product is how the punished shapes come back.
var identityFields = []string{"ForkFrom", "ResumeFrom", "SessionID"}

// packageFuncs indexes every function this package declares, by name, out of
// its non-test files. Methods and plain functions share one index because the
// call sites this file resolves are distinguishable without types: a method
// call is a selector on the enclosing function's own receiver, and anything
// else selecting through an identifier is another package.
func packageFuncs(t *testing.T) (map[string]*ast.FuncDecl, *token.FileSet) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	fset := token.NewFileSet()
	out := map[string]*ast.FuncDecl{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// Build constraints, evaluated rather than guessed at. procgroup_unix.go
		// and procgroup_other.go declare the same three functions behind
		// //go:build unix and !unix, so a scan that read both would see every
		// one of them twice and call it an ambiguity. This is the same GOOS the
		// test binary was compiled for, so the program scanned is the program
		// that runs.
		ok, merr := build.Default.MatchFile(".", name)
		if merr != nil {
			t.Fatalf("build constraints for %s: %v", name, merr)
		}
		if !ok {
			continue
		}
		parsed, perr := parser.ParseFile(fset, name, readSource(t, name), 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if prev, dup := out[fn.Name.Name]; dup {
				t.Fatalf("%s is declared twice in this package (%s and %s): this file resolves calls by "+
					"name, and two functions sharing one makes every resolution below a guess",
					fn.Name.Name, fset.Position(prev.Pos()), fset.Position(fn.Pos()))
			}
			out[fn.Name.Name] = fn
		}
	}
	if len(out) == 0 {
		t.Fatal("no function declarations found in this package: the parse is broken and every check here would pass over nothing")
	}
	return out, fset
}

// receiverOf returns the name of a function's receiver, or "" for a plain
// function.
func receiverOf(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 {
		return ""
	}
	return fn.Recv.List[0].Names[0].Name
}

// calleeName resolves one call to a function this package declares, or "" for
// anything else. A bare identifier is a package-level function (or a builtin,
// which is not in the index); a selector through the enclosing function's own
// receiver is a method on *Session. A selector through anything else is another
// package and is deliberately unresolvable.
func calleeName(call *ast.CallExpr, recv string, funcs map[string]*ast.FuncDecl) string {
	var name string
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		name = fn.Name
	case *ast.SelectorExpr:
		id, ok := fn.X.(*ast.Ident)
		if !ok || recv == "" || id.Name != recv {
			return ""
		}
		name = fn.Sel.Name
	default:
		return ""
	}
	if _, ok := funcs[name]; !ok {
		return ""
	}
	return name
}

// argvPath is every function that can decide what the argv contains: the seed,
// and everything reachable from it by a call this package declares. Derived by
// walking the call graph so a helper cannot be added outside the rules by
// existing - the rules follow the call.
func argvPath(t *testing.T, funcs map[string]*ast.FuncDecl) []string {
	t.Helper()
	if _, ok := funcs[argvSeed]; !ok {
		t.Fatalf("this package declares no %s: the argv path has no seed and every check over it is scanning nothing", argvSeed)
	}
	seen := map[string]bool{argvSeed: true}
	queue := []string{argvSeed}
	var order []string
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		order = append(order, name)
		fn := funcs[name]
		recv := receiverOf(fn)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if callee := calleeName(call, recv, funcs); callee != "" && !seen[callee] {
				seen[callee] = true
				queue = append(queue, callee)
			}
			return true
		})
	}
	slices.Sort(order)
	return order
}

// The path has to reach what it claims to cover. Without this every check below
// could be scanning one function, or none.
func TestTheArgvPathReachesEveryFunctionThatDecidesTheArgv(t *testing.T) {
	funcs, _ := packageFuncs(t)
	path := argvPath(t, funcs)
	for _, want := range argvPathFloor {
		if !slices.Contains(path, want) {
			t.Errorf("%s is not on the argv path (%v): either it stopped being called from %s, or the call "+
				"resolution is broken - and every static check in this file then holds over a smaller "+
				"program than it names", want, path, argvSeed)
		}
	}
}

// parentOf maps every node under root to the node that encloses it, so a check
// can ask what an expression is *used as* rather than only what it is.
func parentOf(root ast.Node) map[ast.Node]ast.Node {
	out := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			out[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	return out
}

// rangedConfigElements are the identifiers a function binds to an *element* of
// a Config field, by ranging over one.
//
// They are Config data wearing a plain name, and without this they launder the
// rule below straight out of reach: `for _, dir := range s.cfg.AddDir { if dir
// == "/etc" { continue } }` mentions no field, so binaryProblem's
// identifier-versus-literal arm accepts it and one directory in the list is
// silently dropped from the argv. The first slice on Config arrived with
// --add-dir and opened exactly that door.
//
// Only the value is collected. The **key** is an index or a map key, which is
// not Config data - `i == 0` is a question about position, and a position
// cannot encode which session this is.
func rangedConfigElements(fn *ast.FuncDecl, recv string) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		rs, ok := n.(*ast.RangeStmt)
		if !ok || rs.Value == nil {
			return true
		}
		if _, isField := cfgField(rs.X, recv); !isField {
			return true
		}
		if id, ok := rs.Value.(*ast.Ident); ok {
			out[id.Name] = true
		}
		return true
	})
	return out
}

// cfgField reports the Config field an expression reads, for an expression of
// exactly the form <recv>.cfg.<Field>.
func cfgField(e ast.Expr, recv string) (string, bool) {
	outer, ok := e.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	inner, ok := outer.X.(*ast.SelectorExpr)
	if !ok || inner.Sel.Name != "cfg" {
		return "", false
	}
	id, ok := inner.X.(*ast.Ident)
	if !ok || recv == "" || id.Name != recv {
		return "", false
	}
	return outer.Sel.Name, true
}

// Every mention of a *Session receiver on the argv path is <recv>.cfg.<Field>.
//
// This is what stops the comparison moving one call down: preferredModel(s)
// carries no selector, so a check that only inspected selectors would not see
// it. It says nothing about which field - that is the two checks below.
func TestTheArgvPathReadsConfigOnlyThroughItsFields(t *testing.T) {
	funcs, fset := packageFuncs(t)
	mentions := 0
	for _, name := range argvPath(t, funcs) {
		fn := funcs[name]
		recv := receiverOf(fn)
		if recv == "" {
			continue
		}
		parents := parentOf(fn.Body)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || id.Name != recv {
				return true
			}
			mentions++
			if sel, ok := parents[n].(*ast.SelectorExpr); ok {
				// <recv>.cfg.<Field> - a read, and the two checks below say
				// which field and what may be asked of it.
				if sel.Sel.Name == "cfg" {
					if outer, ok := parents[sel].(ast.Expr); ok {
						if _, isField := cfgField(outer, recv); isField {
							return true
						}
					}
				}
				// <recv>.method() - a call onto the path, whose own body is
				// held to these same rules because the path follows the call.
				if call, ok := parents[sel].(*ast.CallExpr); ok && call.Fun == ast.Expr(sel) {
					if calleeName(call, recv, funcs) != "" {
						return true
					}
				}
			}
			t.Errorf("%s mentions %s at %s in something other than %s.cfg.<field> or a call onto the path: "+
				"the argv path may decide what it emits from Config fields alone, and a receiver handed "+
				"anywhere else is a decision no check over those fields can see",
				name, recv, fset.Position(id.Pos()), recv)
			return true
		})
	}
	if mentions == 0 {
		t.Fatal("no function on the argv path mentions its receiver: the scan is broken, and 'reads nothing' is the strongest pass this check has for the weakest reason")
	}
}

// predicatesIn returns every expression the function uses as a truth value.
//
// Return values count when the function returns a single bool, because that is
// how a predicate moves out of the caller: a helper whose body is one return is
// exactly the evasion this file was beaten by.
func predicatesIn(fn *ast.FuncDecl) []ast.Expr {
	boolResult := false
	if r := fn.Type.Results; r != nil && len(r.List) == 1 && len(r.List[0].Names) == 0 {
		id, ok := r.List[0].Type.(*ast.Ident)
		boolResult = ok && id.Name == "bool"
	}

	var out []ast.Expr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.IfStmt:
			out = append(out, s.Cond)
		case *ast.ForStmt:
			if s.Cond != nil {
				out = append(out, s.Cond)
			}
		case *ast.SwitchStmt:
			if s.Tag != nil {
				// A tagged switch is a comparison against every case label, so
				// the tag and the labels are operands rather than predicates.
				out = append(out, s.Tag)
				break
			}
			for _, stmt := range s.Body.List {
				if cc, ok := stmt.(*ast.CaseClause); ok {
					out = append(out, cc.List...)
				}
			}
		case *ast.ReturnStmt:
			if boolResult && len(s.Results) == 1 {
				out = append(out, s.Results[0])
			}
		}
		return true
	})
	return out
}

// predicateProblem reports why an expression is not an allowed truth value, or
// "" if it is. The grammar, and every clause is answering a mutant:
//
//	p && q, p || q, !p          -> each side must itself be allowed
//	true, false                 -> a constant is not a narrowing
//	x == y, x != y              -> operands may be a Config field, a plain
//	                               identifier, or a literal; and a Config
//	                               field - or an element ranged out of one,
//	                               see rangedConfigElements - may only be
//	                               compared against ""
//	f(s.cfg.A, s.cfg.B)         -> a call to a path function, at least one
//	                               argument, every argument a Config field
//
// Everything else is a build failure. That closes strings.HasPrefix (an
// unresolvable call), forkModelAllowed() (no arguments to constrain), <, >, len
// (not == or !=), and s.cfg.Model == "sonnet" (a field compared against
// something other than empty).
func predicateProblem(e ast.Expr, recv string, elems map[string]bool, path []string, funcs map[string]*ast.FuncDecl) string {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return predicateProblem(x.X, recv, elems, path, funcs)
	case *ast.UnaryExpr:
		if x.Op != token.NOT {
			return "a unary " + x.Op.String() + " is not a truth value this path may build"
		}
		return predicateProblem(x.X, recv, elems, path, funcs)
	case *ast.Ident:
		if x.Name == "true" || x.Name == "false" {
			return ""
		}
		return "the bare identifier " + x.Name + " decides something this check cannot read"
	case *ast.BinaryExpr:
		return binaryProblem(x, recv, elems, path, funcs)
	case *ast.CallExpr:
		return callProblem(x, recv, path, funcs)
	default:
		return "an expression of this shape can encode any question at all"
	}
}

// configData reports whether an expression is Config's own data: a field, or an
// identifier bound to one of a field's elements by a range. Both are held to
// the same rule, because a range is not a laundry.
func configData(e ast.Expr, recv string, elems map[string]bool) bool {
	if _, isField := cfgField(e, recv); isField {
		return true
	}
	id, ok := e.(*ast.Ident)
	return ok && elems[id.Name]
}

func binaryProblem(x *ast.BinaryExpr, recv string, elems map[string]bool, path []string, funcs map[string]*ast.FuncDecl) string {
	if x.Op == token.LAND || x.Op == token.LOR {
		if p := predicateProblem(x.X, recv, elems, path, funcs); p != "" {
			return p
		}
		return predicateProblem(x.Y, recv, elems, path, funcs)
	}
	if x.Op != token.EQL && x.Op != token.NEQ {
		return "a " + x.Op.String() + " comparison narrows on a value, and no table of sample values closes a value space"
	}
	for _, pair := range [][2]ast.Expr{{x.X, x.Y}, {x.Y, x.X}} {
		side, other := pair[0], pair[1]
		if !configData(side, recv, elems) {
			continue
		}
		if lit, ok := other.(*ast.BasicLit); ok && lit.Kind == token.STRING && lit.Value == `""` {
			continue
		}
		return "a Config field, or an element ranged out of one, may only be compared against the empty string - anything else is a narrowing keyed on a value"
	}
	for _, side := range []ast.Expr{x.X, x.Y} {
		if _, isField := cfgField(side, recv); isField {
			continue
		}
		switch side.(type) {
		case *ast.Ident, *ast.BasicLit:
		default:
			return "an operand of this shape can compute anything, so the comparison around it constrains nothing"
		}
	}
	return ""
}

func callProblem(x *ast.CallExpr, recv string, path []string, funcs map[string]*ast.FuncDecl) string {
	callee := calleeName(x, recv, funcs)
	if callee == "" || !slices.Contains(path, callee) {
		return "a call this package does not declare, or one off the argv path, can decide anything and is not checked here"
	}
	if len(x.Args) == 0 {
		return "a call with no arguments answers from something other than this Config - an environment variable, a global, a clock"
	}
	for _, arg := range x.Args {
		if _, isField := cfgField(arg, recv); !isField {
			return "every argument has to be a Config field, or the question being asked is about something else"
		}
	}
	return ""
}

// Every decision the argv path makes is an emptiness test on a Config field.
//
// This is the rung-5 half: constraining which fields may be read leaves
// `!strings.HasPrefix(s.cfg.ForkFrom, "9")` untouched, because it reads only
// allowed fields. Constraining the *form* of the question closes it, and closes
// the class rather than the example.
func TestEveryDecisionOnTheArgvPathIsAnEmptinessTest(t *testing.T) {
	funcs, fset := packageFuncs(t)
	path := argvPath(t, funcs)
	checked := 0
	for _, name := range path {
		fn := funcs[name]
		recv := receiverOf(fn)
		elems := rangedConfigElements(fn, recv)
		for _, pred := range predicatesIn(fn) {
			checked++
			if p := predicateProblem(pred, recv, elems, path, funcs); p != "" {
				t.Errorf("%s decides something at %s that is not an emptiness test on a Config field: %s.\n"+
					"The argv path emits three shapes and two of them fail silently, so what it may ask is "+
					"as load-bearing as what it may read",
					name, fset.Position(pred.Pos()), p)
			}
		}
	}
	if checked == 0 {
		t.Fatal("the argv path makes no decisions at all: the predicate scan is broken, and a check that finds no questions approves every answer")
	}
}

// identityArgs may read the identity fields and nothing else.
//
// Narrower than the path-wide rule above, and it stays: buildArgs legitimately
// reads Name, Model, Effort and PermissionMode, and the switch that decides the
// identity block may not. This is a claim about the *field set* - the value
// space is closed by the predicate grammar, not by this.
func TestIdentityArgsReadsNothingButTheIdentityFields(t *testing.T) {
	funcs, fset := packageFuncs(t)
	fn, ok := funcs["identityArgs"]
	if !ok {
		t.Fatal("this package declares no identityArgs: it was renamed or moved, and this check would otherwise pass by scanning nothing")
	}
	recv := receiverOf(fn)
	if recv == "" {
		t.Fatal("identityArgs has no named receiver, so there is nothing to trace reads through")
	}

	read := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		e, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		field, isField := cfgField(e, recv)
		if !isField {
			return true
		}
		if !slices.Contains(identityFields, field) {
			t.Errorf("identityArgs reads %s.cfg.%s at %s. It may read %v and nothing else: the cross product "+
				"in fork_test.go is exhaustive over its input only while its input is exactly those",
				recv, field, fset.Position(e.Pos()), identityFields)
			return true
		}
		read[field] = true
		return true
	})
	for _, f := range identityFields {
		if !read[f] {
			t.Errorf("identityArgs never reads %s.cfg.%s: either the scan is broken or the switch stopped "+
				"reading a field the cross product is built on", recv, f)
		}
	}
}

// The identity block buildArgs receives is appended whole and never edited.
//
// This is the beat the field rules cannot reach, because it names no field:
// `identity = identity[2:]` drops --resume and its value, leaving
// `--fork-session --session-id <child>` - accepted, exit 0, empty stderr, an
// ordinary empty session under the id that was meant to receive the fork. The
// switch being closed is necessary and not sufficient; the argv is buildArgs'
// output, and whatever it does to that block afterwards is part of the shape.
func TestTheIdentityBlockIsAppendedWholeAndNeverEdited(t *testing.T) {
	funcs, fset := packageFuncs(t)
	fn, ok := funcs[argvSeed]
	if !ok {
		t.Fatalf("this package declares no %s", argvSeed)
	}
	recv := receiverOf(fn)

	// The name buildArgs binds identityArgs' result to, found rather than typed.
	block, binding := "", ast.Node(nil)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 || len(as.Lhs) == 0 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok || calleeName(call, recv, funcs) != "identityArgs" {
			return true
		}
		id, ok := as.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		block, binding = id.Name, id
		return true
	})
	if block == "" {
		t.Fatalf("%s never binds the result of identityArgs to a name: either it stopped calling the switch "+
			"or this scan cannot see the call, and the check below would hold over nothing", argvSeed)
	}

	parents := parentOf(fn.Body)
	uses := 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id.Name != block || ast.Node(id) == binding {
			return true
		}
		uses++
		call, ok := parents[n].(*ast.CallExpr)
		spread := ok && call.Ellipsis.IsValid() && slices.Contains(call.Args, ast.Expr(id))
		appended := false
		if spread {
			fnIdent, isIdent := call.Fun.(*ast.Ident)
			appended = isIdent && fnIdent.Name == "append"
		}
		if !appended {
			t.Errorf("%s uses %s at %s for something other than append(..., %s...): the identity block is "+
				"emitted as one literal precisely so no statement can drop half of it, and a reslice here "+
				"builds --fork-session with no --resume - accepted, exit 0, and an empty agent under the "+
				"child's id", argvSeed, block, fset.Position(id.Pos()), block)
		}
		return true
	})
	if uses != 1 {
		t.Errorf("%s uses %s %d times, want exactly 1: the block is received and appended, and any second "+
			"mention is a chance to edit it", argvSeed, block, uses)
	}
}

// The two fork guards held from the side an example cannot reach: which
// parents `wake fork` will resolve, over the whole state space rather than the
// two states a fixture happens to carry, and which id it waits on, proved about
// the function rather than about one run of it.
//
// Split off fork_test.go by subject. The tests there drive a fake daemon and
// prove each guard is wired to something; these prove they are *total*, which
// is the question docs/notes/decisions.md says this project keeps being burned
// by - a deletion mutant asks whether a guard exists, a narrowing mutant asks
// whether it covers its whole input, and only the second is what the next
// change actually looks like.

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// forkParentStates is every state a session can be reported in **by a running
// daemon** - which is a strictly narrower set than the one rpc declares, and
// the narrowing is the whole point of this block.
//
// `wake fork` resolves a parent in all of them. Ended is deliberately included:
// a fork resumes a transcript on disk, so a parent whose process has exited is
// a perfectly good thing to branch. The rest resolve here and are judged by the
// daemon, which is the only process that can see whether a turn is in flight -
// so this table says "resolved and sent", never "allowed".
//
// It is a hand-written decision checked against a derived set rather than the
// derived set itself, so a seventh state - park/wake's is the next one - is a
// build failure until somebody decides what `wake fork` does with it.
var forkParentStates = map[string]bool{
	rpc.StateIdle:    true,
	rpc.StateEnded:   true,
	rpc.StateWorking: true,
	rpc.StateBlocked: true,
	rpc.StateSilent:  true,
	// Parked belongs here rather than in the unreachable map, and the reason is
	// the producer: agent.stateLocked returns it, so a running daemon reports
	// it and `wake fork <a parked agent>` is a command somebody types. It
	// resolves here for the same reason every other row does - only the daemon
	// can judge a parent, and it says yes to this one, because a parked
	// process has exited and an exited parent is what every recorded fork
	// resumed.
	rpc.StateParked: true,
}

// unreachableFromARunningDaemon is the state rpc declares that no running
// daemon can report, with the producer that is the reason.
//
// StateOrphaned is written in exactly one place, daemon.FleetOnDisk, which is
// what daemon.Status returns **when the dial fails** - so it arrives only on a
// report whose Running is false, which resolveSession refuses before any state
// is looked at. fleet() is the only writer of Running true and its rows come
// from agent.stateLocked plus the retire-time snapshots in s.recent, and
// stateLocked cannot return it.
var unreachableFromARunningDaemon = map[string]string{
	rpc.StateOrphaned: "daemon.FleetOnDisk, which leaves Running false",
}

// The reachable domain, derived from the producer rather than from the type -
// and the assertion that the two differ by exactly the state that cannot arrive.
//
// # Why this test exists, which is a rung the ladder did not have
//
// The first version of this guard enumerated rpc's **constant block** and gave
// every declared state a verdict, including StateOrphaned. Rung 2 says derive a
// domain from the code that declares it rather than from a list somebody
// remembered, and that is what it did. It was still wrong, in a way that is
// worse than a missing case:
//
//   - `rpc` declares six states; a report from a *running* daemon can carry
//     five. The sixth arrives only on a report resolveSession has already
//     refused.
//   - So the orphan cell asserted a verdict over an input that cannot occur,
//     using a fixture (`Running: true` with an orphaned row) that rpc's own type
//     documentation calls impossible - and it read as coverage.
//   - And the accompanying static guard then **pinned the dead branch**: the
//     only read of `sess.ID` in forkParent was inside the unreachable arm, so
//     deleting the arm failed the floor asserting that ID is read. The guard had
//     stopped protecting the behaviour and started protecting its own blind spot.
//
// **Rung 4: the enumerated domain must be the reachable one.** An AST scan over
// declared members closes narrowing-on-a-member and says nothing about whether
// every member can arrive at this call site. Derive it from the **producer**.
//
// The split is asserted in both directions, so this is not a comment: a state
// stateLocked starts returning must gain a verdict, and a state that stops being
// produced must move to the unreachable map - where it carries the reason, which
// is what makes deleting the code that handles it a decision rather than a
// guess.
func TestTheStatesAForkableParentCanBeInAreTheOnesARunningDaemonProduces(t *testing.T) {
	declared := sessionStateConstants(t)
	reachable := statesARunningDaemonReports(t)

	for name, state := range declared {
		_, decided := forkParentStates[state]
		why, excused := unreachableFromARunningDaemon[state]
		switch {
		case decided && excused:
			t.Errorf("rpc.%s = %q is both decided here and excused as unreachable (%s): one of the two is wrong", name, state, why)
		case !decided && !excused:
			t.Errorf("rpc.%s = %q is a state a session can be reported in, and nothing here says "+
				"whether `wake fork` resolves a parent in it or why it cannot arrive", name, state)
		case decided && !reachable[state]:
			t.Errorf("rpc.%s = %q has a verdict here, but agent.stateLocked never returns it - so the "+
				"cell asserts something about an input no running daemon can produce. That reads as "+
				"coverage and is worse than no cell at all: a guard written over it pins whatever "+
				"code handles the impossible case", name, state)
		case excused && reachable[state]:
			t.Errorf("rpc.%s = %q is excused here as unreachable (%s), but agent.stateLocked returns "+
				"it now - so a running daemon can report it and `wake fork` needs a decision about it",
				name, state, why)
		}
	}
	for state := range reachable {
		if _, decided := forkParentStates[state]; !decided {
			t.Errorf("agent.stateLocked can report %q and nothing here says what `wake fork` does with it", state)
		}
	}
}

// The resolution asserted per member over the states a parent can actually be
// in, rather than by the two the fixture happens to hold.
//
// TestForkResolvesAParentThatHasEnded is an example: it kills `forkParent`
// calling liveSession and sees nothing else. The narrowing it cannot see is the
// realistic one - an arm that refuses a working parent here as well, duplicating
// the daemon's gate a round trip early and going stale the day that gate
// changes, with the example still green because it never reaches a working
// parent.
func TestForkResolvesAParentInEveryStateARunningDaemonCanReport(t *testing.T) {
	for state := range forkParentStates {
		t.Run(state, func(t *testing.T) {
			d := startFakeDaemon(t, 0, rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
				{ID: idAlpha, Name: "sydney", Label: "dev-5748", State: state},
			}})

			got, err := forkParent(d.socket, "sydney")
			switch {
			case err != nil:
				t.Errorf("forkParent refused a %s parent here: %v. Only the daemon can judge whether "+
					"a turn is in flight, and a second copy of that gate on this side goes stale the "+
					"day the first one changes", state, err)
			case got.ID != idAlpha:
				t.Errorf("forkParent on a %s parent reached %q, want %q", state, got.ID, idAlpha)
			}
		})
	}
}

// forkParentReadsNothing is empty, and that is the assertion rather than a gap.
//
// forkParent used to read State to refuse an orphan and ID to name it in the
// refusal. The orphan could not arrive (see above), so the arm is gone and with
// it every read: which parents resolve is not this side's question at all. An
// empty set makes **any** selector on the resolved row a failure, which is a
// stricter guard than the one it replaces.
var forkParentReadsNothing = map[string]bool{}

// forkParent decides nothing about the row it resolves, proved about the
// function rather than sampled over values of its fields.
//
// # Why the table above cannot do this job
//
// It walks every state a running daemon can report, which closes narrowings on
// a member of a closed set. A field's **value** space is neither declared nor
// closed, so no finite sample closes it. Both of these survive the table with
// the whole package green:
//
//	if sess.QuietMS > 100_000 { return rpc.SessionStatus{}, errStale }
//	if sess.Error != "" { return rpc.SessionStatus{}, errStale }
//
// Adding `QuietMS` to the fixture answers the first mutant and not the class -
// reaching for one more sample value is the tell that this is the wrong rung.
// The closing move is to deny the function the field.
//
// # What it allows, and what it deliberately does not
//
// Every mention of the resolved row must be its own definition or a value
// returned directly. That is stricter than "no disallowed selector", on purpose
// - `helper(sess)` carries no selector at all, so a check that looked only at
// selectors would be evaded by moving the comparison one call down. Returning it
// *nested*, as `helper(sess)` in a return statement, is a call and fails with
// the rest.
func TestForkParentAppliesNoRuleOfItsOwnToTheRowItResolves(t *testing.T) {
	fn := funcDecl(t, "match.go", "forkParent")
	assertReadsOnly(t, fn, soleDefinedName(t, fn, "forkParent"), forkParentReadsNothing, returnable,
		"Which parents resolve is the daemon's question and not this side's: it is the only process "+
			"that can see whether a turn is in flight, and a second copy of that rule here goes stale "+
			"the day forkRefusal changes - in the direction that refuses forks the daemon would allow")
}

// forkedFromMayRead is the one field a row's ancestry line depends on. The
// parent's *name* comes from the index beside it, which is the whole reason
// that index is built - a row cannot answer this from its own fields.
var forkedFromMayRead = map[string]bool{"ParentID": true}

// A fork's row says what it came from whatever else is true of it, proved the
// same way and for the same reason.
//
// The two status tests are examples with an idle fork in them, so this survives
// both with the package green:
//
//	if s.ParentID == "" || s.State == rpc.StateEnded { return "" }
//
// And it is not a strawman - it is the row most worth having. Comparing two
// branches of one conversation is what fork is *for*, and by the time somebody
// is comparing them at least one has usually ended, so an arm that quietly drops
// ancestry from ended rows loses the line exactly where it earns its place.
func TestForkedFromReadsNothingButTheParentItNames(t *testing.T) {
	fn := funcDecl(t, "status.go", "forkedFrom")
	assertReadsOnly(t, fn, paramName(t, fn, 0), forkedFromMayRead, notReturnable,
		"A row's ancestry is a function of ParentID alone: a fork is still a fork whatever state "+
			"it is in, how long it has been quiet, or how it ended")
}

// Whether handing the value straight back is a read of it. It is the function's
// own output for forkParent, which returns the row it resolved, and nothing
// forkedFrom does - which returns a string.
const (
	returnable    = true
	notReturnable = false
)

// assertReadsOnly requires that every mention of one value inside a function is
// the reading of an allowed field - and, optionally, returning it.
//
// Stricter than "no disallowed selector", on purpose: `helper(s)` carries no
// selector at all, so a check that looked only at selectors would be evaded by
// moving the comparison one call down. Passing the value anywhere is therefore
// the failure, and so is assigning it to a local.
//
// Two floors stop it being vacuous, and the first is the one that survives an
// **empty** allowed set: the value has to be mentioned at all, and then each
// allowed field has to be read. A renamed function, a renamed parameter or a
// broken parse all yield zero matches, which would otherwise read as "reads
// nothing" - the strongest possible pass, for the weakest possible reason.
func assertReadsOnly(t *testing.T, fn *ast.FuncDecl, value string, mayRead map[string]bool, returns bool, why string) {
	t.Helper()

	allowed, read, mentions := excusedMentions(fn, value, mayRead, returns)

	// The floor that applies whatever the allowed set contains, and the only one
	// that applies when it is empty: a renamed function, a renamed parameter or a
	// broken parse all yield zero mentions, which would otherwise read as "reads
	// nothing" - the strongest possible pass, for the weakest possible reason.
	if mentions == 0 {
		t.Fatalf("%s never mentions %s at all, so this scan matched nothing and is asserting nothing. "+
			"Either the value was renamed or the scan is broken", fn.Name.Name, value)
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id.Name != value || allowed[id] {
			return true
		}
		t.Errorf("%s uses %s at %v other than to read %v. %s",
			fn.Name.Name, value, id.Pos(), keysOf(mayRead), why)
		return true
	})

	for field := range mayRead {
		if !read[field] {
			t.Errorf("%s never reads %s.%s, so this scan matched nothing there and is asserting "+
				"nothing about it. Either it no longer needs that field - in which case take it out "+
				"of the allowed set - or the scan is broken", fn.Name.Name, value, field)
		}
	}
}

// excusedMentions collects the mentions of one value that are not reads of a
// forbidden field, and the field names it was seen reading.
func excusedMentions(fn *ast.FuncDecl, value string, mayRead map[string]bool, returns bool) (map[*ast.Ident]bool, map[string]bool, int) {
	allowed := map[*ast.Ident]bool{}
	read := map[string]bool{}
	mentions := 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == value {
			mentions++
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			// The definition itself. A name on the left of `:=` is not a read.
			for _, lhs := range n.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == value {
					allowed[id] = true
				}
			}
		case *ast.RangeStmt:
			// The other way a name is defined. Excusing the definition is the
			// same rule as above and cannot weaken anything: what is being
			// policed is what a function *does* with a value, and `for _, s :=
			// range` does nothing with one.
			for _, def := range []ast.Expr{n.Key, n.Value} {
				if id, ok := def.(*ast.Ident); ok && id.Name == value {
					allowed[id] = true
				}
			}
		case *ast.ReturnStmt:
			// Only a bare mention, so `helper(s)` in a return is not excused.
			for _, res := range n.Results {
				if id, ok := res.(*ast.Ident); ok && id.Name == value && returns {
					allowed[id] = true
				}
			}
		case *ast.SelectorExpr:
			x, ok := n.X.(*ast.Ident)
			if !ok || x.Name != value {
				return true
			}
			read[n.Sel.Name] = true
			if mayRead[n.Sel.Name] {
				allowed[x] = true
			}
		}
		return true
	})
	return allowed, read, mentions
}

// paramName is the name of one of a function's parameters, failing rather than
// guessing when the signature is not the one the guard was written for.
func paramName(t *testing.T, fn *ast.FuncDecl, n int) string {
	t.Helper()

	if fn.Type.Params == nil || len(fn.Type.Params.List) <= n || len(fn.Type.Params.List[n].Names) != 1 {
		t.Fatalf("%s takes %v, and this guard is written for a named parameter %d",
			fn.Name.Name, fn.Type.Params, n)
	}
	return fn.Type.Params.List[n].Names[0].Name
}

// --- reading this package's own source ---------------------------------------

// sessionStateConstants reads every `State… = "…"` constant rpc declares.
//
// The same scan internal/daemon's fork gate runs, for the same reason: a
// hand-written list cannot fail for a member nobody added to it, and the claim
// above is that the table covers the state space rather than a remembered part
// of it.
//
// Globbed rather than pointed at lifecycle.go, which is where the states live
// today, because a state constant added to wire.go would otherwise leave the
// count right and the scan blind.
func sessionStateConstants(t *testing.T) map[string]string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("..", "..", "internal", "rpc", "*.go"))
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

// statesARunningDaemonReports is the reachable half: the states
// daemon.agent.stateLocked can return.
//
// **Read from the producer rather than from the type**, which is the whole of
// rung 4. rpc's constant block declares what a `SessionStatus.State` may
// contain; this function decides what one actually does contain on the only
// report `resolveSession` lets through, because daemon.fleet() is the sole
// writer of `Running: true` and every live row on it comes from here. The
// retire-time snapshots in `s.recent` come from the same function, so they add
// no state this misses.
//
// Names are resolved back to values through the constant scan, so a state
// referred to here by a constant rpc does not declare is a build failure in Go
// before it is one here.
func statesARunningDaemonReports(t *testing.T) map[string]bool {
	t.Helper()

	byName := sessionStateConstants(t)
	fn := funcDeclIn(t, filepath.Join("..", "..", "internal", "daemon", "agent.go"), "stateLocked")

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
		t.Fatalf("agent.stateLocked names no rpc.State… constant: the scan is broken, and every " +
			"reachability claim resting on it is asserting nothing")
	}
	return out
}

// stringConstants returns the `Name = "value"` constants in one file whose
// names start with a prefix.
func stringConstants(t *testing.T, file, prefix string) map[string]string {
	t.Helper()

	out := map[string]string{}
	for _, decl := range parseFile(t, file).Decls {
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

// funcDecl finds one function in one file of this package, failing rather than
// returning nil - a scan that found nothing must not read as a scan that found
// nothing wrong.
func funcDecl(t *testing.T, file, name string) *ast.FuncDecl {
	t.Helper()

	for _, decl := range parseFile(t, file).Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == name && fn.Body != nil {
			return fn
		}
	}
	t.Fatalf("no func %s in %s: it was renamed or moved, and the test over it is asserting nothing", name, file)
	return nil
}

// funcDeclIn is funcDecl for a function with a receiver, in any file - which is
// what reading another package's producer takes. Methods are matched by name
// alone; a package with two of them would be ambiguous, and it fails rather
// than picking.
func funcDeclIn(t *testing.T, file, name string) *ast.FuncDecl {
	t.Helper()

	var found *ast.FuncDecl
	for _, decl := range parseFile(t, file).Decls {
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

// soleDefinedName is the one local a function defines with `:=`, which is how
// both guards here name the value they are about without hard-coding a
// variable name that a rename would silently retire.
func soleDefinedName(t *testing.T, fn *ast.FuncDecl, what string) string {
	t.Helper()

	names := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || as.Tok != token.DEFINE {
			return true
		}
		for _, lhs := range as.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" && id.Name != "err" {
				names[id.Name] = true
			}
		}
		return true
	})
	if len(names) != 1 {
		t.Fatalf("%s defines %v, and this guard is written for the single value it resolves: "+
			"a second local is a place for the check to move to where the scan cannot see it",
			what, keysOf(names))
	}
	for name := range names {
		return name
	}
	return ""
}

func parseFile(t *testing.T, file string) *ast.File {
	t.Helper()

	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	return f
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// mintedBy is the constructor a fresh session id comes from. Named here so the
// scan below fails loudly if it ever moves, rather than matching nothing and
// reading as a pass.
const mintedBy = "uuid.NewString"

// The id `wake fork` waits on is the one it minted, minted once, and never the
// parent's - asserted about the function rather than about one run of it.
//
// # Why the behavioural test cannot do this job
//
// TestForkWaitsOnTheForksOwnIdRatherThanItsParents drives one fixture: an idle
// parent. It kills both mutants that swap the id outright. It cannot see either
// of these, because each keeps the good behaviour and excludes a subset of the
// inputs - which is what the next change to this function actually looks like:
//
//	sessionID := uuid.NewString()
//	if parent.State == rpc.StateEnded { sessionID = parent.ID }
//	if strings.HasPrefix(parent.ID, "b22b") { sessionID = parent.ID }
//
// The first is a sentence somebody writes: "an ended parent's id is free, so
// the fork can just take it." It is wrong - claude refuses a --session-id that
// is already on disk - and against a fixture whose parent is idle it is
// invisible. Adding an ended-parent fixture answers that mutant and not the
// class; a parent's field values are not a closed set, so no finite sample
// closes them, and reaching for one more fixture is the tell that this is the
// wrong rung. See docs/notes/decisions.md.
//
// # What it requires
//
// One mint, one assignment, and the same identifier in both places that matter:
// the argument openSession keys its wait on, and the id the frame is written
// with. A branch that substituted another value would have to assign the name a
// second time, which the count forbids, or pass something else at one of the
// two call sites, which the identity check forbids.
//
// The floors at the end are what stop it being vacuous: a rename of any of the
// three functions leaves every scan matching nothing, which would otherwise be
// the strongest possible pass for the weakest possible reason.
func TestTheIdForkWaitsOnIsTheOneItMintedAndNeverTheParents(t *testing.T) {
	fn := funcDecl(t, "fork.go", "forkSession")

	minted := mintedOnce(t, fn)

	waited := argumentTo(t, fn, "openSession", 1)
	if id, ok := waited.(*ast.Ident); !ok || id.Name != minted {
		t.Errorf("openSession is keyed on %s rather than on %s. That argument is what awaitSpawn "+
			"matches the daemon's refusal against, and the wait has no deadline - so a mismatch "+
			"here is `wake fork` hanging on a blank terminal, not `wake fork` failing",
			exprText(waited), minted)
	}

	onFrame := argumentTo(t, fn, "requestFork", 1)
	if id, ok := onFrame.(*ast.Ident); !ok || id.Name != minted {
		t.Errorf("the frame is written with %s rather than with %s, so the daemon runs the fork "+
			"under an id nothing is waiting on", exprText(onFrame), minted)
	}
	parent := argumentTo(t, fn, "requestFork", 2)
	if id, ok := parent.(*ast.Ident); ok && id.Name == minted {
		t.Errorf("the frame names %s as both the fork and its parent, which asks the daemon to "+
			"fork a session into itself", minted)
	}
}

// mintedOnce is the name a function assigns from mintedBy, having established
// that it assigns it exactly once.
//
// The count is the half that matters. A branch substituting the parent's id for
// some subset of parents has to write the name a second time, and no fixture can
// see that - it keeps the good behaviour for every input the fixture supplies.
func mintedOnce(t *testing.T, fn *ast.FuncDecl) string {
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
		id, ok := soleIdentAssignedFrom(as, mintedBy)
		if !ok {
			return true
		}
		if minted != "" {
			t.Fatalf("%s mints more than one id (%s and %s), and this guard is written for the "+
				"single id the daemon is asked for and the client waits on", fn.Name.Name, minted, id)
		}
		minted = id
		return true
	})

	if minted == "" {
		t.Fatalf("%s assigns nothing from %s: a fork's id has to be minted here the way attach "+
			"mints a spawn's, and this scan is asserting nothing about where it comes from",
			fn.Name.Name, mintedBy)
	}
	if n := assignments[minted]; n != 1 {
		t.Errorf("%s assigns %s %d times. It has to be written once: a second assignment is where "+
			"a branch substitutes the parent's id for some subset of parents, which the behavioural "+
			"test cannot see because it drives one fixture", fn.Name.Name, minted, n)
	}
	return minted
}

// soleIdentAssignedFrom is the single name an assignment writes from one call,
// when that is the shape of it.
func soleIdentAssignedFrom(as *ast.AssignStmt, call string) (string, bool) {
	if len(as.Lhs) != 1 || len(as.Rhs) != 1 || !callsFunction(as.Rhs[0], call) {
		return "", false
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

// callsFunction reports whether an expression is a call to pkg.Fn.
func callsFunction(e ast.Expr, want string) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	return exprText(call.Fun) == want
}

// argumentTo is the nth argument of the one call to a named function inside a
// function body, failing when there is not exactly one such call - a scan that
// found no call must not read as a scan that found nothing wrong.
func argumentTo(t *testing.T, fn *ast.FuncDecl, name string, n int) ast.Expr {
	t.Helper()

	var found []*ast.CallExpr
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && exprText(call.Fun) == name {
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
		t.Fatalf("the call to %s takes %d arguments, so there is no argument %d to check: the "+
			"signature changed under this guard", name, len(found[0].Args), n)
	}
	return found[0].Args[n]
}

// exprText renders the identifiers and selectors this file compares, so a
// failure names what it found rather than a node address.
func exprText(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprText(e.X) + "." + e.Sel.Name
	default:
		return fmt.Sprintf("%T", e)
	}
}

// Both fork surfaces say a fork is a snapshot, and this is the half a
// behavioural test in this package cannot reach.
//
// `announceFork` is asserted directly by fork_test.go; what nothing there can
// see is whether `forkSession` ever hands it over, because the success path runs
// into `converse`, which opens an alt screen and takes stdin. That gap is
// exactly how the promise came to be true on one surface while three artefacts
// asserted it for both.
//
// The `attach` half is pinned in the same breath and is not symmetry for its own
// sake: the sentence says the fork holds the parent's conversation *as of now*,
// which is true when a fork is taken and false at every later attach.
func TestBothForkSurfacesSayItAndAttachDoesNot(t *testing.T) {
	const confirmedArg = 3

	fork := funcDecl(t, "fork.go", "forkSession")
	if got := argumentTo(t, fork, "openSession", confirmedArg); exprText(got) != "announceFork" {
		t.Errorf("forkSession hands openSession %s as its confirmation, want announceFork: the "+
			"sentence is only said if somebody passes it, and no test in this package can watch the "+
			"success path run", exprText(got))
	}

	attach := funcDecl(t, "attach.go", "attach")
	if got := argumentTo(t, attach, "openSession", confirmedArg); exprText(got) != "nil" {
		t.Errorf("attach hands openSession %s as its confirmation. The line it would say is that the "+
			"fork has its parent's conversation *as of now*, which is false for every attach after the "+
			"one that made it", exprText(got))
	}

	open := funcDecl(t, "attach.go", "openSession")
	if !callsParameter(open, confirmedArg) {
		t.Error("openSession never calls its confirmation parameter, so passing announceFork does " +
			"nothing and `wake fork` is silent again")
	}
}

// callsParameter reports whether a function calls the parameter in position n.
func callsParameter(fn *ast.FuncDecl, n int) bool {
	var names []string
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	if n >= len(names) {
		return false
	}
	called := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && exprText(call.Fun) == names[n] {
			called = true
		}
		return !called
	})
	return called
}

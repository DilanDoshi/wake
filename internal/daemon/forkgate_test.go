// The fork gate held from the side a lifecycle test cannot reach: forkRefusal
// as a pure function over its whole input, and a static assertion about what
// that input may be read from.
//
// Split off fork_test.go by subject rather than by size. The tests there drive
// a real socket and prove the gate is wired to something; these two prove it is
// *total*, which is a different question and the one this project keeps being
// burned by.
//
// # The three rungs of the same ladder, because this file is the third
//
//  1. **Examples.** Three end-to-end tests reach a working, an ended and an
//     unheld parent. They kill a deleted arm and see nothing else.
//  2. **Enumerate the domain where the code declares it.** States are a closed
//     set rpc spells out, so the state dimension is read out of rpc's own source
//     and asserted per member. That killed the narrowing the four brief-mandated
//     mutations all survived (`if p.Tool != "" { return "" }`).
//  3. **Assert statically where the code does not declare the domain.** Field
//     *values* are not a closed set, and no finite sample closes an open one: a
//     table sampling `QuietMS = 90_000` cannot see `if p.QuietMS > 100_000`, and
//     the 100s-300s band that mutant opens is a mid-turn parent forked with the
//     whole suite green. So the property is proved about the *function* instead
//     of about the sample - forkRefusal may not read a field a value could vary.
//
// **A sampled dimension is an open dimension.** That is the rule; the enumerated
// half below and the static half below it are the two halves of obeying it.

package daemon

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

// forkVerdicts is whether a session in each state may be forked, which is the
// whole of forkRefusal's contract stated as data.
//
// True for the two the recordings cover - an idle parent whose turn has
// flushed, and a parent whose process has already exited - and false for every
// other state, because refusing what is unrecorded is the policy and guessing
// is what it exists instead of.
var forkVerdicts = map[string]bool{
	rpc.StateIdle:  true,
	rpc.StateEnded: true,
	// A parked parent is the exited-parent case, which is the most-recorded
	// fork source in the corpus rather than an extrapolation from it: park
	// works by closing stdin and letting the in-flight turn finish, so the
	// process is gone and the transcript is flushed. 2026-08-09 findings §10
	// says it in one line - *"every fork here resumed a parent whose process
	// had already exited"* - which is the state resume-fork-parent.jsonl was in
	// when resume-fork-both-flags.jsonl and resume-fork-no-session-id.jsonl
	// were taken from it. The cwd objection does not reach it either: fork runs
	// in the parent's own directory, which a parked session keeps.
	rpc.StateParked:   true,
	rpc.StateWorking:  false,
	rpc.StateBlocked:  false,
	rpc.StateSilent:   false,
	rpc.StateOrphaned: false,
}

// forkNextSteps are the actions a refusal may leave the operator with, and every
// refusal for a declared state has to name at least one of them.
//
// A closed set of *verbs Wake actually offers* rather than a sentence per arm,
// and the distinction is the whole design of this assertion. Pinning each arm's
// wording is a test that fails on every improvement to it, which is how a guard
// over prose gets deleted. What must not change is that the sentence ends in
// something to do: `alex no.` is the failure this exists for, and degrading the
// blocked and silent arms to exactly that used to leave the package green.
//
// "fork" is deliberately **not** in the set even though three arms say it, and
// the fourth is why: every refusal names the verb it is refusing - the default
// arm ends "so it cannot be forked" - so counting it would make this vacuous.
//
// StateOrphaned's next step is `wake status` and carries no "when", which is
// correct rather than an exception: an orphan has no daemon to wait for. That is
// the reason this asserts an *action* and not a time.
var forkNextSteps = []string{"stop", "answer", "deny", "check"}

// The gate asserted per member over its whole state space, rather than by the
// three examples the lifecycle tests happen to reach.
//
// This is the shape docs/notes/decisions.md names: **a deletion mutant asks
// whether a guard exists, a narrowing mutant asks whether it is total.** The
// end-to-end tests kill a deleted arm and cannot see a narrowed one - an arm
// that keeps refusing a working parent *unless* it is mid-tool refuses fewer
// states than it should and leaves all four of them green, because none reaches
// a working parent inside a tool call. That mutant is the realistic one: nobody
// deletes a working case, they add a condition above it.
//
// Two dimensions here, and a third assertion below that this one cannot make:
//
//   - **Every declared state**, read out of rpc's own source rather than from a
//     list somebody remembered. A seventh state added there with no verdict
//     decided here is a build failure, instead of falling silently into
//     forkRefusal's default and being refused with a sentence about nothing
//     being known. park/wake's `StateParked` is the next one.
//   - **Every other field populated**, which is what makes an arm *reading* a
//     second field visible. It does not make an arm *comparing* one visible -
//     see TestForkRefusalReadsNothingButTheStateAndWhoTheSentenceIsAbout, which
//     is the assertion that closes it.
func TestForkRefusalIsTotalOverEveryStateASessionCanBeIn(t *testing.T) {
	declared := sessionStateConstants(t)
	if len(declared) != len(forkVerdicts) {
		t.Fatalf("rpc declares %d session states and this test decides %d of them (%v vs %v): "+
			"a state with no verdict here is refused by forkRefusal's default with a sentence "+
			"about nothing being known, which is the right safe answer and the wrong way to "+
			"find out about it", len(declared), len(forkVerdicts), declared, forkVerdicts)
	}
	for name, state := range declared {
		if _, decided := forkVerdicts[state]; !decided {
			t.Fatalf("rpc.%s = %q is a state a session can be reported in, and nothing here says "+
				"whether such a session may be forked", name, state)
		}
	}

	for state, forkable := range forkVerdicts {
		for _, dressed := range []struct {
			what string
			row  func(rpc.SessionStatus) rpc.SessionStatus
		}{
			{what: "bare", row: func(p rpc.SessionStatus) rpc.SessionStatus { return p }},
			{what: "with every other field set", row: dressRow},
		} {
			// core.ManagerName is deliberately not a fourth entry here. It is
			// refused in **every** state, above this switch, so it does not
			// have a per-state verdict to check - and adding it would make
			// every forkable cell fail for a reason that has nothing to do
			// with the recording this table is about.
			// TestTheManagerCannotBeForked is where that arm lives.
			for _, who := range []struct{ what, name, want string }{
				{what: "named", name: "alex", want: "alex"},
				{what: "unnamed", name: "", want: idAlpha},
			} {
				p := dressed.row(rpc.SessionStatus{ID: idAlpha, Name: who.name, State: state})
				checkForkVerdict(t, p, forkable, dressed.what, who.what, who.want)
			}
		}
	}
}

// checkForkVerdict holds one cell of the table: the verdict, who the sentence is
// about, and - for a refusal - that it leaves the operator something to do.
func checkForkVerdict(t *testing.T, p rpc.SessionStatus, forkable bool, dressed, who, want string) {
	t.Helper()

	why := forkRefusal(p)
	if forkable {
		if why != "" {
			t.Errorf("a %s parent (%s, %s) was refused with %q, and it is one of the two "+
				"states the recordings cover", p.State, dressed, who, why)
		}
		return
	}

	switch {
	case why == "":
		t.Errorf("a %s parent (%s, %s) was accepted, and what a fork of one inherits "+
			"is unrecorded - so this ships a fork nothing knows the contents of",
			p.State, dressed, who)
		return
	case !strings.Contains(why, want):
		t.Errorf("the refusal for a %s parent (%s, %s) is %q and does not name %q: at "+
			"15-30 sessions a refusal that names no agent is unattributable",
			p.State, dressed, who, why, want)
	case strings.TrimSpace(strings.ReplaceAll(why, want, "")) == "":
		t.Errorf("the refusal for a %s parent (%s, %s) is %q, which is the agent's name "+
			"and nothing else - an operator is owed what to do instead",
			p.State, dressed, who, why)
	}

	lower := strings.ToLower(why)
	for _, step := range forkNextSteps {
		if strings.Contains(lower, step) {
			return
		}
	}
	t.Errorf("the refusal for a %s parent (%s, %s) is %q and names none of %v - it says no "+
		"and does not say what to do instead. An unrecorded state is a reason to wait, not a "+
		"dead end, and a key that refuses without a next step is the failure the legend rule "+
		"exists for arriving at runtime", p.State, dressed, who, why, forkNextSteps)
}

// dressRow fills every field of a status row except the three the caller set,
// so a gate that reads one of them is visible.
//
// It samples one value per field, which is why it cannot be the whole of the
// argument - `QuietMS = 90_000` says nothing about `p.QuietMS > 100_000`. The
// static assertion below is what covers the values this cannot.
func dressRow(p rpc.SessionStatus) rpc.SessionStatus {
	p.Label = "dev-5748"
	p.Dir = "/repo/api"
	p.ParentID = idBeta
	p.Tool = "Edit"
	p.ToolArg = "auth/token.go"
	p.QuietMS = 90_000
	p.RequestIDs = []string{"req-1"}
	p.Error = "signal: killed"
	p.PID = 4242
	return p
}

// forkRefusalMayRead is every rpc.SessionStatus field forkRefusal is allowed to
// look at, and it is three: State decides the verdict, Name and ID decide who
// the sentence is about.
//
// Anything else is a second source of truth for a question already answered.
// agent.stateLocked has folded the outstanding ask, the ending, the failed write
// and the silence into that one word before the report is built, so a gate that
// consulted RequestIDs or QuietMS as well would be re-deciding liveness from a
// worse vantage point - and disagreeing with the row it was handed.
var forkRefusalMayRead = map[string]bool{"State": true, "Name": true, "ID": true}

// The verdict is a function of State alone, proved about the function rather
// than sampled over values of the other fields.
//
// # Why a table cannot do this job
//
// TestForkRefusalIsTotalOverEveryStateASessionCanBeIn populates every other
// field, which catches an arm that reads one *at all* only because the value it
// happens to write trips the predicate. Two narrowings that survive it, both
// constructed and run green against the whole package:
//
//	case rpc.StateWorking:
//	    if p.QuietMS > 100_000 { return "" }             // dressRow writes 90_000
//	    if p.Tool == "Read" || p.Tool == "Grep" { return "" }  // dressRow writes "Edit"
//
// The first is not a strawman. stateLocked reports `working` right up to
// silenceLimit, which is five minutes, so that mutant opens a **100s-300s band in
// which a mid-turn parent is silently forkable** with the suite green. The second
// is the sentence somebody actually writes: "read-only tools have nothing in
// flight, so a fork of one is safe."
//
// Adding those values to dressRow answers those two mutants and not the class.
// A field's value space is open, so no finite sample closes it - the only closing
// move is to deny the function the field.
//
// # What it checks, and what it deliberately refuses to allow
//
// Every mention of the parameter must be the `X` of a selector naming an allowed
// field. That is stricter than "no disallowed selector", on purpose: `helper(p)`
// carries no selector at all, so a check that looked only at selectors would be
// evaded by moving the comparison one call down. Passing the row anywhere is
// therefore the failure, and so is assigning it to a local.
//
// The floor below is what stops the whole thing being vacuous. A renamed
// function, a renamed parameter or a broken parse all yield zero matches, which
// would otherwise read as "reads nothing" - the strongest possible pass, for the
// weakest possible reason.
func TestForkRefusalReadsNothingButTheStateAndWhoTheSentenceIsAbout(t *testing.T) {
	fn := funcDecl(t, "spawn.go", "forkRefusal")
	if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 || len(fn.Type.Params.List[0].Names) != 1 {
		t.Fatalf("forkRefusal takes %v, and this test is written for the single status row it is "+
			"supposed to judge", fn.Type.Params)
	}
	param := fn.Type.Params.List[0].Names[0].Name

	// The selector nodes that are allowed, by identity, so the walk below can
	// tell `p.State` from a bare `p` handed to something else.
	allowed := map[*ast.Ident]bool{}
	read := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || x.Name != param {
			return true
		}
		read[sel.Sel.Name] = true
		if forkRefusalMayRead[sel.Sel.Name] {
			allowed[x] = true
		}
		return true
	})

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id.Name != param || allowed[id] {
			return true
		}
		t.Errorf("forkRefusal uses %s at %v other than to read %v. The verdict has to be a "+
			"function of State alone, and a row handed to a helper - or compared, or stored - "+
			"is a value the table test above cannot see the use of: it samples one value per "+
			"field, and a predicate on some other value survives it with the suite green",
			param, id.Pos(), keysOf(forkRefusalMayRead))
		return true
	})

	// The floor: without it a renamed parameter passes by matching nothing.
	for field := range forkRefusalMayRead {
		if !read[field] {
			t.Errorf("forkRefusal never reads %s.%s, so this scan matched nothing there and is "+
				"asserting nothing about it. Either the function no longer needs that field - in "+
				"which case take it out of forkRefusalMayRead - or the scan is broken",
				param, field)
		}
	}
}

// sessionStateConstants reads every `State… = "…"` constant rpc declares.
//
// The same trick internal/rpc's own frameKindConstants plays, for the same
// reason: a hand-written list cannot fail for a member nobody added to it, and
// the whole claim above is that this covers the state space rather than a
// remembered part of it.
//
// Globbed rather than pointed at lifecycle.go, which is where the states live
// today. frameKindConstants is called with *two* files because rpc declares
// frame kinds in two, and a state constant added to wire.go would otherwise
// leave the count at six, pass both checks, and fall into the default arm this
// is written to prevent.
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

// stringConstants returns the `Name = "value"` constants in one file whose names
// start with a prefix.
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

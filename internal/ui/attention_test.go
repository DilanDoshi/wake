package ui

// Attention, and the guards that keep its table honest.
//
// The states this ranks are rpc's own, so every guard below derives its set
// from internal/rpc/lifecycle.go rather than restating it. A hand-written list
// of the six would pass over a seventh, and a seventh state sorts into an
// arbitrary hole in the roster an operator scans first.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestAttentionRanksBlockedFirstAndStalestWorkFirstWithinIt(t *testing.T) {
	// `newest` sits after `stale` on purpose: without a working agent that is
	// fresher than one already placed, the comparator is only ever asked which
	// of two is staler in one direction, and the other half of it goes
	// unexercised.
	in := []Agent{
		{ID: "idle", State: rpc.StateIdle},
		{ID: "fresh", State: rpc.StateWorking, QuietMS: 2_000},
		{ID: "ended", State: rpc.StateEnded},
		{ID: "stale", State: rpc.StateWorking, QuietMS: 720_000},
		{ID: "blocked", State: rpc.StateBlocked},
		{ID: "silent", State: rpc.StateSilent},
		{ID: "newest", State: rpc.StateWorking, QuietMS: 500},
	}
	want := []string{"blocked", "stale", "fresh", "newest", "idle", "silent", "ended"}

	if got := ids(Rank(in)); !slices.Equal(got, want) {
		t.Errorf("Rank = %v, want %v: blocked is the only evidence-backed state and the agent on one pytest call for twelve minutes must float above one that started thirty seconds ago", got, want)
	}
}

func TestRankDoesNotMutateWhatItIsGiven(t *testing.T) {
	in := []Agent{{ID: "a", State: rpc.StateIdle}, {ID: "b", State: rpc.StateBlocked}}
	_ = Rank(in)
	if in[0].ID != "a" {
		t.Error("Rank sorted its caller's slice: every value in this package is copied on write, and a sort in place makes two holders of one Fleet disagree about the roster")
	}
}

// The states this ranks are rpc's, and a state nobody ranked would sort into
// an arbitrary hole. Derived from the declaration rather than restated: a
// hand-written list of the six is the exact failure decisions.md names.
func TestEveryStateTheDaemonCanReportHasARank(t *testing.T) {
	for _, state := range declaredStateConstants(t) {
		if _, ok := attentionRank[state]; !ok {
			t.Errorf("rpc reports state %q and attention.go does not rank it: it would sort into an arbitrary position in the roster the operator scans first", state)
		}
	}
}

// And the other direction, which is the half that rots quietly: a rank for a
// state rpc no longer declares is dead text, and it is what makes deleting a
// state a two-place edit rather than a one-line one.
func TestNoRankIsDeadTextAndNoTwoStatesShareOne(t *testing.T) {
	declared := declaredStateConstants(t)
	seen := map[int]string{}
	for state, rank := range attentionRank {
		if !slices.Contains(declared, state) {
			t.Errorf("attention.go ranks %q and rpc declares no such state: a rank nobody can reach is dead text", state)
		}
		if other, clash := seen[rank]; clash {
			t.Errorf("%q and %q both rank %d: Rank's tie-break reads only one side's state, so two states sharing a rank make the comparison asymmetric", state, other, rank)
		}
		seen[rank] = state
	}
}

// A version skew must not be able to push a blocked agent off the first
// screen, which is a claim about `unranked` that nothing else checks.
func TestAStateThisBuildDoesNotKnowSortsLastAndNeverFirst(t *testing.T) {
	for state, rank := range attentionRank {
		if rank >= unranked {
			t.Errorf("%q ranks %d and an unknown state ranks %d: an unknown state would sort at or above one this build understands", state, rank, unranked)
		}
	}

	in := []Agent{{ID: "future", State: "a state from a later build"}, {ID: "blocked", State: rpc.StateBlocked}}
	if got := ids(Rank(in)); !slices.Equal(got, []string{"blocked", "future"}) {
		t.Errorf("Rank = %v, want [blocked future]: a state this build cannot rank sorts after everything it can", got)
	}
}

// Ties fall back to the order they were first seen in, which is what stops two
// equal rows swapping places between frames.
//
// At a fleet the size Wake is for, and interleaved so the sort has to do real
// work. Four agents in two pairs does not distinguish a stable sort from an
// unstable one: Go's unstable sort is an insertion sort below thirteen
// elements and a partial-insertion pass short-circuits an input that is
// already ordered, so the shape that catches this has to be both long enough
// and out of order.
func TestEqualAgentsKeepTheOrderTheyWereFirstSeenIn(t *testing.T) {
	// The tied group is `working` rather than `idle`, so the tie runs through
	// the QuietMS comparison as well as through the rank: two agents that have
	// been quiet for exactly as long are the tie an operator actually sees.
	const pairs = 20
	var in []Agent
	var want []string
	for i := range pairs {
		in = append(in,
			Agent{ID: fmt.Sprintf("working-%02d", i), State: rpc.StateWorking, QuietMS: 4_000},
			Agent{ID: fmt.Sprintf("blocked-%02d", i), State: rpc.StateBlocked})
	}
	for i := range pairs {
		want = append(want, fmt.Sprintf("blocked-%02d", i))
	}
	for i := range pairs {
		want = append(want, fmt.Sprintf("working-%02d", i))
	}

	if got := ids(Rank(in)); !slices.Equal(got, want) {
		t.Errorf("Rank = %v, want %v: an unstable sort makes the roster shuffle on every frame that changes nothing", got, want)
	}
}

func declaredStateConstants(t *testing.T) []string {
	t.Helper()
	return declaredConstants(t, "../rpc/lifecycle.go", "State")
}

// declaredConstants returns the value of every string constant in path whose
// name begins with prefix.
//
// It fails rather than returning a short answer, in both directions: a
// prefixed name it could not read a string out of is reported by name, and an
// empty result is reported at all - a derivation that silently sees three of
// fourteen is a guard that passes over the other eleven, which is the failure
// this whole approach exists to avoid.
func declaredConstants(t *testing.T, path, prefix string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var out, skipped []string
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if !strings.HasPrefix(name.Name, prefix) {
				continue
			}
			value, ok := stringValue(spec, i)
			if !ok {
				skipped = append(skipped, name.Name)
				continue
			}
			out = append(out, value)
		}
		return true
	})
	if len(skipped) > 0 {
		t.Fatalf("%s declares %v with the %q prefix and no string literal this could read: the derivation would pass over them", path, skipped, prefix)
	}
	if len(out) == 0 {
		t.Fatalf("found no %q constants in %s: this guard would pass over anything", prefix, path)
	}
	return out
}

func stringValue(spec *ast.ValueSpec, i int) (string, bool) {
	if i >= len(spec.Values) {
		return "", false
	}
	lit, ok := spec.Values[i].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

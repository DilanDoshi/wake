package daemon

// The name pool and the registry that hands names out.
//
// Two properties carry the feature and both are asserted rather than argued:
// no two live sessions share a name, and the registry always answers - a
// display name is not allowed to be the reason somebody cannot start an agent.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// The pool has to be comfortably larger than the fleet the spec sizes for, or
// the numbered fallback stops being a corner and becomes the ordinary
// experience. Spec §5 sizes the product at 15-30 sessions.
const specFleetCap = 30

func TestThePoolIsLargerThanTheFleetItNames(t *testing.T) {
	if len(namePool) <= specFleetCap {
		t.Fatalf("the pool holds %d names against a fleet cap of %d, so a full fleet exhausts it",
			len(namePool), specFleetCap)
	}
	t.Logf("pool = %d names; at the %d-session cap %d remain free (%.0f%% of the pool)",
		len(namePool), specFleetCap, len(namePool)-specFleetCap,
		100*float64(len(namePool)-specFleetCap)/float64(len(namePool)))
}

// Every name in the pool has to survive the same validation a name arriving on
// the wire does, or the daemon can hand out something it would refuse.
func TestEveryPooledNameIsOneTheDaemonWouldAccept(t *testing.T) {
	for _, want := range namePool {
		got, err := normalizeName(want)
		if err != nil {
			t.Errorf("pooled name %q is not one the daemon would accept: %v", want, err)
			continue
		}
		if got != want {
			t.Errorf("normalizeName(%q) = %q; a pooled name must already be in its normal form", want, got)
		}
	}
}

// The load-bearing property of the pool, and the one that is easy to break by
// adding a name.
//
// `wake attach` resolves a unique *prefix*, and it searches names and session
// ids together. A pooled name that is also a prefix of another pooled name
// makes `wake attach <the shorter one>` ambiguous between two agents that were
// named automatically - a refusal nobody caused and nobody can avoid.
func TestNoPooledNameIsAPrefixOfAnother(t *testing.T) {
	for _, a := range namePool {
		for _, b := range namePool {
			if a != b && strings.HasPrefix(b, a) {
				t.Errorf("pooled name %q is a prefix of pooled name %q, so `wake attach %s` is ambiguous between two agents nobody named", a, b, a)
			}
		}
	}
}

// A duplicate in the pool is invisible to every other test here. The prefix
// property below compares distinct entries, so it cannot see one; the
// distinctness test draws 30 from 64 and would not notice 63; and the exhaustion
// test fills "the pool" by count, so 63 names plus one ordinal satisfies it.
// Three aggregates, each staying correct while the thing they are aggregates of
// is wrong - which is exactly the shape that hides a compensating change.
func TestThePoolHoldsNoNameTwice(t *testing.T) {
	seen := make(map[string]int, len(namePool))
	for i, name := range namePool {
		if at, dup := seen[name]; dup {
			t.Errorf("pooled name %q appears at both %d and %d, so the pool is smaller than it counts", name, at, i)
		}
		seen[name] = i
	}
	if len(seen) != len(namePool) {
		t.Errorf("the pool lists %d names and holds %d distinct ones", len(namePool), len(seen))
	}
}

// The other half of that, against the id space rather than against itself.
//
// A session id is a UUID and `wake status` prints its first eight characters,
// which are hex. A name made only of hex letters - "ada", "beef", "cafe" -
// could therefore be a prefix of somebody's session id, and the two spaces
// would collide in the matcher. The predicate is positional rather than an
// alphabet test, because the hyphen is a legal name rune and sits at UUID
// positions 8, 13, 18 and 23 - see couldPrefixASessionID.
//
// It asserts with the daemon's own predicate rather than a second copy of it,
// per the no-parallel-implementations rule. What holds that predicate honest is
// TestAChosenNameThatCouldBeASessionIDPrefixIsRefused, which is behavioural: a
// predicate that always answered false would make this test vacuous and that
// one fail.
func TestNoPooledNameCanBeAPrefixOfASessionID(t *testing.T) {
	for _, name := range namePool {
		if couldPrefixASessionID(name) {
			t.Errorf("pooled name %q can be the front of a session id", name)
		}
	}
}

// The hex property enforced where it actually bites: a name somebody *chooses*.
//
// Over the pool it is a rule nobody would break by accident - nothing is going
// to name an agent "ravi" and get "cafe". A person naming one `beefcafe` is a
// different matter, and the harm is not a cosmetic collision: `wake status`
// prints eight characters of a session id and invites them to be copied, and
// matchSession resolves a whole *name* before it looks at the id space, because
// exactness beats partiality. So a chosen hex name that matches a printed short
// id silently wins, and somebody who copied the id column lands in a different
// agent's live conversation and types into it.
//
// Mutation check: removing the call from normalizeName leaves this failing at
// `normalizeName("beefcafe") was accepted`.
//
// **The hyphenated entries are the ones an alphabet test let through**, and they
// are not contrived: `wake status` prints eight characters, an operator copies a
// few more, and `a4f78b3d-1e2f` is a name the old rule accepted and matchSession
// resolves ahead of the id space.
func TestAChosenNameThatCouldBeASessionIDPrefixIsRefused(t *testing.T) {
	for _, name := range []string{
		"beefcafe", "dead", "ada", "cab", "face", "b0a7", "BeefCafe",
		"a4f78b3d-1e2f", "a4f78b3d-", "a4f78b3d-1e2f-4a0b", "A4F78B3D-1E2F",
	} {
		got, err := normalizeName(name)
		if err == nil {
			t.Errorf("normalizeName(%q) was accepted as %q; it can be the front of a session id", name, got)
			continue
		}
		if !strings.Contains(err.Error(), "session id") {
			t.Errorf("the refusal of %q does not say why: %v", name, err)
		}
	}
}

// …and a name that cannot be the front of a UUID is still a name, or the guard
// has quietly become "no short names".
//
// `abcdefabcdef` is the entry the positional rule *added* to this list: twelve
// hex characters, refused by the old alphabet test, and not a prefix of any
// session id because position 8 of a canonical UUID is a hyphen. Refusing a name
// that is not a hazard is the other half of getting the question right.
func TestANameThatCannotBeTheFrontOfASessionIDIsStillAName(t *testing.T) {
	for _, name := range []string{"beefcake", "deal", "ivy", "cap", "fax", "b0a7z", "abcdefabcdef", "a4f78b3dx"} {
		if _, err := normalizeName(name); err != nil {
			t.Errorf("normalizeName(%q) was refused, and it cannot be the front of a UUID: %v", name, err)
		}
	}
}

// The relationship the reaper's safety rests on, asserted rather than described.
//
// A name reaches the child's argv as `--name`, and the reaper's whole proof that
// a process group is still the agent it recorded is that the session UUID is
// somewhere in that argv. A name able to *contain* a UUID forges that proof:
// after pid reuse, verifyAgent(pid, idA) inspects a process that is now B, finds
// idA inside B's --name, and SIGKILLs B's group mid-Edit.
//
// Nothing but the length bound prevents it. Every character of a UUID passes
// isNameRune, and a whole UUID is 36 characters, so couldPrefixASessionID does
// not catch one either. maxNameLen < 36 is the guard, and until this test it was
// unpinned: TestTheDaemonRefusesANameItCannotUse derives its input *from*
// maxNameLen, so raising the constant kept it green.
//
// Mutation check: raising maxNameLen to 40 leaves this failing at "maxNameLen is
// 40 against a session id of 36".
func TestANameCannotCarryASessionIDIntoAnAgentsArgv(t *testing.T) {
	// Starts with a letter, so it is refused for its length rather than for
	// isNameStart - which is the property under test.
	const victim = "a11a0000-0000-4000-8000-00000000a11a"

	if live := len(uuid.NewString()); live != len(victim) {
		t.Fatalf("a real session id is %d characters and this test uses %d; the arithmetic below is about the wrong number", live, len(victim))
	}
	if maxNameLen >= len(victim) {
		t.Fatalf("maxNameLen is %d against a session id of %d characters, so a name can carry one into an agent's argv "+
			"and forge the reaper's only proof of identity", maxNameLen, len(victim))
	}
	for _, name := range []string{victim, "wake-" + victim, victim + "-x"} {
		if got, err := normalizeName(name); err == nil {
			t.Errorf("normalizeName(%q) = %q, which puts a session id in another agent's argv", name, got)
		}
	}
}

// The property the whole registry exists for, measured at the fleet the spec
// sizes for rather than argued about.
func TestAFullFleetGetsDistinctNames(t *testing.T) {
	r := newNameRegistry()

	seen := make(map[string]int, specFleetCap)
	for i := range specFleetCap {
		name, err := r.claim("")
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if at, dup := seen[name]; dup {
			t.Fatalf("session %d was named %q, which session %d already holds", i, name, at)
		}
		seen[name] = i
	}
	t.Logf("%d sessions, %d distinct names, %d pooled names left free", specFleetCap, len(seen), len(namePool)-len(seen))
}

// A name comes back when its session ends, which is what keeps a long-running
// daemon from exhausting the pool by churning sessions.
func TestANameIsFreeAgainOnceItsSessionEnds(t *testing.T) {
	r := newNameRegistry()

	first, err := r.claim("sydney")
	if err != nil {
		t.Fatalf("claim sydney: %v", err)
	}
	if _, err := r.claim("sydney"); err == nil {
		t.Fatal("two live sessions were both named sydney")
	}

	r.release(first)
	again, err := r.claim("sydney")
	if err != nil {
		t.Fatalf("sydney was not free again after its session ended: %v", err)
	}
	if again != "sydney" {
		t.Errorf("re-claimed %q, want sydney", again)
	}
}

// Exhaustion is reached rather than reasoned about: every pooled name is taken
// and one more session is started.
//
// The ruling is that it is answered rather than refused. A name is display and
// an id is identity, so running out of display names must never be the reason
// somebody cannot start an agent - and a numbered name is still a name a person
// can say out loud, which a UUID is not.
func TestAnExhaustedPoolStillNamesTheNextSession(t *testing.T) {
	r := newNameRegistry()

	held := make(map[string]struct{}, len(namePool))
	for range namePool {
		name, err := r.claim("")
		if err != nil {
			t.Fatalf("filling the pool: %v", err)
		}
		held[name] = struct{}{}
	}
	if len(held) != len(namePool) {
		t.Fatalf("filling the pool produced %d distinct names out of %d", len(held), len(namePool))
	}

	for i := range 5 {
		extra, err := r.claim("")
		if err != nil {
			t.Fatalf("session %d past the pool was refused a name: %v", len(namePool)+i, err)
		}
		if _, dup := held[extra]; dup {
			t.Fatalf("session %d past the pool was named %q, which is already held", len(namePool)+i, extra)
		}
		if !strings.ContainsRune(extra, nameOrdinalSep) {
			t.Errorf("a name past the pool is %q, want a pooled name with an ordinal on it", extra)
		}
		if _, err := normalizeName(extra); err != nil {
			t.Errorf("a name past the pool is not one the daemon would accept: %q: %v", extra, err)
		}
		held[extra] = struct{}{}
	}
	t.Logf("pool of %d exhausted; the next 5 sessions were named without a collision", len(namePool))
}

// The registry is reached from every client's connection goroutine at once, so
// "no two live sessions share a name" has to hold under concurrency and not
// merely in sequence.
//
// Mutation check: dropping the mutex from claim leaves this failing under
// -race, and failing on the duplicate count without it.
func TestConcurrentClaimsNeverCollide(t *testing.T) {
	r := newNameRegistry()

	const claimers = 40
	names := make([]string, claimers)
	var wg sync.WaitGroup
	for i := range claimers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name, err := r.claim("")
			if err != nil {
				t.Errorf("concurrent claim: %v", err)
				return
			}
			names[i] = name
		}()
	}
	wg.Wait()

	seen := make(map[string]int, claimers)
	for i, name := range names {
		if at, dup := seen[name]; dup {
			t.Fatalf("claimers %d and %d were both given %q", at, i, name)
		}
		seen[name] = i
	}
}

// A pool that always answered with its first free name would pass every test
// above and would not be random: every fleet would be alex, john, sydney in
// that order, and the name would carry no information at all.
//
// Asserted over many draws from a one-session registry rather than over one
// draw, so this cannot flake: a picker that is not constant produces at least
// two distinct answers in 40 draws with probability 1 - 64^-39.
func TestNamesAreDrawnAtRandomRatherThanInOrder(t *testing.T) {
	r := newNameRegistry()

	seen := make(map[string]struct{})
	for range 40 {
		name, err := r.claim("")
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		seen[name] = struct{}{}
		r.release(name)
	}
	if len(seen) < 2 {
		t.Fatalf("40 draws from a %d-name pool produced only %v, so names are handed out in order rather than at random", len(namePool), seen)
	}
}

// What the daemon refuses, and why each one matters. A name is display; the
// things it must not be able to become are all here.
func TestTheDaemonRefusesANameItCannotUse(t *testing.T) {
	tests := []struct {
		what string
		name string
		want string
	}{
		{what: "a name longer than the column that shows it", name: strings.Repeat("a", maxNameLen+1), want: "at most"},
		{what: "a name with a space in it", name: "two words", want: "letters"},
		{what: "a name starting with a digit", name: "4chan", want: "start"},
		{what: "a name that is only punctuation", name: "--", want: "start"},
		{what: "a name with a shell metacharacter", name: "alex;rm", want: "letters"},
	}
	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			_, err := normalizeName(tc.name)
			if err == nil {
				t.Fatalf("normalizeName(%q) was accepted", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal does not say what is wrong: %v", err)
			}
		})
	}
}

// A name arrives from a person typing it, so it is folded rather than refused
// for a capital letter. Folding is what makes `wake attach Sydney` reach the
// session called sydney.
func TestANameIsFoldedRatherThanRefusedForItsCase(t *testing.T) {
	got, err := normalizeName("  SyDnEy ")
	if err != nil {
		t.Fatalf("normalizeName: %v", err)
	}
	if got != "sydney" {
		t.Errorf("normalizeName(\"  SyDnEy \") = %q, want sydney", got)
	}
}

// An empty request is not an error - it is what bare `wake` sends, and it means
// "pick one".
func TestNoNameRequestedIsARequestForAPooledName(t *testing.T) {
	r := newNameRegistry()

	got, err := r.claim("")
	if err != nil {
		t.Fatalf("claim with no name requested: %v", err)
	}
	if got == "" {
		t.Fatal("a session with no name requested was left unnamed")
	}
	if !pooled(got) {
		t.Errorf("claim(\"\") = %q, which is not from the pool", got)
	}
}

// Releasing a name nothing holds is a no-op rather than a panic: retire runs on
// every path out of a session, including ones where the spawn never got as far
// as claiming.
func TestReleasingANameNothingHoldsIsHarmless(t *testing.T) {
	r := newNameRegistry()
	r.release("")
	r.release("nobody")

	got, err := r.claim("nobody")
	if err != nil {
		t.Fatalf("claim after a spurious release: %v", err)
	}
	if got != "nobody" {
		t.Errorf("claim(\"nobody\") = %q", got)
	}
}

func pooled(name string) bool {
	for _, n := range namePool {
		if n == name {
			return true
		}
	}
	return false
}

// The refusal a person reads when they ask for a name somebody else is using.
// It has to name the name, or `wake new sydney` fails with nothing to act on.
func TestTakingANameSomebodyElseHoldsSaysWhich(t *testing.T) {
	r := newNameRegistry()
	if _, err := r.claim("sydney"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	_, err := r.claim("Sydney")
	if err == nil {
		t.Fatal("a name already held was handed out again under a different case")
	}
	if !strings.Contains(err.Error(), "sydney") {
		t.Errorf("the refusal does not name the name: %v", err)
	}
}

// The numbered fallback has to terminate against a registry that already holds
// every ordinal it would try first.
func TestTheNumberedFallbackSkipsOrdinalsAlreadyHeld(t *testing.T) {
	r := newNameRegistry()
	for _, n := range namePool {
		if _, err := r.claim(n); err != nil {
			t.Fatalf("claim %s: %v", n, err)
		}
	}
	// Every "<pooled>-2" as well, so the fallback cannot answer with its first
	// candidate for any base it might draw.
	for _, n := range namePool {
		if _, err := r.claim(fmt.Sprintf("%s%c2", n, nameOrdinalSep)); err != nil {
			t.Fatalf("claim %s-2: %v", n, err)
		}
	}

	got, err := r.claim("")
	if err != nil {
		t.Fatalf("claim past the pool and every -2: %v", err)
	}
	if !strings.HasSuffix(got, "-3") {
		t.Errorf("claim past the pool and every -2 = %q, want an ordinal past 2", got)
	}
}

// routerFile is the client-side routing that this package's reservation
// exists to protect.
//
// It is read as *text* rather than reached through its constants, and that is
// the whole point of the test below: importing core.BroadcastName proves the
// two spellings agree and says nothing about whether there is a third word.
// The file's own source is the only place that fact lives.
const routerFile = "../core/router.go"

// TestTheReservedNamesAreExactlyTheOnesRoutingSpends holds the two halves of
// the reservation against each other, in both directions.
//
// A hand-written `want` of the two constants was written here first and
// deleted, because it is the shape docs/notes/decisions.md names: a literal in
// a test enumerating what the code already declares. It would have caught a
// rename and a deletion, and passed - silently, permanently - for a third
// routing word added to router.go and never reserved here, which is the only
// way this can realistically go wrong. Deriving the set from the router's own
// AST is about fifteen lines and closes it.
func TestTheReservedNamesAreExactlyTheOnesRoutingSpends(t *testing.T) {
	spent := nameShapedLiteralsIn(t, routerFile)
	if len(spent) == 0 {
		t.Fatalf("%s names no word an agent could be called: either routing stopped spending names or this derivation is broken, and either way it is asserting nothing", routerFile)
	}
	if !maps.Equal(reservedNames, spent) {
		t.Errorf("reserved = %v, routing spends %v: a name the router treats specially and the pool hands out is an agent nobody can address, and one it reserves for nothing is a name somebody cannot have",
			sortedKeys(reservedNames), sortedKeys(spent))
	}
}

// The behavioural half. The bijection above is satisfied by a map with the
// right contents and no effect on anything - deleting the reservedNames check
// from normalizeName leaves it green.
func TestAReservedNameIsRefusedWhereverItIsAskedFor(t *testing.T) {
	if len(reservedNames) == 0 {
		t.Fatal("nothing is reserved: this test is asserting nothing")
	}
	for name := range reservedNames {
		// Folded first, so the refusal cannot be dodged by the shift key.
		for _, requested := range []string{name, strings.ToUpper(name), "  " + name + "  "} {
			got, err := normalizeName(requested)
			switch {
			case err == nil:
				t.Errorf("normalizeName(%q) = %q with no error: `wake new %s` would take a word the room routes on, and broadcast would stop working with nothing said", requested, got, requested)
			case !strings.Contains(err.Error(), name):
				t.Errorf("normalizeName(%q) refused without naming %q: %v", requested, name, err)
			}
		}
		if _, err := newNameRegistry().claim(name); err == nil {
			t.Errorf("claim(%q) succeeded: the refusal has to sit on the path a spawn actually takes, not only on the validator beside it", name)
		}
		if slices.Contains(namePool, name) {
			t.Errorf("the pool hands out %q, which routing spends: an agent nobody could address", name)
		}
	}
}

// nameShapedLiteralsIn returns every string literal in a file that could be an
// agent's name.
//
// It applies this package's own character rules rather than a second copy of
// them, so router.go's "@" and any format string are excluded structurally and
// not by an excuse table that would then need maintaining. normalizeName
// itself cannot be the predicate: it now refuses reserved names, so using it
// here would make the derivation agree with whatever this package already
// believes.
//
// Every literal is scanned rather than only the const block, because a routing
// word declared as a var or written inline is the same word. The consequence
// is worth stating: a name-shaped string literal in router.go is presumed to
// be a word routing spends, and a file so small and so pure has no other use
// for one.
func nameShapedLiteralsIn(t *testing.T, path string) map[string]bool {
	t.Helper()

	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	// An import path is a string literal too, and "strings" and "unicode" are
	// both name-shaped - the first draft of this reported that routing spends
	// them. Skipped structurally rather than listed, which is what
	// internal/core/airlock_test.go does with the same problem.
	imports := map[*ast.BasicLit]bool{}
	for _, spec := range parsed.Imports {
		imports[spec.Path] = true
	}

	found, literals := map[string]bool{}, 0
	ast.Inspect(parsed, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || imports[lit] {
			return true
		}
		literals++
		text, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Errorf("%s: cannot read the literal %s: %v", path, lit.Value, err)
			return true
		}
		if couldBeAName(text) {
			found[text] = true
		}
		return true
	})
	if literals == 0 {
		t.Fatalf("no string literal at all in %s: the parse is broken, and a broken parse would report that routing spends nothing", path)
	}
	return found
}

// couldBeAName is normalizeName's shape rules and nothing else, composed from
// the same predicates rather than restated.
func couldBeAName(s string) bool {
	if s == "" || len(s) > maxNameLen || !isNameStart(rune(s[0])) {
		return false
	}
	for _, r := range s {
		if !isNameRune(r) {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]bool) []string { return slices.Sorted(maps.Keys(m)) }

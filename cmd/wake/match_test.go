package main

// Reaching a session by the word somebody has in front of them: a name, an id,
// or the front of either - and the refusal when that word reaches more than
// one agent.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// --- attaching by name --------------------------------------------------------

func namedFleet() rpc.Status {
	return rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "sydney", Label: "dev-5748", State: rpc.StateWorking},
		{ID: idBeta, Name: "marco", Label: "main", State: rpc.StateIdle},
	}}
}

// The point of the whole verb: `wake attach 4f78b3d7` is not an interface
// anybody wants.
func TestAttachFindsASessionByName(t *testing.T) {
	d := startFakeDaemon(t, 0, namedFleet())

	got, _, err := liveSession(d.socket, "marco")
	if err != nil {
		t.Fatalf("liveSession by name: %v", err)
	}
	if got.ID != idBeta {
		t.Errorf("attaching to marco reached %s, want %s", got.ID, idBeta)
	}
}

// A name is something a person types, and the daemon lower-cases every name it
// assigns. `wake attach Sydney` reaching nothing would be a trap.
func TestAttachFindsANameWhateverItsCase(t *testing.T) {
	d := startFakeDaemon(t, 0, namedFleet())

	got, _, err := liveSession(d.socket, "SyDnEy")
	if err != nil {
		t.Fatalf("liveSession on a name in mixed case: %v", err)
	}
	if got.ID != idAlpha {
		t.Errorf("attaching to SyDnEy reached %s, want %s", got.ID, idAlpha)
	}
}

// The same prefix rule the ids already had, extended rather than duplicated.
func TestAttachTakesAUniquePrefixOfAName(t *testing.T) {
	d := startFakeDaemon(t, 0, namedFleet())

	got, _, err := liveSession(d.socket, "syd")
	if err != nil {
		t.Fatalf("liveSession on a name prefix: %v", err)
	}
	if got.ID != idAlpha {
		t.Errorf("a prefix of sydney resolved to %s", got.ID)
	}
}

// An exact name must beat a prefix, or a session called `sam` becomes
// unreachable the moment a `sammy` exists - refused as ambiguous with itself.
//
// Both cases of the word, because the two rules meet here and only here. The
// prefix pass folds case on its own, so a case-sensitive exact pass still finds
// `sam` from `sam` - by falling through to the prefix pass, which is
// unambiguous while `sammy` is not there. Put `sammy` beside it and a
// case-sensitive exact pass has nothing to answer with.
//
// Mutation check: folding the exact-name pass into the prefix pass leaves this
// failing at `"sam" names 2 sessions`; making that pass case-sensitive leaves
// the SAM subtest failing the same way.
func TestAnExactNameBeatsALongerOneItPrefixes(t *testing.T) {
	for _, want := range []string{"sam", "SAM"} {
		t.Run(want, func(t *testing.T) {
			d := startFakeDaemon(t, 0, rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
				{ID: idAlpha, Name: "sam", State: rpc.StateIdle},
				{ID: idBeta, Name: "sammy", State: rpc.StateIdle},
			}})

			got, _, err := liveSession(d.socket, want)
			if err != nil {
				t.Fatalf("a session whose name prefixes another was unreachable: %v", err)
			}
			if got.ID != idAlpha {
				t.Errorf("attaching to %s reached %s, want %s", want, got.ID, idAlpha)
			}
		})
	}
}

// A whole name beats a partial id, and this is the rule rather than an
// accident: what somebody typed is exactly one session's name and is only the
// front of the other's id, so one reading is complete and the other is a
// coincidence. No pooled name can even reach this - names_test.go holds that
// none is entirely hex - so it takes a name somebody chose to get here.
//
// Mutation check: folding the exact-name pass into the prefix pass leaves this
// failing at `"abc" names 2 sessions`.
func TestAWholeNameBeatsAPartialId(t *testing.T) {
	const hexish = "abc10000-0000-4000-8000-000000000001"
	d := startFakeDaemon(t, 0, rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: hexish, Name: "sydney", State: rpc.StateIdle},
		{ID: idBeta, Name: "abc", State: rpc.StateIdle},
	}})

	got, _, err := liveSession(d.socket, "abc")
	if err != nil {
		t.Fatalf("a session named abc was unreachable by its own name: %v", err)
	}
	if got.ID != idBeta {
		t.Errorf("attaching to abc reached %s, want the session actually called abc", got.ID)
	}
}

// When neither reading is complete, the two spaces collide and it is refused.
// Names and ids are searched together for exactly this: two matchers, each
// unambiguous within its own space, would both answer and one would win by
// accident of which ran first.
//
// Mutation check: dropping the id-prefix half of prefixedBy leaves this failing
// at "an ambiguous word picked a session".
func TestAPrefixThatSpansBothNamesAndIdsIsRefused(t *testing.T) {
	const hexish = "abc10000-0000-4000-8000-000000000001"
	d := startFakeDaemon(t, 0, rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: hexish, Name: "sydney", State: rpc.StateIdle},
		{ID: idBeta, Name: "abcd", State: rpc.StateIdle},
	}})

	_, _, err := liveSession(d.socket, "abc")
	if err == nil {
		t.Fatal("an ambiguous word picked a session")
	}
	if !strings.Contains(err.Error(), "names 2 sessions") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// A name goes back to the pool when its session ends, and a status report keeps
// the ending for a while - so one word can be a live agent and a remembered one
// at the same time. The live one wins, exactly as it does for an id prefix.
//
// Mutation check: putting ended sessions in the same bucket as live ones leaves
// this failing at `"sydney" names 2 sessions`.
func TestALiveSessionWinsANameItSharesWithAnEndedOne(t *testing.T) {
	d := startFakeDaemon(t, 0, rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "sydney", State: rpc.StateEnded, Error: "exit status 1"},
		{ID: idBeta, Name: "sydney", State: rpc.StateWorking},
	}})

	got, _, err := liveSession(d.socket, "sydney")
	if err != nil {
		t.Fatalf("a name shared with an ended session was refused: %v", err)
	}
	if got.ID != idBeta {
		t.Errorf("attaching to sydney reached the ended session %s", got.ID)
	}
}

// The refusal has to name both spaces, because the person typing does not know
// which of the two they got wrong.
func TestAWordThatMatchesNothingSaysSoInBothSpaces(t *testing.T) {
	d := startFakeDaemon(t, 0, namedFleet())

	_, _, err := liveSession(d.socket, "nobody")
	if err == nil {
		t.Fatal("a word matching nothing found a session")
	}
	if !strings.Contains(err.Error(), "named") || !strings.Contains(err.Error(), "starts with") {
		t.Errorf("the refusal does not cover both names and ids: %v", err)
	}
	// And it lists what there is, with labels, so choosing again costs one
	// command rather than two.
	if !strings.Contains(err.Error(), "sydney <> dev-5748") {
		t.Errorf("the listing does not say what each session is working on: %v", err)
	}
}

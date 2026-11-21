package ui

// The matcher the jump picker (and, later, the sidebar's search bar) finds an agent by:
// a few letters of its name, in order, with the name's own prefix and substring
// outranking letters found further apart.

import (
	"slices"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func namedAgents(names ...string) []Agent {
	out := make([]Agent, len(names))
	for i, n := range names {
		out[i] = Agent{ID: "id-" + n, Name: n, State: rpc.StateIdle}
	}
	return out
}

func namesOfAgents(as []Agent) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.Name
	}
	return out
}

func TestFindClassRanksPrefixThenSubstringThenLettersInOrder(t *testing.T) {
	for _, tc := range []struct {
		query, name string
		class       int
		ok          bool
	}{
		{"web", "web-blur", 0, true},
		{"blu", "web-blur", 1, true},
		{"wbl", "web-blur", 2, true},
		{"blu", "docs-bulk-upload", 2, true}, // b, l, u in order, apart
		{"xyz", "web-blur", 0, false},
		{"blub", "web-blur", 0, false}, // every letter, in order, or no match
		{"BLU", "Web-Blur", 1, true},
		{"", "anything", 0, true},
		{"  ", "anything", 0, true},
		{"web blur", "web-blur", 2, true}, // a space is not a letter to find
	} {
		class, ok := findClass(tc.query, tc.name)
		if ok != tc.ok || ok && class != tc.class {
			t.Errorf("findClass(%q, %q) = %d, %v; want %d, %v", tc.query, tc.name, class, ok, tc.class, tc.ok)
		}
	}
}

func TestMatchAgentsPutsWhoNeedsYouFirstThenTheBestMatchThenTheGivenOrder(t *testing.T) {
	in := namedAgents("web-blur", "ui-blueprint", "infra-blue-green", "qa-visual-blur", "docs-bulk-upload", "pay-ledger")
	in[1].State = rpc.StateBlocked
	got := namesOfAgents(matchAgents("blu", in))
	want := []string{"ui-blueprint", "web-blur", "infra-blue-green", "qa-visual-blur", "docs-bulk-upload"}
	if !slices.Equal(got, want) {
		t.Errorf("matches %q, want %q: blocked first, then substring in the given order, then letters apart", got, want)
	}
}

func TestAPrefixOutranksASubstringAndTheGivenOrderBreaksATie(t *testing.T) {
	got := namesOfAgents(matchAgents("blur", namedAgents("web-blur", "qa-blur", "blur-fix", "blurry")))
	if want := []string{"blur-fix", "blurry", "web-blur", "qa-blur"}; !slices.Equal(got, want) {
		t.Errorf("matches %q, want %q", got, want)
	}
}

func TestAnEmptyQueryKeepsEveryAgentInTheGivenOrderBlockedFirst(t *testing.T) {
	in := namedAgents("a-one", "b-two", "c-three")
	in[2].State = rpc.StateBlocked
	if got, want := namesOfAgents(matchAgents("", in)), []string{"c-three", "a-one", "b-two"}; !slices.Equal(got, want) {
		t.Errorf("an empty query gave %q, want %q", got, want)
	}
}

func TestMatchAgentsLeavesItsInputAlone(t *testing.T) {
	in := namedAgents("web-blur", "ui-blueprint", "infra-blue-green")
	in[2].State = rpc.StateBlocked
	before := namesOfAgents(in)
	matchAgents("blu", in)
	if !slices.Equal(namesOfAgents(in), before) {
		t.Errorf("the input was reordered to %q from %q", namesOfAgents(in), before)
	}
}

func TestNothingMatchesNothing(t *testing.T) {
	if got := matchAgents("zzq", namedAgents("web-blur", "pay-ledger")); len(got) != 0 {
		t.Errorf("zzq matched %q", namesOfAgents(got))
	}
}

package core

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestIsModelReply(t *testing.T) {
	cases := map[string]bool{
		"Current model: Opus 5 (1M context) (effort: xhigh)\nUsage: /model <name>.": true,
		"Current model: Sonnet 5 (effort: medium)":                                  true,
		"  Current model: Fable 5 (effort: low)":                                    true,
		"Sure, the current model is opus":                                           false,
		"":                                                                          false,
	}
	for in, want := range cases {
		if got := IsModelReply(in); got != want {
			t.Errorf("IsModelReply(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestModelFromModelReply(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
		ok             bool
	}{
		// The recorded shape: a model name that itself carries a parenthesised
		// note, the effort clause, and a second usage line - all of which the
		// parse must strip off without eating the "(1M context)" the name owns.
		{"the recorded reply", "Current model: Opus 5 (1M context) (effort: xhigh)\nUsage: /model <name>.", "Opus 5 (1M context)", true},
		{"no note", "Current model: Sonnet 5 (effort: medium)", "Sonnet 5", true},
		{"leading space", "  Current model: Fable 5 (effort: low)", "Fable 5", true},
		{"not a model reply", "Sure, the current model is opus", "", false},
		// The live shape since 2.1.28x wraps the name in backticks, with the clause
		// or without it (bare-model-effort.jsonl, bare-model-no-effort.jsonl).
		{"the live reply", "Current model: `Opus 5.5 (1M context)` (effort: xhigh)\nUsage: /model <name>.", "Opus 5.5 (1M context)", true},
		{"the live reply, no effort", "Current model: `Opus 5.5 (default)`\nUsage: /model <name>.", "Opus 5.5 (default)", true},
		// Neither the backticks nor the clause: a line an agent wrote, not a probe's.
		{"a bare name with no clause", "Current model: is the phrase this agent chose to open with", "", false},
		{"the prefix and nothing else", "Current model:", "", false},
		{"empty", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ModelFromModelReply(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Errorf("ModelFromModelReply(%q) = (%q,%v), want (%q,%v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestEffortFromModelReply(t *testing.T) {
	lvl, ok := EffortFromModelReply("Current model: Opus 5 (1M context) (effort: xhigh)")
	if !ok || lvl != "xhigh" {
		t.Fatalf("got (%q,%v), want (\"xhigh\",true)", lvl, ok)
	}
	if _, ok := EffortFromModelReply("Current model: Opus 5"); ok {
		t.Error("a reply with no effort clause must not parse")
	}
	if _, ok := EffortFromModelReply("Current model: X (effort: bogus)"); ok {
		t.Error("an effort not in EffortCommands must not parse")
	}
}

// recordedResults is every result text a fixture carries, read through the
// decoder so the parse sees exactly what the daemon would.
func recordedResults(t *testing.T, path string) []string {
	t.Helper()
	var out []string
	for n, line := range fixtureLines(t, path) {
		evs, err := DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("%s line %d: %v", path, n, err)
		}
		for _, ev := range evs {
			if ev.Kind == KindTurnEnd {
				out = append(out, ev.Text)
			}
		}
	}
	return out
}

// The two recorded peers, in the order the listing printed them.
var recordedPeers = []Peer{
	{Name: "wf-beta", Dir: "/private/tmp/wake-rec/beta"},
	{Name: "wf-alpha", Dir: "/private/tmp/wake-rec/alpha"},
}

// Every result the bare one-shot's recordings carry is read, the empty form
// included.
func TestEveryRecordedLocalReplyParses(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		results [][]Peer
	}{
		{"../../testdata/stream/list-agents-bare.jsonl", [][]Peer{recordedPeers}},
		{"../../testdata/stream/list-agents-bare-empty.jsonl", [][]Peer{nil}},
	} {
		results := recordedResults(t, tc.fixture)
		if len(results) != len(tc.results) {
			t.Fatalf("%s carries %d results, want %d", tc.fixture, len(results), len(tc.results))
		}
		for i, text := range results {
			peers, ok := PeersFromListAgents(text)
			if !ok || !reflect.DeepEqual(peers, tc.results[i]) {
				t.Errorf("%s result %d = (%+v, %v), want (%+v, true)", tc.fixture, i, peers, ok, tc.results[i])
			}
		}
	}
}

// list-agents.jsonl is a live session's: its /rename reply is read as one, and
// its listings, opening with a self line the bare one-shot never prints, are
// refused whole.
func TestTheRecordedRenameParsesAndALiveListingIsRefused(t *testing.T) {
	results := recordedResults(t, "../../testdata/stream/list-agents.jsonl")
	if len(results) != 3 {
		t.Fatalf("list-agents.jsonl carries %d results, want a listing, the rename and a listing", len(results))
	}
	if got, ok := RenamedFromReply(results[1]); !ok || got != "wf-delta" {
		t.Errorf("RenamedFromReply(%q) = (%q, %v), want (\"wf-delta\", true)", results[1], got, ok)
	}
	for _, text := range results {
		if peers, ok := PeersFromListAgents(text); ok || peers != nil {
			t.Errorf("PeersFromListAgents(%q) = (%+v, %v), want (nil, false)", text, peers, ok)
		}
	}
}

const (
	betaRow  = "  [idle]  ·  wf-beta  ·  /private/tmp/wake-rec/beta  ·  started 19s ago"
	alphaRow = "  [idle]  ·  wf-alpha  ·  /private/tmp/wake-rec/alpha  ·  started 19s ago"
)

// others is the recorded Other-sessions section, both rows under its header.
const others = "Other Claude sessions (2):\n" + betaRow + "\n" + alphaRow

// Any other `<Title> (<n>):` section - the subagents and teammates Claude's
// docs name, which a bare one-shot has none of - is counted and skipped, as
// tolerance of CLI drift. Its rows are never parsed.
func TestOtherSectionsAreCountedAndSkipped(t *testing.T) {
	const subagents = "Subagents (1):\n  explorer (running, 2s)"
	for _, tc := range []struct {
		name, text string
		peers      []Peer
	}{
		{"a section before the peers", subagents + "\n\n" + others, recordedPeers},
		{"a section after the peers", others + "\n\n" + subagents, recordedPeers},
		{"sections back to back, ended by the next header", subagents +
			"\nTeammates (2):\n  a\n  b\n\n" + others, recordedPeers},
		{"a skipped section alone", subagents, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			peers, ok := PeersFromListAgents(tc.text)
			if !ok || !reflect.DeepEqual(peers, tc.peers) {
				t.Errorf("PeersFromListAgents = (%+v, %v), want (%+v, true)", peers, ok, tc.peers)
			}
		})
	}
}

// A shape the parser was not shown is refused whole - never the rows it could
// read beside one it could not.
func TestAnUnrecognisedListingIsRefusedWhole(t *testing.T) {
	cases := map[string]string{
		"empty":                               "",
		"prose":                               "I can list the other sessions if you like.",
		"the model reply":                     "Current model: Opus 5 (effort: xhigh)",
		"the rename reply":                    "Session renamed to: wf-delta",
		"the count disagrees":                 "Other Claude sessions (3):\n" + betaRow + "\n" + alphaRow,
		"a count that is no number":           "Other Claude sessions (two):\n" + betaRow + "\n" + alphaRow,
		"a skipped section's count disagrees": "Subagents (2):\n  explorer\n\n" + others,
		"an unknown line after the sections":  others + "\n\nTip: message a session by its name.",
		"an unknown line inside a section":    "Other Claude sessions (2):\n" + betaRow + "\nand one more\n" + alphaRow,
		"a row under no header":               "Other Claude sessions (1):\n" + betaRow + "\n\n" + alphaRow,
		"rows parted from their header":       "Other Claude sessions (2):\n\n" + betaRow + "\n" + alphaRow,
		"a row with a column missing":         "Other Claude sessions (1):\n  [idle]  ·  wf-beta  ·  started 19s ago",
		"a row with a column more":            "Other Claude sessions (1):\n" + betaRow + "  ·  remote",
		"a state unbracketed":                 "Other Claude sessions (1):\n  idle  ·  wf-beta  ·  /private/tmp/wake-rec/beta  ·  started 19s ago",
		"an empty state":                      "Other Claude sessions (1):\n  []  ·  wf-beta  ·  /private/tmp/wake-rec/beta  ·  started 19s ago",
		"a relative directory":                "Other Claude sessions (1):\n  [idle]  ·  wf-beta  ·  tmp/beta  ·  started 19s ago",
		"a blank name":                        "Other Claude sessions (1):\n  [idle]  ·    ·  /private/tmp/wake-rec/beta  ·  started 19s ago",
		"the empty form with more before it":  others + "\n\nNo subagents, teammates or other Claude sessions.",
		"the empty form with more after it":   "No subagents, teammates or other Claude sessions.\n" + betaRow,
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if peers, ok := PeersFromListAgents(text); ok || peers != nil {
				t.Errorf("PeersFromListAgents = (%+v, %v), want (nil, false)", peers, ok)
			}
		})
	}
}

func TestRenamedFromReply(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
		ok             bool
	}{
		{"the recorded reply", "Session renamed to: wf-delta", "wf-delta", true},
		{"leading space", "  Session renamed to: wf-delta\n", "wf-delta", true},
		{"a second line", "Session renamed to: wf-delta\nnote", "wf-delta", true},
		{"no name", "Session renamed to: ", "", false},
		{"prose", "I renamed the session to wf-delta", "", false},
		{"empty", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := RenamedFromReply(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Errorf("RenamedFromReply(%q) = (%q,%v), want (%q,%v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// The live /model replies the probe reads (2.1.288): the name in backticks,
// with the effort clause when the session has a level and without it when not.
func TestTheRecordedLiveModelRepliesParse(t *testing.T) {
	for _, tc := range []struct {
		fixture, model, effort string
	}{
		{"../../testdata/stream/bare-model-effort.jsonl", "Opus 5.5 (default)", EffortXHigh},
		{"../../testdata/stream/bare-model-no-effort.jsonl", "Opus 5.5 (default)", ""},
	} {
		results := recordedResults(t, tc.fixture)
		if len(results) != 1 {
			t.Fatalf("%s carries %d results, want the one reply", tc.fixture, len(results))
		}
		if model, ok := ModelFromModelReply(results[0]); !ok || model != tc.model {
			t.Errorf("%s: ModelFromModelReply = (%q, %v), want (%q, true)", tc.fixture, model, ok, tc.model)
		}
		if effort, ok := EffortFromModelReply(results[0]); effort != tc.effort || ok != (tc.effort != "") {
			t.Errorf("%s: EffortFromModelReply = (%q, %v), want %q", tc.fixture, effort, ok, tc.effort)
		}
	}
}

// On disk a /model reply is a system/local_command record, which the transcript
// decoder drops whole, so a probe's reply - with the effort clause or without it
// - never restores as anybody's speech (model-reply-*.jsonl).
func TestARecordedModelReplyOnDiskRestoresAsNothing(t *testing.T) {
	for _, fixture := range []string{
		"../../testdata/transcript/model-reply-effort.jsonl",
		"../../testdata/transcript/model-reply-no-effort.jsonl",
	} {
		lines := fixtureLines(t, fixture)
		if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, modelReplyPrefix) }) {
			t.Fatalf("%s holds no /model reply, so this asserts nothing", fixture)
		}
		for n, line := range lines {
			evs, err := DecodeTranscriptLine([]byte(line))
			if err != nil {
				t.Fatalf("%s:%d: %v", fixture, n+1, err)
			}
			for _, ev := range evs {
				if strings.Contains(ev.Text, modelReplyPrefix) {
					t.Errorf("%s:%d restored the probe's reply as %s: %q", fixture, n+1, ev.Kind, ev.Text)
				}
			}
		}
	}
}

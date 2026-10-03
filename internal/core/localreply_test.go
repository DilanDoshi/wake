package core

import (
	"reflect"
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

// renamedPeers is list-agents-bare-renamed.jsonl's: a spaced name never renamed,
// beside wf-alpha renamed to wf-delta after holding its name for a while.
var renamedPeers = []Peer{
	{Name: "wf beta", Dir: "/private/tmp/wake-rec/beta"},
	{Name: "wf-delta", Dir: "/private/tmp/wake-rec/alpha"},
}

// Every result the bare one-shot's recordings carry is read whole, the empty
// form and a renamed session's row included.
func TestEveryRecordedLocalReplyParses(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		results [][]Peer
	}{
		{"../../testdata/stream/list-agents-bare.jsonl", [][]Peer{recordedPeers}},
		{"../../testdata/stream/list-agents-bare-empty.jsonl", [][]Peer{nil}},
		{"../../testdata/stream/list-agents-bare-renamed.jsonl", [][]Peer{renamedPeers}},
	} {
		results := recordedResults(t, tc.fixture)
		if len(results) != len(tc.results) {
			t.Fatalf("%s carries %d results, want %d", tc.fixture, len(results), len(tc.results))
		}
		for i, text := range results {
			peers, dropped, ok := PeersFromListAgents(text)
			if !ok || dropped != 0 || !reflect.DeepEqual(peers, tc.results[i]) {
				t.Errorf("%s result %d = (%+v, %d dropped, %v), want (%+v, 0 dropped, true)",
					tc.fixture, i, peers, dropped, ok, tc.results[i])
			}
		}
	}
}

// A session renamed after holding its name a while is listed with a column
// between its name and directory, `says it was <old> until <age> ago`. It is
// offered under its new name only: a name is never an alias.
func TestARenamedSessionIsListedUnderItsNewNameOnly(t *testing.T) {
	const dir = "/private/tmp/wake-rec/alpha"
	for name, row := range map[string]string{
		"the recorded row":            "  [idle]  ·  wf-delta  ·  says it was wf-alpha until 3s ago  ·  " + dir + "  ·  started 1m ago",
		"hours since":                 "  [busy]  ·  wf-delta  ·  says it was wf-alpha until 2h ago  ·  " + dir + "  ·  started 3d ago",
		"a former name holding until": "  [idle]  ·  wf-delta  ·  says it was wait until noon until 4m ago  ·  " + dir + "  ·  started 1h ago",
	} {
		t.Run(name, func(t *testing.T) {
			peers, dropped, ok := PeersFromListAgents("Other Claude sessions (1):\n" + row)
			want := []Peer{{Name: "wf-delta", Dir: dir}}
			if !ok || dropped != 0 || !reflect.DeepEqual(peers, want) {
				t.Errorf("PeersFromListAgents = (%+v, %d dropped, %v), want (%+v, 0 dropped, true)", peers, dropped, ok, want)
			}
		})
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
		if peers, _, ok := PeersFromListAgents(text); ok || peers != nil {
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
			peers, dropped, ok := PeersFromListAgents(tc.text)
			if !ok || dropped != 0 || !reflect.DeepEqual(peers, tc.peers) {
				t.Errorf("PeersFromListAgents = (%+v, %d dropped, %v), want (%+v, 0 dropped, true)", peers, dropped, ok, tc.peers)
			}
		})
	}
}

// A listing whose frame the parser was not shown is refused whole: no
// recognised header, a count that disagrees with the rows under it, or a line
// under it that does not open with a `[state]` column.
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
		"a state unbracketed":                 "Other Claude sessions (1):\n  idle  ·  wf-beta  ·  /private/tmp/wake-rec/beta  ·  started 19s ago",
		"an empty state":                      "Other Claude sessions (1):\n  []  ·  wf-beta  ·  /private/tmp/wake-rec/beta  ·  started 19s ago",
		"the empty form with more before it":  others + "\n\nNo subagents, teammates or other Claude sessions.",
		"the empty form with more after it":   "No subagents, teammates or other Claude sessions.\n" + betaRow,
		"an indented line that is no row":     "Other Claude sessions (2):\n" + betaRow + "\n  and one more",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if peers, _, ok := PeersFromListAgents(text); ok || peers != nil {
				t.Errorf("PeersFromListAgents = (%+v, %v), want (nil, false)", peers, ok)
			}
		})
	}
}

// A row of a shape the parser was not shown, in a listing whose frame it was,
// is dropped and counted: the rows beside it are still offered, so the next
// column claude adds costs one session rather than the whole menu.
func TestAnUnrecognisedRowIsDroppedFromARecognisedListing(t *testing.T) {
	const dir = "  ·  /private/tmp/wake-rec/gamma  ·  started 19s ago"
	for name, row := range map[string]string{
		"a column missing":        "  [idle]  ·  wf-gamma  ·  started 19s ago",
		"a column more":           alphaRow + "  ·  remote",
		"a relative directory":    "  [idle]  ·  wf-gamma  ·  tmp/gamma  ·  started 19s ago",
		"a blank name":            "  [idle]  ·  " + dir,
		"an annotation not shown": "  [idle]  ·  wf-gamma  ·  says hello" + dir,
		"a renamed row with more": "  [idle]  ·  wf-gamma  ·  says it was wf-alpha until 3s ago" + dir + "  ·  remote",
		"a rename with no age":    "  [idle]  ·  wf-gamma  ·  says it was wf-alpha" + dir,
	} {
		t.Run(name, func(t *testing.T) {
			peers, dropped, ok := PeersFromListAgents("Other Claude sessions (2):\n" + betaRow + "\n" + row)
			want := []Peer{recordedPeers[0]}
			if !ok || dropped != 1 || !reflect.DeepEqual(peers, want) {
				t.Errorf("PeersFromListAgents = (%+v, %d dropped, %v), want (%+v, 1 dropped, true)", peers, dropped, ok, want)
			}
		})
	}
	peers, dropped, ok := PeersFromListAgents("Other Claude sessions (1):\n  [idle]  ·  wf-gamma  ·  started 19s ago")
	if !ok || dropped != 1 || peers != nil {
		t.Errorf("a listing of one unreadable row = (%+v, %d dropped, %v), want (nil, 1 dropped, true)", peers, dropped, ok)
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

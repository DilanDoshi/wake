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
	{Name: "wf-beta", Dir: "/private/tmp/wake-rec/beta", State: "idle"},
	{Name: "wf-alpha", Dir: "/private/tmp/wake-rec/alpha", State: "idle"},
}

// renameReply marks the one recorded result that is a /rename, not a listing.
var renameReply = []Peer{{Name: "(the rename reply)"}}

// Every result the four recordings carry is read: a live session's listings
// (which open with its own name) and a bare one-shot's (which do not), each
// empty form, and the rename as a rename - and neither as the other.
func TestEveryRecordedLocalReplyParses(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		results [][]Peer // in order; renameReply marks the rename
	}{
		{"../../testdata/stream/list-agents.jsonl", [][]Peer{recordedPeers, renameReply, recordedPeers}},
		{"../../testdata/stream/list-agents-empty.jsonl", [][]Peer{nil}},
		{"../../testdata/stream/list-agents-bare.jsonl", [][]Peer{recordedPeers}},
		{"../../testdata/stream/list-agents-bare-empty.jsonl", [][]Peer{nil}},
	} {
		results := recordedResults(t, tc.fixture)
		if len(results) != len(tc.results) {
			t.Fatalf("%s carries %d results, want %d", tc.fixture, len(results), len(tc.results))
		}
		for i, text := range results {
			want := tc.results[i]
			if reflect.DeepEqual(want, renameReply) {
				assertRename(t, text, "wf-delta")
				continue
			}
			peers, ok := PeersFromListAgents(text)
			if !ok || !reflect.DeepEqual(peers, want) {
				t.Errorf("%s result %d = (%+v, %v), want (%+v, true)", tc.fixture, i, peers, ok, want)
			}
		}
	}
}

func assertRename(t *testing.T, text, want string) {
	t.Helper()
	if got, ok := RenamedFromReply(text); !ok || got != want {
		t.Errorf("RenamedFromReply(%q) = (%q, %v), want (%q, true)", text, got, ok, want)
	}
	if _, ok := PeersFromListAgents(text); ok {
		t.Errorf("the rename reply %q parses as a listing", text)
	}
}

const (
	selfLine = "This session: wf-gamma [68bfa0] (the name other sessions use to message it)"
	betaRow  = "  [idle]  ·  wf-beta  ·  /private/tmp/wake-rec/beta  ·  started 19s ago"
	alphaRow = "  [idle]  ·  wf-alpha  ·  /private/tmp/wake-rec/alpha  ·  started 19s ago"
)

// others is the recorded Other-sessions section, both rows under its header.
const others = "Other Claude sessions (2):\n" + betaRow + "\n" + alphaRow

// bothForms is text as a live session prints it (its own name first) and as a
// bare one-shot does (no self line).
func bothForms(body string) map[string]string {
	return map[string]string{"live": selfLine + "\n\n" + body, "bare": body}
}

// Any other `<Title> (<n>):` section - the subagents and teammates the listing
// also names - is counted and skipped, so an agent with a background subagent
// still reports the machine's sessions. Its rows are never parsed.
func TestOtherSectionsAreCountedAndSkipped(t *testing.T) {
	const subagents = "Subagents (1):\n  explorer (running, 2s)"
	for _, tc := range []struct {
		name, body string
		peers      []Peer
	}{
		{"a section before the peers", subagents + "\n\n" + others, recordedPeers},
		{"a section after the peers", others + "\n\n" + subagents, recordedPeers},
		{"sections back to back, ended by the next header", subagents +
			"\nTeammates (2):\n  a\n  b\n\n" + others, recordedPeers},
		{"a skipped section alone", subagents, nil},
	} {
		for form, text := range bothForms(tc.body) {
			t.Run(tc.name+"/"+form, func(t *testing.T) {
				peers, ok := PeersFromListAgents(text)
				if !ok || !reflect.DeepEqual(peers, tc.peers) {
					t.Errorf("PeersFromListAgents = (%+v, %v), want (%+v, true)", peers, ok, tc.peers)
				}
			})
		}
	}
}

// A shape the parser was not shown is refused whole, in either form - never
// the rows it could read beside one it could not.
func TestAnUnrecognisedListingIsRefusedWhole(t *testing.T) {
	bodies := []struct{ name, body string }{
		{"the count disagrees", "Other Claude sessions (3):\n" + betaRow + "\n" + alphaRow},
		{"a count that is no number", "Other Claude sessions (two):\n" + betaRow + "\n" + alphaRow},
		{"a skipped section's count disagrees", "Subagents (2):\n  explorer\n\n" + others},
		{"an unknown line after the sections", others + "\n\nTip: message a session by its name."},
		{"an unknown line inside a section", "Other Claude sessions (2):\n" + betaRow + "\nand one more\n" + alphaRow},
		{"a row under no header", "Other Claude sessions (1):\n" + betaRow + "\n\n" + alphaRow},
		{"rows parted from their header", "Other Claude sessions (2):\n\n" + betaRow + "\n" + alphaRow},
		{"a row with a column missing", "Other Claude sessions (1):\n  [idle]  ·  wf-beta  ·  started 19s ago"},
		{"a row with a column more", "Other Claude sessions (1):\n" + betaRow + "  ·  remote"},
		{"a state unbracketed", "Other Claude sessions (1):\n  idle  ·  wf-beta  ·  /private/tmp/wake-rec/beta  ·  started 19s ago"},
		{"a relative directory", "Other Claude sessions (1):\n  [idle]  ·  wf-beta  ·  tmp/beta  ·  started 19s ago"},
		{"a blank name", "Other Claude sessions (1):\n  [idle]  ·    ·  /private/tmp/wake-rec/beta  ·  started 19s ago"},
		{"the empty form with more before it", others + "\n\nNo subagents, teammates or other Claude sessions."},
		{"the empty form with more after it", "No subagents, teammates or other Claude sessions.\n" + betaRow},
		{"a self line after the rows", others + "\n\n" + selfLine},
	}
	cases := map[string]string{
		"empty":               "",
		"prose":               "I can list the other sessions if you like.",
		"the model reply":     "Current model: Opus 5 (effort: xhigh)",
		"the rename reply":    "Session renamed to: wf-delta",
		"the self line alone": selfLine,
		"no short id":         "This session: wf-gamma (the name other sessions use to message it)\n\n" + others,
	}
	for _, b := range bodies {
		for form, text := range bothForms(b.body) {
			cases[b.name+"/"+form] = text
		}
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

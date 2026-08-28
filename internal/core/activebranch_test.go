package core

// The active-branch reconstruction: given a conversation's on-disk transcript
// nodes, which uuids lie on the path from root to the live leaf. Rewind leaves
// the rewound turns as a dead branch under an earlier node and writes the
// continuation after the marker, so newest-branch-wins recovers the live one.

import "testing"

// u, a and marker build the three node shapes ActiveBranch reads: a user node,
// an assistant node, and a rewind marker - a rewound last-prompt line whose
// leafUuid names the node the conversation was rewound to.
func u(uuid, parent string) TranscriptNode {
	return TranscriptNode{UUID: uuid, ParentUUID: parent, Kind: "user"}
}

func a(uuid, parent string) TranscriptNode {
	return TranscriptNode{UUID: uuid, ParentUUID: parent, Kind: "assistant"}
}

func marker(leaf string) TranscriptNode {
	return TranscriptNode{Kind: "last-prompt", Rewound: true, LeafUUID: leaf}
}

func TestActiveBranch(t *testing.T) {
	cases := []struct {
		name string
		in   []TranscriptNode
		want []string // uuids expected IN the active set, order-independent
		gone []string // uuids expected NOT in it (dead branch)
	}{
		{
			name: "no rewind is linear",
			in:   []TranscriptNode{u("a", ""), u("b", "a"), u("c", "b")},
			want: []string{"a", "b", "c"},
		},
		{
			name: "one rewind drops the dead branch",
			// a -> b(assistant leaf). dead: c(user) -> d, written first.
			// marker -> b. active: e(user) -> f, written after the marker.
			in: []TranscriptNode{
				u("a", ""), a("b", "a"),
				u("c", "b"), a("d", "c"),
				marker("b"),
				u("e", "b"), a("f", "e"),
			},
			want: []string{"a", "b", "e", "f"},
			gone: []string{"c", "d"},
		},
		{
			name: "stacked rewinds keep only the newest branch",
			in: []TranscriptNode{
				u("a", ""), a("b", "a"),
				u("c", "b"), a("d", "c"), marker("b"),
				u("e", "b"), a("g", "e"), marker("b"), // rewound to b again
				u("h", "b"), a("i", "h"), // newest branch
			},
			want: []string{"a", "b", "h", "i"},
			gone: []string{"c", "d", "e", "g"},
		},
		{
			name: "rewind with no continuation excludes the dead branch",
			// a -> b(leaf, rewound to). dead: c(user) -> d, written before the
			// marker. No node after the marker: the reply hasn't been sent yet.
			in: []TranscriptNode{
				u("a", ""), a("b", "a"),
				u("c", "b"), a("d", "c"),
				marker("b"),
			},
			want: []string{"a", "b"},
			gone: []string{"c", "d"},
		},
		{
			name: "stacked rewind with no continuation keeps only the prefix",
			in: []TranscriptNode{
				u("a", ""), a("b", "a"),
				u("c", "b"), a("d", "c"), marker("b"),
				u("e", "b"), a("g", "e"), marker("b"), // rewound to b again, nothing sent since
			},
			want: []string{"a", "b"},
			gone: []string{"c", "d", "e", "g"},
		},
		{
			// Adversarial review, 2026-08-26: a marker whose LeafUUID names no
			// node - malformed, never written by a real rewind, since the
			// target has to already exist to be rewound to - used to fall
			// straight through to lastWritten with no cutoff at all.
			// lastWritten's own backward walk climbs parent pointers
			// unconditionally, so it resurrected c/d wholesale (the branch the
			// FIRST, valid marker had already killed) and x with them, while
			// dropping e/f - the actual live branch - entirely.
			name: "a garbage marker after a valid rewind does not resurrect the dead branch",
			in: []TranscriptNode{
				u("a", ""), a("b", "a"),
				u("c", "b"), a("d", "c"), // dead: killed by the marker below
				marker("b"),
				u("e", "b"), a("f", "e"), // live: written after the marker
				marker("does-not-exist"), // malformed: no node carries this uuid
				u("x", "d"),              // grafted onto the dead branch afterwards
			},
			want: []string{"a", "b", "e", "f"},
			gone: []string{"c", "d", "x"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set := ActiveBranch(tc.in)
			for _, w := range tc.want {
				if !set[w] {
					t.Errorf("%s: %q missing from the active set", tc.name, w)
				}
			}
			for _, g := range tc.gone {
				if set[g] {
					t.Errorf("%s: %q should be dead but is active", tc.name, g)
				}
			}
		})
	}
}

// The recorded rewind fixture proves the reconstruction on a real transcript.
// It rewinds past a "remember 42" turn and its "7,42" answer; the active set
// must hold the branch written after the rewind marker and drop the one before.
func TestActiveBranchOverTheRecordedRewind(t *testing.T) {
	lines := readLines(t, "testdata/transcript/rewind-tree.jsonl")
	var nodes []TranscriptNode
	for _, l := range lines {
		if n, ok := DecodeTranscriptNode(l); ok {
			nodes = append(nodes, n)
		}
	}
	set := ActiveBranch(nodes)

	const (
		liveTarget = "e1a3188f-12d6-4b11-9ba5-86f2a346ac32" // "ok" - the rewind leaf
		liveReask  = "54bef517-1e4c-48a2-a09b-0fc6fbac2841" // the re-asked question
		liveLeaf   = "6e106760-7e6a-41dc-85f2-4042d7aa954d" // the live answer, "7"

		deadAsk42 = "209bcbe6-f911-4de6-8316-f8c4a7b2cfbb" // "remember 42"
		deadOK    = "fa30dd21-072c-4912-a276-014a9fdc60a1"
		deadList  = "83767235-9760-449c-a942-5c30a6681dd6"
		deadAns   = "0e30d10d-61ba-4ec6-8dc0-7acdacf6efcd" // "7,42"
	)
	for _, id := range []string{liveTarget, liveReask, liveLeaf} {
		if !set[id] {
			t.Errorf("%s is on the live branch but the reconstruction dropped it", id)
		}
	}
	for _, id := range []string{deadAsk42, deadOK, deadList, deadAns} {
		if set[id] {
			t.Errorf("%s was rewound away but the reconstruction kept it: the rewound turn comes back on reopen", id)
		}
	}
}

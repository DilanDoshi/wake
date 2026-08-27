package core

// Reconstructing a conversation's live branch from its on-disk tree.
//
// A rewind does not delete the rewound turns: claude writes them to disk under
// a dead branch hanging off an earlier node, and writes the continuation after
// a rewind marker. So on reopen both branches are on disk, and the reader has
// to choose the live one - or the rewound turns come back. The choice is file
// order: among a node's children the one written last is the live one.

// ActiveBranch returns the uuids on the live path of a conversation's tree,
// from the root down to the live leaf. The leaf is found by descending from
// the latest rewind marker whose leaf resolves to a node - or, with no
// marker that resolves, from the last node written - taking the newest child
// at each fork that was written *after* that marker ("newest branch wins");
// the active set is that leaf's path back to the root. See spec §5.
//
// A rewind's dead branch is written before its marker, so a plain "newest
// child" walk would still descend into it the moment the marker has no
// continuation yet - the operator rewound but hasn't sent the replacement
// turn. markerOrder is the cutoff that keeps that dead branch out: only a
// child written after the latest marker may be descended into.
//
// The latest marker is trusted only once it resolves. A marker naming a leaf
// no node carries - never written by a real rewind, since the target has to
// already exist on disk to be rewound to - used to fall straight through to
// lastWritten with no cutoff at all: the backward walk below climbs parent
// pointers unconditionally, with nothing to stop it crossing back through a
// branch an EARLIER, valid rewind had already killed. Walking the markers
// newest to oldest and taking the first whose leaf resolves keeps that
// earlier marker's own cutoff in force instead of discarding it. Not
// reachable from a real Claude Code transcript; bulletproofing all the same.
//
// Pure: nodes in, uuid set out. It reads only tree structure - identity,
// parentage and the rewind markers - never message content.
func ActiveBranch(nodes []TranscriptNode) map[string]bool {
	parent := make(map[string]string, len(nodes))
	order := make(map[string]int, len(nodes))
	children := map[string][]string{}
	type rewindMarker struct {
		leaf string
		idx  int
	}
	var markers []rewindMarker
	for i, n := range nodes {
		// Rewound is set by the airlock on a rewind marker and on nothing else,
		// so it stands in for the marker kind without naming Claude's wire word.
		// Collected in file order; which one is trusted is decided below, once
		// order is complete enough to test resolvability against.
		if n.Rewound {
			markers = append(markers, rewindMarker{leaf: n.LeafUUID, idx: i})
			continue
		}
		if n.UUID == "" {
			continue
		}
		parent[n.UUID] = n.ParentUUID
		order[n.UUID] = i
		children[n.ParentUUID] = append(children[n.ParentUUID], n.UUID)
	}

	// The newest marker whose leaf actually names a node. None resolving - no
	// rewind at all, or every marker malformed - reads the same as no rewind:
	// start from the last node written, with no cutoff.
	start, markerOrder := "", -1
	for i := len(markers) - 1; i >= 0; i-- {
		if _, ok := order[markers[i].leaf]; ok {
			start, markerOrder = markers[i].leaf, markers[i].idx
			break
		}
	}
	if start == "" {
		start = lastWritten(order)
	}
	leaf := descendNewest(start, children, order, markerOrder)

	set := make(map[string]bool, len(nodes))
	for u := leaf; u != ""; u = parent[u] {
		if set[u] {
			break // a parent cycle in a malformed transcript ends the walk
		}
		set[u] = true
	}
	return set
}

// lastWritten is the uuid with the greatest file order - the last node the
// transcript wrote, and the leaf of a conversation that was never rewound.
func lastWritten(order map[string]int) string {
	best, bi := "", -1
	for u, i := range order {
		if i > bi {
			best, bi = u, i
		}
	}
	return best
}

// descendNewest follows the newest child at each fork down to the live leaf,
// considering only a child written after cutoff - the latest rewind marker's
// file-order index, or -1 with no rewind, so every child qualifies. A child
// at or before cutoff is a dead branch a rewind left behind; if none of a
// node's children qualify - no kids at all, or a rewind with no continuation
// written yet - that node is the live leaf itself. seen stops a child cycle
// in a malformed transcript.
func descendNewest(u string, children map[string][]string, order map[string]int, cutoff int) string {
	seen := map[string]bool{}
	for !seen[u] {
		seen[u] = true
		next, ni := "", cutoff
		for _, k := range children[u] {
			if order[k] > ni {
				next, ni = k, order[k]
			}
		}
		if next == "" {
			return u
		}
		u = next
	}
	return u
}

package ui

// A subagent's own subagents (claude 2.1.314 names the parent on task_started):
// the running dispatches as a tree, so the sidebar draws each under the one that
// dispatched it. Pure, for tasks.go's reason.

// taskTree is the rows listed keeps, in tree order: each after the nearest listed
// row it descends from, siblings in the order they started, Depth counting the
// listed rows above it. One whose ancestors are all unlisted - finished, or
// never seen - stands at the top. Parent is the wire's word, so a loop in it
// ends at the top too, and nothing listed is ever dropped.
func taskTree(rows []Task, listed func(Task) bool) []Task {
	kept := make([]Task, 0, len(rows))
	nests := false
	for _, row := range rows {
		if listed(row) {
			kept = append(kept, row)
			nests = nests || row.Parent != ""
		}
	}
	if !nests {
		return kept // every draw asks, and almost every one is flat: no map built
	}
	return treeOf(rows, kept, listed)
}

// treeOf orders kept as taskTree says, reading rows for the unlisted ancestors.
func treeOf(rows, kept []Task, listed func(Task) bool) []Task {
	byID := make(map[string]Task, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	var tops []Task
	children := map[string][]Task{}
	for _, row := range kept {
		if above, ok := listedAncestor(row, byID, listed, len(rows)); ok {
			children[above] = append(children[above], row)
		} else {
			tops = append(tops, row)
		}
	}
	out := make([]Task, 0, len(kept))
	placed := map[string]bool{}
	var place func(row Task, depth int)
	place = func(row Task, depth int) {
		if placed[row.ID] {
			return
		}
		placed[row.ID] = true
		row.Depth = depth
		out = append(out, row)
		for _, child := range children[row.ID] {
			place(child, depth+1)
		}
	}
	for _, row := range tops {
		place(row, 0)
	}
	for _, row := range kept { // a loop has no top to be reached from
		place(row, 0)
	}
	return out
}

// listedAncestor is the nearest listed row above row, in at most bound steps so
// a loop in Parent ends.
func listedAncestor(row Task, byID map[string]Task, listed func(Task) bool, bound int) (string, bool) {
	p := row.Parent
	for range bound {
		above, ok := byID[p]
		if p == "" || !ok {
			return "", false
		}
		if listed(above) {
			return above.ID, true
		}
		p = above.Parent
	}
	return "", false
}

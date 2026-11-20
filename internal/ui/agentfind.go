package ui

// Finding an agent by a few letters of its name: the one matcher the jump picker uses.
// Case and spaces in the query do not count. A name's own start beats a run inside
// it, which beats the letters found in order with others between - `blu` finds
// web-blur before docs-bulk-upload.

import (
	"slices"
	"strings"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// findClass is how well query finds name: 0 for its start, 1 for a run inside it, 2
// for its letters in order. ok is false when a letter is missing. An empty query finds
// everything at class 0.
func findClass(query, name string) (class int, ok bool) {
	q := strings.ToLower(strings.Join(strings.Fields(query), ""))
	n := strings.ToLower(name)
	switch {
	case strings.HasPrefix(n, q):
		return 0, true
	case strings.Contains(n, q):
		return 1, true
	}
	rest := []rune(q)
	for _, r := range n {
		if len(rest) > 0 && r == rest[0] {
			rest = rest[1:]
		}
	}
	return 2, len(rest) == 0
}

// matchAgents is the agents query finds, as a new slice: whoever is blocked on you
// first, then the better matches, and within each the order agents came in.
func matchAgents(query string, agents []Agent) []Agent {
	type hit struct {
		agent Agent
		class int
	}
	hits := make([]hit, 0, len(agents))
	for _, a := range agents {
		if class, ok := findClass(query, a.Name); ok {
			hits = append(hits, hit{a, class})
		}
	}
	slices.SortStableFunc(hits, func(x, y hit) int {
		if bx, by := x.agent.State == rpc.StateBlocked, y.agent.State == rpc.StateBlocked; bx != by {
			if bx {
				return -1
			}
			return 1
		}
		return x.class - y.class
	})
	out := make([]Agent, len(hits))
	for i, h := range hits {
		out[i] = h.agent
	}
	return out
}

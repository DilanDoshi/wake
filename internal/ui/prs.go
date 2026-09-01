package ui

import "slices"

// prSet is the GitHub pull requests a session has opened, held behind a pointer
// for commandSet's reason: Agent stays comparable because Observe decides whether
// an event moved anything by comparing two Agents with ==, and a slice field
// would make that a compile error. The daemon scrapes the numbers from a
// `gh pr create` tool result and carries them on the report; nothing on Claude's
// wire names a PR, so the report is the only source. Immutable - withPRs replaces
// the pointer rather than appending.
type prSet struct{ nums []int }

// numbers is what to draw, and nil for a session that has opened none.
func (p *prSet) numbers() []int {
	if p == nil {
		return nil
	}
	return p.nums
}

// same reports whether this set already holds exactly these numbers, which is
// what keeps an unchanged report from replacing the pointer and re-rendering the
// status bar every report (BUG-5's rule, one field over from commandSet.same).
func (p *prSet) same(nums []int) bool {
	if p == nil {
		return len(nums) == 0
	}
	return slices.Equal(p.nums, nums)
}

// withPRs replaces the set when a report names new PRs and keeps the pointer
// when the numbers have not changed. Report path only - nothing on the event
// stream carries a PR - and it only ever grows, so an empty report leaves the
// set alone the way withCommands does.
func (a Agent) withPRs(nums []int) Agent {
	if len(nums) > 0 && !a.prs.same(nums) {
		a.prs = &prSet{nums: slices.Clone(nums)}
	}
	return a
}

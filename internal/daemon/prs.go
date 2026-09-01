package daemon

// Scraping the pull requests a session has opened out of what it already
// streams. A session opens a PR by running `gh pr create`, whose tool result
// prints the URL; recognising that URL is how the status bar learns of the PR
// without a subprocess, a `gh` call, a poll or a timer - the "cheap to leave
// open" non-negotiable, which a per-agent process on a ticker violates thirty
// times over.
//
// It reads core.Event.Text - Wake's own decoded text, off a KindToolResult -
// never the raw JSON, so the airlock's four files stay the only place that
// knows Claude's wire. **A tool result, never prose**: a PR URL an agent writes
// in a reply is a link, not the output of a command it ran.
//
// **What it cannot tell apart** is a PR the session *opened* from one it merely
// *read*: the command that produced a result (`gh pr create` vs `gh pr view`)
// lived in the preceding tool_use, and the scrape does not correlate back to it -
// that is the accepted cost of the no-subprocess, no-correlation design the owner
// chose. So a `gh pr view`/`gh pr checkout` or a file that names a PR URL also
// lands here. The segment is therefore "PRs this session's tools surfaced",
// dominated in practice by the create it was built for.

import (
	"regexp"
	"slices"
	"strconv"
)

// prURL matches a GitHub pull-request URL and captures its number. The owner's
// chosen pattern: the owner and repo segments are "not a slash or space" so a
// trailing path or punctuation ends them, and the literal `/pull/` before the
// digits is what keeps a `/pulls` listing or an `/issues/29` link from reading
// as a PR.
var prURL = regexp.MustCompile(`github\.com/[^/\s]+/[^/\s]+/pull/(\d+)`)

// recordPRs returns prs with every new pull-request number in text appended,
// deduped by number and in first-seen order. It never removes one: a session's
// PRs only accumulate, so an empty text leaves the set as it was.
func recordPRs(prs []int, text string) []int {
	for _, m := range prURL.FindAllStringSubmatch(text, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || slices.Contains(prs, n) {
			continue
		}
		prs = append(prs, n)
	}
	return prs
}

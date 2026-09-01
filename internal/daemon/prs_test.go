package daemon

import (
	"reflect"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// recordPRs scrapes a GitHub pull-request number out of the text `gh pr create`
// prints, dedups by number, and keeps first-seen order.
func TestRecordPRsScrapesDedupsAndOrders(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		seed []int
		want []int
	}{
		{"one url", "https://github.com/acme/widgets/pull/29\n", nil, []int{29}},
		{"trailing path", "see https://github.com/acme/widgets/pull/29/files here", nil, []int{29}},
		{
			"two urls in first-seen order",
			"opened https://github.com/acme/widgets/pull/30 and https://github.com/acme/widgets/pull/29",
			nil,
			[]int{30, 29},
		},
		{"a cross-repo pr is still one", "https://github.com/upstream/widgets/pull/7", []int{7}, []int{7}},
		{"dedup against the seed", "https://github.com/acme/widgets/pull/29", []int{29}, []int{29}},
		{"appended after the seed", "https://github.com/acme/widgets/pull/30", []int{29}, []int{29, 30}},
		// The two shapes that read like a PR link and are not.
		{"a pulls listing is not a pull", "https://github.com/acme/widgets/pulls", nil, nil},
		{"an issue is not a pull", "https://github.com/acme/widgets/issues/29", nil, nil},
		{"no url at all", "the build is green", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := recordPRs(tc.seed, tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("recordPRs(%v, %q) = %v, want %v", tc.seed, tc.text, got, tc.want)
			}
		})
	}
}

// The scrape is a tool result and not prose: a PR URL an agent writes in a reply
// is a link, not the output of a command it ran.
func TestObserveRecordsAPRFromAToolResultAndNotFromProse(t *testing.T) {
	a := newAgent(idAlpha, "alex", "dev", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	a.observe(core.Event{Kind: core.KindAssistantText, Text: "I will open https://github.com/acme/widgets/pull/41"})
	if a.prs != nil {
		t.Fatalf("a PR URL in prose was scraped: %v", a.prs)
	}

	a.observe(core.Event{Kind: core.KindToolResult, Text: "https://github.com/acme/widgets/pull/41\n"})
	if got, want := a.prs, []int{41}; !reflect.DeepEqual(got, want) {
		t.Errorf("after a tool result the session's PRs = %v, want %v", got, want)
	}
}

// A subagent's tool result is not the parent's own: a subagent that runs
// `gh pr create` opened its own PR, and the parent's status bar must not claim it.
// The gate is the tree's standing rule for tool activity - fold, rollup and the
// checklist all exclude a subagent-attributed event.
func TestObserveIgnoresASubagentsToolResult(t *testing.T) {
	a := newAgent(idAlpha, "alex", "dev", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	a.observe(core.Event{
		Kind:     core.KindToolResult,
		Text:     "https://github.com/acme/widgets/pull/77",
		Subagent: &core.Subagent{Dispatch: "call-1"},
	})
	if a.prs != nil {
		t.Errorf("a subagent's PR was attributed to the parent: %v", a.prs)
	}
}

// The report is the only route a client that attached after the PR was opened
// has to it, so the snapshot has to carry it - the same reason Effort and
// Commands ride the report.
func TestASnapshotCarriesTheScrapedPRs(t *testing.T) {
	a := newAgent(idAlpha, "alex", "dev", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	a.observe(core.Event{Kind: core.KindToolResult, Text: "https://github.com/acme/widgets/pull/12"})

	if got, want := a.snapshot().PRs, []int{12}; !reflect.DeepEqual(got, want) {
		t.Errorf("snapshot().PRs = %v, want %v", got, want)
	}
}

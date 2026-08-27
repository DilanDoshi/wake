// Telling a question apart from a permission ask, and from a plan.
//
// One control_request subtype carries all three, so the classification is read
// off two wire fields and never off a tool's name. What makes these worth
// having is the second test: the label has to line up with what a bare allow
// recorded doing, or it is a name with nothing behind it.
//
// This file is one of the airlock's own tests, so it may name Claude's frame
// types - and only ever to prove the airlock decodes them. See
// fixtures_helpers_test.go's header for the full list.

package core

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// wantAsk is what a recorded ask should classify as, read off the raw line by
// a path that shares nothing with the decoder.
//
// requires_user_interaction is a *bool so that absent and explicitly false are
// separable here even though askKind treats them alike. The corpus carries no
// explicit false at all, which is asserted below rather than assumed: it is
// the whole basis for reading "absent" as "not interactive", and a re-recording
// that started emitting false would make that reading a guess again.
type wantAsk struct {
	kind     AskKind
	explicit bool
}

func wantAskOf(t *testing.T, line string) (wantAsk, bool) {
	t.Helper()
	var f struct {
		Type    string `json:"type"`
		Request *struct {
			Subtype  string                     `json:"subtype"`
			Input    map[string]json.RawMessage `json:"input"`
			Requires *bool                      `json:"requires_user_interaction"`
		} `json:"request"`
	}
	if err := json.Unmarshal([]byte(line), &f); err != nil {
		t.Fatalf("fixture line is not JSON: %v", err)
	}
	if f.Type != "control_request" || f.Request == nil || f.Request.Subtype != "can_use_tool" {
		return wantAsk{}, false
	}
	w := wantAsk{explicit: f.Request.Requires != nil}
	switch {
	case f.Request.Requires == nil || !*f.Request.Requires:
		w.kind = AskPermission
	case f.Request.Input["questions"] != nil:
		w.kind = AskChoice
	default:
		w.kind = AskApproval
	}
	return w, true
}

// Every can_use_tool in the corpus, classified from its own two wire fields
// and compared against what DecodeLine made of it.
//
// Derived rather than listed on purpose. A table of (fixture, line, kind)
// would pin the fourteen asks that exist today and say nothing about the
// fifteenth, which is the shape this project keeps finding: a hand-written
// list standing in for something the bytes already declare. This walks the
// corpus, so a re-recording that changes an ask changes what is asserted about
// it.
//
// The counts underneath are what stop the walk from asserting nothing. All
// three classes have to be present in the numbers the recording actually
// holds - six ordinary permission asks, six questions, two plan approvals - so
// a decoder that collapsed every ask to one kind fails here even though the
// loop above would still run fourteen times.
func TestEveryRecordedAskIsClassifiedFromItsOwnWireFields(t *testing.T) {
	seen := map[AskKind]int{}
	explicitFalse := 0

	for _, path := range fixtureFiles(t) {
		for n, line := range fixtureLines(t, path) {
			want, isAsk := wantAskOf(t, line)
			if !isAsk {
				continue
			}
			if want.explicit && want.kind == AskPermission {
				explicitFalse++
			}
			evs, err := DecodeLine([]byte(line))
			if err != nil {
				t.Fatalf("%s:%d failed to decode: %v", path, n+1, err)
			}
			if len(evs) != 1 || evs[0].Kind != KindPermissionRequest {
				t.Fatalf("%s:%d decoded to %v, want one permission request", path, n+1, kinds(evs))
			}
			if evs[0].Ask != want.kind {
				t.Errorf("%s:%d decoded Ask = %q, want %q - the ask's own requires_user_interaction and payload say otherwise",
					path, n+1, evs[0].Ask, want.kind)
			}
			seen[want.kind]++
		}
	}

	// The recording's own arithmetic. Exact rather than a floor: these are
	// counted frames in committed bytes, so a change is a re-recording and
	// wants looking at, not a number to slacken.
	for _, tc := range []struct {
		kind AskKind
		want int
		what string
	}{
		// 6 until the resume/fork spike landed, which added exactly three more -
		// one each in resume-plain-permission, resume-fork-parent and
		// resume-fork-no-session-id. Checked rather than slackened: none of the
		// three carries requires_user_interaction, so all three are ordinary
		// permission asks and the classification is unchanged. That a resumed
		// and a forked process both ask this way is the point of §6 of the
		// findings, and this count is where it is held.
		//
		// 10 since the live-fork spike, which added exactly one:
		// live-fork-child.jsonl's Write ask, raised by a process forked from a
		// parent that was **still running**. Read off its own wire fields the
		// same way - request.requires_user_interaction absent, request.input
		// carrying only file_path and content and no questions - so it is an
		// ordinary permission ask and nothing about askKind moved. The claim
		// this count holds is §4 of 2026-08-10-live-fork-findings.md: a fork
		// taken from a live parent is a full Wake agent, asking on the same
		// control_request every other agent asks on.
		//
		// 11 since the tool-bounding spike, which added exactly one:
		// tools-allowed-manual.jsonl's Write ask. Read off its own wire fields
		// the same way - requires_user_interaction absent, request.input
		// carrying only content and file_path and no questions - so it is an
		// ordinary permission ask and askKind is untouched. The claim this
		// count holds is §3 of 2026-08-12-tool-bounding-findings.md:
		// --allowed-tools is an auto-approve list rather than a bound, so a
		// tool that is *not* on it raises exactly the ask every other tool
		// raises. It is the only fixture in that spike with an ask at all,
		// which is the same finding from the other side - a tool bounded away
		// by --tools produces no frame to assert on.
		//
		// 14 since the permission-mode spike, which added exactly three: the
		// Write asks in mode-set, mode-suggestion-accepted and mode-cycle.
		// Same reading of the same two wire fields, so all three are ordinary
		// permission asks. What the count holds is §3 of
		// 2026-08-12-permission-mode-findings.md: every one of them was raised
		// by a session whose permission mode had been changed *mid-session* by
		// a set_permission_mode control request, so that verb moves WHEN an ask
		// is raised and nothing about the SHAPE of one - which is why deferred
		// I7's ⇧⇥ needs nothing from the airlock. The mode-set pair is the
		// sharpest: its first Write asks and its second, after the mode change,
		// does not appear here at all because no ask was raised for it.
		{AskPermission, 14, "ordinary permission asks, none of which carries requires_user_interaction"},
		{AskChoice, 6, "AskUserQuestion asks, each carrying questions"},
		// 3 since the permission-mode spike, which added exactly one:
		// mode-cycle's ExitPlanMode, raised by a session put into plan mode by
		// a set_permission_mode control request rather than by
		// --permission-mode. That is the count's whole claim - a mode reached
		// mid-session is the real mode and produces the same ask a mode reached
		// at spawn does, so the plan arm of I7's cycle needs nothing new here
		// either.
		{AskApproval, 3, "ExitPlanMode asks, interactive and carrying no questions"},
	} {
		if seen[tc.kind] != tc.want {
			t.Errorf("walked %d asks of kind %q, the corpus holds %d %s", seen[tc.kind], tc.kind, tc.want, tc.what)
		}
	}
	if explicitFalse != 0 {
		t.Errorf("%d asks carry an explicit requires_user_interaction: false - askKind reads absent as not-interactive, "+
			"which was only ever safe because the corpus never distinguished them", explicitFalse)
	}
}

// The classification has to line up with what the recording says a bare allow
// actually did, or it is a label with nothing behind it.
//
// This is the A/B the spike exists to deliver, read out of the committed
// bytes: two asks classified AskChoice, answered differently, with the model
// told different things; and one classified AskApproval whose bare allow was a
// complete approval. The join is the tool_use_id, so nothing here depends on
// line numbers.
func TestTheClassificationMatchesWhatABareAllowActuallyDid(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		ask     AskKind
		want    string
		why     string
	}{
		{
			fixture: "question-answer.jsonl",
			ask:     AskChoice,
			want:    "Your questions have been answered:",
			why:     "the allow carried updatedInput.answers",
		},
		{
			fixture: "question-bare-allow.jsonl",
			ask:     AskChoice,
			want:    "The user did not answer the questions.",
			why:     "the allow was bare - the same bytes Wake wrote before this change",
		},
		{
			fixture: "question-plan-bare.jsonl",
			ask:     AskApproval,
			want:    "User has approved your plan.",
			why:     "a bare allow is the whole approval for a plan",
		},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			ask, result := askAndItsResult(t, tc.fixture, tc.ask)
			if ask.Ask != tc.ask {
				t.Fatalf("the ask decoded as %q, want %q", ask.Ask, tc.ask)
			}
			if !strings.Contains(result, tc.want) {
				t.Errorf("the model was told %.80q, want it to contain %q (%s)", result, tc.want, tc.why)
			}
		})
	}
}

// askAndItsResult finds the last ask of the given kind in a fixture and the
// tool_result the CLI produced for it, joined on the tool_use_id.
//
// Last rather than first because the plan fixtures hold two asks and the
// ExitPlanMode one closes them; for the single-ask fixtures the two are the
// same frame.
func askAndItsResult(t *testing.T, fixture string, kind AskKind) (Event, string) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "stream", fixture)

	var ask Event
	found := false
	for _, ev := range decodeFixture(t, path) {
		if ev.Kind == KindPermissionRequest && ev.Ask == kind {
			ask, found = ev.Event, true
		}
	}
	if !found {
		t.Fatalf("%s holds no ask of kind %q", fixture, kind)
	}
	if ask.Tool == nil || ask.Tool.ID == "" {
		t.Fatalf("%s: the ask carries no tool_use_id, so nothing joins it to its result", fixture)
	}

	results := 0
	text := ""
	for _, ev := range decodeFixture(t, path) {
		if ev.Kind == KindToolResult && ev.Tool != nil && ev.Tool.ID == ask.Tool.ID {
			text, results = ev.Text, results+1
		}
	}
	if results != 1 {
		t.Fatalf("%s: %d tool results name %s, want exactly 1", fixture, results, ask.Tool.ID)
	}
	return ask, text
}

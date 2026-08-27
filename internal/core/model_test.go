package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The list is claude's own, read off the reply the bare command prints, and
// this is what makes it derived rather than typed.
//
// The recording is the authority because it is the only source that enumerates:
// the init frame names one model, and --help gives an e.g. A drift between the
// two fails here with the corrected list in the message.
func TestTheModelListIsTheOneTheRecordedReplyNames(t *testing.T) {
	got := modelsInRecordedReply(t)
	if !slices.Equal(got, ModelAliases) {
		t.Errorf("the recorded /model reply offers\n  %v\nand this build offers\n  %v\n"+
			"Re-read testdata/stream/bare-model.jsonl rather than editing one of them to match.", got, ModelAliases)
	}
}

// modelsInRecordedReply is the "Available: …" list out of the fixture.
//
// It stops at "or a full model ID", which is claude's own name for the typed
// escape and not a model - so a parser that kept it would offer an option that
// selects nothing.
func modelsInRecordedReply(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "stream", "bare-model.jsonl"))
	if err != nil {
		t.Fatalf("read the recorded reply: %v", err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		_, after, found := strings.Cut(line, "Available: ")
		if !found {
			continue
		}
		list, _, _ := strings.Cut(after, ", or a full model ID")
		out := make([]string, 0, 10)
		for _, name := range strings.Split(list, ",") {
			if name = strings.TrimSpace(name); name != "" {
				out = append(out, name)
			}
		}
		return out
	}
	t.Fatal("no `Available:` line in the recorded /model reply; the reply's shape changed")
	return nil
}

// --help remains a second and weaker source: it gives examples, so it can only
// be checked as a subset. Kept because one list moving without the other is
// exactly what a single source cannot catch.
func TestTheHelpExamplesAreAllOffered(t *testing.T) {
	for _, alias := range []string{"opus", "sonnet", "fable"} {
		if !slices.Contains(ModelAliases, alias) {
			t.Errorf("%q is named by claude --help and is not offered", alias)
		}
	}
}

// The typed escape is why an incomplete list is safe: a model shipped tomorrow
// must be reachable without a Wake release. A predicate that refused an unknown
// name would turn the sample into a fence.
func TestAnyNonEmptyModelIsAllowedThrough(t *testing.T) {
	if !ValidModel("claude-something-not-invented-yet") {
		t.Error("an unknown model was refused; the list is a sample, not a gate")
	}
	if ValidModel("") {
		t.Error("the empty string is the absence of a model, not a model")
	}
}

// The extracted fixture is the check on both lists, and it checks them
// differently on purpose: --help prints the effort set closed, so a mismatch is
// real drift, and it prints the model aliases as examples, so only containment
// is knowable.
func TestTheFixtureAgreesWithTheConstants(t *testing.T) {
	var fixture struct {
		Version           string   `json:"claude_code_version"`
		EffortLevels      []string `json:"effort_levels"`
		ModelAliasSamples []string `json:"model_alias_examples"`
	}
	body, err := os.ReadFile(filepath.Join("testdata", "claude-flags.json"))
	if err != nil {
		t.Fatalf("read the extracted flags: %v", err)
	}
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatalf("decode the extracted flags: %v", err)
	}
	if !slices.Equal(fixture.EffortLevels, EffortLevels) {
		t.Errorf("--effort takes %v at %s and this build passes %v; re-run scripts/extract-claude-flags.py",
			fixture.EffortLevels, fixture.Version, EffortLevels)
	}
	for _, alias := range fixture.ModelAliasSamples {
		if !slices.Contains(ModelAliases, alias) {
			t.Errorf("claude --help names %q at %s and the picker does not offer it", alias, fixture.Version)
		}
	}
}

// A model reaches the argv as its own word, and a config without one emits no
// flag at all. Effort's own guard, for the field that until now had an emitter
// and no producer.
func TestOnlyAChosenModelReachesTheArgv(t *testing.T) {
	args := argsFor(t, Config{SessionID: "s1", Model: "opus"})
	i := indexOf(args, "--model")
	if i < 0 {
		t.Fatalf("a chosen model produced no --model in %v", args)
	}
	if args[i+1] != "opus" {
		t.Errorf("--model carried %q, want %q", args[i+1], "opus")
	}
	if args := argsFor(t, Config{SessionID: "s1"}); indexOf(args, "--model") >= 0 {
		t.Errorf("a config with no model emitted --model: %v", args)
	}
}
